// Package api_test — tenant_isolation_test.go
//
// Tests for RLS policies and tenant isolation.
// Ported from emergent.memory/apps/server/tests/e2e/tenant_isolation_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

// =============================================================================
// Header Validation
// =============================================================================

func TestTenantIsolation_RejectsInvalidUUIDHeader(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/documents", e2eTestToken(), "not-a-uuid",
		jsonBody(map[string]any{
			"filename": "test.txt",
			"content":  "test content",
		}))

	// Accept 400, 422, or 500
	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusInternalServerError:
		// ok
	default:
		mustStatus(t, resp, http.StatusBadRequest) // will print a meaningful failure
	}
	resp.Body.Close()
}

func TestTenantIsolation_RequiresProjectIDHeader(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// No X-Project-ID header
	resp := doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestTenantIsolation_RequiresProjectIDForChunks(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// =============================================================================
// Document RLS Isolation
// =============================================================================

func TestTenantIsolation_DocumentsIsolatedByProject(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)
	project2ID, _ := setupProjectLogged(t, rl)

	doc1ID := createDocument(t, project1ID, "project1-doc.txt", "Content for project 1")
	doc2ID := createDocument(t, project2ID, "project2-doc.txt", "Content for project 2")

	// List project1 documents — should only see project1's document
	resp := doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), project1ID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result1 struct {
		Documents []map[string]any `json:"documents"`
	}
	parseBodyJSON(t, body, &result1)

	doc1Found, doc2Found := false, false
	for _, doc := range result1.Documents {
		if doc["id"] == doc1ID {
			doc1Found = true
		}
		if doc["id"] == doc2ID {
			doc2Found = true
		}
	}
	if !doc1Found {
		t.Error("document from project 1 should be visible in project 1 context")
	}
	if doc2Found {
		t.Error("document from project 2 should NOT be visible in project 1 context")
	}

	// List project2 documents — should only see project2's document
	resp = doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), project2ID, nil)
	body = mustStatus(t, resp, http.StatusOK)
	var result2 struct {
		Documents []map[string]any `json:"documents"`
	}
	parseBodyJSON(t, body, &result2)

	doc1Found, doc2Found = false, false
	for _, doc := range result2.Documents {
		if doc["id"] == doc1ID {
			doc1Found = true
		}
		if doc["id"] == doc2ID {
			doc2Found = true
		}
	}
	if doc1Found {
		t.Error("document from project 1 should NOT be visible in project 2 context")
	}
	if !doc2Found {
		t.Error("document from project 2 should be visible in project 2 context")
	}
	rl.Printf("documents correctly isolated between project1=%s and project2=%s", project1ID, project2ID)
}

func TestTenantIsolation_CannotAccessOtherProjectDocument(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)
	project2ID, _ := setupProjectLogged(t, rl)

	doc1ID := createDocument(t, project1ID, "secret-doc.txt", "Secret content")

	// Try to GET document from project 2 context — should fail
	resp := doAPILogged(t, rl, "GET", "/api/documents/"+doc1ID, e2eTestToken(), project2ID, nil)
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusNotFound:
		// expected
	default:
		t.Errorf("expected 403 or 404 when accessing document from wrong project, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	rl.Printf("cross-project document access correctly blocked")
}

func TestTenantIsolation_CannotDeleteOtherProjectDocument(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)
	project2ID, _ := setupProjectLogged(t, rl)

	doc1ID := createDocument(t, project1ID, "to-delete.txt", "Content to delete")

	// Try to DELETE from project 2 context
	resp := doAPILogged(t, rl, "DELETE", "/api/documents/"+doc1ID, e2eTestToken(), project2ID, nil)
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusNotFound:
		// expected
	default:
		t.Errorf("expected 403 or 404 when deleting document from wrong project, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Document still exists — verify via correct project context
	resp2 := doAPILogged(t, rl, "GET", "/api/documents/"+doc1ID, e2eTestToken(), project1ID, nil)
	mustStatus(t, resp2, http.StatusOK)
	rl.Printf("cross-project document deletion correctly blocked")
}

func TestTenantIsolation_OwnerCanAccessAndDelete(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)

	doc1ID := createDocument(t, project1ID, "owner-doc.txt", "Owner content")

	// Owner can GET
	resp := doAPILogged(t, rl, "GET", "/api/documents/"+doc1ID, e2eTestToken(), project1ID, nil)
	mustStatus(t, resp, http.StatusOK)

	// Owner can DELETE
	resp = doAPILogged(t, rl, "DELETE", "/api/documents/"+doc1ID, e2eTestToken(), project1ID, nil)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		// expected
	default:
		t.Errorf("owner DELETE should succeed, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	rl.Printf("owner can access and delete own document")
}

