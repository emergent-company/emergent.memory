package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
)

// streamAgent builds an Agent backed by an httptest server whose handler
// streams SSE events for POST /message:stream.
func streamAgent(t *testing.T, handler http.HandlerFunc) *Agent {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := a2a.NewClientWithHTTP(srv.URL, "emt_test", srv.Client())
	return NewAgent(client, "research-agent", "test")
}

// writeSSEEvents writes the given StreamResponses as SSE data lines.
func writeSSEEvents(w http.ResponseWriter, events []a2a.StreamResponse) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		_, _ = fmt.Fprintf(w, "data: %s\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
}

// runLines feeds the given input lines to the agent loop and returns the raw
// stdout bytes after stdin closes.
func runLines(t *testing.T, agent *Agent, input string) []byte {
	t.Helper()
	var out, errBuf bytes.Buffer
	if err := Run(context.Background(), strings.NewReader(input), &out, &errBuf, agent); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return out.Bytes()
}

// runLinesErr is like runLines but also returns the stderr diagnostics.
func runLinesErr(t *testing.T, agent *Agent, input string) (stdout, stderr []byte) {
	t.Helper()
	var out, errBuf bytes.Buffer
	if err := Run(context.Background(), strings.NewReader(input), &out, &errBuf, agent); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return out.Bytes(), errBuf.Bytes()
}

// decodeLines decodes a newline-delimited stream of JSON objects.
func decodeLines(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var msgs []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("failed to decode line %q: %v", line, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// chunkText extracts the text of a session/update agent_message_chunk message.
func chunkText(t *testing.T, m map[string]any) string {
	t.Helper()
	params, ok := m["params"].(map[string]any)
	if !ok {
		t.Fatalf("message has no params: %v", m)
	}
	update := params["update"].(map[string]any)
	content := update["content"].(map[string]any)
	text, _ := content["text"].(string)
	return text
}

func TestInitialize(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request: %s %s", r.Method, r.URL.Path)
	})

	out := runLines(t, agent, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	m := msgs[0]
	if m["id"] != float64(1) {
		t.Errorf("id = %v, want 1", m["id"])
	}
	result := m["result"].(map[string]any)
	if result["protocolVersion"] != float64(ProtocolVersion) {
		t.Errorf("protocolVersion = %v, want %d", result["protocolVersion"], ProtocolVersion)
	}
	caps := result["agentCapabilities"].(map[string]any)
	if caps["loadSession"] != false {
		t.Errorf("loadSession = %v, want false", caps["loadSession"])
	}
	sessCaps, ok := caps["sessionCapabilities"].(map[string]any)
	if !ok {
		t.Fatalf("agentCapabilities.sessionCapabilities = %v, want object", caps["sessionCapabilities"])
	}
	if _, ok := sessCaps["delete"]; !ok {
		t.Errorf("sessionCapabilities = %v, want delete advertised", sessCaps)
	}
	info := result["agentInfo"].(map[string]any)
	if info["name"] != "memory" {
		t.Errorf("agentInfo.name = %v, want memory", info["name"])
	}
}

func TestSessionPromptStreams(t *testing.T) {
	bodyCh := make(chan a2a.SendMessageRequest, 1)
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message:stream" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var body a2a.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodyCh <- body
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Artifact: a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("Hel")}}}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Artifact: a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("Hello")}}}},
			{Message: &a2a.Message{MessageID: "m1", Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.TextPart("Hello")}}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Artifact: a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("Hello")}}, LastChunk: true}},
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"hello"}]}}`,
	}, "\n") + "\n"

	msgs := decodeLines(t, runLines(t, agent, input))

	body := <-bodyCh
	if body.Message.TaskID != "" {
		t.Errorf("expected no taskId on new turn, got %q", body.Message.TaskID)
	}
	if got := body.Message.Metadata[a2a.SkillIDMetadataKey]; got != "research-agent" {
		t.Errorf("skillId = %v, want research-agent", got)
	}
	if len(body.Message.Parts) != 1 || body.Message.Parts[0].Text == nil || *body.Message.Parts[0].Text != "hello" {
		t.Errorf("parts = %+v, want single text part 'hello'", body.Message.Parts)
	}

	// init, session/new, chunk("Hel"), chunk("lo"), prompt(end_turn).
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %v", len(msgs), msgs)
	}
	if got := chunkText(t, msgs[2]); got != "Hel" {
		t.Errorf("chunk 1 = %q, want Hel", got)
	}
	if got := chunkText(t, msgs[3]); got != "lo" {
		t.Errorf("chunk 2 = %q, want lo", got)
	}
	if pr := msgs[4]["result"].(map[string]any); pr["stopReason"] != "end_turn" {
		t.Errorf("stopReason = %v, want end_turn", pr["stopReason"])
	}
}

func TestHITLResume(t *testing.T) {
	bodyCh := make(chan a2a.SendMessageRequest, 2)
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		var body a2a.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodyCh <- body

		if body.Message.TaskID == "" {
			// New turn pauses for input.
			writeSSEEvents(w, []a2a.StreamResponse{
				{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
				{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
				{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: &a2a.Message{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.TextPart("What's your name?")}}}}},
			})
			return
		}
		// Resume completes.
		writeSSEEvents(w, []a2a.StreamResponse{
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Artifact: a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("Nice to meet you")}}}},
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})

	// Phase 1: new turn → HITL pause.
	input1 := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"hi"}]}}`,
	}, "\n") + "\n"

	out1 := decodeLines(t, runLines(t, agent, input1))
	if len(out1) != 4 {
		t.Fatalf("phase1: expected 4 messages (init, session/new, question chunk, prompt), got %d: %v", len(out1), out1)
	}
	if got := chunkText(t, out1[2]); got != "What's your name?" {
		t.Errorf("question chunk = %q", got)
	}
	if pr := out1[3]["result"].(map[string]any); pr["stopReason"] != "end_turn" {
		t.Errorf("phase1 stopReason = %v", pr["stopReason"])
	}

	body1 := <-bodyCh
	if body1.Message.TaskID != "" {
		t.Errorf("phase1 taskId = %q, want empty", body1.Message.TaskID)
	}

	// Phase 2: same session resumes the paused task.
	input2 := `{"jsonrpc":"2.0","id":4,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"Alice"}]}}` + "\n"
	out2 := decodeLines(t, runLines(t, agent, input2))
	if len(out2) != 2 {
		t.Fatalf("phase2: expected 2 messages (answer chunk, prompt), got %d: %v", len(out2), out2)
	}
	if got := chunkText(t, out2[0]); got != "Nice to meet you" {
		t.Errorf("answer chunk = %q", got)
	}

	body2 := <-bodyCh
	if body2.Message.TaskID != "task-1" {
		t.Errorf("phase2 taskId = %q, want task-1", body2.Message.TaskID)
	}
	if _, hasSkill := body2.Message.Metadata[a2a.SkillIDMetadataKey]; hasSkill {
		t.Errorf("phase2 should not send skillId on resume")
	}
}

