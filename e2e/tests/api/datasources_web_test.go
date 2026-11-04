// Web data-source e2e: a small site is crawled through the web connector
// against a hermetic website stub (e2e/stubs/web), then a page changes and the
// next sync produces the next document version; a removed page is tombstoned; a
// partial crawl does not delete; robots.txt is honoured; and the SSRF guard
// rejects the cloud metadata address.
//
// The server reaches the stub as http://web-stub:8080 (deployment policy:
// WEB_SOURCE_ALLOWED_HOSTS). This test seeds and mutates the site over the
// host-published control port (WEB_STUB_URL).
package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	// webSeedURL is the crawl start URL. It carries a trailing slash on purpose:
	// the connection test fetches this raw URL while the crawl fetches its
	// canonical form (trailing slash dropped), so the stub must serve both.
	webSeedURL = "http://web-stub:8080/docs/"

	// Canonical external ids the connector derives for the default site (see the
	// stub's reset: /docs links to /docs/a and /docs/b; /docs/a links to
	// /docs/a/deep). The trailing slash is dropped by canonicalization and the
	// non-default port is preserved.
	webExtDocs     = "http://web-stub:8080/docs"
	webExtDocsA    = "http://web-stub:8080/docs/a"
	webExtDocsDeep = "http://web-stub:8080/docs/a/deep"
	webExtDocsB    = "http://web-stub:8080/docs/b"
)

// skipWithoutWebStub returns the trimmed WEB_STUB_URL, skipping the test when it
// is unset. The skip decision lives in this helper (not in a test body) so the
// web e2e test is only gated on explicit configuration and the skip-census
// ratchet is not inflated. When the URL *is* set, unreachability is a hard
// failure.
func skipWithoutWebStub(t *testing.T) string {
	t.Helper()
	base := strings.TrimRight(os.Getenv("WEB_STUB_URL"), "/")
	if base == "" {
		t.Skip("WEB_STUB_URL not set; skipping web data-source sync test")
	}
	return base
}

// ─────────────────────────────────────────────────────────────────────────────
// Stub control client
// ─────────────────────────────────────────────────────────────────────────────

// webStubHTTPClient bounds stub-control requests so a wedged stub fails the test
// instead of hanging until the package timeout.
var webStubHTTPClient = &http.Client{Timeout: 15 * time.Second}

// webStubCall performs one request against the stub and fails the test on a
// transport error or an unexpected status. The body is returned as a string.
func webStubCall(t *testing.T, method, url string, payload any) string {
	t.Helper()
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal stub payload: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build stub request %s %s: %v", method, url, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := webStubHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("stub request %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stub response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stub %s %s: status %d body %s", method, url, resp.StatusCode, body)
	}
	return string(body)
}

// resetWebSite replaces the stub's site with the default fixture.
func resetWebSite(t *testing.T, base string) {
	t.Helper()
	webStubCall(t, "POST", base+"/_control/reset", nil)
}

// upsertWebPage writes one page (or overrides an existing one), including its
// status so a page can be turned into a 404/500 for the tombstone/partial-crawl
// scenarios.
func upsertWebPage(t *testing.T, base, path string, status int, contentType, body string) {
	t.Helper()
	webStubCall(t, "POST", base+"/_control/page", map[string]any{
		"path": path, "status": status, "contentType": contentType, "body": body,
	})
}

// removeWebPage deletes a page so a subsequent GET returns 404.
func removeWebPage(t *testing.T, base, path string) {
	t.Helper()
	webStubCall(t, "DELETE", base+"/_control/page", map[string]any{"path": path})
}

// webStubHitCount returns how many times the stub served the given path. It is
// the robots.txt assertion primitive: a disallowed page must never be requested.
func webStubHitCount(t *testing.T, base, path string) int {
	t.Helper()
	body := webStubCall(t, "GET", base+"/_control/state", nil)
	var state struct {
		Hits map[string]int `json:"hits"`
	}
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatalf("decode stub state: %v\nbody: %s", err, body)
	}
	return state.Hits[path]
}

