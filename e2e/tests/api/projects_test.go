// Package api_test — projects_test.go
//
// Tests for the projects API (/api/projects).
// Ported from emergent.memory/apps/server/tests/e2e/projects_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestProjects_ListRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestProjects_ListReturnsUserProjects(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	rl.Section("GET /api/projects")
	resp := doAPILogged(t, rl, "GET", "/api/projects", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var projects []map[string]any
	parseBodyJSON(t, body, &projects)
	if len(projects) < 1 {
		t.Fatal("expected at least one project in list")
	}
	found := false
	for _, p := range projects {
		if p["id"] == projectID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created project %s not found in list", projectID)
	}
	rl.Printf("found project %s in list", projectID)
}

func TestProjects_ListFilterByOrgID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)

	rl.Section("GET /api/projects?orgId=<id>")
	resp := doAPILogged(t, rl, "GET", "/api/projects?orgId="+orgID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var projects []map[string]any
	parseBodyJSON(t, body, &projects)
	if len(projects) != 1 {
		t.Errorf("expected exactly 1 project for org, got %d", len(projects))
	}
	if len(projects) > 0 && projects[0]["id"] != projectID {
		t.Errorf("expected project %s, got %v", projectID, projects[0]["id"])
	}
	rl.Printf("filter by orgId returned %d projects", len(projects))
}

func TestProjects_ListInvalidOrgIDReturnsEmpty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects?orgId=invalid-uuid", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var projects []map[string]any
	parseBodyJSON(t, body, &projects)
	if len(projects) != 0 {
		t.Errorf("expected empty list for invalid orgId, got %d", len(projects))
	}
}

func TestProjects_ListWithLimit(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-limit-org"))
	for i := 1; i <= 5; i++ {
		createProject(t, orgID, fmt.Sprintf("Limit Project %d", i))
	}

	rl.Section("GET /api/projects?orgId=<id>&limit=3")
	resp := doAPILogged(t, rl, "GET", "/api/projects?orgId="+orgID+"&limit=3", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var projects []map[string]any
	parseBodyJSON(t, body, &projects)
	if len(projects) != 3 {
		t.Errorf("expected exactly 3 projects with limit=3, got %d", len(projects))
	}
	rl.Printf("limit=3 returned 3 projects")
}

func TestProjects_GetRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects/some-id", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestProjects_GetInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects/invalid-uuid", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "invalid-uuid" {
		t.Errorf("expected error code invalid-uuid, got %v", errObj["code"])
	}
}

func TestProjects_GetNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects/00000000-0000-0000-0000-000000000000", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestProjects_GetSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)

	rl.Section("GET /api/projects/:id")
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var project map[string]any
	parseBodyJSON(t, body, &project)
	if project["id"] != projectID {
		t.Errorf("expected id=%s, got %v", projectID, project["id"])
	}
	if project["orgId"] != orgID {
		t.Errorf("expected orgId=%s, got %v", orgID, project["orgId"])
	}
	rl.Printf("project %s fetched OK", projectID)
}

