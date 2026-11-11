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
		`{"createSurface":{"surfaceId":"s1","catalogId":"memory-basic"}}` + "\n",
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
	answerTextEvents(open, `{"createSurface":{"surfaceId":"s1","catalogId":"memory-basic"}}`)
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

func TestReplyForStorage(t *testing.T) {
	valid := "```a2ui\n" +
		`{"createSurface":{"surfaceId":"s1","catalogId":"memory-basic"}}` + "\n```"

	// Prose plus a valid surface: prose kept, surface extracted.
	stored, surfaces, hasBlock := replyForStorage("Prose before.\n" + valid + "\nProse after.")
	if !hasBlock {
		t.Fatal("hasBlock = false, want true")
	}
	if len(surfaces) != 1 || surfaces[0].CreateSurface == nil || surfaces[0].CreateSurface.SurfaceID != "s1" {
		t.Fatalf("surfaces = %+v, want the s1 createSurface", surfaces)
	}
	if strings.Contains(stored, "```a2ui") || strings.Contains(stored, "createSurface") {
		t.Fatalf("stored text still carries the block: %q", stored)
	}
	if !strings.Contains(stored, "Prose before.") || !strings.Contains(stored, "Prose after.") {
		t.Fatalf("stored text lost the prose: %q", stored)
	}

	// A valid surface alone: nothing to store, but the surface is reported.
	if stored, surfaces, hasBlock := replyForStorage(valid); !hasBlock || stored != "" || len(surfaces) != 1 {
		t.Fatalf("valid-only: stored=%q surfaces=%d hasBlock=%v", stored, len(surfaces), hasBlock)
	}

	// An INVALID surface alone is the case that used to keep the raw block: it
	// must store empty text, still reporting that a block was present.
	for name, block := range map[string]string{
		"malformed":   "```a2ui\nnot json\n```",
		"truncated":   "```a2ui\n" + `{"createSurface":{"surfaceId":"s1"`,
		"unknown tag": "```a2ui\n" + `{"updateComponents":{"surfaceId":"s1","components":[{"id":"x","component":"bogus"}]}}` + "\n```",
	} {
		stored, surfaces, hasBlock := replyForStorage(block)
		if !hasBlock {
			t.Fatalf("%s: hasBlock = false, want true", name)
		}
		if stored != "" || len(surfaces) != 0 {
			t.Fatalf("%s: stored=%q surfaces=%+v, want empty", name, stored, surfaces)
		}
	}

	// A malformed block next to prose keeps the prose.
	if stored, _, hasBlock := replyForStorage("Answer.\n```a2ui\nnot json\n```"); !hasBlock || stored != "Answer." {
		t.Fatalf("prose+invalid: stored=%q hasBlock=%v", stored, hasBlock)
	}

	// No block: text untouched, nothing reported.
	if stored, surfaces, hasBlock := replyForStorage("plain answer"); hasBlock || stored != "plain answer" || surfaces != nil {
		t.Fatalf("plain text changed: %q / %+v / %v", stored, surfaces, hasBlock)
	}
}
