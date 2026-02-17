package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/domain/chunks"
	"github.com/emergent-company/emergent/internal/testutil"
)

// ChunksTestSuite tests the chunks API endpoints
type ChunksTestSuite struct {
	testutil.BaseSuite
}

func TestChunksSuite(t *testing.T) {
	suite.Run(t, new(ChunksTestSuite))
}

func (s *ChunksTestSuite) SetupSuite() {
	s.SetDBSuffix("chunks")
	s.BaseSuite.SetupSuite()
}

// createDocumentViaAPI creates a document via API and returns (documentID, []chunkIDs)
// When running in-process, chunks are created via direct DB access since the API
// doesn't automatically chunk documents (that's done by async workers)
func (s *ChunksTestSuite) createDocumentViaAPI(content string) (string, []string) {
	resp := s.Client.POST("/api/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"filename": fmt.Sprintf("test-doc-%s.txt", uuid.NewString()[:8]),
			"content":  content,
		}),
	)
	// Accept both 200 (deduplicated) and 201 (created)
	s.Require().True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated,
		"Expected 200 or 201, got %d: %s", resp.StatusCode, resp.String())

	var doc map[string]any
	err := json.Unmarshal(resp.Body, &doc)
	s.Require().NoError(err)

	docID := doc["id"].(string)

	// When running in-process, create chunks directly via DB
	// (chunks are normally created by async workers, not the API)
	var chunkIDs []string
	if !s.IsExternal() {
		chunkID := uuid.NewString()
		err = testutil.CreateTestChunk(context.Background(), s.DB(), testutil.TestChunk{
			ID:         chunkID,
			DocumentID: docID,
			ChunkIndex: 0,
			Text:       content,
		})
		s.Require().NoError(err, "Failed to create test chunk")
		chunkIDs = append(chunkIDs, chunkID)
	} else {
		// In external mode, try to get chunks via API (they may have been created by workers)
		chunkResp := s.Client.GET(fmt.Sprintf("/api/chunks?documentId=%s", docID),
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
		)
		s.Require().Equal(http.StatusOK, chunkResp.StatusCode)

		var chunkResult chunks.ListChunksResponse
		err = json.Unmarshal(chunkResp.Body, &chunkResult)
		s.Require().NoError(err)

		for _, c := range chunkResult.Data {
			chunkIDs = append(chunkIDs, c.ID)
		}
	}

	return docID, chunkIDs
}

// createProjectViaAPI creates a project via API and returns its ID
func (s *ChunksTestSuite) createProjectViaAPI(name string) string {
	resp := s.Client.POST("/api/projects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSON(),
		testutil.WithBody(fmt.Sprintf(`{"name": "%s", "orgId": "%s"}`, name, s.OrgID)),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode)

	var project map[string]any
	err := json.Unmarshal(resp.Body, &project)
	s.Require().NoError(err)
	return project["id"].(string)
}

// ============= List Tests =============

