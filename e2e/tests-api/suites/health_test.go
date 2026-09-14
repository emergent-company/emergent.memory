package suites

import (
	"net/http"
	"testing"
)

// HealthTestSuite tests the health endpoints.
type HealthTestSuite struct {
	BaseSuite
}

func TestHealthSuite(t *testing.T) {
	RunSuite(t, new(HealthTestSuite))
}

// TestHealthEndpoint tests GET /health returns full health status.
func (s *HealthTestSuite) TestHealthEndpoint() {
	resp, err := s.Client.GET("/health")
	s.Require().NoError(err)

	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	// Check response structure
	s.Equal("healthy", body["status"])
	s.Contains(body, "timestamp")
	s.Contains(body, "uptime")
	s.Contains(body, "version")
	s.Contains(body, "checks")

	// Check database health check
	checks, ok := body["checks"].(map[string]any)
	s.True(ok, "checks should be a map")
	s.Contains(checks, "database")

	dbCheck, ok := checks["database"].(map[string]any)
	s.True(ok, "database check should be a map")
	s.Equal("healthy", dbCheck["status"])
}

// TestHealthzEndpoint tests GET /healthz returns simple OK.
func (s *HealthTestSuite) TestHealthzEndpoint() {
	resp, err := s.Client.GET("/healthz")
	s.Require().NoError(err)

	s.Equal(http.StatusOK, resp.StatusCode)
	s.Equal("OK", resp.BodyString())
}

// TestReadyEndpoint tests GET /ready returns readiness status.
func (s *HealthTestSuite) TestReadyEndpoint() {
	resp, err := s.Client.GET("/ready")
	s.Require().NoError(err)

	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal("ready", body["status"])
}

// TestDebugEndpoint tests GET /debug returns debug information.
func (s *HealthTestSuite) TestDebugEndpoint() {
	resp, err := s.Client.GET("/debug")
	s.Require().NoError(err)

	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	// Check response contains expected fields
	s.Contains(body, "environment")
	s.Contains(body, "go_version")
	s.Contains(body, "goroutines")
	s.Contains(body, "memory")
	s.Contains(body, "database")

	// Check database stats
	dbStats, ok := body["database"].(map[string]any)
	s.True(ok, "database should be a map")
	s.Contains(dbStats, "pool_total")
	s.Contains(dbStats, "pool_idle")
}

// TestHealthNoAuth tests that health endpoints don't require authentication.
func (s *HealthTestSuite) TestHealthNoAuth() {
	// Health endpoints should work without any auth headers
	endpoints := []string{"/health", "/healthz", "/ready", "/debug"}

	for _, endpoint := range endpoints {
		resp, err := s.Client.GET(endpoint)
		s.Require().NoError(err, "Request to %s failed", endpoint)

		s.NotEqual(http.StatusUnauthorized, resp.StatusCode, "Endpoint %s should not require auth", endpoint)
		s.NotEqual(http.StatusForbidden, resp.StatusCode, "Endpoint %s should not require auth", endpoint)
	}
}
