package agents

import (
	"errors"
	"strings"
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

// thinkingEventRecorder is a StreamCallback that captures thinking events for
// assertions.
func thinkingEventRecorder(events *[]StreamEvent) StreamCallback {
	return func(ev StreamEvent) {
		if ev.Type == StreamEventThinking {
			*events = append(*events, ev)
		}
	}
}

func TestThinkingTracker_StableIDsAcrossDeltas(t *testing.T) {
	var events []StreamEvent
	tr := newThinkingTracker(thinkingEventRecorder(&events))

	tr.emit(1, "reasoning", "first")
	tr.emit(1, "reasoning", "second")
	tr.emit(1, "reasoning", "third")

	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	for i, ev := range events {
		if ev.ID != "step-1-reasoning" {
			t.Errorf("[%d] ID = %q, want step-1-reasoning", i, ev.ID)
		}
		if ev.Done {
			t.Errorf("[%d] Done = true, want false (open delta)", i)
		}
		if ev.Role != "reasoning" {
			t.Errorf("[%d] Role = %q, want reasoning", i, ev.Role)
		}
	}
}

func TestThinkingTracker_SingleCloseOnRoleChange(t *testing.T) {
	var events []StreamEvent
	tr := newThinkingTracker(thinkingEventRecorder(&events))

	tr.emit(1, "operator", "plan")
	tr.emit(1, "reasoning", "think")

	// expect: operator open (done:false), operator close (done:true), reasoning open (done:false)
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 (%+v)", len(events), events)
	}
	if events[0].ID != "step-1-operator" || events[0].Done {
		t.Errorf("events[0] = %+v, want open step-1-operator", events[0])
	}
	if events[1].ID != "step-1-operator" || !events[1].Done {
		t.Errorf("events[1] = %+v, want close step-1-operator", events[1])
	}
	if events[2].ID != "step-1-reasoning" || events[2].Done {
		t.Errorf("events[2] = %+v, want open step-1-reasoning", events[2])
	}
}

func TestThinkingTracker_SingleCloseOnStepAdvance(t *testing.T) {
	var events []StreamEvent
	tr := newThinkingTracker(thinkingEventRecorder(&events))

	tr.emit(1, "reasoning", "step one")
	tr.emit(2, "reasoning", "step two")

	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 (%+v)", len(events), events)
	}
	if events[1].ID != "step-1-reasoning" || !events[1].Done {
		t.Errorf("events[1] = %+v, want close step-1-reasoning", events[1])
	}
	if events[2].ID != "step-2-reasoning" || events[2].Done {
		t.Errorf("events[2] = %+v, want open step-2-reasoning", events[2])
	}
}

func TestThinkingTracker_CloseNoopWhenNothingOpen(t *testing.T) {
	var events []StreamEvent
	tr := newThinkingTracker(thinkingEventRecorder(&events))

	tr.close()
	tr.close()

	if len(events) != 0 {
		t.Fatalf("events = %d, want 0 (%+v)", len(events), events)
	}
}

func TestThinkingTracker_RunEndClosesOpenSegment(t *testing.T) {
	var events []StreamEvent
	tr := newThinkingTracker(thinkingEventRecorder(&events))

	tr.emit(1, "reasoning", "delta")
	tr.close()

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (%+v)", len(events), events)
	}
	if events[0].Done {
		t.Errorf("events[0] = %+v, want open delta", events[0])
	}
	if !events[1].Done || events[1].ID != "step-1-reasoning" {
		t.Errorf("events[1] = %+v, want close step-1-reasoning", events[1])
	}
	if events[1].Text != "" {
		t.Errorf("close event Text = %q, want empty", events[1].Text)
	}
}

func TestThinkingTracker_SuppressWholeBlockReasoningAfterPartials(t *testing.T) {
	// A reasoner streams partial reasoning deltas, then the whole non-partial
	// block for the same step supplies the reasoning again. The whole-block
	// reasoning path must be suppressed so the reasoning is not emitted twice.
	var events []StreamEvent
	tr := newThinkingTracker(thinkingEventRecorder(&events))

	// Step 1 partial reasoning deltas.
	step := 1
	tr.markReasoningPartial(step)
	tr.emit(step, "reasoning", "hidden")
	tr.emit(step, "reasoning", "chain")

	// Step 1 whole block: operator + reasoning. Mirror the executor's exact
	// suppression condition.
	operatorText := []string{"planning monologue"}
	reasoningText := []string{"hidden", "chain"}
	if len(operatorText) > 0 {
		tr.emit(step, "operator", strings.Join(operatorText, "\n"))
	}
	if len(reasoningText) > 0 && !tr.reasoningWasPartial(step) {
		tr.emit(step, "reasoning", strings.Join(reasoningText, "\n"))
	}

	// Expected sequence:
	//   reasoning open (hidden), reasoning open (chain),
	//   reasoning close (role change → operator), operator open.
	// The whole-block reasoning is suppressed, so no "hidden\nchain" delta.
	for _, ev := range events {
		if ev.Type == StreamEventThinking && ev.Role == "reasoning" && ev.Text == "hidden\nchain" {
			t.Fatalf("whole-block reasoning was not suppressed: %+v", ev)
		}
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4 (%+v)", len(events), events)
	}
	if events[2].ID != "step-1-reasoning" || !events[2].Done {
		t.Errorf("events[2] = %+v, want reasoning close before operator", events[2])
	}
	if events[3].ID != "step-1-operator" || events[3].Done {
		t.Errorf("events[3] = %+v, want operator open", events[3])
	}
}

func TestThinkingTracker_WholeBlockReasoningWithoutPartials(t *testing.T) {
	// Without partials, the whole-block reasoning must still be emitted once.
	var events []StreamEvent
	tr := newThinkingTracker(thinkingEventRecorder(&events))

	step := 2
	if !tr.reasoningWasPartial(step) {
		tr.emit(step, "reasoning", "whole thought")
	}

	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (%+v)", len(events), events)
	}
	if events[0].ID != "step-2-reasoning" || events[0].Done || events[0].Text != "whole thought" {
		t.Errorf("events[0] = %+v, want open step-2-reasoning 'whole thought'", events[0])
	}
}

func TestResolveToolStreamStatus(t *testing.T) {
	tests := []struct {
		name    string
		toolErr error
		carried string
		want    string
	}{
		{name: "normal completion", want: "completed"},
		{name: "real error", toolErr: errors.New("boom"), want: "error"},
		{name: "policy-blocked carried error", carried: "error", want: "error"},
		{name: "awaiting confirmation", carried: "awaiting_confirmation", want: "awaiting_confirmation"},
		{name: "real error overrides carried", toolErr: errors.New("boom"), carried: "awaiting_confirmation", want: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveToolStreamStatus(tt.toolErr, tt.carried); got != tt.want {
				t.Errorf("resolveToolStreamStatus(%v, %q) = %q, want %q", tt.toolErr, tt.carried, got, tt.want)
			}
		})
	}
}
