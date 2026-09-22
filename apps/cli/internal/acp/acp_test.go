package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/idgen"
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

// writeSSEEvents writes the given StreamResponses as SSE data lines. Each
// event is terminated by a blank line, as required by the SSE spec: the SDK
// client only dispatches an event once it sees the terminating blank line.
func writeSSEEvents(w http.ResponseWriter, events []a2a.StreamResponse) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
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
	if _, ok := sessCaps["close"]; !ok {
		t.Errorf("sessionCapabilities = %v, want close advertised", sessCaps)
	}
	if _, ok := sessCaps["list"]; ok {
		t.Errorf("sessionCapabilities = %v, want list deliberately NOT advertised", sessCaps)
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
	sessID := mustNewSession(t, agent)
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
	sessID := mustNewSession(t, agent)

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

// TestConcurrentPromptsOnSameSessionEachCancelable proves cancel functions are
// tracked per in-flight turn rather than in a single slot: two prompts on the same
// session id both register, a single session/cancel reaches both (none orphaned),
// and the cancel entries drain to zero afterwards so no CancelFunc leaks.
func TestConcurrentPromptsOnSameSessionEachCancelable(t *testing.T) {
	const turns = 2
	chunks := make(chan struct{}, turns)
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		b, _ := json.Marshal(a2a.StreamResponse{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("x")}},
		}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
		chunks <- struct{}{}

		// Hold the stream open until the client cancels (or the test times out).
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("stream not torn down by cancellation")
		}
	})

	sessID := mustNewSession(t, agent)

	var wg sync.WaitGroup
	reasons := make([]string, turns)
	for i := 0; i < turns; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := agent.prompt(context.Background(), PromptParams{
				SessionID: sessID,
				Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
			}, func(any) error { return nil })
			if err != nil {
				t.Errorf("prompt %d returned error: %v", i, err)
				return
			}
			reasons[i] = res.(PromptResponse).StopReason
		}(i)
	}

	// Both streams are open, which means both turns registered their cancel
	// function before the chunk was emitted.
	for i := 0; i < turns; i++ {
		<-chunks
	}
	agent.mu.Lock()
	registered := agent.sessions[sessID].inFlight()
	agent.mu.Unlock()
	if registered != turns {
		t.Fatalf("in-flight turns = %d, want %d (per-turn cancel tracking)", registered, turns)
	}

	// A single cancel notification must reach every in-flight turn.
	agent.cancel(CancelParams{SessionID: sessID})
	wg.Wait()

	for i, got := range reasons {
		if got != StopReasonCancelled {
			t.Errorf("prompt %d stopReason = %q, want cancelled", i, got)
		}
	}
	agent.mu.Lock()
	remaining := agent.sessions[sessID].inFlight()
	agent.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("in-flight turns after cancel = %d, want 0 (leaked cancel funcs)", remaining)
	}
}

// mustNewSession creates a session and returns its id, failing the test if the
// cap refused it.
func mustNewSession(t *testing.T, a *Agent) string {
	t.Helper()
	resp, err := a.newSession()
	if err != nil {
		t.Fatalf("newSession returned error: %v", err)
	}
	return resp.SessionID
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
	id := mustNewSession(t, agent)
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
	id := mustNewSession(t, agent)

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

// TestSessionCloseViaRun exercises the session/close request through the dispatch
// table, including the empty-result response and removal of the session.
func TestSessionCloseViaRun(t *testing.T) {
	agent := noopAgent(t)
	id := mustNewSession(t, agent)

	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"session/close","params":{"sessionId":%q}}`, id) + "\n"
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
		t.Fatalf("session %q not removed via session/close", id)
	}
}

// TestSessionCloseNotificationViaRun verifies the fire-and-forget notification
// form of session/close removes the session and produces no response.
func TestSessionCloseNotificationViaRun(t *testing.T) {
	agent := noopAgent(t)
	id := mustNewSession(t, agent)

	input := fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/close","params":{"sessionId":%q}}`, id) + "\n"
	out := runLines(t, agent, input)
	if len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("notification produced output: %q", out)
	}
	if hasSession(agent, id) {
		t.Fatalf("session %q not removed via session/close notification", id)
	}
}

