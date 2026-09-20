// Package api_test — chunks_test.go
//
// Tests for the chunks API (/api/chunks).
// Ported from emergent.memory/apps/server/tests/e2e/chunks_test.go
//
// Note: chunks are created by async workers after document creation. Tests that
// require actual chunks will skip if workers are not running in the test environment.
package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// List Chunks
// ─────────────────────────────────────────────────────────────────────────────

func TestChunks_ListRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chunks", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestChunks_ListRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChunks_ListRequiresChunksReadScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chunks", "no-scope", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestChunks_ListEmpty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		Data       []any `json:"data"`
		TotalCount int   `json:"totalCount"`
	}
	parseBodyJSON(t, body, &result)
	if len(result.Data) != 0 {
		t.Errorf("expected 0 chunks, got %d", len(result.Data))
	}
	if result.TotalCount != 0 {
		t.Errorf("expected totalCount=0, got %d", result.TotalCount)
	}
}

func TestChunks_ListReturnsChunks(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	docID := createDocument(t, projectID,
		fmt.Sprintf("chunk-list-test-%d.txt", time.Now().UnixNano()),
		"This is test content that will be chunked by the document service.")

	chunkIDs := waitForChunks(t, projectID, docID, 10*time.Second)
	if len(chunkIDs) == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}

	resp := doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		Data []struct {
			ID         string `json:"id"`
			DocumentID string `json:"documentId"`
			Text       string `json:"text"`
			Size       int    `json:"size"`
		} `json:"data"`
		TotalCount int `json:"totalCount"`
	}
	parseBodyJSON(t, body, &result)

	if len(result.Data) < 1 {
		t.Fatal("expected at least 1 chunk")
	}

	found := false
	for _, c := range result.Data {
		if c.DocumentID == docID {
			found = true
			if c.Text == "" {
				t.Error("chunk text should not be empty")
			}
			if c.Size < 1 {
				t.Error("chunk size should be >= 1")
			}
			break
		}
	}
	if !found {
		t.Errorf("no chunk found for document %s", docID)
	}
}

func TestChunks_ListFilterByDocumentID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	doc1ID := createDocument(t, projectID, fmt.Sprintf("doc1-%d.txt", time.Now().UnixNano()), "Content for document one with unique text.")
	doc2ID := createDocument(t, projectID, fmt.Sprintf("doc2-%d.txt", time.Now().UnixNano()), "Content for document two with different text.")

	chunkIDs1 := waitForChunks(t, projectID, doc1ID, 10*time.Second)
	if len(chunkIDs1) == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}
	_ = waitForChunks(t, projectID, doc2ID, 5*time.Second)

	// Filter by doc1
	resp := doAPILogged(t, rl, "GET", "/api/chunks?documentId="+doc1ID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		Data []struct {
			ID         string `json:"id"`
			DocumentID string `json:"documentId"`
		} `json:"data"`
	}
	parseBodyJSON(t, body, &result)
	if len(result.Data) < 1 {
		t.Fatal("expected at least 1 chunk for doc1")
	}
	for _, c := range result.Data {
		if c.DocumentID != doc1ID {
			t.Errorf("chunk %s belongs to document %s, expected %s", c.ID, c.DocumentID, doc1ID)
		}
	}

	// Filter by doc2
	resp = doAPILogged(t, rl, "GET", "/api/chunks?documentId="+doc2ID, e2eTestToken(), projectID, nil)
	body = mustStatus(t, resp, http.StatusOK)
	parseBodyJSON(t, body, &result)
	for _, c := range result.Data {
		if c.DocumentID != doc2ID {
			t.Errorf("chunk %s belongs to document %s, expected %s", c.ID, c.DocumentID, doc2ID)
		}
	}
}

