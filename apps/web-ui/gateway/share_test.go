package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- public agent-share test harness -----------------------------------------

// newShareTestServer wires the public share route group against a fakeMemory and
// a configured cookie/ref secret, mirroring the routes registered in main.go.
func newShareTestServer(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{ShareCookieSecret: "test-cookie-secret", ShareRefSecret: "test-ref-secret"}, memory: f}
	e := echo.New()
	share := e.Group("/share")
	share.GET("/api/sessions/:id", s.shareGetSession)
	share.POST("/api/sessions/:id/archive", s.shareArchiveSession)
	share.POST("/api/sessions/:id/unarchive", s.shareUnarchiveSession)
	share.DELETE("/api/sessions/:id", s.shareDeleteSession)
	share.POST("/api/sessions/:id/approvals/:questionId/cancel", s.shareApproveCancel)
	share.GET("/api/partial/sessions", s.sharePartialSessions)
	return s, e
}

// shareTestCookie seals a share cookie for the test server's secret so a request
// passes shareClaimsOrFail.
func shareTestCookie(t *testing.T) *http.Cookie {
	t.Helper()
	sealed, err := sealShareCookie("test-cookie-secret", shareCookieClaims{Token: "key-123", EndUserRef: "user-1"})
	if err != nil {
		t.Fatalf("sealShareCookie: %v", err)
	}
	return &http.Cookie{Name: shareCookieName, Value: sealed}
}

// --- rewriteShareStream ------------------------------------------------------

// TestRewriteShareStreamTokenPassthrough asserts the share SSE proxy emits a
// leading meta frame, forwards `token` frames unchanged (wire parity with
// /api/chat — never a share-only `delta`), annotates known error codes, and
// emits the terminal html markdown snapshot.
func TestRewriteShareStreamTokenPassthrough(t *testing.T) {
	in := "data: {\"type\":\"token\",\"token\":\"Hel\"}\n\n" +
		"data: {\"type\":\"token\",\"token\":\"lo\"}\n\n" +
		"data: {\"type\":\"error\",\"error\":\"share_budget_exceeded: out of budget\"}\n\n" +
		"data: {\"type\":\"done\"}\n\n"

	var out bytes.Buffer
	if err := rewriteShareStream(&out, strings.NewReader(in), "sess-1"); err != nil {
		t.Fatalf("rewriteShareStream: %v", err)
	}
	got := out.String()

	for _, want := range []string{
		`"type":"meta"`,
		`"sessionId":"sess-1"`,
		`"type":"token"`,
		`"token":"Hel"`,
		`"token":"lo"`,
		`"type":"error"`,
		`"code":"budget-exceeded"`,
		`"type":"html"`,
		`"type":"done"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten stream missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"delta"`) {
		t.Errorf("rewritten stream must not emit a share-only `delta` field:\n%s", got)
	}
}

// TestRewriteShareStreamUnrecognizedEventPassesThrough asserts a non-JSON data
// event is re-emitted verbatim rather than dropped.
func TestRewriteShareStreamUnrecognizedEventPassesThrough(t *testing.T) {
	in := "data: not-json\n\n"
	var out bytes.Buffer
	if err := rewriteShareStream(&out, strings.NewReader(in), "sess-1"); err != nil {
		t.Fatalf("rewriteShareStream: %v", err)
	}
	if !strings.Contains(out.String(), "data: not-json") {
		t.Errorf("unrecognized event must pass through verbatim:\n%s", out.String())
	}
}

// TestRewriteShareStreamAskUserQuestion asserts the share stream applies the same
// ask_user → client `question` translation as /api/chat: the paused question
// renders as an interactive card instead of a raw tool chip, and the two raw
// mcp_tool frames are suppressed.
func TestRewriteShareStreamAskUserQuestion(t *testing.T) {
	in := "data: {\"type\":\"mcp_tool\",\"tool\":\"ask_user\",\"status\":\"started\",\"result\":{\"question\":\"Scope?\",\"options\":[{\"label\":\"Full\",\"value\":\"full\",\"description\":\"everything\"}],\"interaction_type\":\"select\"}}\n\n" +
		"data: {\"type\":\"mcp_tool\",\"tool\":\"ask_user\",\"status\":\"completed\",\"result\":{\"question_id\":\"q1\",\"status\":\"pausing\"}}\n\n"

	var out bytes.Buffer
	if err := rewriteShareStream(&out, strings.NewReader(in), "sess-1"); err != nil {
		t.Fatalf("rewriteShareStream: %v", err)
	}
	got := out.String()

	var q questionEvent
	found := false
	for _, frame := range strings.Split(got, "\n\n") {
		frame = strings.TrimSpace(frame)
		payload, ok := strings.CutPrefix(frame, "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if !strings.Contains(payload, `"type":"question"`) {
			continue
		}
		if err := json.Unmarshal([]byte(payload), &q); err != nil {
			t.Fatalf("question payload not JSON: %v (%s)", err, payload)
		}
		found = true
	}
	if !found {
		t.Fatalf("no question event emitted:\n%s", got)
	}
	if q.Type != "question" || q.QuestionID != "q1" || q.Question != "Scope?" || q.InteractionType != "select" {
		t.Errorf("question fields lost: %+v", q)
	}
	if len(q.Options) != 1 || q.Options[0].Value != "full" {
		t.Errorf("options lost: %+v", q.Options)
	}
	if strings.Contains(got, `"tool":"ask_user"`) {
		t.Errorf("raw ask_user tool frames must be suppressed:\n%s", got)
	}
}

