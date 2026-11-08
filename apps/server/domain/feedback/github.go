package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

const (
	githubRequestTimeout = 20 * time.Second

	// maxGitHubErrorBytes bounds the error text echoed into logs.
	maxGitHubErrorBytes = 4 << 10

	// maxGitHubResponseBytes bounds the SUCCESS body. GitHub answers a create
	// with the whole issue object, echoing the body it just stored — up to the
	// 16 KiB message cap plus the context table and the rest of the issue. Reusing
	// the small error bound here truncated that JSON and failed the unmarshal
	// *after* the issue had been created, which surfaced as a 502 on a report
	// that actually landed and made the retry a duplicate.
	maxGitHubResponseBytes = 1 << 20
)

// issueCreator is the seam the service uses to file an issue. Tests substitute
// an httptest-backed client.
type issueCreator interface {
	CreateIssue(ctx context.Context, title, body string, labels []string) (*createdIssue, error)
}

type createdIssue struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

type githubClient struct {
	baseURL string
	token   string
	repo    string
	http    *http.Client
}

// splitRepo splits an operator-supplied "owner/name" identifier and rejects
// anything that could smuggle a path segment into the request URL: an empty
// segment, "." or "..", whitespace, or a query/fragment separator.
func splitRepo(repo string) (owner, name string, ok bool) {
	// modelref:allow: repository identifiers are owner/name, not routed model names.
	owner, name, found := strings.Cut(repo, "/")
	if !found || !validRepoSegment(owner) || !validRepoSegment(name) {
		return "", "", false
	}
	return owner, name, true
}

// validRepoSegment accepts what GitHub allows in an owner or repository name.
func validRepoSegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// newGitHubClient validates the operator configuration. A malformed value is an
// error here (the caller logs and disables feedback) rather than a failure on
// the first submission.
func newGitHubClient(cfg config.FeedbackConfig) (*githubClient, error) {
	owner, name, ok := splitRepo(strings.TrimSpace(cfg.Repo))
	if !ok {
		return nil, fmt.Errorf("feedback repo %q must be owner/name", cfg.Repo)
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("feedback GitHub API base URL %q is not a valid http(s) URL", cfg.APIBaseURL)
	}
	if strings.TrimSpace(cfg.GitHubToken) == "" {
		return nil, errors.New("feedback GitHub token is empty")
	}
	return &githubClient{
		baseURL: base,
		token:   cfg.GitHubToken,
		repo:    owner + "/" + name,
		http:    &http.Client{Timeout: githubRequestTimeout},
	}, nil
}

// CreateIssue posts one issue to the configured repository. If the repository
// does not have the configured label, GitHub answers 422; the issue is retried
// once without labels so a missing triage label never drops user feedback.
func (c *githubClient) CreateIssue(ctx context.Context, title, body string, labels []string) (*createdIssue, error) {
	return c.create(ctx, title, body, labels, true)
}

func (c *githubClient) create(ctx context.Context, title, body string, labels []string, retryWithoutLabels bool) (*createdIssue, error) {
	payload := map[string]any{
		"title": title,
		"body":  body,
	}
	if len(labels) > 0 {
		payload["labels"] = labels
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal issue: %w", err)
	}

	endpoint := c.baseURL + "/repos/" + c.repo + "/issues"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build issue request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	defer resp.Body.Close()

	// A 422 on the label is retried; its body is not needed.
	if resp.StatusCode == http.StatusUnprocessableEntity && len(labels) > 0 && retryWithoutLabels {
		return c.create(ctx, title, body, nil, false)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorBody, err := io.ReadAll(io.LimitReader(resp.Body, maxGitHubErrorBytes))
		if err != nil {
			return nil, fmt.Errorf("read issue error response: %w", err)
		}
		return nil, &githubError{status: resp.StatusCode, body: errorBody}
	}

	// Read the success body with its own bound: it carries the issue we just
	// created, including the echoed body, not an error message.
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxGitHubResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read issue response: %w", err)
	}
	if len(responseBody) > maxGitHubResponseBytes {
		return nil, fmt.Errorf("decode issue response: body exceeds %d bytes", maxGitHubResponseBytes)
	}

	var issue createdIssue
	if err := json.Unmarshal(responseBody, &issue); err != nil {
		return nil, fmt.Errorf("decode issue response: %w", err)
	}
	if issue.HTMLURL == "" {
		return nil, errors.New("GitHub returned an issue without a URL")
	}
	return &issue, nil
}

// githubError carries GitHub's status and a bounded message. It never includes
// the request's Authorization header.
type githubError struct {
	status int
	body   []byte
}

func (e *githubError) Error() string {
	message := strings.TrimSpace(string(e.body))
	if len(message) > 512 {
		message = message[:512] + "…"
	}
	return fmt.Sprintf("github: issue creation returned %d: %s", e.status, message)
}
