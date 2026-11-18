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
	return NewAgent(client, "research-agent", "test", nil)
}

// streamAgentWithModes is streamAgent but with an explicit selectable mode list.
func streamAgentWithModes(t *testing.T, handler http.HandlerFunc, modes []Mode) *Agent {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := a2a.NewClientWithHTTP(srv.URL, "emt_test", srv.Client())
	return NewAgent(client, "research-agent", "test", modes)
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

// trackedTaskIDs returns the non-empty A2A task ids a session has tracked. The
// caller must hold Agent.mu. Test-only view over the per-turn state.
func trackedTaskIDs(s *session) []string {
	ids := make([]string, 0, len(s.cancels))
	for _, t := range s.cancels {
		if t.taskID != "" {
			ids = append(ids, t.taskID)
		}
	}
	return ids
}

// waitForInFlightTaskIDs blocks until the session has tracked at least n A2A
// task ids. A test that cancels must wait for the bridge to have actually read
// the id off the stream, or the cancel races the stream reader and finds nothing
// to cancel.
func waitForInFlightTaskIDs(t *testing.T, agent *Agent, sessionID string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		agent.mu.Lock()
		var got int
		if sess := agent.sessions[sessionID]; sess != nil {
			got = len(trackedTaskIDs(sess))
		}
		agent.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d tracked task ids (have %d)", n, got)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// isCancelRequest reports whether r is the A2A CancelTask call the bridge sends
// when a turn is abandoned (POST /tasks/{id}:cancel).
func isCancelRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":cancel")
}

