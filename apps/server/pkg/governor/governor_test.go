package governor

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/syshealth"
)

// mockMonitor is a controllable syshealth.Monitor for tests.
type mockMonitor struct {
	mu sync.Mutex
	h  *syshealth.HealthMetrics
}

func (m *mockMonitor) Start() error { return nil }
func (m *mockMonitor) Stop() error  { return nil }
func (m *mockMonitor) GetHealth() *syshealth.HealthMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.h == nil {
		return &syshealth.HealthMetrics{}
	}
	c := *m.h
	return &c
}

func (m *mockMonitor) set(h syshealth.HealthMetrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.h = &h
}

func newTestGovernor(t *testing.T, cfg *Config) (*Governor, *mockMonitor) {
	t.Helper()
	if cfg == nil {
		cfg = DefaultConfig()
	}
	m := &mockMonitor{}
	g := New(m, cfg, nil)
	return g, m
}

// sample builds a health snapshot with the given io_wait and db-pool percentage
// and a timestamp distinct from prior ones.
func sample(ioWait, dbPool float64, ts time.Time) syshealth.HealthMetrics {
	return syshealth.HealthMetrics{
		IOWaitPercent: ioWait,
		DBPoolPercent: dbPool,
		Timestamp:     ts,
		Stale:         false,
	}
}

func TestGovernor_SafeStaysNormal(t *testing.T) {
	g, m := newTestGovernor(t, nil)
	base := time.Now()

	for i := 0; i < 10; i++ {
		m.set(sample(5.0, 0.0, base.Add(time.Duration(i)*time.Second)))
		assert.Equal(t, Normal, g.Mode(), "sample %d", i)
	}
}

// TestGovernor_ThrottleOnlyNeverShed pins the core fix: high host I/O/CPU
// pressure drives Throttle only, never Shed, no matter how many consecutive
// samples arrive.
func TestGovernor_ThrottleOnlyNeverShed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	g, m := newTestGovernor(t, cfg)
	base := time.Now()

	for i := 1; i <= 10; i++ {
		m.set(sample(80.0, 0.0, base.Add(time.Duration(i)*time.Second)))
		mode := g.Mode()
		require.Equal(t, Throttle, mode, "sample %d: high I/O wait must throttle, never shed", i)
	}
}

// TestGovernor_ShedOnAttributableCapacity pins the new shed trigger: DB pool
// utilization >= threshold AND in-flight bulk admissions >= bound, sustained.
func TestGovernor_ShedOnAttributableCapacity(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	g, m := newTestGovernor(t, cfg)
	g.bulkInflight.Store(1)
	base := time.Now()

	// First shed-eligible sample: not yet sustained.
	m.set(sample(5.0, 90.0, base.Add(1*time.Second)))
	assert.Equal(t, Normal, g.Mode(), "first shed sample should not escalate")

	// Second consecutive shed-eligible sample: escalate to Shed.
	m.set(sample(5.0, 90.0, base.Add(2*time.Second)))
	assert.Equal(t, Shed, g.Mode())
}

// TestGovernor_NoShedWithoutBulkInflight pins that high DB pool utilization
// alone (no in-flight bulk) never sheds.
func TestGovernor_NoShedWithoutBulkInflight(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	g, m := newTestGovernor(t, cfg)
	g.bulkInflight.Store(0)
	base := time.Now()

	for i := 1; i <= 10; i++ {
		m.set(sample(5.0, 99.0, base.Add(time.Duration(i)*time.Second)))
		mode := g.Mode()
		require.NotEqual(t, Shed, mode, "sample %d: pool pressure alone must not shed", i)
	}
}

func TestGovernor_RecoveryAfterShed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	cfg.RecoverSamples = 4
	g, m := newTestGovernor(t, cfg)
	g.bulkInflight.Store(1)
	base := time.Now()

	m.set(sample(5.0, 90.0, base.Add(1*time.Second)))
	_ = g.Mode()
	m.set(sample(5.0, 90.0, base.Add(2*time.Second)))
	assert.Equal(t, Shed, g.Mode())

	// De-escalation requires RecoverSamples consecutive healthy samples.
	for i := 1; i < cfg.RecoverSamples; i++ {
		m.set(sample(5.0, 0.0, base.Add(time.Duration(2+i)*time.Second)))
		assert.Equal(t, Shed, g.Mode(), "should stay shed before recovery count reached")
	}
	m.set(sample(5.0, 0.0, base.Add(time.Duration(2+cfg.RecoverSamples)*time.Second)))
	assert.Equal(t, Normal, g.Mode())
}

