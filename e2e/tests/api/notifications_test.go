// Package api_test — notifications_test.go
//
// Tests for the notifications API endpoints.
// Ported from emergent.memory/apps/server/tests/e2e/notifications_test.go
//
// Tests requiring direct DB inserts (createNotificationViaDB) are omitted.
// Only auth boundary and response structure tests are included here.
package api_test

import (
	"net/http"
	"testing"
)

// ─── Get Stats Tests ──────────────────────────────────────────────────────────

func TestNotifications_GetStats_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/notifications/stats", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestNotifications_GetStats_ReturnsStructure(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/notifications/stats", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	for _, field := range []string{"unread", "dismissed", "total"} {
		if _, ok := result[field]; !ok {
			t.Errorf("stats response missing field %q", field)
		}
	}
	rl.Printf("stats structure OK")
}

// ─── Get Counts Tests ─────────────────────────────────────────────────────────

func TestNotifications_GetCounts_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/notifications/counts", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestNotifications_GetCounts_ReturnsStructure(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/notifications/counts", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	if _, ok := result["data"]; !ok {
		t.Error("counts response missing data field")
	}
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("counts data is not a map: %T", result["data"])
	}
	for _, field := range []string{"all", "important", "other", "snoozed", "cleared"} {
		if _, ok := data[field]; !ok {
			t.Errorf("counts data missing field %q", field)
		}
	}
	rl.Printf("counts structure OK")
}

// ─── List Tests ───────────────────────────────────────────────────────────────

func TestNotifications_List_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/notifications", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestNotifications_List_ReturnsDataArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/notifications", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	if _, ok := result["data"]; !ok {
		t.Error("list response missing data field")
	}
	_, ok := result["data"].([]any)
	if !ok {
		t.Fatalf("list data is not an array: %T", result["data"])
	}
	rl.Printf("list returns data array")
}

// ─── Mark Read Tests ──────────────────────────────────────────────────────────

func TestNotifications_MarkRead_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/notifications/some-id/read", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestNotifications_MarkRead_ReturnsNotFoundForUnknownID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/notifications/00000000-0000-0000-0000-000000000000/read", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
	rl.AssertionStep("unknown notification returns 404", http.StatusNotFound, resp.StatusCode, nil)
}

// ─── Dismiss Tests ────────────────────────────────────────────────────────────

func TestNotifications_Dismiss_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/notifications/some-id/dismiss", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestNotifications_Dismiss_ReturnsNotFoundForUnknownID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/notifications/00000000-0000-0000-0000-000000000000/dismiss", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
	rl.AssertionStep("unknown notification returns 404", http.StatusNotFound, resp.StatusCode, nil)
}

// ─── Mark All Read Tests ──────────────────────────────────────────────────────

func TestNotifications_MarkAllRead_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/notifications/mark-all-read", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestNotifications_MarkAllRead_ReturnsStatus(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/notifications/mark-all-read", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["status"] != "marked_all_read" {
		t.Errorf("expected status=marked_all_read, got %v", result["status"])
	}
	rl.Printf("mark all read returns correct status")
}
