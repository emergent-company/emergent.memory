package supervise

import (
	"testing"
	"time"
)

// fakeClock returns a clock function and a pointer to advance it.
func fakeClock(start time.Time) (func() time.Time, *time.Time) {
	now := start
	return func() time.Time { return now }, &now
}

func testPolicy(clock func() time.Time, opts Options) *Policy {
	opts.Clock = clock
	return New(opts)
}

func TestRestartWithinBoundThenGiveUp(t *testing.T) {
	now, clk := fakeClock(time.Unix(0, 0))
	p := testPolicy(now, Options{
		MaxRestarts: 3,
		Window:      time.Minute,
		BaseDelay:   time.Second,
		MaxDelay:    10 * time.Second,
		Factor:      2,
		ResetAfter:  time.Hour, // disable healthy-reset for this test
	})
	_ = clk

	p.NoteStart()
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	for i, delay := range want {
		d := p.NoteExit()
		if d.Action != ActionRestart {
			t.Fatalf("exit %d: action = %q, want restart", i+1, d.Action)
		}
		if d.Delay != delay {
			t.Errorf("exit %d: delay = %s, want %s", i+1, d.Delay, delay)
		}
		if d.Attempt != i+1 {
			t.Errorf("exit %d: attempt = %d, want %d", i+1, d.Attempt, i+1)
		}
		p.NoteStart()
	}

	d := p.NoteExit()
	if d.Action != ActionGiveUp {
		t.Fatalf("after bound: action = %q, want give_up", d.Action)
	}
	// Give-up persists.
	p.NoteStart()
	if d := p.NoteExit(); d.Action != ActionGiveUp {
		t.Fatalf("give-up did not persist: action = %q", d.Action)
	}
}

func TestDelayIsCapped(t *testing.T) {
	now, _ := fakeClock(time.Unix(0, 0))
	p := testPolicy(now, Options{
		MaxRestarts: 10,
		Window:      time.Hour,
		BaseDelay:   time.Second,
		MaxDelay:    3 * time.Second,
		Factor:      2,
		ResetAfter:  time.Hour,
	})
	p.NoteStart()

	got := make([]time.Duration, 0, 4)
	for i := 0; i < 4; i++ {
		d := p.NoteExit()
		got = append(got, d.Delay)
		p.NoteStart()
	}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("delay %d = %s, want %s", i+1, got[i], want[i])
		}
	}
}

func TestDeliberateStopSuppressesRestart(t *testing.T) {
	now, _ := fakeClock(time.Unix(0, 0))
	p := testPolicy(now, Options{})

	if d := p.NoteExit(); d.Action != ActionRestart {
		t.Fatalf("pre-stop exit: action = %q, want restart", d.Action)
	}
	p.Stop()
	if !p.Stopped() {
		t.Fatal("Stopped() = false after Stop")
	}
	if d := p.NoteExit(); d.Action != ActionStop {
		t.Fatalf("post-stop exit: action = %q, want stop", d.Action)
	}
	// A start after a deliberate stop must not re-enable restarts.
	p.NoteStart()
	if d := p.NoteExit(); d.Action != ActionStop {
		t.Fatalf("post-stop restart did not stay stopped: action = %q", d.Action)
	}
}

func TestWindowExpiryKeepsRestarting(t *testing.T) {
	now, clk := fakeClock(time.Unix(0, 0))
	p := testPolicy(now, Options{
		MaxRestarts: 2,
		Window:      10 * time.Second,
		BaseDelay:   time.Second,
		MaxDelay:    time.Second,
		Factor:      1,
		ResetAfter:  time.Hour,
	})
	p.NoteStart()

	// Exits spaced beyond the window never accumulate, so the bound is never hit.
	for i := 0; i < 5; i++ {
		if d := p.NoteExit(); d.Action != ActionRestart {
			t.Fatalf("exit %d: action = %q, want restart (window should have pruned)", i+1, d.Action)
		}
		*clk = clk.Add(20 * time.Second)
		p.NoteStart()
	}
}

func TestHealthyRunResetsAttempts(t *testing.T) {
	now, clk := fakeClock(time.Unix(0, 0))
	p := testPolicy(now, Options{
		MaxRestarts: 3,
		Window:      time.Minute,
		BaseDelay:   time.Second,
		MaxDelay:    time.Minute,
		Factor:      2,
		ResetAfter:  30 * time.Second,
	})

	p.NoteStart()
	// Consume one attempt.
	if d := p.NoteExit(); d.Attempt != 1 {
		t.Fatalf("first exit attempt = %d, want 1", d.Attempt)
	}
	// A long, healthy run before the next start clears the budget.
	*clk = clk.Add(60 * time.Second)
	p.NoteStart()
	if got := p.Attempts(); got != 0 {
		t.Fatalf("Attempts() = %d after healthy run, want 0", got)
	}
	d := p.NoteExit()
	if d.Attempt != 1 || d.Delay != time.Second {
		t.Fatalf("after healthy run: attempt = %d delay = %s, want 1 / 1s", d.Attempt, d.Delay)
	}
}

func TestResetClearsWithoutUnstopping(t *testing.T) {
	now, _ := fakeClock(time.Unix(0, 0))
	p := testPolicy(now, Options{MaxRestarts: 5, Window: time.Hour, ResetAfter: time.Hour})
	p.NoteExit()
	p.NoteExit()
	if p.Attempts() != 2 {
		t.Fatalf("Attempts() = %d, want 2", p.Attempts())
	}
	p.Reset()
	if p.Attempts() != 0 {
		t.Fatalf("Attempts() = %d after Reset, want 0", p.Attempts())
	}
	if d := p.NoteExit(); d.Attempt != 1 {
		t.Fatalf("attempt after Reset = %d, want 1", d.Attempt)
	}
}

func TestZeroOptionsUseDefaults(t *testing.T) {
	now, _ := fakeClock(time.Unix(0, 0))
	p := testPolicy(now, Options{})
	if p.reset != DefaultResetAfter {
		t.Errorf("reset = %s, want default %s", p.reset, DefaultResetAfter)
	}
	if p.max != DefaultMaxRestarts {
		t.Errorf("max = %d, want default %d", p.max, DefaultMaxRestarts)
	}
	// Default budget: DefaultMaxRestarts restarts, then give up.
	p.NoteStart()
	for i := 0; i < DefaultMaxRestarts; i++ {
		if d := p.NoteExit(); d.Action != ActionRestart {
			t.Fatalf("restart %d: action = %q, want restart", i+1, d.Action)
		}
		p.NoteStart()
	}
	if d := p.NoteExit(); d.Action != ActionGiveUp {
		t.Fatalf("beyond default budget: action = %q, want give_up", d.Action)
	}
}

func TestDefaultConstructor(t *testing.T) {
	if p := Default(); p == nil {
		t.Fatal("Default() returned nil")
	}
}
