// Package github implements the GitHub REST v3 transport used by the GitHub
// data-source connector. It is deliberately a small hand-written client rather
// than a full SDK: the connector only needs to enumerate a repository tree,
// read blob content, list repositories, and validate a connection.
//
// The API base URL is configurable (the deployment's GITHUB_API_BASE_URL), so
// GitHub Enterprise and the hermetic e2e API wrapper can be targeted without
// letting a user-supplied source config choose an arbitrary endpoint.
//
// Enumeration deliberately returns the provider revision id (the git blob SHA)
// per file: blob SHAs are content-addressed, so the sync worker's revision
// pre-filter skips unchanged files without fetching them.
package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the public GitHub REST API origin. GitHub Enterprise
	// exposes the same contract under <host>/api/v3.
	DefaultBaseURL = "https://api.github.com"

	// DefaultMaxBytes is the per-file size cap for a fetched blob (10 MiB).
	// Documentation files are small; anything larger is almost certainly not a
	// markdown document and would bloat the document/chunk pipeline.
	DefaultMaxBytes int64 = 10 << 20

	// DefaultRequestTimeout bounds one HTTP round trip.
	DefaultRequestTimeout = 60 * time.Second

	// maxJSONBytes bounds a metadata/tree response body.
	maxJSONBytes int64 = 32 << 20

	// maxErrorBytes bounds how much of an error body is retained for the
	// message; GitHub error bodies are tiny JSON documents.
	maxErrorBytes int64 = 64 << 10

	// maxRepoPages caps ListRepos pagination at 500 repositories.
	maxRepoPages = 5

	// apiVersion pins the REST API contract this client is written against.
	apiVersion = "2022-11-28"

	reposPerPage = 100
)

// Config controls client behaviour. Zero values fall back to the documented
// defaults.
type Config struct {
	// BaseURL overrides the GitHub API origin. A trailing slash is ignored; a
	// path prefix (e.g. https://ghe.example/api/v3) is preserved. Empty means
	// DefaultBaseURL.
	BaseURL string
	// HTTPClient overrides the default HTTP client (tests inject httptest's).
	HTTPClient *http.Client
	// MaxBytes is the per-file cap for a fetched blob. Default 10 MiB.
	MaxBytes int64
	// RequestTimeout bounds one request. Default 60s.
	RequestTimeout time.Duration
}

// Repo is the repository metadata the connector and resource picker need.
type Repo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

// Blob is one file entry from a repository tree.
type Blob struct {
	Path string
	SHA  string
	Size int64
}

// HTTPError is a non-2xx GitHub API response. Message carries GitHub's own
// error text (e.g. "Bad credentials"); it never includes the access token.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("github: API returned %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("github: API returned %d", e.StatusCode)
}

// ErrTruncatedTree reports a recursive tree response GitHub truncated because
// the repository exceeds its enumeration limit. The sync is aborted rather than
// silently ingesting a partial scope.
var ErrTruncatedTree = errors.New("github: repository tree is truncated (repository too large to enumerate recursively)")

// Client is a GitHub REST v3 client bound to one source's credentials.
type Client struct {
	baseURL  string
	token    string
	http     *http.Client
	maxBytes int64
	timeout  time.Duration
}

// New builds a client with default configuration.
func New(creds Credentials) (*Client, error) {
	return NewWithConfig(creds, Config{})
}

// NewWithConfig builds a client with an explicit configuration. An empty token
// is rejected here so a misconfigured source fails before any request.
func NewWithConfig(creds Credentials, cfg Config) (*Client, error) {
	token := strings.TrimSpace(creds.Token)
	if token == "" {
		return nil, errors.New("github: access token is required")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("github: invalid API base URL %q", baseURL)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}

	return &Client{
		baseURL:  baseURL,
		token:    token,
		http:     httpClient,
		maxBytes: maxBytes,
		timeout:  timeout,
	}, nil
}

