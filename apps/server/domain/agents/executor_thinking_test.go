package agents

import (
	"testing"

	"google.golang.org/genai"
)

func TestThinkingTexts(t *testing.T) {
	tests := []struct {
		name          string
		parts         []*genai.Part
		wantOperator  []string
		wantReasoning []string
	}{
		{
			name: "separates operator and reasoning",
			parts: []*genai.Part{
				{Text: "Let me plan this."},
				{Text: "hidden chain-of-thought", Thought: true},
				{Text: "Then I'll check the schema."},
			},
			wantOperator:  []string{"Let me plan this.", "Then I'll check the schema."},
			wantReasoning: []string{"hidden chain-of-thought"},
		},
		{
			name: "skips nil and empty parts",
			parts: []*genai.Part{
				nil,
				{Text: ""},
				{Text: "only operator"},
			},
			wantOperator:  []string{"only operator"},
			wantReasoning: nil,
		},
		{
			name:          "nil parts",
			parts:         nil,
			wantOperator:  nil,
			wantReasoning: nil,
		},
		{
			name: "reasoning only",
			parts: []*genai.Part{
				{Text: "covert reasoning", Thought: true},
			},
			wantOperator:  nil,
			wantReasoning: []string{"covert reasoning"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operator, reasoning := thinkingTexts(tt.parts)
			assertStrings(t, operator, tt.wantOperator)
			assertStrings(t, reasoning, tt.wantReasoning)
		})
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFinalResponseStreamEvents(t *testing.T) {
	tests := []struct {
		name  string
		parts []*genai.Part
		want  []StreamEvent
	}{
		{
			name: "answer alongside reasoning: text deltas plus reasoning",
			parts: []*genai.Part{
				{Text: "chain of thought", Thought: true},
				{Text: "the answer"},
			},
			want: []StreamEvent{
				{Type: StreamEventThinking, Role: "reasoning", Text: "chain of thought"},
				{Type: StreamEventTextDelta, Text: "the answer"},
			},
		},
		{
			name: "reasoning only: Thought text becomes the answer",
			parts: []*genai.Part{
				{Text: "covert reasoning", Thought: true},
			},
			want: []StreamEvent{
				{Type: StreamEventTextDelta, Text: "covert reasoning"},
			},
		},
		{
			name: "skips nil and empty parts",
			parts: []*genai.Part{
				nil,
				{Text: ""},
				{Text: "answer"},
				{Text: "later thought", Thought: true},
			},
			want: []StreamEvent{
				{Type: StreamEventTextDelta, Text: "answer"},
				{Type: StreamEventThinking, Role: "reasoning", Text: "later thought"},
			},
		},
		{
			name:  "no parts",
			parts: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := finalResponseStreamEvents(tt.parts)
			if len(got) != len(tt.want) {
				t.Fatalf("length = %d, want %d (got %v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i].Type != tt.want[i].Type || got[i].Role != tt.want[i].Role || got[i].Text != tt.want[i].Text {
					t.Errorf("[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
