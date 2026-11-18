package agentcompat

import (
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/domain/agents"
)

// textDeltaCollector accumulates assistant text for the non-streaming
// OpenAI-compatible completions.
//
// ADK invokes StreamCallback from the goroutines that run the function calls of
// one model response (platform.RunTasks, one goroutine per call), so the
// callback body can be entered concurrently even though executeSync and
// resumeSync only read the accumulated text after Execute/Resume returns.
// strings.Builder is not safe for concurrent writes: without the lock two
// deltas can corrupt the buffer (or trip its copyCheck). text() takes the same
// lock so the final read cannot observe a half-written value.
type textDeltaCollector struct {
	mu  sync.Mutex
	buf strings.Builder
}

// callback is the StreamCallback body: it records text deltas and ignores every
// other event. Safe for concurrent use.
func (c *textDeltaCollector) callback(ev agents.StreamEvent) {
	if ev.Type != agents.StreamEventTextDelta {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.WriteString(ev.Text)
}

// text returns the text accumulated so far.
func (c *textDeltaCollector) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
