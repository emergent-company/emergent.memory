// Gmail data-source e2e: a mailbox served by a hermetic Gmail stub
// (e2e/stubs/gmail) is synced through the Gmail connector — OAuth2 refresh
// grant, label/thread listing, thread ingestion as one document per thread,
// revision-skip on re-sync, the two-pass backfill that resumes older windows,
// and retried throttling — with no Google account, credentials, or egress.
//
// The server reaches the stub as http://gmail-stub:8080 (GMAIL_API_BASE_URL)
// and refreshes its access token at http://gmail-stub:8080/token
// (GMAIL_TOKEN_URL). The stub only serves API calls carrying the bearer token
// it issued from /token, so a passing suite also proves the refresh seam works.
// This test seeds the mailbox, hides threads, injects throttles, and reads the
// request log through the stub's host-published /_control routes.
//
// The source is created over the API with its credential map in `config` (the
// same write-only ExtractCredentials path legacy/API clients use); the browser
// OAuth consent screen itself is covered by the Playwright connect-form spec,
// because Google's authorization-code exchange is not seam-overridable.
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

const (
	gmailStubRefreshToken = "e2e-gmail-refresh-token"
	gmailStubClientID     = "e2e-gmail-client-id"
	gmailStubClientSecret = "e2e-gmail-client-secret"
)

// skipWithoutGmailStub returns the trimmed GMAIL_STUB_URL, skipping the test
// when it is unset. The skip decision lives in this helper (not in a test body)
// so the Gmail e2e is only gated on explicit configuration and the skip-census
// ratchet is not inflated. When the URL *is* set, unreachability is a hard
// failure.
func skipWithoutGmailStub(t *testing.T) string {
	t.Helper()
	base := strings.TrimRight(os.Getenv("GMAIL_STUB_URL"), "/")
	if base == "" {
		t.Skip("GMAIL_STUB_URL not set; skipping Gmail data-source e2e")
	}
	return base
}

// ─────────────────────────────────────────────────────────────────────────────
// Stub control client
// ─────────────────────────────────────────────────────────────────────────────

// gmailStubHTTPClient bounds stub-control requests so a wedged stub fails the
// test instead of hanging until the package timeout.
var gmailStubHTTPClient = &http.Client{Timeout: 15 * time.Second}

// gmailStubCall performs one control request against the stub and fails the
// test on a transport error or an unexpected status. The body is returned as a
// string.
func gmailStubCall(t *testing.T, method, target string, payload any) string {
	t.Helper()
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal stub payload: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatalf("build stub request %s %s: %v", method, target, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := gmailStubHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("stub request %s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stub response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stub %s %s: status %d body %s", method, target, resp.StatusCode, body)
	}
	return string(body)
}

// gmailSeedThread is one thread handed to the stub.
type gmailSeedThread struct {
	ID       string             `json:"id"`
	Snippet  string             `json:"snippet,omitempty"`
	Labels   []string           `json:"labels,omitempty"`
	Hidden   bool               `json:"hidden,omitempty"`
	Messages []gmailSeedMessage `json:"messages,omitempty"`
}

// gmailSeedMessage is one message handed to the stub. DaysAgo places it at
// midday UTC, which keeps it clear of the connector's day-granular window
// boundaries.
type gmailSeedMessage struct {
	ID      string `json:"id,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Subject string `json:"subject,omitempty"`
	DaysAgo int    `json:"daysAgo,omitempty"`
	Text    string `json:"text,omitempty"`
	HTML    string `json:"html,omitempty"`
}

// seedGmailMailbox replaces the stub's mailbox with the given threads.
func seedGmailMailbox(t *testing.T, base string, threads []gmailSeedThread) {
	t.Helper()
	gmailStubCall(t, "POST", base+"/_control/reset", map[string]any{
		"threads":      threads,
		"refreshToken": gmailStubRefreshToken,
	})
}

// upsertGmailThread appends/replaces a thread, bumping its history id (the
// connector's revision) exactly as a new message or relabel does.
func upsertGmailThread(t *testing.T, base string, thread gmailSeedThread) {
	t.Helper()
	gmailStubCall(t, "POST", base+"/_control/threads", thread)
}

// hideGmailThread drops a thread from every listing while keeping it fetchable,
// which models an archive or relabel — not a deletion.
func hideGmailThread(t *testing.T, base, id string) {
	t.Helper()
	gmailStubCall(t, "POST", base+"/_control/threads", map[string]any{
		"id": id, "hidden": true,
	})
}

// armGmailFailure makes the next `count` calls of one kind fail, so the suite
// can exercise the connector's retry path against a real HTTP response.
func armGmailFailure(t *testing.T, base, kind string, status, count int, retryAfter string) {
	t.Helper()
	gmailStubCall(t, "POST", base+"/_control/fail", map[string]any{
		"kind": kind, "status": status, "count": count, "retryAfter": retryAfter,
	})
}

type gmailStubRequest struct {
	N      int    `json:"n"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Query  string `json:"query"`
	Kind   string `json:"kind"`
	Status int    `json:"status"`
}