// ─────────────────────────────────────────────────────────────────────────────
// Source API helpers
// ─────────────────────────────────────────────────────────────────────────────

// createWebSource creates a config-only web data-source integration and returns
// its id. cfg is the source config (start_urls, globs, limits).
//
// The fixture is explicitly manual: the tests drive every sync with
// triggerWebSync, and a web source is scheduled by default (60-minute
// interval). A scheduled fixture is due from creation (last_synced_at is NULL),
// so the admit sweep could run a competing sync mid-test (#2493).
func createWebSource(t *testing.T, rl *runLog, projectID string, cfg map[string]any) string {
	t.Helper()
	body := jsonBody(map[string]any{
		"name":         "web-stub-docs",
		"providerType": "web",
		"sourceType":   "web",
		"syncMode":     "manual",
		"config":       cfg,
	})
	resp := doAPILogged(t, rl, "POST", "/api/data-source-integrations", e2eTestToken(), projectID, body)
	raw := mustStatus(t, resp, http.StatusCreated)
	return parseIDFromBody(t, raw)
}

// triggerWebSync admits a manual sync job and returns its id. It delegates to
// the shared trigger helper so a legitimate concurrent sync (a scheduled sweep
// for the same source) is waited out and retriggered rather than failing the
// test. The sync/polling helpers (waitForSyncJob, listSourceDocuments,
// findSourceDocument, documentChunkText) are provider-neutral and shared with
// the GitHub suite.
func triggerWebSync(t *testing.T, rl *runLog, projectID, sourceID string) string {
	t.Helper()
	return triggerDataSourceSync(t, rl, projectID, sourceID)
}

