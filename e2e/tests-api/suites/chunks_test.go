package suites

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/emergent/api-tests/client"
	"github.com/emergent/api-tests/testutil"
)

// ChunksTestSuite tests the chunks API endpoints.
// Note: Chunks are created via backend processing, not direct API calls.
// These tests use SQL to create test chunks for testing the chunk API.
type ChunksTestSuite struct {
	BaseSuite
	createdDocIDs   []string // Track created documents for cleanup
	createdChunkIDs []string // Track created chunks for cleanup
}

func TestChunksSuite(t *testing.T) {
	RunSuite(t, new(ChunksTestSuite))
}

// SetupTest runs before each test.
func (s *ChunksTestSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	s.createdDocIDs = nil
	s.createdChunkIDs = nil
}

// TearDownTest cleans up created test data after each test.
func (s *ChunksTestSuite) TearDownTest() {
	// Clean up chunks first (due to FK constraints)
	if len(s.createdChunkIDs) > 0 {
		_ = testutil.DeleteTestChunks(s.Ctx, s.DB, s.createdChunkIDs)
	}
	// Then clean up documents
	if len(s.createdDocIDs) > 0 {
		_ = testutil.DeleteTestDocuments(s.Ctx, s.DB, s.createdDocIDs)
	}
}

// createTestDocument creates a document via SQL and tracks it for cleanup.
func (s *ChunksTestSuite) createTestDocument(filename string) string {
	docID := testutil.NewUUID()
	doc := testutil.TestDocument{
		ID:        docID,
		ProjectID: s.Project,
		Filename:  filename,
	}
	err := testutil.CreateTestDocument(s.Ctx, s.DB, doc)
	s.Require().NoError(err, "Failed to create test document")
	s.createdDocIDs = append(s.createdDocIDs, docID)
	return docID
}

// createTestChunk creates a chunk via SQL and tracks it for cleanup.
func (s *ChunksTestSuite) createTestChunk(documentID string, index int, text string) string {
	chunkID := testutil.NewUUID()
	chunk := testutil.TestChunk{
		ID:         chunkID,
		DocumentID: documentID,
		ChunkIndex: index,
		Text:       text,
	}
	err := testutil.CreateTestChunk(s.Ctx, s.DB, chunk)
	s.Require().NoError(err, "Failed to create test chunk")
	s.createdChunkIDs = append(s.createdChunkIDs, chunkID)
	return chunkID
}

// =============================================================================
// Test: List Chunks
// =============================================================================

