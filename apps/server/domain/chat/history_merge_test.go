package chat

import (
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

func at(sec int) time.Time { return time.Unix(int64(sec), 0).UTC() }

func chatMessage(role, content string, sec int) Message {
	return Message{Role: role, Content: content, CreatedAt: at(sec)}
}

func historyMessage(runID, role, content string, sec int) *agents.ConversationHistoryItem {
	return &agents.ConversationHistoryItem{
		Kind:       "message",
		RunID:      runID,
		StepNumber: 1,
		Role:       role,
		Content:    map[string]any{"text": content},
		CreatedAt:  at(sec),
	}
}

func historyBoundary(runID, kind string, sec int) *agents.ConversationHistoryItem {
	return &agents.ConversationHistoryItem{
		Kind:      kind,
		RunID:     runID,
		CreatedAt: at(sec),
	}
}

// itemSeq renders the ordered timeline as "kind:role:text" strings so tests can
// assert both ordering and dedupe outcomes.
func itemSeq(items []*agents.ConversationHistoryItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it == nil {
			out = append(out, "<nil>")
			continue
		}
		out = append(out, it.Kind+":"+it.Role+":"+conversationItemText(it))
	}
	return out
}

func assertSeq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d\n got: %q\nwant: %q", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q\n got: %q\nwant: %q", i, got[i], want[i], got, want)
		}
	}
}

func TestMergeConversationUserMessages(t *testing.T) {
	augmented := conversationHistoryPrefix +
		"assistant: hi\n\n" +
		conversationCurrentMarker +
		"hello"

	tests := []struct {
		name     string
		runItems []*agents.ConversationHistoryItem
		msgs     []Message
		want     []string
	}{
		{
			name: "preserves run block ordering and splices earlier stored message first",
			runItems: []*agents.ConversationHistoryItem{
				historyBoundary("r1", "run_start", 10),
				historyMessage("r1", RoleUser, "hello", 11),
				historyMessage("r1", RoleAssistant, "hi there", 12),
				historyBoundary("r1", "run_end", 10),
			},
			msgs: []Message{
				chatMessage(RoleUser, "seeded", 5),
				chatMessage(RoleUser, "hello", 9), // duplicate of the run user turn
			},
			want: []string{
				"message:user:seeded",
				"run_start::",
				"message:user:hello",
				"message:assistant:hi there",
				"run_end::",
			},
		},
		{
			name: "splices orphan stored message between runs without hoisting run_end",
			runItems: []*agents.ConversationHistoryItem{
				historyBoundary("r1", "run_start", 10),
				historyMessage("r1", RoleUser, "q1", 11),
				historyBoundary("r1", "run_end", 10),
				historyBoundary("r2", "run_start", 30),
				historyMessage("r2", RoleUser, "q2", 31),
				historyBoundary("r2", "run_end", 30),
			},
			msgs: []Message{
				chatMessage(RoleUser, "q1", 9),
				chatMessage(RoleUser, "orphan", 20),
				chatMessage(RoleUser, "q2", 29),
			},
			want: []string{
				"run_start::",
				"message:user:q1",
				"run_end::",
				"message:user:orphan",
				"run_start::",
				"message:user:q2",
				"run_end::",
			},
		},
		{
			name: "dedupes history-augmented run user message against raw stored row",
			runItems: []*agents.ConversationHistoryItem{
				historyBoundary("r1", "run_start", 10),
				historyMessage("r1", RoleUser, augmented, 11),
				historyBoundary("r1", "run_end", 10),
			},
			msgs: []Message{
				chatMessage(RoleUser, "hello", 9),
			},
			want: []string{
				"run_start::",
				"message:user:" + augmented,
				"run_end::",
			},
		},
		{
			name: "keeps extra identical stored message with no run counterpart",
			runItems: []*agents.ConversationHistoryItem{
				historyBoundary("r1", "run_start", 10),
				historyMessage("r1", RoleUser, "hi", 11),
				historyBoundary("r1", "run_end", 10),
			},
			msgs: []Message{
				chatMessage(RoleUser, "hi", 5),
				chatMessage(RoleUser, "hi", 9),
			},
			want: []string{
				"message:user:hi", // surplus row survives; only one run counterpart
				"run_start::",
				"message:user:hi",
				"run_end::",
			},
		},
		{
			name: "ignores stored assistant rows",
			runItems: []*agents.ConversationHistoryItem{
				historyBoundary("r1", "run_start", 10),
				historyMessage("r1", RoleUser, "hello", 11),
				historyBoundary("r1", "run_end", 10),
			},
			msgs: []Message{
				chatMessage(RoleUser, "hello", 9),
				chatMessage(RoleAssistant, "already run-scoped", 12),
			},
			want: []string{
				"run_start::",
				"message:user:hello",
				"run_end::",
			},
		},
		{
			name: "no stored messages returns run items unchanged",
			runItems: []*agents.ConversationHistoryItem{
				historyBoundary("r1", "run_start", 10),
				historyBoundary("r1", "run_end", 10),
			},
			msgs: nil,
			want: []string{
				"run_start::",
				"run_end::",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeConversationUserMessages(tt.runItems, tt.msgs)
			assertSeq(t, itemSeq(got), tt.want)
		})
	}
}

func TestSynthesizeConversationMessageItems(t *testing.T) {
	msgs := []Message{
		chatMessage(RoleUser, "hello", 1),
		chatMessage(RoleAssistant, "hi", 2),
		chatMessage(RoleSystem, "note", 3),
	}

	items := synthesizeConversationMessageItems(msgs)
	assertSeq(t, itemSeq(items), []string{
		"message:user:hello",
		"message:assistant:hi",
		"message:system:note",
	})

	for i, it := range items {
		if it.RunID != "" {
			t.Errorf("[%d] RunID = %q, want empty for synthesized row", i, it.RunID)
		}
		if it.StepNumber != 0 {
			t.Errorf("[%d] StepNumber = %d, want 0 for synthesized row", i, it.StepNumber)
		}
		if !it.CreatedAt.Equal(msgs[i].CreatedAt) {
			t.Errorf("[%d] CreatedAt = %v, want %v", i, it.CreatedAt, msgs[i].CreatedAt)
		}
	}
}

func TestConversationRunUserText(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "raw first turn",
			content: "hello",
			want:    "hello",
		},
		{
			name: "history augmented turn",
			content: conversationHistoryPrefix +
				"user: earlier\n\n" +
				conversationCurrentMarker +
				"current",
			want: "current",
		},
		{
			name:    "marker without history prefix left untouched",
			content: conversationCurrentMarker + "not-augmented",
			want:    conversationCurrentMarker + "not-augmented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conversationRunUserText(tt.content); got != tt.want {
				t.Errorf("conversationRunUserText() = %q, want %q", got, tt.want)
			}
		})
	}
}