// GetRepo returns repository metadata (private flag, default branch).
func (c *Client) GetRepo(ctx context.Context, repo string) (*Repo, error) {
	repoPath, err := repoAPIPath(repo)
	if err != nil {
		return nil, err
	}
	var out Repo
	if err := c.getJSON(ctx, repoPath, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRepos returns the repositories the token can see, newest activity first.
// Private repositories are included when the token is authorized for them.
// It fails rather than silently truncating when the token sees more than
// maxRepoPages*reposPerPage repositories.
func (c *Client) ListRepos(ctx context.Context) ([]Repo, error) {
	out := make([]Repo, 0, reposPerPage)
	for page := 1; page <= maxRepoPages; page++ {
		path := fmt.Sprintf("/user/repos?per_page=%d&sort=updated&page=%d", reposPerPage, page)
		var batch []Repo
		if err := c.getJSON(ctx, path, &batch); err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(batch) < reposPerPage {
			return out, nil
		}
	}
	// Every page was full: probe past the cap so a token that sees exactly
	// maxRepoPages*reposPerPage repositories is not rejected as truncated.
	var probe []Repo
	probePath := fmt.Sprintf("/user/repos?per_page=1&sort=updated&page=%d", maxRepoPages*reposPerPage+1)
	if err := c.getJSON(ctx, probePath, &probe); err != nil {
		return nil, err
	}
	if len(probe) > 0 {
		return nil, fmt.Errorf("github: token sees more than %d repositories; narrow the token scope", maxRepoPages*reposPerPage)
	}
	return out, nil
}

// ListTree returns the blobs in a repository at ref. An empty ref resolves the
// repository's default branch. Directories and submodules are excluded.
func (c *Client) ListTree(ctx context.Context, repo, ref string) ([]Blob, error) {
	repoPath, err := repoAPIPath(repo)
	if err != nil {
		return nil, err
	}

	resolvedRef := strings.TrimSpace(ref)
	if resolvedRef == "" {
		meta, err := c.GetRepo(ctx, repo)
		if err != nil {
			return nil, err
		}
		resolvedRef = strings.TrimSpace(meta.DefaultBranch)
		if resolvedRef == "" {
			return nil, fmt.Errorf("github: repository %s has no default branch", repo)
		}
	}

	var tree treeResponse
	if err := c.getJSON(ctx, repoPath+"/git/trees/"+url.PathEscape(resolvedRef)+"?recursive=1", &tree); err != nil {
		return nil, err
	}
	if tree.Truncated {
		return nil, ErrTruncatedTree
	}

	blobs := make([]Blob, 0, len(tree.Tree))
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		blobs = append(blobs, Blob{Path: entry.Path, SHA: entry.SHA, Size: entry.Size})
	}
	return blobs, nil
}

// FetchBlob returns the raw content of one blob, enforcing MaxBytes.
func (c *Client) FetchBlob(ctx context.Context, repo, sha string) ([]byte, error) {
	repoPath, err := repoAPIPath(repo)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sha) == "" {
		return nil, errors.New("github: blob sha is required")
	}

	resp, cancel, err := c.open(ctx, repoPath+"/git/blobs/"+url.PathEscape(sha))
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer func() { _ = resp.Body.Close() }()

	// The blob response wraps the content in base64 (~1.37x) plus JSON envelope,
	// so cap the wire body above the content limit and enforce the limit on the
	// decoded bytes below.
	wireLimit := c.maxBytes + c.maxBytes/2 + 64<<10
	if resp.ContentLength > wireLimit {
		return nil, fmt.Errorf("github: blob %s is %d bytes, exceeds the %d-byte limit", sha, resp.ContentLength, c.maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, wireLimit+1))
	if err != nil {
		return nil, fmt.Errorf("github: read blob %s: %w", sha, err)
	}
	if int64(len(body)) > wireLimit {
		return nil, fmt.Errorf("github: blob %s response exceeds the %d-byte limit", sha, c.maxBytes)
	}

	var payload struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("github: decode blob %s: %w", sha, err)
	}
	if payload.Encoding != "base64" {
		return nil, fmt.Errorf("github: blob %s has unsupported encoding %q", sha, payload.Encoding)
	}
	// GitHub wraps base64 content at 60 characters; strip newlines before
	// decoding so both the API and a plain base64 wrapper decode identically.
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(payload.Content), ""))
	if err != nil {
		return nil, fmt.Errorf("github: decode blob %s content: %w", sha, err)
	}
	if int64(len(decoded)) > c.maxBytes {
		return nil, fmt.Errorf("github: blob %s is %d bytes, exceeds the %d-byte limit", sha, len(decoded), c.maxBytes)
	}
	return decoded, nil
}

// TestConnection validates the token and that the configured repo/ref is
// readable. An empty ref resolves and validates the default branch.
func (c *Client) TestConnection(ctx context.Context, repo, ref string) error {
	meta, err := c.GetRepo(ctx, repo)
	if err != nil {
		return err
	}
	resolvedRef := strings.TrimSpace(ref)
	if resolvedRef == "" {
		resolvedRef = meta.DefaultBranch
	}
	if resolvedRef == "" {
		return fmt.Errorf("github: repository %s has no default branch", repo)
	}
	_, err = c.ListTree(ctx, repo, resolvedRef)
	return err
}

// treeResponse is the subset of GET /git/trees/{ref}?recursive=1 in use.
type treeResponse struct {
	SHA       string `json:"sha"`
	Truncated bool   `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	} `json:"tree"`
}

// getJSON performs one request and decodes a JSON response.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, cancel, err := c.open(ctx, path)
	if err != nil {
		return err
	}
	defer cancel()
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes+1))
	if err != nil {
		return fmt.Errorf("github: read response: %w", err)
	}
	if int64(len(body)) > maxJSONBytes {
		return errors.New("github: response body exceeds the metadata size limit")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("github: decode response: %w", err)
	}
	return nil
}

// open issues an authenticated request and maps non-2xx responses to HTTPError.
// The returned cancel function must be called by the caller once the body is
// consumed.
func (c *Client) open(ctx context.Context, path string) (*http.Response, context.CancelFunc, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "emergent-memory-source-sync")

	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("github: request %s: %w", req.URL.Path, err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
		_ = resp.Body.Close()
		cancel()
		return nil, nil, &HTTPError{StatusCode: resp.StatusCode, Message: errorMessage(body)}
	}
	return resp, cancel, nil
}

// errorMessage extracts GitHub's error text without ever echoing credentials.
func errorMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Message) != "" {
		return payload.Message
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

// repoAPIPath validates an owner/name repository identifier and returns its API
// path. Validation keeps a source config from smuggling path segments into the
// request URL.
func repoAPIPath(repo string) (string, error) {
	trimmed := strings.TrimSpace(repo)
	// modelref:allow: repository identifiers are owner/name, not routed model names.
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("github: repository %q must be owner/name", repo)
	}
	for _, part := range parts {
		if !validRepoSegment(part) {
			return "", fmt.Errorf("github: repository %q has an invalid segment", repo)
		}
	}
	return "/repos/" + parts[0] + "/" + parts[1], nil
}

func validRepoSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
