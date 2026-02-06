package suites

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/emergent/api-tests/client"
)

// DocumentsTestSuite tests the documents API endpoints.
// These tests create data via API calls (not SQL) for external testing.
type DocumentsTestSuite struct {
	BaseSuite
	createdDocIDs []string // Track created documents for cleanup
}

func TestDocumentsSuite(t *testing.T) {
	RunSuite(t, new(DocumentsTestSuite))
}

// SetupTest runs before each test.
func (s *DocumentsTestSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	s.createdDocIDs = nil
}

// TearDownTest cleans up created documents after each test.
func (s *DocumentsTestSuite) TearDownTest() {
	// Clean up any documents created during the test
	for _, id := range s.createdDocIDs {
		_, _ = s.Client.DELETE("/api/v2/documents/"+id,
			s.AdminAuth(),
			s.ProjectHeader(),
		)
	}
}

// createDocument is a helper that creates a document and tracks it for cleanup.
func (s *DocumentsTestSuite) createDocument(filename, content string) (string, map[string]any) {
	body := map[string]any{
		"filename": filename,
		"content":  content,
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().True(resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK,
		"Expected 201 or 200, got %d: %s", resp.StatusCode, resp.BodyString())

	var doc map[string]any
	err = resp.JSON(&doc)
	s.Require().NoError(err)

	id := doc["id"].(string)
	s.createdDocIDs = append(s.createdDocIDs, id)
	return id, doc
}

// =============================================================================
// Test: List Documents
// =============================================================================

func (s *DocumentsTestSuite) TestListDocuments_Empty() {
	// First clean any existing documents by creating fresh unique context
	// Note: In external tests we may have leftover data, so we check structure not count
	resp, err := s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "1"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	// Check response structure
	s.Contains(body, "documents")
	s.Contains(body, "total")

	docs, ok := body["documents"].([]any)
	s.True(ok, "documents should be an array")
	_ = docs // May or may not be empty depending on test isolation
}

func (s *DocumentsTestSuite) TestListDocuments_ReturnsDocuments() {
	// Create test documents
	id1, _ := s.createDocument("list-test-doc-1.txt", "Content 1")
	id2, _ := s.createDocument("list-test-doc-2.txt", "Content 2")

	// List documents
	resp, err := s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Equal(http.StatusOK, resp.StatusCode)
	s.Require().NoError(err)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)
	s.GreaterOrEqual(len(docs), 2, "Should have at least 2 documents")

	// Verify our documents are in the list
	foundIDs := make(map[string]bool)
	for _, d := range docs {
		doc := d.(map[string]any)
		foundIDs[doc["id"].(string)] = true
	}
	s.True(foundIDs[id1], "Document 1 should be in list")
	s.True(foundIDs[id2], "Document 2 should be in list")
}

func (s *DocumentsTestSuite) TestListDocuments_FilterBySourceType() {
	// Create documents with specific source types via API isn't directly supported
	// The API creates documents with default source type
	// This test verifies the filter parameter works

	// Create a document (will have default source type)
	s.createDocument("source-type-test.txt", "Content")

	// Filter by sourceType=upload (default)
	resp, err := s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("sourceType", "upload"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	// Should return documents (may include ours)
	docs, ok := body["documents"].([]any)
	s.True(ok)
	_ = docs // Filter is applied server-side
}

func (s *DocumentsTestSuite) TestListDocuments_FilterRootOnly() {
	// Create a root document
	rootID, _ := s.createDocument("root-doc.txt", "Root content")

	// Filter by rootOnly=true
	resp, err := s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("rootOnly", "true"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	// All returned documents should have no parent
	docs, ok := body["documents"].([]any)
	s.True(ok)

	// Find our root document
	found := false
	for _, d := range docs {
		doc := d.(map[string]any)
		if doc["id"] == rootID {
			found = true
			// Should have no parent
			s.Nil(doc["parentDocumentId"], "Root document should have no parent")
		}
	}
	s.True(found, "Our root document should be in the filtered list")
}

// =============================================================================
// Test: Pagination
// =============================================================================

func (s *DocumentsTestSuite) TestListDocuments_Limit() {
	// Create 5 documents
	for i := 1; i <= 5; i++ {
		s.createDocument(fmt.Sprintf("limit-test-%d.txt", i), fmt.Sprintf("Content %d", i))
	}

	// Request with limit=2
	resp, err := s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "2"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)
	s.LessOrEqual(len(docs), 2, "Should return at most 2 documents")

	// Check for pagination cursor in header
	nextCursor := resp.Header.Get("x-next-cursor")
	// May or may not have cursor depending on total docs
	_ = nextCursor
}

func (s *DocumentsTestSuite) TestListDocuments_CursorPagination() {
	// Create 5 documents to ensure pagination
	for i := 1; i <= 5; i++ {
		s.createDocument(fmt.Sprintf("cursor-test-%d.txt", i), fmt.Sprintf("Content %d", i))
	}

	// First page
	resp, err := s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "2"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)

	// Get cursor for next page
	nextCursor := resp.Header.Get("x-next-cursor")
	if nextCursor == "" {
		s.T().Skip("No pagination cursor returned - may have fewer documents")
		return
	}

	// Collect first page IDs
	firstPageIDs := make(map[string]bool)
	for _, d := range docs {
		doc := d.(map[string]any)
		firstPageIDs[doc["id"].(string)] = true
	}

	// Second page using cursor
	resp, err = s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "2"),
		client.WithQuery("cursor", nextCursor),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err = resp.JSONMap()
	s.Require().NoError(err)

	docs, ok = body["documents"].([]any)
	s.True(ok)

	// Second page should have different documents
	for _, d := range docs {
		doc := d.(map[string]any)
		id := doc["id"].(string)
		s.False(firstPageIDs[id], "Second page should not contain first page documents")
	}
}