// TestRewriteShareStreamNonAskUserToolKept asserts a non-ask_user mcp_tool frame
// is NOT swallowed by the ask_user translation — it is still rewritten to the
// tool-result HTML frame so its chip renders.
func TestRewriteShareStreamNonAskUserToolKept(t *testing.T) {
	in := "data: {\"type\":\"mcp_tool\",\"id\":\"t1\",\"tool\":\"web_search\",\"status\":\"completed\",\"result\":{\"items\":[]}}\n\n"

	var out bytes.Buffer
	if err := rewriteShareStream(&out, strings.NewReader(in), "sess-1"); err != nil {
		t.Fatalf("rewriteShareStream: %v", err)
	}
	got := out.String()
	if strings.Contains(got, `"type":"question"`) {
		t.Errorf("non-ask_user tool must not become a question:\n%s", got)
	}
	if !strings.Contains(got, "web_search") {
		t.Errorf("non-ask_user tool frame must still be forwarded:\n%s", got)
	}
}

// --- shareGetSession ---------------------------------------------------------

// TestShareGetSessionFullTimelineAndPendingApprovals asserts the transcript
// endpoint renders the full timeline (assistant markdown → content.html) and
// surfaces pending approvals as inline cards.
func TestShareGetSessionFullTimelineAndPendingApprovals(t *testing.T) {
	f := &fakeMemory{
		shareDetail: &ShareSessionDetail{
			ID: "sess-1",
			Items: []json.RawMessage{
				json.RawMessage(`{"kind":"message","role":"user","content":{"text":"hello"}}`),
				json.RawMessage(`{"kind":"message","role":"assistant","content":{"text":"hi there"}}`),
				json.RawMessage(`{"kind":"tool_call","tool_name":"web_search","tool_input":{"q":"x"}}`),
			},
			// The server detail carries the pending tool approvals itself; the
			// gateway must relay them verbatim (never re-merge /questions, which
			// duplicated every card).
			PendingApprovals: []PendingApprovalItem{
				{QuestionID: "q1", Tool: "send_email", Input: map[string]any{"to": "a@b.c"}},
			},
		},
	}
	_, e := newShareTestServer(f)

	req := httptest.NewRequest(http.MethodGet, "/share/api/sessions/sess-1", nil)
	req.AddCookie(shareTestCookie(t))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var detail ShareSessionDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(detail.Items) != 3 {
		t.Fatalf("want 3 timeline items, got %d", len(detail.Items))
	}
	if len(detail.PendingApprovals) != 1 {
		t.Fatalf("want 1 pending approval, got %d", len(detail.PendingApprovals))
	}
	if got := detail.PendingApprovals[0]; got.QuestionID != "q1" || got.Tool != "send_email" {
		t.Fatalf("pending approval = %+v", got)
	}

	// The assistant message must carry rendered content.html (the history path).
	var assistant map[string]any
	if err := json.Unmarshal(detail.Items[1], &assistant); err != nil {
		t.Fatalf("assistant item decode: %v", err)
	}
	content, _ := assistant["content"].(map[string]any)
	if html, _ := content["html"].(string); html == "" {
		t.Errorf("assistant content.html not rendered: %s", string(detail.Items[1]))
	}
}

// --- sharePage ---------------------------------------------------------------

// TestShareAgentPageLoadsPopoverRuntime guards the rail's per-row actions menu:
// it is a go-daisy ui.Popover, which opens only through the go-daisy-popover.js
// runtime. The public page loads its own chat client scripts (not the app shell
// that would otherwise emit the runtime), so it must pull the runtime in
// explicitly — otherwise the archive/unarchive/delete row menu renders but never
// opens.
func TestShareAgentPageLoadsPopoverRuntime(t *testing.T) {
	html := renderHTML(t, ShareAgentPage(ShareAgentPageProps{State: ShareStateReady, ShowSessionList: true}))
	if !strings.Contains(html, "go-daisy-popover.js") {
		t.Errorf("share page missing the go-daisy popover runtime (rail row menu cannot open)")
	}
}

