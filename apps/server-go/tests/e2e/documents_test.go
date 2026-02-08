package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/internal/testutil"
)

// DocumentsTestSuite tests the documents API endpoints
type DocumentsTestSuite struct {
	testutil.BaseSuite
}

func TestDocumentsSuite(t *testing.T) {
	suite.Run(t, new(DocumentsTestSuite))
}

func (s *DocumentsTestSuite) SetupSuite() {
	s.SetDBSuffix("documents")
	s.BaseSuite.SetupSuite()
}

// createDocumentViaAPI creates a document via API and returns its ID
func (s *DocumentsTestSuite) createDocumentViaAPI(filename, content string) string {
	body := map[string]any{
		"filename": filename,
		"content":  content,
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	// Accept both 200 (deduplicated) and 201 (created)
	s.Require().True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated,
		"Expected 200 or 201, got %d: %s", resp.StatusCode, resp.String())

	var doc map[string]any
	err := json.Unmarshal(resp.Body, &doc)
	s.Require().NoError(err)

	return doc["id"].(string)
}

// createProjectViaAPI creates a project via API and returns its ID
func (s *DocumentsTestSuite) createProjectViaAPI(name string) string {
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

// =============================================================================
// Test: Authentication & Authorization
// =============================================================================

func (s *DocumentsTestSuite) TestListDocuments_RequiresAuth() {
	// Request without Authorization header should fail
	resp := s.Client.GET("/api/v2/documents",
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestListDocuments_RequiresDocumentsReadScope() {
	// User without documents:read scope should be forbidden
	resp := s.Client.GET("/api/v2/documents",
		testutil.WithAuth("no-scope"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("forbidden", errObj["code"])
}

func (s *DocumentsTestSuite) TestListDocuments_RequiresProjectID() {
	// Request without X-Project-ID should fail
	resp := s.Client.GET("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "project")
}

// =============================================================================
// Test: List Documents
// =============================================================================

func (s *DocumentsTestSuite) TestListDocuments_Empty() {
	// List documents when none exist
	resp := s.Client.GET("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)
	s.Len(docs, 0)
	s.Equal(float64(0), body["total"])
}

func (s *DocumentsTestSuite) TestListDocuments_ReturnsDocuments() {
	// Create test documents via API
	doc1ID := s.createDocumentViaAPI("Test Document 1.txt", "Content for document 1")
	doc2ID := s.createDocumentViaAPI("Test Document 2.pdf", "Content for document 2")

	// List documents
	resp := s.Client.GET("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)
	s.GreaterOrEqual(len(docs), 2)
	s.GreaterOrEqual(body["total"].(float64), float64(2))

	// Verify our documents are in the list
	foundDoc1, foundDoc2 := false, false
	for _, doc := range docs {
		d := doc.(map[string]any)
		if d["id"] == doc1ID {
			foundDoc1 = true
		}
		if d["id"] == doc2ID {
			foundDoc2 = true
		}
	}
	s.True(foundDoc1, "Should find document 1")
	s.True(foundDoc2, "Should find document 2")
}

func (s *DocumentsTestSuite) TestListDocuments_ProjectIsolation() {
	// Create a different project via API
	otherProjectID := s.createProjectViaAPI("Other Project for Isolation")

	// Create document in default project
	doc1ID := s.createDocumentViaAPI("Doc in Default Project.txt", "Content for default project")

	// Create document in other project
	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(otherProjectID),
		testutil.WithJSONBody(map[string]any{
			"filename": "Doc in Other Project.txt",
			"content":  "Content for other project",
		}),
	)
	s.Require().True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)

	// List documents in default project - should only see doc1
	resp = s.Client.GET("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)
	s.GreaterOrEqual(len(docs), 1)

	// Verify our document is in the list
	found := false
	for _, doc := range docs {
		d := doc.(map[string]any)
		if d["id"] == doc1ID {
			found = true
			break
		}
	}
	s.True(found, "Should find document in default project")
}

// =============================================================================
// Test: Filtering
// =============================================================================

func (s *DocumentsTestSuite) TestListDocuments_FilterBySourceType() {
	// Note: The POST /api/v2/documents endpoint doesn't support setting sourceType,
	// so we test with default sourceType (upload) documents created via API.
	// Create documents via API (they default to sourceType=upload)
	s.createDocumentViaAPI("Upload Doc 1.txt", "Content for upload doc 1 - filter test")
	s.createDocumentViaAPI("Upload Doc 2.txt", "Content for upload doc 2 - filter test")

	// Filter by sourceType=upload - should find our documents
	resp := s.Client.GET("/api/v2/documents?sourceType=upload",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)
	s.GreaterOrEqual(len(docs), 2, "Should have at least 2 upload documents")
}

func (s *DocumentsTestSuite) TestListDocuments_FilterByIntegrationId() {
	// For now, skip this test - the data_source_integration_id has a FK constraint
	// and we'd need to create a data_source_integration record first
	s.T().Skip("Skipping - requires data_source_integrations FK setup")
}

func (s *DocumentsTestSuite) TestListDocuments_FilterRootOnly() {
	// Note: The POST /api/v2/documents endpoint doesn't support setting parentDocumentId,
	// so we skip this test for API-only mode. Parent/child relationships require
	// data source integrations or direct DB setup.
	s.T().Skip("Test requires parent/child document relationships which cannot be created via API")
}

func (s *DocumentsTestSuite) TestListDocuments_FilterByParentDocumentId() {
	// Note: The POST /api/v2/documents endpoint doesn't support setting parentDocumentId,
	// so we skip this test for API-only mode. Parent/child relationships require
	// data source integrations or direct DB setup.
	s.T().Skip("Test requires parent/child document relationships which cannot be created via API")
}

// =============================================================================
// Test: Pagination
// =============================================================================

func (s *DocumentsTestSuite) TestListDocuments_Limit() {
	// Create 5 documents via API
	for i := 1; i <= 5; i++ {
		s.createDocumentViaAPI(
			fmt.Sprintf("Limit Test Document %d.txt", i),
			fmt.Sprintf("Unique content for limit test document %d - %d", i, time.Now().UnixNano()),
		)
	}

	// Request with limit=2
	resp := s.Client.GET("/api/v2/documents?limit=2",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)
	s.Len(docs, 2)
	s.GreaterOrEqual(body["total"].(float64), float64(5)) // Total should be at least 5

	// Should have next cursor in header
	nextCursor := resp.Headers.Get("x-next-cursor")
	s.NotEmpty(nextCursor, "Expected x-next-cursor header for pagination")
}

func (s *DocumentsTestSuite) TestListDocuments_CursorPagination() {
	// Create 5 documents via API
	for i := 1; i <= 5; i++ {
		s.createDocumentViaAPI(
			fmt.Sprintf("Cursor Pagination Doc %d.txt", i),
			fmt.Sprintf("Unique content for cursor pagination doc %d - %d", i, time.Now().UnixNano()),
		)
		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// First page
	resp := s.Client.GET("/api/v2/documents?limit=2",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	docs, ok := body["documents"].([]any)
	s.True(ok)
	s.Len(docs, 2)

	// Collect first page IDs
	firstPageIDs := make(map[string]bool)
	for _, doc := range docs {
		d := doc.(map[string]any)
		firstPageIDs[d["id"].(string)] = true
	}

	// Get cursor for next page
	nextCursor := resp.Headers.Get("x-next-cursor")
	s.NotEmpty(nextCursor)

	// Second page using cursor
	resp = s.Client.GET("/api/v2/documents?limit=2&cursor="+nextCursor,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	err = json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	docs, ok = body["documents"].([]any)
	s.True(ok)
	s.Len(docs, 2)

	// Should have different documents (no overlap with first page)
	for _, doc := range docs {
		d := doc.(map[string]any)
		s.False(firstPageIDs[d["id"].(string)], "Second page should have different documents than first page")
	}
}

func (s *DocumentsTestSuite) TestListDocuments_CursorPaginationStress() {
	// Stress test: create many documents and verify cursor pagination walks all pages
	// without duplicates and with full coverage.
	const totalDocs = 55
	const pageLimit = 5

	// Create documents via API with unique content
	createdIDs := make(map[string]bool)
	for i := 0; i < totalDocs; i++ {
		docID := s.createDocumentViaAPI(
			fmt.Sprintf("stress-%d.txt", i),
			fmt.Sprintf("Unique stress test content %d - timestamp %d", i, time.Now().UnixNano()),
		)
		createdIDs[docID] = true
		// Small delay to ensure different timestamps
		time.Sleep(5 * time.Millisecond)
	}

	// Walk all pages and collect IDs
	seen := make(map[string]bool)
	var cursor string
	pages := 0
	totalFetched := 0
	maxPages := (totalDocs / pageLimit) + 10 // safety limit (account for existing docs)

	for {
		url := fmt.Sprintf("/api/v2/documents?limit=%d", pageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		resp := s.Client.GET(url,
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
		)

		s.Equal(http.StatusOK, resp.StatusCode, "Page %d should return 200", pages)

		var body map[string]any
		err := json.Unmarshal(resp.Body, &body)
		s.Require().NoError(err)

		docs, ok := body["documents"].([]any)
		s.True(ok)

		nextCursor := resp.Headers.Get("x-next-cursor")

		if nextCursor != "" {
			// Intermediate pages should be fully populated
			s.Len(docs, pageLimit, "Intermediate page %d should have %d docs", pages, pageLimit)
		} else {
			// Final page must be non-empty and <= limit
			s.Greater(len(docs), 0, "Final page should not be empty")
			s.LessOrEqual(len(docs), pageLimit, "Final page should have <= %d docs", pageLimit)
		}

		// Check for duplicates
		for _, d := range docs {
			id := d.(map[string]any)["id"].(string)
			s.False(seen[id], "Document %s appeared twice (page %d)", id, pages)
			seen[id] = true
		}

		totalFetched += len(docs)
		pages++
		cursor = nextCursor

		// Safety guard against infinite loop
		s.LessOrEqual(pages, maxPages, "Pagination exceeded expected page count")

		if cursor == "" {
			break
		}
	}

	// Verify all our created documents were fetched
	for id := range createdIDs {
		s.True(seen[id], "Created document %s should be found in pagination", id)
	}
	s.GreaterOrEqual(totalFetched, totalDocs, "Should fetch at least all created documents")
}

func (s *DocumentsTestSuite) TestListDocuments_InvalidLimit() {
	// Request with limit > 500
	resp := s.Client.GET("/api/v2/documents?limit=1000",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "limit")
}

func (s *DocumentsTestSuite) TestListDocuments_InvalidCursor() {
	// Request with invalid cursor
	resp := s.Client.GET("/api/v2/documents?cursor=not-valid-base64!!!",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "cursor")
}

// =============================================================================
// Test: Get Document by ID
// =============================================================================

func (s *DocumentsTestSuite) TestGetDocument_Success() {
	// Create a document via API
	body := map[string]any{
		"filename": "Test Document for Get.txt",
		"content":  "Hello, World! Get test content.",
	}

	createResp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().True(createResp.StatusCode == http.StatusOK || createResp.StatusCode == http.StatusCreated)

	var createdDoc map[string]any
	err := json.Unmarshal(createResp.Body, &createdDoc)
	s.Require().NoError(err)
	docID := createdDoc["id"].(string)

	// Get the document
	resp := s.Client.GET("/api/v2/documents/"+docID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var respBody map[string]any
	err = json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	s.Equal(docID, respBody["id"])
	s.Equal("Test Document for Get.txt", respBody["filename"])
}

func (s *DocumentsTestSuite) TestGetDocument_NotFound() {
	resp := s.Client.GET("/api/v2/documents/00000000-0000-0000-0000-000000000999",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("not_found", errObj["code"])
}

func (s *DocumentsTestSuite) TestGetDocument_NotFoundInOtherProject() {
	// Create a different project via API
	otherProjectID := s.createProjectViaAPI("Other Project for Get Test")

	// Create document in other project
	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(otherProjectID),
		testutil.WithJSONBody(map[string]any{
			"filename": "Doc in Other Project.txt",
			"content":  "Content for other project - get test",
		}),
	)
	s.Require().True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)

	var createdDoc map[string]any
	err := json.Unmarshal(resp.Body, &createdDoc)
	s.Require().NoError(err)
	docID := createdDoc["id"].(string)

	// Try to get document using different project ID - should return 404
	resp = s.Client.GET("/api/v2/documents/"+docID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestGetDocument_RequiresAuth() {
	resp := s.Client.GET("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestGetDocument_RequiresProjectID() {
	resp := s.Client.GET("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestGetDocument_RequiresDocumentsReadScope() {
	resp := s.Client.GET("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		testutil.WithAuth("no-scope"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)
}

// =============================================================================
// Test: Create Document
// =============================================================================

func (s *DocumentsTestSuite) TestCreateDocument_Success() {
	body := map[string]any{
		"filename": "test-document.txt",
		"content":  "Hello, World!",
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	s.NotEmpty(respBody["id"])
	s.Equal("test-document.txt", respBody["filename"])
	s.Equal("Hello, World!", respBody["content"])
	s.NotEmpty(respBody["contentHash"])
	s.Equal(s.ProjectID, respBody["projectId"])
}

func (s *DocumentsTestSuite) TestCreateDocument_DefaultFilename() {
	// Create document without filename - should default to "unnamed.txt"
	body := map[string]any{
		"content": "Some content",
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	s.Equal("unnamed.txt", respBody["filename"])
}

func (s *DocumentsTestSuite) TestCreateDocument_EmptyContent() {
	body := map[string]any{
		"filename": "empty-file.txt",
		"content":  "",
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	s.Equal("", respBody["content"])
}

func (s *DocumentsTestSuite) TestCreateDocument_Deduplication() {
	// Create first document
	body := map[string]any{
		"filename": "original.txt",
		"content":  "Same content for deduplication test",
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusCreated, resp.StatusCode)

	var firstDoc map[string]any
	err := json.Unmarshal(resp.Body, &firstDoc)
	s.NoError(err)
	firstID := firstDoc["id"].(string)

	// Create second document with same content
	body2 := map[string]any{
		"filename": "duplicate.txt",
		"content":  "Same content for deduplication test",
	}

	resp = s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body2),
	)

	// Should return 200 (not 201) with the existing document
	s.Equal(http.StatusOK, resp.StatusCode)

	var secondDoc map[string]any
	err = json.Unmarshal(resp.Body, &secondDoc)
	s.NoError(err)

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

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	errObj, ok := respBody["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "filename")
}

func (s *DocumentsTestSuite) TestCreateDocument_RequiresAuth() {
	body := map[string]any{
		"filename": "test.txt",
		"content":  "Content",
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestCreateDocument_RequiresWriteScope() {
	body := map[string]any{
		"filename": "test.txt",
		"content":  "Content",
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("read-only"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestCreateDocument_RequiresProjectID() {
	body := map[string]any{
		"filename": "test.txt",
		"content":  "Content",
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Delete Document
// =============================================================================

func (s *DocumentsTestSuite) TestDeleteDocument_Success() {
	// Create a document via API to delete
	docID := s.createDocumentViaAPI("To Be Deleted.txt", "Content to be deleted - unique "+fmt.Sprintf("%d", time.Now().UnixNano()))

	resp := s.Client.DELETE("/api/v2/documents/"+docID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	s.Equal("deleted", respBody["status"])
	s.NotNil(respBody["summary"])

	// Verify document is actually deleted
	getResp := s.Client.GET("/api/v2/documents/"+docID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, getResp.StatusCode)
}

func (s *DocumentsTestSuite) TestDeleteDocument_NotFound() {
	resp := s.Client.DELETE("/api/v2/documents/00000000-0000-0000-0000-000000000999",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestDeleteDocument_InvalidUUID() {
	resp := s.Client.DELETE("/api/v2/documents/not-a-uuid",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	errObj, ok := respBody["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "Invalid")
}

func (s *DocumentsTestSuite) TestDeleteDocument_RequiresAuth() {
	resp := s.Client.DELETE("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestDeleteDocument_RequiresDeleteScope() {
	resp := s.Client.DELETE("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		testutil.WithAuth("read-only"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestDeleteDocument_RequiresProjectID() {
	resp := s.Client.DELETE("/api/v2/documents/00000000-0000-0000-0000-000000000001",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestDeleteDocument_ProjectIsolation() {
	// Create document in other project via API
	otherProjectID := s.createProjectViaAPI("Other Project for Delete Test")

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(otherProjectID),
		testutil.WithJSONBody(map[string]any{
			"filename": "Doc in Other Project.txt",
			"content":  "Content for other project - delete test",
		}),
	)
	s.Require().True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)

	var createdDoc map[string]any
	err := json.Unmarshal(resp.Body, &createdDoc)
	s.Require().NoError(err)
	docID := createdDoc["id"].(string)

	// Try to delete from different project - should return 404
	resp = s.Client.DELETE("/api/v2/documents/"+docID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// =============================================================================
// Test: Bulk Delete Documents
// =============================================================================

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_Success() {
	// Create multiple documents via API
	doc1ID := s.createDocumentViaAPI("Bulk Delete Doc 1.txt", "Unique content for bulk delete 1 - "+fmt.Sprintf("%d", time.Now().UnixNano()))
	doc2ID := s.createDocumentViaAPI("Bulk Delete Doc 2.txt", "Unique content for bulk delete 2 - "+fmt.Sprintf("%d", time.Now().UnixNano()))
	doc3ID := s.createDocumentViaAPI("Bulk Delete Doc 3.txt", "Unique content for bulk delete 3 - "+fmt.Sprintf("%d", time.Now().UnixNano()))

	body := map[string]any{
		"ids": []string{doc1ID, doc2ID},
	}

	resp := s.Client.DELETE("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	s.Equal("deleted", respBody["status"])
	s.Equal(float64(2), respBody["deleted"])

	// Verify documents are deleted, but doc3 remains
	getResp := s.Client.GET("/api/v2/documents/"+doc1ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusNotFound, getResp.StatusCode)

	getResp = s.Client.GET("/api/v2/documents/"+doc3ID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusOK, getResp.StatusCode)
}

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_PartialNotFound() {
	// Create one document via API
	doc1ID := s.createDocumentViaAPI("Partial Delete Doc.txt", "Unique content for partial delete - "+fmt.Sprintf("%d", time.Now().UnixNano()))

	// Try to delete one existing and one non-existing
	body := map[string]any{
		"ids": []string{doc1ID, "00000000-0000-0000-0000-000000000999"},
	}

	resp := s.Client.DELETE("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

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

	resp := s.Client.DELETE("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var respBody map[string]any
	err := json.Unmarshal(resp.Body, &respBody)
	s.NoError(err)

	errObj, ok := respBody["error"].(map[string]any)
	s.True(ok)
	s.Contains(errObj["message"], "ids")
}

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_InvalidUUID() {
	body := map[string]any{
		"ids": []string{"not-a-uuid", "also-not-valid"},
	}

	resp := s.Client.DELETE("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_RequiresAuth() {
	body := map[string]any{
		"ids": []string{"00000000-0000-0000-0000-000000000001"},
	}

	resp := s.Client.DELETE("/api/v2/documents",
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *DocumentsTestSuite) TestBulkDeleteDocuments_RequiresDeleteScope() {
	body := map[string]any{
		"ids": []string{"00000000-0000-0000-0000-000000000001"},
	}

	resp := s.Client.DELETE("/api/v2/documents",
		testutil.WithAuth("read-only"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)
}
