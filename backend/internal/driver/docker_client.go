package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DockerClient struct {
	httpClient  *http.Client
	baseURL     string
	hostAddress string
}

func NewDockerClient(hostAddress string) (*DockerClient, error) {
	if hostAddress == "" || hostAddress == "local" || hostAddress == "/var/run/docker.sock" {
		hostAddress = defaultDockerSocket
	}

	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}

	if strings.HasPrefix(hostAddress, "tcp://") || strings.HasPrefix(hostAddress, "http://") || strings.HasPrefix(hostAddress, "https://") {
		clean := strings.TrimPrefix(hostAddress, "tcp://")
		if !strings.HasPrefix(clean, "http") {
			clean = "http://" + clean
		}
		u, err := url.Parse(clean)
		if err != nil {
			return nil, err
		}
		return &DockerClient{
			httpClient:  &http.Client{Transport: tr, Timeout: 30 * time.Second},
			baseURL:     u.String(),
			hostAddress: hostAddress,
		}, nil
	}

	// Socket / Named Pipe Transport
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialDockerSocket(ctx, hostAddress)
	}

	return &DockerClient{
		httpClient:  &http.Client{Transport: tr},
		baseURL:     "http://docker",
		hostAddress: hostAddress,
	}, nil
}

func (c *DockerClient) Dial(ctx context.Context) (net.Conn, error) {
	if strings.HasPrefix(c.hostAddress, "tcp://") || strings.HasPrefix(c.hostAddress, "http://") || strings.HasPrefix(c.hostAddress, "https://") {
		clean := strings.TrimPrefix(c.hostAddress, "tcp://")
		if !strings.HasPrefix(clean, "http") {
			clean = "http://" + clean
		}
		u, err := url.Parse(clean)
		if err != nil {
			return nil, err
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", u.Host)
	}

	return dialDockerSocket(ctx, c.hostAddress)
}

func (c *DockerClient) Get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API error (status %d): %s", resp.StatusCode, string(b))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *DockerClient) Post(ctx context.Context, path string, body interface{}, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API error (status %d): %s", resp.StatusCode, string(b))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *DockerClient) Delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API error (status %d): %s", resp.StatusCode, string(b))
	}
	return nil
}

func (c *DockerClient) Stream(ctx context.Context, path string, writer io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker API stream error (status %d): %s", resp.StatusCode, string(b))
	}

	reader := bufio.NewReader(resp.Body)
	header := make([]byte, 8)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			_, err := io.ReadFull(reader, header)
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				// If not standard multiplexed header, stream raw
				_, copyErr := io.Copy(writer, reader)
				return copyErr
			}
			size := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
			if size <= 0 || size > 10*1024*1024 {
				// Raw fallback
				_, _ = writer.Write(header)
				_, copyErr := io.Copy(writer, reader)
				return copyErr
			}
			buf := make([]byte, size)
			_, err = io.ReadFull(reader, buf)
			if err != nil {
				return err
			}
			_, _ = writer.Write(buf)
		}
	}
}
