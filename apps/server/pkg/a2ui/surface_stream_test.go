package a2ui

import "testing"

func TestSurfaceStream_ProgressiveParse(t *testing.T) {
	s := NewSurfaceStream()

	block := "```a2ui\n" +
		`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n" +
		`{"updateComponents":{"surfaceId":"s1","components":[{"id":"p1","component":"proposal","kind":"change","summary":"hi"}]}}` + "\n" +
		"```\n"

	got := s.Feed(block)
	if len(got) != 2 {
		t.Fatalf("Feed returned %d messages, want 2", len(got))
	}
	if got[0].CreateSurface == nil || got[0].CreateSurface.SurfaceID != "s1" {
		t.Fatalf("first message = %+v, want createSurface s1", got[0])
	}
	if got[1].UpdateComponents == nil {
		t.Fatalf("second message = %+v, want updateComponents", got[1])
	}
	if !s.Opened() {
		t.Fatal("Opened() = false, want true after createSurface")
	}
	if s.Invalid() || s.InFence() {
		t.Fatalf("stream state = invalid=%v inFence=%v, want false/false", s.Invalid(), s.InFence())
	}
	if s.SurfaceID() != "s1" {
		t.Fatalf("SurfaceID() = %q, want s1", s.SurfaceID())
	}
	if len(s.Messages()) != 2 {
		t.Fatalf("Messages() = %d, want 2 cumulative", len(s.Messages()))
	}
}

func TestSurfaceStream_ChunkBoundarySplitsLine(t *testing.T) {
	s := NewSurfaceStream()

	if got := s.Feed("```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\""); got != nil {
		t.Fatalf("partial line yielded %+v, want nil", got)
	}
	got := s.Feed(",\"catalogId\":\"basic\"}}\n```\n")
	if len(got) != 1 || got[0].CreateSurface == nil {
		t.Fatalf("completed line yielded %+v, want one createSurface", got)
	}
	if s.InFence() {
		t.Fatal("InFence() = true after the closing fence")
	}
}

func TestSurfaceStream_IgnoresOrdinaryText(t *testing.T) {
	s := NewSurfaceStream()
	if got := s.Feed("Here is a card:\n"); got != nil {
		t.Fatalf("ordinary text yielded %+v", got)
	}
	if got := s.Feed("```a2ui\n"); got != nil {
		t.Fatalf("opening fence yielded %+v", got)
	}
	if s.SurfaceID() != "" {
		t.Fatalf("SurfaceID() = %q before any message, want empty", s.SurfaceID())
	}
}

func TestSurfaceStream_InvalidLineMarksInvalid(t *testing.T) {
	s := NewSurfaceStream()
	s.Feed("```a2ui\n")
	s.Feed(`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n")

	if !s.Opened() {
		t.Fatal("Opened() = false before the invalid line")
	}

	s.Feed("not json\n")
	if !s.Invalid() {
		t.Fatal("Invalid() = false after a malformed line, want true")
	}
	// Subsequent valid lines are consumed but not yielded.
	if got := s.Feed(`{"updateComponents":{"surfaceId":"s1","components":[]}}` + "\n"); got != nil {
		t.Fatalf("line after invalid yielded %+v, want nil", got)
	}
	// The cumulative messages keep only the pre-invalid ones.
	if len(s.Messages()) != 1 {
		t.Fatalf("Messages() = %d, want only the createSurface", len(s.Messages()))
	}
}

func TestSurfaceStream_InvalidMessageFailsValidation(t *testing.T) {
	s := NewSurfaceStream()
	s.Feed("```a2ui\n")
	// An unknown component id fails catalog validation.
	s.Feed(`{"updateComponents":{"surfaceId":"s1","components":[{"id":"x","component":"bogus"}]}}` + "\n")
	if !s.Invalid() {
		t.Fatal("Invalid() = false for an unknown component, want true")
	}
}

func TestSurfaceStream_DefaultSurfaceID(t *testing.T) {
	s := NewSurfaceStream()
	s.Feed("```a2ui\n")
	s.Feed(`{"updateComponents":{"surfaceId":"s1","components":[]}}` + "\n")
	if s.SurfaceID() != DefaultSurfaceID {
		t.Fatalf("SurfaceID() = %q, want default %q", s.SurfaceID(), DefaultSurfaceID)
	}
}

func TestSurfaceStream_UnterminatedFence(t *testing.T) {
	s := NewSurfaceStream()
	s.Feed("```a2ui\n")
	s.Feed(`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n")
	if !s.InFence() {
		t.Fatal("InFence() = false while the block is unterminated")
	}
	if !s.Opened() {
		t.Fatal("Opened() = false, want true")
	}
}

func TestSurfaceStream_MessagesReturnsCopy(t *testing.T) {
	s := NewSurfaceStream()
	s.Feed("```a2ui\n")
	s.Feed(`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n")

	first := s.Messages()
	if len(first) != 1 {
		t.Fatalf("Messages() = %d, want 1", len(first))
	}
	s.Feed(`{"updateComponents":{"surfaceId":"s1","components":[]}}` + "\n")
	// The earlier snapshot must not have grown with the internal buffer.
	if len(first) != 1 {
		t.Fatalf("earlier Messages() snapshot grew to %d, want 1", len(first))
	}
}

func TestSurfaceStream_InvalidResetsAtNextFence(t *testing.T) {
	s := NewSurfaceStream()

	// Block 1: valid createSurface, then a malformed line → invalid.
	s.Feed("```a2ui\n")
	s.Feed(`{"createSurface":{"surfaceId":"s1","catalogId":"basic"}}` + "\n")
	s.Feed("not json\n```\n")
	if !s.Invalid() {
		t.Fatal("Invalid() = false after a malformed line, want true")
	}

	// Block 2 opens fresh: a valid line parses again, mirroring ExtractFromText's
	// per-block validity.
	got := s.Feed("```a2ui\n" + `{"updateComponents":{"surfaceId":"s2","components":[]}}` + "\n```\n")
	if len(got) != 1 || got[0].UpdateComponents == nil {
		t.Fatalf("valid block after an invalid one yielded %+v, want one updateComponents", got)
	}
}
