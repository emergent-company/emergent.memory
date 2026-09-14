package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- template render tests (mirroring agent_ui_test.go) ---

func testBackups() []Backup {
	ready := Backup{
		ID: "b1", OrganizationID: "org-1", ProjectID: "proj-1", ProjectName: "Acme",
		Status: backupStatusReady, Progress: 100, SizeBytes: 1048576, BackupType: backupTypeFull,
		StorageKey:       "backups/org-1/b1/backup.zip",
		CreatedAt:        time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC),
		CompletedAt:      pt(time.Date(2026, 8, 26, 10, 0, 42, 0, time.UTC)),
		ExpiresAt:        pt(time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)),
		CreatedBy:        sp("user-1"),
		Includes:         map[string]any{"documents": true, "chunks": true, "graph": true, "chat": true, "deleted": true},
		Stats:            map[string]any{"documents": 12.0, "chunks": 340.0, "graphObjects": 88.0},
		ManifestChecksum: sp("deadbeefdeadbeefdeadbeefdeadbeef"),
		ContentChecksum:  sp("cafebabecafebabecafebabecafebabe"),
	}
	creating := Backup{
		ID: "b2", OrganizationID: "org-1", ProjectID: "proj-1", ProjectName: "Acme",
		Status: backupStatusCreating, Progress: 40, BackupType: backupTypeFull,
		CreatedAt: time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC),
	}
	failed := Backup{
		ID: "b3", OrganizationID: "org-1", ProjectID: "proj-1", ProjectName: "Acme",
		Status: backupStatusFailed, Progress: 55, SizeBytes: 42, BackupType: backupTypeFull,
		CreatedAt:    time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC),
		ErrorMessage: sp("storage write timed out"),
	}
	return []Backup{ready, creating, failed}
}

func sp(s string) *string       { return &s }
func pt(t time.Time) *time.Time { return &t }

func TestRenderBackupsPage(t *testing.T) {
	backups := testBackups()
	html := renderHTML(t, BackupsPage(backups, 3, "", nil, "", nil))

	// list content: names/ids/status/size/created + header count
	for _, want := range []string{
		"Backups", "Acme", "b1", "b2", "b3",
		"ready", "creating", "failed",
		"1.0 MB", "40%",
		"storage write timed out",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("backups page missing %q", want)
		}
	}
	if !strings.Contains(html, "3 backup") {
		t.Errorf("count badge missing: %s", html)
	}
	// the list no longer shows checksums — they live on the details page
	for _, unwanted := range []string{"deadbeef…beef", "cafebabe…babe", "content checksum", "manifest checksum", "Checksums"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("list should not render %q", unwanted)
		}
	}
	// ready-row expiry still shown
	if !strings.Contains(html, "expires 2026-09-25") {
		t.Error("ready expiry missing")
	}
	// every row links to its details page
	for _, want := range []string{`href="/backups/b1"`, `href="/backups/b2"`, `href="/backups/b3"`} {
		if !strings.Contains(html, want) {
			t.Errorf("details link missing %q", want)
		}
	}
	// download offered only for the ready backup
	if !strings.Contains(html, `href="/backups/b1/download"`) {
		t.Error("ready backup missing download link")
	}
	if strings.Contains(html, `href="/backups/b2/download"`) || strings.Contains(html, `href="/backups/b3/download"`) {
		t.Error("download must not be offered for non-ready backups")
	}
	// per-row delete confirm dialogs + actions
	for _, want := range []string{`action="/backups/b1/delete"`, `action="/backups/b2/delete"`, `action="/backups/b3/delete"`, `id="backup-delete-b1"`, "Delete backup"} {
		if !strings.Contains(html, want) {
			t.Errorf("delete affordance missing %q", want)
		}
	}
	// create form
	for _, want := range []string{`action="/backups"`, `name="retentionDays"`, `name="includeDeleted"`, `name="includeChat"`, "Start backup"} {
		if !strings.Contains(html, want) {
			t.Errorf("create form missing %q", want)
		}
	}
	// restore is surfaced as unavailable, never a working action
	if !strings.Contains(html, "Restore is not available yet") {
		t.Error("restore-unavailable note missing")
	}
	if strings.Contains(html, "/restore") || strings.Contains(html, "Restore backup") {
		t.Error("restore must never be wired to an action")
	}
}

