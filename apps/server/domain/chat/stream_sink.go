package chat

import (
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/pkg/a2ui"
	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// chatStreamWriter is the slice of *sse.Writer the sink needs. It exists so the
// concurrency test can drive the sink without an HTTP response.
type chatStreamWriter interface {
	WriteData(data any) error
}

// chatStreamSink serialises the chat StreamCallback body.
//
// ADK runs the function calls of one model response in parallel goroutines
// (platform.RunTasks, one goroutine per call), so StreamCallback can be entered
// concurrently even though the handler reads the accumulated state only after
// Execute/Resume returns. The callback keeps the turn's streamed text in a
// strings.Builder and its authoritative A2UI surfaces in a slice, neither of
// which is safe for concurrent use; without the lock the builder can corrupt
// its buffer (or trip its copyCheck) and appends to the surface slice can lose
// frames.
//
// The mutex makes one event atomic: accumulate, then write, in event order.
// sse.Writer already serialises its own writes, so a single lock around the
// whole body is enough — no separate writer lock is needed, and holding one
// lock here also keeps a token event and the text it contributes from
// interleaving with another goroutine's pair.
type chatStreamSink struct {
	mu           sync.Mutex
	w            chatStreamWriter
	fullResponse strings.Builder
	surfaces     []a2ui.Message
}

// newChatStreamSink builds a sink that streams events through w.
func newChatStreamSink(w chatStreamWriter) *chatStreamSink {
	return &chatStreamSink{w: w}
}

// handle maps one executor stream event to its SSE event and accumulated state.
// Safe for concurrent use.
func (s *chatStreamSink) handle(event agents.StreamEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch event.Type {
	case agents.StreamEventTextDelta:
		s.fullResponse.WriteString(event.Text)
		_ = s.w.WriteData(sse.NewTokenEvent(event.Text))
	case agents.StreamEventThinking:
		_ = s.w.WriteData(sse.NewThinkingEvent(event.ID, event.Role, event.Text, event.Done))
	case agents.StreamEventToolCallStart:
		_ = s.w.WriteData(sse.NewMCPToolEventWithID(event.Tool, "started", event.ID, event.Input, ""))
	case agents.StreamEventToolCallEnd:
		status := event.Status
		if status == "" {
			status = "completed"
			if event.Error != "" {
				status = "error"
			}
		}
		_ = s.w.WriteData(sse.NewMCPToolEventWithID(event.Tool, status, event.ID, event.Output, event.Error))
	case agents.StreamEventError:
		_ = s.w.WriteData(sse.NewErrorEvent(event.Error))
	case agents.StreamEventToolApproval:
		_ = s.w.WriteData(sse.NewApprovalEvent(event.Tool, event.Input, event.QuestionID))
	case agents.StreamEventA2UI:
		// Persist/replay only the authoritative frame: progressive frames are
		// partial and a teardown deletes the surface, so appending them would
		// store a duplicated or already-deleted card (#427).
		s.surfaces = finalA2UISurfaces(s.surfaces, event)
		_ = s.w.WriteData(sse.NewUIEvent(event.SurfaceID, event.A2UI))
	case agents.StreamEventSubagent:
		_ = s.w.WriteData(sse.NewSubagentEvent(event.SubagentID, event.ParentRunID, event.RunID, event.AgentName, event.Task, event.Status, event.Error))
	}
}

// text returns the assistant text accumulated so far.
func (s *chatStreamSink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fullResponse.String()
}

// finalSurfaces returns a snapshot of the authoritative A2UI surfaces. The copy
// keeps the caller from retaining a slice the callback can replace underneath
// it.
func (s *chatStreamSink) finalSurfaces() []a2ui.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.surfaces) == 0 {
		return nil
	}
	out := make([]a2ui.Message, len(s.surfaces))
	copy(out, s.surfaces)
	return out
}
