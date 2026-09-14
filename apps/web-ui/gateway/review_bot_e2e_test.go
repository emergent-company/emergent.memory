package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// TestReviewBotE2E is an opt-in, live end-to-end harness for the GitHub review
// bot. It is skipped unless REVIEW_BOT_E2E=1 and every required environment
// variable is set, because it talks to real GitHub and a real Memory backend:
//
//	REVIEW_BOT_AUTHOR_TOKEN  USER token (not the reviewing App) with repo write
//	                         access — the PR author must be a DISTINCT identity
//	                         from the App that reviews it (see below)
//	REVIEW_BOT_REPO          owner/repo the bot is configured to review
//	MEMORY_URL               Memory base URL the gateway should trigger
//	MEMORY_TOKEN             Memory bearer token
//	MEMORY_PROJECT_ID        Memory project id
//	REVIEW_BOT_AGENT_ID      Memory runtime agent id triggered for the PR
//	REVIEW_BOT_BASE_BRANCH   base branch (default "main")
//
// It creates a branch with a deliberately buggy fixture, opens a PR, sends a
// signed pull_request.opened webhook at the real gateway handler wired to a
// real MemoryClient, and polls the PR reviews for a CHANGES_REQUESTED review
// from a bot/App. Cleanup closes the PR and deletes the branch best-effort.
func TestReviewBotE2E(t *testing.T) {
	if os.Getenv("REVIEW_BOT_E2E") != "1" {
		t.Skip("set REVIEW_BOT_E2E=1 to run the live review-bot e2e")
	}

	token := os.Getenv("REVIEW_BOT_AUTHOR_TOKEN")
	repo := os.Getenv("REVIEW_BOT_REPO")
	memoryURL := os.Getenv("MEMORY_URL")
	memoryToken := os.Getenv("MEMORY_TOKEN")
	memoryProject := os.Getenv("MEMORY_PROJECT_ID")
	agentID := os.Getenv("REVIEW_BOT_AGENT_ID")
	baseBranch := envOr("REVIEW_BOT_BASE_BRANCH", "main")

	// REVIEW_BOT_AUTHOR_TOKEN is a USER token used for every GitHub REST call
	// in this harness (create branch, put file, open PR, poll reviews,
	// cleanup). The PR author identity MUST differ from the reviewing GitHub
	// App — the reviewer identity is the App configured inside Memory (native
	// App auth) — because GitHub forbids an App from submitting a
	// REQUEST_CHANGES/APPROVE review on its own PR; it silently downgrades to a
	// plain COMMENTED review. Using the reviewing App's installation token as
	// the author would therefore make the CHANGES_REQUESTED assertion
	// impossible. This token is only the PR author/reader.
	required := map[string]string{
		"REVIEW_BOT_AUTHOR_TOKEN": token,
		"REVIEW_BOT_REPO":         repo,
		"MEMORY_URL":              memoryURL,
		"MEMORY_TOKEN":            memoryToken,
		"MEMORY_PROJECT_ID":       memoryProject,
		"REVIEW_BOT_AGENT_ID":     agentID,
	}
	var missing []string
	for name, value := range required {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Skipf("live review-bot e2e requires: %s", strings.Join(missing, ", "))
	}
	if !strings.Contains(repo, "/") {
		t.Fatalf("REVIEW_BOT_REPO = %q, want owner/repo", repo)
	}

	gh := &reviewBotGitHub{
		token: token,
		base:  "https://api.github.com",
		http:  &http.Client{Timeout: 30 * time.Second},
	}

	// --- 1. Create the branch, fixture file and PR. ---

	branch := fmt.Sprintf("review-bot-e2e-%d", time.Now().Unix())

	var refResp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	gh.must(t, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+baseBranch, nil, http.StatusOK, &refResp)
	if refResp.Object.SHA == "" {
		t.Fatalf("base branch %s has no sha", baseBranch)
	}

	gh.must(t, http.MethodPost, "/repos/"+repo+"/git/refs", map[string]any{
		"ref": "refs/heads/" + branch,
		"sha": refResp.Object.SHA,
	}, http.StatusCreated, nil)
	t.Cleanup(func() {
		gh.bestEffort(t, http.MethodDelete, "/repos/"+repo+"/git/refs/heads/"+branch, nil)
	})
	t.Logf("created branch %s from %s", branch, baseBranch)

	const fixture = `"""Deliberately buggy fixture for the review-bot e2e."""


def last(items):
    # BUG: returns the first item instead of the last.
    return items[0]
`
	var fileResp struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	gh.must(t, http.MethodPut, "/repos/"+repo+"/contents/e2e_fixture.py", map[string]any{
		"message": "e2e: add buggy review-bot fixture",
		"content": base64.StdEncoding.EncodeToString([]byte(fixture)),
		"branch":  branch,
	}, http.StatusCreated, &fileResp)
	headSHA := fileResp.Commit.SHA

	var prResp struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	gh.must(t, http.MethodPost, "/repos/"+repo+"/pulls", map[string]any{
		"title": "e2e: review-bot fixture",
		"head":  branch,
		"base":  baseBranch,
		"body":  "Automated review-bot e2e. Closes on cleanup.",
	}, http.StatusCreated, &prResp)
	if prResp.Number == 0 {
		t.Fatalf("created PR has no number: %s", prResp.HTMLURL)
	}
	t.Cleanup(func() {
		gh.bestEffort(t, http.MethodPatch, fmt.Sprintf("/repos/%s/pulls/%d", repo, prResp.Number), map[string]any{"state": "closed"})
	})
	t.Logf("opened %s%s", repo, fmt.Sprintf("#%d", prResp.Number))

	// --- 2. Build a real MemoryClient + minimal Server at the webhook route. ---

	const webhookSecret = "review-bot-e2e-local-secret"
	client := NewMemoryClient(memoryURL, memoryToken, memoryProject)
	s := &Server{
		cfg: Config{
			GitHubWebhookSecret: webhookSecret,
			GitHubReviewAgentID: agentID,
			GitHubReviewRepos:   repo,
			MemoryURL:           memoryURL,
			MemoryProjectID:     memoryProject,
		},
		memory: client,
	}
	e := echo.New()
	e.POST("/webhooks/github", s.githubWebhook)
	gw := httptest.NewServer(e)
	defer gw.Close()

	// --- 3. Send a signed pull_request.opened webhook. ---

	payload, err := json.Marshal(map[string]any{
		"action": "opened",
		"number": prResp.Number,
		"repository": map[string]any{
			"full_name": repo,
			"html_url":  "https://github.com/" + repo,
		},
		"pull_request": map[string]any{
			"number": prResp.Number,
			"title":  "e2e: review-bot fixture",
			"base":   map[string]any{"ref": baseBranch},
			"head":   map[string]any{"ref": branch, "sha": headSHA},
		},
	})
	if err != nil {
		t.Fatalf("marshal webhook payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, gw.URL+"/webhooks/github", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new webhook request: %v", err)
	}
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("e2e-%d", time.Now().UnixNano()))
	req.Header.Set(githubSignatureHeader, signGitHubBody(webhookSecret, string(payload)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	func() {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("webhook status = %d, want %d; body: %s", resp.StatusCode, http.StatusAccepted, body)
		}
	}()

	// --- 4-5. Poll the PR reviews for a bot CHANGES_REQUESTED. ---

	const (
		pollInterval = 10 * time.Second
		pollTimeout  = 6 * time.Minute
	)
	reviewsPath := fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, prResp.Number)
	deadline := time.Now().Add(pollTimeout)
	var lastReviews []reviewBotReview
	var lastErr error
	for {
		status, raw, err := gh.roundTrip(http.MethodGet, reviewsPath, nil)
		if err != nil {
			lastErr = err
			t.Logf("poll reviews: %v", err)
		} else if status != http.StatusOK {
			lastErr = fmt.Errorf("status %d: %s", status, raw)
			t.Logf("poll reviews: %v", lastErr)
		} else {
			var reviews []reviewBotReview
			if jsonErr := json.Unmarshal(raw, &reviews); jsonErr != nil {
				lastErr = jsonErr
				t.Logf("decode reviews: %v", jsonErr)
			} else {
				lastReviews = reviews
				lastErr = nil
				for _, r := range reviews {
					if reviewBotIsBot(r.User.Type, r.User.Login) && r.State == "CHANGES_REQUESTED" {
						t.Logf("review bot %s posted CHANGES_REQUESTED: %s", r.User.Login, r.Body)
						return
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no CHANGES_REQUESTED review by a bot within %s (last error: %v); reviews: %+v",
				pollTimeout, lastErr, lastReviews)
		}
		time.Sleep(pollInterval)
	}
}

// reviewBotReview is the subset of GitHub's review object the harness needs.
type reviewBotReview struct {
	State string `json:"state"`
	Body  string `json:"body"`
	User  struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

// reviewBotIsBot reports whether a review author is a GitHub App / bot.
func reviewBotIsBot(userType, login string) bool {
	return strings.EqualFold(userType, "Bot") || strings.HasSuffix(login, "[bot]")
}

// reviewBotGitHub is a tiny stdlib-only GitHub REST client for the harness.
type reviewBotGitHub struct {
	token string
	base  string
	http  *http.Client
}

// roundTrip performs one GitHub REST call and returns status + body.
func (g *reviewBotGitHub) roundTrip(method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, g.base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

// must round-trips a request, decodes a 2xx body into out when non-nil, and
// fails the test on any transport error or unexpected status.
func (g *reviewBotGitHub) must(t *testing.T, method, path string, body any, wantStatus int, out any) []byte {
	t.Helper()
	status, raw, err := g.roundTrip(method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if status != wantStatus {
		t.Fatalf("%s %s: status %d, want %d; body: %s", method, path, status, wantStatus, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: decode response: %v; body: %s", method, path, err, raw)
		}
	}
	return raw
}

// bestEffort issues a cleanup request and only logs failures — cleanup must
// never fail the test.
func (g *reviewBotGitHub) bestEffort(t *testing.T, method, path string, body any) {
	t.Helper()
	status, raw, err := g.roundTrip(method, path, body)
	if err != nil {
		t.Logf("cleanup %s %s: %v", method, path, err)
		return
	}
	if status >= 400 {
		t.Logf("cleanup %s %s: status %d: %s", method, path, status, raw)
	}
}
