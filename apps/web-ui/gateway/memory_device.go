package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

// deviceTokenInfo is the subset of memory's GET /api/auth/me response the
// gateway needs to recognise and bind a scoped device credential: the token
// type, its scopes (to spot the reserved device:api marker), and the
// project/org it is bound to.
type deviceTokenInfo struct {
	Type      string   `json:"type"`
	Scopes    []string `json:"scopes"`
	ProjectID string   `json:"project_id"`
	OrgID     string   `json:"org_id"`
}

// hasDeviceMarker reports whether the introspected token carries the reserved
// device:api marker scope, i.e. is a scoped device credential.
func (i *deviceTokenInfo) hasDeviceMarker() bool {
	for _, s := range i.Scopes {
		if s == "device:api" {
			return true
		}
	}
	return false
}

// IntrospectDeviceToken resolves a raw bearer token against memory's
// GET /api/auth/me without a session context. It is the gateway's lightweight
// marker-introspection path: the token is presented as the bearer and memory
// echoes its resolved identity. Only the caller who holds the token can learn
// anything about it. A non-2xx (unknown/revoked/expired/store-down) is an error
// so the caller fails closed.
func (m *MemoryClient) IntrospectDeviceToken(ctx context.Context, token string) (*deviceTokenInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+"/api/auth/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxMemoryResponseBytes+1))
		return nil, parseMemoryError(resp.StatusCode, raw)
	}
	var out deviceTokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDeviceToken mints a scoped per-device credential for the request's
// active project via memory's POST /api/projects/{projectId}/device-tokens.
// The scope set is hardcoded server-side; only the device name is sent. The
// request rides the session's token, so it is only called on the
// session-authenticated devices page (setup-token mint time).
func (m *MemoryClient) CreateDeviceToken(ctx context.Context, name string) (*APITokenCreateResponse, error) {
	var out APITokenCreateResponse
	body := CreateAPITokenRequest{Name: name}
	if err := m.doH(ctx, http.MethodPost, "/api/projects/"+url.PathEscape(m.projectIDFor(ctx))+"/device-tokens", body, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
