// Command github-stub is a hermetic stand-in for a private GitHub repository
// served over the GitHub REST v3 contract, used by the e2e harness. It lets the
// GitHub data-source connector be exercised end to end — tree enumeration, blob
// fetch, revision-based incremental sync, and version-on-update — without a
// real repository, a real token, or network egress.
//
// It impersonates a *private* repo: every /repos and /user request requires the
// configured bearer token and answers 401 Bad credentials otherwise.
//
// GitHub-compatible routes:
//
//	GET /repos/{owner}/{repo}                        repository metadata (private)
//	GET /user/repos                                  repositories the token can see
//	GET /repos/{owner}/{repo}/git/trees/{ref}        tree (recursive=1 supported)
//	GET /repos/{owner}/{repo}/git/blobs/{sha}        base64 blob content
//	GET /healthz                                     200 "ok"
//
// Test-control routes (no auth; not part of the GitHub contract):
//
//	POST   /_control/reset   seed/replace a repository: {repo, ref, files:[{path,content}]}
//	POST   /_control/files   upsert one file (new blob sha): {repo, ref?, path, content}
//	DELETE /_control/files   remove one file: {repo, ref?, path}
//	GET    /_control/state   inspect a repository: ?repo=owner/name
//
// Configuration (env):
//
//	PORT                listen port (default 8080)
//	GITHUB_STUB_TOKEN   bearer token the fake repository requires
//	                    (default e2e-github-token)
package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort  = "8080"
	defaultToken = "e2e-github-token"
)

// repository is one seeded fake repository. files is keyed by ref, then by
// repository-relative path. blobs maps a git blob SHA to its content so a blob
// can be fetched by revision id exactly as GitHub serves it.
type repository struct {
	fullName      string
	private       bool
	defaultBranch string
	files         map[string]map[string]string
	blobs         map[string]string
}

// stub is the in-memory GitHub impersonation.
type stub struct {
	mu    sync.Mutex
	token string
	repos map[string]*repository
}

func newStub(token string) *stub {
	return &stub{token: token, repos: map[string]*repository{}}
}

func main() {
	token := strings.TrimSpace(os.Getenv("GITHUB_STUB_TOKEN"))
	if token == "" {
		token = defaultToken
	}
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = defaultPort
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newStub(token),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("github-stub listening on :%s (token required on /repos and /user)", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("github-stub: %v", err)
	}
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	segments := pathSegments(r.URL.EscapedPath())

	switch {
	case len(segments) == 1 && segments[0] == "healthz":
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
		return
	case len(segments) > 0 && segments[0] == "_control":
		s.serveControl(w, r, segments[1:])
		return
	}

	if !s.authorized(r) {
		writeGitHubError(w, http.StatusUnauthorized, "Bad credentials")
		return
	}

	// /user/repos
	if len(segments) == 2 && segments[0] == "user" && segments[1] == "repos" {
		s.listRepos(w)
		return
	}

	// /repos/{owner}/{repo}[/git/{trees|blobs}/{id}]
	if len(segments) >= 3 && segments[0] == "repos" {
		s.serveRepo(w, r, segments[1:])
		return
	}

	writeGitHubError(w, http.StatusNotFound, "Not Found")
}

func (s *stub) serveRepo(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) < 2 {
		writeGitHubError(w, http.StatusNotFound, "Not Found")
		return
	}
	fullName := rest[0] + "/" + rest[1]
	repo, ok := s.getRepo(fullName)
	if !ok {
		writeGitHubError(w, http.StatusNotFound, "Not Found")
		return
	}

	switch {
	case len(rest) == 2:
		writeJSON(w, http.StatusOK, map[string]any{
			"full_name":      repo.fullName,
			"private":        repo.private,
			"default_branch": repo.defaultBranch,
		})
	case len(rest) == 5 && rest[2] == "git" && rest[3] == "trees" && r.Method == http.MethodGet:
		s.serveTree(w, repo, rest[4])
	case len(rest) == 5 && rest[2] == "git" && rest[3] == "blobs" && r.Method == http.MethodGet:
		s.serveBlob(w, repo, rest[4])
	default:
		writeGitHubError(w, http.StatusNotFound, "Not Found")
	}
}

// treeEntry is one entry in a GitHub tree response.
type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size,omitempty"`
}