// TestSessionCloseMissingSessionIDReturnsCode verifies session/close validates
// its params.
func TestSessionCloseMissingSessionIDReturnsCode(t *testing.T) {
	agent := noopAgent(t)
	out := runLines(t, agent, `{"jsonrpc":"2.0","id":12,"method":"session/close","params":{}}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	assertErrorCode(t, msgs[0], codeInvalidParams)
}

// TestSessionListIsDeclined verifies session/list is deliberately unsupported:
// it is not advertised on initialize, and an attempted call is answered with
// -32601 rather than pretending to list ephemeral sessions.
func TestSessionListIsDeclined(t *testing.T) {
	agent := noopAgent(t)

	out := runLines(t, agent, `{"jsonrpc":"2.0","id":1,"method":"session/list","params":{}}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %v", len(msgs), msgs)
	}
	assertErrorCode(t, msgs[0], codeMethodNotFound)
}

// TestSessionCapEvictsLeastRecentlyUsed verifies the map is bounded: inserting
// past the cap evicts the least-recently-used session.
func TestSessionCapEvictsLeastRecentlyUsed(t *testing.T) {
	agent := noopAgent(t)
	agent.maxSessions = 3

	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, mustNewSession(t, agent))
	}
	// Touch the middle session so ids[1] becomes the most recently used.
	agent.mu.Lock()
	agent.seq++
	agent.sessions[ids[1]].lastUsed = agent.seq
	agent.mu.Unlock()

	newest := mustNewSession(t, agent)

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

// TestSessionCapRefusesWhenAllInFlight verifies the cap is strict: when every
// tracked session has an in-flight turn nothing can be evicted, so a new session
// is refused rather than growing the map past the cap. The in-flight session must
// be retained and its cancel function not invoked.
func TestSessionCapRefusesWhenAllInFlight(t *testing.T) {
	agent := noopAgent(t)
	agent.maxSessions = 1

	cancelCalled := false
	agent.mu.Lock()
	agent.sessions["busy"] = &session{
		cancels:  map[uint64]context.CancelFunc{0: func() { cancelCalled = true }},
		lastUsed: 0,
	}
	agent.mu.Unlock()

	_, err := agent.newSession()
	if err == nil {
		t.Fatal("expected newSession to be refused while every session is in-flight")
	}
	if !hasSession(agent, "busy") {
		t.Fatal("in-flight session must not be evicted")
	}
	if cancelCalled {
		t.Fatal("cancel func of an in-flight session should not be invoked")
	}
	if got := sessionCount(agent); got != 1 {
		t.Fatalf("session count = %d, want the cap to hold at 1", got)
	}
}

// TestPromptRefusesNewSessionWhenAllInFlight verifies the lazy create inside
// session/prompt also honours the strict cap: an unknown session id is refused
// when the map is full of in-flight sessions and no backend turn is attempted.
func TestPromptRefusesNewSessionWhenAllInFlight(t *testing.T) {
	agent := noopAgent(t)
	agent.maxSessions = 1

	agent.mu.Lock()
	agent.sessions["busy"] = &session{
		cancels:  map[uint64]context.CancelFunc{0: func() {}},
		lastUsed: 0,
	}
	agent.mu.Unlock()

	_, err := agent.prompt(context.Background(), PromptParams{
		SessionID: "unknown",
		Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
	}, func(any) error { return nil })
	if err == nil {
		t.Fatal("expected prompt to refuse creating a session past the strict cap")
	}
	if hasSession(agent, "unknown") {
		t.Fatal("refused prompt must not add the session to the map")
	}
	if got := sessionCount(agent); got != 1 {
		t.Fatalf("session count = %d, want the cap to hold at 1", got)
	}
}

// TestPromptRefusalDoesNotRaceSessionMap verifies the strict-cap refusal path
// reads the tracked-session count while still holding a.mu: it drives the prompt
// refusal concurrently with real mutations of the sessions map (the same write
// newSession and the lazy create perform) and must stay clean under -race. A
// read of len(a.sessions) after unlocking is a data race with those writes.
func TestPromptRefusalDoesNotRaceSessionMap(t *testing.T) {
	agent := noopAgent(t)
	agent.maxSessions = 1

	agent.mu.Lock()
	agent.sessions["busy"] = &session{cancels: map[uint64]context.CancelFunc{0: func() {}}}
	agent.mu.Unlock()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := agent.prompt(context.Background(), PromptParams{
				SessionID: "unknown",
				Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
			}, func(any) error { return nil }); err == nil {
				t.Error("expected prompt refusal while the only session is in-flight")
				return
			}
		}
	}()

	for i := 0; i < 2000; i++ {
		agent.mu.Lock()
		agent.sessions["churn"] = &session{}
		delete(agent.sessions, "churn")
		agent.mu.Unlock()
	}
	close(stop)
	wg.Wait()
}

