package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// recordedTrigger captures one HTTP request the fake Memory backend received,
// so the webhook end-to-end test can assert exactly what TriggerAgent sent.
type recordedTrigger struct {
	Method    string
	Path      string
	Auth      string
	ProjectID string
	Body      triggerAgentRequest
}

// startWebhookHTTPTest wires a fake Memory backend (a real httptest server,
// NOT the in-memory fakeMemory) in front of a real MemoryClient, then exposes
// the webhook handler over a second httptest server so the test exercises the
// full HTTP path: signed POST -> Echo handler -> async MemoryClient.TriggerAgent
// -> fake Memory. It returns the gateway base URL and the channel carrying each
// recorded Memory request.
func startWebhookHTTPTest(t *testing.T, cfg Config) (string, chan recordedTrigger) {
	t.Helper()
	requests := make(chan recordedTrigger, 8)

	memSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body triggerAgentRequest
		_ = json.Unmarshal(raw, &body)
		requests <- recordedTrigger{
			Method:    r.Method,
			Path:      r.URL.Path,
			Auth:      r.Header.Get("Authorization"),
			ProjectID: r.Header.Get("X-Project-ID"),
			Body:      body,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"runId":"run-1","message":"queued"}`)
	}))
	t.Cleanup(memSrv.Close)

	client := NewMemoryClient(memSrv.URL, "mem-token", cfg.MemoryProjectID)
	s := &Server{cfg: cfg, memory: client}

	e := echo.New()
	e.POST("/webhooks/github", s.githubWebhook)
	gwSrv := httptest.NewServer(e)
	t.Cleanup(gwSrv.Close)

	return gwSrv.URL, requests
}

// sendGitHubWebhook POSTs body to the gateway's /webhooks/github route. An
// empty signature signs body with secret; "-" omits the header entirely.
func sendGitHubWebhook(t *testing.T, baseURL, secret, event, body, signature string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/webhooks/github", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", "delivery-e2e")
	sig := signature
	if sig == "" {
		sig = signGitHubBody(secret, body)
	}
	if sig != "-" {
		req.Header.Set(githubSignatureHeader, sig)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// assertNoMemoryRequest fails if the fake Memory received anything within the
// quiet window. The rejected paths must not spawn the trigger goroutine at all.
func assertNoMemoryRequest(t *testing.T, requests chan recordedTrigger) {
	t.Helper()
	select {
	case got := <-requests:
		t.Fatalf("unexpected Memory request: method=%s path=%s body=%+v", got.Method, got.Path, got.Body)
	case <-time.After(500 * time.Millisecond):
	}
}

// TestGitHubWebhookHTTPEndToEnd drives the handler through real HTTP with a
// real MemoryClient and a fake Memory backend. It covers the happy path plus
// the two rejection paths, and verifies the webhook route is auth-exempt.
func TestGitHubWebhookHTTPEndToEnd(t *testing.T) {
	const (
		secret    = "topsecret"
		agentID   = "agent-42"
		projectID = "proj-9"
	)
	repos := "acme/widgets, other/repo"
	cfg := Config{
		MemoryProjectID:     projectID,
		GitHubWebhookSecret: secret,
		GitHubReviewAgentID: agentID,
		GitHubReviewRepos:   repos,
	}

	t.Run("valid signed pull_request.opened triggers Memory once", func(t *testing.T) {
		baseURL, requests := startWebhookHTTPTest(t, cfg)
		body := githubPayloadJSON("acme/widgets", "opened", 7)

		if got := sendGitHubWebhook(t, baseURL, secret, "pull_request", body, ""); got != http.StatusAccepted {
			t.Fatalf("status = %d, want %d", got, http.StatusAccepted)
		}

		var got recordedTrigger
		select {
		case got = <-requests:
		case <-time.After(5 * time.Second):
			t.Fatal("fake Memory received no request")
		}

		if got.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", got.Method)
		}
		wantPath := "/api/projects/" + projectID + "/agents/" + agentID + "/trigger"
		if got.Path != wantPath {
			t.Errorf("path = %s, want %s", got.Path, wantPath)
		}
		if got.Auth != "Bearer mem-token" {
			t.Errorf("Authorization = %q, want %q", got.Auth, "Bearer mem-token")
		}
		// No session context is attached on the webhook path, so the project is
		// carried in the URL and X-Project-ID stays empty.
		if got.ProjectID != "" {
			t.Errorf("X-Project-ID = %q, want empty (project is in the URL)", got.ProjectID)
		}
		if !strings.Contains(got.Body.Prompt, "#7") || !strings.Contains(got.Body.Prompt, "acme/widgets") {
			t.Errorf("prompt = %q, want PR number and repo", got.Body.Prompt)
		}
		wantCtx := map[string]string{
			"repository_url":      "https://github.com/acme/widgets",
			"pull_request_number": "7",
			"base_branch":         "main",
			"head_branch":         "feature/x",
			"head_sha":            "abc123",
		}
		for k, want := range wantCtx {
			if got := got.Body.Context[k]; got != want {
				t.Errorf("context[%q] = %q, want %q", k, got, want)
			}
		}

		// Exactly one trigger: nothing else arrives afterwards.
		select {
		case extra := <-requests:
			t.Fatalf("unexpected extra Memory request: %+v", extra)
		case <-time.After(200 * time.Millisecond):
		}
	})

	t.Run("tampered signature rejected without triggering Memory", func(t *testing.T) {
		baseURL, requests := startWebhookHTTPTest(t, cfg)
		body := githubPayloadJSON("acme/widgets", "opened", 7)

		if got := sendGitHubWebhook(t, baseURL, secret, "pull_request", body, "sha256=deadbeef"); got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", got, http.StatusUnauthorized)
		}
		assertNoMemoryRequest(t, requests)
	})

	t.Run("repo not allowlisted ignored without triggering Memory", func(t *testing.T) {
		baseURL, requests := startWebhookHTTPTest(t, cfg)
		// other/repo IS allowlisted above; use a third repo to exercise rejection.
		body := githubPayloadJSON("evil/repo", "opened", 9)

		if got := sendGitHubWebhook(t, baseURL, secret, "pull_request", body, ""); got != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", got, http.StatusNoContent)
		}
		assertNoMemoryRequest(t, requests)
	})
}

// TestWebhookGitHubIsPublicAuthPath guards the auth exemption the ingress relies
// on: GitHub authenticates by HMAC, not by a session cookie.
func TestWebhookGitHubIsPublicAuthPath(t *testing.T) {
	if !publicAuthPath("/webhooks/github") {
		t.Fatal("publicAuthPath(\"/webhooks/github\") = false, want true")
	}
}

// TestCanonicalHostRedirectSkipsWebhooks guards the second gate a real GitHub
// delivery passes: canonicalHostRedirect. It 302s browser requests to
// PUBLIC_BASE_URL, but GitHub cannot follow a redirect and webhooks authenticate
// by HMAC rather than cookies, so /webhooks/* must be exempt regardless of Host.
func TestCanonicalHostRedirectSkipsWebhooks(t *testing.T) {
	s := &Server{cfg: Config{PublicBaseURL: "https://canonical.example.com", GitHubWebhookSecret: "s3cret"}}
	e := echo.New()
	e.Use(s.canonicalHostRedirect)
	e.POST("/webhooks/github", s.githubWebhook)
	e.GET("/some/page", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	// Webhook path on a non-canonical Host must reach the handler, not 302.
	req := httptest.NewRequest(http.MethodPost, "http://internal.local/webhooks/github", strings.NewReader("{}"))
	req.Host = "internal.local"
	req.Header.Set("X-GitHub-Event", "pull_request")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code == http.StatusFound {
		t.Fatalf("webhook must not be canonical-host redirected, got 302 to %q", rec.Header().Get("Location"))
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned webhook: got %d, want 401", rec.Code)
	}

	// A normal page on a non-canonical Host still redirects.
	req2 := httptest.NewRequest(http.MethodGet, "http://internal.local/some/page", nil)
	req2.Host = "internal.local"
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusFound {
		t.Fatalf("page with non-canonical host: got %d, want 302", rec2.Code)
	}
}
