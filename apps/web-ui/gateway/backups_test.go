package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestBackupsEntityRoundTrip decodes a full memory Backup entity (all nullable
// fields populated) to confirm the gateway struct mirrors the wire shape.
func TestBackupsEntityRoundTrip(t *testing.T) {
	wire := `{
		"id":"b1","organizationId":"org-1","projectId":"proj-1","projectName":"Acme",
		"storageKey":"backups/org-1/b1/backup.zip","sizeBytes":1048576,
		"status":"ready","progress":100,"errorMessage":null,
		"backupType":"full",
		"includes":{"documents":true,"chunks":true,"graph":true,"chat":false,"deleted":true},
		"stats":{"documents":12,"chunks":340,"graphObjects":88,"chatMessages":5},
		"createdAt":"2026-08-26T10:00:00Z","createdBy":"user-1",
		"completedAt":"2026-08-26T10:00:42Z","expiresAt":"2026-09-25T10:00:00Z","deletedAt":null,
		"manifestChecksum":"deadbeefdeadbeefdeadbeefdeadbeef","contentChecksum":"cafebabecafebabecafebabecafebabe",
		"parentBackupId":null,"baselineBackupId":null,"changeWindow":null
	}`
	var b Backup
	if err := json.Unmarshal([]byte(wire), &b); err != nil {
		t.Fatal(err)
	}
	if b.ID != "b1" || b.OrganizationID != "org-1" || b.ProjectName != "Acme" || b.StorageKey == "" {
		t.Errorf("identity fields wrong: %+v", b)
	}
	if b.SizeBytes != 1048576 || b.Status != backupStatusReady || b.Progress != 100 {
		t.Errorf("status fields wrong: %+v", b)
	}
	if b.BackupType != "full" {
		t.Errorf("type = %q", b.BackupType)
	}
	if b.CreatedAt.IsZero() || b.CompletedAt == nil || b.ExpiresAt == nil {
		t.Errorf("times not parsed: %+v", b)
	}
	if b.CreatedBy == nil || *b.CreatedBy != "user-1" {
		t.Errorf("createdBy = %v", b.CreatedBy)
	}
	if b.ManifestChecksum == nil || b.ContentChecksum == nil {
		t.Error("checksums not parsed")
	}
	if b.ErrorMessage != nil || b.DeletedAt != nil || b.ParentBackupID != nil {
		t.Error("null fields should decode to nil pointers")
	}
	// nullable fields absent entirely (omitempty on the wire) decode to nil.
	var bare Backup
	if err := json.Unmarshal([]byte(`{"id":"b2","createdAt":"2026-08-26T10:00:00Z"}`), &bare); err != nil {
		t.Fatal(err)
	}
	if bare.ErrorMessage != nil || bare.CompletedAt != nil || bare.CreatedBy != nil || bare.ManifestChecksum != nil {
		t.Error("absent nullable fields should decode to nil")
	}
}

// TestListBackups exercises GET /api/v1/organizations/:orgId/backups: the
// /api/v1 prefix, the project_id filter, and response parsing.
func TestListBackups(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"backups":[
			{"id":"b1","organizationId":"org-1","projectId":"proj-1","projectName":"Acme","status":"ready","progress":100,"backupType":"full","sizeBytes":2048,"createdAt":"2026-08-26T10:00:00Z","manifestChecksum":"aaaa","contentChecksum":"bbbb"},
			{"id":"b2","organizationId":"org-1","projectId":"proj-1","projectName":"Acme","status":"creating","progress":40,"backupType":"full","sizeBytes":0,"createdAt":"2026-08-26T11:00:00Z"}
		],"total":7,"nextCursor":{"createdAt":"2026-08-26T11:00:00Z","id":"b2"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	backups, total, err := m.ListBackups(context.Background(), "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/organizations/org-1/backups?limit=100&project_id=proj-1" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if total != 7 {
		t.Errorf("total = %d, want 7", total)
	}
	if len(backups) != 2 {
		t.Fatalf("got %d backups, want 2", len(backups))
	}
	if backups[0].ID != "b1" || backups[0].Status != backupStatusReady || backups[0].SizeBytes != 2048 {
		t.Errorf("b1 = %+v", backups[0])
	}
	if backups[1].Status != backupStatusCreating || backups[1].Progress != 40 {
		t.Errorf("b2 = %+v", backups[1])
	}
}

// TestListBackupsEmpty covers the empty list: no backups is not an error.
func TestListBackupsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"backups":[],"total":0}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	backups, total, err := m.ListBackups(context.Background(), "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(backups) != 0 {
		t.Errorf("got %d backups / total %d, want none", len(backups), total)
	}
}

// TestListBackupsNotFound covers the feature-gate path (design D6): a 404 from
// the list must be identifiable so the UI degrades to "unavailable" rather
// than an error.
func TestListBackupsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"not found"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, _, err := m.ListBackups(context.Background(), "org-1"); err == nil {
		t.Fatal("want error, got nil")
	} else if !isMemoryNotFound(err) {
		t.Errorf("404 should be identifiable as not-found, got %v", err)
	}
}

