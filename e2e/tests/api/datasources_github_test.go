// GitHub data-source e2e: a private repository is synced through the GitHub
// connector against a hermetic REST wrapper (e2e/stubs/github), then a document
// changes and the next sync produces the next document version.
//
// The stub impersonates a *private* repo: every /repos request needs the bearer
// token, so the create/test/sync path exercises real credential handling. The
// server reaches the stub as http://github-stub:8080 (GITHUB_API_BASE_URL);
// this test seeds and mutates the repo over the host-published control port.
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	githubStubToken = "e2e-github-token"
	// dataSourceSyncTimeout covers the worker's 5s poll interval plus ingestion and
	// chunking for a handful of documents.
	dataSourceSyncTimeout = 90 * time.Second
)

// skipWithoutGitHubStub returns the trimmed GITHUB_STUB_URL, skipping the test
// when it is unset. The skip decision lives in this helper (not in a test body)
// so the GitHub e2e test is only gated on explicit configuration and the
// skip-census ratchet is not inflated. When the URL *is* set, unreachability is
// a hard failure.
func skipWithoutGitHubStub(t *testing.T) string {
	t.Helper()
	base := strings.TrimRight(os.Getenv("GITHUB_STUB_URL"), "/")
	if base == "" {
		t.Skip("GITHUB_STUB_URL not set; skipping GitHub data-source sync test")
	}
	return base
}

// ─────────────────────────────────────────────────────────────────────────────
// Stub control client
// ─────────────────────────────────────────────────────────────────────────────

// githubStubHTTPClient bounds stub-control requests so a wedged stub fails the
// test instead of hanging until the package timeout.
var githubStubHTTPClient = &http.Client{Timeout: 15 * time.Second}

