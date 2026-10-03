package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- memory client: list param + lifecycle routes (task 4.1) ---

// TestMemoryClientListConversationsIncludeArchived pins the list contract: the
// default request omits includeArchived (archived excluded) and the opt-in sends
// includeArchived=true. It also confirms the new archive fields decode.
func TestMemoryClientListConversationsIncludeArchived(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"conversations":[{"id":"c1","title":"Old","isArchived":true,"archivedAt":"2026-09-01T00:00:00Z"}],"total":1}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	out, err := m.ListConversations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Has("includeArchived") {
		t.Errorf("default list must not send includeArchived, got %v", gotQuery)
	}
	if len(out.Conversations) != 1 || !out.Conversations[0].IsArchived || out.Conversations[0].ArchivedAt != "2026-09-01T00:00:00Z" {
		t.Errorf("archive fields not decoded: %+v", out.Conversations)
	}

	if _, err := m.ListConversations(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := gotQuery.Get("includeArchived"); got != "true" {
		t.Errorf("includeArchived = %q, want true", got)
	}
}

// TestMemoryClientConversationLifecycleRoutes pins the request method + path for
// archive/unarchive/delete (memory's real /api/chat/:id shape).
func TestMemoryClientConversationLifecycleRoutes(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		wantPath string
		call     func(*MemoryClient) error
	}{
		{"archive", http.MethodPost, "/api/chat/c1/archive", func(m *MemoryClient) error { return m.ArchiveConversation(context.Background(), "c1") }},
		{"unarchive", http.MethodPost, "/api/chat/c1/unarchive", func(m *MemoryClient) error { return m.UnarchiveConversation(context.Background(), "c1") }},
		{"delete", http.MethodDelete, "/api/chat/c1", func(m *MemoryClient) error { return m.DeleteConversation(context.Background(), "c1") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			m := NewMemoryClient(srv.URL, "proj")
			if err := tc.call(m); err != nil {
				t.Fatal(err)
			}
			if gotMethod != tc.method || gotPath != tc.wantPath {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, tc.method, tc.wantPath)
			}
		})
	}
}

// --- gateway routes: lifecycle actions (tasks 5.2/5.5) ---

func newConversationLifecycleEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/api/conversations/:id/archive", s.archiveConversation)
	e.POST("/api/conversations/:id/unarchive", s.unarchiveConversation)
	e.DELETE("/api/conversations/:id", s.deleteConversation)
	return s, e
}

// TestConversationLifecycleRoutes asserts each rail action reaches its backend
// call and returns the expected status.
func TestConversationLifecycleRoutes(t *testing.T) {
	f := &fakeMemory{}
	_, e := newConversationLifecycleEcho(f)

	post := func(path string) int {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		return rec.Code
	}
	if code := post("/api/conversations/c1/archive"); code != http.StatusOK {
		t.Errorf("archive status = %d, want 200", code)
	}
	if code := post("/api/conversations/c2/unarchive"); code != http.StatusOK {
		t.Errorf("unarchive status = %d, want 200", code)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/conversations/c3", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", rec.Code)
	}

	if len(f.archivedConvs) != 1 || f.archivedConvs[0] != "c1" {
		t.Errorf("archived = %v, want [c1]", f.archivedConvs)
	}
	if len(f.unarchivedConvs) != 1 || f.unarchivedConvs[0] != "c2" {
		t.Errorf("unarchived = %v, want [c2]", f.unarchivedConvs)
	}
	if len(f.deletedConvs) != 1 || f.deletedConvs[0] != "c3" {
		t.Errorf("deleted = %v, want [c3]", f.deletedConvs)
	}
}

// TestConversationLifecycleNotFound asserts a foreign/unknown id fails closed to
// 404 at the gateway (parity with memory's owner-or-shared predicate).
func TestConversationLifecycleNotFound(t *testing.T) {
	notFound := &memoryHTTPError{Status: http.StatusNotFound, Code: "not_found", Message: "conversation not found"}
	f := &fakeMemory{archiveConvErr: notFound, deleteConvErr: notFound}
	_, e := newConversationLifecycleEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/conversations/foreign/archive", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("archive foreign status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/conversations/foreign", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete foreign status = %d, want 404", rec.Code)
	}
}

// --- rail render: include-archived control + archived row state (tasks 5.1/5.3) ---

func newChatRailEcho(f MemoryBackend) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/partial/chat-rail", s.uiChatRail)
	return s, e
}