// serveCancel answers a CancelTask request with a canceled task envelope so the
// bridge's best-effort cancel does not fall through to a test's stream handler.
func serveCancel(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/a2a+json")
	_, _ = w.Write([]byte(`{"id":"task-1","contextId":"ctx-1","status":{"state":"canceled"}}`))
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

// updateKind extracts params.update.sessionUpdate from a session/update message.
func updateKind(t *testing.T, m map[string]any) string {
	t.Helper()
	params, ok := m["params"].(map[string]any)
	if !ok {
		t.Fatalf("message has no params: %v", m)
	}
	update, ok := params["update"].(map[string]any)
	if !ok {
		t.Fatalf("params has no update object: %v", m)
	}
	kind, _ := update["sessionUpdate"].(string)
	return kind
}

// updateOf extracts params.update from a session/update message.
func updateOf(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	params, ok := m["params"].(map[string]any)
	if !ok {
		t.Fatalf("message has no params: %v", m)
	}
	update, ok := params["update"].(map[string]any)
	if !ok {
		t.Fatalf("params has no update object: %v", m)
	}
	return update
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

	// init, session/new, thought chunk (WORKING), chunk("Hel"), chunk("lo"),
	// prompt(end_turn).
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages, got %d: %v", len(msgs), msgs)
	}
	if got := updateKind(t, msgs[2]); got != "agent_thought_chunk" {
		t.Errorf("frame 3 = %q, want the WORKING progress chunk", got)
	}
	if got := chunkText(t, msgs[3]); got != "Hel" {
		t.Errorf("chunk 1 = %q, want Hel", got)
	}
	if got := chunkText(t, msgs[4]); got != "lo" {
		t.Errorf("chunk 2 = %q, want lo", got)
	}
	if pr := msgs[5]["result"].(map[string]any); pr["stopReason"] != "end_turn" {
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
	// init, session/new, thought chunk (WORKING), question chunk, prompt.
	if len(out1) != 5 {
		t.Fatalf("phase1: expected 5 messages (init, session/new, thought, question chunk, prompt), got %d: %v", len(out1), out1)
	}
	if got := chunkText(t, out1[3]); got != "What's your name?" {
		t.Errorf("question chunk = %q", got)
	}
	if pr := out1[4]["result"].(map[string]any); pr["stopReason"] != "end_turn" {
		t.Errorf("phase1 stopReason = %v", pr["stopReason"])
	}

	body1 := <-bodyCh
	if body1.Message.TaskID != "" {
		t.Errorf("phase1 taskId = %q, want empty", body1.Message.TaskID)
	}

	// Phase 2: same session resumes the paused task.
	input2 := `{"jsonrpc":"2.0","id":4,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"Alice"}]}}` + "\n"
	out2 := decodeLines(t, runLines(t, agent, input2))
	// thought chunk (WORKING), answer chunk, prompt.
	if len(out2) != 3 {
		t.Fatalf("phase2: expected 3 messages (thought, answer chunk, prompt), got %d: %v", len(out2), out2)
	}
	if got := chunkText(t, out2[1]); got != "Nice to meet you" {
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

// TestHITLResume_NoPendingQuestionRetriesAsNewTask pins the client safety net
// for issue #867: an older server rejects the resume of a paused task with no
// pending question. The client must clear the latched task id and retry the
// prompt as a new task in the same context instead of failing the turn.
func TestHITLResume_NoPendingQuestionRetriesAsNewTask(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []a2a.SendMessageRequest
	)
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		var body a2a.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()

		if body.Message.TaskID != "" {
			// Old server: the step-limit pause wrote no question row.
			w.Header().Set("Content-Type", "application/a2a+json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"status":"BAD_REQUEST","message":"no pending question for task","details":[{"@type":"type.googleapis.com/lf.a2a.v1.Error","reason":"INVALID_STATE","domain":"a2a-protocol.org"}]}}`))
			return
		}

		text := ""
		if len(body.Message.Parts) > 0 && body.Message.Parts[0].Text != nil {
			text = *body.Message.Parts[0].Text
		}
		if text == "hi" {
			writeSSEEvents(w, []a2a.StreamResponse{
				{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
				{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
				{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: &a2a.Message{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.TextPart("Step limit reached. Run has been paused.")}}}}},
			})
			return
		}

		// Retried as a new task in the same context: completes.
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-2", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-2", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{TaskID: "task-2", ContextID: "ctx-1", Artifact: a2a.Artifact{ArtifactID: "artifact-task-2", Parts: []a2a.Part{a2a.TextPart("continued")}}}},
			{Task: &a2a.Task{ID: "task-2", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})

	// Phase 1: new turn pauses at the step limit.
	input1 := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"hi"}]}}`,
	}, "\n") + "\n"
	_ = decodeLines(t, runLines(t, agent, input1))

	// Phase 2: the follow-up resume is rejected and retried as a new task.
	input2 := `{"jsonrpc":"2.0","id":4,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"continue"}]}}` + "\n"
	out2 := decodeLines(t, runLines(t, agent, input2))

	mu.Lock()
	got := append([]a2a.SendMessageRequest(nil), bodies...)
	mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("expected 3 backend calls (new, resume, retry), got %d", len(got))
	}
	if got[1].Message.TaskID != "task-1" {
		t.Errorf("resume taskId = %q, want task-1", got[1].Message.TaskID)
	}
	if got[2].Message.TaskID != "" {
		t.Errorf("retry taskId = %q, want empty (latched task cleared)", got[2].Message.TaskID)
	}
	if got[2].Message.ContextID != "ctx-1" {
		t.Errorf("retry contextId = %q, want ctx-1", got[2].Message.ContextID)
	}
	if _, hasSkill := got[2].Message.Metadata[a2a.SkillIDMetadataKey]; !hasSkill {
		t.Errorf("retry must send the skill id for a new task")
	}

	if len(out2) != 3 {
		t.Fatalf("phase2: expected 3 messages (thought, chunk, prompt), got %d: %v", len(out2), out2)
	}
	if got := updateKind(t, out2[0]); got != "agent_thought_chunk" {
		t.Errorf("phase2 first update = %q, want agent_thought_chunk", got)
	}
	if gotText := chunkText(t, out2[1]); gotText != "continued" {
		t.Errorf("phase2 chunk = %q, want %q", gotText, "continued")
	}
	if pr := out2[2]["result"].(map[string]any); pr["stopReason"] != "end_turn" {
		t.Errorf("phase2 stopReason = %v, want end_turn", pr["stopReason"])
	}
}

// TestA2ANewTaskRetry_PreservesMetadata pins the retry rewrite: the latched task
// id is cleared, the skill id is set for the new task, and metadata the original
// request carried survives (issue #867 review).
func TestA2ANewTaskRetry_PreservesMetadata(t *testing.T) {
	req := a2a.SendMessageRequest{Message: a2a.Message{
		TaskID:    "task-1",
		ContextID: "ctx-old",
		Metadata: map[string]any{
			"keep":                 "me",
			a2a.SkillIDMetadataKey: "stale-skill",
		},
	}}

	got := a2aNewTaskRetry(req, "research-agent", "ctx-1")

	if got.Message.TaskID != "" {
		t.Fatalf("taskId = %q, want empty", got.Message.TaskID)
	}
	if got.Message.ContextID != "ctx-1" {
		t.Fatalf("contextId = %q, want ctx-1", got.Message.ContextID)
	}
	if v, _ := got.Message.Metadata["keep"].(string); v != "me" {
		t.Fatalf("metadata keep = %v, want \"me\" (unrelated metadata must survive)", got.Message.Metadata["keep"])
	}
	if v, _ := got.Message.Metadata[a2a.SkillIDMetadataKey].(string); v != "research-agent" {
		t.Fatalf("metadata skill = %v, want research-agent", got.Message.Metadata[a2a.SkillIDMetadataKey])
	}
	if req.Message.TaskID != "task-1" || req.Message.ContextID != "ctx-old" {
		t.Fatalf("the original request must not be mutated")
	}
}

// TestA2ANewTaskRetry_EmptySkillLeavesNoSkillKey pins that an unknown skill does
// not overwrite the map with an empty-string skill id.
func TestA2ANewTaskRetry_EmptySkillLeavesNoSkillKey(t *testing.T) {
	req := a2a.SendMessageRequest{Message: a2a.Message{
		Metadata: map[string]any{"keep": "me"},
	}}

	got := a2aNewTaskRetry(req, "", "")

	if _, ok := got.Message.Metadata[a2a.SkillIDMetadataKey]; ok {
		t.Fatalf("empty skill must not be written into metadata: %v", got.Message.Metadata)
	}
	if v, _ := got.Message.Metadata["keep"].(string); v != "me" {
		t.Fatalf("metadata keep = %v, want \"me\"", got.Message.Metadata["keep"])
	}
}

// TestIsInvalidStateError pins the structured match: the reason decides, not the
// human-readable message.
func TestIsInvalidStateError(t *testing.T) {
	if !isInvalidStateError(&a2a.Error{Reason: "INVALID_STATE", Message: "reworded by a newer server"}) {
		t.Fatalf("a structured INVALID_STATE must match regardless of wording")
	}
	if isInvalidStateError(&a2a.Error{Reason: "TASK_NOT_FOUND", Message: "no pending question for task"}) {
		t.Fatalf("a different reason must not match even with the legacy wording")
	}
	if isInvalidStateError(errors.New("no pending question for task")) {
		t.Fatalf("a non-A2A error must not match")
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
		if isCancelRequest(r) {
			serveCancel(w)
			return
		}
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
		if isCancelRequest(r) {
			serveCancel(w)
			return
		}
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
		cancels:  map[uint64]*promptTurn{0: {cancel: func() { cancelCalled = true }}},
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
		cancels:  map[uint64]*promptTurn{0: {cancel: func() {}}},
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
	agent.sessions["busy"] = &session{cancels: map[uint64]*promptTurn{0: {cancel: func() {}}}}
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
	agent.sessions["S"] = &session{cancels: map[uint64]*promptTurn{1: {cancel: cancelA}, 2: {cancel: cancelB}}}
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
	agent.sessions["S"] = &session{cancels: map[uint64]*promptTurn{1: {cancel: cancel}}}
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
		if isCancelRequest(r) {
			serveCancel(w)
			return
		}
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

// TestCancelMidStreamCancelsMemoryTask is the regression test for #868: a
// session/cancel must ask the Memory platform to cancel the in-flight A2A task,
// not merely tear down the local SSE stream (which left the run executing in its
// sandbox). The cancel call is best-effort and must never block the ACP turn:
// here the server waits on a channel before answering CancelTask, and the prompt
// still resolves "cancelled" first.
func TestCancelMidStreamCancelsMemoryTask(t *testing.T) {
	streamStarted := make(chan struct{})
	cancelArrived := make(chan string, 1)
	releaseCancel := make(chan struct{})
	var cancelCalled atomic.Bool

	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if isCancelRequest(r) {
			cancelCalled.Store(true)
			cancelArrived <- strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/"), ":cancel")
			<-releaseCancel
			serveCancel(w)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		b, _ := json.Marshal(a2a.StreamResponse{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
			TaskID:    "task-7",
			ContextID: "ctx-1",
			Artifact:  a2a.Artifact{ArtifactID: "artifact-task-7", Parts: []a2a.Part{a2a.TextPart("working")}},
		}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
		close(streamStarted)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("stream not torn down by cancellation")
		}
	})

	sessID := mustNewSession(t, agent)
	type promptResult struct {
		reason string
		err    error
	}
	resCh := make(chan promptResult, 1)
	go func() {
		res, err := agent.prompt(context.Background(), PromptParams{
			SessionID: sessID,
			Prompt:    []ContentBlock{{Type: "text", Text: "go"}},
		}, func(any) error { return nil })
		if err != nil {
			resCh <- promptResult{err: err}
			return
		}
		resCh <- promptResult{reason: res.(PromptResponse).StopReason}
	}()

	<-streamStarted
	waitForInFlightTaskIDs(t, agent, sessID, 1)
	agent.cancel(CancelParams{SessionID: sessID})

	// The turn resolves while the CancelTask request is still unanswered: the
	// client is never blocked on the platform call.
	got := <-resCh
	if got.err != nil {
		t.Fatalf("prompt returned error: %v", got.err)
	}
	if got.reason != StopReasonCancelled {
		t.Fatalf("stopReason = %q, want cancelled", got.reason)
	}

	select {
	case id := <-cancelArrived:
		if id != "task-7" {
			t.Errorf("cancelled task id = %q, want task-7", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session/cancel did not call CancelTask on the in-flight A2A task")
	}
	close(releaseCancel)
	if !cancelCalled.Load() {
		t.Fatal("cancelled state not observed")
	}
}

// TestCancelAllInFlightTasks proves the task id is tracked per in-flight turn:
// a single session/cancel cancels every running A2A task, not just one.
func TestCancelAllInFlightTasks(t *testing.T) {
	const turns = 2
	started := make(chan struct{}, turns)
	cancelled := make(chan string, turns)
	var nextID atomic.Int32
	streamStarted := make(chan string, turns)

	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if isCancelRequest(r) {
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tasks/"), ":cancel")
			cancelled <- id
			serveCancel(w)
			return
		}
		id := fmt.Sprintf("task-%d", nextID.Add(1))
		streamStarted <- id
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		b, _ := json.Marshal(a2a.StreamResponse{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
			TaskID:    id,
			ContextID: "ctx-1",
			Artifact:  a2a.Artifact{ArtifactID: "artifact-" + id, Parts: []a2a.Part{a2a.TextPart("working")}},
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
	})

	sessID := mustNewSession(t, agent)
	reasons := make(chan string, turns)
	for i := 0; i < turns; i++ {
		go func() {
			res, err := agent.prompt(context.Background(), PromptParams{
				SessionID: sessID,
				Prompt:    []ContentBlock{{Type: "text", Text: "go"}},
			}, func(any) error { return nil })
			if err != nil {
				t.Errorf("prompt returned error: %v", err)
				reasons <- ""
				return
			}
			reasons <- res.(PromptResponse).StopReason
		}()
	}

	wantIDs := map[string]bool{}
	for i := 0; i < turns; i++ {
		<-started
		wantIDs[<-streamStarted] = true
	}
	waitForInFlightTaskIDs(t, agent, sessID, turns)
	agent.cancel(CancelParams{SessionID: sessID})

	for i := 0; i < turns; i++ {
		if got := <-reasons; got != StopReasonCancelled {
			t.Errorf("stopReason = %q, want cancelled", got)
		}
	}
	gotIDs := map[string]bool{}
	for i := 0; i < turns; i++ {
		select {
		case id := <-cancelled:
			gotIDs[id] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d in-flight tasks were cancelled", len(gotIDs), turns)
		}
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Errorf("in-flight task %q was not cancelled", id)
		}
	}
}

// TestCompletedTurnClearsTaskID pins the other half of the task-id tracking: a
// finished turn must not leave its task id on the session, otherwise a later
// session/cancel would cancel an unrelated (possibly already paused) task.
func TestCompletedTurnClearsTaskID(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if isCancelRequest(r) {
			t.Errorf("unexpected CancelTask after the turn completed")
			serveCancel(w)
			return
		}
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
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
	leftover := trackedTaskIDs(sess)
	agent.mu.Unlock()
	if len(leftover) != 0 {
		t.Fatalf("finished turn left task ids behind: %v", leftover)
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
		if isCancelRequest(r) {
			serveCancel(w)
			return
		}
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
	var streamRequests atomic.Int32
	started := make(chan struct{}, 2)
	cancelled := make(chan struct{}, 4)

	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if isCancelRequest(r) {
			select {
			case cancelled <- struct{}{}:
			default:
			}
			serveCancel(w)
			return
		}
		// The first two turns stream a chunk then hold the connection open until
		// the client cancels. The third turn (the following prompt) completes.
		if streamRequests.Add(1) <= 2 {
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

	// Each turn stashes the task id when the stream names it. Wait for both so
	// the cancel below is not racing the first event's arrival: an unnamed turn
	// has nothing to cancel remotely (issue #868).
	deadline := time.Now().Add(2 * time.Second)
	for {
		agent.mu.Lock()
		named := 0
		for _, tn := range agent.sessions[sessID].cancels {
			if tn.taskID != "" {
				named++
			}
		}
		agent.mu.Unlock()
		if named == inFlight {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("in-flight turns with a task id = %d, want %d", named, inFlight)
		}
		time.Sleep(time.Millisecond)
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
	if n := streamRequests.Load(); n != inFlight+1 {
		t.Errorf("stream requests = %d, want %d (following prompt did not execute)", n, inFlight+1)
	}
	// Cancelling the local streams is not enough: each in-flight turn had named
	// task-1, so the cancel must also reach the Memory task (issue #868).
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Error("session/cancel never reached the backend CancelTask endpoint")
	}
}

// --- Session modes / config options ---

// TestSessionNewAdvertisesModes verifies session/new returns the advertised mode
// list (modes + configOptions) with the default skill as the current selection.
func TestSessionNewAdvertisesModes(t *testing.T) {
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	}, []Mode{
		{ID: "research-agent", Name: "Research", Description: "researches"},
		{ID: "writer-agent", Name: "Writer", Description: "writes"},
	})

	out := runLines(t, agent, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	result := msgs[0]["result"].(map[string]any)

	modes, ok := result["modes"].(map[string]any)
	if !ok {
		t.Fatalf("result.modes = %v, want object", result["modes"])
	}
	if modes["currentModeId"] != "research-agent" {
		t.Errorf("currentModeId = %v, want research-agent", modes["currentModeId"])
	}
	available, ok := modes["availableModes"].([]any)
	if !ok || len(available) != 2 {
		t.Fatalf("availableModes = %v, want 2 entries", modes["availableModes"])
	}
	first := available[0].(map[string]any)
	if first["id"] != "research-agent" || first["name"] != "Research" || first["description"] != "researches" {
		t.Errorf("mode[0] = %v, want id/name/description for research-agent", first)
	}

	opts, ok := result["configOptions"].([]any)
	if !ok || len(opts) != 1 {
		t.Fatalf("configOptions = %v, want 1 entry", result["configOptions"])
	}
	opt := opts[0].(map[string]any)
	if opt["id"] != "agent" || opt["category"] != "model" || opt["type"] != "select" || opt["currentValue"] != "research-agent" {
		t.Errorf("configOption = %v, want agent/model/select/research-agent", opt)
	}
	values, ok := opt["options"].([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("configOption.options = %v, want 2 entries", opt["options"])
	}
}

// TestSessionNewSingleDefaultMode verifies a nil mode list degrades to a single
// default mode, so a session always has a valid current mode.
func TestSessionNewSingleDefaultMode(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})

	out := runLines(t, agent, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`+"\n")
	msgs := decodeLines(t, out)
	result := msgs[0]["result"].(map[string]any)
	modes := result["modes"].(map[string]any)
	if modes["currentModeId"] != "research-agent" {
		t.Errorf("currentModeId = %v, want research-agent", modes["currentModeId"])
	}
	available := modes["availableModes"].([]any)
	if len(available) != 1 {
		t.Fatalf("availableModes = %v, want 1 default entry", available)
	}
	if available[0].(map[string]any)["id"] != "research-agent" {
		t.Errorf("mode[0].id = %v, want research-agent", available[0].(map[string]any)["id"])
	}
}

