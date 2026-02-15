package projects_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func TestProjectsList(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	// Mock response
	fixtureProjects := testutil.FixtureProjects()
	mock.OnJSON("GET", "/api/projects", http.StatusOK, fixtureProjects)

	// Create client
	client, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth: sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: "test_key",
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test List
	result, err := client.Projects.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result) != len(fixtureProjects) {
		t.Errorf("expected %d projects, got %d", len(fixtureProjects), len(result))
	}

	if result[0].ID != fixtureProjects[0].ID {
		t.Errorf("expected project ID %s, got %s", fixtureProjects[0].ID, result[0].ID)
	}
}

func TestProjectsListWithOptions(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/projects", func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameters
		if limit := r.URL.Query().Get("limit"); limit != "50" {
			t.Errorf("expected limit=50, got %s", limit)
		}
		if orgID := r.URL.Query().Get("orgId"); orgID != "org_test" {
			t.Errorf("expected orgId=org_test, got %s", orgID)
		}

		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Projects.List(context.Background(), &projects.ListOptions{
		Limit: 50,
		OrgID: "org_test",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestProjectsGet(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureProject := testutil.FixtureProject()
	mock.OnJSON("GET", "/api/projects/proj_test123", http.StatusOK, fixtureProject)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Projects.Get(context.Background(), "proj_test123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if result.ID != fixtureProject.ID {
		t.Errorf("expected project ID %s, got %s", fixtureProject.ID, result.ID)
	}
	if result.Name != fixtureProject.Name {
		t.Errorf("expected project name %s, got %s", fixtureProject.Name, result.Name)
	}
}

func TestProjectsCreate(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureProject := testutil.FixtureProject()
	mock.OnJSON("POST", "/api/projects", http.StatusCreated, fixtureProject)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	req := &projects.CreateProjectRequest{
		Name:  "Test Project",
		OrgID: "org_test456",
	}

	result, err := client.Projects.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.Name != fixtureProject.Name {
		t.Errorf("expected project name %s, got %s", fixtureProject.Name, result.Name)
	}
}

func TestProjectsUpdate(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureProject := testutil.FixtureProject()
	updatedName := "Updated Project Name"
	fixtureProject.Name = updatedName

	mock.OnJSON("PATCH", "/api/projects/proj_test123", http.StatusOK, fixtureProject)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	req := &projects.UpdateProjectRequest{
		Name: &updatedName,
	}

	result, err := client.Projects.Update(context.Background(), "proj_test123", req)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if result.Name != updatedName {
		t.Errorf("expected updated name %s, got %s", updatedName, result.Name)
	}
}

func TestProjectsDelete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("DELETE", "/api/projects/proj_test123", http.StatusOK, map[string]string{"status": "deleted"})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	err := client.Projects.Delete(context.Background(), "proj_test123")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestProjectsListMembers(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureMember := testutil.FixtureProjectMember()
	members := []projects.ProjectMember{*fixtureMember}
	mock.OnJSON("GET", "/api/projects/proj_test123/members", http.StatusOK, members)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Projects.ListMembers(context.Background(), "proj_test123")
	if err != nil {
		t.Fatalf("ListMembers() error = %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 member, got %d", len(result))
	}

	if result[0].Email != fixtureMember.Email {
		t.Errorf("expected member email %s, got %s", fixtureMember.Email, result[0].Email)
	}
}

func TestProjectsRemoveMember(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/projects/proj_123/members/user_456", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		w.WriteHeader(http.StatusNoContent)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	err := client.Projects.RemoveMember(context.Background(), "proj_123", "user_456")
	if err != nil {
		t.Fatalf("RemoveMember() error = %v", err)
	}
}

func TestProjectsGetNotFound(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/projects/invalid", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Project not found"}}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Projects.Get(context.Background(), "invalid")
	if err == nil {
		t.Fatal("expected error for not found project")
	}
}

func TestProjectsCreateValidationError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"Name is required"}}`))
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Projects.Create(context.Background(), &projects.CreateProjectRequest{
		Name: "",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
