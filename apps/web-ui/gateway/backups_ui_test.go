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
	html := renderHTML(t, BackupsPage(backups, 3, nil, false, "", nil, "", nil))

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
	// create is a header action linking to the dedicated page, not an inline form
	for _, want := range []string{`href="/backups/new"`, "Create a backup"} {
		if !strings.Contains(html, want) {
			t.Errorf("list header action missing %q", want)
		}
	}
	for _, unwanted := range []string{`action="/backups"`, `id="backup-retention"`, "Start backup"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("list page must not render the create form (%q found)", unwanted)
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
	html := renderHTML(t, BackupsPage(creating, 1, nil, false, "", nil, "", nil))
	if !strings.Contains(html, "window.location.reload") {
		t.Error("auto-refresh poll missing while a backup is creating")
	}
	if !strings.Contains(html, "refreshes automatically") {
		t.Error("progress note missing")
	}

	terminal := []Backup{{ID: "b1", ProjectName: "Acme", Status: backupStatusReady, Progress: 100, CreatedAt: time.Now()}}
	htmlDone := renderHTML(t, BackupsPage(terminal, 1, nil, false, "", nil, "", nil))
	if strings.Contains(htmlDone, "window.location.reload") {
		t.Error("poll must stop once every backup is terminal")
	}
}

func TestRenderBackupsPageEmpty(t *testing.T) {
	html := renderHTML(t, BackupsPage(nil, 0, nil, false, "", nil, "", nil))
	if !strings.Contains(html, "No backups yet") {
		t.Error("empty state missing")
	}
	if !strings.Contains(html, `href="/backups/new"`) {
		t.Error("create action should still be offered when there are no backups")
	}
}

func TestRenderBackupsPageUnavailable(t *testing.T) {
	// feature-gated degrade (design D6): unavailable state, not an error
	html := renderHTML(t, BackupsPage(nil, 0, nil, false, "The connected Memory service does not expose backups.", nil, "", nil))
	for _, want := range []string{"Backups unavailable", "does not expose backups"} {
		if !strings.Contains(html, want) {
			t.Errorf("unavailable state missing %q", want)
		}
	}
	if strings.Contains(html, "No backups yet") || strings.Contains(html, "Failed to load backups") {
		t.Error("unavailable must not masquerade as empty or as an error")
	}
	if strings.Contains(html, `href="/backups/new"`) || strings.Contains(html, "Create a backup") || strings.Contains(html, "Start backup") {
		t.Error("create action should not render when backups are disabled")
	}
}

func TestRenderBackupsPageError(t *testing.T) {
	// Memory unreachable / non-404 failure: real error state
	html := renderHTML(t, BackupsPage(nil, 0, nil, false, "", errTest, "", nil))
	for _, want := range []string{"Failed to load backups", "backend unreachable"} {
		if !strings.Contains(html, want) {
			t.Errorf("error state missing %q", want)
		}
	}
	if strings.Contains(html, `href="/backups/new"`) {
		t.Error("create action should not render on the error state")
	}
}

func TestRenderBackupNewPage(t *testing.T) {
	html := renderHTML(t, BackupNewPage(nil))

	// the create page is its own page: header chrome + the form, posting to the
	// existing create route
	for _, want := range []string{
		"New backup", `href="/backups"`,
		`action="/backups"`, `method="post"`,
		`id="backup-retention"`, `name="retentionDays"`,
		`name="includeDeleted"`, `name="includeChat"`,
		"Start backup",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("new-backup page missing %q", want)
		}
	}
	// the standalone page uses the normal form width, not the narrow cap the
	// inline list form carried
	if strings.Contains(html, "sm:max-w-40") {
		t.Error("new-backup page should not cap the retention input at sm:max-w-40")
	}
}

