package agentcompat

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// TestTextDeltaCollector_ConcurrentToolFanOut pins issue #916 for the
// non-streaming OpenAI-compatible path. ADK runs the function calls of one
// model response in parallel goroutines (platform.RunTasks), so StreamCallback
// is entered concurrently even though executeSync/resumeSync read the
// accumulated text only after Execute/Resume returns. This drives the real
// callback body the way that fan-out does: N goroutines released on one
// barrier, each emitting a text delta, a non-text event, and a concurrent read.
//
// It must be run with -race. With the collector's mutex removed it fails on the
// shared strings.Builder (data race and torn text); with the mutex every token
// appears exactly once.
func TestTextDeltaCollector_ConcurrentToolFanOut(t *testing.T) {
	const deltas = 32

	var c textDeltaCollector

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < deltas; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := fmt.Sprintf("<%02d>", i)
			id := fmt.Sprintf("call-%02d", i)

			<-start // release every tool goroutine at once
			c.callback(agents.StreamEvent{Type: agents.StreamEventTextDelta, Text: token})
			// Only text deltas contribute; the interleaved callbacks must not.
			c.callback(agents.StreamEvent{Type: agents.StreamEventToolCallStart, ID: id, Tool: "tool-" + id})
			_ = c.text() // concurrent read must not observe a half-written buffer
		}(i)
	}
	close(start)
	wg.Wait()

	got := c.text()
	require.Len(t, got, deltas*len("<00>"), "torn or lost text deltas: %q", got)
	for i := 0; i < deltas; i++ {
		token := fmt.Sprintf("<%02d>", i)
		assert.Equal(t, 1, strings.Count(got, token),
			"token %q appears %d times in %q; concurrent deltas interleaved", token, strings.Count(got, token), got)
	}
}
