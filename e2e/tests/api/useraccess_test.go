// Package api_test — useraccess_test.go
//
// Tests for user access (orgs and projects tree) API endpoints.
// Ported from emergent.memory/apps/server/tests/e2e/useraccess_test.go
package api_test

import (
	"net/http"
	"testing"
)

// TestGetOrgsAndProjects_RequiresAuth verifies the user access endpoint requires auth.
func TestGetOrgsAndProjects_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user/orgs-and-projects", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// TestGetOrgsAndProjects_ReturnsOrgStructure verifies the response includes org fields.
func TestGetOrgsAndProjects_ReturnsOrgStructure(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user/orgs-and-projects", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result []map[string]any
	parseBodyJSON(t, body, &result)

	if len(result) < 1 {
		t.Fatal("expected at least one org in result")
	}

	// Find the org we just created
	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	if foundOrg == nil {
		t.Fatalf("org %s not found in result", orgID)
	}
	if foundOrg["name"] == "" || foundOrg["name"] == nil {
		t.Error("expected non-empty org name")
	}
	if foundOrg["role"] == "" || foundOrg["role"] == nil {
		t.Error("expected non-empty org role")
	}
	if _, ok := foundOrg["projects"]; !ok {
		t.Error("expected org to have projects field")
	}
	rl.Printf("org structure OK, orgID=%s", orgID)
}

// TestGetOrgsAndProjects_OrgHasProjects verifies org contains its projects.
func TestGetOrgsAndProjects_OrgHasProjects(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user/orgs-and-projects", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result []map[string]any
	parseBodyJSON(t, body, &result)

	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	if foundOrg == nil {
		t.Fatalf("org %s not found", orgID)
	}

	projects, ok := foundOrg["projects"].([]any)
	if !ok {
		t.Fatalf("projects field is not an array: %T", foundOrg["projects"])
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	project, ok := projects[0].(map[string]any)
	if !ok {
		t.Fatal("project entry is not a map")
	}
	if project["id"] != projectID {
		t.Errorf("expected project id %s, got %v", projectID, project["id"])
	}
	if project["orgId"] != orgID {
		t.Errorf("expected orgId %s, got %v", orgID, project["orgId"])
	}
	if project["role"] == "" || project["role"] == nil {
		t.Error("expected non-empty project role")
	}
	rl.Printf("project correctly nested in org")
}

// TestGetOrgsAndProjects_MultipleOrgs verifies multiple orgs are all returned.
func TestGetOrgsAndProjects_MultipleOrgs(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Create 3 separate orgs (each setupProject creates an org + project)
	_, orgID1 := setupProjectLogged(t, rl)
	_, orgID2 := setupProjectLogged(t, rl)
	_, orgID3 := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user/orgs-and-projects", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result []map[string]any
	parseBodyJSON(t, body, &result)

	found := 0
	for _, org := range result {
		id, _ := org["id"].(string)
		if id == orgID1 || id == orgID2 || id == orgID3 {
			found++
		}
	}
	if found < 3 {
		t.Errorf("expected to find all 3 orgs, found %d", found)
	}
	rl.Printf("found %d/3 created orgs in result", found)
}

// TestGetOrgsAndProjects_MultipleProjects verifies org returns all its projects.
func TestGetOrgsAndProjects_MultipleProjects(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl) // creates first project
	// Create 2 more projects in the same org
	createProject(t, orgID, uniqueName("proj-b"))
	createProject(t, orgID, uniqueName("proj-c"))

	resp := doAPILogged(t, rl, "GET", "/api/user/orgs-and-projects", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result []map[string]any
	parseBodyJSON(t, body, &result)

	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	if foundOrg == nil {
		t.Fatalf("org %s not found", orgID)
	}

	projects, ok := foundOrg["projects"].([]any)
	if !ok {
		t.Fatalf("projects field is not an array")
	}
	if len(projects) != 3 {
		t.Errorf("expected 3 projects, got %d", len(projects))
	}
	rl.Printf("all 3 projects returned for org")
}

// TestGetOrgsAndProjects_RoleIsIncluded verifies org role is present in the response.
func TestGetOrgsAndProjects_RoleIsIncluded(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user/orgs-and-projects", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result []map[string]any
	parseBodyJSON(t, body, &result)

	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	if foundOrg == nil {
		t.Fatalf("org %s not found", orgID)
	}
	if foundOrg["role"] == "" || foundOrg["role"] == nil {
		t.Error("expected org role to be non-empty")
	}
	rl.Printf("org role=%v", foundOrg["role"])
}

// TestGetOrgsAndProjects_ProjectRoleIsIncluded verifies project role is present.
func TestGetOrgsAndProjects_ProjectRoleIsIncluded(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user/orgs-and-projects", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result []map[string]any
	parseBodyJSON(t, body, &result)

	var foundOrg map[string]any
	for _, org := range result {
		if org["id"] == orgID {
			foundOrg = org
			break
		}
	}
	if foundOrg == nil {
		t.Fatalf("org %s not found", orgID)
	}

	projects, _ := foundOrg["projects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	project, ok := projects[0].(map[string]any)
	if !ok {
		t.Fatal("project entry is not a map")
	}
	if project["id"] != projectID {
		t.Errorf("expected project id %s, got %v", projectID, project["id"])
	}
	if project["role"] == "" || project["role"] == nil {
		t.Error("expected non-empty project role")
	}
	rl.Printf("project role=%v", project["role"])
}
