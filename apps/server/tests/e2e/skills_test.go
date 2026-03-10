package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// SkillsTestSuite tests the skills API endpoints.
type SkillsTestSuite struct {
	testutil.BaseSuite
}

func TestSkillsSuite(t *testing.T) {
	suite.Run(t, new(SkillsTestSuite))
}

func (s *SkillsTestSuite) SetupSuite() {
	s.SetDBSuffix("skills")
	s.BaseSuite.SetupSuite()
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

func (s *SkillsTestSuite) createSkill(name, description, content string) map[string]any {
	resp := s.Client.POST("/api/skills",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":        name,
			"description": description,
			"content":     content,
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "createSkill: unexpected status for %q: %s", name, string(resp.Body))
	var result map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	return result
}

func (s *SkillsTestSuite) createProjectSkill(projectID, name, description, content string) map[string]any {
	resp := s.Client.POST("/api/projects/"+projectID+"/skills",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":        name,
			"description": description,
			"content":     content,
		}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "createProjectSkill: unexpected status for %q: %s", name, string(resp.Body))
	var result map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	return result
}

// ─────────────────────────────────────────────
// Auth tests
// ─────────────────────────────────────────────

func (s *SkillsTestSuite) TestListGlobalSkills_RequiresAuth() {
	resp := s.Client.GET("/api/skills")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *SkillsTestSuite) TestCreateGlobalSkill_RequiresAuth() {
	resp := s.Client.POST("/api/skills",
		testutil.WithJSONBody(map[string]any{
			"name":        "no-auth-skill",
			"description": "should fail",
			"content":     "content",
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// ─────────────────────────────────────────────
// List global skills
// ─────────────────────────────────────────────

func (s *SkillsTestSuite) TestListGlobalSkills_ReturnsEmptyInitially() {
	resp := s.Client.GET("/api/skills",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	data, ok := result["skills"].([]any)
	s.True(ok, "response should have 'skills' array")
	s.NotNil(data)
}

// ─────────────────────────────────────────────
// Create global skill
// ─────────────────────────────────────────────

func (s *SkillsTestSuite) TestCreateGlobalSkill_Success() {
	skill := s.createSkill("test-create-skill", "A test skill", "# Test\nDo the thing.")

	s.Equal("test-create-skill", skill["name"])
	s.Equal("A test skill", skill["description"])
	s.Equal("# Test\nDo the thing.", skill["content"])
	s.Equal("global", skill["scope"])
	s.NotEmpty(skill["id"])
}

func (s *SkillsTestSuite) TestCreateGlobalSkill_InvalidName() {
	resp := s.Client.POST("/api/skills",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":        "Invalid Name With Spaces",
			"description": "bad name",
			"content":     "content",
		}),
	)
	s.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
}

func (s *SkillsTestSuite) TestCreateGlobalSkill_DuplicateName() {
	s.createSkill("duplicate-skill", "First", "content")

	resp := s.Client.POST("/api/skills",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"name":        "duplicate-skill",
			"description": "Second",
			"content":     "content",
		}),
	)
	s.Equal(http.StatusConflict, resp.StatusCode)
}

// ─────────────────────────────────────────────
// Get skill
// ─────────────────────────────────────────────

func (s *SkillsTestSuite) TestGetSkill_Success() {
	created := s.createSkill("get-skill-test", "Get me", "content body")
	id := created["id"].(string)

	resp := s.Client.GET("/api/skills/"+id,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	s.Equal("get-skill-test", result["name"])
	s.Equal("content body", result["content"])
}

func (s *SkillsTestSuite) TestGetSkill_NotFound() {
	resp := s.Client.GET("/api/skills/00000000-0000-0000-0000-000000000000",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// ─────────────────────────────────────────────
// Update skill
// ─────────────────────────────────────────────

func (s *SkillsTestSuite) TestUpdateSkill_Success() {
	created := s.createSkill("update-skill-test", "Original desc", "original content")
	id := created["id"].(string)

	resp := s.Client.PATCH("/api/skills/"+id,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"description": "Updated desc",
			"content":     "updated content",
		}),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	s.Equal("Updated desc", result["description"])
	s.Equal("updated content", result["content"])
	// name must not change
	s.Equal("update-skill-test", result["name"])
}

// ─────────────────────────────────────────────
// Delete skill
// ─────────────────────────────────────────────

func (s *SkillsTestSuite) TestDeleteSkill_Success() {
	created := s.createSkill("delete-skill-test", "To be deleted", "bye")
	id := created["id"].(string)

	resp := s.Client.DELETE("/api/skills/"+id,
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusNoContent, resp.StatusCode)

	// Confirm it's gone
	resp = s.Client.GET("/api/skills/"+id, testutil.WithAuth("e2e-test-user"))
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// ─────────────────────────────────────────────
// Project-scoped skills
// ─────────────────────────────────────────────

func (s *SkillsTestSuite) TestListProjectSkills_IncludesGlobalSkills() {
	s.createSkill("global-skill-for-project-test", "Global", "global content")
	s.createProjectSkill(s.ProjectID, "project-skill-test", "Project", "project content")

	resp := s.Client.GET("/api/projects/"+s.ProjectID+"/skills",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	data := result["skills"].([]any)

	names := make(map[string]bool)
	for _, item := range data {
		skill := item.(map[string]any)
		names[skill["name"].(string)] = true
	}
	s.True(names["global-skill-for-project-test"], "should include global skill")
	s.True(names["project-skill-test"], "should include project skill")
}

func (s *SkillsTestSuite) TestListProjectSkills_ProjectSkillOverridesGlobal() {
	// Create a global skill
	s.createSkill("override-skill", "Global version", "global content")
	// Create a project skill with the same name
	s.createProjectSkill(s.ProjectID, "override-skill", "Project version", "project content")

	resp := s.Client.GET("/api/projects/"+s.ProjectID+"/skills",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	data := result["skills"].([]any)

	var found map[string]any
	for _, item := range data {
		skill := item.(map[string]any)
		if skill["name"].(string) == "override-skill" {
			found = skill
			break
		}
	}
	s.Require().NotNil(found, "override-skill should be in the list")
	// Project skill should win over global
	s.Equal("project", found["scope"], "project skill should override global")
	s.Equal("Project version", found["description"])
}