// serveTree answers GET /repos/{owner}/{repo}/git/trees/{ref} (recursive=1 is
// implied: the client always asks for the whole tree).
func (s *stub) serveTree(w http.ResponseWriter, repo *repository, ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	files, ok := repo.files[ref]
	if !ok {
		writeGitHubError(w, http.StatusNotFound, "Not Found")
		return
	}

	entries := make([]treeEntry, 0, len(files))
	seenDirs := map[string]bool{}
	for path, content := range files {
		entries = append(entries, treeEntry{
			Path: path, Mode: "100644", Type: "blob",
			SHA: blobSHA(content), Size: int64(len(content)),
		})
		for dir := parentDir(path); dir != ""; dir = parentDir(dir) {
			if seenDirs[dir] {
				break
			}
			seenDirs[dir] = true
			entries = append(entries, treeEntry{Path: dir, Mode: "040000", Type: "tree", SHA: treeSHA(dir, files)})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	writeJSON(w, http.StatusOK, map[string]any{
		"sha":       treeSHA("", files),
		"truncated": false,
		"tree":      entries,
	})
}

func (s *stub) serveBlob(w http.ResponseWriter, repo *repository, sha string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, ok := repo.blobs[sha]
	if !ok {
		writeGitHubError(w, http.StatusNotFound, "Not Found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sha":      sha,
		"size":     len(content),
		"encoding": "base64",
		"content":  wrapBase64(base64.StdEncoding.EncodeToString([]byte(content))),
	})
}

func (s *stub) listRepos(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.repos))
	for name := range s.repos {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		repo := s.repos[name]
		out = append(out, map[string]any{
			"full_name":      repo.fullName,
			"private":        repo.private,
			"default_branch": repo.defaultBranch,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// serveControl implements the test-only seeding/mutation endpoints.
func (s *stub) serveControl(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) != 1 {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown control route"})
		return
	}
	switch rest[0] {
	case "reset":
		s.controlReset(w, r)
	case "files":
		s.controlFiles(w, r)
	case "state":
		s.controlState(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown control route"})
	}
}

type seedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s *stub) controlReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "POST required"})
		return
	}
	var req struct {
		Repo  string     `json:"repo"`
		Ref   string     `json:"ref"`
		Files []seedFile `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid body: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Repo) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "repo is required"})
		return
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = "main"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	repo := &repository{
		fullName:      req.Repo,
		private:       true,
		defaultBranch: ref,
		files:         map[string]map[string]string{ref: {}},
		blobs:         map[string]string{},
	}
	out := make([]map[string]any, 0, len(req.Files))
	for _, f := range req.Files {
		path := strings.TrimPrefix(strings.TrimSpace(f.Path), "/")
		if path == "" {
			continue
		}
		repo.files[ref][path] = f.Content
		sha := blobSHA(f.Content)
		repo.blobs[sha] = f.Content
		out = append(out, map[string]any{"path": path, "sha": sha})
	}
	s.repos[req.Repo] = repo

	writeJSON(w, http.StatusOK, map[string]any{
		"repo": req.Repo, "ref": ref, "files": out,
	})
}

func (s *stub) controlFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Repo    string `json:"repo"`
			Ref     string `json:"ref"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid body: " + err.Error()})
			return
		}
		path := strings.TrimPrefix(strings.TrimSpace(req.Path), "/")
		if req.Repo == "" || path == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "repo and path are required"})
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		repo, ok := s.repos[req.Repo]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown repo"})
			return
		}
		ref := strings.TrimSpace(req.Ref)
		if ref == "" {
			ref = repo.defaultBranch
		}
		if _, ok := repo.files[ref]; !ok {
			repo.files[ref] = map[string]string{}
		}
		repo.files[ref][path] = req.Content
		sha := blobSHA(req.Content)
		repo.blobs[sha] = req.Content
		writeJSON(w, http.StatusOK, map[string]any{"repo": req.Repo, "ref": ref, "path": path, "sha": sha})

	case http.MethodDelete:
		var req struct {
			Repo string `json:"repo"`
			Ref  string `json:"ref"`
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid body: " + err.Error()})
			return
		}
		path := strings.TrimPrefix(strings.TrimSpace(req.Path), "/")

		s.mu.Lock()
		defer s.mu.Unlock()
		repo, ok := s.repos[req.Repo]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown repo"})
			return
		}
		ref := strings.TrimSpace(req.Ref)
		if ref == "" {
			ref = repo.defaultBranch
		}
		delete(repo.files[ref], path)
		writeJSON(w, http.StatusOK, map[string]any{"repo": req.Repo, "ref": ref, "path": path, "deleted": true})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "POST or DELETE required"})
	}
}

func (s *stub) controlState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "GET required"})
		return
	}
	name := r.URL.Query().Get("repo")

	s.mu.Lock()
	defer s.mu.Unlock()
	repo, ok := s.repos[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "unknown repo"})
		return
	}
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = repo.defaultBranch
	}
	files := make([]map[string]any, 0, len(repo.files[ref]))
	for path, content := range repo.files[ref] {
		files = append(files, map[string]any{"path": path, "sha": blobSHA(content), "content": content})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repo": repo.fullName, "ref": ref, "private": repo.private, "files": files,
	})
}

func (s *stub) authorized(r *http.Request) bool {
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	return got == s.token
}

func (s *stub) getRepo(fullName string) (*repository, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	repo, ok := s.repos[fullName]
	return repo, ok
}

// pathSegments splits an escaped URL path into decoded, non-empty segments.
func pathSegments(escapedPath string) []string {
	raw := strings.Split(strings.TrimPrefix(escapedPath, "/"), "/")
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		if seg == "" {
			continue
		}
		decoded, err := url.PathUnescape(seg)
		if err != nil {
			decoded = seg
		}
		out = append(out, decoded)
	}
	return out
}

// blobSHA returns the git blob object id for content, matching `git hash-object`.
func blobSHA(content string) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(content))
	_, _ = h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// treeSHA derives a stable tree id from a directory's direct children.
func treeSHA(dir string, files map[string]string) string {
	type child struct{ name, sha string }
	var children []child
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	for path, content := range files {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		if strings.Contains(rest, "/") {
			continue // not a direct child
		}
		children = append(children, child{name: rest, sha: blobSHA(content)})
	}
	sort.Slice(children, func(i, j int) bool { return children[i].name < children[j].name })

	h := sha1.New()
	_, _ = fmt.Fprintf(h, "tree %d\x00", len(children))
	for _, c := range children {
		_, _ = fmt.Fprintf(h, "%s\x00%s\n", c.name, c.sha)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func parentDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return ""
	}
	return path[:idx]
}

func wrapBase64(encoded string) string {
	const width = 60
	var b strings.Builder
	for len(encoded) > width {
		b.WriteString(encoded[:width])
		b.WriteString("\n")
		encoded = encoded[width:]
	}
	b.WriteString(encoded)
	return b.String()
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeGitHubError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"message": message})
}
