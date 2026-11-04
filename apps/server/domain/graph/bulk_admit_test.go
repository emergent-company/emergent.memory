package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/governor"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
)

// routesTestMonitor drives a governor into Shed mode with a monotonically
// advancing timestamp and elevated DB pool utilization.
type routesTestMonitor struct{ n int }

func (m *routesTestMonitor) Start() error { return nil }
func (m *routesTestMonitor) Stop() error  { return nil }
func (m *routesTestMonitor) GetHealth() *syshealth.HealthMetrics {
	m.n++
	return &syshealth.HealthMetrics{
		DBPoolPercent: 95,
		Timestamp:     time.Now().Add(time.Duration(m.n) * time.Second),
	}
}

func shedGovernorForRoutes(t *testing.T) *governor.Governor {
	t.Helper()
	cfg := governor.DefaultConfig()
	cfg.SustainedSamples = 1
	cfg.RecoverSamples = 1
	g := governor.New(&routesTestMonitor{}, cfg, nil)

	// Register an in-flight bulk admission so attributable capacity is >= bound.
	release, err := g.AdmitWait(context.Background(), "proj", governor.ClassBulk, time.Second)
	require.NoError(t, err)
	t.Cleanup(release)

	require.Equal(t, governor.Shed, g.Mode())
	return g
}

// TestBulkAdmitWrapsWithGovernor proves that bulkAdmit (the wrapper applied to
// the relationships /bulk and /bulk-upsert routes) enforces bulk admission
// control: a shed governor rejects the request with 429 + Retry-After, and a
// disabled governor passes it through untouched.
func TestBulkAdmitWrapsWithGovernor(t *testing.T) {
	next := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }

	// Shed governor rejects bulk work.
	g := shedGovernorForRoutes(t)
	h := bulkAdmit(g, next)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/graph/relationships/bulk", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, h(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"shed"`)

	// Disabled governor passes through.
	disabled := governor.New(nil, &governor.Config{Enabled: false}, nil)
	h2 := bulkAdmit(disabled, next)
	e2 := echo.New()
	rec2 := httptest.NewRecorder()
	require.NoError(t, h2(e2.NewContext(httptest.NewRequest(http.MethodPost, "/api/graph/relationships/bulk", nil), rec2)))
	assert.Equal(t, http.StatusOK, rec2.Code)
}