// TestEnsureDefaultMode verifies the configured default skill is advertised only
// when the mode list is empty (degradation). A non-empty list is trusted as-is:
// it comes from the external-only AgentCard, so a default that is not
// external-reachable (internal/project/stale) is never prepended as a selectable
// mode.
func TestEnsureDefaultMode(t *testing.T) {
	cases := []struct {
		name  string
		modes []Mode
		skill string
		want  []string
	}{
		{
			name:  "external default already present is kept",
			modes: []Mode{{ID: "research-agent"}, {ID: "writer-agent"}},
			skill: "research-agent",
			want:  []string{"research-agent", "writer-agent"},
		},
		{
			name:  "project default absent is not prepended",
			modes: []Mode{{ID: "writer-agent"}},
			skill: "project-only-agent",
			want:  []string{"writer-agent"},
		},
		{
			name:  "internal default absent is not prepended",
			modes: []Mode{{ID: "writer-agent"}},
			skill: "internal-agent",
			want:  []string{"writer-agent"},
		},
		{
			name:  "empty list degrades to the default",
			modes: nil,
			skill: "research-agent",
			want:  []string{"research-agent"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ensureDefaultMode(tc.modes, tc.skill)
			if len(got) != len(tc.want) {
				t.Fatalf("ensureDefaultMode = %v, want ids %v", modeIDs(got), tc.want)
			}
			for i := range got {
				if got[i].ID != tc.want[i] {
					t.Errorf("mode[%d].id = %q, want %q", i, got[i].ID, tc.want[i])
				}
			}
		})
	}
}

