// Package api_test — provider_test.go
//
// Tests for the provider API endpoints (credentials, policies, models, usage).
// Ported from emergent.memory/apps/server/tests/e2e/provider_test.go
package api_test

import (
	"fmt"
	"net/http"
	"os"
	"testing"
)

// =============================================================================
// Helpers
// =============================================================================

func providerOrgURL(orgID, path string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/providers%s", orgID, path)
}

func providerProjectURL(projectID, path string) string {
	return fmt.Sprintf("/api/v1/projects/%s/providers%s", projectID, path)
}

// =============================================================================
// Authentication tests
// =============================================================================

func TestProvider_ListOrgCredentials_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)
	resp := doAPIWithOrg(t, "GET", providerOrgURL(orgID, "/credentials"), "", "", orgID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestProvider_SaveGoogleAICredential_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)
	resp := doAPIWithOrg(t, "POST", providerOrgURL(orgID, "/google/credentials"), "", "", orgID,
		jsonBody(map[string]string{"apiKey": "test-key"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

// =============================================================================
// Credential tests — require encryption key config in some cases
// =============================================================================

func TestProvider_SaveAndListGoogleAICredential(t *testing.T) {
	t.Skip("requires encryption key config")
}

func TestProvider_SaveGoogleAICredential_MissingAPIKey(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	_, orgID := setupProjectLogged(t, rl)
	resp := doAPIWithOrg(t, "PUT", providerOrgURL(orgID, "/google"),
		e2eTestToken(), "", orgID,
		jsonBody(map[string]string{}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestProvider_SaveGoogleAICredential_WrongOrg(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	_, orgID := setupProjectLogged(t, rl)
	otherOrgID := "00000000-0000-0000-0000-ffff00000001"
	url := fmt.Sprintf("/api/v1/organizations/%s/providers/google", otherOrgID)

	// auth token scoped to orgID but URL uses a different org
	resp := doAPIWithOrg(t, "PUT", url, e2eTestToken(), "", orgID,
		jsonBody(map[string]string{"apiKey": "key"}))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestProvider_DeleteOrgCredential(t *testing.T) {
	t.Skip("requires encryption key config")
}

// =============================================================================
// Project-level policy tests
// =============================================================================

func TestProvider_SetAndGetProjectPolicy_Organization(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	projectID, orgID := setupProjectLogged(t, rl)

	// Set a deepseek provider config
	resp := doAPIWithOrg(t, "PUT", providerProjectURL(projectID, "/deepseek"),
		e2eTestToken(), projectID, orgID,
		jsonBody(map[string]any{"apiKey": apiKey, "generativeModel": "deepseek-chat"}))
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["provider"] != "deepseek" {
		t.Errorf("expected provider=deepseek, got %v", result["provider"])
	}

	// Read it back
	resp = doAPIWithOrg(t, "GET", providerProjectURL(projectID, "/deepseek"),
		e2eTestToken(), projectID, orgID, nil)
	body = mustStatus(t, resp, http.StatusOK)

	var config map[string]any
	parseBodyJSON(t, body, &config)
	if config["provider"] != "deepseek" {
		t.Errorf("expected provider=deepseek, got %v", config["provider"])
	}
	rl.Printf("project-level provider config set and retrieved for project %s", projectID)
}

func TestProvider_SetAndGetProjectPolicy_ProjectLevel(t *testing.T) {
	t.Skip("requires encryption key config")
}

func TestProvider_SetProjectPolicy_ProjectLevel_MissingAPIKey(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)

	// PUT project-level provider config with missing apiKey → 400
	resp := doAPIWithOrg(t, "PUT", providerProjectURL(projectID, "/google"),
		e2eTestToken(), projectID, orgID,
		jsonBody(map[string]any{
			// apiKey intentionally omitted
		}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestProvider_SetProjectPolicy_InvalidPolicy(t *testing.T) {
	t.Skip("policy concept removed — provider API now stores credentials directly")
}

func TestProvider_GetProjectPolicy_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)

	// No policy has been set yet for google-vertex
	resp := doAPIWithOrg(t, "GET", providerProjectURL(projectID, "/google-vertex/policy"),
		e2eTestToken(), projectID, orgID, nil)
	mustStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// List project policies
// =============================================================================

func TestProvider_ListProjectPolicies(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	projectID, orgID := setupProjectLogged(t, rl)

	// Set two provider configs using deepseek with real key
	for _, provider := range []string{"deepseek"} {
		resp := doAPIWithOrg(t, "PUT", providerProjectURL(projectID, "/"+provider),
			e2eTestToken(), projectID, orgID,
			jsonBody(map[string]any{"apiKey": apiKey, "generativeModel": "deepseek-chat"}))
		if resp.StatusCode != http.StatusOK {
			b := mustStatus(t, resp, http.StatusOK)
			t.Fatalf("set %s provider failed: %s", provider, b)
		}
		resp.Body.Close()
	}

	// List all project providers
	resp := doAPIWithOrg(t, "GET", providerProjectURL(projectID, ""),
		e2eTestToken(), projectID, orgID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var configs []map[string]any
	parseBodyJSON(t, body, &configs)
	if len(configs) < 1 {
		t.Errorf("expected at least 1 provider config, got %d", len(configs))
	}

	providerMap := make(map[string]bool)
	for _, c := range configs {
		if p, ok := c["provider"].(string); ok {
			providerMap[p] = true
		}
	}
	if !providerMap["deepseek"] {
		t.Error("expected deepseek in provider list")
	}
	rl.Printf("list project providers returned %d entries", len(configs))
}

// =============================================================================
// Usage summary endpoints
// =============================================================================

func TestProvider_GetProjectUsageSummary_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)

	resp := doAPIWithOrg(t, "GET",
		fmt.Sprintf("/api/v1/projects/%s/usage", projectID),
		e2eTestToken(), projectID, orgID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var summary map[string]any
	parseBodyJSON(t, body, &summary)
	if summary["note"] == nil || summary["note"] == "" {
		t.Error("expected non-empty 'note' field in usage summary")
	}
	rl.Printf("project usage summary OK")
}

func TestProvider_GetOrgUsageSummary_Empty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)

	resp := doAPIWithOrg(t, "GET",
		fmt.Sprintf("/api/v1/organizations/%s/usage", orgID),
		e2eTestToken(), "", orgID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var summary map[string]any
	parseBodyJSON(t, body, &summary)
	if summary["note"] == nil || summary["note"] == "" {
		t.Error("expected non-empty 'note' field in org usage summary")
	}
	rl.Printf("org usage summary OK")
}