// TestDeleteSessionCancelsInFlightTurn verifies deleteSession invokes every
// stored CancelFunc when it removes a session that still has running turns.
func TestDeleteSessionCancelsInFlightTurn(t *testing.T) {
	agent := noopAgent(t)
	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	agent.mu.Lock()
	agent.sessions["S"] = &session{cancels: map[uint64]context.CancelFunc{1: cancelA, 2: cancelB}}
	agent.mu.Unlock()

	agent.deleteSession("S")

	select {
	case <-ctxA.Done():
	default:
		t.Fatal("deleteSession did not cancel the first in-flight turn")
	}
	select {
	case <-ctxB.Done():
	default:
		t.Fatal("deleteSession did not cancel the second in-flight turn")
	}
	if hasSession(agent, "S") {
		t.Fatal("session not removed by deleteSession")
	}
}

// TestCloseSessionCancelsInFlightTurnAndRemoves verifies closeSession cancels all
// in-flight turns and frees the session, mirroring session/close semantics.
func TestCloseSessionCancelsInFlightTurnAndRemoves(t *testing.T) {
	agent := noopAgent(t)
	ctx, cancel := context.WithCancel(context.Background())
	agent.mu.Lock()
	agent.sessions["S"] = &session{cancels: map[uint64]context.CancelFunc{1: cancel}}
	agent.mu.Unlock()

	agent.closeSession("S")

	select {
	case <-ctx.Done():
	default:
		t.Fatal("closeSession did not cancel the in-flight turn")
	}
	if hasSession(agent, "S") {
		t.Fatal("session not removed by closeSession")
	}
	// Closing an unknown session succeeds silently.
	agent.closeSession("does-not-exist")
}

// TestCompletedTurnClearsCancel verifies an idle session holds no stale
// CancelFunc once its turn has finished.
func TestCompletedTurnClearsCancel(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})
	sessID := mustNewSession(t, agent)

	if _, err := agent.prompt(context.Background(), PromptParams{
		SessionID: sessID,
		Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
	}, func(any) error { return nil }); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}

	agent.mu.Lock()
	sess := agent.sessions[sessID]
	inFlight := sess.inFlight()
	agent.mu.Unlock()

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

func TestNewID(t *testing.T) {
	t.Run("random path returns prefixed id", func(t *testing.T) {
		id := newID("sess")
		if !strings.HasPrefix(id, "sess-") {
			t.Fatalf("id = %q, want sess- prefix", id)
		}
		if len(id) != len("sess-")+32 {
			t.Fatalf("id = %q, want 32 hex chars after prefix", id)
		}
	})

	t.Run("fallback is unique, not a constant", func(t *testing.T) {
		orig := idgen.RandRead
		idgen.RandRead = func([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
		t.Cleanup(func() { idgen.RandRead = orig })

		first := newID("sess")
		if first == "sess-16" {
			t.Fatalf("fallback collapsed to the constant %q", first)
		}
		if !strings.HasPrefix(first, "sess-") {
			t.Fatalf("fallback id = %q, want sess- prefix", first)
		}

		const n = 100
		ids := make([]string, n)
		var wg sync.WaitGroup
		for i := range ids {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ids[i] = newID("sess")
			}(i)
		}
		wg.Wait()

		seen := make(map[string]struct{}, n)
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				t.Fatalf("fallback ids are not unique: %q seen twice", id)
			}
			seen[id] = struct{}{}
		}
	})
}

// TestSessionDeleteNotificationViaRun verifies the fire-and-forget notification
// form of session/delete removes the session and produces no response.
func TestSessionDeleteNotificationViaRun(t *testing.T) {
	agent := noopAgent(t)
	id := mustNewSession(t, agent)

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
	id := mustNewSession(t, agent)

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

	_, _ = agent.newSession()

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
	id := mustNewSession(t, agent)

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
	id := mustNewSession(t, agent)

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
	_, _ = agent.newSession()
	close(released)
	wg.Wait()

	for i, got := range reasons {
		if got != StopReasonCancelled {
			t.Errorf("prompt %d stopReason = %q, want cancelled", i, got)
		}
	}
}