func TestPromptEmptyNoBackendCall(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request for empty prompt")
	})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"S","prompt":[]}}`,
	}, "\n") + "\n"

	msgs := decodeLines(t, runLines(t, agent, input))
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if pr := msgs[2]["result"].(map[string]any); pr["stopReason"] != "end_turn" {
		t.Errorf("stopReason = %v, want end_turn", pr["stopReason"])
	}
}

func TestUnknownMethodReturnsError(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})
	out := runLines(t, agent, `{"jsonrpc":"2.0","id":9,"method":"nope","params":{}}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	assertErrorCode(t, msgs[0], -32601)
}

// assertErrorCode asserts the message carries a JSON-RPC error with the given code.
func assertErrorCode(t *testing.T, m map[string]any, code int) {
	t.Helper()
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", m)
	}
	if errObj["code"] != float64(code) {
		t.Errorf("error code = %v, want %d", errObj["code"], code)
	}
}

// assertErrorFrame asserts a message is a well-formed JSON-RPC error frame with
// the given code and id (nil expects a null/absent id).
func assertErrorFrame(t *testing.T, m map[string]any, code int, wantID any) {
	t.Helper()
	if m["jsonrpc"] != jsonrpcVersion {
		t.Errorf("jsonrpc = %v, want %q", m["jsonrpc"], jsonrpcVersion)
	}
	assertErrorCode(t, m, code)
	if m["id"] != wantID {
		t.Errorf("id = %v, want %v", m["id"], wantID)
	}
}

