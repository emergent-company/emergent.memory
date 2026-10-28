package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// bulkPost drives POST /objects/bulk through the real route and returns the
// recorder. Every visible behaviour of the bulk bar is a redirect, so the tests
// assert on the Location (flash params + return path) as well as the fake's
// recorded calls.
func bulkPost(t *testing.T, s *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.POST("/objects/bulk", s.uiObjectsBulk)
	req := httptest.NewRequest(http.MethodPost, "/objects/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func bulkServer(f *fakeMemory) *Server {
	return &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
}

func TestParseBulkObjectIDs(t *testing.T) {
	got := parseBulkObjectIDs(" a, b ,,a,c ")
	want := []string{"a", "b", "c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if len(parseBulkObjectIDs("  ")) != 0 {
		t.Errorf("blank ids must parse to none")
	}
}

func TestParseBulkObjectIDsCaps(t *testing.T) {
	var parts []string
	for i := 0; i < objectsBulkMaxIDs+25; i++ {
		parts = append(parts, fmt.Sprintf("id-%d", i))
	}
	if got := len(parseBulkObjectIDs(strings.Join(parts, ","))); got != objectsBulkMaxIDs {
		t.Fatalf("len = %d, want the %d cap", got, objectsBulkMaxIDs)
	}
}

func TestObjectsBulkReturnPath(t *testing.T) {
	cases := map[string]string{
		"/objects":                               "/objects",
		"/objects?type=task&order=asc":           "/objects?order=asc&type=task",
		"/agents/a1/objects?provenance=created":  "/agents/a1/objects?provenance=created",
		"/objects?bulk=delete&bulk_n=2&err=nope": "/objects",
		"/objects/abc":                           "/objects",
		"https://evil.example/objects":           "/objects",
		"//evil.example/objects":                 "/objects",
		"/agents/a1/settings":                    "/objects",
		"":                                       "/objects",
	}
	for in, want := range cases {
		if got := objectsBulkReturnPath(in); got != want {
			t.Errorf("objectsBulkReturnPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToggleObjectLabel(t *testing.T) {
	labels := []string{"q3", "ui"}
	added, changed := toggleObjectLabel(labels, "infra", true)
	if !changed || strings.Join(added, ",") != "q3,ui,infra" {
		t.Fatalf("add = %v changed=%v", added, changed)
	}
	if strings.Join(labels, ",") != "q3,ui" {
		t.Errorf("toggleObjectLabel mutated its input: %v", labels)
	}
	if got, changed := toggleObjectLabel(labels, "q3", true); changed || len(got) != 2 {
		t.Errorf("adding an existing label must be a no-op, got %v changed=%v", got, changed)
	}
	removed, changed := toggleObjectLabel(labels, "q3", false)
	if !changed || strings.Join(removed, ",") != "ui" {
		t.Fatalf("remove = %v changed=%v", removed, changed)
	}
	if got, changed := toggleObjectLabel(labels, "absent", false); changed || len(got) != 2 {
		t.Errorf("removing an absent label must be a no-op, got %v changed=%v", got, changed)
	}
}

func TestObjectsBulkLabelAdd(t *testing.T) {
	f := &fakeMemory{objects: []GraphObject{
		{ID: "o1", Type: "task", Labels: []string{"q3"}},
	}}
	rec := bulkPost(t, bulkServer(f), url.Values{
		"ids":    {"o1"},
		"action": {"label_add"},
		"label":  {"infra"},
		"back":   {"/objects?type=task"},
	})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.lastUpdateID != "o1" {
		t.Errorf("updated %q, want o1", f.lastUpdateID)
	}
	if f.lastUpdateReq == nil || !f.lastUpdateReq.ReplaceLabels {
		t.Fatalf("labels must be a whole-set replacement, got %+v", f.lastUpdateReq)
	}
	if got := strings.Join(f.lastUpdateReq.Labels, ","); got != "q3,infra" {
		t.Errorf("labels = %q, want the existing label kept plus the new one", got)
	}
	want := "/objects?type=task&bulk=label_add&bulk_n=1"
	if loc := rec.Header().Get("Location"); loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
}

func TestObjectsBulkLabelAlreadyPresent(t *testing.T) {
	f := &fakeMemory{objects: []GraphObject{{ID: "o1", Labels: []string{"infra"}}}}
	rec := bulkPost(t, bulkServer(f), url.Values{
		"ids": {"o1"}, "action": {"label_add"}, "label": {"infra"},
	})
	if f.lastUpdateReq != nil {
		t.Errorf("an unchanged label must not write a new version")
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "bulk_n=0") {
		t.Errorf("Location = %q, want bulk_n=0", loc)
	}
}

func TestObjectsBulkStatusChunks(t *testing.T) {
	var parts []string
	for i := 0; i < objectsBulkStatusChunk+1; i++ {
		parts = append(parts, fmt.Sprintf("id-%d", i))
	}
	f := &fakeMemory{}
	rec := bulkPost(t, bulkServer(f), url.Values{
		"ids":    {strings.Join(parts, ",")},
		"action": {"status"},
		"status": {"archived"},
	})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.bulkStatusValue != "archived" {
		t.Errorf("status = %q", f.bulkStatusValue)
	}
	if got := len(f.bulkStatusIDs); got != objectsBulkStatusChunk+1 {
		t.Errorf("sent %d ids, want every id across both chunks", got)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "bulk_n="+fmt.Sprint(objectsBulkStatusChunk+1)) {
		t.Errorf("Location = %q, want the full success count", loc)
	}
}

func TestObjectsBulkDelete(t *testing.T) {
	f := &fakeMemory{}
	rec := bulkPost(t, bulkServer(f), url.Values{
		"ids":    {"o1,o2"},
		"action": {"delete"},
		"back":   {"/objects"},
	})
	if strings.Join(f.deletedObjectIDs, ",") != "o1,o2" {
		t.Fatalf("deleted %v", f.deletedObjectIDs)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "bulk=delete&bulk_n=2") {
		t.Errorf("Location = %q", loc)
	}
}

func TestObjectsBulkPartialFailureKeepsSuccess(t *testing.T) {
	f := &fakeMemory{bulkStatusResult: &BulkUpdateStatusResponse{
		Success: 1,
		Failed:  1,
		Results: []BulkUpdateStatusResult{
			{ID: "o1", Success: true},
			{ID: "o2", Success: false, Error: strPtr("status not allowed for type")},
		},
	}}
	rec := bulkPost(t, bulkServer(f), url.Values{
		"ids": {"o1,o2"}, "action": {"status"}, "status": {"archived"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("partial failure must still redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "bulk_n=1") || !strings.Contains(loc, "bulk_f=1") {
		t.Errorf("Location = %q, want applied and failed counts", loc)
	}
}

func TestObjectsBulkAllFailedReportsCause(t *testing.T) {
	f := &fakeMemory{bulkDeleteErr: fmt.Errorf("upstream unavailable")}
	rec := bulkPost(t, bulkServer(f), url.Values{
		"ids": {"o1"}, "action": {"delete"}, "back": {"/objects?type=task"},
	})
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("Location = %q, want an error flash", loc)
	}
	if strings.Contains(loc, "bulk_n=") {
		t.Errorf("Location = %q, must not also claim success", loc)
	}
}

func TestObjectsBulkValidation(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
	}{
		{"no ids", url.Values{"action": {"delete"}}},
		{"unknown action", url.Values{"ids": {"o1"}, "action": {"explode"}}},
		{"label without a value", url.Values{"ids": {"o1"}, "action": {"label_add"}}},
		{"status without a value", url.Values{"ids": {"o1"}, "action": {"status"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{}
			rec := bulkPost(t, bulkServer(f), tc.form)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d", rec.Code)
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=") {
				t.Errorf("Location = %q, want an error flash", loc)
			}
			if len(f.deletedObjectIDs)+len(f.bulkStatusIDs) != 0 || f.lastUpdateReq != nil {
				t.Errorf("a rejected request must not touch the backend")
			}
		})
	}
}

func TestObjectsBulkFlash(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"", ""},
		{"bulk=label_add&bulk_n=3", "Labelled 3 objects."},
		{"bulk=label_add&bulk_n=3&bulk_f=1", "Labelled 3 objects. 1 object failed."},
		{"bulk=label_remove&bulk_n=1", "Removed the label from 1 object."},
		{"bulk=status&bulk_n=2", "Set the status on 2 objects."},
		{"bulk=delete&bulk_n=4", "Deleted 4 objects."},
		{"bulk=label_add&bulk_n=0", "Every selected object already had that label."},
		{"bulk=label_remove&bulk_n=0", "No selected object had that label."},
		{"bulk=delete&bulk_n=0", "Nothing changed."},
		{"bulk=nonsense&bulk_n=2", ""},
	}
	for _, tc := range cases {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/objects?"+tc.query, nil)
		if got := objectsBulkFlash(e.NewContext(req, httptest.NewRecorder())); got != tc.want {
			t.Errorf("objectsBulkFlash(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestObjectsBulkLabelsCountPartialFailures(t *testing.T) {
	// o2 is not in the fake's object list, so GetGraphObject fails for it.
	f := &fakeMemory{objects: []GraphObject{{ID: "o1", Labels: nil}}}
	rec := bulkPost(t, bulkServer(f), url.Values{
		"ids": {"o1,o2"}, "action": {"label_add"}, "label": {"infra"},
	})
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "bulk_n=1") || !strings.Contains(loc, "bulk_f=1") {
		t.Errorf("Location = %q, want 1 applied and 1 failed", loc)
	}
}

// TestObjectsBulkDeleteScopesToBranch pins the delete branch scope. Omitting
// branch_id makes the graph endpoint delete the MAIN head, so deleting from a
// branch-filtered view would remove the object from main while the list the user
// is looking at keeps showing it.
func TestObjectsBulkDeleteScopesToBranch(t *testing.T) {
	f := &fakeMemory{}
	rec := bulkPost(t, bulkServer(f), url.Values{
		"ids":    {"o1,o2"},
		"action": {"delete"},
		"branch": {"b-objs"},
		"back":   {"/objects?branch=b-objs"},
	})

	if got := strings.Join(f.deletedObjectBranches, ","); got != "b-objs,b-objs" {
		t.Errorf("branch args = %q, want the page's branch on both deletes", got)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "branch=b-objs") {
		t.Errorf("Location = %q, want the branch kept in the return path", loc)
	}
}

// TestDeleteGraphObjectSendsBranch covers the wire shape: branch_id is a query
// param on the delete route, and an unfiltered (main) delete must not send one.
func TestDeleteGraphObjectSendsBranch(t *testing.T) {
	var gotPath, gotBranch, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		gotBranch = r.URL.Query().Get("branch_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"deleted"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	if err := m.DeleteGraphObject(context.Background(), "obj-1", "branch-1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/graph/objects/obj-1" || gotBranch != "branch-1" {
		t.Errorf("branch delete = %s ?%s", gotPath, gotQuery)
	}

	if err := m.DeleteGraphObject(context.Background(), "obj-2", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotQuery, "branch_id") {
		t.Errorf("main-branch delete must not send branch_id, got ?%s", gotQuery)
	}
}

// TestObjectsBulkLabelWriteInvalidatesSuggestions pins the #1504 staleness rule
// for the bulk path: a bulk label write must drop the create form's cached label
// list, exactly as a single-object label edit does, or new labels stay missing
// from the suggestion dropdown for up to labelSuggestionsTTL.
func TestObjectsBulkLabelWriteInvalidatesSuggestions(t *testing.T) {
	seed := func(objects ...GraphObject) *Server {
		s := bulkServer(&fakeMemory{objects: objects})
		s.cfg.MemoryProjectID = "p1"
		s.labelCache = map[string]labelSuggestionsEntry{
			"p1": {labels: []string{"old"}, expiresAt: time.Now().Add(time.Hour)},
		}
		return s
	}

	cases := []struct {
		action  string
		form    url.Values
		objects []GraphObject
		clears  bool
	}{
		{"label add", url.Values{"ids": {"o1"}, "action": {"label_add"}, "label": {"new"}}, []GraphObject{{ID: "o1"}}, true},
		// The object already carries the label, so the removal is a real write.
		{"label remove", url.Values{"ids": {"o1"}, "action": {"label_remove"}, "label": {"new"}}, []GraphObject{{ID: "o1", Labels: []string{"new"}}}, true},
		{"status", url.Values{"ids": {"o1"}, "action": {"status"}, "status": {"done"}}, []GraphObject{{ID: "o1"}}, false},
		{"delete", url.Values{"ids": {"o1"}, "action": {"delete"}}, []GraphObject{{ID: "o1"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			s := seed(tc.objects...)
			bulkPost(t, s, tc.form)
			_, cached := s.labelCache["p1"]
			if tc.clears && cached {
				t.Error("label write must invalidate the suggestion cache")
			}
			if !tc.clears && !cached {
				t.Error("a non-label action must not touch the suggestion cache")
			}
		})
	}
}