// TestStreamErrorMidStreamPropagatesAsRPCError verifies that a transport failure
// mid-stream (an undecodable SSE payload) surfaces as a JSON-RPC error for the
// prompt rather than a spurious end_turn, while chunks emitted before the
// failure are still delivered incrementally.
func TestStreamErrorMidStreamPropagatesAsRPCError(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)

		b, _ := json.Marshal(a2a.StreamResponse{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("partial")}},
		}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
		// A subsequent event that is not valid JSON must fail the stream.
		_, _ = fmt.Fprint(w, "data: {not-json}\n\n")
		if fl != nil {
			fl.Flush()
		}
	})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"hi"}]}}`,
	}, "\n") + "\n"

	msgs := decodeLines(t, runLines(t, agent, input))

	// init, session/new, chunk("partial"), prompt error.
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %v", len(msgs), msgs)
	}
	if got := chunkText(t, msgs[2]); got != "partial" {
		t.Errorf("pre-failure chunk = %q, want partial", got)
	}
	if _, hasResult := msgs[3]["result"]; hasResult {
		t.Errorf("prompt returned a result %v, want an error", msgs[3]["result"])
	}
	assertErrorCode(t, msgs[3], codeInternal)
}

// TestCancelMidStreamReturnsCancelled verifies that a session/cancel arriving
// while the A2A stream is still open tears the in-flight stream down and
// resolves the prompt with stopReason "cancelled" (not end_turn).
func TestCancelMidStreamReturnsCancelled(t *testing.T) {
	chunkSent := make(chan struct{})
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)

		b, _ := json.Marshal(a2a.StreamResponse{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-1",
			ContextID: "ctx-1",
			Artifact:  a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("partial")}},
		}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
		close(chunkSent)

		// Hold the stream open until the client cancels (or the test times out).
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("stream not torn down by cancellation")
		}
	})

	sessID := mustNewSession(t, agent)

	// Feed the prompt, wait until the stream is provably open, then cancel, so
	// the cancellation is genuinely mid-stream rather than racing registration.
	promptLine := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":%q,"prompt":[{"type":"text","text":"hi"}]}}`, sessID)
	cancelLine := fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":%q}}`, sessID)

	pr, pw := io.Pipe()
	go func() {
		_, _ = fmt.Fprintln(pw, promptLine)
		<-chunkSent
		_, _ = fmt.Fprintln(pw, cancelLine)
		_ = pw.Close()
	}()

	var out, errBuf bytes.Buffer
	if err := Run(context.Background(), pr, &out, &errBuf, agent); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	msgs := decodeLines(t, out.Bytes())

	var promptResp map[string]any
	for _, m := range msgs {
		if m["id"] == float64(3) {
			promptResp = m
		}
	}
	if promptResp == nil {
		t.Fatalf("no prompt response found in %v", msgs)
	}
	if _, hasErr := promptResp["error"]; hasErr {
		t.Fatalf("prompt returned an error, want cancelled: %v", promptResp["error"])
	}
	pr2 := promptResp["result"].(map[string]any)
	if pr2["stopReason"] != StopReasonCancelled {
		t.Errorf("stopReason = %v, want cancelled", pr2["stopReason"])
	}
}

// TestMidTurnCancelDoesNotCancelNextPrompt is the regression test for #780: a
// session/cancel that lands while turn A is in flight must resolve turn A
// "cancelled" but must NOT leave session-scoped state behind that spuriously
// cancels the next prompt. Turn B is issued on the same session afterwards and
// must reach the backend and return end_turn (previously the sticky flag made it
// resolve "cancelled" without doing any work).
func TestMidTurnCancelDoesNotCancelNextPrompt(t *testing.T) {
	var backendRequests atomic.Int32
	chunkSent := make(chan struct{})

	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		// First turn: stream a chunk, then hold the connection open until the
		// client cancels. Second turn: complete normally.
		if backendRequests.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			b, _ := json.Marshal(a2a.StreamResponse{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
				TaskID:    "task-1",
				ContextID: "ctx-1",
				Artifact:  a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("first")}},
			}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if fl != nil {
				fl.Flush()
			}
			close(chunkSent)
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
				t.Error("stream not torn down by cancellation")
			}
			return
		}
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-2", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})

	sessID := mustNewSession(t, agent)

	// Turn A: run it in the background, wait until its stream is provably open
	// (so the cancel is genuinely mid-turn rather than racing registration),
	// then cancel it mid-flight.
	type promptResult struct {
		reason string
		err    error
	}
	resA := make(chan promptResult, 1)
	go func() {
		res, err := agent.prompt(context.Background(), PromptParams{
			SessionID: sessID,
			Prompt:    []ContentBlock{{Type: "text", Text: "first"}},
		}, func(any) error { return nil })
		if err != nil {
			resA <- promptResult{err: err}
			return
		}
		resA <- promptResult{reason: res.(PromptResponse).StopReason}
	}()

	<-chunkSent
	agent.cancel(CancelParams{SessionID: sessID})

	gotA := <-resA
	if gotA.err != nil {
		t.Fatalf("turn A returned error: %v", gotA.err)
	}
	if gotA.reason != StopReasonCancelled {
		t.Fatalf("turn A stopReason = %q, want cancelled", gotA.reason)
	}

	// Turn B: must run normally, not inherit turn A's cancellation.
	resB, err := agent.prompt(context.Background(), PromptParams{
		SessionID: sessID,
		Prompt:    []ContentBlock{{Type: "text", Text: "second"}},
	}, func(any) error { return nil })
	if err != nil {
		t.Fatalf("turn B returned error: %v", err)
	}
	if got := resB.(PromptResponse).StopReason; got != StopReasonEndTurn {
		t.Errorf("turn B stopReason = %q, want end_turn (mid-turn cancel leaked onto the next prompt)", got)
	}
	if n := backendRequests.Load(); n != 2 {
		t.Errorf("backend requests = %d, want 2 (turn B did not execute)", n)
	}
}