// modeIDs extracts the id field of each mode, for readable test failures.
func modeIDs(modes []Mode) []string {
	ids := make([]string, len(modes))
	for i, m := range modes {
		ids[i] = m.ID
	}
	return ids
}

// TestSetModeViaRun verifies session/set_mode switches the session skill, emits
// a current_mode_update notification carrying the new mode, and returns an empty
// result object (ACP defines SetSessionModeResponse as an empty object).
func TestSetModeViaRun(t *testing.T) {
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	}, []Mode{{ID: "research-agent", Name: "Research"}, {ID: "writer-agent", Name: "Writer"}})
	id := mustNewSession(t, agent)

	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"session/set_mode","params":{"sessionId":%q,"modeId":"writer-agent"}}`, id) + "\n"
	msgs := decodeLines(t, runLines(t, agent, input))
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (notification + response), got %d", len(msgs))
	}

	notif := msgs[0]
	if notif["method"] != "session/update" {
		t.Errorf("notification method = %v, want session/update", notif["method"])
	}
	params, ok := notif["params"].(map[string]any)
	if !ok {
		t.Fatalf("notification params = %v, want object", notif["params"])
	}
	if params["sessionId"] != id {
		t.Errorf("notification sessionId = %v, want %q", params["sessionId"], id)
	}
	update, ok := params["update"].(map[string]any)
	if !ok {
		t.Fatalf("notification update = %v, want object", params["update"])
	}
	if update["sessionUpdate"] != "current_mode_update" {
		t.Errorf("sessionUpdate = %v, want current_mode_update", update["sessionUpdate"])
	}
	if update["currentModeId"] != "writer-agent" {
		t.Errorf("currentModeId = %v, want writer-agent", update["currentModeId"])
	}

	resp := msgs[1]
	if resp["id"] != float64(3) {
		t.Errorf("response id = %v, want 3", resp["id"])
	}
	if result, ok := resp["result"].(map[string]any); !ok || len(result) != 0 {
		t.Errorf("set_mode result = %v, want empty object", resp["result"])
	}

	agent.mu.Lock()
	skill := agent.sessions[id].skill
	agent.mu.Unlock()
	if skill != "writer-agent" {
		t.Errorf("session skill = %q, want writer-agent", skill)
	}
}

// TestSetModeUnknownModeReturnsError verifies set_mode rejects a mode that was
// not advertised.
func TestSetModeUnknownModeReturnsError(t *testing.T) {
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	}, []Mode{{ID: "research-agent", Name: "Research"}})
	id := mustNewSession(t, agent)

	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"session/set_mode","params":{"sessionId":%q,"modeId":"nope"}}`, id) + "\n"
	msgs := decodeLines(t, runLines(t, agent, input))
	assertErrorCode(t, msgs[0], codeInvalidParams)
}

