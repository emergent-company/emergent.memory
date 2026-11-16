package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newBadgeServer is the minimum a badge test needs: a project id (so
// currentProjectID resolves) and the fake backend.
func newBadgeServer(f *fakeMemory) *Server {
	return &Server{cfg: Config{MemoryProjectID: "p1"}, memory: f}
}

// TestPendingInputCount pins what the Approvals badge counts: pending tool
// approvals plus unanswered questions, with a tool-confirmation question folded
// into its approval row (the same dedupe the page applies) so the badge and the
// page cannot disagree.
func TestPendingInputCount(t *testing.T) {
	f := &fakeMemory{
		approvals: []ToolApprovalItem{
			{ID: "a1", QuestionID: "q-approval", Decision: "pending"},
			{ID: "a2", QuestionID: "q-other", Decision: "pending"},
			{ID: "a3", QuestionID: "q-done", Decision: "approved"},
		},
		questions: []AgentQuestionItem{
			// Represented by a1: counted once, as the approval.
			{ID: "q-approval", Status: "pending"},
			// Open-ended question with no approval row: its own item.
			{ID: "q-open", Status: "pending"},
			// Answered: not waiting on anyone.
			{ID: "q-answered", Status: "answered", Response: strPtr("yes")},
		},
	}
	s := newBadgeServer(f)

	got, err := s.cachedPendingInputCount(context.Background())
	if err != nil {
		t.Fatalf("cachedPendingInputCount: %v", err)
	}
	if got != 3 {
		t.Fatalf("count = %d, want 3 (2 approvals + 1 unanswered question)", got)
	}
}

// TestPendingInputCountCached pins the 30s cache the badge depends on: without
// it every page render pays two list round trips. A decision invalidates it.
func TestPendingInputCountCached(t *testing.T) {
	f := &fakeMemory{approvals: []ToolApprovalItem{{ID: "a1", Decision: "pending"}}}
	s := newBadgeServer(f)
	ctx := context.Background()

	if got, _ := s.cachedPendingInputCount(ctx); got != 1 {
		t.Fatalf("first count = %d, want 1", got)
	}
	f.approvals = nil
	if got, _ := s.cachedPendingInputCount(ctx); got != 1 {
		t.Fatalf("cached count = %d, want the cached 1", got)
	}
	s.invalidatePendingInputCount(ctx)
	if got, _ := s.cachedPendingInputCount(ctx); got != 0 {
		t.Fatalf("count after invalidate = %d, want 0", got)
	}
}

// TestPendingInputCountBackendError pins the fail-safe: a failed approval
// fetch returns the error so the caller hides the badge instead of rendering a
// misleading zero.
func TestPendingInputCountBackendError(t *testing.T) {
	s := newBadgeServer(&fakeMemory{approvalsErr: errTest})
	if _, err := s.cachedPendingInputCount(context.Background()); err == nil {
		t.Fatal("want an error so the caller hides the badge")
	}
}

// TestPendingInputCountQuestionsError pins the narrower fail-safe: losing the
// question list leaves the approval count intact, so a partial outage does not
// hide a badge that is still telling the truth.
func TestPendingInputCountQuestionsError(t *testing.T) {
	f := &fakeMemory{
		approvals: []ToolApprovalItem{
			{ID: "a1", QuestionID: "q1", Decision: "pending"},
			{ID: "a2", QuestionID: "q2", Decision: "pending"},
		},
		questionsErr: errTest,
	}
	got, err := newBadgeServer(f).cachedPendingInputCount(context.Background())
	if err != nil {
		t.Fatalf("question failure must not fail the count: %v", err)
	}
	if got != 2 {
		t.Fatalf("count = %d, want 2 (approvals only)", got)
	}
}

// TestPendingInputCountUsesFilteredRead pins that the badge asks for the
// pending list, not the whole audit trail: the endpoint is unpaginated, so an
// unfiltered read would grow with every decision the project ever made.
func TestPendingInputCountUsesFilteredRead(t *testing.T) {
	f := &fakeMemory{}
	if _, err := newBadgeServer(f).cachedPendingInputCount(context.Background()); err != nil {
		t.Fatalf("count: %v", err)
	}
	if f.pendingFetched != 1 {
		t.Fatalf("pending fetches = %d, want 1", f.pendingFetched)
	}
}

// TestPendingInputCountNoProject pins that a request with no project context
// makes no backend call (and therefore renders no badge).
func TestPendingInputCountNoProject(t *testing.T) {
	f := &fakeMemory{approvals: []ToolApprovalItem{{ID: "a1", Decision: "pending"}}}
	s := &Server{cfg: Config{}, memory: f}
	if got, err := s.cachedPendingInputCount(context.Background()); err != nil || got != 0 {
		t.Fatalf("count = %d, err = %v; want 0, nil", got, err)
	}
}

// TestSidebarApprovalsBadge asserts the count lands on the Approvals nav item
// and that a zero count renders nothing.
func TestSidebarApprovalsBadge(t *testing.T) {
	found := false
	for _, g := range sidebarGroupsWithWarn("", false, sidebarWarns{Approvals: 3}) {
		for _, item := range g.Items {
			if item.Label != "Approvals" {
				continue
			}
			found = true
			if item.Warn == nil {
				t.Fatal("Approvals item has no badge")
			}
			var buf bytes.Buffer
			if err := item.Warn.Render(context.Background(), &buf); err != nil {
				t.Fatalf("render badge: %v", err)
			}
			html := buf.String()
			if !strings.Contains(html, ">3<") {
				t.Fatalf("badge = %q, want count 3", html)
			}
			if !strings.Contains(html, `data-testid="approvals-nav-badge"`) {
				t.Fatalf("badge missing its test hook: %q", html)
			}
			if !strings.Contains(html, "ml-1") {
				t.Fatalf("badge lost the caller's ml-1 utility: %q", html)
			}
		}
	}
	if !found {
		t.Fatal("Approvals item missing from sidebar")
	}

	// The badge must sit inside the Approvals anchor, not merely in the nav.
	html := renderHTML(t, appSidebar(sidebarGroupsWithWarn("", false, sidebarWarns{Approvals: 3}), nil, false))
	href := strings.Index(html, `href="/settings/approvals"`)
	badge := strings.Index(html, `data-testid="approvals-nav-badge"`)
	if href < 0 || badge < 0 {
		t.Fatalf("rendered sidebar missing link (%d) or badge (%d)", href, badge)
	}
	if close := strings.Index(html[href:], "</a>"); close >= 0 && href+close < badge {
		t.Fatal("badge rendered outside the Approvals link")
	}

	for _, g := range sidebarGroupsWithWarn("", false, sidebarWarns{}) {
		for _, item := range g.Items {
			if item.Label == "Approvals" && item.Warn != nil {
				t.Fatal("zero pending items rendered a badge")
			}
		}
	}
}

// TestApprovalDecisionInvalidatesBadge pins that answering on the Approvals
// page drops the cached count, so the redirect renders the new number instead
// of the stale one.
func TestApprovalDecisionInvalidatesBadge(t *testing.T) {
	f := &fakeMemory{approvals: []ToolApprovalItem{{ID: "a1", QuestionID: "q1", Decision: "pending"}}}
	s := newBadgeServer(f)
	e := echo.New()
	e.POST("/settings/approvals/:questionId/respond", s.uiApproveReject)

	ctx := context.Background()
	if got, _ := s.cachedPendingInputCount(ctx); got != 1 {
		t.Fatalf("seeded count = %d, want 1", got)
	}
	f.approvals = nil

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/approvals/q1/respond", strings.NewReader("response=approve"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	if got, _ := s.cachedPendingInputCount(ctx); got != 0 {
		t.Fatalf("count after decision = %d, want 0 (cache must be invalidated)", got)
	}
}

