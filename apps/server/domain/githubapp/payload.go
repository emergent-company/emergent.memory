package githubapp

import (
	"encoding/json"
	"strings"
)

// WebhookRef is a base/head ref with its SHA.
type WebhookRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// WebhookRepo is the repository subset used for allowlist matching.
type WebhookRepo struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

// WebhookUser identifies an actor so App-authored PRs can be skipped.
type WebhookUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// WebhookPullRequest is the pull_request subset the producer reads.
type WebhookPullRequest struct {
	Number  int         `json:"number"`
	Title   string      `json:"title"`
	Body    string      `json:"body"`
	HTMLURL string      `json:"html_url"`
	Draft   bool        `json:"draft"`
	Merged  bool        `json:"merged"`
	State   string      `json:"state"`
	User    WebhookUser `json:"user"`
	Base    WebhookRef  `json:"base"`
	Head    WebhookRef  `json:"head"`
}

// WebhookIssue is the issue subset the producer reads. The pull_request marker
// is present on issues that are actually pull requests; those must never enter
// the issue-coding workflow.
type WebhookIssue struct {
	Number      int            `json:"number"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	State       string         `json:"state"`
	HTMLURL     string         `json:"html_url"`
	UpdatedAt   string         `json:"updated_at"`
	Labels      []WebhookLabel `json:"labels"`
	User        WebhookUser    `json:"user"`
	PullRequest *struct{}      `json:"pull_request,omitempty"`
}

type WebhookLabel struct {
	Name string `json:"name"`
}

// WebhookPayload is the subset of the delivery body the producer consumes.
type WebhookPayload struct {
	Action       string `json:"action"`
	Number       int    `json:"number"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository  WebhookRepo        `json:"repository"`
	PullRequest WebhookPullRequest `json:"pull_request"`
	Issue       WebhookIssue       `json:"issue"`
	Sender      WebhookUser        `json:"sender"`
	// Repositories is the full selection on an installation event.
	Repositories []WebhookRepo `json:"repositories"`
	// installation_repositories deltas are top-level in that payload.
	RepositoriesAdded   []WebhookRepo `json:"repositories_added"`
	RepositoriesRemoved []WebhookRepo `json:"repositories_removed"`
}

// ParseWebhook unmarshals a verified delivery body.
func ParseWebhook(body []byte) (*WebhookPayload, error) {
	p := new(WebhookPayload)
	if err := json.Unmarshal(body, p); err != nil {
		return nil, err
	}
	return p, nil
}

// PullNumber returns the PR number, preferring pull_request.number.
func (p *WebhookPayload) PullNumber() int {
	if p.PullRequest.Number > 0 {
		return p.PullRequest.Number
	}
	return p.Number
}

// IsRelevantIssueAction reports the issue actions that may auto-start work.
// Only new/reopened issues auto-start by default; later edits update the board
// item without launching another run.
func (p *WebhookPayload) IsRelevantIssueAction() bool {
	switch p.Action {
	case "opened", "reopened":
		return true
	default:
		return false
	}
}

// IsIssue reports whether the delivery is an issue rather than a PR in the
// shared issues webhook.
func (p *WebhookPayload) IsIssue() bool {
	return p.Issue.Number > 0 && p.Issue.PullRequest == nil
}

// AuthoredByIssue reports whether the issue was authored by login or its App
// bot form.
func (p *WebhookPayload) AuthoredByIssue(login string) bool {
	if login == "" {
		return false
	}
	author := p.Issue.User.Login
	return strings.EqualFold(author, login) || strings.EqualFold(author, login+"[bot]")
}

// IsRelevantPullRequestAction reports the actions that should enqueue a review.
func (p *WebhookPayload) IsRelevantPullRequestAction() bool {
	switch p.Action {
	case "opened", "synchronize", "reopened", "ready_for_review":
		return true
	default:
		return false
	}
}

// ShouldSkipPullRequest reports PR states that must not produce a review.
// Bot-authored PRs are not skipped here: only the integration's own bot must be
// skipped, which the producer checks with AuthoredBy.
func (p *WebhookPayload) ShouldSkipPullRequest() bool {
	if p.PullRequest.Draft || p.PullRequest.Merged {
		return true
	}
	return p.PullRequest.State != "" && p.PullRequest.State != "open"
}

// AuthoredBy reports whether the PR was authored by login or its App bot form
// ("<login>[bot]").
func (p *WebhookPayload) AuthoredBy(login string) bool {
	if login == "" {
		return false
	}
	author := p.PullRequest.User.Login
	return strings.EqualFold(author, login) || strings.EqualFold(author, login+"[bot]")
}