// gmailStubRequests returns the stub's API requests newer than cursor,
// optionally filtered by kind/path substring, plus the next cursor.
func gmailStubRequests(t *testing.T, base string, cursor int, contains string) ([]gmailStubRequest, int) {
	t.Helper()
	target := fmt.Sprintf("%s/_control/requests?since=%d", base, cursor)
	if contains != "" {
		target += "&contains=" + url.QueryEscape(contains)
	}
	raw := gmailStubCall(t, "GET", target, nil)
	var result struct {
		Next     int                `json:"next"`
		Requests []gmailStubRequest `json:"requests"`
	}
	parseBodyJSON(t, raw, &result)
	return result.Requests, result.Next
}

// gmailGrantCount reports how many refresh grants the stub has served.
func gmailGrantCount(t *testing.T, base string) int {
	t.Helper()
	raw := gmailStubCall(t, "GET", base+"/_control/state", nil)
	var state struct {
		Grants int `json:"grants"`
	}
	parseBodyJSON(t, raw, &state)
	return state.Grants
}

// gmailWindowBounds extracts the after:/before: calendar bounds from a listing
// request's q parameter. ok is false for the head pass (no bounds).
func gmailWindowBounds(t *testing.T, req gmailStubRequest) (after, before time.Time, ok bool) {
	t.Helper()
	values, err := url.ParseQuery(req.Query)
	if err != nil {
		t.Fatalf("decode stub query %q: %v", req.Query, err)
	}
	after = gmailQueryDate(values.Get("q"), "after:")
	before = gmailQueryDate(values.Get("q"), "before:")
	return after, before, !after.IsZero() && !before.IsZero()
}

func gmailQueryDate(q, prefix string) time.Time {
	for _, field := range strings.Fields(q) {
		if !strings.HasPrefix(field, prefix) {
			continue
		}
		if day, err := time.Parse("2006/01/02", strings.TrimPrefix(field, prefix)); err == nil {
			return day
		}
	}
	return time.Time{}
}

// ─────────────────────────────────────────────────────────────────────────────
// Source API helpers
// ─────────────────────────────────────────────────────────────────────────────

