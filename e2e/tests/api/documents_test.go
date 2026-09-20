// Package api_test — documents_test.go
//
// Tests for the documents API (/api/documents).
// Ported from emergent.memory/apps/server/tests/e2e/documents_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Authentication & Authorization
// ─────────────────────────────────────────────────────────────────────────────

func TestDocuments_ListRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestDocuments_ListRequiresDocumentsReadScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents", "no-scope", projectID, nil)
	body := mustStatus(t, resp, http.StatusForbidden)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "forbidden" {
		t.Errorf("expected error code 'forbidden', got %v", errObj["code"])
	}
}

func TestDocuments_ListRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "project")
}

// ─────────────────────────────────────────────────────────────────────────────
// List Documents
// ─────────────────────────────────────────────────────────────────────────────

func TestDocuments_ListEmpty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	docs, _ := result["documents"].([]any)
	if len(docs) != 0 {
		t.Errorf("expected 0 documents, got %d", len(docs))
	}
	if result["total"] != float64(0) {
		t.Errorf("expected total=0, got %v", result["total"])
	}
}

func TestDocuments_ListReturnsDocuments(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	doc1ID := createDocument(t, projectID, "Test Document 1.txt", "Content for document 1")
	doc2ID := createDocument(t, projectID, "Test Document 2.pdf", "Content for document 2")

	resp := doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	docs, _ := result["documents"].([]any)
	if len(docs) < 2 {
		t.Fatalf("expected at least 2 documents, got %d", len(docs))
	}

	found1, found2 := false, false
	for _, d := range docs {
		dm := d.(map[string]any)
		if dm["id"] == doc1ID {
			found1 = true
		}
		if dm["id"] == doc2ID {
			found2 = true
		}
	}
	if !found1 {
		t.Error("document 1 not found in list")
	}
	if !found2 {
		t.Error("document 2 not found in list")
	}
}

