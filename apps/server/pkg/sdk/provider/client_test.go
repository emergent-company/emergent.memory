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

// encodeJSON is a test helper to JSON-encode a value into a ResponseWriter.
func encodeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

func fixtureProviderPricing() provider.ProviderPricing {
	return provider.ProviderPricing{
		ID:             "price_test123",
		Provider:       provider.ProviderDeepSeek,
		Model:          "deepseek-v4-pro",
		TextInputPrice: 1.74,
		OutputPrice:    3.48,
		LastSynced:     time.Now(),
	}
}

func TestListPricing(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	fixture := []provider.ProviderPricing{fixtureProviderPricing()}
	mock.OnJSON("GET", "/api/v1/pricing", http.StatusOK, fixture)

	c := newClient(t, mock)
	result, err := c.Provider.ListPricing(context.Background())
	if err != nil {
		t.Fatalf("ListPricing() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 pricing row, got %d", len(result))
	}
	if result[0].Provider != fixture[0].Provider {
		t.Errorf("expected provider %q, got %q", fixture[0].Provider, result[0].Provider)
	}
	if result[0].Model != fixture[0].Model {
		t.Errorf("expected model %q, got %q", fixture[0].Model, result[0].Model)
	}
	if result[0].TextInputPrice != fixture[0].TextInputPrice {
		t.Errorf("expected text input price %v, got %v", fixture[0].TextInputPrice, result[0].TextInputPrice)
	}
	if result[0].OutputPrice != fixture[0].OutputPrice {
		t.Errorf("expected output price %v, got %v", fixture[0].OutputPrice, result[0].OutputPrice)
	}
}

func TestListPricing_Empty(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.OnJSON("GET", "/api/v1/pricing", http.StatusOK, []provider.ProviderPricing{})

	c := newClient(t, mock)
	result, err := c.Provider.ListPricing(context.Background())
	if err != nil {
		t.Fatalf("ListPricing() error = %v", err)
	}
	if result == nil {
		t.Fatal("expected empty (non-nil) slice from ListPricing")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 pricing rows, got %d", len(result))
	}
}