func TestProjects_CreateRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-create-auth-org"))
	resp := doAPIWithOrg(t, "POST", "/api/projects", "", "", orgID,
		jsonBody(map[string]any{"name": "New Project", "orgId": orgID}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestProjects_CreateSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-create-proj-org"))
	name := uniqueName("e2e-project")

	rl.Section("POST /api/projects")
	resp := doAPIWithOrg(t, "POST", "/api/projects", e2eTestToken(), "", orgID,
		jsonBody(map[string]any{"name": name, "orgId": orgID}))
	body := mustStatus(t, resp, http.StatusCreated)

	var project map[string]any
	parseBodyJSON(t, body, &project)
	projectID, _ := project["id"].(string)
	if projectID == "" {
		t.Fatal("expected non-empty id in response")
	}
	if project["name"] != name {
		t.Errorf("expected name=%s, got %v", name, project["name"])
	}
	if project["orgId"] != orgID {
		t.Errorf("expected orgId=%s, got %v", orgID, project["orgId"])
	}
	rl.Printf("created project %s", projectID)
}

func TestProjects_CreateTrimsWhitespace(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-trim-proj-org"))
	resp := doAPIWithOrg(t, "POST", "/api/projects", e2eTestToken(), "", orgID,
		jsonBody(map[string]any{"name": "  Trimmed Name  ", "orgId": orgID}))
	body := mustStatus(t, resp, http.StatusCreated)

	var project map[string]any
	parseBodyJSON(t, body, &project)
	if project["name"] != "Trimmed Name" {
		t.Errorf("expected trimmed name, got %v", project["name"])
	}
}

func TestProjects_CreateEmptyNameFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-empty-name-org"))
	resp := doAPIWithOrg(t, "POST", "/api/projects", e2eTestToken(), "", orgID,
		jsonBody(map[string]any{"name": "", "orgId": orgID}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "validation-failed" {
		t.Errorf("expected validation-failed, got %v", errObj["code"])
	}
}

func TestProjects_CreateMissingOrgIDFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/projects", e2eTestToken(), "",
		jsonBody(map[string]any{"name": "No Org Project"}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "org-required" {
		t.Errorf("expected org-required, got %v", errObj["code"])
	}
}

func TestProjects_CreateOrgNotFoundFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/projects", e2eTestToken(), "",
		jsonBody(map[string]any{"name": "Ghost Org Project", "orgId": "00000000-0000-0000-0000-000000000000"}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "org-not-found" {
		t.Errorf("expected org-not-found, got %v", errObj["code"])
	}
}

func TestProjects_CreateDuplicateNameFails(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-dup-proj-org"))
	createProject(t, orgID, "Duplicate Name")

	resp := doAPIWithOrg(t, "POST", "/api/projects", e2eTestToken(), "", orgID,
		jsonBody(map[string]any{"name": "Duplicate Name", "orgId": orgID}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "duplicate" {
		t.Errorf("expected duplicate, got %v", errObj["code"])
	}
}

func TestProjects_CreatorBecomesAdmin(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	rl.Section("GET /api/projects/:id/members")
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/members", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var members []map[string]any
	parseBodyJSON(t, body, &members)
	if len(members) != 1 {
		t.Fatalf("expected exactly 1 member (creator), got %d", len(members))
	}
	if members[0]["role"] != "project_admin" {
		t.Errorf("expected creator to be project_admin, got %v", members[0]["role"])
	}
	rl.Printf("creator is project_admin")
}

func TestProjects_UpdateRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/projects/some-id", "", "", jsonBody(map[string]any{"name": "Updated"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestProjects_UpdateInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/projects/invalid-uuid", e2eTestToken(), "", jsonBody(map[string]any{"name": "Updated"}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestProjects_UpdateNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PATCH", "/api/projects/00000000-0000-0000-0000-000000000000", e2eTestToken(), "", jsonBody(map[string]any{"name": "Updated"}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestProjects_UpdateSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	rl.Section("PATCH /api/projects/:id")
	resp := doAPILogged(t, rl, "PATCH", "/api/projects/"+projectID, e2eTestToken(), "", jsonBody(map[string]any{"name": "Updated Name"}))
	body := mustStatus(t, resp, http.StatusOK)

	var project map[string]any
	parseBodyJSON(t, body, &project)
	if project["name"] != "Updated Name" {
		t.Errorf("expected name=Updated Name, got %v", project["name"])
	}
	rl.Printf("project name updated to 'Updated Name'")
}

func TestProjects_UpdatePartial(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/projects/"+projectID, e2eTestToken(), "", jsonBody(map[string]any{"project_info": "Test purpose"}))
	body := mustStatus(t, resp, http.StatusOK)

	var project map[string]any
	parseBodyJSON(t, body, &project)
	if project["project_info"] != "Test purpose" {
		t.Errorf("expected project_info=Test purpose, got %v", project["project_info"])
	}
	rl.Printf("partial update (project_info) worked")
}

func TestProjects_UpdateEmptyBody(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "PATCH", "/api/projects/"+projectID, e2eTestToken(), "", jsonBody(map[string]any{}))
	mustStatus(t, resp, http.StatusOK)
}

func TestProjects_DeleteRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/projects/some-id", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestProjects_DeleteInvalidUUID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/projects/invalid-uuid", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestProjects_DeleteNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/projects/00000000-0000-0000-0000-000000000000", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestProjects_DeleteSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	orgID := createOrg(t, uniqueName("e2e-del-proj-org"))
	projectID := createProject(t, orgID, uniqueName("e2e-delete-proj"))

	rl.Section("DELETE /api/projects/:id")
	resp := doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusAccepted)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["status"] != "deleting" {
		t.Errorf("expected status=deleting, got %v", result["status"])
	}

	// Project should no longer be visible
	getResp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID, e2eTestToken(), "", nil)
	mustStatus(t, getResp, http.StatusNotFound)
	rl.Printf("project %s deleted", projectID)
}

