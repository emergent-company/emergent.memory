package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, baseURL string, cfg Config) *Client {
	t.Helper()
	cfg.BaseURL = baseURL
	client, err := NewWithConfig(Credentials{Token: "test-token"}, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	return client
}

func requireAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", got)
	}
	if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
		t.Errorf("Accept = %q, want the GitHub JSON media type", got)
	}
}

func TestNewWithConfig_RejectsMissingToken(t *testing.T) {
	if _, err := NewWithConfig(Credentials{}, Config{}); err == nil {
		t.Fatal("NewWithConfig(no token) = nil error, want error")
	}
}

func TestGetRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		if r.URL.Path != "/repos/acme/private-docs" {
			t.Errorf("path = %q, want /repos/acme/private-docs", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Repo{FullName: "acme/private-docs", Private: true, DefaultBranch: "main"})
	}))
	defer srv.Close()

	repo, err := newTestClient(t, srv.URL, Config{}).GetRepo(context.Background(), "acme/private-docs")
	if err != nil {
		t.Fatalf("GetRepo() error = %v", err)
	}
	if !repo.Private || repo.DefaultBranch != "main" {
		t.Fatalf("GetRepo() = %+v, want a private repo on main", repo)
	}
}

func TestGetRepo_RejectsInvalidIdentifier(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:1", Config{})
	for _, repo := range []string{"", "owner", "owner/name/extra", "owner/../name", "owner/na me"} {
		if _, err := client.GetRepo(context.Background(), repo); err == nil {
			t.Errorf("GetRepo(%q) = nil error, want validation error", repo)
		}
	}
}

func TestListTree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		if r.URL.Path != "/repos/acme/private-docs/git/trees/main" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("recursive") != "1" {
			t.Errorf("recursive = %q, want 1", r.URL.Query().Get("recursive"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sha":       "tree-sha",
			"truncated": false,
			"tree": []map[string]any{
				{"path": "docs", "type": "tree", "sha": "dir-sha"},
				{"path": "docs/guide.md", "type": "blob", "sha": "sha-guide", "size": 12},
				{"path": "docs/install.md", "type": "blob", "sha": "sha-install", "size": 34},
			},
		})
	}))
	defer srv.Close()

	blobs, err := newTestClient(t, srv.URL, Config{}).ListTree(context.Background(), "acme/private-docs", "main")
	if err != nil {
		t.Fatalf("ListTree() error = %v", err)
	}
	if len(blobs) != 2 {
		t.Fatalf("ListTree() = %+v, want only blobs", blobs)
	}
	if blobs[0].Path != "docs/guide.md" || blobs[0].SHA != "sha-guide" || blobs[0].Size != 12 {
		t.Errorf("blob[0] = %+v, want docs/guide.md sha-guide 12", blobs[0])
	}
}

func TestListTree_ResolvesDefaultBranch(t *testing.T) {
	var sawTree bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/private-docs":
			_ = json.NewEncoder(w).Encode(Repo{FullName: "acme/private-docs", DefaultBranch: "trunk"})
		case "/repos/acme/private-docs/git/trees/trunk":
			sawTree = true
			_ = json.NewEncoder(w).Encode(map[string]any{"tree": []map[string]any{}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL, Config{}).ListTree(context.Background(), "acme/private-docs", ""); err != nil {
		t.Fatalf("ListTree() error = %v", err)
	}
	if !sawTree {
		t.Fatal("expected the default branch (trunk) to be used")
	}
}

func TestListTree_EscapesRef(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.URL.Path decodes %2F, so assert on the escaped path instead.
		if got := r.URL.EscapedPath(); got != "/repos/acme/private-docs/git/trees/release%2F1.x" {
			t.Errorf("escaped path = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tree": []map[string]any{}})
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL, Config{}).ListTree(context.Background(), "acme/private-docs", "release/1.x"); err != nil {
		t.Fatalf("ListTree() error = %v", err)
	}
}

func TestListTree_Truncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"truncated": true, "tree": []map[string]any{}})
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, Config{}).ListTree(context.Background(), "acme/private-docs", "main")
	if !errors.Is(err, ErrTruncatedTree) {
		t.Fatalf("ListTree() error = %v, want ErrTruncatedTree", err)
	}
}

