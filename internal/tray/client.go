package tray

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	statePath      = "/api/state"
	requestTimeout = 5 * time.Second
	maxStateBody   = 8 << 10
)

// Fetcher reads the proxy's current state. The tray polls it; tests substitute
// fakes so no D-Bus or network is needed.
type Fetcher interface {
	State(ctx context.Context) (ProxyState, error)
}

// Client polls a proxy's HTTP API for its IPC state.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

// NewClient builds a state client for the given proxy base URL. A nil
// http.Client gets a bounded default.
func NewClient(server, token string, httpClient *http.Client) (*Client, error) {
	base, err := NormalizeServer(server)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{endpoint: base + statePath, token: token, http: httpClient}, nil
}

// State fetches /api/state. Any transport, auth or decoding failure yields
// StateUnreachable together with the error, so the caller can log it and still
// show a disconnected icon.
func (c *Client) State(ctx context.Context) (ProxyState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return StateUnreachable, fmt.Errorf("building state request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return StateUnreachable, fmt.Errorf("polling %s: %w", c.endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxStateBody))
		return StateUnreachable, fmt.Errorf("polling %s: proxy answered HTTP %d", c.endpoint, resp.StatusCode)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxStateBody)).Decode(&payload); err != nil {
		return StateUnreachable, fmt.Errorf("decoding %s response: %w", c.endpoint, err)
	}
	return NormalizeState(payload.Status), nil
}
