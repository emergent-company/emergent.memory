package backups_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/backups"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
)

func backupFixture() map[string]interface{} {
	return map[string]interface{}{
		"id":             "backup_1",
		"organizationId": "org_test",
		"projectId":      "proj_test",
		"projectName":    "MyProject",
		"storageKey":     "backups/backup_1.zip",
		"sizeBytes":      1024,
		"status":         "creating",
		"progress":       0,
		"backupType":     "full",
		"includes":       map[string]interface{}{"documents": true},
		"createdAt":      "2026-01-01T00:00:00Z",
		"imported":       false,
	}
}

func TestBackupsCreateBackup(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/v1/projects/proj_test/backups", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "X-Project-ID", "proj_test")

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["includeDeleted"] != true {
			t.Errorf("expected includeDeleted=true, got %v", body["includeDeleted"])
		}
		if body["retentionDays"] != float64(30) {
			t.Errorf("expected retentionDays=30, got %v", body["retentionDays"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		testutil.JSONResponse(t, w, backupFixture())
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
		ProjectID: "proj_test",
	})

	backup, err := client.Backups.CreateBackup(context.Background(), "proj_test", &backups.CreateBackupRequest{
		IncludeDeleted: true,
		RetentionDays:  30,
	})
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if backup.ID != "backup_1" {
		t.Errorf("expected ID backup_1, got %s", backup.ID)
	}
	if backup.Status != backups.BackupStatusCreating {
		t.Errorf("expected status creating, got %s", backup.Status)
	}
}

func TestBackupsList(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/v1/organizations/org_test/backups", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "X-Org-ID", "org_test")

		if pid := r.URL.Query().Get("project_id"); pid != "proj_test" {
			t.Errorf("expected project_id=proj_test, got %q", pid)
		}
		if limit := r.URL.Query().Get("limit"); limit != "50" {
			t.Errorf("expected limit=50, got %q", limit)
		}
		if cursor := r.URL.Query().Get("cursor"); cursor != "abc123" {
			t.Errorf("expected cursor=abc123, got %q", cursor)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"backups": []map[string]interface{}{backupFixture()},
			"total":   1,
			"nextCursor": map[string]interface{}{
				"createdAt": "2026-01-01T00:00:00Z",
				"id":        "backup_1",
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
		ProjectID: "proj_test",
	})

	result, err := client.Backups.ListBackups(context.Background(), "org_test", &backups.ListBackupsOptions{
		ProjectID: "proj_test",
		Limit:     50,
		Cursor:    "abc123",
	})
	if err != nil {
		t.Fatalf("ListBackups() error = %v", err)
	}
	if len(result.Backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(result.Backups))
	}
	if result.Backups[0].ID != "backup_1" {
		t.Errorf("expected backup_1, got %s", result.Backups[0].ID)
	}
	if result.NextCursor == nil || result.NextCursor.ID != "backup_1" {
		t.Errorf("expected nextCursor.id=backup_1, got %v", result.NextCursor)
	}
	if result.Total != 1 {
		t.Errorf("expected total=1, got %d", result.Total)
	}
}

func TestBackupsGet(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/v1/organizations/org_test/backups/backup_1", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "X-Org-ID", "org_test")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, backupFixture())
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
	})

	backup, err := client.Backups.GetBackup(context.Background(), "org_test", "backup_1")
	if err != nil {
		t.Fatalf("GetBackup() error = %v", err)
	}
	if backup.ID != "backup_1" {
		t.Errorf("expected backup_1, got %s", backup.ID)
	}
}

func TestBackupsDownload(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/v1/organizations/org_test/backups/backup_1/download", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Org-ID", "org_test")
		http.Redirect(w, r, mock.URL+"/download", http.StatusFound)
	})

	mock.On("GET", "/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="backup-MyProject-2026-01-01.zip"`)
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ZIPDATA")
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
	})

	body, filename, err := client.Backups.DownloadBackup(context.Background(), "org_test", "backup_1")
	if err != nil {
		t.Fatalf("DownloadBackup() error = %v", err)
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(data) != "ZIPDATA" {
		t.Errorf("expected body ZIPDATA, got %q", string(data))
	}
	if filename != "backup-MyProject-2026-01-01.zip" {
		t.Errorf("expected filename backup-MyProject-2026-01-01.zip, got %q", filename)
	}
}