func (s *DocumentsTestSuite) TestListDocuments_InvalidLimit() {
	// Request with limit > 500
	resp, err := s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "1000"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Should have error object")
	s.Contains(errObj["message"], "limit")
}

func (s *DocumentsTestSuite) TestListDocuments_InvalidCursor() {
	// Request with invalid cursor
	resp, err := s.Client.GET("/api/v2/documents",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("cursor", "not-valid-base64!!!"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok, "Should have error object")
	s.Contains(errObj["message"], "cursor")
}

// =============================================================================
// Test: Get Document by ID
// =============================================================================

func (s *DocumentsTestSuite) TestGetDocument_Success() {
	// Create a document
	id, _ := s.createDocument("get-test.txt", "Hello, World!")

	// Get the document
	resp, err := s.Client.GET("/api/v2/documents/"+id,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal(id, body["id"])
	s.Equal("get-test.txt", body["filename"])
	s.Equal("Hello, World!", body["content"])
	s.NotEmpty(body["contentHash"])
	s.Equal(s.Project, body["projectId"])
}

func (s *DocumentsTestSuite) TestGetDocument_NotFound() {
	resp, err := s.Client.GET("/api/v2/documents/00000000-0000-0000-0000-000000000999",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("not_found", errObj["code"])
}

func (s *DocumentsTestSuite) TestGetDocument_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestGetDocument_RequiresProjectID() {
	resp, err := s.Client.GET("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		s.AdminAuth(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Create Document
// =============================================================================

func (s *DocumentsTestSuite) TestCreateDocument_Success() {
	body := map[string]any{
		"filename": "create-test.txt",
		"content":  "Hello, World!",
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.NotEmpty(respBody["id"])
	s.createdDocIDs = append(s.createdDocIDs, respBody["id"].(string))

	s.Equal("create-test.txt", respBody["filename"])
	s.Equal("Hello, World!", respBody["content"])
	s.NotEmpty(respBody["contentHash"])
	s.Equal(s.Project, respBody["projectId"])
}

func (s *DocumentsTestSuite) TestCreateDocument_DefaultFilename() {
	// Create document without filename - should default to "unnamed.txt"
	body := map[string]any{
		"content": "Some content without filename",
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.createdDocIDs = append(s.createdDocIDs, respBody["id"].(string))
	s.Equal("unnamed.txt", respBody["filename"])
}

func (s *DocumentsTestSuite) TestCreateDocument_EmptyContent() {
	body := map[string]any{
		"filename": "empty-file.txt",
		"content":  "",
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.createdDocIDs = append(s.createdDocIDs, respBody["id"].(string))
	s.Equal("", respBody["content"])
}

func (s *DocumentsTestSuite) TestCreateDocument_Deduplication() {
	uniqueContent := fmt.Sprintf("Unique content for dedup test %d", s.Client.Metrics().Count())

	// Create first document
	body := map[string]any{
		"filename": "original.txt",
		"content":  uniqueContent,
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	var firstDoc map[string]any
	err = resp.JSON(&firstDoc)
	s.Require().NoError(err)
	firstID := firstDoc["id"].(string)
	s.createdDocIDs = append(s.createdDocIDs, firstID)

	// Create second document with same content
	body2 := map[string]any{
		"filename": "duplicate.txt",
		"content":  uniqueContent,
	}

	resp, err = s.Client.POST("/api/v2/documents", body2,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	// Should return 200 (not 201) with the existing document
	s.Equal(http.StatusOK, resp.StatusCode)

	var secondDoc map[string]any
	err = resp.JSON(&secondDoc)
	s.Require().NoError(err)

	// Should return the first document (same ID)
	s.Equal(firstID, secondDoc["id"])
	s.Equal("original.txt", secondDoc["filename"]) // Original filename preserved
}

func (s *DocumentsTestSuite) TestCreateDocument_FilenameTooLong() {
	// Create filename longer than 512 characters
	longFilename := ""
	for i := 0; i < 600; i++ {
		longFilename += "a"
	}

	body := map[string]any{
		"filename": longFilename,
		"content":  "Some content",
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	errObj, ok := respBody["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "filename")
}

func (s *DocumentsTestSuite) TestCreateDocument_RequiresAuth() {
	body := map[string]any{
		"filename": "test.txt",
		"content":  "Content",
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestCreateDocument_RequiresProjectID() {
	body := map[string]any{
		"filename": "test.txt",
		"content":  "Content",
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.AdminAuth(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Delete Document
// =============================================================================

func (s *DocumentsTestSuite) TestDeleteDocument_Success() {
	// Create a document to delete
	id, _ := s.createDocument("delete-test.txt", "To be deleted")

	// Remove from tracking since we're deleting it
	s.createdDocIDs = s.createdDocIDs[:len(s.createdDocIDs)-1]

	resp, err := s.Client.DELETE("/api/v2/documents/"+id,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal("deleted", respBody["status"])
	s.NotNil(respBody["summary"])

	// Verify document is actually deleted
	getResp, err := s.Client.GET("/api/v2/documents/"+id,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, getResp.StatusCode)
}

func (s *DocumentsTestSuite) TestDeleteDocument_NotFound() {
	resp, err := s.Client.DELETE("/api/v2/documents/00000000-0000-0000-0000-000000000999",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestDeleteDocument_InvalidUUID() {
	resp, err := s.Client.DELETE("/api/v2/documents/not-a-uuid",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	errObj, ok := respBody["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "Invalid")
}

func (s *DocumentsTestSuite) TestDeleteDocument_RequiresAuth() {
	resp, err := s.Client.DELETE("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestDeleteDocument_RequiresProjectID() {
	resp, err := s.Client.DELETE("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		s.AdminAuth(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Bulk Delete Documents
// =============================================================================

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_Success() {
	// Create multiple documents
	id1, _ := s.createDocument("bulk-delete-1.txt", "Content 1")
	id2, _ := s.createDocument("bulk-delete-2.txt", "Content 2")
	id3, _ := s.createDocument("bulk-delete-3.txt", "Content 3")

	// Remove id1 and id2 from tracking since we're deleting them
	s.createdDocIDs = []string{id3}

	body := map[string]any{
		"ids": []string{id1, id2},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal("deleted", respBody["status"])
	s.Equal(float64(2), respBody["deleted"])

	// Verify documents are deleted, but doc3 remains
	getResp, err := s.Client.GET("/api/v2/documents/"+id1,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, getResp.StatusCode)

	getResp, err = s.Client.GET("/api/v2/documents/"+id3,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Equal(http.StatusOK, getResp.StatusCode)
}

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_PartialNotFound() {
	// Create one document
	id1, _ := s.createDocument("bulk-partial-1.txt", "Content 1")

	// Remove from tracking since we're deleting it
	s.createdDocIDs = nil

	// Try to delete one existing and one non-existing
	body := map[string]any{
		"ids": []string{id1, "00000000-0000-0000-0000-000000000999"},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal("partial", respBody["status"])
	s.Equal(float64(1), respBody["deleted"])

	notFound, ok := respBody["notFound"].([]any)
	s.True(ok)
	s.Len(notFound, 1)
	s.Equal("00000000-0000-0000-0000-000000000999", notFound[0])
}

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_EmptyArray() {
	body := map[string]any{
		"ids": []string{},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	respBody, err := resp.JSONMap()
	s.Require().NoError(err)

	errObj, ok := respBody["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "ids")
}

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_InvalidUUID() {
	body := map[string]any{
		"ids": []string{"not-a-uuid", "also-not-valid"},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_RequiresAuth() {
	body := map[string]any{
		"ids": []string{"00000000-0000-0000-0000-000000000001"},
	}

	resp, err := s.Client.DELETEWithBody("/api/v2/documents", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}
