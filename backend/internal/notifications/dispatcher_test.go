package notifications

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dockpulse/dockmgr/internal/database"
)

func TestDispatcherSendNtfy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/test-topic" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Title") != "Test Alert" {
			t.Errorf("unexpected Title header: %s", r.Header.Get("Title"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := NewDispatcher(nil)
	cfgJSON := `{"server_url":"` + ts.URL + `","topic":"test-topic"}`
	alert := AlertPayload{
		Title:   "Test Alert",
		Message: "Hello from test",
		Type:    "info",
	}

	err := d.sendNtfy(context.Background(), cfgJSON, alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatcherSendDiscord(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := NewDispatcher(nil)
	cfgJSON := `{"webhook_url":"` + ts.URL + `"}`
	alert := AlertPayload{
		Title:   "Discord Alert",
		Message: "DockerPulse test message",
		Type:    "update",
	}

	err := d.sendDiscord(context.Background(), cfgJSON, alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatcherSendSignal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	d := NewDispatcher(nil)
	cfgJSON := `{"endpoint_url":"` + ts.URL + `","recipients":"+1234567890"}`
	alert := AlertPayload{
		Title:   "Signal Alert",
		Message: "Testing signal notification",
		Type:    "warning",
	}

	err := d.sendSignal(context.Background(), cfgJSON, alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatcherIntegrationWithDB(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := database.InitDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer db.Close()

	d := NewDispatcher(db)

	// Test unconfigured service should fail validation
	err = d.TestService(context.Background(), "ntfy", `{}`)
	if err == nil {
		t.Errorf("expected error for empty ntfy config")
	}

	cfg, _ := db.GetNotificationConfig("ntfy")
	if cfg.Status != "error" {
		t.Errorf("expected status 'error', got %s", cfg.Status)
	}
}