// =============================================================================
// Project-Level Document Isolation (same org, different projects)
// =============================================================================

func TestTenantIsolation_DocumentsListScopedByProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Two projects in the same org
	orgID := createOrg(t, uniqueName("e2e-isolation-org"))
	project1ID := createProject(t, orgID, uniqueName("e2e-isolation-proj-A"))
	project1BID := createProject(t, orgID, uniqueName("e2e-isolation-proj-B"))

	doc1A_ID := createDocument(t, project1ID, "only-in-A.txt", "Content in A")
	doc1B_ID := createDocument(t, project1BID, "only-in-B.txt", "Content in B")

	// List from project A
	resp := doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), project1ID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var resultA struct {
		Documents []map[string]any `json:"documents"`
	}
	parseBodyJSON(t, body, &resultA)

	docAFound, docBFound := false, false
	for _, doc := range resultA.Documents {
		if doc["id"] == doc1A_ID {
			docAFound = true
		}
		if doc["id"] == doc1B_ID {
			docBFound = true
		}
	}
	if !docAFound {
		t.Error("document from project A should be visible in project A context")
	}
	if docBFound {
		t.Error("document from project B should NOT be visible in project A context")
	}

	// List from project B
	resp = doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), project1BID, nil)
	body = mustStatus(t, resp, http.StatusOK)
	var resultB struct {
		Documents []map[string]any `json:"documents"`
	}
	parseBodyJSON(t, body, &resultB)

	docAFound, docBFound = false, false
	for _, doc := range resultB.Documents {
		if doc["id"] == doc1A_ID {
			docAFound = true
		}
		if doc["id"] == doc1B_ID {
			docBFound = true
		}
	}
	if docAFound {
		t.Error("document from project A should NOT be visible in project B context")
	}
	if !docBFound {
		t.Error("document from project B should be visible in project B context")
	}
	rl.Printf("same-org cross-project isolation verified")
}

// =============================================================================
// Cross-Project Document Isolation
// =============================================================================

func TestTenantIsolation_CrossProject_PreventAccessFromAnotherProject(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)
	project2ID, _ := setupProjectLogged(t, rl)

	docID := createDocument(t, project1ID, "cross-project.txt", "Secret data")

	// GET from project 2 — should fail
	resp := doAPILogged(t, rl, "GET", "/api/documents/"+docID, e2eTestToken(), project2ID, nil)
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusNotFound:
		// ok
	default:
		t.Errorf("GET document from wrong project: expected 403 or 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// DELETE from project 2 — should fail
	resp = doAPILogged(t, rl, "DELETE", "/api/documents/"+docID, e2eTestToken(), project2ID, nil)
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusNotFound:
		// ok
	default:
		t.Errorf("DELETE document from wrong project: expected 403 or 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	rl.Printf("cross-project access correctly blocked")
}

func TestTenantIsolation_CrossProject_RLSFiltersDocumentList(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)
	project2ID, _ := setupProjectLogged(t, rl)

	createDocument(t, project1ID, "doc-rls-1.txt", "Project 1 content")
	createDocument(t, project2ID, "doc-rls-2.txt", "Project 2 content")

	// List with project 1 context — all docs should belong to project 1
	resp := doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), project1ID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result1 struct {
		Documents []map[string]any `json:"documents"`
	}
	parseBodyJSON(t, body, &result1)
	for _, doc := range result1.Documents {
		if doc["projectId"] != project1ID {
			t.Errorf("project 1 list contains doc with projectId=%v, expected %s", doc["projectId"], project1ID)
		}
	}

	// List with project 2 context — all docs should belong to project 2
	resp = doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), project2ID, nil)
	body = mustStatus(t, resp, http.StatusOK)
	var result2 struct {
		Documents []map[string]any `json:"documents"`
	}
	parseBodyJSON(t, body, &result2)
	for _, doc := range result2.Documents {
		if doc["projectId"] != project2ID {
			t.Errorf("project 2 list contains doc with projectId=%v, expected %s", doc["projectId"], project2ID)
		}
	}

	// Verify no overlap in IDs
	ids1 := make(map[string]bool)
	for _, doc := range result1.Documents {
		ids1[fmt.Sprintf("%v", doc["id"])] = true
	}
	for _, doc := range result2.Documents {
		if ids1[fmt.Sprintf("%v", doc["id"])] {
			t.Error("project 2 documents include a document from project 1")
		}
	}
	rl.Printf("RLS document list filtering verified")
}

