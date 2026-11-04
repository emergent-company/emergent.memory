package agents

import (
	"sync/atomic"
)

// WorkTerminatorState carries the run-finalizing terminator signal from the
// work_complete / work_block tools to the executor's beforeModelCb (which stops
// the model loop) and final-status detection (which reports the run outcome).
// It is shared across the parallel tool-call goroutines a single LLM turn may
// dispatch, so it is atomic.
type WorkTerminatorState struct {
	kind    atomic.Value // string: WorkTerminatorComplete | WorkTerminatorBlock
	summary atomic.Value // map[string]any
}

// Work terminator kinds.
const (
	WorkTerminatorComplete = "complete"
	WorkTerminatorBlock    = "block"
)

// Finalize records a terminator call. The first call wins (the run ends on the
// first terminator; later steps are ignored).
func (s *WorkTerminatorState) Finalize(kind string, summary map[string]any) {
	if s == nil {
		return
	}
	if s.kind.Load() != nil {
		return
	}
	s.kind.Store(kind)
	s.summary.Store(summary)
}

// Kind returns the recorded terminator kind, or empty when none was recorded.
func (s *WorkTerminatorState) Kind() string {
	if s == nil {
		return ""
	}
	if v := s.kind.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// ShouldFinalize reports whether a terminator has been recorded.
func (s *WorkTerminatorState) ShouldFinalize() bool {
	return s != nil && s.kind.Load() != nil
}

// Summary returns the terminator's run summary (includes the work_terminator
// marker), or nil when none was recorded.
func (s *WorkTerminatorState) Summary() map[string]any {
	if s == nil {
		return nil
	}
	if v := s.summary.Load(); v != nil {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return nil
}
