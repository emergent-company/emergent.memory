package agents

import (
	"strings"
	"testing"
)

// A surface action rides the public share chat turn too (#222). The share page
// streams to /api/share/agent/stream, so the framing has to happen on that
// request — with the same wording `/api/chat` uses, via A2UIActionMessage, so
// the agent cannot tell which host delivered the click.
func TestFrameShareTurn(t *testing.T) {
	t.Run("plain text is unchanged", func(t *testing.T) {
		got, err := frameShareTurn(&streamShareRequest{SessionID: "s1", Message: "  hello  "})
		if err != nil {
			t.Fatalf("frameShareTurn() error = %v", err)
		}
		if got != "hello" {
			t.Errorf("frameShareTurn() = %q, want %q", got, "hello")
		}
	})

	t.Run("surface action is framed with the shared wording", func(t *testing.T) {
		got, err := frameShareTurn(&streamShareRequest{
			SessionID: "s1",
			SurfaceAction: &A2UIAction{
				SurfaceID: " surf-1 ",
				Action:    map[string]any{"componentId": "c1", "response": "approve"},
			},
		})
		if err != nil {
			t.Fatalf("frameShareTurn() error = %v", err)
		}
		want := A2UIActionMessage(A2UIAction{
			SurfaceID: "surf-1",
			Action:    map[string]any{"componentId": "c1", "response": "approve"},
		}, "")
		if got != want {
			t.Errorf("frameShareTurn() = %q, want %q", got, want)
		}
	})

	t.Run("typed text is appended after the action", func(t *testing.T) {
		got, err := frameShareTurn(&streamShareRequest{
			SessionID:     "s1",
			Message:       "why?",
			SurfaceAction: &A2UIAction{SurfaceID: "surf-1", Action: map[string]any{"response": "reject"}},
		})
		if err != nil {
			t.Fatalf("frameShareTurn() error = %v", err)
		}
		if !strings.HasSuffix(got, "\n\nwhy?") {
			t.Errorf("frameShareTurn() = %q, want typed text appended", got)
		}
	})

	t.Run("a turn with no message and no action is rejected", func(t *testing.T) {
		if _, err := frameShareTurn(&streamShareRequest{SessionID: "s1"}); err == nil {
			t.Fatal("expected an error for an empty turn")
		}
	})

	t.Run("an empty surface id is rejected", func(t *testing.T) {
		_, err := frameShareTurn(&streamShareRequest{
			SessionID:     "s1",
			SurfaceAction: &A2UIAction{SurfaceID: "  ", Action: map[string]any{"response": "approve"}},
		})
		if err == nil {
			t.Fatal("expected an error for an empty surfaceId")
		}
	})

	t.Run("an empty action payload is rejected", func(t *testing.T) {
		for _, action := range []any{nil, map[string]any{}, "approve"} {
			_, err := frameShareTurn(&streamShareRequest{
				SessionID:     "s1",
				SurfaceAction: &A2UIAction{SurfaceID: "surf-1", Action: action},
			})
			if err == nil {
				t.Fatalf("expected an error for action %#v", action)
			}
		}
	})

	t.Run("a session is required", func(t *testing.T) {
		if _, err := frameShareTurn(&streamShareRequest{Message: "hi"}); err == nil {
			t.Fatal("expected an error for a missing sessionId")
		}
	})
}
