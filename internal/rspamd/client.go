// Package rspamd provides a client for the rspamd spam-filtering HTTP API.
package rspamd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client communicates with a rspamd instance over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient returns a Client for the given address.
// Returns nil if addr is empty, so callers can guard with a nil check.
//
// Two address forms are accepted:
//
//	http://host:port          — plain TCP
//	unix:///path/to/sock      — Unix domain socket
func NewClient(addr string) *Client {
	if addr == "" {
		return nil
	}

	var (
		baseURL    string
		httpClient *http.Client
	)

	if after, ok := strings.CutPrefix(addr, "unix://"); ok {
		socketPath := after
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		}
		baseURL = "http://localhost"
		httpClient = &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		}
	} else {
		baseURL = strings.TrimRight(addr, "/")
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// CheckResult holds the fields returned by rspamd's /checkv2 endpoint.
type CheckResult struct {
	Score         float64           `json:"score"`
	RequiredScore float64           `json:"required_score"`
	Action        string            `json:"action"`
	Symbols       map[string]Symbol `json:"symbols"`
}

// Symbol is a single rspamd rule that fired.
type Symbol struct {
	Score       float64 `json:"score"`
	Description string  `json:"description"`
}

// Check submits raw email bytes to rspamd for analysis.
func (c *Client) Check(ctx context.Context, raw []byte) (*CheckResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/checkv2", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("rspamd check: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rspamd check: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("rspamd check: read response: %w", err)
	}

	var result CheckResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("rspamd check: parse response: %w", err)
	}

	return &result, nil
}

// LearnSpam teaches rspamd that the message is spam.
func (c *Client) LearnSpam(ctx context.Context, raw []byte) error {
	return c.learn(ctx, "learnspam", raw)
}

// LearnHam teaches rspamd that the message is not spam.
func (c *Client) LearnHam(ctx context.Context, raw []byte) error {
	return c.learn(ctx, "learnham", raw)
}

func (c *Client) learn(ctx context.Context, endpoint string, raw []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("rspamd %s: build request: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("rspamd %s: %w", endpoint, err)
	}
	resp.Body.Close()
	return nil
}
