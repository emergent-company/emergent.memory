// Package governor provides admission control and load shedding for heavy
// endpoints, driven by system health. When the host is saturated the governor
// escalates through Normal -> Throttle -> Shed, shedding heavy (bulk) work early
// and pausing background workers, rather than adding capacity.
package governor

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
)

// Mode is the current operating mode of the governor.
type Mode int

const (
	// Normal means the system is healthy and full capacity is available.
	Normal Mode = iota
	// Throttle means the system is under pressure and background work should
	// not increase its load.
	Throttle
	// Shed means the system is saturated and heavy (bulk) work must be rejected.
	Shed
)

// String returns the lower-case name of the mode.
func (m Mode) String() string {
	switch m {
	case Normal:
		return "normal"
	case Throttle:
		return "throttle"
	case Shed:
		return "shed"
	default:
		return "unknown"
	}
}

// Class is the admission class of a request.
type Class int

const (
	// ClassInteractive is latency-sensitive, user-facing work that must not be
	// shed under load.
	ClassInteractive Class = iota
	// ClassBulk is heavy, batch work that may be shed under load.
	ClassBulk
)

// String returns the lower-case name of the admission class.
func (c Class) String() string {
	switch c {
	case ClassInteractive:
		return "interactive"
	case ClassBulk:
		return "bulk"
	default:
		return "unknown"
	}
}

// Sentinel errors returned by Admit.
var (
	// ErrShed is returned when the governor is in Shed mode and the request is
	// classified as bulk work.
	ErrShed = errors.New("governor: request shed")
	// ErrNoCapacity is returned when the per-project or global admission limit
	// (semaphore or rate limiter) is exhausted.
	ErrNoCapacity = errors.New("governor: no capacity")
)

// Config holds the governor's runtime configuration.
type Config struct {
	// Enabled toggles the governor. When false, Admit always allows and
	// Mode() always reports Normal.
	Enabled bool

	// ThrottleIOWaitPercent is the I/O wait percentage at or above which the
	// governor escalates to Throttle.
	ThrottleIOWaitPercent float64
	// ShedIOWaitPercent is the I/O wait percentage at or above which the
	// governor escalates to Shed.
	ShedIOWaitPercent float64
	// WarningCPULoadFactor is the load-average multiple of CPU cores at or
	// above which the governor escalates to Throttle.
	WarningCPULoadFactor float64
	// ShedCPULoadFactor is the load-average multiple of CPU cores at or above
	// which the governor escalates to Shed.
	ShedCPULoadFactor float64

	// SustainedSamples is the number of consecutive elevated samples required
	// before escalating to Shed.
	SustainedSamples int
	// RecoverSamples is the number of consecutive healthy samples required
	// before de-escalating back to Normal.
	RecoverSamples int

	// MaxInflightPerProject is the maximum number of concurrent admitted
	// requests per project.
	MaxInflightPerProject int
	// MaxInflightGlobal is the maximum number of concurrent admitted requests
	// across all projects.
	MaxInflightGlobal int
	// RatePerMinutePerProject is the sustained admission rate (requests per
	// minute) per project.
	RatePerMinutePerProject int
	// RateBurst is the burst size for the per-project rate limiter.
	RateBurst int

	// RetryAfterSeconds is the value sent in the Retry-After header on
	// rejected requests.
	RetryAfterSeconds int
}

// DefaultConfig returns a Config with sensible production defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:                 true,
		ThrottleIOWaitPercent:   30.0,
		ShedIOWaitPercent:       40.0,
		WarningCPULoadFactor:    2.0,
		ShedCPULoadFactor:       3.0,
		SustainedSamples:        2,
		RecoverSamples:          4,
		MaxInflightPerProject:   1,
		MaxInflightGlobal:       2,
		RatePerMinutePerProject: 30,
		RateBurst:               5,
		RetryAfterSeconds:       15,
	}
}

// projectSlot is a per-project admission semaphore with idle tracking.
type projectSlot struct {
	sem      chan struct{}
	lastUsed time.Time
}

// projectIdleTTL evicts a per-project slot that has been fully idle this long.
const projectIdleTTL = 5 * time.Minute