func TestRenderBackupsPageCreatingPolls(t *testing.T) {
	creating := []Backup{{ID: "b2", ProjectName: "Acme", Status: backupStatusCreating, Progress: 40, CreatedAt: time.Now()}}
	html := renderHTML(t, BackupsPage(creating, 1, "", nil, "", nil))
	if !strings.Contains(html, "window.location.reload") {
		t.Error("auto-refresh poll missing while a backup is creating")
	}
	if !strings.Contains(html, "refreshes automatically") {
		t.Error("progress note missing")
	}

	terminal := []Backup{{ID: "b1", ProjectName: "Acme", Status: backupStatusReady, Progress: 100, CreatedAt: time.Now()}}
	htmlDone := renderHTML(t, BackupsPage(terminal, 1, "", nil, "", nil))
	if strings.Contains(htmlDone, "window.location.reload") {
		t.Error("poll must stop once every backup is terminal")
	}
}

func TestRenderBackupsPageEmpty(t *testing.T) {
	html := renderHTML(t, BackupsPage(nil, 0, "", nil, "", nil))
	if !strings.Contains(html, "No backups yet") {
		t.Error("empty state missing")
	}
	if !strings.Contains(html, `action="/backups"`) {
		t.Error("create form should still be offered when there are no backups")
	}
}

func TestRenderBackupsPageUnavailable(t *testing.T) {
	// feature-gated degrade (design D6): unavailable state, not an error
	html := renderHTML(t, BackupsPage(nil, 0, "The connected Memory service does not expose backups.", nil, "", nil))
	for _, want := range []string{"Backups unavailable", "does not expose backups"} {
		if !strings.Contains(html, want) {
			t.Errorf("unavailable state missing %q", want)
		}
	}
	if strings.Contains(html, "No backups yet") || strings.Contains(html, "Failed to load backups") {
		t.Error("unavailable must not masquerade as empty or as an error")
	}
	if strings.Contains(html, `action="/backups"`) || strings.Contains(html, "Start backup") {
		t.Error("create form should not render when backups are disabled")
	}
}

func TestRenderBackupsPageError(t *testing.T) {
	// Memory unreachable / non-404 failure: real error state
	html := renderHTML(t, BackupsPage(nil, 0, "", errTest, "", nil))
	for _, want := range []string{"Failed to load backups", "backend unreachable"} {
		if !strings.Contains(html, want) {
			t.Errorf("error state missing %q", want)
		}
	}
	if strings.Contains(html, `action="/backups"`) {
		t.Error("create form should not render on the error state")
	}
}

// --- Backup details render tests ---

