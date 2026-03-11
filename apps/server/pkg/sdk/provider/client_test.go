package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/provider"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
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

func fixtureProviderConfig() provider.ProviderConfig {
	return provider.ProviderConfig{
		ID:              "cfg_test123",
		Provider:        provider.ProviderGoogleAI,
		GenerativeModel: "gemini-2.0-flash",
		EmbeddingModel:  "text-embedding-004",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
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

// --- Organization Provider Config Tests ---

func TestUpsertOrgConfig(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := fixtureProviderConfig()
	mock.On("PUT", "/api/v1/organizations/org_test456/providers/google",
		func(w http.ResponseWriter, r *http.Request) {
			testutil.AssertHeader(t, r, "Content-Type", "application/json")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := encodeJSON(w, fixture); err != nil {
				t.Fatalf("encode: %v", err)
			}
		})

	c := newClient(t, mock)
	result, err := c.Provider.UpsertOrgConfig(context.Background(), "org_test456", provider.ProviderGoogleAI,
		&provider.UpsertProviderConfigRequest{APIKey: "AIza-test-key"})
	if err != nil {
		t.Fatalf("UpsertOrgConfig() error = %v", err)
	}
	if result.Provider != provider.ProviderGoogleAI {
		t.Errorf("expected provider %s, got %s", provider.ProviderGoogleAI, result.Provider)
	}
	if result.GenerativeModel != fixture.GenerativeModel {
		t.Errorf("expected generative model %s, got %s", fixture.GenerativeModel, result.GenerativeModel)
	}
}

func TestGetOrgConfig(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := fixtureProviderConfig()
	mock.OnJSON("GET", "/api/v1/organizations/org_test456/providers/google",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.GetOrgConfig(context.Background(), "org_test456", provider.ProviderGoogleAI)
	if err != nil {
		t.Fatalf("GetOrgConfig() error = %v", err)
	}
	if result.ID != fixture.ID {
		t.Errorf("expected ID %s, got %s", fixture.ID, result.ID)
	}
}

func TestDeleteOrgConfig(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("DELETE", "/api/v1/organizations/org_test456/providers/google",
		http.StatusOK, map[string]string{"status": "deleted"})

	c := newClient(t, mock)
	err := c.Provider.DeleteOrgConfig(context.Background(), "org_test456", provider.ProviderGoogleAI)
	if err != nil {
		t.Fatalf("DeleteOrgConfig() error = %v", err)
	}
}

func TestListOrgConfigs(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := []provider.ProviderConfig{fixtureProviderConfig()}
	mock.OnJSON("GET", "/api/v1/organizations/org_test456/providers",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.ListOrgConfigs(context.Background(), "org_test456")
	if err != nil {
		t.Fatalf("ListOrgConfigs() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 config, got %d", len(result))
	}
	if result[0].ID != fixture[0].ID {
		t.Errorf("expected config ID %s, got %s", fixture[0].ID, result[0].ID)
	}
	if result[0].Provider != provider.ProviderGoogleAI {
		t.Errorf("expected provider %s, got %s", provider.ProviderGoogleAI, result[0].Provider)
	}
}

func TestListOrgConfigs_Empty(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/api/v1/organizations/org_test456/providers",
		http.StatusOK, []provider.ProviderConfig{})

	c := newClient(t, mock)
	result, err := c.Provider.ListOrgConfigs(context.Background(), "org_test456")
	if err != nil {
		t.Fatalf("ListOrgConfigs() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 configs, got %d", len(result))
	}
}

// --- Project Provider Config Tests ---

func TestUpsertProjectConfig(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := fixtureProviderConfig()
	mock.On("PUT", "/api/v1/projects/proj_test123/providers/google",
		func(w http.ResponseWriter, r *http.Request) {
			testutil.AssertHeader(t, r, "Content-Type", "application/json")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := encodeJSON(w, fixture); err != nil {
				t.Fatalf("encode: %v", err)
			}
		})

	c := newClient(t, mock)
	result, err := c.Provider.UpsertProjectConfig(context.Background(), "proj_test123", provider.ProviderGoogleAI,
		&provider.UpsertProviderConfigRequest{APIKey: "AIza-project-key"})
	if err != nil {
		t.Fatalf("UpsertProjectConfig() error = %v", err)
	}
	if result.GenerativeModel != fixture.GenerativeModel {
		t.Errorf("expected generative model %s, got %s", fixture.GenerativeModel, result.GenerativeModel)
	}
}

func TestGetProjectConfig(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := fixtureProviderConfig()
	mock.OnJSON("GET", "/api/v1/projects/proj_test123/providers/google",
		http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.GetProjectConfig(context.Background(), "proj_test123", provider.ProviderGoogleAI)
	if err != nil {
		t.Fatalf("GetProjectConfig() error = %v", err)
	}
	if result.ID != fixture.ID {
		t.Errorf("expected ID %s, got %s", fixture.ID, result.ID)
	}
}

func TestDeleteProjectConfig(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("DELETE", "/api/v1/projects/proj_test123/providers/google",
		http.StatusOK, map[string]string{"status": "deleted"})

	c := newClient(t, mock)
	err := c.Provider.DeleteProjectConfig(context.Background(), "proj_test123", provider.ProviderGoogleAI)
	if err != nil {
		t.Fatalf("DeleteProjectConfig() error = %v", err)
	}
}

// --- Model Catalog Tests ---

func TestListModels(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := []provider.SupportedModel{fixtureModel()}
	mock.OnJSON("GET", "/api/v1/providers/google/models",
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
	mock.On("GET", "/api/v1/providers/google/models",
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

func TestUpsertOrgConfig_4xxError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("PUT", "/api/v1/organizations/org_test456/providers/google",
		http.StatusBadRequest, map[string]string{"error": "missing api key"})

	c := newClient(t, mock)
	_, err := c.Provider.UpsertOrgConfig(context.Background(), "org_test456", provider.ProviderGoogleAI,
		&provider.UpsertProviderConfigRequest{})
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
}

func TestListOrgConfigs_ServerError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/api/v1/organizations/org_test456/providers",
		http.StatusInternalServerError, map[string]string{"error": "internal error"})

	c := newClient(t, mock)
	_, err := c.Provider.ListOrgConfigs(context.Background(), "org_test456")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// encodeJSON is a test helper to JSON-encode a value into a ResponseWriter.
func encodeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}