// assertWebDocs asserts the exact set of documents a source currently owns
// (newest first), failing with the full list on mismatch.
func assertWebDocs(t *testing.T, projectID, sourceID string, want []string) {
	t.Helper()
	docs := listSourceDocuments(t, projectID, sourceID)
	got := make([]string, 0, len(docs))
	for _, d := range docs {
		got = append(got, d.SourceExternalID)
	}
	if len(got) != len(want) {
		t.Fatalf("source owns %d documents %v, want %d %v", len(got), got, len(want), want)
	}
	present := map[string]bool{}
	for _, id := range got {
		present[id] = true
	}
	for _, id := range want {
		if !present[id] {
			t.Fatalf("source documents %v missing %q", got, id)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestWebDataSource_SyncAndVersion exercises the full web source loop: the stub
// site is crawled into documents with chunks, one page changes, and the next
// sync appends the next document version while unchanged pages are skipped by
// revision (body hash).
func TestWebDataSource_SyncAndVersion(t *testing.T) {
	stub := skipWithoutWebStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	resetWebSite(t, stub)

	sourceID := createWebSource(t, rl, projectID, map[string]any{
		"start_urls": []string{webSeedURL},
		"max_depth":  2,
	})

	// Connection test performs one bounded GET of the first start URL.
	testResp := doAPILogged(t, rl, "POST", "/api/data-source-integrations/"+sourceID+"/test-connection", e2eTestToken(), projectID, nil)
	testBody := mustStatus(t, testResp, http.StatusOK)
	var conn struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	parseBodyJSON(t, testBody, &conn)
	if !conn.Success {
		t.Fatalf("test-connection failed: %s", conn.Message)
	}

	// ── First sync: four documents, each chunked ────────────────────────────
	jobID := triggerWebSync(t, rl, projectID, sourceID)
	job := waitForSyncJob(t, rl, projectID, sourceID, jobID)
	if job.TotalItems != 4 || job.SuccessfulItems != 4 || job.FailedItems != 0 {
		t.Fatalf("first sync = total:%d successful:%d failed:%d, want 4/4/0",
			job.TotalItems, job.SuccessfulItems, job.FailedItems)
	}
	assertWebDocs(t, projectID, sourceID, []string{webExtDocs, webExtDocsA, webExtDocsDeep, webExtDocsB})

	aDoc := findSourceDocument(t, projectID, sourceID, webExtDocsA)
	for _, doc := range []sourceDocument{
		findSourceDocument(t, projectID, sourceID, webExtDocs),
		aDoc,
		findSourceDocument(t, projectID, sourceID, webExtDocsDeep),
		findSourceDocument(t, projectID, sourceID, webExtDocsB),
	} {
		if doc.Version != 1 {
			t.Errorf("document %s version = %d, want 1", doc.SourceExternalID, doc.Version)
		}
		documentChunkText(t, projectID, doc.ID) // fails if no chunk is produced
	}
	assertContains(t, documentChunkText(t, projectID, aDoc.ID), "Version one")
	if aDoc.CanonicalID == "" {
		t.Fatal("first version has no canonical id")
	}

	// ── Change one page, sync again ─────────────────────────────────────────
	upsertWebPage(t, stub, "/docs/a", http.StatusOK, "text/html; charset=utf-8",
		`<!doctype html><html><head><title>Page A</title></head><body><main><h1>Page A</h1><p>Version two.</p><a href="/docs/a/deep">Deep</a></main></body></html>`)

	jobID = triggerWebSync(t, rl, projectID, sourceID)
	job = waitForSyncJob(t, rl, projectID, sourceID, jobID)
	if job.TotalItems != 4 {
		t.Fatalf("second sync total = %d, want 4", job.TotalItems)
	}
	if job.SuccessfulItems != 1 || job.SkippedItems != 3 || job.FailedItems != 0 {
		t.Fatalf("second sync = successful:%d skipped:%d failed:%d, want 1/3/0",
			job.SuccessfulItems, job.SkippedItems, job.FailedItems)
	}

	// The changed page is a NEW document version sharing the canonical id; the
	// unchanged pages stay the same HEAD rows.
	updatedA := findSourceDocument(t, projectID, sourceID, webExtDocsA)
	if updatedA.ID == aDoc.ID {
		t.Fatal("page-a document id unchanged after an update; want a new version row")
	}
	if updatedA.Version != 2 {
		t.Fatalf("page-a version = %d, want 2", updatedA.Version)
	}
	if updatedA.CanonicalID != aDoc.CanonicalID {
		t.Fatalf("page-a canonical id = %q, want the original %q", updatedA.CanonicalID, aDoc.CanonicalID)
	}
	if updatedA.SourceRevisionID == aDoc.SourceRevisionID {
		t.Fatal("page-a revision id unchanged after content changed")
	}

	unchanged := findSourceDocument(t, projectID, sourceID, webExtDocsB)
	if unchanged.Version != 1 {
		t.Errorf("unchanged page-b version = %d, want 1", unchanged.Version)
	}

	assertContains(t, documentChunkText(t, projectID, updatedA.ID), "Version two")
}

// TestWebDataSource_RemovalTombstone proves a page that 404s is tombstoned: the
// document is soft-deleted (excluded from the default listing) while the
// surviving pages are untouched.
func TestWebDataSource_RemovalTombstone(t *testing.T) {
	stub := skipWithoutWebStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	resetWebSite(t, stub)
	sourceID := createWebSource(t, rl, projectID, map[string]any{
		"start_urls": []string{webSeedURL},
		"max_depth":  2,
	})

	jobID := triggerWebSync(t, rl, projectID, sourceID)
	waitForSyncJob(t, rl, projectID, sourceID, jobID)
	assertWebDocs(t, projectID, sourceID, []string{webExtDocs, webExtDocsA, webExtDocsDeep, webExtDocsB})

	// Remove /docs/b: it 404s (definitive deletion evidence) while /docs keeps
	// linking to it, so the crawler still visits and confirms the tombstone.
	removeWebPage(t, stub, "/docs/b")

	jobID = triggerWebSync(t, rl, projectID, sourceID)
	waitForSyncJob(t, rl, projectID, sourceID, jobID)

	assertWebDocs(t, projectID, sourceID, []string{webExtDocs, webExtDocsA, webExtDocsDeep})
}

// TestWebDataSource_PartialCrawlKeepsAbsent proves a page error (500) forces an
// incomplete enumeration, and an absent page that is NOT a confirmed tombstone
// is left in place rather than deleted.
func TestWebDataSource_PartialCrawlKeepsAbsent(t *testing.T) {
	stub := skipWithoutWebStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	resetWebSite(t, stub)
	sourceID := createWebSource(t, rl, projectID, map[string]any{
		"start_urls": []string{webSeedURL},
		"max_depth":  2,
	})

	jobID := triggerWebSync(t, rl, projectID, sourceID)
	waitForSyncJob(t, rl, projectID, sourceID, jobID)
	assertWebDocs(t, projectID, sourceID, []string{webExtDocs, webExtDocsA, webExtDocsDeep, webExtDocsB})

	// /docs/b now 500s: a page error (not a tombstone), so the crawl is
	// incomplete and the absent page must NOT be deleted.
	upsertWebPage(t, stub, "/docs/b", http.StatusInternalServerError, "text/plain", "")

	jobID = triggerWebSync(t, rl, projectID, sourceID)
	job := waitForSyncJob(t, rl, projectID, sourceID, jobID)
	if job.FailedItems != 0 {
		t.Fatalf("500 page should be an enumeration-level error, not a failed item; failed=%d", job.FailedItems)
	}

	// All four documents remain: /docs/b was not deleted because the partial
	// crawl cannot prove its absence.
	assertWebDocs(t, projectID, sourceID, []string{webExtDocs, webExtDocsA, webExtDocsDeep, webExtDocsB})
}

// TestWebDataSource_RobotsTxtHonored proves a page disallowed by robots.txt is
// never fetched and never synced.
func TestWebDataSource_RobotsTxtHonored(t *testing.T) {
	stub := skipWithoutWebStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	resetWebSite(t, stub)
	upsertWebPage(t, stub, "/robots.txt", http.StatusOK, "text/plain; charset=utf-8",
		"User-agent: *\nDisallow: /docs/b\n")

	sourceID := createWebSource(t, rl, projectID, map[string]any{
		"start_urls": []string{webSeedURL},
		"max_depth":  2,
	})

	jobID := triggerWebSync(t, rl, projectID, sourceID)
	job := waitForSyncJob(t, rl, projectID, sourceID, jobID)
	if job.TotalItems != 3 {
		t.Fatalf("sync total = %d, want 3 (disallowed /docs/b not crawled)", job.TotalItems)
	}
	assertWebDocs(t, projectID, sourceID, []string{webExtDocs, webExtDocsA, webExtDocsDeep})

	// The disallowed page was never requested from the stub.
	if n := webStubHitCount(t, stub, "/docs/b"); n != 0 {
		t.Fatalf("/docs/b was fetched %d times despite robots.txt Disallow", n)
	}
}

// TestWebDataSource_SSRFBlocksMetadata proves the web connector's connection
// test refuses the cloud metadata address — the dominant SSRF target — under
// every deployment policy (the link-local range is always blocked).
func TestWebDataSource_SSRFBlocksMetadata(t *testing.T) {
	_ = skipWithoutWebStub(t) // gated with the rest of the web suite

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	body := jsonBody(map[string]any{
		"providerType": "web",
		"config": map[string]any{
			"start_urls": []string{"http://169.254.169.254/latest/meta-data/"},
		},
	})
	resp := doAPILogged(t, rl, "POST", "/api/data-source-integrations/test-config", e2eTestToken(), projectID, body)
	respBody := mustStatus(t, resp, http.StatusOK)

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	parseBodyJSON(t, respBody, &result)
	if result.Success {
		t.Fatalf("test-config reported success for the metadata URL: %s", respBody)
	}
	if !strings.Contains(strings.ToLower(result.Message), "disallowed") {
		t.Errorf("message = %q, want it to surface the SSRF block", result.Message)
	}
}