func TestGovernor_SameTimestampDoesNotAdvance(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	g, m := newTestGovernor(t, cfg)
	g.bulkInflight.Store(1)
	ts := time.Now()

	// A throttle sample establishes lastTS.
	m.set(sample(35.0, 0.0, ts))
	assert.Equal(t, Throttle, g.Mode())

	// Same timestamp but shed-eligible must NOT advance the counter, so the
	// governor stays at Throttle (not Shed).
	m.set(sample(5.0, 90.0, ts))
	assert.Equal(t, Throttle, g.Mode())
}

func TestGovernor_StaleHealthDoesNotShed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	g, m := newTestGovernor(t, cfg)
	g.bulkInflight.Store(1)
	base := time.Now()

	for i := 1; i <= 5; i++ {
		h := sample(5.0, 90.0, base.Add(time.Duration(i)*time.Second))
		h.Stale = true
		m.set(h)
		assert.Equal(t, Normal, g.Mode(), "stale health must never shed")
	}
}

func TestGovernor_AdmitShedsBulkNotInteractive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	g, m := newTestGovernor(t, cfg)
	g.bulkInflight.Store(1)
	base := time.Now()

	m.set(sample(5.0, 90.0, base.Add(1*time.Second)))
	_ = g.Mode()
	m.set(sample(5.0, 90.0, base.Add(2*time.Second)))
	require.Equal(t, Shed, g.Mode())

	// Bulk is shed.
	release, err := g.Admit("p1", ClassBulk)
	assert.Nil(t, release)
	assert.ErrorIs(t, err, ErrShed)

	// Interactive is NOT shed.
	release, err = g.Admit("p1", ClassInteractive)
	assert.NoError(t, err)
	require.NotNil(t, release)
	release()
}

func TestGovernor_AdmitNoCapacityPerProject(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxInflightPerProject = 1
	cfg.MaxInflightGlobalBulk = 10 // isolate the per-project limit from the global budget
	g, m := newTestGovernor(t, cfg)
	m.set(sample(5.0, 0.0, time.Now())) // healthy => Normal

	release1, err := g.Admit("p1", ClassBulk)
	require.NoError(t, err)
	require.NotNil(t, release1)

	// Second admission for the same project must fail with no capacity.
	release2, err := g.Admit("p1", ClassBulk)
	assert.Nil(t, release2)
	assert.ErrorIs(t, err, ErrNoCapacity)

	// A different project is unaffected.
	release3, err := g.Admit("p2", ClassBulk)
	assert.NoError(t, err)
	assert.NotNil(t, release3)
	release3()

	release1()
}

func TestGovernor_AdmitReleaseIdempotent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxInflightPerProject = 1
	g, m := newTestGovernor(t, cfg)
	m.set(sample(5.0, 0.0, time.Now()))

	release, err := g.Admit("p1", ClassBulk)
	require.NoError(t, err)
	require.NotNil(t, release)

	// Releasing twice must be safe (no deadlock / double free).
	release()
	release()

	// After release the slot is available again.
	release2, err := g.Admit("p1", ClassBulk)
	assert.NoError(t, err)
	assert.NotNil(t, release2)
	release2()
}

func TestGovernor_AdmitDisabledAlwaysAllows(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	g, m := newTestGovernor(t, cfg)
	m.set(sample(80.0, 0.0, time.Now()))

	release, err := g.Admit("p1", ClassBulk)
	assert.NoError(t, err)
	assert.NotNil(t, release)
	release()
}

// TestGovernor_AdmitWaitShedReturnsErrShed pins that AdmitWait also sheds bulk
// in Shed mode (immediately, before waiting).
func TestGovernor_AdmitWaitShedReturnsErrShed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	g, m := newTestGovernor(t, cfg)
	g.bulkInflight.Store(1)
	base := time.Now()

	m.set(sample(5.0, 90.0, base.Add(1*time.Second)))
	_ = g.Mode()
	m.set(sample(5.0, 90.0, base.Add(2*time.Second)))
	require.Equal(t, Shed, g.Mode())

	release, err := g.AdmitWait(t.Context(), "p1", ClassBulk, time.Second)
	assert.Nil(t, release)
	assert.ErrorIs(t, err, ErrShed)
}
