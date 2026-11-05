package agents

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewStreamSubagentEmitterMapsToStreamEvent pins the adapter from a
// SubagentEmitter update to the parent's StreamEvent: every lifecycle field is
// carried as a StreamEventSubagent, and concurrent emission (parallel spawns)
// is serialized into the callback.
func TestNewStreamSubagentEmitterMapsToStreamEvent(t *testing.T) {
	var mu sync.Mutex
	var got []StreamEvent
	emit := newStreamSubagentEmitter(func(ev StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, ev)
	})
	require.NotNil(t, emit)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			emit(SubagentUpdate{
				ID:          fmt.Sprintf("child-%d", i),
				ParentRunID: "parent",
				RunID:       fmt.Sprintf("child-%d", i),
				AgentName:   "research-agent",
				Task:        "do work",
				Status:      "running",
			})
		}(i)
	}
	wg.Wait()

	require.Len(t, got, 8)
	for _, ev := range got {
		require.Equal(t, StreamEventSubagent, ev.Type)
		require.Equal(t, "parent", ev.ParentRunID)
		require.Equal(t, "research-agent", ev.AgentName)
		require.Equal(t, "do work", ev.Task)
		require.Equal(t, "running", ev.Status)
		require.Equal(t, ev.SubagentID, ev.RunID)
	}
}

// TestNewStreamSubagentEmitterNilCallback asserts a run with no stream listener
// yields no emitter, so suspension/other paths do not panic on nil callbacks.
func TestNewStreamSubagentEmitterNilCallback(t *testing.T) {
	require.Nil(t, newStreamSubagentEmitter(nil))
}

// TestSubagentEmitterContextRoundTrip pins the context carrier used by
// trigger_agent to reach the parent run's stream.
func TestSubagentEmitterContextRoundTrip(t *testing.T) {
	require.Nil(t, SubagentEmitterFromContext(context.Background()))

	var got SubagentUpdate
	emit := func(u SubagentUpdate) { got = u }
	ctx := ContextWithSubagentEmitter(context.Background(), emit)
	SubagentEmitterFromContext(ctx)(SubagentUpdate{ID: "child", Status: "queued"})
	require.Equal(t, "child", got.ID)
	require.Equal(t, "queued", got.Status)
}