// TestChatRailIncludeArchivedControl asserts the filter-row control exists with
// its stable contract hooks.
func TestChatRailIncludeArchivedControl(t *testing.T) {
	html := renderHTML(t, chatWorkspace(nil, nil, nil, nil, nil, "", "", "", false, nil, chatRunControl{}))
	for _, want := range []string{
		`id="chat-include-archived"`,
		`data-testid="chat-include-archived"`,
		`aria-label="Include archived"`,
		`type="checkbox"`,
		"Include archived",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rail filter row missing %q", want)
		}
	}
}

// TestChatRailArchivedFiltering drives the real rail route: archived rows are
// hidden by default (server request excludes them) and appear marked when the
// include-archived option is set.
func TestChatRailArchivedFiltering(t *testing.T) {
	f := &fakeMemory{convs: []Conversation{
		{ID: "active1", Title: "Active one"},
		{ID: "arch1", Title: "Archived one", IsArchived: true},
	}}
	_, e := newChatRailEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-rail", nil))
	body := rec.Body.String()
	if f.convIncludeArchived {
		t.Error("default rail refresh must not request includeArchived")
	}
	if strings.Contains(body, "Archived one") || strings.Contains(body, `data-testid="session-archived-marker"`) {
		t.Errorf("archived row must be hidden by default:\n%s", body)
	}
	if !strings.Contains(body, "Active one") {
		t.Error("active row missing from default rail")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/partial/chat-rail?includeArchived=true", nil))
	body = rec.Body.String()
	if !f.convIncludeArchived {
		t.Error("include-archived rail refresh must request includeArchived=true")
	}
	if !strings.Contains(body, "Archived one") {
		t.Errorf("archived row must appear when included:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="session-archived-marker"`) {
		t.Error("archived row must carry the archived marker")
	}
}

// TestSessionRailItemArchivedState pins the per-row contract: an archived row
// offers Unarchive + Delete and carries the marker; an active row offers
// Archive + Delete and no archived marker.
func TestSessionRailItemArchivedState(t *testing.T) {
	archived := renderHTML(t, sessionRailItem(Conversation{ID: "c1", Title: "Old", IsArchived: true}, agentAppearance{}, false))
	for _, want := range []string{
		`data-testid="session-archived-marker"`,
		`data-archived="true"`,
		`data-testid="session-actions"`,
		`data-action="unarchive-session"`,
		`data-testid="session-unarchive"`,
		`data-action="delete-session"`,
		`data-testid="session-delete"`,
	} {
		if !strings.Contains(archived, want) {
			t.Errorf("archived row missing %q", want)
		}
	}
	if strings.Contains(archived, `data-testid="session-archive"`) {
		t.Error("archived row must not offer Archive")
	}

	active := renderHTML(t, sessionRailItem(Conversation{ID: "c2", Title: "New"}, agentAppearance{}, true))
	for _, want := range []string{
		`data-action="archive-session"`,
		`data-testid="session-archive"`,
		`data-action="delete-session"`,
		`data-testid="session-delete"`,
	} {
		if !strings.Contains(active, want) {
			t.Errorf("active row missing %q", want)
		}
	}
	if strings.Contains(active, "session-archived-marker") || strings.Contains(active, `data-archived="true"`) {
		t.Error("active row must not be marked archived")
	}
}

// TestChatRailListArchivedMarkerOnlyOnArchivedRows asserts exactly one marker
// renders when one of two rows is archived.
func TestChatRailListArchivedMarkerOnlyOnArchivedRows(t *testing.T) {
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "active1", Title: "Active one"},
		{ID: "arch1", Title: "Archived one", IsArchived: true},
	}}
	html := renderHTML(t, chatRailList(convs, nil, nil, nil, ""))
	if n := strings.Count(html, `data-testid="session-archived-marker"`); n != 1 {
		t.Errorf("archived marker count = %d, want 1", n)
	}
	if n := strings.Count(html, `data-archived="true"`); n != 1 {
		t.Errorf("data-archived count = %d, want 1", n)
	}
}

// --- bulk session selection (#1385) ---

