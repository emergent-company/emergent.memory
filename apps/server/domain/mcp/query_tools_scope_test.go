package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Item B: search-knowledge must NOT fail closed under a scope. It threads the
// caller's graph type scope through the /query loopback so the internal
// graph-query-agent enforces the same boundary. These tests pin the request body
// the tool emits; removing the threading block makes them fail.

func TestExecuteQueryKnowledge_ScopedThreadsScopeIntoLoopback(t *testing.T) {
	var gotBody []byte
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, buildSseLines(`{"type":"token","token":"Answer"}`, `{"type":"done","runId":"run-1"}`))
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	ctx := scopedCtx([]string{"Law"}, []string{"CITES"})

	result, err := svc.executeQueryKnowledge(ctx, "proj-id", map[string]any{"question": "q"})
	require.NoError(t, err, "a scoped call must no longer fail closed")
	require.NotNil(t, result)

	var body map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &body))
	assert.Equal(t, []any{"Law"}, body["object_types"], "object allowlist must be threaded through the loopback")
	assert.Equal(t, []any{"CITES"}, body["relationship_types"], "relationship allowlist must be threaded through the loopback")
}

func TestExecuteQueryKnowledge_ObjectOnlyScopeThreadsObjectAxis(t *testing.T) {
	var gotBody []byte
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, buildSseLines(`{"type":"done","runId":"run-1"}`))
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	ctx := scopedCtx([]string{"Law"}, nil)

	_, err := svc.executeQueryKnowledge(ctx, "proj-id", map[string]any{"question": "q"})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &body))
	assert.Equal(t, []any{"Law"}, body["object_types"])
	_, hasRel := body["relationship_types"]
	assert.False(t, hasRel, "an unrestricted relationship axis must not emit a relationship_types field")
}

func TestExecuteQueryKnowledge_UnscopedOmitsScopeFields(t *testing.T) {
	var gotBody []byte
	ts, port := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, buildSseLines(`{"type":"done","runId":"run-1"}`))
	})
	defer ts.Close()

	svc := &Service{serverPort: port}
	_, err := svc.executeQueryKnowledge(context.Background(), "proj-id", map[string]any{"question": "q"})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &body))
	_, hasObj := body["object_types"]
	_, hasRel := body["relationship_types"]
	assert.False(t, hasObj, "unscoped call must not emit object_types")
	assert.False(t, hasRel, "unscoped call must not emit relationship_types")
}
