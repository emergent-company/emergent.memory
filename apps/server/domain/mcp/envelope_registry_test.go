package mcp

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phase1ResultTools lists the MCP tools whose successful results must be a
// single JSON text object with a top-level boolean "ok" (issue #319). New
// structured-result tools must be produced through envelopeResult and should
// be added here.
var phase1ResultTools = []string{
	"entity-create",
	"entity-update",
	"entity-delete",
	"relationship-create",
	"relationship-delete",
	"search-hybrid",
	"search-semantic",
	"entity-query",
	"remember",
	"forget",
}

func TestPhase1ToolsAreRegistered(t *testing.T) {
	svc := &Service{}
	registered := make(map[string]bool)
	for _, tool := range svc.GetToolDefinitions() {
		registered[tool.Name] = true
	}
	for _, name := range phase1ResultTools {
		assert.True(t, registered[name], "Phase-1 envelope tool %q must be registered in GetToolDefinitions", name)
	}
}

// TestPhase1EnvelopeShapes pins the data/meta nesting contract for each
// Phase-1 tool by serializing the same payload maps the execute* handlers
// pass to envelopeResult and asserting the result is JSON text with a
// top-level boolean ok and no legacy top-level fields. Tools with a
// hermetic execution path (remember/forget via a loopback REST stub,
// entity-create via the DB-backed harness in entity_create_dedup_test.go)
// are additionally exercised end-to-end elsewhere; this test guards the
// shared envelope builder for every Phase-1 tool without a live server.
func TestPhase1EnvelopeShapes(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		meta map[string]any
	}{
		{
			name: "entity-create",
			data: map[string]any{
				"results": []any{
					map[string]any{"ok": true, "index": 0, "entity": map[string]any{"id": "e1", "type": "Note"}},
				},
				"message": "Batch create completed: 1 created, 0 failed, 0 near-duplicate",
			},
			meta: map[string]any{"created": 1, "failed": 0, "total": 1, "similar": []any{}, "similar_count": 0},
		},
		{
			name: "relationship-create",
			data: map[string]any{
				"results": []any{
					map[string]any{"ok": true, "index": 0, "relationship": map[string]any{"id": "r1", "type": "links", "source_id": "a", "target_id": "b"}},
				},
				"message": "Batch create completed: 1 succeeded, 0 failed",
			},
			meta: map[string]any{"created": 1, "failed": 0, "total": 1},
		},
		{
			name: "entity-update",
			data: map[string]any{
				"entity": map[string]any{"id": "e1", "type": "Note"},
			},
		},
		{
			name: "entity-delete",
			data: map[string]any{"entity_id": "e1"},
		},
		{
			name: "relationship-delete",
			data: map[string]any{"relationship_id": "r1"},
		},
		{
			name: "search-hybrid",
			data: map[string]any{
				"data": []any{
					map[string]any{"object": map[string]any{"id": "e1", "type": "Note"}, "score": 0.9},
				},
				"total":    1,
				"has_more": false,
			},
		},
		{
			name: "search-semantic",
			data: map[string]any{
				"data": []any{
					map[string]any{"object": map[string]any{"id": "e1", "type": "Note"}, "score": 0.9},
				},
				"total":    1,
				"has_more": false,
			},
		},
		{
			name: "entity-query",
			data: map[string]any{
				"projectId": "p1",
				"entities":  []any{},
				"pagination": map[string]any{
					"total": 0, "limit": 10, "offset": 0, "has_more": false,
				},
			},
		},
		{
			name: "remember",
			data: map[string]any{"run_id": "run-123", "status": "completed", "message": "Remember completed"},
		},
		{
			name: "forget",
			data: map[string]any{"run_id": "run-456", "status": "completed", "message": "Forget completed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := envelopeResult(true, tt.data, tt.meta, "")
			m := parseResultMap(t, result)
			require.NoError(t, err)

			okVal, isBool := m["ok"].(bool)
			require.True(t, isBool, "top-level ok must be a bool, got %T", m["ok"])
			assert.True(t, okVal)
			_, hasError := m["error"]
			assert.False(t, hasError, "ok=true envelope must omit error")
			_, hasData := m["data"]
			assert.True(t, hasData, "envelope must carry data")
			if len(tt.meta) == 0 {
				_, hasMeta := m["meta"]
				assert.False(t, hasMeta, "empty meta must be omitted")
			} else {
				_, hasMeta := m["meta"]
				assert.True(t, hasMeta, "non-empty meta must be present")
			}
			// No numeric-typed (or any) "success" field at any of the tested levels.
			_, hasSuccess := m["success"]
			assert.False(t, hasSuccess, "top-level success must not exist")
		})
	}
}