// =============================================================================
// Cross-Project Chunk Isolation
// =============================================================================

func TestTenantIsolation_Chunks_FilteredByProject(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)
	project2ID, _ := setupProjectLogged(t, rl)

	doc1ID := createDocument(t, project1ID, "chunks-test-1.txt", "Content for project 1")
	doc2ID := createDocument(t, project2ID, "chunks-test-2.txt", "Content for project 2")

	// Project 1 chunks should not include project 2 doc
	resp := doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), project1ID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var chunks1 struct {
		Data []map[string]any `json:"data"`
	}
	parseBodyJSON(t, body, &chunks1)
	for _, chunk := range chunks1.Data {
		if chunk["documentId"] == doc2ID {
			t.Error("project 1 chunks should not include project 2 documents")
		}
	}

	// Project 2 chunks should not include project 1 doc
	resp = doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), project2ID, nil)
	body = mustStatus(t, resp, http.StatusOK)
	var chunks2 struct {
		Data []map[string]any `json:"data"`
	}
	parseBodyJSON(t, body, &chunks2)
	for _, chunk := range chunks2.Data {
		if chunk["documentId"] == doc1ID {
			t.Error("project 2 chunks should not include project 1 documents")
		}
	}
	rl.Printf("chunk cross-project isolation verified")
}

func TestTenantIsolation_Chunks_CannotAccessWithWrongProjectContext(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)
	project2ID, _ := setupProjectLogged(t, rl)

	doc1ID := createDocument(t, project1ID, "private-chunks.txt", "Private content")

	// Filter by project1 doc from project2 context — should return empty
	resp := doAPILogged(t, rl, "GET", "/api/chunks?documentId="+doc1ID, e2eTestToken(), project2ID, nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result struct {
		Data []map[string]any `json:"data"`
	}
	parseBodyJSON(t, body, &result)
	if len(result.Data) != 0 {
		t.Errorf("expected 0 chunks when querying wrong project context, got %d", len(result.Data))
	}
	rl.Printf("chunk access with wrong project context correctly returns empty")
}

// =============================================================================
// Isolation Matrix
// =============================================================================

func TestTenantIsolation_IsolationMatrix(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	project1ID, _ := setupProjectLogged(t, rl)
	project2ID, _ := setupProjectLogged(t, rl)

	doc1ID := createDocument(t, project1ID, "matrix-doc-1.txt", "Matrix test content 1")
	doc2ID := createDocument(t, project2ID, "matrix-doc-2.txt", "Matrix test content 2")

	tests := []struct {
		name           string
		method         string
		path           string
		projectID      string
		expectedStatus []int
		description    string
	}{
		{
			name:           "GET own document",
			method:         "GET",
			path:           "/api/documents/" + doc1ID,
			projectID:      project1ID,
			expectedStatus: []int{http.StatusOK},
			description:    "Should access own document",
		},
		{
			name:           "GET other project document",
			method:         "GET",
			path:           "/api/documents/" + doc2ID,
			projectID:      project1ID,
			expectedStatus: []int{http.StatusForbidden, http.StatusNotFound},
			description:    "Should NOT access other project's document",
		},
		{
			name:           "DELETE other project document",
			method:         "DELETE",
			path:           "/api/documents/" + doc2ID,
			projectID:      project1ID,
			expectedStatus: []int{http.StatusForbidden, http.StatusNotFound},
			description:    "Should NOT delete other project's document",
		},
		{
			name:           "List documents with project context",
			method:         "GET",
			path:           "/api/documents",
			projectID:      project1ID,
			expectedStatus: []int{http.StatusOK},
			description:    "Should list only project's documents",
		},
		{
			name:           "List chunks with project context",
			method:         "GET",
			path:           "/api/chunks",
			projectID:      project1ID,
			expectedStatus: []int{http.StatusOK},
			description:    "Should list only project's chunks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doAPILogged(t, rl, tt.method, tt.path, e2eTestToken(), tt.projectID, nil)
			body := readRespBody(resp)
			found := false
			for _, want := range tt.expectedStatus {
				if resp.StatusCode == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: expected one of %v, got %d\nbody: %s", tt.description, tt.expectedStatus, resp.StatusCode, body)
			}
		})
	}
	rl.Printf("isolation matrix tests passed")
}
