// Package governor provides admission control and load shedding for heavy
// endpoints. Shedding is driven by attributable capacity — database pool
// utilization plus in-flight bulk admissions — while host I/O/CPU pressure only
// throttles (bounded backpressure) and pauses background workers, never sheds.
package governor

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
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
	// Shed means attributable capacity is exhausted and heavy (bulk) work must
	// be rejected.
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

// Sentinel errors returned by Admit / AdmitWait.
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
	// WarningCPULoadFactor is the load-average multiple of CPU cores at or
	// above which the governor escalates to Throttle.
	WarningCPULoadFactor float64

	// ShedDBPoolPercent is the DB pool utilization percentage at or above which
	// the governor considers attributable capacity exhausted (together with
	// ShedBulkInflightBound).
	ShedDBPoolPercent float64
	// ShedBulkInflightBound is the number of in-flight bulk admissions at or
	// above which, combined with ShedDBPoolPercent, the governor sheds.
	ShedBulkInflightBound int

	// SustainedSamples is the number of consecutive elevated samples required
	// before escalating to Shed.
	SustainedSamples int
	// RecoverSamples is the number of consecutive healthy samples required
	// before de-escalating back to Normal.
	RecoverSamples int

	// MaxInflightPerProject is the maximum number of concurrent admitted
	// requests per project.
	MaxInflightPerProject int
	// MaxInflightGlobalInteractive is the maximum number of concurrent admitted
	// interactive requests across all projects.
	MaxInflightGlobalInteractive int
	// MaxInflightGlobalBulk is the maximum number of concurrent admitted bulk
	// requests across all projects.
	MaxInflightGlobalBulk int
	// RatePerMinutePerProject is the sustained interactive admission rate
	// (requests per minute) per project.
	RatePerMinutePerProject int
	// RatePerMinutePerProjectBulk is the sustained bulk admission rate (requests
	// per minute) per project. 0 disables bulk rate limiting (bulk is governed
	// by inflight + backpressure instead).
	RatePerMinutePerProjectBulk int
	// RateBurst is the burst size for the per-project rate limiters.
	RateBurst int

	// BulkWaitBudget is the maximum time a bulk admission waits for capacity
	// under Throttle before failing with HTTP 429 + Retry-After.
	BulkWaitBudget time.Duration
	// MaxWaiters is the maximum number of concurrent bulk admissions waiting
	// for capacity. Excess waiters fail fast with ErrNoCapacity.
	MaxWaiters int

	// RetryAfterSeconds is the value sent in the Retry-After header on
	// rejected requests.
	RetryAfterSeconds int
}

// DefaultConfig returns a Config with sensible production defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:                      true,
		ThrottleIOWaitPercent:        30.0,
		WarningCPULoadFactor:         2.0,
		ShedDBPoolPercent:            90.0,
		ShedBulkInflightBound:        1,
		SustainedSamples:             2,
		RecoverSamples:               4,
		MaxInflightPerProject:        1,
		MaxInflightGlobalInteractive: 1,
		MaxInflightGlobalBulk:        1,
		RatePerMinutePerProject:      30,
		RatePerMinutePerProjectBulk:  0,
		RateBurst:                    5,
		BulkWaitBudget:               2 * time.Second,
		MaxWaiters:                   100,
		RetryAfterSeconds:            15,
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
// health and attributable capacity.
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

	// admission state — class-segregated global budgets so a bulk op never
	// starves interactive work, and vice versa.
	globalInteractiveSem chan struct{}
	globalBulkSem        chan struct{}
	// waiterSem bounds the number of concurrent bulk admissions waiting for
	// capacity.
	waiterSem chan struct{}
	// bulkInflight counts currently admitted bulk requests (attributable
	// capacity signal for shedding).
	bulkInflight atomic.Int64

	projMu      sync.Mutex
	projects    map[string]*projectSlot
	limiters    map[string]*rate.Limiter
	bulkLimiter map[string]*rate.Limiter
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
		monitor:     monitor,
		cfg:         cfg,
		log:         log.With(logger.Scope("governor")),
		mode:        Normal,
		cores:       cores,
		projects:    make(map[string]*projectSlot),
		limiters:    make(map[string]*rate.Limiter),
		bulkLimiter: make(map[string]*rate.Limiter),
	}
	if cfg.Enabled {
		if cfg.MaxInflightGlobalInteractive > 0 {
			g.globalInteractiveSem = make(chan struct{}, cfg.MaxInflightGlobalInteractive)
		}
		if cfg.MaxInflightGlobalBulk > 0 {
			g.globalBulkSem = make(chan struct{}, cfg.MaxInflightGlobalBulk)
		}
		if cfg.MaxWaiters > 0 {
			g.waiterSem = make(chan struct{}, cfg.MaxWaiters)
		}
	}
	return g
}