// TestParseErrorReturnsCode covers a line that is not valid JSON: the client
// gets a -32700 Parse error frame with a null id, and the failure is still
// logged to stderr for the operator.
func TestParseErrorReturnsCode(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})
	// Truncated JSON: nothing is parseable, so no id is recoverable.
	out, errOut := runLinesErr(t, agent, `{"jsonrpc":"2.0","id":7,"method":`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %v", len(msgs), msgs)
	}
	assertErrorFrame(t, msgs[0], -32700, nil)
	if len(bytes.TrimSpace(errOut)) == 0 {
		t.Error("expected the malformed line to be logged to stderr")
	}
}

// TestInvalidRequestErrors covers structurally valid JSON that is not a valid
// JSON-RPC Request: the client gets a -32600 Invalid Request frame, echoing the
// request id when one was recoverable.
func TestInvalidRequestErrors(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		wantID any
	}{
		{"missing method", `{"jsonrpc":"2.0","id":8}`, float64(8)},
		{"empty object", `{}`, nil},
		{"unsupported version", `{"jsonrpc":"1.0","id":9,"method":"initialize"}`, float64(9)},
		{"non-string method", `{"jsonrpc":"2.0","id":10,"method":123}`, float64(10)},
		{"non-object literal", `"not-a-request"`, nil},
		{"array", `[1,2,3]`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected HTTP request")
			})
			out, _ := runLinesErr(t, agent, tc.line+"\n")
			msgs := decodeLines(t, out)
			if len(msgs) != 1 {
				t.Fatalf("expected 1 message, got %d: %v", len(msgs), msgs)
			}
			assertErrorFrame(t, msgs[0], -32600, tc.wantID)
		})
	}
}

// TestMalformedLineDoesNotStopLoop verifies a bad line yields an error frame and
// the loop keeps serving later, well-formed requests.
func TestMalformedLineDoesNotStopLoop(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})
	input := strings.Join([]string{
		`{not json`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
	}, "\n") + "\n"
	out, _ := runLinesErr(t, agent, input)
	msgs := decodeLines(t, out)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	assertErrorFrame(t, msgs[0], -32700, nil)
	if msgs[1]["result"] == nil {
		t.Errorf("expected initialize result after malformed line, got %v", msgs[1])
	}
}

func TestInvalidParamsReturnsCode(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})
	out := runLines(t, agent, `{"jsonrpc":"2.0","id":10,"method":"session/prompt","params":"not-an-object"}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	assertErrorCode(t, msgs[0], -32602)
}

func TestMissingSessionIDReturnsCode(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})
	out := runLines(t, agent, `{"jsonrpc":"2.0","id":11,"method":"session/prompt","params":{"prompt":[{"type":"text","text":"hi"}]}}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	assertErrorCode(t, msgs[0], -32602)
}

// TestLargePromptAccepted verifies a prompt line larger than the default
// 64 KiB scanner token is accepted rather than triggering an error loop.
func TestLargePromptAccepted(t *testing.T) {
	bodyCh := make(chan a2a.SendMessageRequest, 1)
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		var body a2a.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodyCh <- body
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})

	bigText := strings.Repeat("x", 200*1024) // 200 KiB > 64 KiB default
	params, _ := json.Marshal(map[string]any{
		"sessionId": "S",
		"prompt":    []map[string]any{{"type": "text", "text": bigText}},
	})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":%s}`, params),
	}, "\n") + "\n"

	msgs := decodeLines(t, runLines(t, agent, input))
	body := <-bodyCh
	if body.Message.Parts[0].Text == nil || *body.Message.Parts[0].Text != bigText {
		t.Error("large prompt text not preserved")
	}
	// init, session/new, prompt response (no chunks: only a Task event).
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

// errReader fails every read, simulating a terminal stdin read error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestRunStopsOnTerminalReadError(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})
	var out, errBuf bytes.Buffer
	if err := Run(context.Background(), errReader{}, &out, &errBuf, agent); err == nil {
		t.Fatal("expected Run to return an error on a terminal read error")
	}
}

