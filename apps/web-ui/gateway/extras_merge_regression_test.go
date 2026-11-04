package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHistoryNoDuplicateUserMessage is a regression test for the duplicated
// user turn in chat transcripts. Memory's history endpoint already merges the
// stored kb.chat_messages row into the run history and suppresses the duplicate
// stored copy, so the gateway must not re-merge the two sources: doing so
// emitted the same user message twice (their created times differ, so the old
// exact-nanosecond dedupe never matched).
func TestHistoryNoDuplicateUserMessage(t *testing.T) {
	f := &fakeMemory{
		histories: map[string]*ConversationHistory{
			"conv-dup": {
				ConversationID: "conv-dup",
				Items: []json.RawMessage{
					json.RawMessage(`{"kind":"message","run_id":"r1","step_number":1,"created_at":"2026-09-10T17:17:07.910Z","role":"user","content":{"text":"hello"}}`),
				},
			},
		},
		details: map[string]*ConversationDetail{
			"conv-dup": {
				ID: "conv-dup",
				Messages: []Message{
					{Role: "user", Content: "hello", CreatedAt: "2026-09-10T17:17:07.100Z"},
				},
			},
		},
	}
	_, e := newTestServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations/conv-dup/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var out struct {
		Items []struct {
			Role string `json:"role"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	user := 0
	for _, it := range out.Items {
		if it.Role == "user" {
			user++
		}
	}
	if user != 1 {
		t.Fatalf("want exactly 1 user message, got %d: %s", user, rec.Body.String())
	}
}
