package mcp

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestHTTPToolsCallAdminToolNotGated proves the HTTP transports mark their
// ExecuteTool call as trusted, so the in-process trust gate (which covers the
// agent-run path) does NOT re-fire for an authenticated HTTP client whose token
// already passed the transport's RequiredScope check. Without this, the
// admin-scoped tools (token-*, trace-*, provider-*, project-create) would be
// wrongly refused over HTTP (issue #994 repair).
func TestHTTPToolsCallAdminToolNotGated(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions() // populate the tool index GetToolByName reads

	h := NewHandler(svc, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)

	user := &auth.AuthUser{
		ID:        uuid.New().String(),
		Scopes:    []string{"admin"}, // token carries the admin scope the transport check admits
		ProjectID: uuid.New().String(),
	}

	token := "tok-admin"
	h.sessionsMu.Lock()
	h.sessions[token] = &Session{Initialized: true, ProjectID: user.ProjectID}
	h.sessionsMu.Unlock()

	c, _ := gateContext(user, token)
	params, err := json.Marshal(ToolsCallParams{Name: "token-create", Arguments: map[string]any{}})
	require.NoError(t, err)
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}

	resp := h.handleToolsCall(c, req, user)

	// The tool dispatches and fails on its own argument validation ("name" is
	// required), NOT on the in-process trust gate. That proves the HTTP marker
	// kept the admin tool reachable.
	require.NotNil(t, resp.Error, "token-create with empty args must error on validation")
	require.NotContains(t, resp.Error.Message, "admin authority",
		"HTTP admin tool must not be refused by the in-process trust gate")
	require.NotContains(t, resp.Error.Message, "untrusted surface",
		"HTTP admin tool must not be refused by the in-process trust gate")
	require.Contains(t, resp.Error.Message, "name",
		"must reach the tool's own argument validation, got: %s", resp.Error.Message)
}