func TestFetchBlob(t *testing.T) {
	content := []byte("# Guide\n\nhello world\n")
	wrapped := base64.StdEncoding.EncodeToString(content)
	// GitHub wraps base64 at 60 chars.
	var lines []string
	for len(wrapped) > 60 {
		lines = append(lines, wrapped[:60])
		wrapped = wrapped[60:]
	}
	lines = append(lines, wrapped)
	encoded := strings.Join(lines, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		if r.URL.Path != "/repos/acme/private-docs/git/blobs/sha-guide" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sha": "sha-guide", "encoding": "base64", "size": len(content), "content": encoded,
		})
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv.URL, Config{}).FetchBlob(context.Background(), "acme/private-docs", "sha-guide")
	if err != nil {
		t.Fatalf("FetchBlob() error = %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("FetchBlob() = %q, want %q", got, content)
	}
}

func TestFetchBlob_EnforcesMaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 1024))),
		})
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, Config{MaxBytes: 16}).FetchBlob(context.Background(), "acme/private-docs", "sha")
	if err == nil {
		t.Fatal("FetchBlob() = nil error, want size-limit error")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("FetchBlob() error = %v, want a size-limit error", err)
	}
}

func TestHTTPErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, Config{}).GetRepo(context.Background(), "acme/private-docs")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("GetRepo() error = %v, want *HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized || httpErr.Message != "Bad credentials" {
		t.Fatalf("HTTPError = %+v, want 401 Bad credentials", httpErr)
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Fatal("error leaked the access token")
	}
}

func TestListRepos_Paginates(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		if r.URL.Path != "/user/repos" {
			t.Errorf("path = %q", r.URL.Path)
		}
		page++
		count := reposPerPage
		if page == 2 {
			count = 3
		}
		repos := make([]Repo, count)
		for i := range repos {
			repos[i] = Repo{FullName: fmt.Sprintf("acme/repo-%d-%d", page, i)}
		}
		_ = json.NewEncoder(w).Encode(repos)
	}))
	defer srv.Close()

	repos, err := newTestClient(t, srv.URL, Config{}).ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos() error = %v", err)
	}
	if len(repos) != reposPerPage+3 {
		t.Fatalf("ListRepos() returned %d repos, want %d", len(repos), reposPerPage+3)
	}
	if page != 2 {
		t.Fatalf("server saw %d pages, want 2", page)
	}
}

// newRepoListServer serves a token's repository listing with total entries,
// honouring per_page/page so the cap probe can be exercised.
func newRepoListServer(t *testing.T, total int) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		requests++
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		if perPage <= 0 {
			perPage = reposPerPage
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page <= 0 {
			page = 1
		}
		start := (page - 1) * perPage
		count := 0
		if start < total {
			count = total - start
			if count > perPage {
				count = perPage
			}
		}
		repos := make([]Repo, count)
		for i := range repos {
			repos[i] = Repo{FullName: fmt.Sprintf("acme/repo-%d", start+i)}
		}
		_ = json.NewEncoder(w).Encode(repos)
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func TestListRepos_ExactlyAtCapIsNotTruncated(t *testing.T) {
	srv, requests := newRepoListServer(t, maxRepoPages*reposPerPage)

	repos, err := newTestClient(t, srv.URL, Config{}).ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos() error = %v", err)
	}
	if want := maxRepoPages * reposPerPage; len(repos) != want {
		t.Fatalf("ListRepos() returned %d repos, want %d", len(repos), want)
	}
	if want := maxRepoPages + 1; *requests != want {
		t.Errorf("server saw %d requests, want %d (full pages + empty probe)", *requests, want)
	}
}

func TestListRepos_OverCapFails(t *testing.T) {
	srv, _ := newRepoListServer(t, maxRepoPages*reposPerPage+1)

	_, err := newTestClient(t, srv.URL, Config{}).ListRepos(context.Background())
	if err == nil {
		t.Fatal("ListRepos() = nil error, want a truncation error")
	}
	if !strings.Contains(err.Error(), "more than") {
		t.Errorf("ListRepos() error = %v, want it to mention the cap", err)
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/repos/acme/private-docs" {
			t.Errorf("path = %q, want the GitHub Enterprise prefix", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Repo{FullName: "acme/private-docs"})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL+"/api/v3", Config{})
	if _, err := client.GetRepo(context.Background(), "acme/private-docs"); err != nil {
		t.Fatalf("GetRepo() error = %v", err)
	}
}

func TestTestConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/private-docs":
			_ = json.NewEncoder(w).Encode(Repo{FullName: "acme/private-docs", DefaultBranch: "main"})
		case strings.HasSuffix(r.URL.Path, "/git/trees/main"):
			_ = json.NewEncoder(w).Encode(map[string]any{"tree": []map[string]any{
				{"path": "docs/guide.md", "type": "blob", "sha": "sha-guide"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer srv.Close()

	if err := newTestClient(t, srv.URL, Config{}).TestConnection(context.Background(), "acme/private-docs", "main"); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
}