// gmailSourceConfig is the create body's non-secret config plus the write-only
// credential map, which the server encrypts and strips.
func gmailSourceConfig(extra map[string]any) map[string]any {
	cfg := map[string]any{
		"refresh_token": gmailStubRefreshToken,
		"client_id":     gmailStubClientID,
		"client_secret": gmailStubClientSecret,
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

// createGmailSource creates a Gmail source and returns its id. It asserts the
// write-only credentials are never echoed back.
//
// The fixture is explicitly manual: the tests drive every sync with
// triggerDataSourceSync, and a Gmail source is scheduled by default. A
// scheduled fixture is due from creation (last_synced_at is NULL), so the admit
// sweep could run a competing sync mid-test (#2493).
func createGmailSource(t *testing.T, rl *runLog, projectID, name string, config map[string]any) string {
	t.Helper()
	body := jsonBody(map[string]any{
		"name":         name,
		"providerType": "gmail",
		"sourceType":   "email",
		"syncMode":     "manual",
		"config":       config,
	})
	resp := doAPILogged(t, rl, "POST", "/api/data-source-integrations", e2eTestToken(), projectID, body)
	raw := mustStatus(t, resp, http.StatusCreated)
	for _, secret := range []string{gmailStubRefreshToken, gmailStubClientSecret} {
		if strings.Contains(raw, secret) {
			t.Fatalf("create response echoed a credential (%q): %s", secret, raw)
		}
	}
	return parseIDFromBody(t, raw)
}

// assertGmailSourceConfig proves the non-secret config round-trips while the
// credential map stays write-only.
func assertGmailSourceConfig(t *testing.T, rl *runLog, projectID, sourceID string, want map[string]any) {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/data-source-integrations/"+sourceID, e2eTestToken(), projectID, nil)
	raw := mustStatus(t, resp, http.StatusOK)
	for _, secret := range []string{gmailStubRefreshToken, gmailStubClientSecret} {
		if strings.Contains(raw, secret) {
			t.Fatalf("source read exposed a credential (%q): %s", secret, raw)
		}
	}
	var src struct {
		Config map[string]any `json:"config"`
	}
	parseBodyJSON(t, raw, &src)
	for key, wantValue := range want {
		if got := fmt.Sprint(src.Config[key]); got != fmt.Sprint(wantValue) {
			t.Errorf("source config[%q] = %q, want %q", key, got, fmt.Sprint(wantValue))
		}
	}
}

// assertGmailConnection runs the source's connection test, which reaches the
// stub's users/me/profile with the token from the refresh grant.
func assertGmailConnection(t *testing.T, rl *runLog, projectID, sourceID string) {
	t.Helper()
	resp := doAPILogged(t, rl, "POST", "/api/data-source-integrations/"+sourceID+"/test-connection", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var conn struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	parseBodyJSON(t, body, &conn)
	if !conn.Success {
		t.Fatalf("test-connection failed: %s", conn.Message)
	}
}

// assertSyncCounts fails with the job's message when the counters differ.
func assertSyncCounts(t *testing.T, job dataSourceSyncJob, total, successful, skipped, failed int) {
	t.Helper()
	if job.TotalItems == total && job.SuccessfulItems == successful &&
		job.SkippedItems == skipped && job.FailedItems == failed {
		return
	}
	msg := ""
	if job.ErrorMessage != nil {
		msg = *job.ErrorMessage
	}
	t.Fatalf("sync = total:%d successful:%d skipped:%d failed:%d, want %d/%d/%d/%d (error: %s)",
		job.TotalItems, job.SuccessfulItems, job.SkippedItems, job.FailedItems,
		total, successful, skipped, failed, msg)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestGmailDataSource_SyncRevisionsAndVanishedThread exercises the full Gmail
// source loop: two in-scope threads are ingested as one document each, a
// re-sync skips them by history id, a new message produces the next document
// version, and a thread that drops out of the listing deletes nothing.
func TestGmailDataSource_SyncRevisionsAndVanishedThread(t *testing.T) {
	stub := skipWithoutGmailStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	// Two inbox threads and one SENT thread. The default query is in:inbox, so
	// the out-of-scope thread must never be ingested.
	seedGmailMailbox(t, stub, []gmailSeedThread{
		{
			ID: "thread-plan", Snippet: "Quarterly plan", Labels: []string{"INBOX"},
			Messages: []gmailSeedMessage{{
				ID: "msg-plan-1", From: "alice@example.com", To: "e2e@example.com",
				Subject: "Quarterly plan", DaysAgo: 1,
				Text: "Ship the roadmap by Friday.\n",
				HTML: "<p>Ship the roadmap by Friday.</p>",
			}},
		},
		{
			ID: "thread-invoice", Snippet: "Invoice 42", Labels: []string{"INBOX"},
			Messages: []gmailSeedMessage{{
				ID: "msg-invoice-1", From: "billing@example.com", To: "e2e@example.com",
				Subject: "Invoice 42", DaysAgo: 2,
				Text: "Payment received.\n",
			}},
		},
		{
			ID: "thread-outbound", Snippet: "Outbound", Labels: []string{"SENT"},
			Messages: []gmailSeedMessage{{
				ID: "msg-outbound-1", From: "e2e@example.com", To: "alice@example.com",
				Subject: "Re: Quarterly plan", DaysAgo: 3,
				Text: "Sounds good.\n",
			}},
		},
	})

	sourceID := createGmailSource(t, rl, projectID,
		fmt.Sprintf("gmail-e2e-%d", time.Now().UnixMilli()),
		gmailSourceConfig(map[string]any{"query": "in:inbox"}))
	assertGmailSourceConfig(t, rl, projectID, sourceID, map[string]any{"query": "in:inbox"})
	assertGmailConnection(t, rl, projectID, sourceID)

	// ── First sync: two in-scope threads, one document each ─────────────────
	job := waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 2, 2, 0, 0)

	docs := listSourceDocuments(t, projectID, sourceID)
	if len(docs) != 2 {
		t.Fatalf("first sync produced %d documents, want 2: %+v", len(docs), docs)
	}
	plan := findSourceDocument(t, projectID, sourceID, "thread-plan")
	invoice := findSourceDocument(t, projectID, sourceID, "thread-invoice")
	for _, doc := range []sourceDocument{plan, invoice} {
		if doc.Version != 1 {
			t.Errorf("document %s version = %d, want 1", doc.SourceExternalID, doc.Version)
		}
	}
	planText := documentChunkText(t, projectID, plan.ID)
	assertContains(t, planText, "Quarterly plan")
	assertContains(t, planText, "Ship the roadmap by Friday.")
	assertContains(t, documentChunkText(t, projectID, invoice.ID), "Payment received.")

	// ── Re-sync: unchanged history ids are skipped without a fetch ──────────
	job = waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 2, 0, 2, 0)
	if again := findSourceDocument(t, projectID, sourceID, "thread-plan"); again.ID != plan.ID {
		t.Fatal("unchanged thread was re-ingested; want it skipped by revision")
	}

	// ── New message: the next document version shares the canonical id ──────
	upsertGmailThread(t, stub, gmailSeedThread{
		ID: "thread-plan", Labels: []string{"INBOX"},
		Messages: []gmailSeedMessage{
			{ID: "msg-plan-1", Subject: "Quarterly plan", DaysAgo: 2, Text: "Ship the roadmap by Friday.\n"},
			{ID: "msg-plan-2", Subject: "Re: Quarterly plan", DaysAgo: 1, Text: "Shipped — follow-up scheduled.\n"},
		},
	})
	job = waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 2, 1, 1, 0)

	updated := findSourceDocument(t, projectID, sourceID, "thread-plan")
	if updated.ID == plan.ID {
		t.Fatal("thread document id unchanged after a new message; want a new version row")
	}
	if updated.Version != 2 {
		t.Fatalf("thread version = %d, want 2", updated.Version)
	}
	if updated.CanonicalID != plan.CanonicalID {
		t.Fatalf("canonical id = %q, want the original %q", updated.CanonicalID, plan.CanonicalID)
	}
	if updated.SourceRevisionID == plan.SourceRevisionID {
		t.Fatal("revision id unchanged after the thread gained a message")
	}
	assertContains(t, documentChunkText(t, projectID, updated.ID), "follow-up scheduled")

	// ── Vanished thread: absence from the listing is not a deletion ─────────
	hideGmailThread(t, stub, "thread-invoice")
	job = waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 1, 0, 1, 0)

	// The hidden thread's document survives at version 1 (no tombstone, no new
	// version), because a Gmail listing is a filter over a live mailbox.
	surviving := findSourceDocument(t, projectID, sourceID, "thread-invoice")
	if surviving.ID != invoice.ID || surviving.Version != 1 {
		t.Fatalf("vanished thread document = %s v%d, want %s v1 (no delete, no new version)",
			surviving.ID, surviving.Version, invoice.ID)
	}
	if got := len(listSourceDocuments(t, projectID, sourceID)); got != 2 {
		t.Fatalf("documents after the thread vanished = %d, want 2 (nothing deleted)", got)
	}
}

