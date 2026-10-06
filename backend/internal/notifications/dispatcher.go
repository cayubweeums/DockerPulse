package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dockpulse/dockmgr/internal/database"
)

type AlertPayload struct {
	Title   string `json:"title"`
	Message string `json:"message"`
	Type    string `json:"type"` // "update", "info", "warning", "error"
	Host    string `json:"host,omitempty"`
}

type NtfyConfig struct {
	ServerURL string `json:"server_url"` // default https://ntfy.sh
	Topic     string `json:"topic"`
	Token     string `json:"token,omitempty"`
	Priority  string `json:"priority,omitempty"` // default "default" or "high"
	Tags      string `json:"tags,omitempty"`     // e.g. "package,docker"
}

type DiscordConfig struct {
	WebhookURL string `json:"webhook_url"`
	Username   string `json:"username,omitempty"`
	AvatarURL  string `json:"avatar_url,omitempty"`
}

type SignalConfig struct {
	EndpointURL string `json:"endpoint_url"` // e.g. http://signal-cli:8080/v2/send
	Number      string `json:"number,omitempty"`
	Recipients  string `json:"recipients"` // comma-separated phone numbers or group IDs
}

type Dispatcher struct {
	db     *database.DB
	client *http.Client
}

func NewDispatcher(db *database.DB) *Dispatcher {
	return &Dispatcher{
		db: db,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Dispatch sends the alert to all enabled services concurrently and records status/errors
func (d *Dispatcher) Dispatch(ctx context.Context, alert AlertPayload) error {
	configs, err := d.db.GetNotificationConfigs()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	var errs []string
	var mu sync.Mutex

	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}

		wg.Add(1)
		go func(c database.NotificationConfig) {
			defer wg.Done()
			sendErr := d.SendToService(ctx, c.ID, c.ConfigJSON, alert)

			mu.Lock()
			defer mu.Unlock()

			if sendErr != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", c.ID, sendErr))
				c.Status = "error"
				c.LastError = sendErr.Error()
			} else {
				c.Status = "enabled"
				c.LastError = ""
			}
			_ = d.db.SaveNotificationConfig(&c)
		}(cfg)
	}

	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("dispatch errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// SendToService routes alert payload to the appropriate service handler
func (d *Dispatcher) SendToService(ctx context.Context, serviceID string, configJSON string, alert AlertPayload) error {
	switch strings.ToLower(serviceID) {
	case "ntfy":
		return d.sendNtfy(ctx, configJSON, alert)
	case "discord":
		return d.sendDiscord(ctx, configJSON, alert)
	case "signal":
		return d.sendSignal(ctx, configJSON, alert)
	default:
		return fmt.Errorf("unsupported notification service: %s", serviceID)
	}
}

// TestService sends a test alert to verify service configuration and credentials
func (d *Dispatcher) TestService(ctx context.Context, serviceID string, configJSON string) error {
	testAlert := AlertPayload{
		Title:   "DockerPulse Test Notification",
		Message: "Connection successful! DockerPulse is configured to send notifications to this channel.",
		Type:    "info",
		Host:    "DockerPulse Manager",
	}

	err := d.SendToService(ctx, serviceID, configJSON, testAlert)
	cfg, getErr := d.db.GetNotificationConfig(serviceID)
	if getErr == nil {
		if err != nil {
			cfg.Status = "error"
			cfg.LastError = err.Error()
		} else {
			cfg.Status = "enabled"
			cfg.LastError = ""
		}
		_ = d.db.SaveNotificationConfig(cfg)
	}
	return err
}

func (d *Dispatcher) sendNtfy(ctx context.Context, configJSON string, alert AlertPayload) error {
	var cfg NtfyConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return fmt.Errorf("invalid ntfy config: %w", err)
	}

	serverURL := strings.TrimRight(cfg.ServerURL, "/")
	if serverURL == "" {
		serverURL = "https://ntfy.sh"
	}
	topic := strings.TrimSpace(cfg.Topic)
	if topic == "" {
		return fmt.Errorf("ntfy topic is required")
	}

	endpoint := fmt.Sprintf("%s/%s", serverURL, topic)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(alert.Message))
	if err != nil {
		return err
	}

	req.Header.Set("Title", alert.Title)
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	priority := cfg.Priority
	if priority == "" {
		if alert.Type == "error" {
			priority = "urgent"
		} else if alert.Type == "warning" {
			priority = "high"
		} else {
			priority = "default"
		}
	}
	req.Header.Set("Priority", priority)

	tags := cfg.Tags
	if tags == "" {
		tags = "docker,package"
	}
	req.Header.Set("Tags", tags)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ntfy responded with status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (d *Dispatcher) sendDiscord(ctx context.Context, configJSON string, alert AlertPayload) error {
	var cfg DiscordConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return fmt.Errorf("invalid discord config: %w", err)
	}

	webhookURL := strings.TrimSpace(cfg.WebhookURL)
	if webhookURL == "" {
		return fmt.Errorf("discord webhook URL is required")
	}

	username := cfg.Username
	if username == "" {
		username = "DockerPulse"
	}

	// Choose embed color based on severity
	// 0x38bdf8 = Sky (default info), 0x10b981 = Emerald (update), 0xf59e0b = Amber (warning), 0xef4444 = Red (error)
	color := 0x38bdf8
	switch alert.Type {
	case "update":
		color = 0x10b981
	case "warning":
		color = 0xf59e0b
	case "error":
		color = 0xef4444
	}

	var fields []map[string]interface{}
	if alert.Host != "" {
		fields = append(fields, map[string]interface{}{
			"name":   "Host",
			"value":  alert.Host,
			"inline": true,
		})
	}

	payload := map[string]interface{}{
		"username": username,
		"embeds": []map[string]interface{}{
			{
				"title":       alert.Title,
				"description": alert.Message,
				"color":       color,
				"fields":      fields,
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
				"footer": map[string]string{
					"text": "DockerPulse Fleet Manager",
				},
			},
		},
	}

	if cfg.AvatarURL != "" {
		payload["avatar_url"] = cfg.AvatarURL
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord responded with status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func (d *Dispatcher) sendSignal(ctx context.Context, configJSON string, alert AlertPayload) error {
	var cfg SignalConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return fmt.Errorf("invalid signal config: %w", err)
	}

	endpointURL := strings.TrimSpace(cfg.EndpointURL)
	if endpointURL == "" {
		return fmt.Errorf("signal endpoint URL is required")
	}

	var recipientList []string
	for _, r := range strings.Split(cfg.Recipients, ",") {
		trimmed := strings.TrimSpace(r)
		if trimmed != "" {
			recipientList = append(recipientList, trimmed)
		}
	}
	if len(recipientList) == 0 {
		return fmt.Errorf("at least one signal recipient is required")
	}

	msg := fmt.Sprintf("[%s] %s\n\n%s", alert.Type, alert.Title, alert.Message)
	if alert.Host != "" {
		msg += fmt.Sprintf("\nHost: %s", alert.Host)
	}

	payload := map[string]interface{}{
		"message":    msg,
		"recipients": recipientList,
	}
	if cfg.Number != "" {
		payload["number"] = cfg.Number
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpointURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("signal responded with status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
