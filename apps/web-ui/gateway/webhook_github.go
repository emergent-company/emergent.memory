package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// githubSignatureHeader carries GitHub's HMAC-SHA256 signature of the raw body.
const githubSignatureHeader = "X-Hub-Signature-256"

// githubWebhookRepo is the subset of the payload's repository object used for
// allowlist matching and the trigger context.
type githubWebhookRepo struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	URL      string `json:"url"`
}

// githubWebhookRef is a base/head ref of a pull request.
type githubWebhookRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// githubWebhookPR is the subset of the pull_request object used by the review
// trigger.
type githubWebhookPR struct {
	Number int              `json:"number"`
	Title  string           `json:"title"`
	Base   githubWebhookRef `json:"base"`
	Head   githubWebhookRef `json:"head"`
}

// githubWebhookPayload is the subset of the GitHub webhook payload this
// ingress consumes. Unknown fields are ignored, so one struct covers every
// event type.
type githubWebhookPayload struct {
	Action      string            `json:"action"`
	Number      int               `json:"number"`
	Repository  githubWebhookRepo `json:"repository"`
	PullRequest githubWebhookPR   `json:"pull_request"`
}

// pullNumber returns the PR number, preferring pull_request.number (present on
// pull_request events) and falling back to the top-level number.
func (p *githubWebhookPayload) pullNumber() int {
	if p.PullRequest.Number > 0 {
		return p.PullRequest.Number
	}
	return p.Number
}

// githubWebhook is the POST /webhooks/github handler. GitHub authenticates via
// an HMAC-SHA256 signature header (not a session), so the route is registered
// outside the /api group and listed in publicAuthPath. Work is accepted with
// 202 and performed asynchronously so GitHub's delivery times out fast.
func (s *Server) githubWebhook(c echo.Context) error {
	secret := s.cfg.GitHubWebhookSecret
	if secret == "" {
		return c.String(http.StatusServiceUnavailable, "github webhook is not configured")
	}
	raw, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.String(http.StatusBadRequest, "cannot read request body")
	}
	if !verifyGitHubSignature(secret, raw, c.Request().Header.Get(githubSignatureHeader)) {
		return c.String(http.StatusUnauthorized, "invalid signature")
	}
	var payload githubWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return c.String(http.StatusBadRequest, "malformed payload")
	}

	event := c.Request().Header.Get("X-GitHub-Event")
	delivery := c.Request().Header.Get("X-GitHub-Delivery")
	log.Printf("github webhook: event=%s action=%s delivery=%s repo=%s", event, payload.Action, delivery, payload.Repository.FullName)

	if event != "pull_request" {
		return c.NoContent(http.StatusNoContent)
	}
	switch payload.Action {
	case "opened", "synchronize", "reopened":
	default:
		return c.NoContent(http.StatusNoContent)
	}
	repoFullName := payload.Repository.FullName
	if !githubReviewRepoAllowed(s.cfg.GitHubReviewRepos, repoFullName) {
		return c.NoContent(http.StatusNoContent)
	}
	agentID := s.cfg.GitHubReviewAgentID
	if agentID == "" {
		log.Printf("github webhook: GITHUB_REVIEW_AGENT_ID unset; ignoring %s#%d", repoFullName, payload.pullNumber())
		return c.NoContent(http.StatusNoContent)
	}

	prompt := githubReviewPrompt(&payload)
	ctxValues := githubReviewContext(&payload)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := s.memory.TriggerAgent(ctx, agentID, prompt, ctxValues); err != nil {
			log.Printf("github webhook: trigger agent %s for %s#%d failed: %v", agentID, repoFullName, payload.pullNumber(), err)
		}
	}()
	return c.NoContent(http.StatusAccepted)
}

// verifyGitHubSignature reports whether header ("sha256=<hex>") is a valid
// HMAC-SHA256 of body under secret, compared in constant time.
func verifyGitHubSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// githubReviewRepoAllowed reports whether fullName ("owner/repo") is in the
// comma-separated allowlist. An empty allowlist matches nothing (ignore all).
func githubReviewRepoAllowed(allowlist, fullName string) bool {
	if fullName == "" {
		return false
	}
	for _, entry := range strings.Split(allowlist, ",") {
		if strings.EqualFold(strings.TrimSpace(entry), fullName) {
			return true
		}
	}
	return false
}

// githubReviewPrompt builds the review-agent prompt for a relevant PR.
func githubReviewPrompt(p *githubWebhookPayload) string {
	return fmt.Sprintf("Review pull request #%d %q in %s", p.pullNumber(), p.PullRequest.Title, p.Repository.FullName)
}

// githubReviewContext extracts the optional trigger-context keys from the
// payload, omitting any that are empty.
func githubReviewContext(p *githubWebhookPayload) map[string]string {
	values := map[string]string{}
	if repoURL := firstNonEmpty(p.Repository.HTMLURL, p.Repository.URL); repoURL != "" {
		values["repository_url"] = repoURL
	}
	if n := p.pullNumber(); n > 0 {
		values["pull_request_number"] = strconv.Itoa(n)
	}
	if p.PullRequest.Base.Ref != "" {
		values["base_branch"] = p.PullRequest.Base.Ref
	}
	if p.PullRequest.Head.Ref != "" {
		values["head_branch"] = p.PullRequest.Head.Ref
	}
	if p.PullRequest.Head.SHA != "" {
		values["head_sha"] = p.PullRequest.Head.SHA
	}
	return values
}
