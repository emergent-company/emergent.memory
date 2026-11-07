package chat

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// A decision-bearing A2UI card answers the conversation's pending question by
// clicking an option, so the option itself is what the question records — not
// the framed prose the agent reads (#223). Everything that is not a decision
// (typed text, a card with no option payload) keeps the old behaviour: the
// turn's message is the answer.
func TestPendingQuestionAnswer(t *testing.T) {
	const framed = "The user took an action on UI surface \"s1\":\n{\"componentId\":\"c1\"}"

	tests := []struct {
		name string
		req  *StreamRequest
		msg  string
		want string
	}{
		{
			name: "typed text is the answer",
			req:  &StreamRequest{Message: "yes please"},
			msg:  "yes please",
			want: "yes please",
		},
		{
			name: "an option click answers with the option value",
			req: &StreamRequest{SurfaceAction: &SurfaceAction{
				SurfaceID: "s1",
				Action:    map[string]any{"componentId": "q-1", "response": "yes"},
			}},
			want: "yes",
		},
		{
			name: "a boolean response answers as its text form",
			req: &StreamRequest{SurfaceAction: &SurfaceAction{
				SurfaceID: "s1",
				Action:    map[string]any{"componentId": "q-1", "response": true},
			}},
			want: "true",
		},
		{
			name: "a numeric response answers as its text form",
			req: &StreamRequest{SurfaceAction: &SurfaceAction{
				SurfaceID: "s1",
				Action:    map[string]any{"componentId": "q-1", "response": float64(3)},
			}},
			want: "3",
		},
		{
			name: "an action with no component id is not a decision",
			req: &StreamRequest{SurfaceAction: &SurfaceAction{
				SurfaceID: "s1",
				Action:    map[string]any{"response": "yes"},
			}},
			want: framed,
		},
		{
			name: "an empty component id is not a decision",
			req: &StreamRequest{SurfaceAction: &SurfaceAction{
				SurfaceID: "s1",
				Action:    map[string]any{"componentId": "  ", "response": "yes"},
			}},
			want: framed,
		},
		{
			name: "an action with no response is not a decision",
			req: &StreamRequest{SurfaceAction: &SurfaceAction{
				SurfaceID: "s1",
				Action:    map[string]any{"componentId": "c1"},
			}},
			want: framed,
		},
		{
			name: "a structured response is not an answer value",
			req: &StreamRequest{SurfaceAction: &SurfaceAction{
				SurfaceID: "s1",
				Action:    map[string]any{"componentId": "c1", "response": map[string]any{"a": 1}},
			}},
			want: framed,
		},
		{
			name: "an empty surface action falls back to the message",
			req:  &StreamRequest{SurfaceAction: &SurfaceAction{SurfaceID: "s1"}},
			want: framed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.msg
			if msg == "" {
				msg = framed
			}
			if got := pendingQuestionAnswer(tt.req, msg); got != tt.want {
				t.Errorf("pendingQuestionAnswer() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The hard boundary from the surface-action contract: an A2UI click must never
// decide a tool-confirmation pause — that decision belongs to the approval card
// that carries the question id. An ask_user pause stays answerable.
func TestPauseAnswerableByChat(t *testing.T) {
	tests := []struct {
		name string
		sig  *agents.SuspendSignal
		want bool
	}{
		{
			name: "a tool confirmation is decided by its own card",
			sig:  &agents.SuspendSignal{Reason: agents.SuspendReasonAwaitingToolConfirm},
			want: false,
		},
		{
			name: "an ask_user question is answerable",
			sig:  &agents.SuspendSignal{Reason: agents.SuspendReasonAwaitingHuman},
			want: true,
		},
		{
			name: "no suspend signal leaves the run answerable",
			sig:  nil,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pauseAnswerableByChat(tt.sig); got != tt.want {
				t.Errorf("pauseAnswerableByChat() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Symmetric coverage for the two pause kinds, end to end through the two helpers
// the handler composes: a decision-shaped action answers an ask_user pause with
// the option that was clicked, and cannot claim a tool-confirmation pause at all
// (that pause keeps its own approval card, and the click falls through to an
// ordinary turn).
func TestSurfaceActionAgainstAPausedRun(t *testing.T) {
	click := &StreamRequest{SurfaceAction: &SurfaceAction{
		SurfaceID: "surf-1",
		Action:    map[string]any{"componentId": "question-1", "response": "webpage"},
	}}
	const framed = "The user took an action on UI surface \"surf-1\":\n{\"componentId\":\"question-1\",\"response\":\"webpage\"}"

	t.Run("ask_user records the clicked option", func(t *testing.T) {
		run := &agents.AgentRun{SuspendContext: agents.SuspendSignal{
			Reason:     agents.SuspendReasonAwaitingHuman,
			QuestionID: "q-1",
		}.ToMap()}
		if !pauseAnswerableByChat(agents.SuspendSignalFromMap(run.SuspendContext)) {
			t.Fatal("an ask_user pause must stay answerable")
		}
		if got := pendingQuestionAnswer(click, framed); got != "webpage" {
			t.Errorf("answer = %q, want the clicked option", got)
		}
	})

	t.Run("a tool confirmation is never decided", func(t *testing.T) {
		run := &agents.AgentRun{SuspendContext: agents.SuspendSignal{
			Reason:     agents.SuspendReasonAwaitingToolConfirm,
			QuestionID: "q-2",
		}.ToMap()}
		if pauseAnswerableByChat(agents.SuspendSignalFromMap(run.SuspendContext)) {
			t.Fatal("a surface action must never decide a tool confirmation")
		}
	})
}
