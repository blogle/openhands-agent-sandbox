// Package initclient implements the Agent Server deferred-init protocol.
package initclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// InitRequest represents the POST /api/init body sent to Agent Server.
type InitRequest struct {
	SessionAPIKeys []string          `json:"session_api_keys,omitempty"`
	SecretKey      string            `json:"secret_key,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

// InitStatus represents the GET /api/init response.
type InitStatus struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// Client handles deferred-init communication with Agent Server.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new init client.
func NewClient(timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
	}
}

// url builds an HTTP URL using net.JoinHostPort for IPv6 correctness.
func (c *Client) url(podIP string, port int, path string) string {
	return fmt.Sprintf("http://%s%s", net.JoinHostPort(podIP, fmt.Sprintf("%d", port)), path)
}

// Init sends POST /api/init to the Agent Server at the given pod address.
func (c *Client) Init(ctx context.Context, podIP string, port int, bootstrapKey string, req InitRequest) error {
	url := c.url(podIP, port, "/api/init")

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal init request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create init request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Init-API-Key", bootstrapKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("init request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusBadRequest {
		// 400 might mean already initialized — check by GET /api/init
		status, statusErr := c.GetStatus(ctx, podIP, port)
		if statusErr == nil && status.State == "ready" {
			return nil
		}
		return fmt.Errorf("init failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return fmt.Errorf("init failed with status %d: %s", resp.StatusCode, string(respBody))
}

// GetStatus sends GET /api/init to check the init state.
func (c *Client) GetStatus(ctx context.Context, podIP string, port int) (*InitStatus, error) {
	url := c.url(podIP, port, "/api/init")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create status request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("status request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("status check failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var status InitStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status: %w", err)
	}

	return &status, nil
}

// WaitForReady polls GET /api/init until state is "ready" or "dormant".
func (c *Client) WaitForReady(ctx context.Context, podIP string, port int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for init ready: %w", ctx.Err())
		case <-ticker.C:
			status, err := c.GetStatus(ctx, podIP, port)
			if err != nil {
				continue // Transient error, retry
			}
			if status.State == "ready" || status.State == "dormant" {
				return nil
			}
			if status.Error != "" {
				return fmt.Errorf("init error: %s", status.Error)
			}
		}
	}
}

// WaitForHealth checks GET /health until it succeeds.
func (c *Client) WaitForHealth(ctx context.Context, podIP string, port int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := c.url(podIP, port, "/health")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for health: %w", ctx.Err())
		case <-ticker.C:
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				continue
			}
			resp, err := c.httpClient.Do(httpReq)
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}
