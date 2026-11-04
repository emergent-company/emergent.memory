// Package api_test — health_test.go
//
// Tests for the health, readiness, and debug endpoints.
// Ported from emergent.memory/apps/server/tests/e2e/health_test.go
package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHealth_Endpoint(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /health")
	resp := doAPILogged(t, rl, "GET", "/health", "", "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	if result["status"] != "healthy" {
		t.Errorf("expected status=healthy, got %v", result["status"])
	}
	for _, field := range []string{"timestamp", "uptime", "version", "checks"} {
		if _, ok := result[field]; !ok {
			t.Errorf("missing field %q in /health response", field)
		}
	}
	checks, ok := result["checks"].(map[string]any)
	if !ok {
		t.Fatal("checks field is not an object")
	}
	dbCheck, ok := checks["database"].(map[string]any)
	if !ok {
		t.Fatal("checks.database field is not an object")
	}
	if dbCheck["status"] != "healthy" {
		t.Errorf("expected database check status=healthy, got %v", dbCheck["status"])
	}
	rl.Printf("/health returned healthy")
}

func TestHealth_Healthz(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /healthz")
	resp := doAPILogged(t, rl, "GET", "/healthz", "", "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	if body != "OK" {
		t.Errorf("expected body=OK, got %q", body)
	}
	rl.Printf("/healthz returned OK")
}

func TestHealth_Ready(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /ready")
	resp := doAPILogged(t, rl, "GET", "/ready", "", "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["status"] != "ready" {
		t.Errorf("expected status=ready, got %v", result["status"])
	}
	rl.Printf("/ready returned ready")
}

func TestHealth_Debug(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /debug requires superadmin_full")

	// Unauthenticated → 401.
	resp := doAPILogged(t, rl, "GET", "/debug", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)

	// Authenticated non-superadmin → 403. e2e-test-user is confirmed
	// non-superadmin (see TestSuperadmin_GetMe_ReturnsNullForRegularUser).
	resp = doAPILogged(t, rl, "GET", "/debug", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusForbidden)
	rl.Printf("/debug gated: 401 unauthenticated, 403 non-superadmin")
}

// TestHealth_Debug_SuperadminFull_ReturnsFields asserts the /debug response
// shape for an active superadmin_full principal. The external e2e environment
// provisions no superadmin credential (TestSuperadmin_GetMe_ReturnsNullForRegularUser
// confirms e2e-test-user is not a superadmin), so this case is skipped here and
// covered by the DB-backed unit test (domain/health TestDiagnosticsAuthz).
func TestHealth_Debug_SuperadminFull_ReturnsFields(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("requires direct DB access to seed a superadmin_full principal")

	resp := doAPILogged(t, rl, "GET", "/debug", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("unmarshal /debug response: %v\nbody: %s", err, body)
	}
	for _, field := range []string{"environment", "go_version", "goroutines", "memory", "database"} {
		if _, ok := result[field]; !ok {
			t.Errorf("missing field %q in /debug response", field)
		}
	}
	dbStats, ok := result["database"].(map[string]any)
	if !ok {
		t.Fatal("database field is not an object in /debug response")
	}
	for _, f := range []string{"pool_total", "pool_idle"} {
		if _, ok := dbStats[f]; !ok {
			t.Errorf("missing field %q in /debug database stats", f)
		}
	}
	rl.Printf("/debug returned expected fields")
}

// TestHealth_Diagnostics_RequiresSuperadmin pins the /api/diagnostics gate in
// e2e (401 unauthenticated / 403 non-superadmin). The 200 superadmin_full case
// is exercised by the DB-backed unit test (domain/health TestDiagnosticsAuthz).
func TestHealth_Diagnostics_RequiresSuperadmin(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/diagnostics requires superadmin_full")

	// Unauthenticated → 401.
	resp := doAPILogged(t, rl, "GET", "/api/diagnostics", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)

	// Authenticated non-superadmin → 403.
	resp = doAPILogged(t, rl, "GET", "/api/diagnostics", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusForbidden)
	rl.Printf("/api/diagnostics gated: 401 unauthenticated, 403 non-superadmin")
}

func TestHealth_NoAuthRequired(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("Health probes require no auth")
	for _, path := range []string{"/health", "/healthz", "/ready", "/api/health"} {
		resp := doAPILogged(t, rl, "GET", path, "", "", nil) // no token
		body := readRespBody(resp)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			t.Errorf("%s should not require auth, got %d\nbody: %s", path, resp.StatusCode, body)
		}
	}
	rl.Printf("all health probes accessible without auth")
}