// githubStubCall performs one request against the stub and fails the test on a
// transport error or an unexpected status. The body is returned as a string.
func githubStubCall(t *testing.T, method, url string, payload any) string {
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
	resp, err := githubStubHTTPClient.Do(req)
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

type githubSeedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// seedGitHubRepo replaces the stub's repository with the given files at ref.
func seedGitHubRepo(t *testing.T, base, repo, ref string, files []githubSeedFile) {
	t.Helper()
	githubStubCall(t, "POST", base+"/_control/reset", map[string]any{
		"repo": repo, "ref": ref, "files": files,
	})
}

// upsertGitHubFile writes one file, producing a new blob SHA (a new revision)
// exactly as a commit would.
func upsertGitHubFile(t *testing.T, base, repo, path, content string) {
	t.Helper()
	githubStubCall(t, "POST", base+"/_control/files", map[string]any{
		"repo": repo, "path": path, "content": content,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Source API helpers
// ─────────────────────────────────────────────────────────────────────────────

type dataSourceSyncJob struct {
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	TotalItems      int     `json:"totalItems"`
	ProcessedItems  int     `json:"processedItems"`
	SuccessfulItems int     `json:"successfulItems"`
	FailedItems     int     `json:"failedItems"`
	SkippedItems    int     `json:"skippedItems"`
	StatusMessage   *string `json:"statusMessage,omitempty"`
	ErrorMessage    *string `json:"errorMessage,omitempty"`
	// TriggerType and CreatedAt are diagnostic only: a conflict that had to be
	// waited out is attributable to a scheduled sweep or a manual trigger from
	// these two fields in the run log (#2493).
	TriggerType string `json:"triggerType"`
	CreatedAt   string `json:"createdAt"`
}

type sourceDocument struct {
	ID               string `json:"id"`
	Filename         string `json:"filename"`
	Version          int    `json:"version"`
	CanonicalID      string `json:"canonicalId"`
	SourceID         string `json:"sourceId"`
	SourceExternalID string `json:"sourceExternalId"`
	SourceRevisionID string `json:"sourceRevisionId"`
	SourceStatus     string `json:"sourceStatus"`
	Chunks           int    `json:"chunks"`
}

// triggerSyncConflictRetryBudget is how many trigger attempts a lost
// single-active-job race may consume: attempt 1, then up to two
// wait-for-the-active-job-and-retry rounds. The wait for one job is itself
// bounded by dataSourceSyncTimeout, so an exhausted budget means the source kept
// being re-admitted, not that a job hung.
const triggerSyncConflictRetryBudget = 3

// triggerSyncResponse is the POST /sync result the trigger helpers act on.
// A conflict (success=false) still carries the active job id, which is what
// makes a legitimate concurrent sync waitable instead of a test failure.
type triggerSyncResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	JobID   *string `json:"jobId"`
}

// triggerSyncWithRetry runs trigger until it admits a sync, waiting out a
// legitimate concurrent sync between attempts.
//
// The server admits at most one active (pending/processing) sync job per source
// (idx_source_sync_jobs_active_source), so a trigger that finds one returns
// success=false with that job's id rather than an error. That is not a test
// failure: the scheduler's admit sweep can legitimately have a job in flight
// for a scheduled source (the web provider is scheduled by default). Wait for
// the active job to finish, then retrigger, bounded by maxAttempts.
//
// A conflict with no job id cannot be waited out, and a budget that runs out
// while the source keeps reporting an active job, both fail with the reason —
// the failure stays loud. trigger and wait are injected so the retry policy is
// unit-testable without a server; wait returns once the active job reached a
// terminal state (any status — the slot is free again) and fails the test only
// if that job never finishes.
func triggerSyncWithRetry(trigger func() (triggerSyncResponse, error), wait func(jobID string), maxAttempts int) (string, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var last triggerSyncResponse
	lastActive := ""
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := trigger()
		if err != nil {
			return "", err
		}
		last = result
		jobID := ""
		if result.JobID != nil {
			jobID = *result.JobID
		}
		if jobID != "" {
			lastActive = jobID
		}
		switch {
		case result.Success && jobID != "":
			return jobID, nil
		case result.Success:
			return "", fmt.Errorf("trigger sync: success=true without a job id")
		case jobID == "":
			return "", fmt.Errorf("trigger sync: success=false message=%q with no active job id to wait for", result.Message)
		}
		if attempt == maxAttempts {
			break
		}
		wait(jobID)
	}
	return "", fmt.Errorf("trigger sync: sync still in progress after %d attempt(s) (last message %q, active job %s)",
		maxAttempts, last.Message, lastActive)
}

// triggerDataSourceSync admits a manual sync job and returns its id. It is
// shared by every data-source suite (GitHub, Gmail, web) and tolerates a
// legitimate concurrent sync by waiting out the active job and retriggering
// (see triggerSyncWithRetry), so two syncs in a row cannot flake the suite.
func triggerDataSourceSync(t *testing.T, rl *runLog, projectID, sourceID string) string {
	t.Helper()
	jobID, err := triggerSyncWithRetry(
		func() (triggerSyncResponse, error) {
			resp := doAPILogged(t, rl, "POST", "/api/data-source-integrations/"+sourceID+"/sync", e2eTestToken(), projectID, nil)
			body := mustStatus(t, resp, http.StatusOK)
			var result triggerSyncResponse
			parseBodyJSON(t, body, &result)
			return result, nil
		},
		func(activeJobID string) {
			rl.LogStep("sync already in progress; waiting for the active job", map[string]any{
				"active_job_id": activeJobID,
			})
			// Any terminal status frees the admission slot, so a drained job
			// that ended failed/cancelled is not fatal here: the retrigger below
			// is the real attempt, and its own outcome is asserted by the caller.
			drained := pollSyncJob(t, projectID, sourceID, activeJobID)
			details := map[string]any{
				"job_id":       drained.ID,
				"status":       drained.Status,
				"trigger_type": drained.TriggerType,
				"created_at":   drained.CreatedAt,
				"total_items":  drained.TotalItems,
			}
			if drained.ErrorMessage != nil {
				details["error_message"] = *drained.ErrorMessage
			}
			rl.LogStep("active sync finished; retriggering", details)
		},
		triggerSyncConflictRetryBudget,
	)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return jobID
}

// syncJobFreedAdmission reports whether a sync job's status frees the
// per-source admission slot. The partial unique index blocks only pending and
// processing jobs, so every terminal status does — including failed and
// cancelled. That is why a drained concurrent job does not have to have
// succeeded before the helper retriggers.
func syncJobFreedAdmission(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

// pollSyncJob polls a sync job until it reports a terminal status and returns
// it. It fails the test only when the job never finishes: the caller decides
// whether completed vs failed/cancelled is acceptable (a concurrent job being
// drained just needs to free the admission slot).
func pollSyncJob(t *testing.T, projectID, sourceID, jobID string) dataSourceSyncJob {
	t.Helper()
	path := fmt.Sprintf("/api/data-source-integrations/%s/sync-jobs/%s", sourceID, jobID)
	deadline := time.Now().Add(dataSourceSyncTimeout)
	var last dataSourceSyncJob
	for time.Now().Before(deadline) {
		resp := doAPI(t, "GET", path, e2eTestToken(), projectID, nil)
		body := mustStatus(t, resp, http.StatusOK)
		if err := json.Unmarshal([]byte(body), &last); err != nil {
			t.Fatalf("decode sync job: %v\nbody: %s", err, body)
		}
		if syncJobFreedAdmission(last.Status) {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("sync job %s did not finish within %s (last status %q)", jobID, dataSourceSyncTimeout, last.Status)
	return last
}

// waitForSyncJob polls a sync job to completion and fails on failure.
func waitForSyncJob(t *testing.T, rl *runLog, projectID, sourceID, jobID string) dataSourceSyncJob {
	t.Helper()
	last := pollSyncJob(t, projectID, sourceID, jobID)
	if last.Status != "completed" {
		msg := ""
		if last.ErrorMessage != nil {
			msg = *last.ErrorMessage
		}
		t.Fatalf("sync job %s ended %s: %s", last.ID, last.Status, msg)
	}
	rl.LogStep("sync completed", map[string]any{
		"job_id":           last.ID,
		"trigger_type":     last.TriggerType,
		"created_at":       last.CreatedAt,
		"total_items":      last.TotalItems,
		"successful_items": last.SuccessfulItems,
		"skipped_items":    last.SkippedItems,
		"failed_items":     last.FailedItems,
	})
	return last
}

// listSourceDocuments returns the documents a source owns, newest first.
func listSourceDocuments(t *testing.T, projectID, sourceID string) []sourceDocument {
	t.Helper()
	resp := doAPI(t, "GET", "/api/documents?limit=500", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result struct {
		Documents []sourceDocument `json:"documents"`
	}
	parseBodyJSON(t, body, &result)
	out := make([]sourceDocument, 0, len(result.Documents))
	for _, doc := range result.Documents {
		if doc.SourceID == sourceID {
			out = append(out, doc)
		}
	}
	return out
}

// findSourceDocument returns the HEAD document for a repository path.
func findSourceDocument(t *testing.T, projectID, sourceID, externalID string) sourceDocument {
	t.Helper()
	for _, doc := range listSourceDocuments(t, projectID, sourceID) {
		if doc.SourceExternalID == externalID {
			return doc
		}
	}
	t.Fatalf("no document for source external id %q (source %s)", externalID, sourceID)
	return sourceDocument{}
}

// documentChunkText polls for chunks and returns their concatenated text.
func documentChunkText(t *testing.T, projectID, documentID string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp := doAPI(t, "GET", "/api/chunks?documentId="+documentID, e2eTestToken(), projectID, nil)
		body := mustStatus(t, resp, http.StatusOK)
		var result struct {
			Data []struct {
				Text string `json:"text"`
			} `json:"data"`
		}
		parseBodyJSON(t, body, &result)
		if len(result.Data) > 0 {
			var b strings.Builder
			for _, chunk := range result.Data {
				b.WriteString(chunk.Text)
				b.WriteString("\n")
			}
			return b.String()
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("document %s never produced chunks", documentID)
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestGitHubDataSource_SyncAndVersion exercises the full GitHub source loop:
// a private repo is seeded with markdown docs, the source syncs them into
// documents with chunks, one file changes, and the next sync appends the next
// document version while unchanged files are skipped by revision.
func TestGitHubDataSource_SyncAndVersion(t *testing.T) {
	stub := skipWithoutGitHubStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	timestamp := time.Now().UnixMilli()
	repo := fmt.Sprintf("acme/private-docs-%d", timestamp)

	// A private repository with three in-scope markdown files and two
	// out-of-scope files (wrong extension / non-markdown asset).
	seedGitHubRepo(t, stub, repo, "main", []githubSeedFile{
		{Path: "README.md", Content: "# Private Docs\n\nEntry point.\n"},
		{Path: "docs/guide.md", Content: "# Guide\n\nInstall the thing.\n\nVersion one.\n"},
		{Path: "docs/api.md", Content: "# API\n\nEndpoints and examples.\n"},
		{Path: "notes.txt", Content: "not markdown\n"},
		{Path: "assets/diagram.png", Content: "\x89PNG\r\n"},
	})

	// ── Create the source (token is write-only) ─────────────────────────────
	createBody := jsonBody(map[string]any{
		"name":         "github-private-docs",
		"providerType": "github",
		"sourceType":   "repo",
		"config": map[string]any{
			"repo":         repo,
			"access_token": githubStubToken,
		},
	})
	createResp := doAPILogged(t, rl, "POST", "/api/data-source-integrations", e2eTestToken(), projectID, createBody)
	createRaw := mustStatus(t, createResp, http.StatusCreated)
	if strings.Contains(createRaw, githubStubToken) {
		t.Fatalf("create response echoed the access token: %s", createRaw)
	}
	sourceID := parseIDFromBody(t, createRaw)

	// Connection test validates the repo/ref against the private stub.
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

	// ── First sync: three documents, each chunked ───────────────────────────
	jobID := triggerDataSourceSync(t, rl, projectID, sourceID)
	job := waitForSyncJob(t, rl, projectID, sourceID, jobID)
	if job.TotalItems != 3 || job.SuccessfulItems != 3 || job.FailedItems != 0 {
		t.Fatalf("first sync = total:%d successful:%d failed:%d, want 3/3/0",
			job.TotalItems, job.SuccessfulItems, job.FailedItems)
	}

	docs := listSourceDocuments(t, projectID, sourceID)
	if len(docs) != 3 {
		t.Fatalf("first sync produced %d documents, want 3: %+v", len(docs), docs)
	}

	guide := findSourceDocument(t, projectID, sourceID, "docs/guide.md")
	apiDoc := findSourceDocument(t, projectID, sourceID, "docs/api.md")
	readme := findSourceDocument(t, projectID, sourceID, "README.md")
	for _, doc := range []sourceDocument{guide, apiDoc, readme} {
		if doc.Version != 1 {
			t.Errorf("document %s version = %d, want 1", doc.SourceExternalID, doc.Version)
		}
		// Fails the test if no chunk is ever produced for the document.
		documentChunkText(t, projectID, doc.ID)
	}
	assertContains(t, documentChunkText(t, projectID, guide.ID), "Version one")
	if guide.CanonicalID == "" {
		t.Fatal("first version has no canonical id")
	}

	// ── Change one file, sync again ─────────────────────────────────────────
	upsertGitHubFile(t, stub, repo, "docs/guide.md", "# Guide\n\nInstall the thing.\n\nVersion two.\n")

	jobID = triggerDataSourceSync(t, rl, projectID, sourceID)
	job = waitForSyncJob(t, rl, projectID, sourceID, jobID)
	if job.TotalItems != 3 {
		t.Fatalf("second sync total = %d, want 3", job.TotalItems)
	}
	if job.SuccessfulItems != 1 || job.SkippedItems != 2 || job.FailedItems != 0 {
		t.Fatalf("second sync = successful:%d skipped:%d failed:%d, want 1/2/0",
			job.SuccessfulItems, job.SkippedItems, job.FailedItems)
	}

	// The changed file is a NEW document version sharing the canonical id; the
	// unchanged files are the same HEAD rows.
	updatedGuide := findSourceDocument(t, projectID, sourceID, "docs/guide.md")
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

	updatedAPI := findSourceDocument(t, projectID, sourceID, "docs/api.md")
	updatedReadme := findSourceDocument(t, projectID, sourceID, "README.md")
	if updatedAPI.ID != apiDoc.ID || updatedReadme.ID != readme.ID {
		t.Fatal("unchanged documents were re-ingested; want them skipped by revision")
	}
	if updatedAPI.Version != 1 {
		t.Errorf("unchanged api.md version = %d, want 1", updatedAPI.Version)
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

// TestGitHubDataSource_RejectsBadToken proves the connector actually
// authenticates: a wrong token against the private stub fails the connection
// test instead of silently listing nothing.
func TestGitHubDataSource_RejectsBadToken(t *testing.T) {
	stub := skipWithoutGitHubStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	repo := fmt.Sprintf("acme/private-docs-badtoken-%d", time.Now().UnixMilli())
	seedGitHubRepo(t, stub, repo, "main", []githubSeedFile{
		{Path: "README.md", Content: "# Private Docs\n"},
	})

	body := jsonBody(map[string]any{
		"providerType": "github",
		"config": map[string]any{
			"repo":         repo,
			"access_token": "definitely-not-the-token",
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
		t.Fatalf("test-config reported success with a bad token: %s", respBody)
	}
	if !strings.Contains(strings.ToLower(result.Message), "bad credentials") {
		t.Errorf("message = %q, want it to surface GitHub's Bad credentials", result.Message)
	}
}
