package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/provider"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

// --- helpers ---

func newClient(t *testing.T, mock *testutil.MockServer) *sdk.Client {
	t.Helper()
	c, err := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})
	if err != nil {
		t.Fatalf("failed to create SDK client: %v", err)
	}
	return c
}

func fixtureOrgCredential() provider.OrgCredential {
	return provider.OrgCredential{
		ID:        "cred_test123",
		OrgID:     "org_test456",
		Provider:  provider.ProviderGoogleAI,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func fixtureProjectPolicy() provider.ProjectPolicy {
	return provider.ProjectPolicy{
		ID:        "policy_test123",
		ProjectID: "proj_test123",
		Provider:  provider.ProviderGoogleAI,
		Policy:    provider.PolicyOrganization,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func fixtureModel() provider.SupportedModel {
	return provider.SupportedModel{
		ID:          "model_test123",
		Provider:    provider.ProviderGoogleAI,
		ModelName:   "gemini-2.0-flash",
		ModelType:   provider.ModelTypeGenerative,
		DisplayName: "Gemini 2.0 Flash",
		LastSynced:  time.Now(),
	}
}

func fixtureUsageSummary() provider.UsageSummary {
	return provider.UsageSummary{
		Note: "Showing usage for the last 30 days",
		Data: []provider.UsageSummaryRow{
			{
				Provider:         provider.ProviderGoogleAI,
				Model:            "gemini-2.0-flash",
				TotalText:        1_000_000,
				TotalOutput:      500_000,
				EstimatedCostUSD: 0.225,
			},
		},
	}
}

// --- Organization Credential Tests ---

func TestSaveGoogleAICredential(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/v1/organizations/org_test456/providers/google-ai/credentials",
		func(w http.ResponseWriter, r *http.Request) {
			testutil.AssertHeader(t, r, "Content-Type", "application/json")
			w.WriteHeader(http.StatusNoContent)
		})

	c := newClient(t, mock)
	err := c.Provider.SaveGoogleAICredential(context.Background(), "org_test456",
		&provider.SaveGoogleAICredentialRequest{APIKey: "AIza-test-key"})
	if err != nil {
		t.Fatalf("SaveGoogleAICredential() error = %v", err)
	}
}

func TestSaveVertexAICredential(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/v1/organizations/org_test456/providers/vertex-ai/credentials",
		func(w http.ResponseWriter, r *http.Request) {
			testutil.AssertHeader(t, r, "Content-Type", "application/json")
			w.WriteHeader(http.StatusNoContent)
		})

	c := newClient(t, mock)
	err := c.Provider.SaveVertexAICredential(context.Background(), "org_test456",
		&provider.SaveVertexAICredentialRequest{
			GCPProject: "my-project",
			Location:   "us-central1",
		})
	if err != nil {
		t.Fatalf("SaveVertexAICredential() error = %v", err)
	}
}

func TestDeleteOrgCredential(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("DELETE", "/api/v1/organizations/org_test456/providers/google-ai/credentials",
		http.StatusOK, map[string]string{"status": "deleted"})

	c := newClient(t, mock)
	err := c.Provider.DeleteOrgCredential(context.Background(), "org_test456", provider.ProviderGoogleAI)
	if err != nil {
		t.Fatalf("DeleteOrgCredential() error = %v", err)
	}
}

func TestListOrgCredentials(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := []provider.OrgCredential{fixtureOrgCredential()}
	mock.OnJSON("GET", "/api/v1/organizations/org_test456/providers/credentials",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.ListOrgCredentials(context.Background(), "org_test456")
	if err != nil {
		t.Fatalf("ListOrgCredentials() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(result))
	}
	if result[0].ID != fixture[0].ID {
		t.Errorf("expected credential ID %s, got %s", fixture[0].ID, result[0].ID)
	}
	if result[0].Provider != provider.ProviderGoogleAI {
		t.Errorf("expected provider %s, got %s", provider.ProviderGoogleAI, result[0].Provider)
	}
}

func TestListOrgCredentials_Empty(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/api/v1/organizations/org_test456/providers/credentials",
		http.StatusOK, []provider.OrgCredential{})

	c := newClient(t, mock)
	result, err := c.Provider.ListOrgCredentials(context.Background(), "org_test456")
	if err != nil {
		t.Fatalf("ListOrgCredentials() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 credentials, got %d", len(result))
	}
}

func TestSetOrgModelSelection(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("PUT", "/api/v1/organizations/org_test456/providers/google-ai/models",
		func(w http.ResponseWriter, r *http.Request) {
			testutil.AssertHeader(t, r, "Content-Type", "application/json")
			w.WriteHeader(http.StatusNoContent)
		})

	c := newClient(t, mock)
	err := c.Provider.SetOrgModelSelection(context.Background(), "org_test456", provider.ProviderGoogleAI,
		&provider.SetOrgModelSelectionRequest{
			EmbeddingModel:  "text-embedding-004",
			GenerativeModel: "gemini-2.0-flash",
		})
	if err != nil {
		t.Fatalf("SetOrgModelSelection() error = %v", err)
	}
}

// --- Model Catalog Tests ---

func TestListModels(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := []provider.SupportedModel{fixtureModel()}
	mock.OnJSON("GET", "/api/v1/providers/google-ai/models",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.ListModels(context.Background(), provider.ProviderGoogleAI, "")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}
	if result[0].ModelName != fixture[0].ModelName {
		t.Errorf("expected model name %s, got %s", fixture[0].ModelName, result[0].ModelName)
	}
}

func TestListModels_WithTypeFilter(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := []provider.SupportedModel{fixtureModel()}
	mock.On("GET", "/api/v1/providers/google-ai/models",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("type") != provider.ModelTypeGenerative {
				http.Error(w, "expected type=generative", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := encodeJSON(w, fixture); err != nil {
				t.Fatalf("encode: %v", err)
			}
		})

	c := newClient(t, mock)
	result, err := c.Provider.ListModels(context.Background(), provider.ProviderGoogleAI, provider.ModelTypeGenerative)
	if err != nil {
		t.Fatalf("ListModels(generative) error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 model, got %d", len(result))
	}
}

// --- Project Policy Tests ---

func TestSetProjectPolicy(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("PUT", "/api/v1/projects/proj_test123/providers/google-ai/policy",
		func(w http.ResponseWriter, r *http.Request) {
			testutil.AssertHeader(t, r, "Content-Type", "application/json")
			w.WriteHeader(http.StatusNoContent)
		})

	c := newClient(t, mock)
	err := c.Provider.SetProjectPolicy(context.Background(), "proj_test123", provider.ProviderGoogleAI,
		&provider.SetProjectPolicyRequest{Policy: provider.PolicyOrganization})
	if err != nil {
		t.Fatalf("SetProjectPolicy() error = %v", err)
	}
}

func TestGetProjectPolicy(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := fixtureProjectPolicy()
	mock.OnJSON("GET", "/api/v1/projects/proj_test123/providers/google-ai/policy",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.GetProjectPolicy(context.Background(), "proj_test123", provider.ProviderGoogleAI)
	if err != nil {
		t.Fatalf("GetProjectPolicy() error = %v", err)
	}
	if result.Policy != provider.PolicyOrganization {
		t.Errorf("expected policy %s, got %s", provider.PolicyOrganization, result.Policy)
	}
	if result.ProjectID != fixture.ProjectID {
		t.Errorf("expected project ID %s, got %s", fixture.ProjectID, result.ProjectID)
	}
}

func TestListProjectPolicies(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := []provider.ProjectPolicy{fixtureProjectPolicy()}
	mock.OnJSON("GET", "/api/v1/projects/proj_test123/providers/policies",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.ListProjectPolicies(context.Background(), "proj_test123")
	if err != nil {
		t.Fatalf("ListProjectPolicies() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(result))
	}
	if result[0].ID != fixture[0].ID {
		t.Errorf("expected policy ID %s, got %s", fixture[0].ID, result[0].ID)
	}
}

// --- Usage Tests ---

func TestGetProjectUsage(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := fixtureUsageSummary()
	mock.OnJSON("GET", "/api/v1/projects/proj_test123/usage",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.GetProjectUsage(context.Background(), "proj_test123", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("GetProjectUsage() error = %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 usage row, got %d", len(result.Data))
	}
	if result.Data[0].Model != "gemini-2.0-flash" {
		t.Errorf("expected model gemini-2.0-flash, got %s", result.Data[0].Model)
	}
	if result.Data[0].EstimatedCostUSD != 0.225 {
		t.Errorf("expected cost 0.225, got %f", result.Data[0].EstimatedCostUSD)
	}
}

func TestGetProjectUsage_WithTimeRange(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := fixtureUsageSummary()
	mock.On("GET", "/api/v1/projects/proj_test123/usage",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("since") == "" {
				http.Error(w, "expected since param", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := encodeJSON(w, fixture); err != nil {
				t.Fatalf("encode: %v", err)
			}
		})

	c := newClient(t, mock)
	since := time.Now().Add(-24 * time.Hour)
	_, err := c.Provider.GetProjectUsage(context.Background(), "proj_test123", since, time.Time{})
	if err != nil {
		t.Fatalf("GetProjectUsage(with since) error = %v", err)
	}
}

func TestGetOrgUsage(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := fixtureUsageSummary()
	mock.OnJSON("GET", "/api/v1/organizations/org_test456/usage",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.GetOrgUsage(context.Background(), "org_test456", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("GetOrgUsage() error = %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 usage row, got %d", len(result.Data))
	}
}

// --- Error handling ---

func TestSaveGoogleAICredential_4xxError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("POST", "/api/v1/organizations/org_test456/providers/google-ai/credentials",
		http.StatusBadRequest, map[string]string{"error": "missing api key"})

	c := newClient(t, mock)
	err := c.Provider.SaveGoogleAICredential(context.Background(), "org_test456",
		&provider.SaveGoogleAICredentialRequest{})
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
}

func TestListOrgCredentials_ServerError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/api/v1/organizations/org_test456/providers/credentials",
		http.StatusInternalServerError, map[string]string{"error": "internal error"})

	c := newClient(t, mock)
	_, err := c.Provider.ListOrgCredentials(context.Background(), "org_test456")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// encodeJSON is a test helper to JSON-encode a value into a ResponseWriter.
func encodeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}