// TestGmailDataSource_BackfillResumesOlderWindows proves the two-pass walk: a
// mailbox larger than max_threads syncs its newest threads first, persists the
// watermark, and the next sync lists an older window and ingests it.
func TestGmailDataSource_BackfillResumesOlderWindows(t *testing.T) {
	stub := skipWithoutGmailStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	// Four inbox threads: two in the newest 30-day window, two in the one
	// before it. max_threads=2 keeps the head pass capped.
	seedGmailMailbox(t, stub, []gmailSeedThread{
		{
			ID: "thread-recent-a", Snippet: "Recent A", Labels: []string{"INBOX"},
			Messages: []gmailSeedMessage{{Subject: "Recent A", DaysAgo: 1, Text: "newest thread\n"}},
		},
		{
			ID: "thread-recent-b", Snippet: "Recent B", Labels: []string{"INBOX"},
			Messages: []gmailSeedMessage{{Subject: "Recent B", DaysAgo: 2, Text: "second newest\n"}},
		},
		{
			ID: "thread-older-a", Snippet: "Older A", Labels: []string{"INBOX"},
			Messages: []gmailSeedMessage{{Subject: "Older A", DaysAgo: 45, Text: "older window\n"}},
		},
		{
			ID: "thread-older-b", Snippet: "Older B", Labels: []string{"INBOX"},
			Messages: []gmailSeedMessage{{Subject: "Older B", DaysAgo: 46, Text: "older window\n"}},
		},
	})

	sourceID := createGmailSource(t, rl, projectID,
		fmt.Sprintf("gmail-backfill-%d", time.Now().UnixMilli()),
		gmailSourceConfig(map[string]any{"query": "in:inbox", "max_threads": 2}))
	assertGmailConnection(t, rl, projectID, sourceID)

	// ── Sync 1: capped head pass only ──────────────────────────────────────
	cursor := 0
	job := waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 2, 2, 0, 0)
	if got := len(listSourceDocuments(t, projectID, sourceID)); got != 2 {
		t.Fatalf("first sync documents = %d, want the 2 newest", got)
	}

	firstRequests, cursor := gmailStubRequests(t, stub, cursor, "threads.list")
	var firstWindowBefore time.Time
	sawWindow := false
	for _, req := range firstRequests {
		after, before, ok := gmailWindowBounds(t, req)
		if !ok {
			continue
		}
		sawWindow = true
		if !firstWindowBefore.IsZero() && before.After(firstWindowBefore) {
			t.Fatalf("first sync listed windows out of order: %v then %v", firstWindowBefore, before)
		}
		firstWindowBefore = before
		if after.After(before) {
			t.Fatalf("window after:%v is newer than before:%v", after, before)
		}
	}
	if !sawWindow {
		t.Fatalf("first sync issued no backfill window query: %+v", firstRequests)
	}

	// ── Sync 2: the watermark resumes one window older ─────────────────────
	job = waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 4, 2, 2, 0)

	// Both older threads now exist as documents, and the newest ones were
	// re-listed but skipped by revision.
	findSourceDocument(t, projectID, sourceID, "thread-older-a")
	findSourceDocument(t, projectID, sourceID, "thread-older-b")
	if got := len(listSourceDocuments(t, projectID, sourceID)); got != 4 {
		t.Fatalf("second sync documents = %d, want 4", got)
	}

	secondRequests, _ := gmailStubRequests(t, stub, cursor, "threads.list")
	var resumed bool
	for _, req := range secondRequests {
		_, before, ok := gmailWindowBounds(t, req)
		if !ok {
			continue
		}
		// One calendar day of overlap keeps the boundary thread from being
		// dropped, so "older" is before the first sync's window start.
		if before.Before(firstWindowBefore) {
			resumed = true
		}
	}
	if !resumed {
		t.Fatalf("second sync never listed a window older than before:%s: %+v",
			firstWindowBefore.Format("2006/01/02"), secondRequests)
	}
}