// TestSharePageEmitsNoSentry pins issue #219: the public share page is served
// under the strict CSP in shareSecurityHeaders, which disallows third-party
// scripts and outbound reporting. Even with the deployment's SENTRY_DSN
// configured, the page must ship neither the Sentry CDN bundle nor the five
// sentry-* meta tags (nothing on this surface reads them).
func TestSharePageEmitsNoSentry(t *testing.T) {
	s := &Server{cfg: Config{
		SentryDSN:         "https://key@sentry.example.com/1",
		SentryEnvironment: "production",
	}}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/share/agent", nil)
	rec := httptest.NewRecorder()
	if err := s.sharePage(e.NewContext(req, rec)); err != nil {
		t.Fatalf("sharePage: %v", err)
	}
	html := rec.Body.String()
	if strings.TrimSpace(html) == "" {
		t.Fatal("share page rendered empty — the absence assertions would pass vacuously")
	}
	if strings.Contains(html, sentryCDNScript) {
		t.Error("Sentry CDN script emitted on the public share page, but its CSP blocks third-party scripts")
	}
	if strings.Contains(html, "Sentry.init") {
		t.Error("Sentry init bootstrap emitted on the public share page, but its CSP blocks third-party scripts")
	}
	for _, meta := range []string{
		"sentry-dsn", "sentry-environment", "sentry-traces-sample-rate",
		"sentry-replay-session-sample-rate", "sentry-replay-on-error-sample-rate",
	} {
		if strings.Contains(html, `name="`+meta+`"`) {
			t.Errorf("meta tag %q emitted on the public share page; Sentry must be absent from this surface", meta)
		}
	}
}

// --- unarchive / delete / cancel --------------------------------------------

// TestShareUnarchiveDeleteCancelProxy asserts the three new lifecycle handlers
// forward the right ids to the backend.
func TestShareUnarchiveDeleteCancelProxy(t *testing.T) {
	f := &fakeMemory{}
	_, e := newShareTestServer(f)

	req := httptest.NewRequest(http.MethodPost, "/share/api/sessions/s1/unarchive", nil)
	req.AddCookie(shareTestCookie(t))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unarchive status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/share/api/sessions/s2", nil)
	req.AddCookie(shareTestCookie(t))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/share/api/sessions/s3/approvals/q9/cancel", nil)
	req.AddCookie(shareTestCookie(t))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d", rec.Code)
	}

	if len(f.shareUnarchivedIDs) != 1 || f.shareUnarchivedIDs[0] != "s1" {
		t.Fatalf("unarchive ids = %v", f.shareUnarchivedIDs)
	}
	if len(f.shareDeletedIDs) != 1 || f.shareDeletedIDs[0] != "s2" {
		t.Fatalf("delete ids = %v", f.shareDeletedIDs)
	}
	if len(f.shareCancelledIDs) != 1 || f.shareCancelledIDs[0] != "q9" || len(f.shareCancelledSess) != 1 || f.shareCancelledSess[0] != "s3" {
		t.Fatalf("cancel ids = %v / %v", f.shareCancelledSess, f.shareCancelledIDs)
	}
}

// --- sharePartialSessions ----------------------------------------------------

// TestSharePartialSessionsMarkupAndArchivedFilter asserts the rail fragment uses
// the shared `ui.ListRow` markup (data-testid) and that ?includeArchived=true
// widens the backend filter to "all".
func TestSharePartialSessionsMarkupAndArchivedFilter(t *testing.T) {
	f := &fakeMemory{
		sharePublicSessions: []ShareSession{{ID: "s1", Title: strPtr("First chat")}},
	}
	_, e := newShareTestServer(f)

	req := httptest.NewRequest(http.MethodGet, "/share/api/partial/sessions", nil)
	req.AddCookie(shareTestCookie(t))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.sharePublicListFilter != "" {
		t.Errorf("default partial must not set a filter, got %q", f.sharePublicListFilter)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="share-session-row"`,
		`data-action="share-resume-session"`,
		`data-action="share-session-menu"`,
		`data-action="share-archive-session"`,
		`data-action="share-delete-session"`,
		"First chat",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("partial missing %q:\n%s", want, body)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/share/api/partial/sessions?includeArchived=true", nil)
	req.AddCookie(shareTestCookie(t))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if f.sharePublicListFilter != "all" {
		t.Errorf("includeArchived=true must map to filter=all, got %q", f.sharePublicListFilter)
	}
}
