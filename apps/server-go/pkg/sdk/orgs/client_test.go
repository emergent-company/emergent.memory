package orgs_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/orgs"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func TestOrgsList(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureOrgs := testutil.FixtureOrganizations()
	mock.OnJSON("GET", "/api/orgs", http.StatusOK, fixtureOrgs)

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

	result, err := client.Orgs.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result) != len(fixtureOrgs) {
		t.Errorf("expected %d orgs, got %d", len(fixtureOrgs), len(result))
	}

	if result[0].ID != fixtureOrgs[0].ID {
		t.Errorf("expected org ID %s, got %s", fixtureOrgs[0].ID, result[0].ID)
	}
}

func TestOrgsGet(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureOrg := testutil.FixtureOrganization()
	mock.OnJSON("GET", "/api/orgs/org_test456", http.StatusOK, fixtureOrg)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Orgs.Get(context.Background(), "org_test456")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if result.ID != fixtureOrg.ID {
		t.Errorf("expected org ID %s, got %s", fixtureOrg.ID, result.ID)
	}
	if result.Name != fixtureOrg.Name {
		t.Errorf("expected org name %s, got %s", fixtureOrg.Name, result.Name)
	}
}

func TestOrgsCreate(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixtureOrg := testutil.FixtureOrganization()
	mock.OnJSON("POST", "/api/orgs", http.StatusCreated, fixtureOrg)

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	req := &orgs.CreateOrganizationRequest{
		Name: "Test Organization",
	}

	result, err := client.Orgs.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.Name != fixtureOrg.Name {
		t.Errorf("expected org name %s, got %s", fixtureOrg.Name, result.Name)
	}
}

func TestOrgsDelete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("DELETE", "/api/orgs/org_test456", http.StatusOK, map[string]string{"status": "deleted"})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	err := client.Orgs.Delete(context.Background(), "org_test456")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}