// Governor implements admission control and load shedding driven by system
// health.
type Governor struct {
	monitor syshealth.Monitor
	cfg     *Config
	log     *slog.Logger

	// mode state
	mu        sync.Mutex
	mode      Mode
	lastTS    time.Time
	sustained int // consecutive elevated (shed-eligible) samples
	recovered int // consecutive healthy samples
	cores     int

	// admission state
	globalSem chan struct{}

	projMu   sync.Mutex
	projects map[string]*projectSlot
	limiters map[string]*rate.Limiter
}

// New creates a Governor from a monitor, config, and logger. cfg may be nil,
// in which case DefaultConfig is used. A nil logger falls back to the default.
func New(monitor syshealth.Monitor, cfg *Config, log *slog.Logger) *Governor {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if log == nil {
		log = slog.Default()
	}
	// Clamp counters so a misconfigured value never disables hysteresis.
	if cfg.SustainedSamples < 1 {
		cfg.SustainedSamples = 1
	}
	if cfg.RecoverSamples < 1 {
		cfg.RecoverSamples = 1
	}
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}

	g := &Governor{
		monitor:  monitor,
		cfg:      cfg,
		log:      log.With(logger.Scope("governor")),
		mode:     Normal,
		cores:    cores,
		projects: make(map[string]*projectSlot),
		limiters: make(map[string]*rate.Limiter),
	}
	if cfg.Enabled && cfg.MaxInflightGlobal > 0 {
		g.globalSem = make(chan struct{}, cfg.MaxInflightGlobal)
	}
	return g
}

// Mode derives the current mode from the health monitor with hysteresis.
//
// Escalation: a single sample at/above the throttle thresholds moves Normal ->
// Throttle; SustainedSamples consecutive samples at/above the shed thresholds
// move to Shed. De-escalation back to Normal requires RecoverSamples consecutive
// healthy samples. Counter state only advances when a NEW sample arrives (the
// health Timestamp moves forward); stale health never causes shedding.
func (g *Governor) Mode() Mode {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.monitor == nil {
		return g.mode
	}
	health := g.monitor.GetHealth()
	if health == nil {
		return g.mode
	}

	// Only advance state when a new sample arrives.
	if !health.Timestamp.After(g.lastTS) {
		return g.mode
	}
	g.lastTS = health.Timestamp

	target := Normal
	if !health.Stale {
		switch {
		case health.IOWaitPercent >= g.cfg.ShedIOWaitPercent ||
			health.CPULoadAvg >= g.cfg.ShedCPULoadFactor*float64(g.cores):
			target = Shed
		case health.IOWaitPercent >= g.cfg.ThrottleIOWaitPercent ||
			health.CPULoadAvg >= g.cfg.WarningCPULoadFactor*float64(g.cores):
			target = Throttle
		}
	}

	g.apply(target)
	return g.mode
}

// apply folds a freshly-observed target mode into the governor state using the
// escalation/de-escalation hysteresis. Callers must hold g.mu.
func (g *Governor) apply(target Mode) {
	switch target {
	case Shed:
		g.recovered = 0
		if g.mode == Shed {
			return
		}
		g.sustained++
		if g.sustained >= g.cfg.SustainedSamples {
			g.setMode(Shed)
		}
	case Throttle:
		g.recovered = 0
		if g.mode == Shed {
			// Once shed, only healthy samples de-escalate (never straight back
			// to throttle).
			return
		}
		g.sustained = 0
		if g.mode == Normal {
			g.setMode(Throttle)
		}
	case Normal:
		g.sustained = 0
		g.recovered++
		if g.recovered >= g.cfg.RecoverSamples {
			g.setMode(Normal)
		}
	}
}

// setMode records a mode transition and publishes the gauge. Callers must hold
// g.mu.
func (g *Governor) setMode(m Mode) {
	if g.mode == m {
		return
	}
	g.mode = m
	governorModeGauge.Set(float64(m))
	g.log.Info("governor mode changed", slog.String("mode", m.String()))
}

