package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAPIBase    = "https://api.github.com"
	maxAPIResponse    = 1 << 20
	githubAPIVersion  = "2022-11-28"
	checkNameReviewed = "reviewed"
)

// PullRequestState is the live PR state read just before reviewing or posting.
type PullRequestState struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Draft     bool   `json:"draft"`
	Merged    bool   `json:"merged"`
	UserLogin string `json:"-"`
	UserType  string `json:"-"`
	HeadSHA   string `json:"-"`
	HeadRef   string `json:"-"`
	BaseRef   string `json:"-"`
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

// GetPullRequest reads the live PR state.
func (c *Client) GetPullRequest(ctx context.Context, repo string, number int) (*PullRequestState, error) {
	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Draft  bool   `json:"draft"`
		Merged bool   `json:"merged"`
		User   struct {
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
	return &PullRequestState{
		Number:    raw.Number,
		Title:     raw.Title,
		State:     raw.State,
		Draft:     raw.Draft,
		Merged:    raw.Merged,
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
// moving PR head) keeps the diff aligned with the reviewed SHA.
func (c *Client) GetCommit(ctx context.Context, repo, sha string) (*CommitDiff, error) {
	var raw struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
		Files []CommitFile `json:"files"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s", repo, sha), nil, &raw); err != nil {
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

// CreateReviewedCheck publishes the head-bound "reviewed" check run.
func (c *Client) CreateReviewedCheck(ctx context.Context, repo, headSHA, conclusion, title, summary string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/check-runs", repo), map[string]any{
		"name":       checkNameReviewed,
		"head_sha":   headSHA,
		"status":     "completed",
		"conclusion": conclusion,
		"output": map[string]string{
			"title":   title,
			"summary": summary,
		},
	}, nil)
}

// CreateIssueComment posts the merge-guard marker as a PR conversation comment.
func (c *Client) CreateIssueComment(ctx context.Context, repo string, number int, body string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number), map[string]any{
		"body": body,
	}, nil)
}