func (s *ChunksTestSuite) TestListChunks_Empty() {
	// Note: May not be truly empty if other tests left data, but structure should be correct
	resp, err := s.Client.GET("/api/v2/chunks",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "1"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	// Check response structure
	s.Contains(body, "data")
	s.Contains(body, "totalCount")

	data, ok := body["data"].([]any)
	s.True(ok, "data should be an array")
	_ = data // May or may not be empty
}

func (s *ChunksTestSuite) TestListChunks_ReturnsChunks() {
	// Create document and chunks
	docID := s.createTestDocument("list-chunks-test.txt")
	s.createTestChunk(docID, 0, "First chunk text")
	s.createTestChunk(docID, 1, "Second chunk text")

	// List chunks
	resp, err := s.Client.GET("/api/v2/chunks",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("documentId", docID),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	data, ok := body["data"].([]any)
	s.True(ok)
	s.Len(data, 2)

	// Check first chunk structure
	chunk := data[0].(map[string]any)
	s.Equal(docID, chunk["documentId"])
	s.Contains(chunk, "index")
	s.Contains(chunk, "text")
	s.Contains(chunk, "size")
	s.Contains(chunk, "hasEmbedding")
}

func (s *ChunksTestSuite) TestListChunks_FilterByDocumentID() {
	// Create two documents with chunks
	doc1ID := s.createTestDocument("chunks-filter-doc1.txt")
	doc2ID := s.createTestDocument("chunks-filter-doc2.txt")

	s.createTestChunk(doc1ID, 0, "Doc 1 Chunk 1")
	s.createTestChunk(doc1ID, 1, "Doc 1 Chunk 2")
	s.createTestChunk(doc2ID, 0, "Doc 2 Chunk 1")

	// Filter by doc1
	resp, err := s.Client.GET("/api/v2/chunks",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("documentId", doc1ID),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	data, ok := body["data"].([]any)
	s.True(ok)
	s.Len(data, 2)

	// All chunks should be from doc1
	for _, c := range data {
		chunk := c.(map[string]any)
		s.Equal(doc1ID, chunk["documentId"])
	}
}

func (s *ChunksTestSuite) TestListChunks_InvalidDocumentID() {
	resp, err := s.Client.GET("/api/v2/chunks",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("documentId", "invalid-uuid"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestListChunks_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/chunks",
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ChunksTestSuite) TestListChunks_RequiresProjectID() {
	resp, err := s.Client.GET("/api/v2/chunks",
		s.AdminAuth(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Delete Chunk
// =============================================================================

func (s *ChunksTestSuite) TestDeleteChunk_Success() {
	docID := s.createTestDocument("delete-chunk-test.txt")
	chunkID := s.createTestChunk(docID, 0, "To be deleted")

	// Remove from tracking since we're deleting it
	s.createdChunkIDs = s.createdChunkIDs[:len(s.createdChunkIDs)-1]

	resp, err := s.Client.DELETE("/api/v2/chunks/"+chunkID,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusNoContent, resp.StatusCode)

	// Verify chunk is deleted by trying to list it
	listResp, err := s.Client.GET("/api/v2/chunks",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("documentId", docID),
	)
	s.Require().NoError(err)

	body, _ := listResp.JSONMap()
	data := body["data"].([]any)
	s.Len(data, 0, "Chunk should be deleted")
}

func (s *ChunksTestSuite) TestDeleteChunk_NotFound() {
	resp, err := s.Client.DELETE("/api/v2/chunks/00000000-0000-0000-0000-000000000999",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteChunk_InvalidUUID() {
	resp, err := s.Client.DELETE("/api/v2/chunks/invalid-uuid",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteChunk_RequiresAuth() {
	resp, err := s.Client.DELETE("/api/v2/chunks/"+testutil.NewUUID(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteChunk_RequiresProjectID() {
	resp, err := s.Client.DELETE("/api/v2/chunks/"+testutil.NewUUID(),
		s.AdminAuth(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Bulk Delete Chunks
// =============================================================================

func (s *ChunksTestSuite) TestBulkDeleteChunks_Success() {
	docID := s.createTestDocument("bulk-delete-chunks.txt")
	chunk1ID := s.createTestChunk(docID, 0, "Chunk 1")
	chunk2ID := s.createTestChunk(docID, 1, "Chunk 2")

	// Remove from tracking since we're deleting them
	s.createdChunkIDs = nil

	body := map[string]any{
		"ids": []string{chunk1ID, chunk2ID},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/chunks", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal(float64(2), respBody["totalRequested"])
	s.Equal(float64(2), respBody["totalDeleted"])
	s.Equal(float64(0), respBody["totalFailed"])
}

func (s *ChunksTestSuite) TestBulkDeleteChunks_PartialNotFound() {
	docID := s.createTestDocument("bulk-partial-chunks.txt")
	chunkID := s.createTestChunk(docID, 0, "Exists")

	// Remove from tracking since we're deleting it
	s.createdChunkIDs = nil

	nonExistentID := testutil.NewUUID()

	body := map[string]any{
		"ids": []string{chunkID, nonExistentID},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/chunks", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal(float64(2), respBody["totalRequested"])
	s.Equal(float64(1), respBody["totalDeleted"])
	s.Equal(float64(1), respBody["totalFailed"])
}

func (s *ChunksTestSuite) TestBulkDeleteChunks_EmptyArray() {
	body := map[string]any{
		"ids": []string{},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/chunks", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestBulkDeleteChunks_RequiresAuth() {
	body := map[string]any{
		"ids": []string{testutil.NewUUID()},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/chunks", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// =============================================================================
// Test: Delete Chunks By Document
// =============================================================================

func (s *ChunksTestSuite) TestDeleteByDocument_Success() {
	docID := s.createTestDocument("delete-by-doc.txt")
	s.createTestChunk(docID, 0, "Chunk 1")
	s.createTestChunk(docID, 1, "Chunk 2")
	s.createTestChunk(docID, 2, "Chunk 3")

	// Clear tracking since we're deleting them
	s.createdChunkIDs = nil

	resp, err := s.Client.DELETE("/api/v2/chunks/by-document/"+docID,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal(docID, respBody["documentId"])
	s.Equal(float64(3), respBody["chunksDeleted"])
	s.Equal(true, respBody["success"])
}

func (s *ChunksTestSuite) TestDeleteByDocument_NoChunks() {
	docID := s.createTestDocument("no-chunks-doc.txt")
	// No chunks created

	resp, err := s.Client.DELETE("/api/v2/chunks/by-document/"+docID,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal(float64(0), respBody["chunksDeleted"])
	s.Equal(true, respBody["success"])
}

func (s *ChunksTestSuite) TestDeleteByDocument_InvalidUUID() {
	resp, err := s.Client.DELETE("/api/v2/chunks/by-document/invalid-uuid",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteByDocument_RequiresAuth() {
	resp, err := s.Client.DELETE("/api/v2/chunks/by-document/"+testutil.NewUUID(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// =============================================================================
// Test: Bulk Delete Chunks By Documents
// =============================================================================

func (s *ChunksTestSuite) TestBulkDeleteByDocuments_Success() {
	doc1ID := s.createTestDocument("bulk-by-docs-1.txt")
	doc2ID := s.createTestDocument("bulk-by-docs-2.txt")
	s.createTestChunk(doc1ID, 0, "Doc 1 Chunk")
	s.createTestChunk(doc2ID, 0, "Doc 2 Chunk 1")
	s.createTestChunk(doc2ID, 1, "Doc 2 Chunk 2")

	// Clear tracking since we're deleting them
	s.createdChunkIDs = nil

	body := map[string]any{
		"documentIds": []string{doc1ID, doc2ID},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/chunks/by-documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal(float64(2), respBody["totalDocuments"])
	s.Equal(float64(3), respBody["totalChunks"])

	results, ok := respBody["results"].([]any)
	s.True(ok)
	s.Len(results, 2)
}

func (s *ChunksTestSuite) TestBulkDeleteByDocuments_EmptyArray() {
	body := map[string]any{
		"documentIds": []string{},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/chunks/by-documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestBulkDeleteByDocuments_RequiresAuth() {
	body := map[string]any{
		"documentIds": []string{testutil.NewUUID()},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/chunks/by-documents", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// =============================================================================
// Test: Pagination (Note: Chunks API doesn't support limit parameter)
// =============================================================================

func (s *ChunksTestSuite) TestListChunks_ReturnsAllChunksForDocument() {
	docID := s.createTestDocument("all-chunks.txt")

	// Create 5 chunks
	for i := 0; i < 5; i++ {
		s.createTestChunk(docID, i, fmt.Sprintf("Chunk %d content", i))
	}

	// Request all chunks for document
	resp, err := s.Client.GET("/api/v2/chunks",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("documentId", docID),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	data, ok := body["data"].([]any)
	s.True(ok)
	s.Len(data, 5, "Should return all 5 chunks")

	// Total count should be 5
	s.Equal(float64(5), body["totalCount"])
}