func TestDocuments_ListProjectIsolation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	otherProjectID := createProject(t, orgID, uniqueName("other-proj"))

	doc1ID := createDocument(t, projectID, "Doc in Default Project.txt", "Content for default project")

	// Create document in other project
	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), otherProjectID,
		jsonBody(map[string]any{"filename": "Doc in Other Project.txt", "content": "Content for other project"}))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create doc in other project: got %d", resp.StatusCode)
	}
	var otherDoc map[string]any
	parseBodyJSON(t, readRespBody(resp), &otherDoc)
	t.Cleanup(func() { deleteDocument(t, otherProjectID, otherDoc["id"].(string)) })

	// List in default project — should only see our document
	resp = doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	docs, _ := result["documents"].([]any)

	found := false
	for _, d := range docs {
		if d.(map[string]any)["id"] == doc1ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("document should be found in default project list")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Filtering
// ─────────────────────────────────────────────────────────────────────────────

func TestDocuments_FilterBySourceType(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	createDocument(t, projectID, "Upload Doc 1.txt", "Content for upload doc 1 - filter test")
	createDocument(t, projectID, "Upload Doc 2.txt", "Content for upload doc 2 - filter test")

	resp := doAPILogged(t, rl, "GET", "/api/documents?sourceType=upload", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	docs, _ := result["documents"].([]any)
	if len(docs) < 2 {
		t.Errorf("expected at least 2 upload documents, got %d", len(docs))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Pagination
// ─────────────────────────────────────────────────────────────────────────────

func TestDocuments_ListLimit(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	for i := 1; i <= 5; i++ {
		createDocument(t, projectID,
			fmt.Sprintf("Limit Test Document %d.txt", i),
			fmt.Sprintf("Unique content for limit test doc %d - %d", i, time.Now().UnixNano()),
		)
	}

	resp := doAPILogged(t, rl, "GET", "/api/documents?limit=2", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	docs, _ := result["documents"].([]any)
	if len(docs) != 2 {
		t.Errorf("expected 2 documents with limit=2, got %d", len(docs))
	}
	if result["total"].(float64) < 5 {
		t.Errorf("expected total >= 5, got %v", result["total"])
	}

	nextCursor := resp.Header.Get("x-next-cursor")
	if nextCursor == "" {
		t.Error("expected x-next-cursor header for pagination")
	}
}

func TestDocuments_CursorPagination(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	for i := 1; i <= 5; i++ {
		createDocument(t, projectID,
			fmt.Sprintf("Cursor Pagination Doc %d.txt", i),
			fmt.Sprintf("Unique content for cursor pagination doc %d - %d", i, time.Now().UnixNano()),
		)
		time.Sleep(10 * time.Millisecond)
	}

	// First page
	resp := doAPILogged(t, rl, "GET", "/api/documents?limit=2", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	docs, _ := result["documents"].([]any)
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs on first page, got %d", len(docs))
	}

	firstPageIDs := make(map[string]bool)
	for _, d := range docs {
		firstPageIDs[d.(map[string]any)["id"].(string)] = true
	}

	nextCursor := resp.Header.Get("x-next-cursor")
	if nextCursor == "" {
		t.Fatal("expected x-next-cursor header")
	}

	// Second page
	resp = doAPILogged(t, rl, "GET", "/api/documents?limit=2&cursor="+nextCursor, e2eTestToken(), projectID, nil)
	body = mustStatus(t, resp, http.StatusOK)

	parseBodyJSON(t, body, &result)
	docs, _ = result["documents"].([]any)
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs on second page, got %d", len(docs))
	}

	for _, d := range docs {
		id := d.(map[string]any)["id"].(string)
		if firstPageIDs[id] {
			t.Errorf("document %s appeared on both pages", id)
		}
	}
}

func TestDocuments_CursorPaginationStress(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	const totalDocs = 55
	const pageLimit = 5

	projectID, _ := setupProjectLogged(t, rl)
	createdIDs := make(map[string]bool)
	for i := 0; i < totalDocs; i++ {
		id := createDocument(t, projectID,
			fmt.Sprintf("stress-%d.txt", i),
			fmt.Sprintf("Unique stress test content %d - timestamp %d", i, time.Now().UnixNano()),
		)
		createdIDs[id] = true
		time.Sleep(5 * time.Millisecond)
	}

	seen := make(map[string]bool)
	var cursor string
	pages := 0
	totalFetched := 0
	maxPages := (totalDocs / pageLimit) + 10

	for {
		path := fmt.Sprintf("/api/documents?limit=%d", pageLimit)
		if cursor != "" {
			path += "&cursor=" + cursor
		}

		resp := doAPILogged(t, rl, "GET", path, e2eTestToken(), projectID, nil)
		body := mustStatus(t, resp, http.StatusOK)

		var result map[string]any
		parseBodyJSON(t, body, &result)
		docs, _ := result["documents"].([]any)

		nextCursor := resp.Header.Get("x-next-cursor")
		if nextCursor != "" {
			if len(docs) != pageLimit {
				t.Errorf("page %d: expected %d docs, got %d", pages, pageLimit, len(docs))
			}
		} else {
			if len(docs) == 0 {
				t.Error("final page is empty")
			}
		}

		for _, d := range docs {
			id := d.(map[string]any)["id"].(string)
			if seen[id] {
				t.Errorf("document %s appeared twice (page %d)", id, pages)
			}
			seen[id] = true
		}

		totalFetched += len(docs)
		pages++
		cursor = nextCursor

		if pages > maxPages {
			t.Fatalf("pagination exceeded %d pages", maxPages)
		}
		if cursor == "" {
			break
		}
	}

	for id := range createdIDs {
		if !seen[id] {
			t.Errorf("created document %s not found in pagination", id)
		}
	}
	if totalFetched < totalDocs {
		t.Errorf("expected to fetch at least %d docs, got %d", totalDocs, totalFetched)
	}
}

func TestDocuments_InvalidLimit(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents?limit=1000", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "limit")
}

func TestDocuments_InvalidCursor(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents?cursor=not-valid-base64!!!", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "cursor")
}

// ─────────────────────────────────────────────────────────────────────────────
// Get Document by ID
// ─────────────────────────────────────────────────────────────────────────────

func TestDocuments_GetSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	docID := createDocument(t, projectID, "Test Document for Get.txt", "Hello, World! Get test content.")

	resp := doAPILogged(t, rl, "GET", "/api/documents/"+docID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["id"] != docID {
		t.Errorf("expected id %s, got %v", docID, result["id"])
	}
	if result["filename"] != "Test Document for Get.txt" {
		t.Errorf("unexpected filename: %v", result["filename"])
	}
}

func TestDocuments_GetNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents/00000000-0000-0000-0000-000000000999", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusNotFound)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "not_found" {
		t.Errorf("expected error code 'not_found', got %v", errObj["code"])
	}
}

func TestDocuments_GetNotFoundInOtherProject(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	otherProjectID := createProject(t, orgID, uniqueName("other-proj"))

	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), otherProjectID,
		jsonBody(map[string]any{"filename": "Doc in Other Project.txt", "content": "Content for other project - get test"}))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create doc in other project: got %d", resp.StatusCode)
	}
	var created map[string]any
	parseBodyJSON(t, readRespBody(resp), &created)
	docID := created["id"].(string)
	t.Cleanup(func() { deleteDocument(t, otherProjectID, docID) })

	// Try to get using different project — should 404
	resp = doAPILogged(t, rl, "GET", "/api/documents/"+docID, e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestDocuments_GetRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents/00000000-0000-0000-0000-000000000001", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestDocuments_GetRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/documents/00000000-0000-0000-0000-000000000001", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestDocuments_GetRequiresDocumentsReadScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents/00000000-0000-0000-0000-000000000001", "no-scope", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
}

