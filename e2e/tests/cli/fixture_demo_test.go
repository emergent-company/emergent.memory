// Package cli_test — fixture_demo_test.go
//
// Demo migrations showing the same tests rewritten with framework.Use.
// Compare with projects_test.go (TestCLIInstalled_ProjectCreateGetDelete) and
// documents_test.go (TestCLIInstalled_DocumentUploadGetDelete) to see the
// boilerplate reduction.
//
// Before (7-8 lines of ritual per test):
//
//	rl := newRunLog(t); t.Cleanup(rl.Close)
//	home := t.TempDir()
//	requireServerReady(t, home)
//	srv := serverURL()
//	name := uniqueProjectName("e2e-foo")
//	projectID := createProject(t, home, srv, name)
//	deleteProjectOnCleanup(t, home, projectID)
//
// After (1 line):
//
//	fx := framework.Use(t, framework.WithProject("e2e-foo"))
package cli_test

import (
	"strings"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestCLIInstalled_Fixture_ProjectGetDelete demonstrates Use + WithProject.
// Equivalent to TestCLIInstalled_ProjectCreateGetDelete in projects_test.go,
// but with ~70% less boilerplate.
func TestCLIInstalled_Fixture_ProjectGetDelete(t *testing.T) {
	fx := framework.Use(t, framework.WithProject("e2e-proj-fixture"))

	fx.RunLog.Describe("Verify project get → delete via Fixture API",
		"Use(t, WithProject) creates the project and registers cleanup",
		"CLI calls need no --project flag — config file carries the project_id",
	)

	// Get the project. No --project flag needed.
	fx.Section("Get project")
	getOut := fx.CLI("projects", "get", fx.ProjectID)
	getOut.Contains(fx.ProjectID)

	// Delete the project explicitly (cleanup would do it too, but this tests the CLI).
	fx.Section("Delete project")
	delOut := fx.CLI("projects", "delete", fx.ProjectID)
	lower := strings.ToLower(delOut.Output())
	if !strings.Contains(lower, "delete") && !strings.Contains(lower, "removed") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", framework.Truncate(delOut.Output(), 300))
	}
}

// TestCLIInstalled_Fixture_DocumentUploadGetDelete demonstrates Use + WithProject
// + TempFile for the full document lifecycle.
// Equivalent to TestCLIInstalled_DocumentUploadGetDelete in documents_test.go.
func TestCLIInstalled_Fixture_DocumentUploadGetDelete(t *testing.T) {
	fx := framework.Use(t, framework.WithProject("e2e-docs-fixture"))

	fx.RunLog.Describe("Verify document upload → get → delete via Fixture API",
		"Use(t, WithProject) creates project; TempFile writes the upload file",
		"CLI calls need no --project flag — config file carries the project_id",
	)

	// Write a temp file to upload.
	fx.Section("Create temp file")
	content := "This is a test document created by the e2e fixture demo.\nMultiple lines.\n"
	filePath := fx.TempFile("fixture-document.txt", content)
	fx.Log("wrote %d bytes to fixture-document.txt", len(content))

	// Upload the document.
	fx.Section("Upload document")
	uploadOut := fx.CLI("documents", "upload", filePath, "--output", "json")

	docID := parseNestedDocumentID(uploadOut.Output())
	if docID == "" {
		t.Fatalf("could not parse document ID from upload output: %s", framework.Truncate(uploadOut.Output(), 300))
	}
	fx.Log("uploaded document ID: %s", docID)

	// Get by ID.
	fx.Section("Get document")
	getOut := fx.CLI("documents", "get", docID, "--output", "json")
	if !strings.Contains(getOut.Output(), "fixture-document.txt") {
		t.Errorf("expected get output to contain filename, got:\n%s", framework.Truncate(getOut.Output(), 500))
	}

	// Delete.
	fx.Section("Delete document")
	delOut := fx.CLI("documents", "delete", docID)
	lower := strings.ToLower(delOut.Output())
	if !strings.Contains(lower, "delete") && !strings.Contains(lower, "removed") {
		t.Errorf("expected delete output to confirm deletion, got:\n%s", framework.Truncate(delOut.Output(), 300))
	}
	fx.Log("document delete confirmed")
}
