package chat

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

// recordingChatWriter is the sink's SSE writer in the test. handle() holds the
// sink lock while writing, so this slice needs no lock of its own; with the
// lock removed both this append and the shared builder are races that -race
// reports (and the builder can corrupt its buffer outright).
type recordingChatWriter struct {
	events []any
}

func (w *recordingChatWriter) WriteData(data any) error {
	w.events = append(w.events, data)
	return nil
}

// TestChatStreamSink_ConcurrentToolFanOut pins issue #916. ADK runs the
// function calls of one model response in parallel goroutines
// (platform.RunTasks), so StreamCallback — and therefore the chat sink body —
// is entered concurrently. This drives the sink the way that fan-out does: N
// goroutines released on one barrier, each emitting a tool-call lifecycle
// around a text delta and a final A2UI frame, while also reading the
// accumulated state.
//
// It must be run with -race. With the sink's mutex removed it fails on the
// shared strings.Builder (data race and torn text) and on the surfaces slice;
// with the mutex it asserts the properties the fix relies on: every token
// appears exactly once, exactly one authoritative surface survives, and every
// event reaches the writer exactly once.
func TestChatStreamSink_ConcurrentToolFanOut(t *testing.T) {
	const calls = 32

	w := &recordingChatWriter{}
	sink := newChatStreamSink(w)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("call-%02d", i)
			token := fmt.Sprintf("<%02d>", i)
			surfaceID := "surface-" + id

			<-start // release every tool goroutine at once
			sink.handle(agents.StreamEvent{Type: agents.StreamEventToolCallStart, ID: id, Tool: "tool-" + id})
			sink.handle(agents.StreamEvent{Type: agents.StreamEventTextDelta, Text: token})
			// Concurrent readers: the handler reads after Execute returns, but a
			// read racing an in-flight delta must never see a torn buffer.
			_ = sink.text()
			_ = sink.finalSurfaces()
			sink.handle(agents.StreamEvent{
				Type:      agents.StreamEventA2UI,
				SurfaceID: surfaceID,
				A2UI:      []a2ui.Message{{DeleteSurface: &a2ui.DeleteSurface{SurfaceID: surfaceID}}},
				Final:     true,
			})
			sink.handle(agents.StreamEvent{Type: agents.StreamEventToolCallEnd, ID: id, Tool: "tool-" + id})
		}(i)
	}
	close(start)
	wg.Wait()

	got := sink.text()
	require.Len(t, got, calls*len("<00>"), "torn or lost text deltas: %q", got)
	for i := 0; i < calls; i++ {
		token := fmt.Sprintf("<%02d>", i)
		assert.Equal(t, 1, strings.Count(got, token),
			"token %q appears %d times in %q; concurrent deltas interleaved", token, strings.Count(got, token), got)
	}

	// Each final frame replaces the previous authoritative set, so exactly one
	// surface survives; a torn append would leave a different count.
	surfaces := sink.finalSurfaces()
	require.Len(t, surfaces, 1)
	require.NotNil(t, surfaces[0].DeleteSurface)

	// 2 tool events + 1 token + 1 A2UI event per goroutine.
	require.Len(t, w.events, calls*4)
}
