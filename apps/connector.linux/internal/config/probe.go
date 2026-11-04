package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrAuthFailed reports that the server rejected the token (HTTP 401/403).
var ErrAuthFailed = errors.New("authentication failed")

// HubSession mirrors the hub's session summary (mcprelay SessionInfo) for one
// connected relay instance.
type HubSession struct {
	InstanceID  string    `json:"instance_id"`
	Version     string    `json:"version,omitempty"`
	ToolCount   int       `json:"tool_count"`
	ConnectedAt time.Time `json:"connected_at"`
}

// sessionsResponse is the ListSessions response body shape.
type sessionsResponse struct {
	Sessions []HubSession `json:"sessions"`
}

// sessionsEndpoint returns the sessions endpoint for a server base URL.
func sessionsEndpoint(serverURL string) string {
	return strings.TrimRight(serverURL, "/") + "/api/mcp-relay/sessions"
}

// sessionsRequest issues GET /api/mcp-relay/sessions with a Bearer token
// (project context comes from the token). 2xx returns the open response;
// 401/403 return ErrAuthFailed; other statuses and transport errors return
// wrapped errors with the body already closed.
func sessionsRequest(ctx context.Context, client *http.Client, serverURL, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sessionsEndpoint(serverURL), nil)
	if err != nil {
		return nil, fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("server unreachable: %w", err)
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		_ = resp.Body.Close()
		return nil, ErrAuthFailed
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("server unreachable: unexpected status %s", resp.Status)
	}
}

// ProbeConnectivity verifies that serverURL and token are usable by calling
// GET /api/mcp-relay/sessions with a Bearer token. 2xx returns nil; 401/403
// return ErrAuthFailed; other statuses and transport errors return wrapped
// errors.
func ProbeConnectivity(ctx context.Context, client *http.Client, serverURL, token string) error {
	resp, err := sessionsRequest(ctx, client, serverURL, token)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain a bounded amount so the connection can be reused; body is unused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

// ListSessions returns the hub's connected relay sessions for the token's
// project. Errors follow ProbeConnectivity's classification (ErrAuthFailed for
// 401/403, wrapped transport/status errors otherwise).
func ListSessions(ctx context.Context, client *http.Client, serverURL, token string) ([]HubSession, error) {
	resp, err := sessionsRequest(ctx, client, serverURL, token)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var out sessionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("parse sessions response: %w", err)
	}
	if out.Sessions == nil {
		out.Sessions = []HubSession{}
	}
	return out.Sessions, nil
}
