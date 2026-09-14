package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// githubPayloadJSON renders a minimal pull_request webhook body.
func githubPayloadJSON(repo, action string, number int) string {
	return fmt.Sprintf(
		`{"action":%q,"number":%d,"repository":{"full_name":%q,"html_url":"https://github.com/%s"},`+
			`"pull_request":{"number":%d,"title":"Add feature","base":{"ref":"main"},"head":{"ref":"feature/x","sha":"abc123"}}}`,
		action, number, repo, repo, number,
	)
}

// signGitHubBody returns the X-Hub-Signature-256 value for body under secret.
func signGitHubBody(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestGitHubWebhook(t *testing.T) {
	validBody := githubPayloadJSON("acme/widgets", "opened", 7)

	tests := []struct {
		name        string
		secret      string
		agentID     string
		repos       string
		event       string
		body        string
		signature   string // explicit override; "" = sign with secret (when set)
		wantStatus  int
		wantTrigger bool
	}{
		{
			name:        "valid signature accepted",
			secret:      "topsecret",
			agentID:     "agent-1",
			repos:       "acme/widgets",
			event:       "pull_request",
			body:        validBody,
			wantStatus:  http.StatusAccepted,
			wantTrigger: true,
		},
		{
			name:       "tampered signature rejected",
			secret:     "topsecret",
			agentID:    "agent-1",
			repos:      "acme/widgets",
			event:      "pull_request",
			body:       validBody,
			signature:  "sha256=deadbeef",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing signature rejected",
			secret:     "topsecret",
			agentID:    "agent-1",
			repos:      "acme/widgets",
			event:      "pull_request",
			body:       validBody,
			signature:  " ",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "non pull_request event filtered",
			secret:     "topsecret",
			agentID:    "agent-1",
			repos:      "acme/widgets",
			event:      "push",
			body:       validBody,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "irrelevant action filtered",
			secret:     "topsecret",
			agentID:    "agent-1",
			repos:      "acme/widgets",
			event:      "pull_request",
			body:       githubPayloadJSON("acme/widgets", "closed", 7),
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "repo not in allowlist ignored",
			secret:     "topsecret",
			agentID:    "agent-1",
			repos:      "acme/widgets",
			event:      "pull_request",
			body:       githubPayloadJSON("other/repo", "opened", 9),
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "empty allowlist ignores all",
			secret:     "topsecret",
			agentID:    "agent-1",
			repos:      "",
			event:      "pull_request",
			body:       validBody,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "empty agent id is a no-op",
			secret:     "topsecret",
			agentID:    "",
			repos:      "acme/widgets",
			event:      "pull_request",
			body:       validBody,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "missing secret returns 503",
			secret:     "",
			agentID:    "agent-1",
			repos:      "acme/widgets",
			event:      "pull_request",
			body:       validBody,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "malformed body returns 400",
			secret:     "topsecret",
			agentID:    "agent-1",
			repos:      "acme/widgets",
			event:      "pull_request",
			body:       `{"action": not-json`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeMemory{}
			var calls chan triggerAgentCall
			if tc.wantTrigger {
				calls = make(chan triggerAgentCall, 1)
				fake.triggerAgentCalls = calls
			}
			s := &Server{
				cfg: Config{
					GitHubWebhookSecret: tc.secret,
					GitHubReviewAgentID: tc.agentID,
					GitHubReviewRepos:   tc.repos,
				},
				memory: fake,
			}
			e := echo.New()
			e.POST("/webhooks/github", s.githubWebhook)

			req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(tc.body))
			req.Header.Set("X-GitHub-Event", tc.event)
			req.Header.Set("X-GitHub-Delivery", "delivery-1")
			sig := tc.signature
			if sig == "" && tc.secret != "" {
				sig = signGitHubBody(tc.secret, tc.body)
			}
			if sig != "" && sig != " " {
				req.Header.Set(githubSignatureHeader, sig)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}

			if tc.wantTrigger {
				select {
				case call := <-calls:
					if call.AgentID != "agent-1" {
						t.Errorf("agent id = %q, want agent-1", call.AgentID)
					}
					if !strings.Contains(call.Prompt, "#7") || !strings.Contains(call.Prompt, "acme/widgets") {
						t.Errorf("prompt = %q, want PR number and repo", call.Prompt)
					}
					wantCtx := map[string]string{
						"repository_url":      "https://github.com/acme/widgets",
						"pull_request_number": "7",
						"base_branch":         "main",
						"head_branch":         "feature/x",
						"head_sha":            "abc123",
					}
					for k, want := range wantCtx {
						if got := call.Context[k]; got != want {
							t.Errorf("context[%q] = %q, want %q", k, got, want)
						}
					}
				case <-time.After(3 * time.Second):
					t.Fatal("TriggerAgent was not called")
				}
			}
		})
	}
}

func TestGitHubReviewRepoAllowed(t *testing.T) {
	tests := []struct {
		allowlist string
		fullName  string
		want      bool
	}{
		{"acme/widgets", "acme/widgets", true},
		{"acme/widgets", "ACME/Widgets", true},
		{"acme/widgets, other/repo", "other/repo", true},
		{" acme/widgets , other/repo ", "acme/widgets", true},
		{"acme/widgets", "other/repo", false},
		{"", "acme/widgets", false},
		{"acme/widgets", "", false},
	}
	for _, tc := range tests {
		if got := githubReviewRepoAllowed(tc.allowlist, tc.fullName); got != tc.want {
			t.Errorf("githubReviewRepoAllowed(%q, %q) = %v, want %v", tc.allowlist, tc.fullName, got, tc.want)
		}
	}
}
