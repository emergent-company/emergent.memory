// Package supervise implements the connector's engine restart policy: a
// bounded number of restarts within a sliding time window, a give-up state
// once that bound is exceeded, and suppression of restarts after a deliberate
// user stop.
//
// It is a pure, clock-injectable component so the macOS child supervisor can
// reuse it and unit-test it deterministically. On Linux the systemd unit owns
// restart, so the daemon does not use this policy.
package supervise

import (
	"math"
	"sync"
	"time"
)

// Action is the supervisor's next step after an engine exit.
type Action string

const (
	// ActionRestart means the engine should be started again after Delay.
	ActionRestart Action = "restart"
	// ActionGiveUp means the restart bound was exceeded; stop supervising.
	ActionGiveUp Action = "give_up"
	// ActionStop means a deliberate stop was requested; do not restart.
	ActionStop Action = "stop"
)

// Decision is the policy's verdict after NoteExit.
type Decision struct {
	Action  Action
	Delay   time.Duration // meaningful only when Action == ActionRestart
	Attempt int           // 1-based restart attempt number
}

// Default policy values. Zero-valued Options fields fall back to these.
const (
	DefaultMaxRestarts = 5
	DefaultWindow      = 10 * time.Minute
	DefaultBaseDelay   = time.Second
	DefaultMaxDelay    = 30 * time.Second
	DefaultFactor      = 2.0
	DefaultResetAfter  = 30 * time.Second
)

// Options configures a Policy. The zero value is usable: every field falls
// back to its default.
type Options struct {
	// MaxRestarts is the number of restarts permitted within Window.
	MaxRestarts int
	// Window is the sliding window over which restarts are counted.
	Window time.Duration
	// BaseDelay is the delay before the first restart.
	BaseDelay time.Duration
	// MaxDelay caps the exponential restart delay.
	MaxDelay time.Duration
	// Factor is the exponential growth factor applied per attempt.
	Factor float64
	// ResetAfter is the run uptime that counts as healthy and resets the
	// attempt counter and window on the next start.
	ResetAfter time.Duration
	// Clock is the time source; nil means time.Now.
	Clock func() time.Time
}

// Policy is a bounded restart policy. Its methods are safe for concurrent use.
type Policy struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	base     time.Duration
	maxDelay time.Duration
	factor   float64
	reset    time.Duration
	now      func() time.Time

	starts    []time.Time // restart timestamps within the window, ascending
	attempt   int         // consecutive restart attempts since the last reset
	lastStart time.Time   // start time of the current/last run
	stopped   bool
}

// New builds a Policy, filling zero-valued options with defaults.
func New(opts Options) *Policy {
	p := &Policy{
		max:      opts.MaxRestarts,
		window:   opts.Window,
		base:     opts.BaseDelay,
		maxDelay: opts.MaxDelay,
		factor:   opts.Factor,
		reset:    opts.ResetAfter,
		now:      opts.Clock,
	}
	if p.max <= 0 {
		p.max = DefaultMaxRestarts
	}
	if p.window <= 0 {
		p.window = DefaultWindow
	}
	if p.base <= 0 {
		p.base = DefaultBaseDelay
	}
	if p.maxDelay <= 0 {
		p.maxDelay = DefaultMaxDelay
	}
	if p.factor <= 0 {
		p.factor = DefaultFactor
	}
	if p.reset <= 0 {
		p.reset = DefaultResetAfter
	}
	if p.now == nil {
		p.now = time.Now
	}
	return p
}

// Default returns a Policy configured entirely from defaults.
func Default() *Policy { return New(Options{}) }

// NoteStart records that the engine (re)started. If the previous run lasted at
// least ResetAfter, the attempt counter and restart window are cleared first,
// so a healthy run earns a fresh budget.
func (p *Policy) NoteStart() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if !p.lastStart.IsZero() && now.Sub(p.lastStart) >= p.reset {
		p.starts = p.starts[:0]
		p.attempt = 0
	}
	p.lastStart = now
}

// NoteExit records an unexpected exit and returns the next step. After Stop it
// returns ActionStop; once MaxRestarts restarts have occurred within Window it
// returns ActionGiveUp; otherwise ActionRestart with the computed delay.
func (p *Policy) NoteExit() Decision {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopped {
		return Decision{Action: ActionStop}
	}

	now := p.now()
	p.pruneLocked(now)

	if len(p.starts) >= p.max {
		return Decision{Action: ActionGiveUp, Attempt: p.attempt}
	}

	p.attempt++
	p.starts = append(p.starts, now)
	return Decision{Action: ActionRestart, Delay: p.delayLocked(p.attempt), Attempt: p.attempt}
}

// Stop marks a deliberate user stop. Subsequent NoteExit calls return
// ActionStop and no further restarts are scheduled.
func (p *Policy) Stop() {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
}

// Stopped reports whether Stop has been called.
func (p *Policy) Stopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

// Attempts returns the current consecutive restart attempt count.
func (p *Policy) Attempts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.attempt
}

// Reset clears the attempt counter and restart window (for example after a
// successful manual restart) without changing the stopped state.
func (p *Policy) Reset() {
	p.mu.Lock()
	p.starts = p.starts[:0]
	p.attempt = 0
	p.mu.Unlock()
}

// pruneLocked drops restart timestamps older than the window. starts is kept
// in ascending order, so a single forward scan suffices.
func (p *Policy) pruneLocked(now time.Time) {
	cutoff := now.Add(-p.window)
	i := 0
	for i < len(p.starts) && p.starts[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		p.starts = append(p.starts[:0], p.starts[i:]...)
	}
}

// delayLocked returns the capped exponential delay for a 1-based attempt:
// base * factor^(attempt-1), capped at MaxDelay.
func (p *Policy) delayLocked(attempt int) time.Duration {
	d := float64(p.base) * math.Pow(p.factor, float64(attempt-1))
	if d > float64(p.maxDelay) {
		d = float64(p.maxDelay)
	}
	if d < 0 {
		return p.base
	}
	return time.Duration(d)
}
