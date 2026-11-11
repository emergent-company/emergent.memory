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
//
// ns scopes every surfaceId the stream yields (see NamespaceMessages). The
// messages the model wrote are kept verbatim and scoped on read, so SurfaceID()
// and Messages() derive the identifier by one rule: SurfaceIDOf on the model's
// own messages, scoped to ns.
type SurfaceStream struct {
	pending string
	ns      string
	inFence bool
	invalid bool
	opened  bool // a createSurface line has been parsed (a surface may be shown)
	msgs    []Message
}

// NewSurfaceStream returns a stream with no pending input and no surface-id
// namespace.
func NewSurfaceStream() *SurfaceStream { return &SurfaceStream{} }

// NewNamespacedSurfaceStream returns a stream whose parsed messages carry every
// surfaceId scoped to ns. The executor scopes a surface to the run and step that
// emitted it, so two answers that both name their surface "s1" (the id the
// prompt teaches) reach a renderer as two distinct surfaces (issue #592).
func NewNamespacedSurfaceStream(ns string) *SurfaceStream { return &SurfaceStream{ns: ns} }

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
	return NamespaceMessages(s.ns, out)
}

func (s *SurfaceStream) consumeLine(line string) {
	trimmed := strings.TrimSpace(line)
	if !s.inFence {
		if trimmed == fenceOpen {
			s.inFence = true
			// A new block starts fresh: an invalid line in a previous block must
			// not poison the next one (mirrors ExtractFromText's per-block curValid).
			s.invalid = false
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

// SurfaceID returns the surface identifier for the block parsed so far, scoped
// to the stream's namespace, derived by the same rule as SurfaceIDOf. It is ""
// before any message has parsed.
func (s *SurfaceStream) SurfaceID() string {
	if len(s.msgs) == 0 {
		return ""
	}
	return NamespaceID(s.ns, SurfaceIDOf(s.msgs))
}

// Messages returns the cumulative valid messages parsed so far, in block order,
// with their surfaceIds scoped to the stream's namespace. The returned slice is
// a copy; later Feed calls do not mutate it.
func (s *SurfaceStream) Messages() []Message {
	out := make([]Message, len(s.msgs))
	copy(out, s.msgs)
	return NamespaceMessages(s.ns, out)
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