func (s *ChunksTestSuite) TestListChunks_RequiresAuth() {
	resp := s.Client.GET("/api/chunks",
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ChunksTestSuite) TestListChunks_RequiresProjectID() {
	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth("e2e-test-user"),
	)
	// RequireProjectID middleware returns 400 for missing header
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestListChunks_RequiresChunksReadScope() {
	// User without chunks:read scope should be forbidden
	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth("no-scope"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func (s *ChunksTestSuite) TestListChunks_Empty() {
	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.ListChunksResponse
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Empty(result.Data)
	s.Equal(0, result.TotalCount)
}

func (s *ChunksTestSuite) TestListChunks_ReturnsChunks() {
	// Create document via API - chunks are created automatically from content
	content := "This is test content that will be chunked by the document service."
	docID, chunkIDs := s.createDocumentViaAPI(content)

	// Should have at least one chunk
	s.Require().NotEmpty(chunkIDs, "Document should have created at least one chunk")

	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.ListChunksResponse
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.GreaterOrEqual(len(result.Data), 1)
	s.GreaterOrEqual(result.TotalCount, 1)

	// Check first chunk belongs to our document
	found := false
	for _, chunk := range result.Data {
		if chunk.DocumentID == docID {
			found = true
			s.NotEmpty(chunk.Text)
			s.GreaterOrEqual(chunk.Size, 1)
			break
		}
	}
	s.True(found, "Should find a chunk for our document")
}

func (s *ChunksTestSuite) TestListChunks_FilterByDocumentID() {
	// Create two documents via API
	doc1ID, _ := s.createDocumentViaAPI("Content for document one with unique text.")
	doc2ID, _ := s.createDocumentViaAPI("Content for document two with different text.")

	// Filter by doc1
	resp := s.Client.GET(fmt.Sprintf("/api/chunks?documentId=%s", doc1ID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.ListChunksResponse
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.GreaterOrEqual(len(result.Data), 1)

	for _, chunk := range result.Data {
		s.Equal(doc1ID, chunk.DocumentID, "All chunks should be from doc1")
	}

	// Filter by doc2
	resp = s.Client.GET(fmt.Sprintf("/api/chunks?documentId=%s", doc2ID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	err = json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.GreaterOrEqual(len(result.Data), 1)

	for _, chunk := range result.Data {
		s.Equal(doc2ID, chunk.DocumentID, "All chunks should be from doc2")
	}
}

func (s *ChunksTestSuite) TestListChunks_InvalidDocumentID() {
	resp := s.Client.GET("/api/chunks?documentId=invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestListChunks_ProjectIsolation() {
	// Create chunk in user's project
	docID, _ := s.createDocumentViaAPI("User's content for isolation test.")

	// Create another project via API
	otherProjectID := s.createProjectViaAPI("Other Project for Chunks")

	// Create document in other project
	otherDocResp := s.Client.POST("/api/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(otherProjectID),
		testutil.WithJSONBody(map[string]any{
			"filename": "other-doc.txt",
			"content":  "Content in other project.",
		}),
	)
	s.Require().True(otherDocResp.StatusCode == http.StatusOK || otherDocResp.StatusCode == http.StatusCreated)

	// Request with user's project should only see their chunk
	resp := s.Client.GET("/api/chunks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.ListChunksResponse
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	// All chunks should belong to documents in our project
	for _, chunk := range result.Data {
		s.Equal(docID, chunk.DocumentID, "All chunks should be from our project's document")
	}
}

// ============= Delete Tests =============

func (s *ChunksTestSuite) TestDeleteChunk_RequiresAuth() {
	resp := s.Client.DELETE("/api/chunks/" + uuid.NewString())
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteChunk_RequiresProjectID() {
	resp := s.Client.DELETE("/api/chunks/"+uuid.NewString(),
		testutil.WithAuth("e2e-test-user"),
	)
	// RequireProjectID middleware returns 400 for missing header
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteChunk_RequiresWriteScope() {
	// read-only token has chunks:read but not chunks:write
	resp := s.Client.DELETE("/api/chunks/"+uuid.NewString(),
		testutil.WithAuth("read-only"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteChunk_Success() {
	// Create document and get chunk IDs via API
	_, chunkIDs := s.createDocumentViaAPI("Content to be deleted via chunk deletion.")
	s.Require().NotEmpty(chunkIDs, "Should have at least one chunk")

	chunkID := chunkIDs[0]

	resp := s.Client.DELETE("/api/chunks/"+chunkID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNoContent, resp.StatusCode)

	// Verify deleted via API - listing by a non-existent chunk should return empty
	verifyResp := s.Client.GET("/api/chunks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, verifyResp.StatusCode)

	var result chunks.ListChunksResponse
	err := json.Unmarshal(verifyResp.Body, &result)
	s.NoError(err)

	// The deleted chunk should not be in the results
	for _, chunk := range result.Data {
		s.NotEqual(chunkID, chunk.ID, "Deleted chunk should not appear in list")
	}
}

func (s *ChunksTestSuite) TestDeleteChunk_NotFound() {
	resp := s.Client.DELETE("/api/chunks/"+uuid.NewString(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteChunk_InvalidUUID() {
	resp := s.Client.DELETE("/api/chunks/invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestDeleteChunk_ProjectIsolation() {
	// Create another project via API
	otherProjectID := s.createProjectViaAPI("Other Project for Delete Isolation")

	// Create document in other project via API
	otherDocResp := s.Client.POST("/api/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(otherProjectID),
		testutil.WithJSONBody(map[string]any{
			"filename": "other-project-doc.txt",
			"content":  "Content in another project for isolation test.",
		}),
	)
	s.Require().True(otherDocResp.StatusCode == http.StatusOK || otherDocResp.StatusCode == http.StatusCreated)

	var otherDoc map[string]any
	err := json.Unmarshal(otherDocResp.Body, &otherDoc)
	s.Require().NoError(err)
	otherDocID := otherDoc["id"].(string)

	// When running in-process, create chunks directly via DB for the other project
	var otherChunkID string
	if !s.IsExternal() {
		otherChunkID = uuid.NewString()
		err = testutil.CreateTestChunk(context.Background(), s.DB(), testutil.TestChunk{
			ID:         otherChunkID,
			DocumentID: otherDocID,
			ChunkIndex: 0,
			Text:       "Content in another project for isolation test.",
		})
		s.Require().NoError(err, "Failed to create test chunk in other project")
	} else {
		// In external mode, get chunks via API (they may have been created by workers)
		otherChunksResp := s.Client.GET(fmt.Sprintf("/api/chunks?documentId=%s", otherDocID),
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(otherProjectID),
		)
		s.Require().Equal(http.StatusOK, otherChunksResp.StatusCode)

		var otherChunks chunks.ListChunksResponse
		err = json.Unmarshal(otherChunksResp.Body, &otherChunks)
		s.Require().NoError(err)
		s.Require().NotEmpty(otherChunks.Data, "Other project should have chunks")

		otherChunkID = otherChunks.Data[0].ID
	}

	// Try to delete with user's project (different project) - should get 404
	resp := s.Client.DELETE("/api/chunks/"+otherChunkID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)

	// Verify chunk still exists in other project
	verifyResp := s.Client.GET(fmt.Sprintf("/api/chunks?documentId=%s", otherDocID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(otherProjectID),
	)
	s.Equal(http.StatusOK, verifyResp.StatusCode)

	var verifyResult chunks.ListChunksResponse
	err = json.Unmarshal(verifyResp.Body, &verifyResult)
	s.NoError(err)
	s.NotEmpty(verifyResult.Data, "Chunk should still exist in other project")
}

// ============= Bulk Delete Tests =============

func (s *ChunksTestSuite) TestBulkDeleteChunks_Success() {
	// Create document via API to get chunks
	_, chunkIDs := s.createDocumentViaAPI("Content for bulk deletion test with enough text to potentially create multiple chunks or at least one chunk.")

	s.Require().NotEmpty(chunkIDs, "Should have at least one chunk")

	// If we only have one chunk, create another document
	if len(chunkIDs) < 2 {
		_, moreChunkIDs := s.createDocumentViaAPI("Additional content for more chunks in bulk delete test.")
		chunkIDs = append(chunkIDs, moreChunkIDs...)
	}

	s.Require().GreaterOrEqual(len(chunkIDs), 1, "Should have at least one chunk for bulk delete")

	// Use available chunk IDs for bulk delete
	idsToDelete := chunkIDs
	if len(idsToDelete) > 2 {
		idsToDelete = idsToDelete[:2]
	}

	resp := s.Client.DELETE("/api/chunks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"ids": idsToDelete,
		}),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.BulkDeletionSummary
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal(len(idsToDelete), result.TotalRequested)
	s.Equal(len(idsToDelete), result.TotalDeleted)
	s.Equal(0, result.TotalFailed)
}

func (s *ChunksTestSuite) TestBulkDeleteChunks_EmptyArray() {
	resp := s.Client.DELETE("/api/chunks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"ids": []string{},
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ChunksTestSuite) TestBulkDeleteChunks_PartialNotFound() {
	// Create document via API to get chunks
	_, chunkIDs := s.createDocumentViaAPI("Content for partial bulk delete test.")
	s.Require().NotEmpty(chunkIDs, "Should have at least one chunk")

	chunkID := chunkIDs[0]
	nonExistentID := uuid.NewString()

	resp := s.Client.DELETE("/api/chunks",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"ids": []string{chunkID, nonExistentID},
		}),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.BulkDeletionSummary
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal(2, result.TotalRequested)
	s.Equal(1, result.TotalDeleted)
	s.Equal(1, result.TotalFailed)
}

// ============= Delete By Document Tests =============

func (s *ChunksTestSuite) TestDeleteByDocument_Success() {
	// Create document via API to get chunks
	docID, chunkIDs := s.createDocumentViaAPI("Content for delete by document test with some additional text.")
	s.Require().NotEmpty(chunkIDs, "Should have at least one chunk")
	initialChunkCount := len(chunkIDs)

	resp := s.Client.DELETE("/api/chunks/by-document/"+docID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.DocumentChunksDeletionResult
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal(docID, result.DocumentID)
	s.Equal(initialChunkCount, result.ChunksDeleted)
	s.True(result.Success)
}

func (s *ChunksTestSuite) TestDeleteByDocument_NoChunks() {
	// Create document via API, then delete all its chunks
	docID, chunkIDs := s.createDocumentViaAPI("Content for no chunks test.")

	// Delete all chunks first
	if len(chunkIDs) > 0 {
		s.Client.DELETE("/api/chunks",
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
			testutil.WithJSONBody(map[string]any{
				"ids": chunkIDs,
			}),
		)
	}

	// Now try to delete by document when no chunks exist
	resp := s.Client.DELETE("/api/chunks/by-document/"+docID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.DocumentChunksDeletionResult
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal(0, result.ChunksDeleted)
	s.True(result.Success)
}

func (s *ChunksTestSuite) TestDeleteByDocument_InvalidUUID() {
	resp := s.Client.DELETE("/api/chunks/by-document/invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// ============= Bulk Delete By Documents Tests =============

func (s *ChunksTestSuite) TestBulkDeleteByDocuments_Success() {
	// Create two documents via API
	doc1ID, doc1ChunkIDs := s.createDocumentViaAPI("Content for bulk document delete test one.")
	doc2ID, doc2ChunkIDs := s.createDocumentViaAPI("Content for bulk document delete test two with more text.")

	totalChunks := len(doc1ChunkIDs) + len(doc2ChunkIDs)
	s.Require().GreaterOrEqual(totalChunks, 1, "Should have at least one chunk total")

	resp := s.Client.DELETE("/api/chunks/by-documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"documentIds": []string{doc1ID, doc2ID},
		}),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result chunks.BulkDocumentChunksDeletionSummary
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal(2, result.TotalDocuments)
	s.Equal(totalChunks, result.TotalChunks)
	s.Len(result.Results, 2)
}

func (s *ChunksTestSuite) TestBulkDeleteByDocuments_EmptyArray() {
	resp := s.Client.DELETE("/api/chunks/by-documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"documentIds": []string{},
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}