// Admit performs a non-blocking admission check for the given project and
// class. On success it returns a release function that MUST be called when the
// work completes; the release is idempotent.
//
// ErrShed is returned when the governor is in Shed mode and class is
// ClassBulk. ErrNoCapacity is returned when the per-project or global
// semaphore, or the per-project rate limiter, is exhausted.
func (g *Governor) Admit(projectID string, class Class) (release func(), err error) {
	noop := func() {}
	if !g.cfg.Enabled {
		return noop, nil
	}

	if g.Mode() == Shed && class == ClassBulk {
		return nil, ErrShed
	}

	// Global inflight limit.
	if g.globalSem != nil {
		select {
		case g.globalSem <- struct{}{}:
		default:
			return nil, ErrNoCapacity
		}
	}

	// Per-project inflight limit.
	slot := g.acquireProjectSlot(projectID)
	if slot == nil {
		if g.globalSem != nil {
			<-g.globalSem
		}
		return nil, ErrNoCapacity
	}

	// Per-project rate limit.
	if g.cfg.RatePerMinutePerProject > 0 && !g.limiterFor(projectID).Allow() {
		g.releaseProjectSlot(slot)
		if g.globalSem != nil {
			<-g.globalSem
		}
		return nil, ErrNoCapacity
	}

	governorAdmissionInflight.WithLabelValues(class.String()).Inc()

	var once sync.Once
	release = func() {
		once.Do(func() {
			governorAdmissionInflight.WithLabelValues(class.String()).Dec()
			g.releaseProjectSlot(slot)
			if g.globalSem != nil {
				<-g.globalSem
			}
		})
	}
	return release, nil
}

// acquireProjectSlot non-blockingly acquires a per-project slot, creating the
// slot (and opportunistically evicting idle slots) as needed. Returns nil when
// the project's limit is exhausted.
func (g *Governor) acquireProjectSlot(projectID string) *projectSlot {
	g.projMu.Lock()
	defer g.projMu.Unlock()

	now := time.Now()
	for id, ps := range g.projects {
		if now.Sub(ps.lastUsed) > projectIdleTTL && len(ps.sem) == cap(ps.sem) {
			delete(g.projects, id)
		}
	}

	ps, ok := g.projects[projectID]
	if !ok {
		size := g.cfg.MaxInflightPerProject
		if size < 1 {
			size = 1
		}
		ps = &projectSlot{sem: make(chan struct{}, size)}
		g.projects[projectID] = ps
	}

	select {
	case ps.sem <- struct{}{}:
		ps.lastUsed = now
		return ps
	default:
		return nil
	}
}

// releaseProjectSlot returns a held per-project slot to the pool.
func (g *Governor) releaseProjectSlot(ps *projectSlot) {
	if ps == nil {
		return
	}
	select {
	case <-ps.sem:
	default:
	}
}

// limiterFor returns the per-project rate limiter, creating it on first use.
func (g *Governor) limiterFor(projectID string) *rate.Limiter {
	g.projMu.Lock()
	defer g.projMu.Unlock()
	if l, ok := g.limiters[projectID]; ok {
		return l
	}
	perSec := float64(g.cfg.RatePerMinutePerProject) / 60.0
	burst := g.cfg.RateBurst
	if burst < 1 {
		burst = 1
	}
	l := rate.NewLimiter(rate.Limit(perSec), burst)
	g.limiters[projectID] = l
	return l
}

// BackgroundPauser is implemented by components that can pause and resume
// background work. It is defined here (a neutral package) to avoid import
// cycles; the adapter that wires concrete workers lives in the extraction
// domain.
type BackgroundPauser interface {
	PauseAll()
	ResumeAll()
}

// RunModeWatcher polls Mode() at the given interval and invokes PauseAll /
// ResumeAll on mode transitions: entering Shed pauses background workers,
// leaving Shed resumes them. It blocks until ctx is cancelled.
func (g *Governor) RunModeWatcher(ctx context.Context, pauser BackgroundPauser, interval time.Duration) {
	if pauser == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	prev := g.Mode()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m := g.Mode()
			if m == prev {
				continue
			}
			g.log.Info("governor mode transition",
				slog.String("from", prev.String()),
				slog.String("to", m.String()))
			switch {
			case m == Shed:
				pauser.PauseAll()
			case prev == Shed:
				pauser.ResumeAll()
			}
			prev = m
		}
	}
}