// TestGmailDataSource_RetriesThrottledCalls proves a throttled page and a
// throttled thread fetch are retried in place instead of failing the sync or
// the item: the stub answers 429 with Retry-After once per kind, and the job
// still completes with every item ingested.
func TestGmailDataSource_RetriesThrottledCalls(t *testing.T) {
	stub := skipWithoutGmailStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	seedGmailMailbox(t, stub, []gmailSeedThread{
		{
			ID: "thread-throttle-a", Snippet: "Throttled A", Labels: []string{"INBOX"},
			Messages: []gmailSeedMessage{{Subject: "Throttled A", DaysAgo: 1, Text: "quota-bound thread\n"}},
		},
		{
			ID: "thread-throttle-b", Snippet: "Throttled B", Labels: []string{"INBOX"},
			Messages: []gmailSeedMessage{{Subject: "Throttled B", DaysAgo: 2, Text: "quota-bound thread\n"}},
		},
	})

	sourceID := createGmailSource(t, rl, projectID,
		fmt.Sprintf("gmail-retry-%d", time.Now().UnixMilli()),
		gmailSourceConfig(map[string]any{"query": "in:inbox"}))

	// One throttled listing (the head pass) and one throttled fetch.
	armGmailFailure(t, stub, "threads.list", http.StatusTooManyRequests, 1, "1")
	armGmailFailure(t, stub, "threads.get", http.StatusTooManyRequests, 1, "1")

	job := waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 2, 2, 0, 0)
	if got := len(listSourceDocuments(t, projectID, sourceID)); got != 2 {
		t.Fatalf("documents after throttled sync = %d, want 2", got)
	}

	// The stub's log shows the rejected attempt and the successful retry, for
	// both the listing and the thread fetch. This is the regression signal: a
	// connector that failed on the first 429 would show one request per kind
	// and a failed job.
	listRequests, _ := gmailStubRequests(t, stub, 0, "threads.list")
	if !hasGmailStatus(listRequests, http.StatusTooManyRequests) || !hasGmailStatus(listRequests, http.StatusOK) {
		t.Fatalf("listing requests = %+v, want a 429 followed by a 200", listRequests)
	}
	getRequests, _ := gmailStubRequests(t, stub, 0, "threads.get")
	if !hasGmailStatus(getRequests, http.StatusTooManyRequests) || !hasGmailStatus(getRequests, http.StatusOK) {
		t.Fatalf("thread fetch requests = %+v, want a 429 followed by a 200", getRequests)
	}

	// The API calls carried a token the stub issued: the refresh seam ran
	// against GMAIL_TOKEN_URL, not Google.
	if grants := gmailGrantCount(t, stub); grants == 0 {
		t.Fatal("stub served no refresh grant; the connector did not use GMAIL_TOKEN_URL")
	}
}

func hasGmailStatus(requests []gmailStubRequest, status int) bool {
	for _, req := range requests {
		if req.Status == status {
			return true
		}
	}
	return false
}