// Mode derives the current mode from the health monitor with hysteresis.
//
// Shed is driven only by attributable capacity: DB pool utilization at/above
// the shed threshold AND at least ShedBulkInflightBound in-flight bulk
// admissions. Host I/O/CPU pressure drives Throttle only, never Shed.
//
// Escalation: SustainedSamples consecutive shed-eligible samples move to Shed;
// a single throttle-eligible sample moves Normal -> Throttle. De-escalation back
// to Normal requires RecoverSamples consecutive healthy samples. Counter state
// only advances when a NEW sample arrives (the health Timestamp moves forward);
// stale health never causes shedding.
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
		// Shed only on attributable capacity (DB pool + in-flight bulk).
		if health.DBPoolPercent >= g.cfg.ShedDBPoolPercent &&
			g.bulkInflight.Load() >= int64(g.cfg.ShedBulkInflightBound) {
			target = Shed
		} else if health.IOWaitPercent >= g.cfg.ThrottleIOWaitPercent ||
			health.CPULoadAvg >= g.cfg.WarningCPULoadFactor*float64(g.cores) {
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

// globalSemFor returns the class-segregated global semaphore (nil if unbounded).
func (g *Governor) globalSemFor(class Class) chan struct{} {
	if class == ClassBulk {
		return g.globalBulkSem
	}
	return g.globalInteractiveSem
}

// Admit performs a non-blocking admission check for the given project and
// class. On success it returns a release function that MUST be called when the
// work completes; the release is idempotent.
//
// Interactive admissions use the interactive global semaphore and the
// interactive per-project rate limiter. Bulk admissions use the bulk global
// semaphore and are rate-exempt (governed by inflight + backpressure).
//
// ErrShed is returned when the governor is in Shed mode and class is
// ClassBulk. ErrNoCapacity is returned when the per-project or global
// semaphore, or the interactive per-project rate limiter, is exhausted.
func (g *Governor) Admit(projectID string, class Class) (release func(), err error) {
	noop := func() {}
	if !g.cfg.Enabled {
		return noop, nil
	}

	if g.Mode() == Shed && class == ClassBulk {
		return nil, ErrShed
	}

	// Class-segregated global inflight limit.
	global := g.globalSemFor(class)
	if global != nil {
		select {
		case global <- struct{}{}:
		default:
			return nil, ErrNoCapacity
		}
	}

	// Per-project inflight limit.
	slot := g.acquireProjectSlot(projectID)
	if slot == nil {
		if global != nil {
			<-global
		}
		return nil, ErrNoCapacity
	}

	// Per-project rate limit (interactive only; bulk is rate-exempt here).
	if class == ClassInteractive && g.cfg.RatePerMinutePerProject > 0 && !g.limiterFor(projectID).Allow() {
		g.releaseProjectSlot(slot)
		if global != nil {
			<-global
		}
		return nil, ErrNoCapacity
	}

	governorAdmissionInflight.WithLabelValues(class.String()).Inc()

	var once sync.Once
	release = func() {
		once.Do(func() {
			governorAdmissionInflight.WithLabelValues(class.String()).Dec()
			g.releaseProjectSlot(slot)
			if global != nil {
				<-global
			}
		})
	}
	return release, nil
}

// AdmitWait performs a blocking bulk admission with bounded backpressure. It
// waits up to budget for a global bulk slot and a per-project slot before
// returning ErrNoCapacity (the middleware maps this to HTTP 429 + Retry-After).
// Interactive callers should use Admit instead.
//
// Order of acquisition: waiter slot -> global bulk semaphore -> per-project
// slot -> (optional) bulk rate limiter. Every failure path releases everything
// already acquired. The returned release is idempotent.
func (g *Governor) AdmitWait(ctx context.Context, projectID string, class Class, budget time.Duration) (release func(), err error) {
	noop := func() {}
	if !g.cfg.Enabled {
		return noop, nil
	}
	if class != ClassBulk {
		return g.Admit(projectID, class)
	}
	if g.Mode() == Shed {
		return nil, ErrShed
	}

	// 1. Bound the number of concurrent waiters (fail fast when full).
	if g.waiterSem != nil {
		select {
		case g.waiterSem <- struct{}{}:
		default:
			return nil, ErrNoCapacity
		}
	}

	var (
		globalHeld = false
		slotHeld   *projectSlot
		waiterHeld = g.waiterSem != nil
	)

	releaseAll := func() {
		if slotHeld != nil {
			g.releaseProjectSlot(slotHeld)
			slotHeld = nil
		}
		if globalHeld {
			<-g.globalBulkSem
			globalHeld = false
		}
		if waiterHeld {
			<-g.waiterSem
			waiterHeld = false
		}
	}

	// 2. Blocking acquire of the global bulk semaphore.
	if g.globalBulkSem != nil {
		select {
		case g.globalBulkSem <- struct{}{}:
			globalHeld = true
		case <-ctx.Done():
			releaseAll()
			return nil, ctx.Err()
		case <-time.After(budget):
			releaseAll()
			return nil, ErrNoCapacity
		}
	}

	// 3. Blocking acquire of the per-project slot.
	slot, err := g.acquireProjectSlotBlocking(ctx, projectID, budget)
	if err != nil {
		releaseAll()
		return nil, err
	}
	slotHeld = slot

	// 4. Optional bulk rate limiter (disabled by default).
	if g.cfg.RatePerMinutePerProjectBulk > 0 {
		if err := g.bulkLimiterFor(projectID).WaitN(ctx, 1); err != nil {
			releaseAll()
			return nil, err
		}
	}

	// 5. Register the in-flight bulk admission (attributable capacity signal).
	g.bulkInflight.Add(1)
	governorAdmissionInflight.WithLabelValues(class.String()).Inc()

	var once sync.Once
	release = func() {
		once.Do(func() {
			g.bulkInflight.Add(-1)
			governorAdmissionInflight.WithLabelValues(class.String()).Dec()
			releaseAll()
		})
	}
	return release, nil
}

// Snapshot is a point-in-time view of the governor's mode and attributable
// capacity, surfaced in the health endpoint.
type Snapshot struct {
	Mode          string
	DBPoolPercent float64
	BulkInflight  int64
}

// Snapshot returns the governor's current mode, DB pool utilization, and
// in-flight bulk admissions. Safe to call when the governor is disabled or the
// monitor is nil.
func (g *Governor) Snapshot() Snapshot {
	s := Snapshot{
		Mode:         g.Mode().String(),
		BulkInflight: g.bulkInflight.Load(),
	}
	if g.monitor != nil {
		if h := g.monitor.GetHealth(); h != nil {
			s.DBPoolPercent = h.DBPoolPercent
		}
	}
	return s
}

// BulkInflight returns the current number of in-flight bulk admissions.
func (g *Governor) BulkInflight() int64 {
	return g.bulkInflight.Load()
}

// acquireProjectSlot non-blockingly acquires a per-project slot, creating the
// slot (and opportunistically evicting idle slots) as needed. Returns nil when
// the project's limit is exhausted.
func (g *Governor) acquireProjectSlot(projectID string) *projectSlot {
	ps := g.getOrCreateProjectSlot(projectID)
	select {
	case ps.sem <- struct{}{}:
		g.touchProjectSlot(ps)
		return ps
	default:
		return nil
	}
}

// acquireProjectSlotBlocking acquires a per-project slot, waiting up to budget
// for capacity. Returns ctx.Err() if the context is cancelled or ErrNoCapacity
// if the budget elapses first.
func (g *Governor) acquireProjectSlotBlocking(ctx context.Context, projectID string, budget time.Duration) (*projectSlot, error) {
	ps := g.getOrCreateProjectSlot(projectID)
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case ps.sem <- struct{}{}:
		g.touchProjectSlot(ps)
		return ps, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrNoCapacity
	}
}