// TestConcurrentMidTurnCancelDoesNotCancelFollowingPrompt covers the hard case
// the single-turn regression test does not: two turns are in flight at once
// (generations N and N+1). A cancel advances the watermark only to the newest
// in-flight generation (N+1), so both in-flight turns are cancelled while a
// following turn (generation N+2) is untouched and runs normally. It also pins
// the "cancel all in-flight" contract from #775 under the watermark design.
func TestConcurrentMidTurnCancelDoesNotCancelFollowingPrompt(t *testing.T) {
	var backendRequests atomic.Int32
	started := make(chan struct{}, 2)

	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		// The first two turns stream a chunk then hold the connection open until
		// the client cancels. The third turn (the following prompt) completes.
		if backendRequests.Add(1) <= 2 {
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			b, _ := json.Marshal(a2a.StreamResponse{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
				TaskID:    "task-1",
				ContextID: "ctx-1",
				Artifact:  a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("x")}},
			}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if fl != nil {
				fl.Flush()
			}
			started <- struct{}{}
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
				t.Error("stream not torn down by cancellation")
			}
			return
		}
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-3", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})

	sessID := mustNewSession(t, agent)

	type promptResult struct {
		reason string
		err    error
	}
	const inFlight = 2
	results := make(chan promptResult, inFlight)
	for i := 0; i < inFlight; i++ {
		go func() {
			res, err := agent.prompt(context.Background(), PromptParams{
				SessionID: sessID,
				Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
			}, func(any) error { return nil })
			if err != nil {
				results <- promptResult{err: err}
				return
			}
			results <- promptResult{reason: res.(PromptResponse).StopReason}
		}()
	}

	// Both streams are open, which means both turns registered their cancel
	// function and are concurrently in flight.
	for i := 0; i < inFlight; i++ {
		<-started
	}
	agent.mu.Lock()
	registered := agent.sessions[sessID].inFlight()
	agent.mu.Unlock()
	if registered != inFlight {
		t.Fatalf("in-flight turns = %d, want %d", registered, inFlight)
	}

	// One cancel must reach every in-flight turn.
	agent.cancel(CancelParams{SessionID: sessID})
	for i := 0; i < inFlight; i++ {
		got := <-results
		if got.err != nil {
			t.Fatalf("in-flight turn returned error: %v", got.err)
		}
		if got.reason != StopReasonCancelled {
			t.Errorf("in-flight turn stopReason = %q, want cancelled", got.reason)
		}
	}

	// The following turn (generation after the newest cancelled one) must run.
	res, err := agent.prompt(context.Background(), PromptParams{
		SessionID: sessID,
		Prompt:    []ContentBlock{{Type: "text", Text: "after"}},
	}, func(any) error { return nil })
	if err != nil {
		t.Fatalf("following prompt returned error: %v", err)
	}
	if got := res.(PromptResponse).StopReason; got != StopReasonEndTurn {
		t.Errorf("following prompt stopReason = %q, want end_turn (concurrent cancel leaked)", got)
	}
	if n := backendRequests.Load(); n != inFlight+1 {
		t.Errorf("backend requests = %d, want %d (following prompt did not execute)", n, inFlight+1)
	}
}