// listTypedSourceObjects returns graph objects of one type for a project.
func listTypedSourceObjects(t *testing.T, rl *runLog, projectID, typ string) []map[string]any {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search?type="+url.QueryEscape(typ), e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var out struct {
		Items []map[string]any `json:"items"`
	}
	parseBodyJSON(t, body, &out)
	return out.Items
}

// TestGmailDataSource_TypedObjectsAndMCPTools proves the typed-sync path and the
// per-account MCP surface: EmailMessage/EmailThread objects are created, the
// account's read tools are listed on the official /api/mcp endpoint, and write
// tools stay hidden until the source opts in.
func TestGmailDataSource_TypedObjectsAndMCPTools(t *testing.T) {
	stub := skipWithoutGmailStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	seedGmailMailbox(t, stub, []gmailSeedThread{{
		ID: "thread-typed", Snippet: "Typed metadata", Labels: []string{"INBOX", "CATEGORY_PERSONAL"},
		Messages: []gmailSeedMessage{{
			ID: "msg-typed-1", From: "alice@example.com", To: "e2e@example.com",
			Subject: "Typed metadata", DaysAgo: 1, Text: "Structured body.\n",
		}},
	}})

	sourceID := createGmailSource(t, rl, projectID,
		fmt.Sprintf("gmail-typed-%d", time.Now().UnixMilli()),
		gmailSourceConfig(map[string]any{
			"filter":       map[string]any{"categories": []any{"Primary"}},
			"object_types": []any{"EmailMessage", "EmailThread"},
			"mcp_alias":    "e2e",
		}))

	job := waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 1, 1, 0, 0)

	msgs := listTypedSourceObjects(t, rl, projectID, "EmailMessage")
	if len(msgs) != 1 {
		t.Fatalf("EmailMessage objects = %d, want 1: %+v", len(msgs), msgs)
	}
	props, _ := msgs[0]["properties"].(map[string]any)
	if got := fmt.Sprint(props["subject"]); got != "Typed metadata" {
		t.Errorf("EmailMessage.subject = %q", got)
	}
	if got := fmt.Sprint(props["threadId"]); got != "thread-typed" {
		t.Errorf("EmailMessage.threadId = %q", got)
	}
	threads := listTypedSourceObjects(t, rl, projectID, "EmailThread")
	if len(threads) != 1 {
		t.Fatalf("EmailThread objects = %d, want 1", len(threads))
	}

	// Official MCP surface: the account's read tools are listed with the slug.
	sessionID := initAPIUnifiedMCPSession(t, projectID)
	listResp := framework.DoMCPJSON(t, serverURL()+"/api/mcp", e2eTestToken(), projectID, sessionID, "2025-11-25",
		jsonBody(map[string]any{"jsonrpc": "2.0", "method": "tools/list", "id": 2}))
	listBody := mustStatus(t, listResp, http.StatusOK)
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	parseBodyJSON(t, listBody, &listed)
	names := map[string]bool{}
	for _, tool := range listed.Result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"gmail-e2e-message-search", "gmail-e2e-thread-get", "gmail-e2e-label-list"} {
		if !names[want] {
			t.Errorf("tools/list missing %q", want)
		}
	}
	if names["gmail-e2e-message-send"] {
		t.Error("write tool listed before the source opted in")
	}

	// Read tool executes through the source credentials.
	res := callAPIMCPTool(t, projectID, sessionID, "gmail-e2e-message-search", map[string]any{"query": "in:inbox"})
	if txt := fmt.Sprint(res); !strings.Contains(txt, "thread-typed") {
		t.Fatalf("message-search result = %s, want thread-typed", txt)
	}

	// Opting in enables the write surface (email:write is held by the test token).
	resp := doAPILogged(t, rl, "PATCH", "/api/data-source-integrations/"+sourceID, e2eTestToken(), projectID,
		jsonBody(map[string]any{"config": map[string]any{
			"filter":                  map[string]any{"categories": []any{"Primary"}},
			"object_types":            []any{"EmailMessage", "EmailThread"},
			"mcp_alias":               "e2e",
			"mcp_write_tools_enabled": true,
		}}))
	mustStatus(t, resp, http.StatusOK)

	listResp = framework.DoMCPJSON(t, serverURL()+"/api/mcp", e2eTestToken(), projectID, sessionID, "2025-11-25",
		jsonBody(map[string]any{"jsonrpc": "2.0", "method": "tools/list", "id": 3}))
	listBody = mustStatus(t, listResp, http.StatusOK)
	listed = struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}{}
	parseBodyJSON(t, listBody, &listed)
	names = map[string]bool{}
	for _, tool := range listed.Result.Tools {
		names[tool.Name] = true
	}
	if !names["gmail-e2e-message-send"] {
		t.Error("write tool not listed after opting in")
	}

	sendRes := callAPIMCPTool(t, projectID, sessionID, "gmail-e2e-message-send", map[string]any{
		"to": "bob@example.com", "subject": "Hello", "text": "Body",
	})
	if txt := fmt.Sprint(sendRes); !strings.Contains(txt, "sent-1") {
		t.Fatalf("message-send result = %s, want sent-1", txt)
	}
	draftRes := callAPIMCPTool(t, projectID, sessionID, "gmail-e2e-draft-create", map[string]any{
		"to": "bob@example.com", "subject": "Draft", "text": "Draft body",
	})
	if txt := fmt.Sprint(draftRes); !strings.Contains(txt, "draft-1") {
		t.Fatalf("draft-create result = %s, want draft-1", txt)
	}
	draftSendRes := callAPIMCPTool(t, projectID, sessionID, "gmail-e2e-draft-send", map[string]any{"draft_id": "draft-1"})
	if txt := fmt.Sprint(draftSendRes); !strings.Contains(txt, "sent-draft-1") {
		t.Fatalf("draft-send result = %s, want sent-draft-1", txt)
	}
}