// TestListBackupsError covers a non-404 failure: it must NOT be mistaken for
// the feature-gated "unavailable" degrade.
func TestListBackupsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"down"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, _, err := m.ListBackups(context.Background(), "org-1"); err == nil {
		t.Fatal("want error, got nil")
	} else if isMemoryNotFound(err) {
		t.Errorf("503 should NOT look like not-found: %v", err)
	}
}

// TestCreateBackup exercises POST /api/v1/projects/:projectId/backups: the
// request body (includeDeleted/includeChat/retentionDays), the 202 response,
// and the "creating" entity returned.
func TestCreateBackup(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody CreateBackupRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"b-new","organizationId":"org-1","projectId":"proj-1","projectName":"Acme","status":"creating","progress":0,"backupType":"full","sizeBytes":0,"createdAt":"2026-08-26T12:00:00Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	b, err := m.CreateBackup(context.Background(), true, true, 14)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/projects/proj-1/backups" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if !gotBody.IncludeDeleted || !gotBody.IncludeChat || gotBody.RetentionDays != 14 {
		t.Errorf("body = %+v", gotBody)
	}
	if b.ID != "b-new" || b.Status != backupStatusCreating {
		t.Errorf("backup = %+v", b)
	}
}

// TestCreateBackupRejected covers memory rejecting the create (e.g. invalid
// retention): the error surfaces and no phantom backup exists.
func TestCreateBackupRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"bad_request","message":"retention_days must be between 1 and 365"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.CreateBackup(context.Background(), false, false, 999); err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestGetBackup exercises GET /api/v1/organizations/:orgId/backups/:backupId.
func TestGetBackup(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"b1","organizationId":"org-1","projectId":"proj-1","projectName":"Acme","status":"ready","progress":100,"backupType":"full","sizeBytes":1024,"createdAt":"2026-08-26T10:00:00Z","completedAt":"2026-08-26T10:00:42Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	b, err := m.GetBackup(context.Background(), "org-1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/organizations/org-1/backups/b1" {
		t.Errorf("path = %q", gotPath)
	}
	if b.ID != "b1" || b.Status != backupStatusReady || b.CompletedAt == nil {
		t.Errorf("backup = %+v", b)
	}
}

// TestDownloadBackupReady exercises the 302 flow: the client must NOT follow
// the redirect to the object store; it returns the Location header (the
// presigned URL) instead.
func TestDownloadBackupReady(t *testing.T) {
	hits := 0
	const presigned = "https://storage.example.com/backups/org-1/b1/backup.zip?X-Amz-Signature=abc"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits > 1 {
			t.Errorf("client followed the redirect (hits=%d)", hits)
		}
		w.Header().Set("Location", presigned)
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	got, err := m.DownloadBackup(context.Background(), "org-1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if got != presigned {
		t.Errorf("url = %q, want %q", got, presigned)
	}
	if hits != 1 {
		t.Errorf("server hits = %d, want 1 (redirect must not be followed)", hits)
	}
}

// TestDownloadBackupNotReady covers memory rejecting the download of a backup
// that is not status "ready" (400): the client surfaces the error and returns
// no URL.
func TestDownloadBackupNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"bad_request","message":"backup is not ready for download"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if u, err := m.DownloadBackup(context.Background(), "org-1", "b1"); err == nil {
		t.Fatalf("want error for non-ready download, got url %q", u)
	}
}

// TestDownloadBackupMissing covers a 404 from the download route (backup
// gone): surfaced as an error, identifiably not-found.
func TestDownloadBackupMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"backup not found"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if _, err := m.DownloadBackup(context.Background(), "org-1", "nope"); err == nil {
		t.Fatal("want error, got nil")
	} else if !isMemoryNotFound(err) {
		t.Errorf("404 should be identifiable, got %v", err)
	}
}