func TestRenderBackupNewPageFlashError(t *testing.T) {
	html := renderHTML(t, BackupNewPage(fmt.Errorf("retentionDays must be a number of days")))
	if !strings.Contains(html, "retentionDays must be a number of days") {
		t.Error("?err= feedback should render on the create page")
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
	e.GET("/backups/new", s.uiBackupNewPage)
	e.POST("/backups", s.uiBackupCreate)
	e.GET("/backups/:id", s.uiBackupDetail)
	e.GET("/backups/:id/download", s.uiBackupDownload)
	e.POST("/backups/:id/delete", s.uiBackupDelete)
	e.POST("/backups/schedule", s.uiBackupScheduleSave)
	e.POST("/backups/schedule/delete", s.uiBackupScheduleDelete)
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
	for _, want := range []string{"Backups", "Acme", "ready", "creating", "failed", `href="/backups/new"`, `href="/backups/b1/download"`, "3 backup"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, `action="/backups"`) {
		t.Error("list page must not render the create form; it lives on /backups/new")
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
	f := &fakeMemory{projectErr: &url.Error{ // project lookup transport failure
		Op:  "Get",
		URL: "http://172.17.0.1:8080/api/projects/current",
		Err: errors.New("dial tcp 172.17.0.1:8080: connect: connection refused"),
	}}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (page error state)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Failed to load backups") || !strings.Contains(body, memoryUnavailableMessage) {
		t.Errorf("unreachable error state missing sanitized message: %s", body)
	}
	for _, leak := range []string{"172.17.0.1", "dial tcp", ":8080", "memory 5"} {
		if strings.Contains(body, leak) {
			t.Errorf("unreachable error state leaked %q", leak)
		}
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

func TestUIBackupNewPage(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`id="backup-retention"`, `name="includeDeleted"`, `name="includeChat"`, `action="/backups"`, "Start backup"} {
		if !strings.Contains(body, want) {
			t.Errorf("new-backup page missing %q", want)
		}
	}
}

// TestUIBackupNewPageFlashError covers the ?err= landing on the create page
// (the POST flow still redirects to the list, but the page consumes the same
// flash key for direct visits).
func TestUIBackupNewPageFlashError(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups/new?err=retentionDays+must+be+a+number+of+days", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "retentionDays must be a number of days") {
		t.Error("?err= flash missing on the create page")
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

// --- Scheduled backups: render ---

// TestRenderBackupsPageSchedule covers the card for a configured schedule:
// state badge, next/last fire, the preset selection and the delete affordance.
func TestRenderBackupsPageSchedule(t *testing.T) {
	next := time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC)
	last := time.Date(2026, 11, 7, 2, 0, 0, 0, time.UTC)
	lastID := "b1"
	schedule := &BackupSchedule{
		Enabled:       true,
		CronSchedule:  "0 2 * * *",
		RetentionDays: 30,
		NextRunAt:     next,
		LastRunAt:     &last,
		LastBackupID:  &lastID,
	}
	html := renderHTML(t, BackupsPage(nil, 0, schedule, false, "", nil, "", nil))

	for _, want := range []string{
		"Scheduled backups",
		"Enabled",
		"Next run 2026-11-08 02:00 UTC",
		"Last run 2026-11-07 02:00 UTC",
		`href="/backups/b1"`,
		`action="/backups/schedule"`,
		`action="/backups/schedule/delete"`,
		`value="30"`,
		`value="0 2 * * *" selected`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("schedule card missing %q", want)
		}
	}
	if strings.Contains(html, "Not configured") {
		t.Error("configured schedule should not render the not-configured badge")
	}
	if strings.Contains(html, `value="custom" selected`) {
		t.Error("a preset cadence should not select the custom option")
	}
}

// TestRenderBackupsPageScheduleAbsent covers the no-schedule card: the form
// renders with the daily preset and enabled defaulted, and there is nothing to
// delete yet.
func TestRenderBackupsPageScheduleAbsent(t *testing.T) {
	html := renderHTML(t, BackupsPage(nil, 0, nil, false, "", nil, "", nil))

	for _, want := range []string{
		"Scheduled backups",
		"Not configured",
		`action="/backups/schedule"`,
		`value="0 2 * * *" selected`,
		`value="30"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("empty schedule card missing %q", want)
		}
	}
	if strings.Contains(html, `action="/backups/schedule/delete"`) {
		t.Error("delete form must not render when no schedule exists")
	}
}

// TestRenderBackupsPageScheduleCustomCron covers a stored expression outside the
// preset list: the custom option is selected and the field carries the value.
func TestRenderBackupsPageScheduleCustomCron(t *testing.T) {
	schedule := &BackupSchedule{
		Enabled:       false,
		CronSchedule:  "15 4 * * 1-5",
		RetentionDays: 14,
		NextRunAt:     time.Date(2026, 11, 9, 4, 15, 0, 0, time.UTC),
	}
	html := renderHTML(t, BackupsPage(nil, 0, schedule, false, "", nil, "", nil))

	for _, want := range []string{
		"Disabled",
		`value="custom" selected`,
		`value="15 4 * * 1-5"`,
		`value="14"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("custom schedule card missing %q", want)
		}
	}
	if strings.Contains(html, `value="0 2 * * *" selected`) {
		t.Error("a custom expression should not select a preset")
	}
}

// --- Scheduled backups: handlers ---