// TestGmailDataSource_AgentScopeChange proves an agent can narrow/adjust the
// sync scope through the MCP tools: preview, hash-guarded update, then trigger.
func TestGmailDataSource_AgentScopeChange(t *testing.T) {
	stub := skipWithoutGmailStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)

	seedGmailMailbox(t, stub, []gmailSeedThread{
		{
			ID: "thread-primary", Snippet: "Primary mail", Labels: []string{"INBOX", "CATEGORY_PERSONAL"},
			Messages: []gmailSeedMessage{{ID: "msg-primary", Subject: "Primary mail", DaysAgo: 1, Text: "primary\n"}},
		},
		{
			ID: "thread-updates", Snippet: "Update mail", Labels: []string{"INBOX", "CATEGORY_UPDATES"},
			Messages: []gmailSeedMessage{{ID: "msg-updates", Subject: "Update mail", DaysAgo: 2, Text: "updates\n"}},
		},
	})

	sourceID := createGmailSource(t, rl, projectID,
		fmt.Sprintf("gmail-scope-%d", time.Now().UnixMilli()),
		gmailSourceConfig(map[string]any{
			"filter":       map[string]any{"categories": []any{"Primary"}},
			"object_types": []any{"EmailMessage", "EmailThread"},
			"mcp_alias":    "scope",
		}))

	job := waitForSyncJob(t, rl, projectID, sourceID, triggerDataSourceSync(t, rl, projectID, sourceID))
	assertSyncCounts(t, job, 1, 1, 0, 0)
	if got := len(listSourceDocuments(t, projectID, sourceID)); got != 1 {
		t.Fatalf("primary sync documents = %d, want 1", got)
	}

	sessionID := initAPIUnifiedMCPSession(t, projectID)

	listRes := parseAPIMCPToolResultText(t, callAPIMCPTool(t, projectID, sessionID, "source-list", map[string]any{}))
	listText := fmt.Sprint(listRes["sources"])
	if !strings.Contains(listText, sourceID) {
		t.Fatalf("source-list did not include %s: %s", sourceID, listText)
	}

	preview := parseAPIMCPToolResultText(t, callAPIMCPTool(t, projectID, sessionID, "source-scope-preview", map[string]any{
		"source_id":    sourceID,
		"filter":       map[string]any{"categories": []any{"Updates"}},
		"object_types": []any{"EmailMessage", "EmailThread"},
	}))
	previewHash, _ := preview["previewHash"].(string)
	baseHash, _ := preview["baseScopeHash"].(string)
	if previewHash == "" || baseHash == "" {
		t.Fatalf("preview hashes missing: %v", preview)
	}

	// No hashes: hard reject.
	noHash := callAPIMCPTool(t, projectID, sessionID, "source-scope-update", map[string]any{
		"source_id": sourceID,
		"filter":    map[string]any{"categories": []any{"Updates"}},
	})
	if noHash["error"] == nil {
		t.Fatal("source-scope-update accepted a request without preview hashes")
	}

	updated := parseAPIMCPToolResultText(t, callAPIMCPTool(t, projectID, sessionID, "source-scope-update", map[string]any{
		"source_id":       sourceID,
		"filter":          map[string]any{"categories": []any{"Updates"}},
		"object_types":    []any{"EmailMessage", "EmailThread"},
		"preview_hash":    previewHash,
		"base_scope_hash": baseHash,
	}))
	if changed, _ := updated["changed"].(bool); !changed {
		t.Fatalf("scope update reported no change: %v", updated)
	}

	trigger := parseAPIMCPToolResultText(t, callAPIMCPTool(t, projectID, sessionID, "source-sync-trigger", map[string]any{"source_id": sourceID}))
	jobID, _ := trigger["jobId"].(string)
	if jobID == "" {
		t.Fatalf("source-sync-trigger returned no job: %v", trigger)
	}
	job = waitForSyncJob(t, rl, projectID, sourceID, jobID)
	assertSyncCounts(t, job, 1, 1, 0, 0)
	if got := len(listSourceDocuments(t, projectID, sourceID)); got != 2 {
		t.Fatalf("documents after scope change = %d, want 2 (old retained, new ingested)", got)
	}
	findSourceDocument(t, projectID, sourceID, "thread-updates")
}