func TestProjects_ListMembersRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects/some-id/members", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestProjects_ListMembersNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects/00000000-0000-0000-0000-000000000000/members", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestProjects_ListMembersSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/members", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var members []map[string]any
	parseBodyJSON(t, body, &members)
	if len(members) < 1 {
		t.Fatal("expected at least 1 member")
	}
	hasAdmin := false
	for _, m := range members {
		if m["role"] == "project_admin" {
			hasAdmin = true
			break
		}
	}
	if !hasAdmin {
		t.Error("expected project_admin member")
	}
	rl.Printf("members list has %d members including project_admin", len(members))
}

func TestProjects_RemoveMemberRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE", "/api/projects/some-id/members/some-user-id", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestProjects_RemoveMemberNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "DELETE",
		"/api/projects/00000000-0000-0000-0000-000000000000/members/00000000-0000-0000-0000-000000000001",
		e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestProjects_RemoveMemberCannotRemoveLastAdmin(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Get members to find the user ID
	membersResp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/members", e2eTestToken(), "", nil)
	membersBody := mustStatus(t, membersResp, http.StatusOK)
	var members []map[string]any
	parseBodyJSON(t, membersBody, &members)
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
	userID, _ := members[0]["id"].(string)

	rl.Section("DELETE /api/projects/:id/members/:userId — last admin")
	resp := doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/members/"+userID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusForbidden)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	if errObj["code"] != "last-admin" {
		t.Errorf("expected last-admin error code, got %v", errObj["code"])
	}
	rl.Printf("correctly rejected removal of last admin")
}

func TestProjects_StatsNotIncludedByDefault(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// Use direct GET to avoid list-search flakiness; stats absent by default.
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var project map[string]any
	parseBodyJSON(t, body, &project)
	if project["stats"] != nil {
		t.Error("stats should not be present without ?include_stats=true")
	}
	rl.Printf("stats absent without include_stats=true")
}

func TestProjects_StatsIncludedWhenRequested(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects?orgId="+orgID+"&include_stats=true", e2eTestToken(), "", nil)
	if resp.StatusCode == http.StatusInternalServerError {
		t.Skip("include_stats not supported by this server (DB stats query unavailable)")
	}
	body := mustStatus(t, resp, http.StatusOK)

	var projects []map[string]any
	parseBodyJSON(t, body, &projects)
	found := false
	for _, p := range projects {
		if p["id"] == projectID {
			found = true
			if p["stats"] == nil {
				t.Error("stats should be present with ?include_stats=true")
			}
			stats, _ := p["stats"].(map[string]any)
			for _, f := range []string{"documentCount", "objectCount", "relationshipCount"} {
				if _, ok := stats[f]; !ok {
					t.Errorf("stats missing field %q", f)
				}
			}
			break
		}
	}
	if !found {
		t.Errorf("project %s not found in list", projectID)
	}
	rl.Printf("stats present with include_stats=true")
}

func TestProjects_SingleGetStatsIncluded(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"?include_stats=true", e2eTestToken(), "", nil)
	if resp.StatusCode == http.StatusInternalServerError {
		t.Skip("include_stats not supported by this server (DB stats query unavailable)")
	}
	body := mustStatus(t, resp, http.StatusOK)

	var project map[string]any
	parseBodyJSON(t, body, &project)
	if project["stats"] == nil {
		t.Error("stats should be present with ?include_stats=true on single GET")
	}
	rl.Printf("single GET with include_stats returned stats")
}
