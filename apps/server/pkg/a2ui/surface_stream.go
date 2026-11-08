package a2ui

import (
	"encoding/json"
	"strings"
)

// SurfaceStream incrementally parses a streamed ```a2ui fenced block and yields
// each valid message as soon as its line is complete, so a card can be shown
// while the block is still streaming rather than only after the run finishes.
//
// It mirrors ExtractFromText's fence rules exactly: an opening line must be
// exactly ```a2ui and a closing line exactly ``` (surrounding whitespace
// ignored), and every non-empty line inside the fence is one JSON message. A
// line that is not valid JSON, or a message that fails Validate, marks the whole
// block invalid — its lines are still consumed (they must never leak into the
// answer text) but no further messages are yielded and Opened/Invalid report the
// terminal state so a surface already shown can be torn down.
//
// Feed is fed the same streamed text as TextFilter; it ignores ordinary text and
// acts only on fence content. One SurfaceStream tracks one block, matching how
// the executor keeps one TextFilter per step.
type SurfaceStream struct {
	pending string
	inFence bool
	invalid bool
	opened  bool // a createSurface line has been parsed (a surface may be shown)
	msgs    []Message
}

// NewSurfaceStream returns a stream with no pending input.
func NewSurfaceStream() *SurfaceStream { return &SurfaceStream{} }

// Feed consumes the next chunk of streamed answer text and returns the messages
// that became complete in this chunk (nil when nothing new is ready). Each
// message is returned exactly once, in block order. The returned slice is a
// copy; later Feed calls do not mutate it.
func (s *SurfaceStream) Feed(chunk string) []Message {
	s.pending += chunk
	before := len(s.msgs)
	for {
		idx := strings.IndexByte(s.pending, '\n')
		if idx < 0 {
			break
		}
		line := s.pending[:idx]
		s.pending = s.pending[idx+1:]
		s.consumeLine(line)
	}
	if len(s.msgs) == before {
		return nil
	}
	out := make([]Message, len(s.msgs)-before)
	copy(out, s.msgs[before:])
	return out
}

func (s *SurfaceStream) consumeLine(line string) {
	trimmed := strings.TrimSpace(line)
	if !s.inFence {
		if trimmed == fenceOpen {
			s.inFence = true
		}
		return
	}
	if trimmed == fenceClose {
		s.inFence = false
		return
	}
	if trimmed == "" || s.invalid {
		return
	}
	var m Message
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		s.invalid = true
		return
	}
	if err := Validate([]Message{m}); err != nil {
		s.invalid = true
		return
	}
	s.msgs = append(s.msgs, m)
	if m.CreateSurface != nil {
		s.opened = true
	}
}

// SurfaceID returns the surface identifier for the block parsed so far, derived
// by the same rule as SurfaceIDOf. It is "" before any message has parsed.
func (s *SurfaceStream) SurfaceID() string {
	if len(s.msgs) == 0 {
		return ""
	}
	return SurfaceIDOf(s.msgs)
}

// Messages returns the cumulative valid messages parsed so far, in block order.
// The returned slice is a copy; later Feed calls do not mutate it.
func (s *SurfaceStream) Messages() []Message {
	out := make([]Message, len(s.msgs))
	copy(out, s.msgs)
	return out
}

// Opened reports whether the block has parsed a createSurface line, i.e. a
// surface may already have been shown to the user. It stays true even after the
// block later turns invalid, so a caller can decide to tear the surface down.
func (s *SurfaceStream) Opened() bool { return s.opened }

// Invalid reports whether the block has been marked invalid (a line failed to
// parse or a message failed validation).
func (s *SurfaceStream) Invalid() bool { return s.invalid }

// InFence reports whether the stream is currently inside an unclosed fence.
func (s *SurfaceStream) InFence() bool { return s.inFence }