// TestGmailDataSource_ScopeAPI proves the narrow scope API: preview returns
// hashes, a hash-guarded update persists only scope keys, and a stale base hash
// conflicts.
func TestGmailDataSource_ScopeAPI(t *testing.T) {
	stub := skipWithoutGmailStub(t)

	rl := newRunLog(t)
	skipIfServerDown(t, rl)
	projectID, _ := setupProjectLogged(t, rl)
	seedGmailMailbox(t, stub, nil)

	sourceID := createGmailSource(t, rl, projectID,
		fmt.Sprintf("gmail-scope-api-%d", time.Now().UnixMilli()),
		gmailSourceConfig(map[string]any{
			"filter":       map[string]any{"categories": []any{"Primary"}},
			"object_types": []any{"EmailMessage"},
			"mcp_alias":    "keepme",
		}))

	previewResp := doAPILogged(t, rl, "POST", "/api/data-source-integrations/"+sourceID+"/scope/preview", e2eTestToken(), projectID,
		jsonBody(map[string]any{"filter": map[string]any{"categories": []any{"Updates"}}, "objectTypes": []any{"EmailMessage", "EmailThread"}}))
	previewBody := mustStatus(t, previewResp, http.StatusOK)
	var preview struct {
		CompiledQuery string `json:"compiledQuery"`
		PreviewHash   string `json:"previewHash"`
		BaseScopeHash string `json:"baseScopeHash"`
	}
	parseBodyJSON(t, previewBody, &preview)
	if preview.CompiledQuery == "" || preview.PreviewHash == "" || preview.BaseScopeHash == "" {
		t.Fatalf("preview = %+v", preview)
	}

	updateResp := doAPILogged(t, rl, "PATCH", "/api/data-source-integrations/"+sourceID+"/scope", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"filter":        map[string]any{"categories": []any{"Updates"}},
			"objectTypes":   []any{"EmailMessage", "EmailThread"},
			"previewHash":   preview.PreviewHash,
			"baseScopeHash": preview.BaseScopeHash,
		}))
	updateBody := mustStatus(t, updateResp, http.StatusOK)
	var updated struct {
		Changed bool `json:"changed"`
	}
	parseBodyJSON(t, updateBody, &updated)
	if !updated.Changed {
		t.Fatalf("update did not report a change: %s", updateBody)
	}

	getResp := doAPILogged(t, rl, "GET", "/api/data-source-integrations/"+sourceID, e2eTestToken(), projectID, nil)
	getBody := mustStatus(t, getResp, http.StatusOK)
	var src struct {
		Config map[string]any `json:"config"`
	}
	parseBodyJSON(t, getBody, &src)
	if fmt.Sprint(src.Config["filter"]) == "" || !strings.Contains(fmt.Sprint(src.Config["filter"]), "Updates") {
		t.Fatalf("stored filter not updated: %v", src.Config["filter"])
	}
	if fmt.Sprint(src.Config["mcp_alias"]) != "keepme" {
		t.Fatalf("non-scope config was clobbered: %v", src.Config)
	}

	// The generic update path must not bypass the scope guard.
	bypass := doAPILogged(t, rl, "PATCH", "/api/data-source-integrations/"+sourceID, e2eTestToken(), projectID,
		jsonBody(map[string]any{"config": map[string]any{"filter": map[string]any{"categories": []any{"Social"}}}}))
	mustStatus(t, bypass, http.StatusBadRequest)

	staleResp := doAPILogged(t, rl, "PATCH", "/api/data-source-integrations/"+sourceID+"/scope", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"filter":        map[string]any{"categories": []any{"Primary"}},
			"previewHash":   preview.PreviewHash,
			"baseScopeHash": preview.BaseScopeHash,
		}))
	mustStatus(t, staleResp, http.StatusConflict)
}
