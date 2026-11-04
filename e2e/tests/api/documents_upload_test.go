// Package api_test — documents_upload_test.go
//
// Tests for the document upload API (/api/documents/upload, /api/documents/upload/batch).
// Ported from emergent.memory/apps/server/tests/e2e/documents_upload_test.go
package api_test

import (
	"net/http"
	"testing"
)

const dummyProjectID = "00000000-0000-0000-0000-000000000001"

// ─────────────────────────────────────────────────────────────────────────────
// Single File Upload — Authentication & Authorization
// ─────────────────────────────────────────────────────────────────────────────

func TestDocumentsUpload_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doMultipartWithFile(t, "/api/documents/upload", "", dummyProjectID, "file", "test.txt", []byte("test content"))
	mustStatus(t, resp, http.StatusUnauthorized)
}

// Contract: a real project with a token lacking documents:write ⇒ 403. A
// non-existent project would be pre-empted by RequireProjectMember (404), so
// this uses a real project to pin the scope-denial branch specifically.
func TestDocumentsUpload_RequiresDocumentsWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, readOnlyToken := createToken(t, projectID, uniqueName("upload-read-only"), []string{"data:read"})

	resp := doMultipartWithFile(t, "/api/documents/upload", readOnlyToken, projectID, "file", "test.txt", []byte("test content"))
	body := mustStatus(t, resp, http.StatusForbidden)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "forbidden" {
		t.Errorf("expected error code 'forbidden', got %v", errObj["code"])
	}
}

func TestDocumentsUpload_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doMultipartWithFile(t, "/api/documents/upload", e2eTestToken(), "", "file", "test.txt", []byte("test content"))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, errObj["message"].(string), "x-project-id")
}

// ─────────────────────────────────────────────────────────────────────────────
// Single File Upload — Validation
// ─────────────────────────────────────────────────────────────────────────────

func TestDocumentsUpload_RejectsWhenFileIsMissing(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Send a multipart form with only a text field, no file
	resp := doMultipartWithField(t, "/api/documents/upload", e2eTestToken(), projectID, "someField", "someValue")
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, errObj["message"].(string), "file")
}

// ─────────────────────────────────────────────────────────────────────────────
// Batch Upload — Authentication & Authorization
// ─────────────────────────────────────────────────────────────────────────────

func TestDocumentsBatchUpload_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doMultipartWithFiles(t, "/api/documents/upload/batch", "", dummyProjectID, "files", map[string][]byte{
		"test1.txt": []byte("test content 1"),
		"test2.txt": []byte("test content 2"),
	})
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestDocumentsBatchUpload_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doMultipartWithFiles(t, "/api/documents/upload/batch", e2eTestToken(), "", "files", map[string][]byte{
		"test1.txt": []byte("test content 1"),
	})
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, errObj["message"].(string), "x-project-id")
}

func TestDocumentsBatchUpload_RequiresDocumentsWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, readOnlyToken := createToken(t, projectID, uniqueName("batch-upload-read-only"), []string{"data:read"})

	resp := doMultipartWithFiles(t, "/api/documents/upload/batch", readOnlyToken, projectID, "files", map[string][]byte{
		"test1.txt": []byte("test content 1"),
	})
	body := mustStatus(t, resp, http.StatusForbidden)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "forbidden" {
		t.Errorf("expected error code 'forbidden', got %v", errObj["code"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Batch Upload — Validation
// ─────────────────────────────────────────────────────────────────────────────

func TestDocumentsBatchUpload_RejectsWhenNoFilesProvided(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Send a multipart form with only a text field, no files
	resp := doMultipartWithField(t, "/api/documents/upload/batch", e2eTestToken(), projectID, "someField", "someValue")
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, errObj["message"].(string), "at least one file")
}