// TestCancelBeforePromptReturnsCancelled verifies a session/cancel arriving
// before the turn registers its cancel function still takes effect (the
// cancel-before-register window), returning a cancelled stop reason without
// making a backend call.
func TestCancelBeforePromptReturnsCancelled(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})
	sessID := agent.newSession().(SessionNewResponse).SessionID
	agent.cancel(CancelParams{SessionID: sessID})

	result, err := agent.prompt(context.Background(), PromptParams{
		SessionID: sessID,
		Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
	}, func(any) error { return nil })
	if err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}
	if got := result.(PromptResponse).StopReason; got != StopReasonCancelled {
		t.Errorf("stopReason = %q, want cancelled", got)
	}
}

// TestCancelConcurrentWithPrompt races session/cancel against in-flight prompts
// to exercise the shared session.cancel field under the race detector (guard
// against regressions of the read-outside-lock bug).
func TestCancelConcurrentWithPrompt(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})
	sessID := agent.newSession().(SessionNewResponse).SessionID

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = agent.prompt(context.Background(), PromptParams{
				SessionID: sessID,
				Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
			}, func(any) error { return nil })
		}()
		go func() {
			defer wg.Done()
			agent.cancel(CancelParams{SessionID: sessID})
		}()
	}
	wg.Wait()
}

// sessionCount returns the number of tracked sessions under the agent lock.
func sessionCount(a *Agent) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sessions)
}

// hasSession reports whether the agent tracks the given session id.
func hasSession(a *Agent, id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.sessions[id]
	return ok
}

// noopAgent builds an agent whose backend must never be called.
func noopAgent(t *testing.T) *Agent {
	t.Helper()
	return streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request: %s %s", r.Method, r.URL.Path)
	})
}

func TestSessionDeleteRemovesSession(t *testing.T) {
	agent := noopAgent(t)
	id := agent.newSession().(SessionNewResponse).SessionID
	if !hasSession(agent, id) {
		t.Fatalf("session %q not created", id)
	}

	agent.deleteSession(id)
	if hasSession(agent, id) {
		t.Fatalf("session %q not removed by deleteSession", id)
	}
	if got := sessionCount(agent); got != 0 {
		t.Fatalf("session count = %d, want 0", got)
	}
	// Deleting an unknown session succeeds silently.
	agent.deleteSession("does-not-exist")
}