// TestSetModeMissingSessionIDReturnsError verifies set_mode validates params.
func TestSetModeMissingSessionIDReturnsError(t *testing.T) {
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	}, []Mode{{ID: "research-agent", Name: "Research"}})

	out := runLines(t, agent, `{"jsonrpc":"2.0","id":3,"method":"session/set_mode","params":{"modeId":"research-agent"}}`+"\n")
	msgs := decodeLines(t, out)
	assertErrorCode(t, msgs[0], codeInvalidParams)
}

// TestSetModeUnknownSessionReturnsError verifies set_mode rejects an untracked
// session id.
func TestSetModeUnknownSessionReturnsError(t *testing.T) {
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	}, []Mode{{ID: "research-agent", Name: "Research"}})

	out := runLines(t, agent, `{"jsonrpc":"2.0","id":3,"method":"session/set_mode","params":{"sessionId":"missing","modeId":"research-agent"}}`+"\n")
	msgs := decodeLines(t, out)
	assertErrorCode(t, msgs[0], codeInvalidParams)
}

// TestSetConfigOptionViaRun verifies session/set_config_option switches the
// session skill and returns the updated config options.
func TestSetConfigOptionViaRun(t *testing.T) {
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	}, []Mode{{ID: "research-agent", Name: "Research"}, {ID: "writer-agent", Name: "Writer"}})
	id := mustNewSession(t, agent)

	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"session/set_config_option","params":{"sessionId":%q,"configId":"agent","value":"writer-agent"}}`, id) + "\n"
	msgs := decodeLines(t, runLines(t, agent, input))
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	result := msgs[0]["result"].(map[string]any)
	opts, ok := result["configOptions"].([]any)
	if !ok || len(opts) != 1 {
		t.Fatalf("configOptions = %v, want 1 entry", result["configOptions"])
	}
	if opts[0].(map[string]any)["currentValue"] != "writer-agent" {
		t.Errorf("currentValue = %v, want writer-agent", opts[0].(map[string]any)["currentValue"])
	}
	agent.mu.Lock()
	skill := agent.sessions[id].skill
	agent.mu.Unlock()
	if skill != "writer-agent" {
		t.Errorf("session skill = %q, want writer-agent", skill)
	}
}

// TestSetConfigOptionUnknownConfigReturnsError verifies set_config_option
// rejects an unknown config id.
func TestSetConfigOptionUnknownConfigReturnsError(t *testing.T) {
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	}, []Mode{{ID: "research-agent", Name: "Research"}})
	id := mustNewSession(t, agent)

	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"session/set_config_option","params":{"sessionId":%q,"configId":"nope","value":"research-agent"}}`, id) + "\n"
	msgs := decodeLines(t, runLines(t, agent, input))
	assertErrorCode(t, msgs[0], codeInvalidParams)
}

// TestSetModeRoutesPromptToSelectedAgent verifies a prompt after set_mode sends
// the selected skill id, not the default.
func TestSetModeRoutesPromptToSelectedAgent(t *testing.T) {
	bodyCh := make(chan a2a.SendMessageRequest, 1)
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		var body a2a.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodyCh <- body
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	}, []Mode{{ID: "research-agent", Name: "Research"}, {ID: "writer-agent", Name: "Writer"}})

	sessID := mustNewSession(t, agent)
	if err := agent.setMode(SetModeParams{SessionID: sessID, ModeID: "writer-agent"}); err != nil {
		t.Fatalf("setMode returned error: %v", err)
	}

	if _, err := agent.prompt(context.Background(), PromptParams{
		SessionID: sessID,
		Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
	}, func(any) error { return nil }); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}

	body := <-bodyCh
	if got := body.Message.Metadata[a2a.SkillIDMetadataKey]; got != "writer-agent" {
		t.Errorf("skillId = %v, want writer-agent", got)
	}
}

// TestSetConfigOptionRoutesPromptToSelectedAgent mirrors the set_mode routing
// case for the config-options surface.
func TestSetConfigOptionRoutesPromptToSelectedAgent(t *testing.T) {
	bodyCh := make(chan a2a.SendMessageRequest, 1)
	agent := streamAgentWithModes(t, func(w http.ResponseWriter, r *http.Request) {
		var body a2a.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodyCh <- body
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	}, []Mode{{ID: "research-agent", Name: "Research"}, {ID: "writer-agent", Name: "Writer"}})

	sessID := mustNewSession(t, agent)
	if _, err := agent.setConfigOption(SetConfigOptionParams{SessionID: sessID, ConfigID: "agent", Value: "writer-agent"}); err != nil {
		t.Fatalf("setConfigOption returned error: %v", err)
	}

	if _, err := agent.prompt(context.Background(), PromptParams{
		SessionID: sessID,
		Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
	}, func(any) error { return nil }); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}

	body := <-bodyCh
	if got := body.Message.Metadata[a2a.SkillIDMetadataKey]; got != "writer-agent" {
		t.Errorf("skillId = %v, want writer-agent", got)
	}
}

// =============================================================================
// #866: tool calls and progress are streamed, not silently dropped.
// =============================================================================