func TestChunks_ListInvalidDocumentID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chunks?documentId=invalid-uuid", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChunks_ListProjectIsolation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	docID := createDocument(t, projectID, fmt.Sprintf("isolation-doc-%d.txt", time.Now().UnixNano()), "User's content for isolation test.")

	chunkIDs := waitForChunks(t, projectID, docID, 10*time.Second)
	if len(chunkIDs) == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}

	// Create another project with its own document
	otherProjectID := createProject(t, orgID, uniqueName("other-chunks-proj"))
	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), otherProjectID,
		jsonBody(map[string]any{"filename": "other-doc.txt", "content": "Content in other project."}))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create doc in other project: got %d", resp.StatusCode)
	}
	var otherDoc map[string]any
	parseBodyJSON(t, readRespBody(resp), &otherDoc)
	t.Cleanup(func() { deleteDocument(t, otherProjectID, otherDoc["id"].(string)) })

	// List chunks for user's project — should only see chunks from docID
	resp = doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		Data []struct {
			ID         string `json:"id"`
			DocumentID string `json:"documentId"`
		} `json:"data"`
	}
	parseBodyJSON(t, body, &result)
	for _, c := range result.Data {
		if c.DocumentID != docID {
			t.Errorf("chunk %s belongs to unexpected document %s (expected %s)", c.ID, c.DocumentID, docID)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete Chunk
// ─────────────────────────────────────────────────────────────────────────────

func TestChunks_DeleteRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/"+uuid.NewString(), "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestChunks_DeleteRequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/"+uuid.NewString(), e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChunks_DeleteRequiresWriteScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/"+uuid.NewString(), "read-only", projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
}

func TestChunks_DeleteSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	docID := createDocument(t, projectID, fmt.Sprintf("delete-chunk-test-%d.txt", time.Now().UnixNano()), "Content to be deleted via chunk deletion.")

	chunkIDs := waitForChunks(t, projectID, docID, 10*time.Second)
	if len(chunkIDs) == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}
	chunkID := chunkIDs[0]

	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/"+chunkID, e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNoContent)

	// Verify chunk is gone
	listResp := doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), projectID, nil)
	body := mustStatus(t, listResp, http.StatusOK)

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	parseBodyJSON(t, body, &result)
	for _, c := range result.Data {
		if c.ID == chunkID {
			t.Errorf("deleted chunk %s still appears in list", chunkID)
		}
	}
}

func TestChunks_DeleteNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/"+uuid.NewString(), e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestChunks_DeleteInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/invalid-uuid", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChunks_DeleteProjectIsolation(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	otherProjectID := createProject(t, orgID, uniqueName("other-chunk-isolation"))

	// Create doc in other project
	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), otherProjectID,
		jsonBody(map[string]any{"filename": "other-project-doc.txt", "content": "Content in another project for isolation test."}))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create doc in other project: got %d", resp.StatusCode)
	}
	var otherDoc map[string]any
	parseBodyJSON(t, readRespBody(resp), &otherDoc)
	otherDocID := otherDoc["id"].(string)
	t.Cleanup(func() { deleteDocument(t, otherProjectID, otherDocID) })

	otherChunkIDs := waitForChunks(t, otherProjectID, otherDocID, 10*time.Second)
	if len(otherChunkIDs) == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}
	otherChunkID := otherChunkIDs[0]

	// Try to delete chunk using user's project — should 404
	resp = doAPILogged(t, rl, "DELETE", "/api/chunks/"+otherChunkID, e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusNotFound)

	// Chunk should still exist in other project
	listResp := doAPILogged(t, rl, "GET", "/api/chunks?documentId="+otherDocID, e2eTestToken(), otherProjectID, nil)
	body := mustStatus(t, listResp, http.StatusOK)

	var result struct {
		Data []any `json:"data"`
	}
	parseBodyJSON(t, body, &result)
	if len(result.Data) == 0 {
		t.Error("chunk should still exist in other project")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Bulk Delete Chunks
// ─────────────────────────────────────────────────────────────────────────────

func TestChunks_BulkDeleteSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	doc1ID := createDocument(t, projectID, fmt.Sprintf("bulk-del-1-%d.txt", time.Now().UnixNano()), "Content for bulk deletion test.")
	chunkIDs := waitForChunks(t, projectID, doc1ID, 10*time.Second)

	if len(chunkIDs) < 2 {
		// Try a second document
		doc2ID := createDocument(t, projectID, fmt.Sprintf("bulk-del-2-%d.txt", time.Now().UnixNano()), "Additional content for more chunks in bulk delete test.")
		more := waitForChunks(t, projectID, doc2ID, 5*time.Second)
		chunkIDs = append(chunkIDs, more...)
	}

	if len(chunkIDs) == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}

	idsToDelete := chunkIDs
	if len(idsToDelete) > 2 {
		idsToDelete = idsToDelete[:2]
	}

	resp := doAPILogged(t, rl, "DELETE", "/api/chunks", e2eTestToken(), projectID,
		jsonBody(map[string]any{"ids": idsToDelete}))
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		TotalRequested int `json:"totalRequested"`
		TotalDeleted   int `json:"totalDeleted"`
		TotalFailed    int `json:"totalFailed"`
	}
	parseBodyJSON(t, body, &result)
	if result.TotalRequested != len(idsToDelete) {
		t.Errorf("expected totalRequested=%d, got %d", len(idsToDelete), result.TotalRequested)
	}
	if result.TotalDeleted != len(idsToDelete) {
		t.Errorf("expected totalDeleted=%d, got %d", len(idsToDelete), result.TotalDeleted)
	}
	if result.TotalFailed != 0 {
		t.Errorf("expected totalFailed=0, got %d", result.TotalFailed)
	}
}