// TestChatRailBulkSelectionContract pins the #1385 markup contract: the
// selection-mode toggle, the footer bulk-action bar, and every conversation
// row's server-rendered selection checkbox (keyboard-operable, labelled).
func TestChatRailBulkSelectionContract(t *testing.T) {
	convs := &ConversationList{Conversations: []Conversation{{ID: "c1", Title: "Alpha"}}}
	html := renderHTML(t, chatWorkspace(nil, convs, nil, nil, nil, "", "", "", false, nil, chatRunControl{}))
	for _, want := range []string{
		`data-action="toggle-session-select"`,
		`data-testid="toggle-session-select"`,
		`aria-pressed="false"`,
		`id="chat-bulk-bar"`,
		`data-testid="chat-bulk-bar"`,
		`role="group"`,
		`aria-label="Bulk session actions"`,
		`id="chat-select-all"`,
		`data-action="select-all-sessions"`,
		`aria-label="Select all sessions"`,
		`id="chat-selection-count"`,
		`aria-live="polite"`,
		`data-action="bulk-archive-sessions"`,
		`data-testid="chat-bulk-archive"`,
		`data-action="select-session"`,
		`data-testid="session-select"`,
		`aria-label="Select session: Alpha"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rail bulk-selection contract missing %q", want)
		}
	}
}

// --- delete confirmation (task 5.5) ---

// TestChatDeleteConfirmDialogRender asserts the shared confirmation dialog and
// its dismiss/confirm controls exist.
func TestChatDeleteConfirmDialogRender(t *testing.T) {
	html := renderHTML(t, chatDeleteConfirmDialog())
	for _, want := range []string{
		`id="chat-delete-confirm-modal"`,
		`id="chat-delete-confirm-name"`,
		`data-testid="chat-delete-confirm-go"`,
		`data-action="close-delete-session"`,
		`method="dialog"`, // backdrop/Escape dismiss, no request
	} {
		if !strings.Contains(html, want) {
			t.Errorf("delete confirm dialog missing %q", want)
		}
	}
}

// TestChatDeleteOnlyFiresAfterConfirmation is a static guard: opening the
// confirmation issues no request, and the DELETE lives only in the confirm
// handler (so dismissing the dialog deletes nothing).
func TestChatDeleteOnlyFiresAfterConfirmation(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("webui", "static", "js", "chat.js"))
	if err != nil {
		t.Fatalf("read chat.js: %v", err)
	}
	js := string(b)

	open := jsFuncBody(t, js, "function openDeleteSessionConfirm(id, title)")
	if strings.Contains(open, "fetch(") {
		t.Error("opening the delete confirmation must not issue a request")
	}
	if !strings.Contains(open, "showModal()") {
		t.Error("opening the delete confirmation must showModal()")
	}

	confirm := jsFuncBody(t, js, "function confirmDeleteSession()")
	if !strings.Contains(confirm, `method: "DELETE"`) {
		t.Error("confirmDeleteSession must issue the DELETE request")
	}
	if !strings.Contains(confirm, "resetConversation()") {
		t.Error("deleting the open conversation must reset the workspace to the ready state")
	}
}

// --- workspace behaviour for the open session (tasks 5.4/5.6) ---

// TestChatWorkspaceOpenArchivedStaysOpen asserts archiving the open conversation
// leaves the workspace rendered (it only drops out of the default rail list).
func TestChatWorkspaceOpenArchivedStaysOpen(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}
	convs := &ConversationList{Conversations: []Conversation{
		{ID: "c1", Title: "Open session", AgentDefinitionID: "a1", IsArchived: true},
	}}
	html := renderHTML(t, chatWorkspace(agents, convs, nil, nil, nil, "", "c1", "", false, nil, chatRunControl{}))
	for _, want := range []string{
		`id="chat-root"`,
		`id="chat-form"`,
		`data-testid="chat-input"`,
		"Open session",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("archived-open workspace missing %q", want)
		}
	}
	if strings.Contains(html, "Session unavailable") {
		t.Error("archived-open workspace must not render an error state")
	}
}

// TestChatWorkspaceDeletedOpenRendersReadyState asserts a conversation that no
// longer exists (deleted while open) renders the ready/new-session state, not
// the deleted transcript or an error page.
func TestChatWorkspaceDeletedOpenRendersReadyState(t *testing.T) {
	agents := []AgentDefinitionSummary{{ID: "a1", Name: "diane"}}
	html := renderHTML(t, chatWorkspace(agents, &ConversationList{}, nil, nil, nil, "", "c1", "", false, nil, chatRunControl{}))
	for _, want := range []string{
		`id="chat-root"`,
		`id="chat-form"`,
		`data-testid="chat-input"`,
		"Ready when you are",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("deleted-open workspace missing %q", want)
		}
	}
	if strings.Contains(html, "Session unavailable") {
		t.Error("deleted-open workspace must not render an error page")
	}
}

// jsFuncBody returns the body of a two-space-indented top-level JS function,
// delimited at the first line that closes it ("\n  }").
func jsFuncBody(t *testing.T, js, sig string) string {
	t.Helper()
	start := strings.Index(js, sig)
	if start < 0 {
		t.Fatalf("chat.js: %s missing", sig)
	}
	rest := js[start:]
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		t.Fatalf("chat.js: could not delimit %s", sig)
	}
	return rest[:end]
}