// TestPromptStreamsToolCalls pins the #866 fix: the server already emits each
// tool call as a structured Data artifact (start: {toolName, input}; end:
// {toolName, output} with append:true under the same artifact id), and the
// bridge must surface both as ACP tool_call / tool_call_update frames instead of
// keeping only text parts. It also pins the WORKING transition as one progress
// chunk and that the text path is unchanged.
func TestPromptStreamsToolCalls(t *testing.T) {
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
				TaskID:    "task-1",
				ContextID: "ctx-1",
				Artifact: a2a.Artifact{
					ArtifactID: "tool-call-1",
					Name:       "workspace_bash",
					Parts:      []a2a.Part{{Data: map[string]any{"toolName": "workspace_bash", "input": map[string]any{"command": "go test ./..."}}}},
				},
			}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
				TaskID:    "task-1",
				ContextID: "ctx-1",
				Artifact: a2a.Artifact{
					ArtifactID: "tool-call-1",
					Name:       "workspace_bash",
					Parts:      []a2a.Part{{Data: map[string]any{"toolName": "workspace_bash", "output": "ok  	example.com/x	0.2s"}}},
				},
				Append: true,
			}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Artifact: a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("Tests pass.")}}}},
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})
	sessID := mustNewSession(t, agent)

	var frames []map[string]any
	if _, err := agent.prompt(context.Background(), PromptParams{
		SessionID: sessID,
		Prompt:    []ContentBlock{{Type: "text", Text: "run the tests"}},
	}, func(m any) error {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		var decoded map[string]any
		if err := json.Unmarshal(b, &decoded); err != nil {
			return err
		}
		if params, ok := decoded["params"].(map[string]any); ok && params["sessionId"] != sessID {
			t.Errorf("sessionId = %v, want %s", params["sessionId"], sessID)
		}
		frames = append(frames, decoded)
		return nil
	}); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}

	// progress chunk, tool_call, tool_call_update, agent_message_chunk.
	if len(frames) != 4 {
		t.Fatalf("frames = %d, want 4: %v", len(frames), frames)
	}
	if got := updateKind(t, frames[0]); got != "agent_thought_chunk" {
		t.Errorf("frame 1 = %q, want the WORKING progress chunk", got)
	}

	start := updateOf(t, frames[1])
	if got := start["sessionUpdate"]; got != "tool_call" {
		t.Fatalf("frame 2 sessionUpdate = %v, want tool_call", got)
	}
	if got := start["toolCallId"]; got != "task-1:tool-call-1" {
		t.Errorf("toolCallId = %v, want task-1:tool-call-1 (task-namespaced artifact id)", got)
	}
	if got := start["title"]; got != "workspace_bash" {
		t.Errorf("title = %v, want workspace_bash", got)
	}
	// `name` is optional in ACP: absent is fine, and so is echoing the title.
	// What must not happen is a third value a client cannot interpret.
	if got, ok := start["name"]; ok && got != "workspace_bash" {
		t.Errorf("name = %v, want absent or workspace_bash", got)
	}
	if got := start["kind"]; got != "execute" {
		t.Errorf("kind = %v, want execute", got)
	}
	if got := start["status"]; got != "pending" {
		t.Errorf("status = %v, want pending", got)
	}
	input, ok := start["rawInput"].(map[string]any)
	if !ok || input["command"] != "go test ./..." {
		t.Errorf("rawInput = %v, want the tool input", start["rawInput"])
	}

	end := updateOf(t, frames[2])
	if got := end["sessionUpdate"]; got != "tool_call_update" {
		t.Fatalf("frame 3 sessionUpdate = %v, want tool_call_update", got)
	}
	if got := end["toolCallId"]; got != "task-1:tool-call-1" {
		t.Errorf("toolCallId = %v, want task-1:tool-call-1", got)
	}
	if got := end["status"]; got != "completed" {
		t.Errorf("status = %v, want completed", got)
	}
	if out, _ := end["rawOutput"].(string); !strings.Contains(out, "0.2s") {
		t.Errorf("rawOutput = %v, want the tool output", end["rawOutput"])
	}

	// The text path is unchanged: the tool frames must not swallow the reply.
	if got := chunkText(t, frames[3]); got != "Tests pass." {
		t.Errorf("text chunk = %q, want Tests pass.", got)
	}
}

// TestToolCallIDsAreUniqueAcrossTurns pins the #866 contract that a toolCallId
// identifies one call within an ACP session. One ACP session is one A2A context,
// every prompt is a new task, and the server's toolSeq restarts at 1 per stream
// (a resume restarts it too), so the bare artifact id repeats on every turn. A
// client that keys by it folds the new call into the last one.
func TestToolCallIDsAreUniqueAcrossTurns(t *testing.T) {
	var turn atomic.Int32
	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		n := turn.Add(1)
		taskID := fmt.Sprintf("task-%d", n)
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: taskID, ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
				TaskID:    taskID,
				ContextID: "ctx-1",
				Artifact: a2a.Artifact{
					ArtifactID: "tool-call-1",
					Name:       "workspace_bash",
					Parts:      []a2a.Part{{Data: map[string]any{"toolName": "workspace_bash", "input": map[string]any{"command": "true"}}}},
				},
			}},
			{Task: &a2a.Task{ID: taskID, ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		})
	})
	sessID := mustNewSession(t, agent)

	send := func() string {
		var id string
		if _, err := agent.prompt(context.Background(), PromptParams{
			SessionID: sessID,
			Prompt:    []ContentBlock{{Type: "text", Text: "go"}},
		}, func(m any) error {
			b, err := json.Marshal(m)
			if err != nil {
				return err
			}
			var decoded map[string]any
			if err := json.Unmarshal(b, &decoded); err != nil {
				return err
			}
			params, _ := decoded["params"].(map[string]any)
			update, _ := params["update"].(map[string]any)
			if update["sessionUpdate"] == "tool_call" {
				id, _ = update["toolCallId"].(string)
			}
			return nil
		}); err != nil {
			t.Fatalf("prompt returned error: %v", err)
		}
		if id == "" {
			t.Fatal("the turn emitted no tool_call")
		}
		return id
	}

	first := send()
	second := send()
	if first != "task-1:tool-call-1" {
		t.Errorf("first toolCallId = %q, want task-1:tool-call-1", first)
	}
	if second != "task-2:tool-call-1" {
		t.Errorf("second toolCallId = %q, want task-2:tool-call-1", second)
	}
	if first == second {
		t.Errorf("toolCallId %q repeats across turns: a client keying by it folds the calls", first)
	}
}