// rememberForgetRESTBody mirrors the chat-domain REST bodies decoded by
// executeRemember/executeForget (verified against chat/handler.go):
//
//	remember async 202: {run_id, status, document_id}
//	remember sync  200: {run_id, status, summary, document_id}
//	forget   async 202: {run_id, status}
//	forget   sync  200: {run_id, status, summary}
func TestRememberForgetReturnEnvelope(t *testing.T) {
	tests := []struct {
		name     string
		tool     string // "remember" | "forget"
		mode     string
		restBody string
		wantKeys []string // data keys that must be present and non-empty
	}{
		{
			name:     "remember sync",
			tool:     "remember",
			mode:     "sync",
			restBody: `{"run_id":"run-remember-sync","status":"completed","summary":{"created":3},"document_id":"doc-1"}`,
			wantKeys: []string{"run_id", "status", "summary", "document_id"},
		},
		{
			name:     "remember async",
			tool:     "remember",
			mode:     "async",
			restBody: `{"run_id":"run-remember-async","status":"running","document_id":"doc-1"}`,
			wantKeys: []string{"run_id", "status", "document_id"},
		},
		{
			name:     "forget sync",
			tool:     "forget",
			mode:     "sync",
			restBody: `{"run_id":"run-forget-sync","status":"completed","summary":{"deleted":2}}`,
			wantKeys: []string{"run_id", "status", "summary"},
		},
		{
			name:     "forget async",
			tool:     "forget",
			mode:     "async",
			restBody: `{"run_id":"run-forget-async","status":"running"}`,
			wantKeys: []string{"run_id", "status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tt.restBody)
			})
			defer ts.Close()

			svc := &Service{serverPort: port}
			args := map[string]any{"message": "some content", "mode": tt.mode}

			var result *ToolResult
			var err error
			ctx := context.Background()
			if tt.tool == "remember" {
				result, err = svc.executeRemember(ctx, "proj-id", args)
			} else {
				result, err = svc.executeForget(ctx, "proj-id", args)
			}
			require.NoError(t, err)

			m := parseResultMap(t, result)
			okVal, isBool := m["ok"].(bool)
			require.True(t, isBool, "tool result must carry top-level boolean ok, got %T", m["ok"])
			assert.True(t, okVal, "%s %s must succeed", tt.tool, tt.mode)
			_, hasError := m["error"]
			assert.False(t, hasError, "successful %s result must omit error", tt.tool)

			data, hasData := m["data"].(map[string]any)
			require.True(t, hasData, "%s result must carry data", tt.tool)
			for _, key := range tt.wantKeys {
				assert.NotEmpty(t, data[key], "data.%s must be present and non-empty", key)
			}
			msg, hasMsg := data["message"].(string)
			require.True(t, hasMsg, "data.message human summary must be present")
			if tt.tool == "forget" {
				assert.Contains(t, msg, "removed", "forget message copy must say what was removed")
				assert.NotContains(t, msg, "created", "forget message copy must not say what was created")
			}
			// run_id must be usable directly, not buried in prose only.
			assert.NotEmpty(t, data["run_id"])
		})
	}
}
