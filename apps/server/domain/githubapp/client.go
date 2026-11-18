package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultAPIBase = "https://api.github.com"
	// A commit payload for a merge commit can exceed 1 MiB once the PR has
	// merged the current base and the commit carries hundreds of files. The
	// review diff tool decodes the full JSON before truncating the rendered
	// patch, so the response buffer must be larger than the raw body.
	maxAPIResponse    = 8 << 20
	githubAPIVersion  = "2022-11-28"
	checkNameReviewed = "reviewed"
)

// PullRequestState is the live PR state read just before reviewing or posting.
type PullRequestState struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Draft     bool     `json:"draft"`
	Merged    bool     `json:"merged"`
	Labels    []string `json:"-"`
	UserLogin string   `json:"-"`
	UserType  string   `json:"-"`
	HeadSHA   string   `json:"-"`
	HeadRef   string   `json:"-"`
	BaseRef   string   `json:"-"`
}

// APIError is a GitHub REST error carrying the API message only (never a token
// or a raw body that could contain one).
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("githubapp: github api status %d: %s", e.Status, e.Message)
}

// Client is a minimal GitHub REST client scoped to one installation token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient builds a client for token. Empty baseURL means github.com.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultAPIBase
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: httpClient}
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("githubapp: marshal request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("githubapp: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("githubapp: github request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return fmt.Errorf("githubapp: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		return &APIError{Status: resp.StatusCode, Message: truncate([]byte(apiErr.Message), 300)}
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("githubapp: decode response: %w", err)
		}
	}
	return nil
}

// IssueState is the issue subset used by the coding workflow and backfill.
type IssueState struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
	UpdatedAt string `json:"updated_at"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

// ListOpenIssues reads one page of open issues. GitHub's issues endpoint also
// returns pull requests; callers must skip entries with PullRequest set.
func (c *Client) ListOpenIssues(ctx context.Context, repo string, page int) ([]IssueState, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("state", "open")
	q.Set("per_page", "30")
	q.Set("page", fmt.Sprintf("%d", page))
	var out []IssueState
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/issues?%s", repo, q.Encode()), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetIssue reads one issue's live state.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (*IssueState, error) {
	var out IssueState
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/issues/%d", repo, number), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EnsureIssueLabel creates a repository label when it does not exist. GitHub
// silently ignores unknown labels on an issue write, so claim labels must be
// created explicitly first.
func (c *Client) EnsureIssueLabel(ctx context.Context, repo, name, color, description string) error {
	labelPath := fmt.Sprintf("/repos/%s/labels/%s", repo, url.PathEscape(name))
	err := c.do(ctx, http.MethodGet, labelPath, nil, nil)
	if err == nil {
		return nil
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusNotFound {
		return err
	}
	createErr := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/labels", repo), map[string]any{
		"name": name, "color": color, "description": description,
	}, nil)
	if createErr == nil {
		return nil
	}
	if createAPIErr, ok := createErr.(*APIError); ok && createAPIErr.Status == http.StatusUnprocessableEntity {
		// Another writer won the create race; only treat it as success when the
		// label is now readable.
		if getErr := c.do(ctx, http.MethodGet, labelPath, nil, nil); getErr == nil {
			return nil
		}
	}
	return createErr
}

// AddIssueLabels adds labels to an issue and returns the label names GitHub
// reports on the issue after the write. The response is authoritative: a label
// that does not exist is not added, so callers can detect a partial write.
func (c *Client) AddIssueLabels(ctx context.Context, repo string, number int, labels []string) ([]string, error) {
	var out []struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/labels", repo, number), map[string]any{
		"labels": labels,
	}, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out))
	for _, label := range out {
		if name := strings.TrimSpace(label.Name); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// RemoveIssueLabel removes one label from an issue. A missing label is a
// successful no-op on GitHub.
func (c *Client) RemoveIssueLabel(ctx context.Context, repo string, number int, label string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/issues/%d/labels/%s", repo, number, url.PathEscape(label)), nil, nil)
}

// AddIssueAssignees assigns users to an issue.
func (c *Client) AddIssueAssignees(ctx context.Context, repo string, number int, assignees []string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/assignees", repo, number), map[string]any{
		"assignees": assignees,
	}, nil)
}

// RemoveIssueAssignees unassigns users from an issue.
func (c *Client) RemoveIssueAssignees(ctx context.Context, repo string, number int, assignees []string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/issues/%d/assignees", repo, number), map[string]any{
		"assignees": assignees,
	}, nil)
}

// GetPullRequest reads the live PR state.
func (c *Client) GetPullRequest(ctx context.Context, repo string, number int) (*PullRequestState, error) {
	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Draft  bool   `json:"draft"`
		Merged bool   `json:"merged"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		User struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"user"`
		Head struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), nil, &raw); err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(raw.Labels))
	for _, label := range raw.Labels {
		if name := strings.TrimSpace(label.Name); name != "" {
			labels = append(labels, name)
		}
	}
	return &PullRequestState{
		Number:    raw.Number,
		Title:     raw.Title,
		State:     raw.State,
		Draft:     raw.Draft,
		Merged:    raw.Merged,
		Labels:    labels,
		UserLogin: raw.User.Login,
		UserType:  raw.User.Type,
		HeadSHA:   raw.Head.SHA,
		HeadRef:   raw.Head.Ref,
		BaseRef:   raw.Base.Ref,
	}, nil
}

// CommitFile is one changed file in a commit diff.
type CommitFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

// CommitDiff is the diff and metadata for one commit SHA.
type CommitDiff struct {
	SHA     string       `json:"sha"`
	Message string       `json:"-"`
	Files   []CommitFile `json:"files"`
}

// GetCommit reads the diff for a specific commit. Reviewing a commit (not the
// moving PR head) keeps the diff aligned with the reviewed SHA. The SHA is
// validated at the tool boundary and path-escaped here so a crafted value cannot
// retarget the request (e.g. at a branch name).
func (c *Client) GetCommit(ctx context.Context, repo, sha string) (*CommitDiff, error) {
	var raw struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
		Files []CommitFile `json:"files"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s", repo, url.PathEscape(sha)), nil, &raw); err != nil {
		return nil, err
	}
	return &CommitDiff{SHA: raw.SHA, Message: raw.Commit.Message, Files: raw.Files}, nil
}