// TestToolCallFromArtifact classifies the artifacts the server emits, so the
// text artifact and A2UI surfaces are never mistaken for tool calls.
func TestToolCallFromArtifact(t *testing.T) {
	tests := []struct {
		name  string
		event *a2a.TaskArtifactUpdateEvent
		want  bool
		end   bool
	}{
		{
			name: "tool start",
			event: &a2a.TaskArtifactUpdateEvent{Artifact: a2a.Artifact{
				ArtifactID: "tool-call-1",
				Name:       "workspace_read",
				Parts:      []a2a.Part{{Data: map[string]any{"toolName": "workspace_read", "input": map[string]any{"path": "a.go"}}}},
			}},
			want: true,
		},
		{
			name: "tool end",
			event: &a2a.TaskArtifactUpdateEvent{Append: true, Artifact: a2a.Artifact{
				ArtifactID: "tool-call-1",
				Name:       "workspace_read",
				Parts:      []a2a.Part{{Data: map[string]any{"toolName": "workspace_read", "output": "package a"}}},
			}},
			want: true,
			end:  true,
		},
		{
			name:  "text artifact",
			event: &a2a.TaskArtifactUpdateEvent{Artifact: a2a.Artifact{ArtifactID: "artifact-task-1", Parts: []a2a.Part{a2a.TextPart("hi")}}},
		},
		{
			name: "a2ui surface",
			event: &a2a.TaskArtifactUpdateEvent{Artifact: a2a.Artifact{
				ArtifactID: "a2ui-s1",
				Parts:      []a2a.Part{{Data: []any{map[string]any{"surfaceId": "s1"}}}},
			}},
		},
		{
			name: "structured data without a tool name",
			event: &a2a.TaskArtifactUpdateEvent{Artifact: a2a.Artifact{
				ArtifactID: "something",
				Parts:      []a2a.Part{{Data: map[string]any{"todo": "x"}}},
			}},
		},
		{name: "nil event"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toolCallFromArtifact(tc.event, "task-1")
			if ok != tc.want {
				t.Fatalf("toolCallFromArtifact ok = %v, want %v", ok, tc.want)
			}
			if got.End != tc.end {
				t.Errorf("End = %v, want %v", got.End, tc.end)
			}
			if ok && got.ID == "" {
				t.Error("tool call has no id")
			}
		})
	}
}

// TestToolKind pins the tool-name to ACP ToolKind mapping, including the
// fallback for a tool the table has never seen.
func TestToolKind(t *testing.T) {
	cases := map[string]string{
		"workspace_bash":    "execute",
		"run_python":        "execute",
		"workspace_git":     "execute",
		"workspace_read":    "read",
		"workspace_glob":    "search",
		"workspace_grep":    "search",
		"workspace_write":   "edit",
		"workspace_edit":    "edit",
		"web-fetch":         "fetch",
		"session-todo-list": "other",
		"ask_user":          "other",
		"":                  "other",
		// Keyword fallback: the specific verbs must win over the "run_"/"git"
		// substrings, or a read tool is rendered as a shell command.
		"run_read":    "read",
		"run_edit":    "edit",
		"run_search":  "search",
		"custom_bash": "execute",
		"custom_git":  "execute",
	}
	for name, want := range cases {
		if got := toolKind(name); got != want {
			t.Errorf("toolKind(%q) = %q, want %q", name, got, want)
		}
	}
}

// =============================================================================
// #868: session/cancel must cancel the Memory run, not just the local request.
// =============================================================================

