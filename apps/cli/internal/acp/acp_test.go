package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
)

// newTestAgent builds an Agent backed by a canned A2A server that returns the
// given task/response for any message:send.
func newTestAgent(t *testing.T, handler http.HandlerFunc) *Agent {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := a2a.NewClientWithHTTP(srv.URL, "emt_test", srv.Client())
	return NewAgent(client, "research-agent", "test")
}

// runLines feeds the given input lines to the agent loop and returns the raw
// stdout bytes after stdin closes.
func runLines(t *testing.T, agent *Agent, input string) []byte {
	t.Helper()
	var out, errBuf bytes.Buffer
	err := Run(context.Background(), strings.NewReader(input), &out, &errBuf, agent)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	return out.Bytes()
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

func TestInitialize(t *testing.T) {
	agent := newTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request: %s %s", r.Method, r.URL.Path)
	})

	out := runLines(t, agent, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %s", len(msgs), out)
	}

	m := msgs[0]
	if m["id"] != float64(1) {
		t.Errorf("id = %v, want 1", m["id"])
	}
	result, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", m)
	}
	if result["protocolVersion"] != float64(ProtocolVersion) {
		t.Errorf("protocolVersion = %v, want %d", result["protocolVersion"], ProtocolVersion)
	}
	caps, ok := result["agentCapabilities"].(map[string]any)
	if !ok {
		t.Fatalf("missing agentCapabilities in %v", result)
	}
	if caps["loadSession"] != false {
		t.Errorf("loadSession = %v, want false", caps["loadSession"])
	}
	info, ok := result["agentInfo"].(map[string]any)
	if !ok {
		t.Fatalf("missing agentInfo in %v", result)
	}
	if info["name"] != "memory" {
		t.Errorf("agentInfo.name = %v, want memory", info["name"])
	}
}

func TestSessionPromptRoundTrip(t *testing.T) {
	agent := newTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message:send" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var body struct {
			Message a2a.Message `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if got := body.Message.Metadata[a2a.SkillIDMetadataKey]; got != "research-agent" {
			t.Errorf("skillId = %v, want research-agent", got)
		}
		if len(body.Message.Parts) != 1 || body.Message.Parts[0].Text == nil || *body.Message.Parts[0].Text != "hello" {
			t.Errorf("parts = %+v, want single text part 'hello'", body.Message.Parts)
		}

		reply := "hi there"
		w.Header().Set("Content-Type", "application/a2a+json")
		_ = json.NewEncoder(w).Encode(a2a.SendMessageResponse{
			Task: &a2a.Task{
				ID:        "task-1",
				ContextID: "ctx-1",
				Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []a2a.Artifact{{ArtifactID: "a1", Parts: []a2a.Part{a2a.TextPart(reply)}}},
			},
		})
	})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"S","prompt":[{"type":"text","text":"hello"}]}}`,
	}, "\n") + "\n"

	msgs := decodeLines(t, runLines(t, agent, input))

	// Expect: initialize response, session/new response, session/update
	// notification, session/prompt response — in that order.
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %s", len(msgs), runLines(t, agent, input))
	}
	// session/new response
	sn := msgs[1]
	snResult := sn["result"].(map[string]any)
	if _, ok := snResult["sessionId"].(string); !ok {
		t.Errorf("session/new result has no string sessionId: %v", snResult)
	}

	// session/update notification
	upd := msgs[2]
	if upd["method"] != "session/update" {
		t.Errorf("msgs[2].method = %v, want session/update", upd["method"])
	}
	if _, hasID := upd["id"]; hasID {
		t.Errorf("session/update notification should have no id: %v", upd)
	}
	params := upd["params"].(map[string]any)
	update := params["update"].(map[string]any)
	if update["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("update.sessionUpdate = %v", update["sessionUpdate"])
	}
	content := update["content"].(map[string]any)
	if content["text"] != "hi there" {
		t.Errorf("content.text = %v, want 'hi there'", content["text"])
	}

	// session/prompt response
	pr := msgs[3]
	prResult := pr["result"].(map[string]any)
	if prResult["stopReason"] != "end_turn" {
		t.Errorf("stopReason = %v, want end_turn", prResult["stopReason"])
	}
}

func TestPromptMissingAgentEnvErrorSurface(t *testing.T) {
	// The agent itself does not validate skill selection (the command does);
	// assert that an empty text prompt ends the turn without a backend call.
	agent := newTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
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
	prResult := msgs[2]["result"].(map[string]any)
	if prResult["stopReason"] != "end_turn" {
		t.Errorf("stopReason = %v, want end_turn", prResult["stopReason"])
	}
}

func TestUnknownMethodReturnsError(t *testing.T) {
	agent := newTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request")
	})
	out := runLines(t, agent, `{"jsonrpc":"2.0","id":9,"method":"nope","params":{}}`+"\n")
	msgs := decodeLines(t, out)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0]["error"] == nil {
		t.Errorf("expected error object, got %v", msgs[0])
	}
}