func TestRenderBackupDetailPage(t *testing.T) {
	ready := testBackups()[0]
	html := renderHTML(t, BackupDetailPage(&ready, nil))

	for _, want := range []string{
		// header + overview
		"Overview", "Acme", "backups/org-1/b1/backup.zip",
		"b1", "1.0 MB", "ready", "full", "user-1",
		// contents statistics
		"Contents", "Documents", "12", "Chunks", "340", "Objects", "88",
		// include settings + retention/type
		"Settings", "Graph", "Chat", "Deleted items", "Journal", "Included", "Excluded",
		"Expires",
		// archive format explainer
		"Archive format", "manifest.json", "ZIP", "files/",
		// integrity checksums, in full
		"Manifest checksum", "Content checksum",
		"deadbeefdeadbeefdeadbeefdeadbeef", "cafebabecafebabecafebabecafebabe",
		// actions + back link
		`href="/backups"`, `href="/backups/b1/download"`, `action="/backups/b1/delete"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
	// checksums must be shown in full, never truncated
	if strings.Contains(html, "deadbeef…beef") || strings.Contains(html, "cafebabe…babe") {
		t.Error("detail page must show full checksums, not truncated labels")
	}
}

func TestRenderBackupDetailPageCreating(t *testing.T) {
	// creating: no checksums, no download, partial stats tolerated
	creating := testBackups()[1]
	html := renderHTML(t, BackupDetailPage(&creating, nil))
	if !strings.Contains(html, "40%") || !strings.Contains(html, "progress") {
		t.Error("creating backup should show a progress bar")
	}
	if strings.Contains(html, `href="/backups/b2/download"`) {
		t.Error("download must not be offered for a creating backup")
	}
	if strings.Contains(html, "Content checksum") || strings.Contains(html, "Manifest checksum") {
		t.Error("creating backup must not present checksums")
	}
	if !strings.Contains(html, "Content counts are not available yet") {
		t.Error("empty stats should show the neutral note")
	}
}

func TestRenderBackupDetailPageFailed(t *testing.T) {
	// failed: error prominent, checksums never presented as valid
	failed := testBackups()[2]
	html := renderHTML(t, BackupDetailPage(&failed, nil))
	if !strings.Contains(html, "storage write timed out") {
		t.Error("failed backup error must be shown")
	}
	if strings.Contains(html, "Manifest checksum") || strings.Contains(html, "Content checksum") {
		t.Error("failed backup must not present checksums")
	}
	if strings.Contains(html, `href="/backups/b3/download"`) {
		t.Error("download must not be offered for a failed backup")
	}
}

func TestRenderBackupDetailPageError(t *testing.T) {
	html := renderHTML(t, BackupDetailPage(nil, errTest))
	for _, want := range []string{"Backup unavailable", "backend unreachable"} {
		if !strings.Contains(html, want) {
			t.Errorf("detail error state missing %q", want)
		}
	}
	if strings.Contains(html, "Overview") {
		t.Error("error state should not render the details sections")
	}
}

// --- UI route handler tests ---

// newBackupsEcho registers the /backups UI routes against a fake backend.
func newBackupsEcho(f *fakeMemory) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/backups", s.uiBackups)
	e.POST("/backups", s.uiBackupCreate)
	e.GET("/backups/:id", s.uiBackupDetail)
	e.GET("/backups/:id/download", s.uiBackupDownload)
	e.POST("/backups/:id/delete", s.uiBackupDelete)
	return s, e
}

// backupProject returns a fake backend whose current project carries an org,
// so the org-scoped backup routes resolve (design D3).
func backupProject() *Project {
	return &Project{ID: "proj-1", Name: "Acme", OrgID: "org-1"}
}

func TestUIBackupsPage(t *testing.T) {
	f := &fakeMemory{
		project: backupProject(),
		backups: testBackups(),
	}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Backups", "Acme", "ready", "creating", "failed", `action="/backups"`, `href="/backups/b1/download"`, "3 backup"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestUIBackupsPageEmpty(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No backups yet") {
		t.Error("no-backups state missing")
	}
}

func TestUIBackupsPageNoOrg(t *testing.T) {
	// active project carries no org → clear error state, never a 500 (D3)
	f := &fakeMemory{project: &Project{ID: "proj-1", Name: "Acme"}}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (page error state)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "no organization is bound to the active project") {
		t.Errorf("clear no-org error missing: %s", body)
	}
}

func TestUIBackupsPageMemoryUnreachable(t *testing.T) {
	f := &fakeMemory{projectErr: errTest} // project lookup transport failure
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (page error state)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Failed to load backups") || !strings.Contains(body, "backend unreachable") {
		t.Errorf("unreachable error state missing: %s", body)
	}
}

func TestUIBackupsPageFeatureGated(t *testing.T) {
	// Memory answered 404 on the list (Config.Features.Backups off): the page
	// degrades to "unavailable" (design D6), NOT a hard error.
	f := &fakeMemory{
		project:   backupProject(),
		backupErr: fmt.Errorf("memory 404 not_found: route not registered"),
	}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Backups unavailable") || !strings.Contains(body, "does not expose backups") {
		t.Errorf("feature-gated state should degrade to unavailable: %s", body)
	}
	if strings.Contains(body, "Failed to load backups") {
		t.Error("feature-gated 404 must not surface as an error")
	}
}

func TestUIBackupsPageNon404Error(t *testing.T) {
	f := &fakeMemory{
		project:   backupProject(),
		backupErr: fmt.Errorf("memory 503: upstream unavailable"),
	}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Failed to load backups") {
		t.Error("non-404 error must surface as a real error state")
	}
}

func TestUIBackupDetail(t *testing.T) {
	f := &fakeMemory{project: backupProject(), backups: testBackups()}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/b1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Overview", "Contents", "Objects", "88", "Manifest checksum", "deadbeefdeadbeefdeadbeefdeadbeef", `href="/backups/b1/download"`} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
}

func TestUIBackupDetailNotFound(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/nope", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (page error state, not 500)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Backup unavailable") || !strings.Contains(body, "not found") {
		t.Errorf("missing-backup error state missing: %s", body)
	}
}

func TestUIBackupDetailNoOrg(t *testing.T) {
	f := &fakeMemory{project: &Project{ID: "proj-1", Name: "Acme"}}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/b1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (page error state)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no organization is bound to the active project") {
		t.Errorf("no-org error state missing: %s", rec.Body.String())
	}
}

func TestUIBackupCreate(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)
	form := url.Values{"includeDeleted": {"on"}, "includeChat": {"on"}, "retentionDays": {"14"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/backups?created=1" {
		t.Errorf("location = %q", loc)
	}
	if !f.createdBackupReq.IncludeDeleted || !f.createdBackupReq.IncludeChat || f.createdBackupReq.RetentionDays != 14 {
		t.Errorf("forwarded request = %+v", f.createdBackupReq)
	}
}

func TestUIBackupCreateDefaultsRetention(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.createdBackupReq.IncludeDeleted || f.createdBackupReq.IncludeChat {
		t.Error("empty checkboxes should forward false")
	}
	if f.createdBackupReq.RetentionDays != 30 {
		t.Errorf("retention should default to 30, got %d", f.createdBackupReq.RetentionDays)
	}
}

func TestUIBackupCreateRejected(t *testing.T) {
	f := &fakeMemory{
		project:          backupProject(),
		createdBackupErr: fmt.Errorf("memory 400 bad_request: retention_days must be between 1 and 365"),
	}
	_, e := newBackupsEcho(f)
	form := url.Values{"retentionDays": {"999"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/backups?err=") {
		t.Errorf("location = %q, want error flash", loc)
	}
}

func TestUIBackupCreateBadRetentionInput(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)
	form := url.Values{"retentionDays": {"thirty"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/backups", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "retentionDays+must+be+a+number") {
		t.Errorf("location = %q", loc)
	}
	if f.createdBackupReq.RetentionDays != 0 {
		t.Error("no create should be forwarded for a malformed retention value")
	}
}

func TestUIBackupDownloadReady(t *testing.T) {
	f := &fakeMemory{
		project:     backupProject(),
		backups:     []Backup{{ID: "b1", Status: backupStatusReady}},
		downloadURL: "https://storage.example.com/backup.zip?sig=x",
	}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/b1/download", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to the presigned URL", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != f.downloadURL {
		t.Errorf("location = %q, want %q", loc, f.downloadURL)
	}
}

func TestUIBackupDownloadNotReady(t *testing.T) {
	f := &fakeMemory{
		project: backupProject(),
		backups: []Backup{{ID: "b1", Status: backupStatusCreating}},
	}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/b1/download", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (no download for a non-ready backup)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/backups?err=") || !strings.Contains(loc, "not+ready") {
		t.Errorf("location = %q, want not-ready error flash", loc)
	}
}

func TestUIBackupDownloadMissing(t *testing.T) {
	f := &fakeMemory{project: backupProject()} // no stored backup b1
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/nope/download", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 error flash", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "?err=") {
		t.Errorf("location = %q", rec.Header().Get("Location"))
	}
}

func TestUIBackupDelete(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/backups/b1/delete", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/backups?deleted=1" {
		t.Errorf("location = %q", loc)
	}
	if f.deletedBackupID != "b1" {
		t.Errorf("deleted id = %q, want b1", f.deletedBackupID)
	}
}

func TestUIBackupDeleteError(t *testing.T) {
	f := &fakeMemory{project: backupProject(), deletedBackupErr: errTest}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/backups/b1/delete", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/backups?err=") {
		t.Errorf("location = %q, want error flash", loc)
	}
}

// TestUIBackupsFlash renders the PRG flash messages on the list page.
func TestUIBackupsFlash(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups?created=1", nil))
	if !strings.Contains(rec.Body.String(), "Backup started") {
		t.Error("created flash missing")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups?deleted=1", nil))
	if !strings.Contains(rec.Body.String(), "Backup deleted.") {
		t.Error("deleted flash missing")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups?err=1", nil))
	if !strings.Contains(rec.Body.String(), "operation failed") {
		t.Error("err flash missing")
	}
}

// --- backupsOrgID org resolution (design D3) ---

func TestBackupsOrgIDFromSessionOrg(t *testing.T) {
	// A web session carrying the org resolves directly, no memory calls.
	s := &Server{memory: &fakeMemory{}}
	ctx := withSessionContext(context.Background(), &sessionContext{OrgID: "org-1"})
	orgID, err := s.backupsOrgID(ctx)
	if err != nil {
		t.Fatalf("backupsOrgID = %v", err)
	}
	if orgID != "org-1" {
		t.Errorf("orgID = %q, want org-1", orgID)
	}
}

func TestBackupsOrgIDFromSessionProject(t *testing.T) {
	// A web session carrying only the active project recovers the org from the
	// tenancy listing (the user access token is not project-bound).
	f := &fakeMemory{projects: []ProjectRef{{ID: "proj-1", Name: "Acme", OrgID: "org-1"}}}
	s := &Server{memory: f}
	ctx := withSessionContext(context.Background(), &sessionContext{ProjectID: "proj-1"})
	orgID, err := s.backupsOrgID(ctx)
	if err != nil {
		t.Fatalf("backupsOrgID = %v", err)
	}
	if orgID != "org-1" {
		t.Errorf("orgID = %q, want org-1", orgID)
	}
}

func TestBackupsOrgIDFromSessionProjectUnknown(t *testing.T) {
	// Project not in the listing (or org-less) → clear no-org error.
	s := &Server{memory: &fakeMemory{projects: []ProjectRef{{ID: "other", Name: "Other", OrgID: "org-other"}}}}
	ctx := withSessionContext(context.Background(), &sessionContext{ProjectID: "proj-ghost"})
	if _, err := s.backupsOrgID(ctx); !errors.Is(err, errBackupsNoOrg) {
		t.Fatalf("backupsOrgID err = %v, want errBackupsNoOrg", err)
	}
}

func TestBackupsOrgIDFromSessionNoProject(t *testing.T) {
	// Session with neither org nor project → clear no-org error.
	s := &Server{memory: &fakeMemory{}}
	ctx := withSessionContext(context.Background(), &sessionContext{})
	if _, err := s.backupsOrgID(ctx); !errors.Is(err, errBackupsNoOrg) {
		t.Fatalf("backupsOrgID err = %v, want errBackupsNoOrg", err)
	}
}