// TestCancelMidStreamCancelsMemoryTask pins the #868 fix: a session/cancel
// arriving while the A2A stream is open must also call CancelTask for the
// in-flight task, so the run stops in its sandbox instead of executing to
// completion for a client that has stopped reading.
func TestCancelMidStreamCancelsMemoryTaskDev(t *testing.T) {
	streaming := make(chan struct{})
	cancelled := make(chan string, 1)

	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":cancel") {
			cancelled <- r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"task-1","status":{"state":"TASK_STATE_CANCELED"}}`))
			return
		}
		// Name the task on the first event (the SUBMITTED snapshot), then stream
		// a tool call and hold the connection open until the client cancels.
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
		})
		close(streaming)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("stream not torn down by cancellation")
		}
	})

	sessID := mustNewSession(t, agent)
	promptLine := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":%q,"prompt":[{"type":"text","text":"hi"}]}}`, sessID)
	cancelLine := fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":%q}}`, sessID)

	pr, pw := io.Pipe()
	go func() {
		_, _ = fmt.Fprintln(pw, promptLine)
		<-streaming
		// Wait until the turn has stashed the task id from the stream: the
		// cancel must not race the first event's arrival, or there is nothing
		// to cancel remotely yet.
		deadline := time.Now().Add(2 * time.Second)
		for {
			agent.mu.Lock()
			named := false
			if s := agent.sessions[sessID]; s != nil {
				for _, tn := range s.cancels {
					named = tn.taskID == "task-1"
				}
			}
			agent.mu.Unlock()
			if named {
				break
			}
			if time.Now().After(deadline) {
				t.Error("turn never stashed the streamed task id")
				break
			}
			time.Sleep(time.Millisecond)
		}
		_, _ = fmt.Fprintln(pw, cancelLine)
		_ = pw.Close()
	}()

	var out, errBuf bytes.Buffer
	if err := Run(context.Background(), pr, &out, &errBuf, agent); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	select {
	case path := <-cancelled:
		if path != "/tasks/task-1:cancel" {
			t.Errorf("cancel path = %q, want /tasks/task-1:cancel", path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session/cancel did not reach the Memory CancelTask endpoint")
	}

	var promptResp map[string]any
	for _, m := range decodeLines(t, out.Bytes()) {
		if m["id"] == float64(3) {
			promptResp = m
		}
	}
	if promptResp == nil {
		t.Fatalf("no prompt response in %q", out.String())
	}
	pr2 := promptResp["result"].(map[string]any)
	if pr2["stopReason"] != StopReasonCancelled {
		t.Errorf("stopReason = %v, want cancelled", pr2["stopReason"])
	}
}

// TestConsumeStream_TaskIDClearedOnPause is the positive assertion for the
// "do not cancel a paused task" guard: it drives consumeStream itself and
// requires the tracker to report the task id while the turn runs and "" as soon
// as INPUT_REQUIRED arrives — while the stream is still open, so the clear cannot
// be confused with the end-of-stream cleanup. Without it the paused task id stays
// tracked, and a session/cancel racing the stream teardown parks the task in
// "cancelling" and strands the resume (issue #868 against #867).
//
// Asserting on the tracker at the pause, not on a 300 ms silence at the end of the
// turn, is what makes this bite: removing the clearTask in the INPUT_REQUIRED
// branch leaves the tracker silent after "task-1" and the test fails regardless of
// scheduling.
func TestConsumeStream_TaskIDClearedOnPause(t *testing.T) {
	release := make(chan struct{})
	paused := make(chan struct{})

	agent := streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvents(w, []a2a.StreamResponse{
			{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
			{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{
				State:   a2a.TaskStateInputRequired,
				Message: &a2a.Message{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.TextPart("Go on?")}},
			}}},
		})
		close(paused)
		// Hold the stream open: the test observes the tracker while it is still
		// live, so an end-of-stream cleanup cannot masquerade as the pause guard.
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})

	stream, err := agent.client.StreamMessage(context.Background(), a2a.SendMessageRequest{
		Message: a2a.Message{MessageID: "msg-1", Role: a2a.RoleUser, Parts: []a2a.Part{a2a.TextPart("hi")}},
	})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	tracked := make(chan string, 8)
	type result struct {
		pending string
		err     error
	}
	consumed := make(chan result, 1)
	go func() {
		emitter := &sessionEmitter{sid: "s1", send: func(any) error { return nil }}
		_, pending, _, err := agent.consumeStream(context.Background(), stream, emitter, func(id string) {
			tracked <- id
		})
		consumed <- result{pending: pending, err: err}
	}()

	<-paused

	next := func() (string, bool) {
		select {
		case id := <-tracked:
			return id, true
		case <-time.After(2 * time.Second):
			return "", false
		}
	}

	first, ok := next()
	if !ok {
		t.Fatal("the tracker was never told the task id: a cancel mid-turn would not reach the run")
	}
	if first != "task-1" {
		t.Errorf("first tracked id = %q, want task-1: the running turn must be cancellable", first)
	}
	second, ok := next()
	if !ok {
		t.Fatal("the pause did not clear the tracked task id before the stream ended: " +
			"a session/cancel would cancel the paused task and strand the resume")
	}
	if second != "" {
		t.Errorf("tracked id at the pause = %q, want \"\": the paused run has stopped", second)
	}

	close(release)

	var res result
	select {
	case res = <-consumed:
	case <-time.After(5 * time.Second):
		t.Fatal("consumeStream did not return after the stream closed")
	}
	if res.err != nil {
		t.Fatalf("consumeStream: %v", res.err)
	}
	if res.pending != "task-1" {
		t.Errorf("pendingTaskID = %q, want task-1 (the resume target)", res.pending)
	}
}

// TestCancelAfterInputRequiredDoesNotCancelPausedTask is the coarse end-to-end
// companion: it runs the whole loop and requires no :cancel request to arrive
// after the pause. It synchronises on the CLIENT having consumed INPUT_REQUIRED
// (the tracker cleared the task id) — not on the server having written it — so
// the test does not race the client's read of the pause frame and cannot fail
// under CPU contention for the wrong reason. The guard itself is pinned
// precisely by TestConsumeStream_TaskIDClearedOnPause.
func TestCancelAfterInputRequiredDoesNotCancelPausedTask(t *testing.T) {
	consumed := make(chan struct{})
	remoteCancels := make(chan string, 4)

	var agent *Agent
	// trackedTaskID reports the task id the in-flight turn is currently
	// cancellable for, under the agent's lock.
	trackedTaskID := func() (string, bool) {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		for _, sess := range agent.sessions {
			for _, tn := range sess.cancels {
				return tn.taskID, true
			}
		}
		return "", false
	}
	waitTracked := func(want string) {
		deadline := time.Now().Add(3 * time.Second)
		for {
			if got, ok := trackedTaskID(); ok && got == want {
				return
			}
			if time.Now().After(deadline) {
				got, ok := trackedTaskID()
				t.Errorf("tracked task id = %q (registered=%v), want %q: the cancel would race the pause", got, ok, want)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}

	agent = streamAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":cancel") {
			remoteCancels <- r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"task-1","status":{"state":"TASK_STATE_CANCELED"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		write := func(ev a2a.StreamResponse) {
			b, _ := json.Marshal(ev)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if fl != nil {
				fl.Flush()
			}
		}
		write(a2a.StreamResponse{Task: &a2a.Task{ID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}})
		// Hold the run open until the client has latched the task id: a cancel
		// before this is legitimately a no-op, and one after it but before the
		// pause frame would legitimately cancel the run.
		waitTracked("task-1")
		write(a2a.StreamResponse{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "task-1", ContextID: "ctx-1", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: &a2a.Message{Role: a2a.RoleAgent, Parts: []a2a.Part{a2a.TextPart("Go on?")}}}}})
		// Only now may the test send session/cancel: the client has consumed the
		// pause and cleared its tracked task id, which is the state under test.
		waitTracked("")
		close(consumed)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("stream not torn down by cancellation")
		}
	})

	sessID := mustNewSession(t, agent)
	promptLine := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":%q,"prompt":[{"type":"text","text":"hi"}]}}`, sessID)
	cancelLine := fmt.Sprintf(`{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":%q}}`, sessID)

	pr, pw := io.Pipe()
	go func() {
		_, _ = fmt.Fprintln(pw, promptLine)
		<-consumed
		_, _ = fmt.Fprintln(pw, cancelLine)
		_ = pw.Close()
	}()

	var out, errBuf bytes.Buffer
	if err := Run(context.Background(), pr, &out, &errBuf, agent); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	select {
	case path := <-remoteCancels:
		t.Fatalf("cancel of a paused task must not reach %q: it strands the resume", path)
	case <-time.After(300 * time.Millisecond):
	}
}
