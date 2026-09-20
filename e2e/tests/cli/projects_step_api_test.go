// Package cli_test — projects_step_api_test.go
//
// Sample test using the structured step API (TestContext + Step + CLIResult).
// This rewrites TestCLIInstalled_ProjectCreateGetDelete using the new API
// to demonstrate the reduction in boilerplate.
package cli_test

import (
	"testing"
)

// TestCLIInstalled_ProjectCreateGetDelete_StepAPI demonstrates the structured
// step API by testing the full project lifecycle: create → get → delete.
//
// Compare with TestCLIInstalled_ProjectCreateGetDelete in projects_test.go
// which performs the same operations with manual RunLog bookkeeping.
func TestCLIInstalled_ProjectCreateGetDelete_StepAPI(t *testing.T) {
	tc := newTest(t, testOpts{
		Describe: "Verify project create → get → delete lifecycle (step API)",
		Bullets: []string{
			"Create a new project with a unique name",
			"Get the project by ID and verify name appears",
			"Delete the project and verify delete confirmed",
		},
		Tags: []string{"api:step"},
	})
	defer tc.Done()

	name := uniqueProjectName("e2e-step-crud")
	var projectID string

	tc.Step("Create project", func(s *step) {
		s.CLI("projects", "create", "--name", name).
			Contains(name).
			ParseID(&projectID)
		s.Log("project: %s (%s)", name, projectID)

		// Set project_id in config so subsequent commands find it.
		s.CLI("config", "set", "project_id", projectID)
	})

	tc.Step("Get project by ID", func(s *step) {
		s.CLI("projects", "get", projectID).
			Contains(name).
			Contains(projectID)
	})

	tc.Step("Delete project", func(s *step) {
		s.CLI("projects", "delete", projectID).
			ContainsAny("delet", "remov", "success")
		s.Log("project deleted successfully")
	})
}
