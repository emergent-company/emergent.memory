package config

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newSessionsServer starts a hub stub for GET /api/mcp-relay/sessions that
// records the Authorization header and returns status.
func newSessionsServer(t *testing.T, status int) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("probe method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/mcp-relay/sessions" {
			t.Errorf("probe path = %s, want /api/mcp-relay/sessions", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotAuth
}

func TestProbeSuccess(t *testing.T) {
	srv, gotAuth := newSessionsServer(t, http.StatusOK)
	client := srv.Client()
	err := ProbeConnectivity(context.Background(), client, srv.URL, "emt_secret")
	if err != nil {
		t.Fatalf("ProbeConnectivity: %v, want nil", err)
	}
	if *gotAuth != "Bearer emt_secret" {
		t.Errorf("Authorization = %q, want %q", *gotAuth, "Bearer emt_secret")
	}
}

func TestProbeTrailingSlashOnServerURL(t *testing.T) {
	srv, _ := newSessionsServer(t, http.StatusOK)
	client := srv.Client()
	err := ProbeConnectivity(context.Background(), client, srv.URL+"/", "emt_secret")
	if err != nil {
		t.Fatalf("ProbeConnectivity with trailing slash: %v, want nil", err)
	}
}

func TestProbeAuthRejected(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := newSessionsServer(t, status)
			err := ProbeConnectivity(context.Background(), srv.Client(), srv.URL, "emt_bad")
			if !errors.Is(err, ErrAuthFailed) {
				t.Errorf("ProbeConnectivity error = %v, want ErrAuthFailed", err)
			}
		})
	}
}

func TestProbeServerError(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, _ := newSessionsServer(t, status)
			err := ProbeConnectivity(context.Background(), srv.Client(), srv.URL, "emt_x")
			if err == nil {
				t.Fatal("ProbeConnectivity: expected error, got nil")
			}
			if errors.Is(err, ErrAuthFailed) {
				t.Errorf("ProbeConnectivity error %v should not be ErrAuthFailed", err)
			}
			if !strings.Contains(err.Error(), "server unreachable") {
				t.Errorf("ProbeConnectivity error %q should mention \"server unreachable\"", err)
			}
		})
	}
}

func TestProbeNetworkError(t *testing.T) {
	// Closed server -> connection refused, deterministic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	err := ProbeConnectivity(context.Background(), http.DefaultClient, url, "emt_x")
	if err == nil {
		t.Fatal("ProbeConnectivity: expected network error, got nil")
	}
	if !strings.Contains(err.Error(), "server unreachable") {
		t.Errorf("ProbeConnectivity error %q should mention \"server unreachable\"", err)
	}
}

// sessionsServer returns a hub stub that answers GET /api/mcp-relay/sessions
// with status and an optional JSON body, recording the Authorization header.
func sessionsServer(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &gotAuth
}

func TestListSessionsDecodes(t *testing.T) {
	body := `{"sessions":[{"instance_id":"mbp-connector","version":"0.1.0","tool_count":4,"connected_at":"2026-09-09T10:00:00Z"},{"instance_id":"other","tool_count":1}]}`
	srv, gotAuth := sessionsServer(t, http.StatusOK, body)

	sessions, err := ListSessions(context.Background(), srv.Client(), srv.URL, "emt_x")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if *gotAuth != "Bearer emt_x" {
		t.Errorf("Authorization = %q", *gotAuth)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions length = %d, want 2", len(sessions))
	}
	first := sessions[0]
	if first.InstanceID != "mbp-connector" || first.ToolCount != 4 || first.Version != "0.1.0" {
		t.Errorf("sessions[0] = %+v", first)
	}
	if first.ConnectedAt.IsZero() {
		t.Error("ConnectedAt should decode from RFC3339")
	}
	if sessions[1].ToolCount != 1 {
		t.Errorf("sessions[1] = %+v", sessions[1])
	}
}

func TestListSessionsEmptyBody(t *testing.T) {
	srv, _ := sessionsServer(t, http.StatusOK, `{"sessions":[]}`)
	sessions, err := ListSessions(context.Background(), srv.Client(), srv.URL, "emt_x")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions == nil || len(sessions) != 0 {
		t.Errorf("sessions = %#v, want empty non-nil", sessions)
	}
}

func TestListSessionsErrors(t *testing.T) {
	t.Run("auth", func(t *testing.T) {
		srv, _ := sessionsServer(t, http.StatusUnauthorized, "")
		_, err := ListSessions(context.Background(), srv.Client(), srv.URL, "emt_bad")
		if !errors.Is(err, ErrAuthFailed) {
			t.Errorf("err = %v, want ErrAuthFailed", err)
		}
	})
	t.Run("server error", func(t *testing.T) {
		srv, _ := sessionsServer(t, http.StatusServiceUnavailable, "")
		_, err := ListSessions(context.Background(), srv.Client(), srv.URL, "emt_x")
		if err == nil || !strings.Contains(err.Error(), "server unreachable") {
			t.Errorf("err = %v, want unreachable", err)
		}
	})
	t.Run("malformed body", func(t *testing.T) {
		srv, _ := sessionsServer(t, http.StatusOK, `not json`)
		_, err := ListSessions(context.Background(), srv.Client(), srv.URL, "emt_x")
		if err == nil || !strings.Contains(err.Error(), "parse sessions response") {
			t.Errorf("err = %v, want parse error", err)
		}
	})
	t.Run("network", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()
		_, err := ListSessions(context.Background(), http.DefaultClient, url, "emt_x")
		if err == nil || !strings.Contains(err.Error(), "server unreachable") {
			t.Errorf("err = %v, want unreachable", err)
		}
	})
}