// CreateReview posts an APPROVE or REQUEST_CHANGES review bound to headSHA.
func (c *Client) CreateReview(ctx context.Context, repo string, number int, headSHA, event, body string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, number), map[string]any{
		"commit_id": headSHA,
		"event":     event,
		"body":      body,
	}, nil)
}

// CreateCheck publishes a head-bound check run.
func (c *Client) CreateCheck(ctx context.Context, repo, headSHA, name, status, conclusion, title, summary string) error {
	body := map[string]any{
		"name":     name,
		"head_sha": headSHA,
		"status":   status,
		"output": map[string]string{
			"title":   title,
			"summary": summary,
		},
	}
	if conclusion != "" {
		body["conclusion"] = conclusion
	}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/check-runs", repo), body, nil)
}

// CreateReviewedCheck publishes the head-bound "reviewed" check run.
func (c *Client) CreateReviewedCheck(ctx context.Context, repo, headSHA, conclusion, title, summary string) error {
	return c.CreateCheck(ctx, repo, headSHA, checkNameReviewed, "completed", conclusion, title, summary)
}

// CreateIssueComment posts the merge-guard marker as a PR conversation comment.
func (c *Client) CreateIssueComment(ctx context.Context, repo string, number int, body string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number), map[string]any{
		"body": body,
	}, nil)
}

// CloseIssue closes an issue as completed after its linked coding PR merges.
func (c *Client) CloseIssue(ctx context.Context, repo string, number int) error {
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/issues/%d", repo, number), map[string]any{
		"state": "closed",
	}, nil)
}