// TestDeleteBackup exercises DELETE /api/v1/organizations/:orgId/backups/
// :backupId (204 no body).
func TestDeleteBackup(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.DeleteBackup(context.Background(), "org-1", "b1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/organizations/org-1/backups/b1" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
}

// TestBackupDisplayHelpers covers the row-formatting helpers shared with the
// template.
func TestBackupDisplayHelpers(t *testing.T) {
	if got := backupSizeLabel(0); got != "0 B" {
		t.Errorf("size 0 = %q", got)
	}
	if got := backupSizeLabel(1536); got != "1.5 KB" {
		t.Errorf("size 1536 = %q", got)
	}
	if got := backupSizeLabel(5 * 1 << 20); got != "5.0 MB" {
		t.Errorf("size 5MB = %q", got)
	}
	if got := backupSizeLabel(2 * 1 << 30); got != "2.0 GB" {
		t.Errorf("size 2GB = %q", got)
	}
	if backupStatusAnimate(backupStatusCreating) != true || backupStatusAnimate(backupStatusReady) != false {
		t.Error("animate should pulse only while creating")
	}
	if got := backupStatusLabel(""); got != "unknown" {
		t.Errorf("empty status label = %q", got)
	}
	if got := backupTypeLabel(""); got != "full" {
		t.Errorf("empty type label = %q", got)
	}
	long := "abcdef0123456789abcdef0123456789"
	got := checksumLabel(&long)
	if got != "abcdef01…6789" {
		t.Errorf("checksumLabel = %q", got)
	}
	if checksumLabel(nil) != "" {
		t.Error("nil checksum should be empty")
	}
	expiry := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	if got := backupDate(&expiry); got != "2026-09-25" {
		t.Errorf("backupDate = %q", got)
	}
	if backupDate(nil) != "—" {
		t.Error("nil expiry should be em-dash")
	}
	if !backupsAnyCreating([]Backup{{Status: backupStatusReady}, {Status: backupStatusCreating}}) {
		t.Error("creating backup should trip the poll")
	}
	if backupsAnyCreating([]Backup{{Status: backupStatusReady}, {Status: backupStatusFailed}}) {
		t.Error("terminal backups should stop the poll")
	}

	stats := map[string]any{"documents": 12.0, "chunks": 340.0, "graphObjects": 88.0, "graphRelationships": 0.0, "chatMessages": 5.0}
	if got := backupStatsSummary(stats); got != "12 docs · 340 chunks · 88 objects · 5 messages" {
		t.Errorf("stats summary = %q", got)
	}
	if backupStatsSummary(nil) != "" {
		t.Error("nil stats should summarize to empty")
	}
	includes := map[string]any{"documents": true, "chat": true, "deleted": true}
	if got := backupIncludesLabel(includes); got != "chat, deleted items" {
		t.Errorf("includes label = %q", got)
	}
	if backupMetaLine(Backup{ID: "b1", ProjectName: "Acme", Stats: stats}) == "" {
		t.Error("meta line should not be empty for a ready-shaped backup")
	}
	if backupMetaLine(Backup{ID: "b1"}) != "" {
		t.Errorf("bare meta line = %q, want empty (no plain-text type fallback)", backupMetaLine(Backup{ID: "b1"}))
	}
	if got := backupTitle(Backup{ID: "b1", ProjectName: "Acme"}); got != "Acme" {
		t.Errorf("title = %q", got)
	}
	if got := backupTitle(Backup{ID: "b1"}); !strings.HasPrefix(got, "Backup ") {
		t.Errorf("fallback title = %q", got)
	}
}

// TestBackupStatRows covers the details-page statistics flattening: known keys
// only, labelled in a fixed order, with totalSizeBytes sized and unknown keys
// dropped.
func TestBackupStatRows(t *testing.T) {
	rows := backupStatRows(map[string]any{
		"documents":          12.0,
		"chunks":             340.0,
		"graphObjects":       88.0,
		"graphRelationships": 0.0,
		"chatConversations":  2.0,
		"chatMessages":       5.0,
		"extractionJobs":     1.0,
		"projectMemberships": 3.0,
		"files":              7.0,
		"totalSizeBytes":     1536.0,
		"unknownKey":         99.0,
	})
	if len(rows) != 10 {
		t.Fatalf("got %d rows, want 10 (unknown key dropped): %+v", len(rows), rows)
	}
	if rows[0].Label != "Documents" || rows[0].Value != "12" {
		t.Errorf("first row = %+v, want Documents/12", rows[0])
	}
	if rows[2].Label != "Objects" || rows[2].Value != "88" {
		t.Errorf("objects row = %+v, want Objects/88", rows[2])
	}
	if rows[3].Label != "Relationships" || rows[3].Value != "0" {
		t.Errorf("relationships row = %+v, want a present zero to be kept", rows[3])
	}
	if last := rows[len(rows)-1]; last.Key != "totalSizeBytes" || last.Value != "1.5 KB" {
		t.Errorf("total size row = %+v, want totalSizeBytes/1.5 KB", last)
	}
	for _, r := range rows {
		if r.Key == "unknownKey" {
			t.Error("unknown stats key should be dropped")
		}
	}
	if got := backupStatRows(nil); len(got) != 0 {
		t.Errorf("nil stats = %+v, want no rows", got)
	}
	// Absent keys are omitted even when other keys exist.
	partial := backupStatRows(map[string]any{"documents": 1})
	if len(partial) != 1 || partial[0].Key != "documents" {
		t.Errorf("partial stats = %+v, want just documents", partial)
	}
}