// TestUIBackupScheduleSave covers the preset cadence path: the select posts the
// cron expression directly and the checkbox/retention/includes are forwarded.
func TestUIBackupScheduleSave(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)

	form := url.Values{
		"enabled":        {"on"},
		"cadence":        {"0 */6 * * *"},
		"retentionDays":  {"7"},
		"includeChat":    {"on"},
		"includeJournal": {"on"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/backups/schedule", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/backups?schedule_saved=1" {
		t.Errorf("location = %q", loc)
	}
	got := f.savedBackupSchedule
	if got == nil {
		t.Fatal("schedule was not saved")
	}
	if !got.Enabled || got.CronSchedule != "0 */6 * * *" || got.RetentionDays != 7 || !got.IncludeChat || !got.IncludeJournal || got.IncludeDeleted {
		t.Errorf("forwarded request = %+v", got)
	}
}

// TestUIBackupScheduleSaveCustomCron covers the custom option: the free-text
// cron field is used instead of the select value.
func TestUIBackupScheduleSaveCustomCron(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)

	form := url.Values{"cadence": {"custom"}, "cron": {" 15 4 * * 1-5 "}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/backups/schedule", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	got := f.savedBackupSchedule
	if got == nil {
		t.Fatal("schedule was not saved")
	}
	if got.CronSchedule != "15 4 * * 1-5" {
		t.Errorf("cron = %q, want the trimmed custom value", got.CronSchedule)
	}
	if got.Enabled {
		t.Error("an absent enabled checkbox must forward disabled")
	}
	if got.RetentionDays != 30 {
		t.Errorf("retention should default to 30, got %d", got.RetentionDays)
	}
}

// TestUIBackupScheduleSaveRejected covers memory rejecting the write (bad cron
// or retention): the message goes to the ?err= flash, not a 500 — and the flash
// carries the sanitized validation copy, not the raw transport prefix or the
// internal "invalid backup schedule:" sentinel.
func TestUIBackupScheduleSaveRejected(t *testing.T) {
	f := &fakeMemory{
		project: backupProject(),
		saveScheduleErr: &memoryHTTPError{
			Status:  http.StatusBadRequest,
			Code:    "bad_request",
			Message: `invalid backup schedule: cron schedule "*/5 * * * *" fires every 5m0s; the minimum interval is 15 minutes`,
		},
	}
	_, e := newBackupsEcho(f)

	form := url.Values{"cadence": {"*/5 * * * *"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/backups/schedule", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/backups?err=") {
		t.Errorf("location = %q, want error flash", loc)
	}
	// The flash must be the sentence, stripped of both the transport prefix and
	// the internal sentinel.
	got, err := url.QueryUnescape(strings.TrimPrefix(loc, "/backups?err="))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	for _, unwanted := range []string{"memory 400 bad_request:", "invalid backup schedule:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("flash leaks %q: %q", unwanted, got)
		}
	}
	if !strings.Contains(got, "minimum interval is 15 minutes") {
		t.Errorf("flash dropped the actionable copy: %q", got)
	}
}

// TestUIBackupScheduleDelete covers the delete form PRG.
func TestUIBackupScheduleDelete(t *testing.T) {
	f := &fakeMemory{project: backupProject()}
	_, e := newBackupsEcho(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/backups/schedule/delete", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/backups?schedule_deleted=1" {
		t.Errorf("location = %q", loc)
	}
	if !f.deletedSchedule {
		t.Error("delete was not forwarded")
	}
}

// TestUIBackupsPageShowsSchedule proves the page loads the schedule alongside the
// list, and that a configured schedule renders on the real handler path.
func TestUIBackupsPageShowsSchedule(t *testing.T) {
	f := &fakeMemory{
		project: backupProject(),
		backups: testBackups(),
		backupSchedule: &BackupSchedule{
			Enabled:       true,
			CronSchedule:  "0 2 * * *",
			RetentionDays: 30,
			NextRunAt:     time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC),
		},
	}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Scheduled backups", "Next run 2026-11-08 02:00 UTC", `action="/backups/schedule/delete"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

// TestRenderBackupsPageScheduleUnavailable covers a schedule that could not be
// read: the card says so and offers no form, so a user cannot overwrite a
// schedule the page could not see with the form's default values.
func TestRenderBackupsPageScheduleUnavailable(t *testing.T) {
	html := renderHTML(t, BackupsPage(nil, 0, nil, true, "", nil, "", nil))

	for _, want := range []string{
		"Scheduled backups",
		"Unavailable",
		"Could not load the saved backup schedule",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("unavailable schedule card missing %q", want)
		}
	}
	for _, unwanted := range []string{`action="/backups/schedule"`, `action="/backups/schedule/delete"`, "Not configured"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("unavailable schedule card must not render %q", unwanted)
		}
	}
}

// TestUIBackupsPageScheduleLoadFailure proves the page distinguishes "could not
// read the schedule" from "not configured" while still rendering the backup
// list that loaded.
func TestUIBackupsPageScheduleLoadFailure(t *testing.T) {
	f := &fakeMemory{
		project:           backupProject(),
		backups:           testBackups(),
		backupScheduleErr: fmt.Errorf("memory 503: upstream unavailable"),
	}
	_, e := newBackupsEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Unavailable") {
		t.Error("page should mark the schedule unavailable when the read fails")
	}
	if strings.Contains(body, `action="/backups/schedule"`) {
		t.Error("the schedule form must not render when the schedule could not be read")
	}
	if !strings.Contains(body, `href="/backups/b1/download"`) {
		t.Error("a schedule read failure must not hide the backup list")
	}
	// The failure must show exactly once — in the card's own "Could not load the
	// saved backup schedule" paragraph, not also as a flash toast.
	if !strings.Contains(body, "Could not load the saved backup schedule") {
		t.Error("the card must explain the schedule read failure in place")
	}
	if strings.Contains(body, memoryUnavailableMessage) {
		t.Error("the schedule read failure must not be flashed a second time")
	}
}
