package agents

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

// These cover the seam the executor actually uses: partial answer text becomes a
// StreamEvent only through answerTextEvents, and the run's tail only through
// flushAnswerText, so the surface filter cannot be bypassed by a call site.

func TestAnswerTextEventsFiltersSurfaces(t *testing.T) {
	filter := a2ui.NewTextFilter()

	evs := answerTextEvents(filter, "Here you go:\n")
	if len(evs) != 1 || evs[0].Type != StreamEventTextDelta || evs[0].Text != "Here you go:\n" {
		t.Fatalf("plain chunk = %+v, want one text delta", evs)
	}

	// The fence marker and everything inside it produce no answer text.
	for _, chunk := range []string{
		"```a2ui\n",
		`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n",
		`{"updateComponents":{"surfaceId":"s1","components":[{"id":"p1","component":"proposal","kind":"change","summary":"hi"}]}}` + "\n",
		"```\n",
	} {
		if evs := answerTextEvents(filter, chunk); len(evs) != 0 {
			t.Fatalf("chunk %q produced %+v, want nothing", chunk, evs)
		}
	}

	evs = answerTextEvents(filter, "After.\n")
	if len(evs) != 1 || evs[0].Text != "After.\n" {
		t.Fatalf("text after the surface = %+v, want %q", evs, "After.\n")
	}
}

func TestFlushAnswerText(t *testing.T) {
	// A markdown fence's closing marker ends the message without a newline: it
	// is text and must be released, or the bubble loses it.
	closer := a2ui.NewTextFilter()
	answerTextEvents(closer, "done:\n")
	answerTextEvents(closer, "```")
	if evs := flushAnswerText(closer); len(evs) != 1 || evs[0].Text != "```" {
		t.Fatalf("flush of a markdown closer = %+v, want %q", evs, "```")
	}

	// An unterminated surface holds only the surface: nothing to show.
	open := a2ui.NewTextFilter()
	answerTextEvents(open, "done:\n```a2ui\n")
	answerTextEvents(open, `{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}`)
	if evs := flushAnswerText(open); len(evs) != 0 {
		t.Fatalf("flush of an unterminated surface = %+v, want nothing", evs)
	}

	// A bare marker that never received content is dropped too.
	bare := a2ui.NewTextFilter()
	answerTextEvents(bare, "done:\n```a2ui")
	if evs := flushAnswerText(bare); len(evs) != 0 {
		t.Fatalf("flush of a bare marker = %+v, want nothing", evs)
	}
}

func TestStripSurfaceText(t *testing.T) {
	valid := "```a2ui\n" +
		`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n```"

	text, surfaces := stripSurfaceText("Prose before.\n" + valid + "\nProse after.")
	if len(surfaces) != 1 || surfaces[0].CreateSurface == nil || surfaces[0].CreateSurface.SurfaceID != "s1" {
		t.Fatalf("surfaces = %+v, want the s1 createSurface", surfaces)
	}
	if strings.Contains(text, "```a2ui") || strings.Contains(text, "createSurface") {
		t.Fatalf("text still carries the block: %q", text)
	}
	if !strings.Contains(text, "Prose before.") || !strings.Contains(text, "Prose after.") {
		t.Fatalf("text lost the prose: %q", text)
	}

	// An invalid block is stripped even though it yields no surface.
	text, surfaces = stripSurfaceText("Answer.\n```a2ui\nnot json\n```")
	if len(surfaces) != 0 {
		t.Fatalf("invalid block produced surfaces: %+v", surfaces)
	}
	if strings.Contains(text, "not json") || strings.Contains(text, "```a2ui") {
		t.Fatalf("invalid block leaked into stored text: %q", text)
	}

	// No block: text is untouched.
	if text, surfaces := stripSurfaceText("plain answer"); text != "plain answer" || surfaces != nil {
		t.Fatalf("plain text changed: %q / %+v", text, surfaces)
	}
}
