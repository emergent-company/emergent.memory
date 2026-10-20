package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestStub(t *testing.T) (*httptest.Server, *stub) {
	t.Helper()
	s := newStub("test-token")
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv, s
}

func do(t *testing.T, method, url, token string, payload any) *http.Response {
	t.Helper()
	var body *strings.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = strings.NewReader(string(encoded))
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func seed(t *testing.T, base, repo string, files []seedFile) {
	t.Helper()
	resp := do(t, "POST", base+"/_control/reset", "", map[string]any{
		"repo": repo, "ref": "main", "files": files,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestStub_HealthAndAuth(t *testing.T) {
	srv, _ := newTestStub(t)

	resp := do(t, "GET", srv.URL+"/healthz", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	for _, path := range []string{"/user/repos", "/repos/acme/docs"} {
		resp = do(t, "GET", srv.URL+path, "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s without token status = %d, want 401", path, resp.StatusCode)
		}
		body := decode[map[string]any](t, resp)
		if body["message"] != "Bad credentials" {
			t.Errorf("%s message = %v", path, body["message"])
		}

		resp = do(t, "GET", srv.URL+path, "wrong-token", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s with wrong token status = %d, want 401", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

func TestStub_RepoTreeBlob(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, "acme/docs", []seedFile{
		{Path: "README.md", Content: "# Docs\n"},
		{Path: "docs/guide.md", Content: "# Guide\n"},
	})

	meta := decode[map[string]any](t, do(t, "GET", srv.URL+"/repos/acme/docs", "test-token", nil))
	if meta["full_name"] != "acme/docs" || meta["private"] != true || meta["default_branch"] != "main" {
		t.Fatalf("repo metadata = %v", meta)
	}

	tree := decode[struct {
		Truncated bool        `json:"truncated"`
		Tree      []treeEntry `json:"tree"`
	}](t, do(t, "GET", srv.URL+"/repos/acme/docs/git/trees/main?recursive=1", "test-token", nil))
	if tree.Truncated {
		t.Fatal("truncated = true")
	}
	types := map[string]string{}
	for _, entry := range tree.Tree {
		types[entry.Path] = entry.Type
	}
	if types["docs"] != "tree" {
		t.Errorf("docs entry type = %q, want tree", types["docs"])
	}
	for _, path := range []string{"README.md", "docs/guide.md"} {
		if types[path] != "blob" {
			t.Errorf("%s entry type = %q, want blob", path, types[path])
		}
	}

	var guide treeEntry
	for _, entry := range tree.Tree {
		if entry.Path == "docs/guide.md" {
			guide = entry
		}
	}
	if guide.SHA != blobSHA("# Guide\n") {
		t.Fatalf("guide sha = %q, want content-addressed blob sha", guide.SHA)
	}

	blob := decode[struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Size     int    `json:"size"`
	}](t, do(t, "GET", srv.URL+"/repos/acme/docs/git/blobs/"+guide.SHA, "test-token", nil))
	if blob.Encoding != "base64" || blob.Size != len("# Guide\n") {
		t.Fatalf("blob = %+v", blob)
	}
	if strings.Contains(blob.Content, "\r") {
		t.Fatal("blob content should be newline-wrapped base64, not CRLF")
	}
}

func TestStub_UpdateProducesNewRevision(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, "acme/docs", []seedFile{{Path: "docs/guide.md", Content: "v1"}})

	tree := func() treeEntry {
		t.Helper()
		out := decode[struct {
			Tree []treeEntry `json:"tree"`
		}](t, do(t, "GET", srv.URL+"/repos/acme/docs/git/trees/main", "test-token", nil))
		for _, entry := range out.Tree {
			if entry.Path == "docs/guide.md" {
				return entry
			}
		}
		t.Fatal("guide.md missing from tree")
		return treeEntry{}
	}

	first := tree()
	if first.SHA != blobSHA("v1") {
		t.Fatalf("first sha = %q", first.SHA)
	}

	resp := do(t, "POST", srv.URL+"/_control/files", "", map[string]any{
		"repo": "acme/docs", "path": "docs/guide.md", "content": "v2",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	second := tree()
	if second.SHA == first.SHA {
		t.Fatal("revision unchanged after content update")
	}
	if second.SHA != blobSHA("v2") {
		t.Fatalf("second sha = %q, want sha of v2", second.SHA)
	}

	// Deleting the file removes it from the tree (drives deleted_from_source).
	resp = do(t, "DELETE", srv.URL+"/_control/files", "", map[string]any{
		"repo": "acme/docs", "path": "docs/guide.md",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	out := decode[struct {
		Tree []treeEntry `json:"tree"`
	}](t, do(t, "GET", srv.URL+"/repos/acme/docs/git/trees/main", "test-token", nil))
	for _, entry := range out.Tree {
		if entry.Path == "docs/guide.md" {
			t.Fatal("deleted file still present in tree")
		}
	}
}

func TestStub_ListReposIncludesPrivate(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, "acme/private-docs", []seedFile{{Path: "README.md", Content: "# x"}})

	repos := decode[[]struct {
		FullName string `json:"full_name"`
		Private  bool   `json:"private"`
	}](t, do(t, "GET", srv.URL+"/user/repos", "test-token", nil))
	if len(repos) != 1 || repos[0].FullName != "acme/private-docs" || !repos[0].Private {
		t.Fatalf("repos = %+v, want the private repo", repos)
	}
}

func TestStub_UnknownRepoAndRef(t *testing.T) {
	srv, _ := newTestStub(t)
	seed(t, srv.URL, "acme/docs", []seedFile{{Path: "README.md", Content: "# x"}})

	resp := do(t, "GET", srv.URL+"/repos/acme/missing", "test-token", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown repo status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = do(t, "GET", srv.URL+"/repos/acme/docs/git/trees/nope", "test-token", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown ref status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