// ─────────────────────────────────────────────────────────────────────────────
// Create Document
// ─────────────────────────────────────────────────────────────────────────────

func TestDocuments_CreateSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"filename": "test-document.txt", "content": "Hello, World!"}))
	body := mustStatus(t, resp, http.StatusCreated)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	if result["id"] == "" || result["id"] == nil {
		t.Error("expected non-empty id")
	}
	if result["filename"] != "test-document.txt" {
		t.Errorf("unexpected filename: %v", result["filename"])
	}
	if result["content"] != "Hello, World!" {
		t.Errorf("unexpected content: %v", result["content"])
	}
	if result["projectId"] != projectID {
		t.Errorf("expected projectId %s, got %v", projectID, result["projectId"])
	}
	t.Cleanup(func() { deleteDocument(t, projectID, result["id"].(string)) })
}

func TestDocuments_CreateDefaultFilename(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"content": "Some content"}))
	body := mustStatus(t, resp, http.StatusCreated)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["filename"] != "unnamed.txt" {
		t.Errorf("expected filename 'unnamed.txt', got %v", result["filename"])
	}
	t.Cleanup(func() { deleteDocument(t, projectID, result["id"].(string)) })
}

func TestDocuments_CreateEmptyContent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"filename": "empty-file.txt", "content": ""}))
	body := mustStatus(t, resp, http.StatusCreated)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["content"] != "" {
		t.Errorf("expected empty content, got %v", result["content"])
	}
	t.Cleanup(func() { deleteDocument(t, projectID, result["id"].(string)) })
}

func TestDocuments_CreateDeduplication(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp1 := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"filename": "original.txt", "content": "Same content for deduplication test"}))
	body1 := mustStatus(t, resp1, http.StatusCreated)
	var first map[string]any
	parseBodyJSON(t, body1, &first)
	firstID := first["id"].(string)
	t.Cleanup(func() { deleteDocument(t, projectID, firstID) })

	resp2 := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"filename": "duplicate.txt", "content": "Same content for deduplication test"}))
	body2 := mustStatus(t, resp2, http.StatusOK) // 200 = deduplicated

	var second map[string]any
	parseBodyJSON(t, body2, &second)
	if second["id"] != firstID {
		t.Errorf("expected deduplication: got id %v, want %s", second["id"], firstID)
	}
	if second["filename"] != "original.txt" {
		t.Errorf("expected original filename preserved, got %v", second["filename"])
	}
}

func TestDocuments_CreateFilenameTooLong(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	longFilename := ""
	for i := 0; i < 600; i++ {
		longFilename += "a"
	}

	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"filename": longFilename, "content": "Some content"}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "filename")
}

func TestDocuments_CreateRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/documents", "", projectID,
		jsonBody(map[string]any{"filename": "test.txt", "content": "Content"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestDocuments_CreateRequiresWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/documents", "read-only", projectID,
		jsonBody(map[string]any{"filename": "test.txt", "content": "Content"}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestDocuments_CreateRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), "",
		jsonBody(map[string]any{"filename": "test.txt", "content": "Content"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete Document
// ─────────────────────────────────────────────────────────────────────────────

func TestDocuments_DeleteSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	// Do NOT register cleanup — test deletes it explicitly.
	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"filename": "To Be Deleted.txt",
			"content":  fmt.Sprintf("Content to be deleted - unique %d", time.Now().UnixNano()),
		}))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create doc: got %d", resp.StatusCode)
	}
	var created map[string]any
	parseBodyJSON(t, readRespBody(resp), &created)
	docID := created["id"].(string)

	delResp := doAPILogged(t, rl, "DELETE", "/api/documents/"+docID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, delResp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["status"] != "deleted" {
		t.Errorf("expected status 'deleted', got %v", result["status"])
	}

	// Verify actually deleted
	getResp := doAPILogged(t, rl, "GET", "/api/documents/"+docID, e2eTestToken(), projectID, nil)
	mustStatus(t, getResp, http.StatusNotFound)
}