// TestChatDecisionInvalidatesBadge covers the other two decision paths: an
// answer given in the chat transcript (the API endpoints chat.js posts to) must
// drop the cached count just like the Approvals page does.
func TestChatDecisionInvalidatesBadge(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{"respond", "/api/chat/questions/q1/respond", `{"response":"approve"}`},
		{"cancel", "/api/chat/questions/q1/cancel", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{approvals: []ToolApprovalItem{{ID: "a1", QuestionID: "q1", Decision: "pending"}}}
			s := newBadgeServer(f)
			e := echo.New()
			e.POST("/api/chat/questions/:questionId/respond", s.respondQuestion)
			e.POST("/api/chat/questions/:questionId/cancel", s.cancelQuestion)

			ctx := context.Background()
			if got, _ := s.cachedPendingInputCount(ctx); got != 1 {
				t.Fatalf("seeded count = %d, want 1", got)
			}
			f.approvals = nil

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}

			if got, _ := s.cachedPendingInputCount(ctx); got != 0 {
				t.Fatalf("count after %s = %d, want 0 (cache must be invalidated)", tc.name, got)
			}
		})
	}
}

// TestApprovalsMutationRedirectsBoostedForms guards the badge's own invariant on
// the mutation path: the decision and cancel forms live inside the boosted
// `<main hx-target="#main-content">`, so a plain 303 is followed in-XHR and
// swaps only #main-content. The sidebar — and therefore the pending-input badge
// — sits outside that target, so it would keep the pre-decision count until a
// full page load.
//
// The boosted answer must therefore be 200 + HX-Redirect, not 303 + HX-Redirect:
// fetch follows the 3xx inside the XHR and htmx only sees the final response's
// headers, so a header on the 303 is invisible and the swap still happens.
func TestApprovalsMutationRedirectsBoostedForms(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{name: "respond", path: "/settings/approvals/q-1/respond"},
		{name: "cancel", path: "/settings/approvals/q-2/cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{}
			_, e := newTestServer(f)

			// Boosted form: htmx must be told to navigate, not to swap a fragment.
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader("response=approve"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("HX-Request", "true")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("boosted status = %d, want 200 (a 3xx hides HX-Redirect from htmx)", rec.Code)
			}
			if got := rec.Header().Get("HX-Redirect"); got != "/settings/approvals" {
				t.Fatalf("boosted HX-Redirect = %q, want /settings/approvals", got)
			}

			// Plain POST keeps the PRG 303 to the same target.
			req = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader("response=approve"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec = httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("plain status = %d, want 303", rec.Code)
			}
			if got := rec.Header().Get("Location"); got != "/settings/approvals" {
				t.Fatalf("plain Location = %q, want /settings/approvals", got)
			}
			if got := rec.Header().Get("HX-Redirect"); got != "" {
				t.Fatalf("plain response carried HX-Redirect %q; only a boosted request should", got)
			}
		})
	}
}
