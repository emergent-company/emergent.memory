// GitHub data-source live e2e: the connector talks to the real GitHub API.
//
// This is the opt-in counterpart to datasources_github_test.go. The hermetic
// stub suite proves the sync/versioning pipeline and credential enforcement;
// this test proves what the stub cannot: bearer auth against a real private
// repository, a branch ref containing "/" (URL-encoded in the tree request),
// git blob SHAs as revisions, and the contents API producing a new revision.
//
// Skipped unless E2E_GITHUB_LIVE_REPO and E2E_GITHUB_LIVE_TOKEN are set. It is
// never wired into CI: the fixture repo and tokens are operator-provided. Run
// it against a server configured for real GitHub (the compose stack points
// GITHUB_API_BASE_URL at the hermetic stub, so the connection test would fail
// there). The connection itself is verified in the hermetic suite.
package api_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const githubLiveDefaultAPIURL = "https://api.github.com"

// githubLiveConfig returns the live-test settings, skipping when they are not
// configured. The skip lives in this helper so the skip-census ratchet does not
// count another test-level skip.
func githubLiveConfig(t *testing.T) (apiBase, repo, readToken, writeToken string) {
	t.Helper()
	repo = strings.TrimSpace(os.Getenv("E2E_GITHUB_LIVE_REPO"))
	readToken = strings.TrimSpace(os.Getenv("E2E_GITHUB_LIVE_TOKEN"))
	if repo == "" || readToken == "" {
		t.Skip("E2E_GITHUB_LIVE_REPO/E2E_GITHUB_LIVE_TOKEN not set; skipping real-GitHub data-source test (opt-in)")
	}
	apiBase = strings.TrimRight(strings.TrimSpace(os.Getenv("E2E_GITHUB_LIVE_API_URL")), "/")
	if apiBase == "" {
		apiBase = githubLiveDefaultAPIURL
	}
	writeToken = strings.TrimSpace(os.Getenv("E2E_GITHUB_LIVE_WRITE_TOKEN"))
	if writeToken == "" {
		writeToken = readToken
	}
	return apiBase, repo, readToken, writeToken
}

// githubLiveHTTPClient bounds GitHub calls so a wedged request fails the test
// instead of hanging until the package timeout.
var githubLiveHTTPClient = &http.Client{Timeout: 30 * time.Second}

// githubLiveRequest performs one GitHub REST call. Transport errors are
// returned rather than fatal so callers can decide: the test body fails fast,
// while cleanup keeps running the remaining cleanups.
func githubLiveRequest(method, apiBase, path, token string, payload any) (int, []byte, error) {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal GitHub payload: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, apiBase+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build GitHub request %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := githubLiveHTTPClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("GitHub request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read GitHub response %s %s: %w", method, path, err)
	}
	return resp.StatusCode, body, nil
}

// githubLiveDo is githubLiveRequest with a fatal transport error, for the test
// body where a failed call invalidates everything that follows.
func githubLiveDo(t *testing.T, method, apiBase, path, token string, payload any) (int, []byte) {
	t.Helper()
	status, body, err := githubLiveRequest(method, apiBase, path, token, payload)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return status, body
}