func TestBackupsDownloadFallbackFilename(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/v1/organizations/org_test/backups/backup_9/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mock.URL+"/download", http.StatusFound)
	})

	mock.On("GET", "/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "DATA")
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
	})

	body, filename, err := client.Backups.DownloadBackup(context.Background(), "org_test", "backup_9")
	if err != nil {
		t.Fatalf("DownloadBackup() error = %v", err)
	}
	body.Close()

	if filename != "backup-backup_9.zip" {
		t.Errorf("expected fallback filename backup-backup_9.zip, got %q", filename)
	}
}

func TestBackupsDelete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/v1/organizations/org_test/backups/backup_1", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Org-ID", "org_test")
		w.WriteHeader(http.StatusNoContent)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
	})

	if err := client.Backups.DeleteBackup(context.Background(), "org_test", "backup_1"); err != nil {
		t.Fatalf("DeleteBackup() error = %v", err)
	}
}

func TestBackupsImport(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/v1/organizations/org_test/backups/import", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Org-ID", "org_test")

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile(file): %v", err)
		}
		defer file.Close()
		if header.Filename != "archive.zip" {
			t.Errorf("expected file field filename archive.zip, got %q", header.Filename)
		}
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if string(data) != "ZIPCONTENT" {
			t.Errorf("expected file content ZIPCONTENT, got %q", string(data))
		}
		if rd := r.FormValue("retentionDays"); rd != "30" {
			t.Errorf("expected retentionDays=30, got %q", rd)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fixture := backupFixture()
		fixture["status"] = "ready"
		fixture["imported"] = true
		testutil.JSONResponse(t, w, fixture)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
	})

	backup, err := client.Backups.ImportBackup(context.Background(), "org_test", &backups.ImportBackupInput{
		Filename:      "archive.zip",
		Reader:        strings.NewReader("ZIPCONTENT"),
		RetentionDays: 30,
	})
	if err != nil {
		t.Fatalf("ImportBackup() error = %v", err)
	}
	if backup.Status != backups.BackupStatusReady {
		t.Errorf("expected status ready, got %s", backup.Status)
	}
	if !backup.Imported {
		t.Errorf("expected imported=true")
	}
}

func TestBackupsCreateCloneRestore(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/v1/organizations/org_test/restore", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-Org-ID", "org_test")

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["backupId"] != "backup_1" {
			t.Errorf("expected backupId=backup_1, got %v", body["backupId"])
		}
		if body["targetProjectName"] != "Clone" {
			t.Errorf("expected targetProjectName=Clone, got %v", body["targetProjectName"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":             "restore_1",
			"organizationId": "org_test",
			"backupId":       "backup_1",
			"mode":           "clone",
			"status":         "pending",
			"progress":       0,
			"createdAt":      "2026-01-01T00:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
	})

	restore, err := client.Backups.CreateCloneRestore(context.Background(), "org_test", &backups.RestoreRequest{
		BackupID:          "backup_1",
		TargetProjectName: "Clone",
	})
	if err != nil {
		t.Fatalf("CreateCloneRestore() error = %v", err)
	}
	if restore.ID != "restore_1" {
		t.Errorf("expected restore_1, got %s", restore.ID)
	}
	if restore.Status != backups.RestoreStatusPending {
		t.Errorf("expected status pending, got %s", restore.Status)
	}
}

func TestBackupsGetRestore(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/v1/restores/restore_1", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":              "restore_1",
			"organizationId":  "org_test",
			"backupId":        "backup_1",
			"mode":            "clone",
			"status":          "completed",
			"progress":        100,
			"targetProjectId": "new_proj",
			"createdAt":       "2026-01-01T00:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	restore, err := client.Backups.GetRestore(context.Background(), "restore_1")
	if err != nil {
		t.Fatalf("GetRestore() error = %v", err)
	}
	if restore.ID != "restore_1" {
		t.Errorf("expected restore_1, got %s", restore.ID)
	}
	if restore.TargetProjectID == nil || *restore.TargetProjectID != "new_proj" {
		t.Errorf("expected targetProjectId new_proj, got %v", restore.TargetProjectID)
	}
}

func TestBackupsErrorParsing(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/v1/organizations/org_test/backups/backup_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"not_ready","message":"backup is not ready"}}`)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
		OrgID:     "org_test",
	})

	_, err := client.Backups.GetBackup(context.Background(), "org_test", "backup_1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var sdkErr *sdkerrors.Error
	if !errors.As(err, &sdkErr) {
		t.Fatalf("expected *sdkerrors.Error, got %T", err)
	}
	if sdkErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", sdkErr.StatusCode)
	}
	if sdkErr.Code != "not_ready" {
		t.Errorf("expected code not_ready, got %q", sdkErr.Code)
	}
}
