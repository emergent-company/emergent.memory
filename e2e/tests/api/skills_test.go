// Package api_test — skills_test.go
package api_test

import (
	"net/http"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func createGlobalSkill(t *testing.T, name, description, content string) map[string]any {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "POST", "/api/skills", token, "", jsonBody(map[string]any{
		"name":        name,
		"description": description,
		"content":     content,
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if id, ok := result["id"].(string); ok && id != "" {
		t.Cleanup(func() {
			doAPI(t, "DELETE", "/api/skills/"+id, token, "", nil)
		})
	}
	return result
}

func createProjectSkillHelper(t *testing.T, projectID, name, description, content string) map[string]any {
	t.Helper()
	token := e2eTestToken()
	resp := doAPI(t, "POST", "/api/projects/"+projectID+"/skills", token, "", jsonBody(map[string]any{
		"name":        name,
		"description": description,
		"content":     content,
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Auth tests
// ─────────────────────────────────────────────────────────────────────────────

func TestSkills_ListGlobal_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/skills", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSkills_CreateGlobal_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/skills", "", "", jsonBody(map[string]any{
		"name":        "no-auth-skill",
		"description": "should fail",
		"content":     "content",
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ─────────────────────────────────────────────────────────────────────────────
// List global skills
// ─────────────────────────────────────────────────────────────────────────────

func TestSkills_ListGlobal_ReturnsArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/skills", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if _, ok := result["skills"].([]any); !ok {
		t.Error("response should have 'skills' array")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Create global skill
// ─────────────────────────────────────────────────────────────────────────────

func TestSkills_CreateGlobal_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	skill := createGlobalSkill(t, uniqueName("test-create-skill"), "A test skill", "# Test\nDo the thing.")
	if skill["scope"] != "global" {
		t.Errorf("expected scope 'global', got %v", skill["scope"])
	}
	if skill["id"] == "" {
		t.Error("expected non-empty ID")
	}
}

func TestSkills_CreateGlobal_InvalidName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	resp := doAPILogged(t, rl, "POST", "/api/skills", e2eTestToken(), "", jsonBody(map[string]any{
		"name":        "Invalid Name With Spaces",
		"description": "bad name",
		"content":     "content",
	}))
	mustStatus(t, resp, http.StatusUnprocessableEntity)
}

func TestSkills_CreateGlobal_DuplicateName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	name := uniqueName("duplicate-skill")
	createGlobalSkill(t, name, "First", "content")

	resp := doAPILogged(t, rl, "POST", "/api/skills", e2eTestToken(), "", jsonBody(map[string]any{
		"name":        name,
		"description": "Second",
		"content":     "content",
	}))
	mustStatus(t, resp, http.StatusConflict)
}

// ─────────────────────────────────────────────────────────────────────────────
// Get skill
// ─────────────────────────────────────────────────────────────────────────────

func TestSkills_GetSkill_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	created := createGlobalSkill(t, uniqueName("get-skill-test"), "Get me", "content body")
	id := created["id"].(string)

	resp := doAPILogged(t, rl, "GET", "/api/skills/"+id, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["content"] != "content body" {
		t.Errorf("expected content 'content body', got %v", result["content"])
	}
}

func TestSkills_GetSkill_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/skills/00000000-0000-0000-0000-000000000000", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Update skill
// ─────────────────────────────────────────────────────────────────────────────

func TestSkills_UpdateSkill_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	created := createGlobalSkill(t, uniqueName("update-skill-test"), "Original desc", "original content")
	id := created["id"].(string)

	resp := doAPILogged(t, rl, "PATCH", "/api/skills/"+id, e2eTestToken(), "", jsonBody(map[string]any{
		"description": "Updated desc",
		"content":     "updated content",
	}))
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["description"] != "Updated desc" {
		t.Errorf("expected description 'Updated desc', got %v", result["description"])
	}
	if result["content"] != "updated content" {
		t.Errorf("expected content 'updated content', got %v", result["content"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete skill
// ─────────────────────────────────────────────────────────────────────────────

func TestSkills_DeleteSkill_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	token := e2eTestToken()
	resp := doAPILogged(t, rl, "POST", "/api/skills", token, "", jsonBody(map[string]any{
		"name":        uniqueName("delete-skill-test"),
		"description": "To be deleted",
		"content":     "bye",
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, body, &created)
	id := created["id"].(string)

	delResp := doAPILogged(t, rl, "DELETE", "/api/skills/"+id, token, "", nil)
	mustStatus(t, delResp, http.StatusNoContent)

	getResp := doAPILogged(t, rl, "GET", "/api/skills/"+id, token, "", nil)
	mustStatus(t, getResp, http.StatusNotFound)
}

// ─────────────────────────────────────────────────────────────────────────────
// Project-scoped skills
// ─────────────────────────────────────────────────────────────────────────────

func TestSkills_ListProjectSkills_IncludesGlobalSkills(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	globalName := uniqueName("global-skill-for-project-test")
	projName := uniqueName("project-skill-test")

	createGlobalSkill(t, globalName, "Global", "global content")
	createProjectSkillHelper(t, projectID, projName, "Project", "project content")

	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/skills", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data := result["skills"].([]any)

	names := make(map[string]bool)
	for _, item := range data {
		skill := item.(map[string]any)
		names[skill["name"].(string)] = true
	}
	if !names[globalName] {
		t.Errorf("expected global skill %q in list", globalName)
	}
	if !names[projName] {
		t.Errorf("expected project skill %q in list", projName)
	}
}

func TestSkills_ListProjectSkills_ProjectSkillOverridesGlobal(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	skillName := uniqueName("override-skill")

	createGlobalSkill(t, skillName, "Global version", "global content")
	createProjectSkillHelper(t, projectID, skillName, "Project version", "project content")

	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/skills", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	data := result["skills"].([]any)

	var found map[string]any
	for _, item := range data {
		skill := item.(map[string]any)
		if skill["name"].(string) == skillName {
			found = skill
			break
		}
	}
	if found == nil {
		t.Fatalf("override skill %q not found in list", skillName)
	}
	if found["scope"] != "project" {
		t.Errorf("expected scope 'project', got %v", found["scope"])
	}
	if found["description"] != "Project version" {
		t.Errorf("expected description 'Project version', got %v", found["description"])
	}
}
