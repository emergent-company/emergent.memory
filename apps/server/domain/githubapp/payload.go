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
	Number int         `json:"number"`
	Title  string      `json:"title"`
	Draft  bool        `json:"draft"`
	Merged bool        `json:"merged"`
	State  string      `json:"state"`
	User   WebhookUser `json:"user"`
	Base   WebhookRef  `json:"base"`
	Head   WebhookRef  `json:"head"`
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
