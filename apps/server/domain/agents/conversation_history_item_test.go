package agents

import (
	"encoding/json"
	"testing"
	"time"
)

// TestConversationHistoryItemToolCallWireShape locks the wire contract the
// session viewer and live chat consume: a tool-call record carries the tool id
// and execution duration under the keys the gateway mirrors.
func TestConversationHistoryItemToolCallWireShape(t *testing.T) {
	dur := 1250
	item := ConversationHistoryItem{
		Kind:       "tool_call",
		RunID:      "run-1",
		StepNumber: 2,
		CreatedAt:  time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC),
		ID:         "call-abc",
		ToolName:   "web_search",
		ToolStatus: "completed",
		DurationMs: &dur,
		ToolInput:  map[string]any{"q": "x"},
		ToolOutput: map[string]any{"n": 1},
	}

	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["id"] != "call-abc" {
		t.Errorf("id = %v, want call-abc", m["id"])
	}
	if m["duration_ms"] != float64(1250) {
		t.Errorf("duration_ms = %v, want 1250", m["duration_ms"])
	}
}

// TestConversationHistoryItemSystemRecord verifies a persisted composed agent
// instruction serialises as a message record with role "system" and its text,
// which is exactly what the transcript renderers key off.
func TestConversationHistoryItemSystemRecord(t *testing.T) {
	item := ConversationHistoryItem{
		Kind:      "message",
		RunID:     "run-1",
		CreatedAt: time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC),
		Role:      "system",
		Content:   map[string]any{"text": "You are a careful agent."},
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["role"] != "system" {
		t.Errorf("role = %v, want system", m["role"])
	}
	content, _ := m["content"].(map[string]any)
	if content["text"] != "You are a careful agent." {
		t.Errorf("content.text = %v", content["text"])
	}
}