// TestSessionDeleteViaRun exercises the session/delete request through the
// dispatch table, including the empty-result response.
func TestSessionDeleteViaRun(t *testing.T) {
	agent := noopAgent(t)
	id := agent.newSession().(SessionNewResponse).SessionID

	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"session/delete","params":{"sessionId":%q}}`, id) + "\n"
	msgs := decodeLines(t, runLines(t, agent, input))
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %v", len(msgs), msgs)
	}
	if _, hasErr := msgs[0]["error"]; hasErr {
		t.Fatalf("unexpected error response: %v", msgs[0])
	}
	res, ok := msgs[0]["result"].(map[string]any)
	if !ok || len(res) != 0 {
		t.Fatalf("result = %v, want empty object", msgs[0]["result"])
	}
	if hasSession(agent, id) {
		t.Fatalf("session %q not removed via session/delete", id)
	}
}

// TestSessionCapEvictsLeastRecentlyUsed verifies the map is bounded: inserting
// past the cap evicts the least-recently-used session.
func TestSessionCapEvictsLeastRecentlyUsed(t *testing.T) {
	agent := noopAgent(t)
	agent.maxSessions = 3

	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, agent.newSession().(SessionNewResponse).SessionID)
	}
	// Touch the middle session so ids[1] becomes the most recently used.
	agent.mu.Lock()
	agent.seq++
	agent.sessions[ids[1]].lastUsed = agent.seq
	agent.mu.Unlock()

	newest := agent.newSession().(SessionNewResponse).SessionID

	if got := sessionCount(agent); got != 3 {
		t.Fatalf("session count = %d, want 3", got)
	}
	if hasSession(agent, ids[0]) {
		t.Errorf("least-recently-used session %q should have been evicted", ids[0])
	}
	for _, id := range []string{ids[1], ids[2], newest} {
		if !hasSession(agent, id) {
			t.Errorf("session %q should have been retained", id)
		}
	}
}

// TestEvictionInvokesCancelFunc verifies a dropped session's CancelFunc is
// invoked so an in-flight turn cannot leak.
func TestEvictionInvokesCancelFunc(t *testing.T) {
	agent := noopAgent(t)
	agent.maxSessions = 1

	called := make(chan struct{}, 1)
	agent.mu.Lock()
	agent.sessions["stale"] = &session{
		cancel:   func() { called <- struct{}{} },
		lastUsed: 0,
	}
	agent.mu.Unlock()

	id := agent.newSession().(SessionNewResponse).SessionID

	if hasSession(agent, "stale") {
		t.Fatal("stale session should have been evicted")
	}
	if !hasSession(agent, id) {
		t.Fatal("new session missing after eviction")
	}
	select {
	case <-called:
	default:
		t.Fatal("cancel func on the evicted session was not invoked")
	}
}

// TestEvictionSkipsInFlightSession verifies a session with a running turn is
// never pruned; the map may exceed the cap temporarily instead.
func TestEvictionSkipsInFlightSession(t *testing.T) {
	agent := noopAgent(t)
	agent.maxSessions = 1

	cancelCalled := false
	agent.mu.Lock()
	agent.sessions["busy"] = &session{
		cancel:   func() { cancelCalled = true },
		inFlight: 1,
		lastUsed: 0,
	}
	agent.mu.Unlock()

	id := agent.newSession().(SessionNewResponse).SessionID

	if !hasSession(agent, "busy") {
		t.Fatal("in-flight session must not be evicted")
	}
	if !hasSession(agent, id) {
		t.Fatal("new session missing")
	}
	if cancelCalled {
		t.Fatal("cancel func of an in-flight session should not be invoked")
	}
	if got := sessionCount(agent); got != 2 {
		t.Fatalf("session count = %d, want 2 (temporary over-cap)", got)
	}
}

// TestDeleteSessionCancelsInFlightTurn verifies deleteSession invokes the stored
// CancelFunc when it removes a session that still has a running turn.
func TestDeleteSessionCancelsInFlightTurn(t *testing.T) {
	agent := noopAgent(t)
	ctx, cancel := context.WithCancel(context.Background())
	agent.mu.Lock()
	agent.sessions["S"] = &session{cancel: cancel, inFlight: 1}
	agent.mu.Unlock()

	agent.deleteSession("S")

	select {
	case <-ctx.Done():
	default:
		t.Fatal("deleteSession did not cancel the in-flight turn")
	}
	if hasSession(agent, "S") {
		t.Fatal("session not removed by deleteSession")
	}
}

// TestCompletedTurnClearsCancel verifies an idle session holds no stale
// CancelFunc once its turn has finished.
func TestCompletedTurnClearsCancel(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})
	sessID := agent.newSession().(SessionNewResponse).SessionID

	if _, err := agent.prompt(context.Background(), PromptParams{
		SessionID: sessID,
		Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
	}, func(any) error { return nil }); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}

	agent.mu.Lock()
	sess := agent.sessions[sessID]
	cancelFn := sess.cancel
	inFlight := sess.inFlight
	agent.mu.Unlock()

	if cancelFn != nil {
		t.Error("cancel func should be cleared once the turn completes")
	}
	if inFlight != 0 {
		t.Errorf("inFlight = %d, want 0", inFlight)
	}
}

func TestFailedTaskReturnsRefusal(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateFailed, Message: &a2a.Message{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.TextPart("boom")}}}}},
		})
	})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"hi"}]}}`,
	}, "\n") + "\n"

	msgs := decodeLines(t, runLines(t, agent, input))
	// init, session/new, failure chunk, prompt response (refusal).
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %v", len(msgs), msgs)
	}
	if got := chunkText(t, msgs[2]); got != "boom" {
		t.Errorf("failure chunk = %q, want boom", got)
	}
	if pr := msgs[3]["result"].(map[string]any); pr["stopReason"] != "refusal" {
		t.Errorf("stopReason = %v, want refusal", pr["stopReason"])
	}
}

// TestSessionDeleteNotificationViaRun verifies the fire-and-forget notification
// form of session/delete removes the session and produces no response.
func TestSessionDeleteNotificationViaRun(t *testing.T) {
	agent := noopAgent(t)
	id := agent.newSession().(SessionNewResponse).SessionID

	input := fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/delete","params":{"sessionId":%q}}`, id) + "\n"
	out := runLines(t, agent, input)
	if len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("notification produced output: %q", out)
	}
	if hasSession(agent, id) {
		t.Fatalf("session %q not removed via session/delete notification", id)
	}
}

