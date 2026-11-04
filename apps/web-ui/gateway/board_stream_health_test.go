package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// healthSwitchMemory is a fakeMemory whose agent list (and failure mode) the
// test can change while the stream goroutine is reading it (fakeMemory's fields
// are unsynchronised).
type healthSwitchMemory struct {
	*fakeMemory
	mu     sync.Mutex
	agents []ScheduledAgent
	err    error
}

func (m *healthSwitchMemory) ListScheduledAgents(context.Context) ([]ScheduledAgent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.agents, nil
}

func (m *healthSwitchMemory) setAgents(agents ...ScheduledAgent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents = agents
}

func (m *healthSwitchMemory) setErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// The health readout exists to warn about an agent that stopped doing its job,
// but an agent-health change publishes no event at all: WorkerPool.handleFailure
// only logs (worker_pool.go), and toggling an agent emits nothing. Refreshing the
// banner on the next unrelated work-item event is backwards — it must catch up by
// itself, because the situations it warns about are exactly the ones that produce
// no work item. The idle tick re-checks it (#2466).
func TestBoardStreamRefreshesAHealthChangeOnItsOwn(t *testing.T) {
	setBoardStreamTiming(t, 10*time.Millisecond, 40*time.Millisecond)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	f := &healthSwitchMemory{fakeMemory: boardStreamTestMemory(pr)}
	rec, stop := startBoardStream(t, f)
	defer stop()

	// Catch-up frame: every agent is healthy, so the readout is empty.
	waitForFrames(t, rec, "#board-health", 1, 2*time.Second)

	// A reaction agent gets auto-disabled (its failure count tripped the
	// circuit breaker). No upstream event accompanies it.
	f.setAgents(ScheduledAgent{
		ID:                  "a1",
		Name:                "worker",
		TriggerType:         "reaction",
		Enabled:             false,
		ConsecutiveFailures: 3,
	})

	waitForFrames(t, rec, "#board-health", 2, 3*time.Second)
	body := rec.body()
	if !strings.Contains(body, "worker · disabled") {
		t.Fatalf("the health frame does not name the disabled agent:\n%s", body)
	}
	// Only the readout moves: a health change must not re-morph the columns,
	// which would disturb a drag in progress for no reason.
	if got := frameCount(body, "#board"); got != 1 {
		t.Errorf("a health-only refresh re-sent the columns (%d #board frames, want 1)", got)
	}

	// And the re-check must not become a frame storm: several more idle ticks
	// with an unchanged readout send nothing. This is the property the whole
	// signature comparison exists for, and the tick is now the only thing that
	// can exercise it.
	time.Sleep(8 * 40 * time.Millisecond)
	if got := frameCount(rec.body(), "#board-health"); got != 2 {
		t.Errorf("unchanged readout re-sent over idle ticks (%d #board-health frames, want 2)", got)
	}
}

// A failed ListScheduledAgents means the readout is unknown, not healthy.
// boardUnhealthyAgents(nil) is empty, so rendering the error as an empty list
// sent a clean "#board-health" frame and cleared the warning banner while the
// scheduling service was unreachable — the exact state the banner exists to
// disclose. The last good readout must survive, and a sustained outage must not
// emit a frame per heartbeat either.
func TestBoardStreamKeepsLastHealthWhenListAgentsFails(t *testing.T) {
	setBoardStreamTiming(t, 10*time.Millisecond, 40*time.Millisecond)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	f := &healthSwitchMemory{fakeMemory: boardStreamTestMemory(pr)}
	f.setAgents(ScheduledAgent{
		ID:                  "a1",
		Name:                "worker",
		TriggerType:         "reaction",
		Enabled:             false,
		ConsecutiveFailures: 3,
	})
	rec, stop := startBoardStream(t, f)
	defer stop()

	// Catch-up renders the warning from a successful fetch.
	waitForFrames(t, rec, "#board-health", 1, 2*time.Second)
	if body := rec.body(); !strings.Contains(body, "worker · disabled") {
		t.Fatalf("catch-up readout does not name the disabled agent:\n%s", body)
	}

	// The scheduling service goes away. Count from the current heartbeat so the
	// assertion below covers ticks that are certainly post-failure.
	before := strings.Count(rec.body(), ": heartbeat")
	f.setErr(errors.New("memory 502 bad gateway"))
	waitForOccurrences(t, rec, ": heartbeat", before+2, 3*time.Second)
	// Several more failed fetches: a per-heartbeat frame would be obvious.
	time.Sleep(6 * 40 * time.Millisecond)

	body := rec.body()
	if got := frameCount(body, "#board-health"); got != 1 {
		t.Fatalf("a failed ListScheduledAgents emitted a health frame (%d frames, want the catch-up only); an empty frame clears the warning banner:\n%s", got, body)
	}
	// The last good warning is still what the client holds.
	if !strings.Contains(body, "worker · disabled") {
		t.Errorf("the warning banner was cleared from the readout while the fetch was failing:\n%s", body)
	}
}