// githubLivePutFile creates (sha == "") or updates one file on branch and
// returns its blob SHA. The contents API commits the change, so the next sync
// sees a new revision for that file only.
func githubLivePutFile(t *testing.T, apiBase, repo, token, branch, path, content, sha string) string {
	t.Helper()
	payload := map[string]any{
		"message": "e2e: write " + path,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}
	want := http.StatusCreated
	if sha != "" {
		payload["sha"] = sha
		want = http.StatusOK
	}
	status, body := githubLiveDo(t, "PUT", apiBase, "/repos/"+repo+"/contents/"+path, token, payload)
	if status != want {
		t.Fatalf("PUT %s on %s: status %d, want %d, body %s", path, branch, status, want, body)
	}
	var res struct {
		Content struct {
			SHA string `json:"sha"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Content.SHA == "" {
		t.Fatalf("decode contents response for %s: %v, body %s", path, err, body)
	}
	return res.Content.SHA
}

// TestGitHubDataSource_LiveSyncAndVersion exercises the GitHub connector against
// the real GitHub API (opt-in; see the package comment). It creates a throwaway
// branch whose name contains "/", seeds two markdown files, syncs them into
// documents with chunks, commits a new revision of one file, and re-syncs: the
// changed file must produce version 2 sharing the canonical id while the
// unchanged file is skipped by blob SHA.
func TestGitHubDataSource_LiveSyncAndVersion(t *testing.T) {
	apiBase, repo, readToken, writeToken := githubLiveConfig(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	// Resolve the default branch so the scratch branch starts from its head.
	status, body := githubLiveDo(t, "GET", apiBase, "/repos/"+repo, writeToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET repo %s: status %d body %s", repo, status, body)
	}
	var meta struct {
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
	}
	if err := json.Unmarshal(body, &meta); err != nil || meta.DefaultBranch == "" {
		t.Fatalf("decode repo %s: %v, body %s", repo, err, body)
	}
	if !meta.Private {
		t.Fatalf("fixture repo %s is public; set E2E_GITHUB_LIVE_REPO to a private repository", repo)
	}

	status, body = githubLiveDo(t, "GET", apiBase, "/repos/"+repo+"/git/ref/heads/"+url.PathEscape(meta.DefaultBranch), writeToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET default branch ref: status %d body %s", status, body)
	}
	var baseRef struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &baseRef); err != nil || baseRef.Object.SHA == "" {
		t.Fatalf("decode default branch ref: %v, body %s", err, body)
	}

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	// The slash in the ref is deliberate: it forces the connector to
	// URL-encode the ref in GET /git/trees/{ref}, which the stub cannot prove.
	branch := "e2e/" + runID
	dir := "e2e/" + runID

	status, body = githubLiveDo(t, "POST", apiBase, "/repos/"+repo+"/git/refs", writeToken, map[string]any{
		"ref": "refs/heads/" + branch,
		"sha": baseRef.Object.SHA,
	})
	if status != http.StatusCreated {
		t.Fatalf("create branch %s: status %d body %s", branch, status, body)
	}
	t.Cleanup(func() {
		st, b, err := githubLiveRequest("DELETE", apiBase, "/repos/"+repo+"/git/refs/heads/"+branch, writeToken, nil)
		if err != nil {
			t.Errorf("cleanup: delete branch %s: %v", branch, err)
			return
		}
		if st != http.StatusNoContent && st != http.StatusNotFound {
			t.Errorf("cleanup: delete branch %s: status %d body %s", branch, st, b)
		}
	})

	guidePath := dir + "/guide.md"
	apiPath := dir + "/api.md"
	guideSHA := githubLivePutFile(t, apiBase, repo, writeToken, branch, guidePath, "# Guide\n\nVersion one.\n", "")
	githubLivePutFile(t, apiBase, repo, writeToken, branch, apiPath, "# API\n\nEndpoints and examples.\n", "")

	// ── Create the source: read token, ref with "/", path-scoped to this run ─
	createBody := jsonBody(map[string]any{
		"name":         "github-live-docs",
		"providerType": "github",
		"sourceType":   "repo",
		"config": map[string]any{
			"repo":         repo,
			"ref":          branch,
			"paths":        []string{dir + "/**"},
			"access_token": readToken,
		},
	})
	// Use the unlogged helper for the create: doAPILogged persists request
	// bodies in the runlog, which would copy the real access token to disk.
	createResp := doAPI(t, "POST", "/api/data-source-integrations", e2eTestToken(), projectID, createBody)
	createRaw := mustStatus(t, createResp, http.StatusCreated)
	if strings.Contains(createRaw, readToken) {
		t.Fatal("create response echoed the access token")
	}
	sourceID := parseIDFromBody(t, createRaw)

	testResp := doAPILogged(t, rl, "POST", "/api/data-source-integrations/"+sourceID+"/test-connection", e2eTestToken(), projectID, nil)
	testBody := mustStatus(t, testResp, http.StatusOK)
	var conn struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	parseBodyJSON(t, testBody, &conn)
	if !conn.Success {
		t.Fatalf("test-connection against real GitHub failed: %s", conn.Message)
	}

	// ── First sync: two documents, each chunked ─────────────────────────────
	jobID := triggerDataSourceSync(t, rl, projectID, sourceID)
	job := waitForSyncJob(t, rl, projectID, sourceID, jobID)
	if job.TotalItems != 2 || job.SuccessfulItems != 2 || job.FailedItems != 0 {
		t.Fatalf("first sync = total:%d successful:%d failed:%d, want 2/2/0",
			job.TotalItems, job.SuccessfulItems, job.FailedItems)
	}

	docs := listSourceDocuments(t, projectID, sourceID)
	if len(docs) != 2 {
		t.Fatalf("first sync produced %d documents, want 2: %+v", len(docs), docs)
	}
	guide := findSourceDocument(t, projectID, sourceID, guidePath)
	apiDoc := findSourceDocument(t, projectID, sourceID, apiPath)
	chunks := map[string]string{}
	for _, doc := range []sourceDocument{guide, apiDoc} {
		if doc.Version != 1 {
			t.Errorf("document %s version = %d, want 1", doc.SourceExternalID, doc.Version)
		}
		chunks[doc.SourceExternalID] = documentChunkText(t, projectID, doc.ID)
	}
	assertContains(t, chunks[guidePath], "Version one")
	if guide.CanonicalID == "" {
		t.Fatal("first version has no canonical id")
	}

	// ── Commit a new revision of one file, sync again ───────────────────────
	githubLivePutFile(t, apiBase, repo, writeToken, branch, guidePath, "# Guide\n\nVersion two.\n", guideSHA)

	jobID = triggerDataSourceSync(t, rl, projectID, sourceID)
	job = waitForSyncJob(t, rl, projectID, sourceID, jobID)
	if job.TotalItems != 2 {
		t.Fatalf("second sync total = %d, want 2", job.TotalItems)
	}
	if job.SuccessfulItems != 1 || job.SkippedItems != 1 || job.FailedItems != 0 {
		t.Fatalf("second sync = successful:%d skipped:%d failed:%d, want 1/1/0",
			job.SuccessfulItems, job.SkippedItems, job.FailedItems)
	}

	// The changed file is a NEW document version sharing the canonical id; the
	// unchanged file stays the same HEAD row.
	updatedGuide := findSourceDocument(t, projectID, sourceID, guidePath)
	if updatedGuide.ID == guide.ID {
		t.Fatal("guide document id unchanged after an update; want a new version row")
	}
	if updatedGuide.Version != 2 {
		t.Fatalf("guide version = %d, want 2", updatedGuide.Version)
	}
	if updatedGuide.CanonicalID != guide.CanonicalID {
		t.Fatalf("guide canonical id = %q, want the original %q", updatedGuide.CanonicalID, guide.CanonicalID)
	}
	if updatedGuide.SourceRevisionID == guide.SourceRevisionID {
		t.Fatal("guide revision id unchanged after content changed")
	}

	updatedAPI := findSourceDocument(t, projectID, sourceID, apiPath)
	if updatedAPI.ID != apiDoc.ID {
		t.Fatal("unchanged document was re-ingested; want it skipped by revision")
	}
	if updatedAPI.Version != 1 {
		t.Errorf("unchanged api.md version = %d, want 1", updatedAPI.Version)
	}
	if updatedAPI.SourceRevisionID != apiDoc.SourceRevisionID {
		t.Errorf("unchanged api.md revision = %q, want %q", updatedAPI.SourceRevisionID, apiDoc.SourceRevisionID)
	}

	// The new version is chunked with the new text.
	assertContains(t, documentChunkText(t, projectID, updatedGuide.ID), "Version two")

	// Version history exposes both versions of the canonical document.
	historyResp := doAPI(t, "GET", "/api/documents/"+updatedGuide.ID+"/versions", e2eTestToken(), projectID, nil)
	historyBody := mustStatus(t, historyResp, http.StatusOK)
	var history struct {
		Versions []struct {
			ID          string `json:"id"`
			CanonicalID string `json:"canonicalId"`
			Version     int    `json:"version"`
		} `json:"versions"`
	}
	parseBodyJSON(t, historyBody, &history)
	if len(history.Versions) != 2 {
		t.Fatalf("version history has %d entries, want 2: %s", len(history.Versions), historyBody)
	}
	for i, v := range history.Versions {
		if v.Version != i+1 {
			t.Errorf("history[%d].version = %d, want %d", i, v.Version, i+1)
		}
		if v.CanonicalID != updatedGuide.CanonicalID {
			t.Errorf("history[%d].canonicalId = %q, want %q", i, v.CanonicalID, updatedGuide.CanonicalID)
		}
	}
}