func TestChunks_BulkDeleteEmptyArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/chunks", e2eTestToken(), projectID,
		jsonBody(map[string]any{"ids": []string{}}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestChunks_BulkDeletePartialNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	docID := createDocument(t, projectID, fmt.Sprintf("partial-bulk-del-%d.txt", time.Now().UnixNano()), "Content for partial bulk delete test.")

	chunkIDs := waitForChunks(t, projectID, docID, 10*time.Second)
	if len(chunkIDs) == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}

	nonExistentID := uuid.NewString()
	resp := doAPILogged(t, rl, "DELETE", "/api/chunks", e2eTestToken(), projectID,
		jsonBody(map[string]any{"ids": []string{chunkIDs[0], nonExistentID}}))
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		TotalRequested int `json:"totalRequested"`
		TotalDeleted   int `json:"totalDeleted"`
		TotalFailed    int `json:"totalFailed"`
	}
	parseBodyJSON(t, body, &result)
	if result.TotalRequested != 2 {
		t.Errorf("expected totalRequested=2, got %d", result.TotalRequested)
	}
	if result.TotalDeleted != 1 {
		t.Errorf("expected totalDeleted=1, got %d", result.TotalDeleted)
	}
	if result.TotalFailed != 1 {
		t.Errorf("expected totalFailed=1, got %d", result.TotalFailed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete By Document
// ─────────────────────────────────────────────────────────────────────────────

func TestChunks_DeleteByDocumentSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	docID := createDocument(t, projectID, fmt.Sprintf("del-by-doc-%d.txt", time.Now().UnixNano()), "Content for delete by document test with some additional text.")

	chunkIDs := waitForChunks(t, projectID, docID, 10*time.Second)
	if len(chunkIDs) == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}

	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/by-document/"+docID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		DocumentID    string `json:"documentId"`
		ChunksDeleted int    `json:"chunksDeleted"`
		Success       bool   `json:"success"`
	}
	parseBodyJSON(t, body, &result)
	if result.DocumentID != docID {
		t.Errorf("expected documentId %s, got %s", docID, result.DocumentID)
	}
	if result.ChunksDeleted != len(chunkIDs) {
		t.Errorf("expected chunksDeleted=%d, got %d", len(chunkIDs), result.ChunksDeleted)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

func TestChunks_DeleteByDocumentNoChunks(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	docID := createDocument(t, projectID, fmt.Sprintf("no-chunks-%d.txt", time.Now().UnixNano()), "Content for no chunks test.")

	// Delete any chunks if they exist, then call delete-by-document
	chunkIDs := waitForChunks(t, projectID, docID, 3*time.Second)
	if len(chunkIDs) > 0 {
		doAPILogged(t, rl, "DELETE", "/api/chunks", e2eTestToken(), projectID, jsonBody(map[string]any{"ids": chunkIDs})).Body.Close()
	}

	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/by-document/"+docID, e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		ChunksDeleted int  `json:"chunksDeleted"`
		Success       bool `json:"success"`
	}
	parseBodyJSON(t, body, &result)
	if result.ChunksDeleted != 0 {
		t.Errorf("expected chunksDeleted=0, got %d", result.ChunksDeleted)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

func TestChunks_DeleteByDocumentInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/by-document/invalid-uuid", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// ─────────────────────────────────────────────────────────────────────────────
// Bulk Delete By Documents
// ─────────────────────────────────────────────────────────────────────────────

func TestChunks_BulkDeleteByDocumentsSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	doc1ID := createDocument(t, projectID, fmt.Sprintf("bulk-by-doc1-%d.txt", time.Now().UnixNano()), "Content for bulk document delete test one.")
	doc2ID := createDocument(t, projectID, fmt.Sprintf("bulk-by-doc2-%d.txt", time.Now().UnixNano()), "Content for bulk document delete test two with more text.")

	chunkIDs1 := waitForChunks(t, projectID, doc1ID, 10*time.Second)
	chunkIDs2 := waitForChunks(t, projectID, doc2ID, 5*time.Second)
	totalChunks := len(chunkIDs1) + len(chunkIDs2)

	if totalChunks == 0 {
		t.Skip("no chunks created within timeout — chunking workers may not be running")
	}

	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/by-documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"documentIds": []string{doc1ID, doc2ID}}))
	body := mustStatus(t, resp, http.StatusOK)

	var result struct {
		TotalDocuments int   `json:"totalDocuments"`
		TotalChunks    int   `json:"totalChunks"`
		Results        []any `json:"results"`
	}
	parseBodyJSON(t, body, &result)
	if result.TotalDocuments != 2 {
		t.Errorf("expected totalDocuments=2, got %d", result.TotalDocuments)
	}
	if result.TotalChunks != totalChunks {
		t.Errorf("expected totalChunks=%d, got %d", totalChunks, result.TotalChunks)
	}
	if len(result.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(result.Results))
	}
}

func TestChunks_BulkDeleteByDocumentsEmptyArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/chunks/by-documents", e2eTestToken(), projectID,
		jsonBody(map[string]any{"documentIds": []string{}}))
	mustStatus(t, resp, http.StatusBadRequest)
}