// getOrCreateProjectSlot returns the per-project slot, creating it (and
// opportunistically evicting idle slots) as needed. Callers must hold no lock.
func (g *Governor) getOrCreateProjectSlot(projectID string) *projectSlot {
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
	return ps
}

// touchProjectSlot records that the slot was recently used, for idle eviction.
func (g *Governor) touchProjectSlot(ps *projectSlot) {
	if ps == nil {
		return
	}
	g.projMu.Lock()
	defer g.projMu.Unlock()
	ps.lastUsed = time.Now()
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

// limiterFor returns the interactive per-project rate limiter, creating it on
// first use.
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

// bulkLimiterFor returns the bulk per-project rate limiter, creating it on
// first use.
func (g *Governor) bulkLimiterFor(projectID string) *rate.Limiter {
	g.projMu.Lock()
	defer g.projMu.Unlock()
	if l, ok := g.bulkLimiter[projectID]; ok {
		return l
	}
	perSec := float64(g.cfg.RatePerMinutePerProjectBulk) / 60.0
	burst := g.cfg.RateBurst
	if burst < 1 {
		burst = 1
	}
	l := rate.NewLimiter(rate.Limit(perSec), burst)
	g.bulkLimiter[projectID] = l
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
// ResumeAll on mode transitions: entering Throttle or Shed pauses background
// workers, returning to Normal resumes them. It blocks until ctx is cancelled.
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
			case m != Normal && prev == Normal:
				pauser.PauseAll()
			case m == Normal && prev != Normal:
				pauser.ResumeAll()
			}
			prev = m
		}
	}
}