// TestDeleteSessionMarksSessionDropped verifies deleteSession marks the removed
// session dropped (not just cancels a registered turn) so a prompt that looked
// the session up before deletion but registers its cancel function afterwards
// aborts instead of starting a backend turn on a detached session.
func TestDeleteSessionMarksSessionDropped(t *testing.T) {
	agent := noopAgent(t)
	id := agent.newSession().(SessionNewResponse).SessionID

	agent.mu.Lock()
	sess := agent.sessions[id]
	agent.mu.Unlock()

	agent.deleteSession(id)

	agent.mu.Lock()
	dropped := sess.dropped
	agent.mu.Unlock()
	if !dropped {
		t.Fatal("deleteSession must mark the removed session dropped")
	}
}

// TestEvictionMarksSessionDropped verifies LRU eviction marks the victim
// dropped, closing the window where a prompt that looked the session up before
// eviction registers its cancel function afterwards.
func TestEvictionMarksSessionDropped(t *testing.T) {
	agent := noopAgent(t)
	agent.maxSessions = 1

	agent.mu.Lock()
	victim := &session{}
	agent.sessions["stale"] = victim
	agent.mu.Unlock()

	_ = agent.newSession()

	if hasSession(agent, "stale") {
		t.Fatal("stale session should have been evicted")
	}
	agent.mu.Lock()
	dropped := victim.dropped
	agent.mu.Unlock()
	if !dropped {
		t.Fatal("evictLocked must mark the evicted session dropped")
	}
}

// TestDroppedSessionLateTurnAborts deterministically exercises the window
// between a prompt's session lookup and its turn registration: two prompts look
// the session up, it is then deleted, and both must abort (cancelled) without
// ever touching the backend.
func TestDroppedSessionLateTurnAborts(t *testing.T) {
	agent := noopAgent(t)
	id := agent.newSession().(SessionNewResponse).SessionID

	released := make(chan struct{})
	lookedUp := make(chan struct{}, 2)
	agent.mu.Lock()
	agent.testHookAfterLookup = func() {
		lookedUp <- struct{}{}
		<-released
	}
	agent.mu.Unlock()

	var wg sync.WaitGroup
	reasons := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := agent.prompt(context.Background(), PromptParams{
				SessionID: id,
				Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
			}, func(any) error { return nil })
			if err != nil {
				t.Errorf("prompt %d returned error: %v", i, err)
				return
			}
			reasons[i] = res.(PromptResponse).StopReason
		}(i)
	}

	for i := 0; i < 2; i++ {
		<-lookedUp
	}
	agent.deleteSession(id)
	close(released)
	wg.Wait()

	for i, got := range reasons {
		if got != StopReasonCancelled {
			t.Errorf("prompt %d stopReason = %q, want cancelled", i, got)
		}
	}
}

// TestEvictedSessionLateTurnAborts mirrors the delete case for LRU eviction: a
// prompt that looked the session up before eviction must abort, not run.
func TestEvictedSessionLateTurnAborts(t *testing.T) {
	agent := noopAgent(t)
	id := agent.newSession().(SessionNewResponse).SessionID

	released := make(chan struct{})
	lookedUp := make(chan struct{}, 2)
	agent.mu.Lock()
	agent.testHookAfterLookup = func() {
		lookedUp <- struct{}{}
		<-released
	}
	agent.mu.Unlock()

	var wg sync.WaitGroup
	reasons := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := agent.prompt(context.Background(), PromptParams{
				SessionID: id,
				Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
			}, func(any) error { return nil })
			if err != nil {
				t.Errorf("prompt %d returned error: %v", i, err)
				return
			}
			reasons[i] = res.(PromptResponse).StopReason
		}(i)
	}

	for i := 0; i < 2; i++ {
		<-lookedUp
	}
	// Force eviction of the idle session both prompts looked up.
	agent.mu.Lock()
	agent.maxSessions = 1
	agent.mu.Unlock()
	_ = agent.newSession()
	close(released)
	wg.Wait()

	for i, got := range reasons {
		if got != StopReasonCancelled {
			t.Errorf("prompt %d stopReason = %q, want cancelled", i, got)
		}
	}
}