func TestDocuments_DeleteNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/documents/00000000-0000-0000-0000-000000000999", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestDocuments_DeleteInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/documents/not-a-uuid", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "Invalid")
}

func TestDocuments_DeleteRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/documents/00000000-0000-0000-0000-000000000001", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestDocuments_DeleteRequiresDeleteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/documents/00000000-0000-0000-0000-000000000001", "read-only", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestDocuments_DeleteRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/documents/00000000-0000-0000-0000-000000000001", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestDocuments_DeleteProjectIsolation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	otherProjectID := createProject(t, orgID, uniqueName("other-proj"))

	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), otherProjectID,
		jsonBody(map[string]any{"filename": "Doc in Other Project.txt", "content": "Content for other project - delete test"}))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create doc in other project: got %d", resp.StatusCode)
	}
	var created map[string]any
	parseBodyJSON(t, readRespBody(resp), &created)
	docID := created["id"].(string)
	t.Cleanup(func() { deleteDocument(t, otherProjectID, docID) })

	// Try to delete using different project — should 404
	resp = doAPILogged(t, rl, "DELETE", "/api/documents/"+docID, e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Bulk Delete Documents
// ─────────────────────────────────────────────────────────────────────────────

func TestDocuments_BulkDeleteSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	ts := time.Now().UnixNano()
	// Create without auto-cleanup since bulk delete removes them
	createRaw := func(filename, content string) string {
		resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
			jsonBody(map[string]any{"filename": filename, "content": content}))
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			t.Fatalf("create doc: got %d", resp.StatusCode)
		}
		var r map[string]any
		parseBodyJSON(t, readRespBody(resp), &r)
		return r["id"].(string)
	}

	doc1ID := createRaw("Bulk Delete Doc 1.txt", fmt.Sprintf("Unique content 1 - %d", ts))
	doc2ID := createRaw("Bulk Delete Doc 2.txt", fmt.Sprintf("Unique content 2 - %d", ts))
	doc3ID := createDocument(t, projectID, "Bulk Delete Doc 3.txt", fmt.Sprintf("Unique content 3 - %d", ts))

	resp := doAPILogged(t, rl, "DELETE", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"ids": []string{doc1ID, doc2ID}}))
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["status"] != "deleted" {
		t.Errorf("expected status 'deleted', got %v", result["status"])
	}
	if result["deleted"] != float64(2) {
		t.Errorf("expected deleted=2, got %v", result["deleted"])
	}

	// doc1 and doc2 should be gone
	mustStatus(t, doAPILogged(t, rl, "GET", "/api/documents/"+doc1ID, e2eTestToken(), projectID, nil), http.StatusNotFound)
	// doc3 should still exist (cleaned up by t.Cleanup from createDocument)
	mustStatus(t, doAPILogged(t, rl, "GET", "/api/documents/"+doc3ID, e2eTestToken(), projectID, nil), http.StatusOK)
}

func TestDocuments_BulkDeletePartialNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	// Create without auto-cleanup since test deletes it
	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{
			"filename": "Partial Delete Doc.txt",
			"content":  fmt.Sprintf("Unique content for partial delete - %d", time.Now().UnixNano()),
		}))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create doc: got %d", resp.StatusCode)
	}
	var created map[string]any
	parseBodyJSON(t, readRespBody(resp), &created)
	doc1ID := created["id"].(string)

	delResp := doAPILogged(t, rl, "DELETE", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"ids": []string{doc1ID, "00000000-0000-0000-0000-000000000999"}}))
	body := mustStatus(t, delResp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["status"] != "partial" {
		t.Errorf("expected status 'partial', got %v", result["status"])
	}
	if result["deleted"] != float64(1) {
		t.Errorf("expected deleted=1, got %v", result["deleted"])
	}
	notFound, _ := result["notFound"].([]any)
	if len(notFound) != 1 {
		t.Errorf("expected 1 notFound, got %d", len(notFound))
	}
}

func TestDocuments_BulkDeleteEmptyArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"ids": []string{}}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	assertContains(t, fmt.Sprint(errObj["message"]), "ids")
}

func TestDocuments_BulkDeleteInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"ids": []string{"not-a-uuid", "also-not-valid"}}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestDocuments_BulkDeleteRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/documents", "", projectID,
		jsonBody(map[string]any{"ids": []string{"00000000-0000-0000-0000-000000000001"}}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestDocuments_BulkDeleteRequiresDeleteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/documents", "read-only", projectID,
		jsonBody(map[string]any{"ids": []string{"00000000-0000-0000-0000-000000000001"}}))
	mustStatus(t, resp, http.StatusForbidden)
}
