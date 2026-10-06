package governor

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// admitKeyForTest mirrors resolveProjectID's fallback for a context with no
// authentication, so tests can address the same admission bucket the
// middleware used.
const admitKeyForTest = "unknown"

func TestMiddleware_ShedReturns429WithRetryAfter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SustainedSamples = 2
	cfg.RetryAfterSeconds = 15
	g, m := newTestGovernor(t, cfg)
	base := time.Now()

	m.set(sample(45.0, base.Add(1*time.Second)))
	_ = g.Mode()
	m.set(sample(45.0, base.Add(2*time.Second)))
	require.Equal(t, Shed, g.Mode())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/graph/objects/bulk-update", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := g.Middleware(ClassBulk)(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	err := h(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, strconv.Itoa(cfg.RetryAfterSeconds), rec.Header().Get("Retry-After"))
	// JSON body carries the apperror-shaped error envelope.
	assert.Contains(t, rec.Body.String(), `"code":"shed"`)
}

func TestMiddleware_NoCapacityReturns429(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxInflightPerProject = 1
	cfg.MaxInflightGlobal = 1
	g, m := newTestGovernor(t, cfg)
	m.set(sample(5.0, time.Now())) // healthy => Normal

	// Hold the single global slot out-of-band so the middleware's admission
	// attempt is rejected for lack of capacity.
	hold, err := g.Admit("held", ClassBulk)
	require.NoError(t, err)
	defer hold()

	e := echo.New()
	h := g.Middleware(ClassBulk)(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/graph/objects/bulk-update", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"no_capacity"`)
}

func TestMiddleware_PassesThroughWhenNormal(t *testing.T) {
	cfg := DefaultConfig()
	g, m := newTestGovernor(t, cfg)
	m.set(sample(5.0, time.Now()))

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/graph/objects/bulk-update", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := g.Middleware(ClassBulk)(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	require.NoError(t, h(c))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestMiddleware_ReleasesPermitOnHandlerError proves the admission permit is
// released when the wrapped handler returns an error, so a failed request can
// never permanently wedge the endpoint.
func TestMiddleware_ReleasesPermitOnHandlerError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxInflightPerProject = 1
	cfg.MaxInflightGlobal = 1
	cfg.RatePerMinutePerProject = 0 // disable rate limiter for determinism
	g, m := newTestGovernor(t, cfg)
	m.set(sample(5.0, time.Now()))

	e := echo.New()
	h := g.Middleware(ClassBulk)(func(c echo.Context) error {
		return errors.New("boom")
	})
	req := httptest.NewRequest(http.MethodPost, "/api/graph/objects/bulk-update", nil)
	err := h(e.NewContext(req, httptest.NewRecorder()))
	require.Error(t, err)

	// The only slot must be free again despite the handler error.
	release, err := g.Admit(admitKeyForTest, ClassBulk)
	require.NoError(t, err, "permit leaked after handler error")
	require.NotNil(t, release)
	release()
}

// TestMiddleware_ReleasesPermitOnPanic proves the admission permit is released
// when the wrapped handler panics.
func TestMiddleware_ReleasesPermitOnPanic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxInflightPerProject = 1
	cfg.MaxInflightGlobal = 1
	cfg.RatePerMinutePerProject = 0
	g, m := newTestGovernor(t, cfg)
	m.set(sample(5.0, time.Now()))

	e := echo.New()
	h := g.Middleware(ClassBulk)(func(c echo.Context) error {
		panic("handler exploded")
	})
	req := httptest.NewRequest(http.MethodPost, "/api/graph/objects/bulk-update", nil)

	func() {
		defer func() { _ = recover() }()
		_ = h(e.NewContext(req, httptest.NewRecorder()))
	}()

	release, err := g.Admit(admitKeyForTest, ClassBulk)
	require.NoError(t, err, "permit leaked after handler panic")
	require.NotNil(t, release)
	release()
}

// TestAdmit_ConcurrentNeverExceedsBound hammers Admit from many goroutines and
// asserts the number of simultaneously-held global permits never exceeds the
// configured bound. This is the mutation target for the inflight limiter.
func TestAdmit_ConcurrentNeverExceedsBound(t *testing.T) {
	const (
		bound    = 3
		projects = 20
	)
	cfg := DefaultConfig()
	cfg.MaxInflightPerProject = 1
	cfg.MaxInflightGlobal = bound
	cfg.RatePerMinutePerProject = 0
	g, m := newTestGovernor(t, cfg)
	m.set(sample(5.0, time.Now()))

	var inflight, peak int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for p := 0; p < projects; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			<-start
			release, err := g.Admit("p"+strconv.Itoa(p), ClassBulk)
			if err != nil {
				assert.ErrorIs(t, err, ErrNoCapacity)
				return
			}
			cur := atomic.AddInt32(&inflight, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if cur <= old || atomic.CompareAndSwapInt32(&peak, old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&inflight, -1)
			release()
		}(p)
	}
	close(start)
	wg.Wait()

	assert.LessOrEqual(t, atomic.LoadInt32(&peak), int32(bound), "inflight exceeded global bound")
	assert.Greater(t, atomic.LoadInt32(&peak), int32(0), "no admission succeeded at all")
}

// TestAdmit_GlobalBoundEnforcedAndReleased exercises the global semaphore
// directly: the bound blocks a third project and is released correctly.
func TestAdmit_GlobalBoundEnforcedAndReleased(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxInflightPerProject = 10
	cfg.MaxInflightGlobal = 2
	cfg.RatePerMinutePerProject = 0
	g, m := newTestGovernor(t, cfg)
	m.set(sample(5.0, time.Now()))

	r1, err := g.Admit("p1", ClassBulk)
	require.NoError(t, err)
	r2, err := g.Admit("p2", ClassBulk)
	require.NoError(t, err)

	_, err = g.Admit("p3", ClassBulk)
	assert.ErrorIs(t, err, ErrNoCapacity, "global bound not enforced")

	r1() // free one global slot
	r3, err := g.Admit("p3", ClassBulk)
	require.NoError(t, err, "global slot not released")
	r3()
	r2()
}
