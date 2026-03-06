package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ProviderTestSuite tests the provider API endpoints (credentials, policies, models, usage).
type ProviderTestSuite struct {
	testutil.BaseSuite
}

func TestProviderSuite(t *testing.T) {
	suite.Run(t, new(ProviderTestSuite))
}

func (s *ProviderTestSuite) SetupSuite() {
	s.SetDBSuffix("provider")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Helpers
// =============================================================================

// orgURL builds the org-scoped provider base URL.
func (s *ProviderTestSuite) orgURL(path string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/providers%s", s.OrgID, path)
}

// projectURL builds the project-scoped provider base URL.
func (s *ProviderTestSuite) projectURL(path string) string {
	return fmt.Sprintf("/api/v1/projects/%s/providers%s", s.ProjectID, path)
}

// withOrgAuth returns options for an authenticated request scoped to the test org.
func (s *ProviderTestSuite) withOrgAuth() []testutil.RequestOption {
	return []testutil.RequestOption{
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
	}
}

// withProjectAuth returns options for an authenticated request scoped to both the
// test org and the test project.
func (s *ProviderTestSuite) withProjectAuth() []testutil.RequestOption {
	return []testutil.RequestOption{
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithProjectID(s.ProjectID),
	}
}

// =============================================================================
// Test: Authentication
// =============================================================================

func (s *ProviderTestSuite) TestListOrgCredentials_RequiresAuth() {
	resp := s.Client.GET(s.orgURL("/credentials"))
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *ProviderTestSuite) TestSaveGoogleAICredential_RequiresAuth() {
	resp := s.Client.POST(s.orgURL("/google-ai/credentials"),
		testutil.WithJSONBody(map[string]string{"apiKey": "test-key"}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// =============================================================================
// Test: Save + List org credentials
// =============================================================================

func (s *ProviderTestSuite) TestSaveAndListGoogleAICredential() {
	s.SkipIfExternalServer("requires encryption key config")

	// Save a Google AI credential
	resp := s.Client.POST(s.orgURL("/google-ai/credentials"),
		append(s.withOrgAuth(), testutil.WithJSONBody(map[string]string{
			"apiKey": "test-google-api-key-12345",
		}))...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "save credential: %s", resp.String())

	var saveResult map[string]string
	s.Require().NoError(json.Unmarshal(resp.Body, &saveResult))
	s.Equal("saved", saveResult["status"])

	// List credentials — should see the stored credential (metadata only)
	resp = s.Client.GET(s.orgURL("/credentials"),
		s.withOrgAuth()...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "list credentials: %s", resp.String())

	var creds []map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &creds))
	s.Require().Len(creds, 1)
	s.Equal(s.OrgID, creds[0]["orgId"])
	s.Equal("google-ai", creds[0]["provider"])
	// Encrypted key must not be exposed
	_, hasAPIKey := creds[0]["apiKey"]
	s.False(hasAPIKey, "API key must not be returned in list response")
}

func (s *ProviderTestSuite) TestSaveGoogleAICredential_MissingAPIKey() {
	resp := s.Client.POST(s.orgURL("/google-ai/credentials"),
		append(s.withOrgAuth(), testutil.WithJSONBody(map[string]string{}))...,
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProviderTestSuite) TestSaveGoogleAICredential_WrongOrg() {
	otherOrgID := "00000000-0000-0000-0000-ffff00000001"
	url := fmt.Sprintf("/api/v1/organizations/%s/providers/google-ai/credentials", otherOrgID)
	resp := s.Client.POST(url,
		// auth token scoped to s.OrgID but URL uses a different org
		testutil.WithAuth("e2e-test-user"),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(map[string]string{"apiKey": "key"}),
	)
	s.Equal(http.StatusForbidden, resp.StatusCode)
}

// =============================================================================
// Test: Delete org credential
// =============================================================================

func (s *ProviderTestSuite) TestDeleteOrgCredential() {
	s.SkipIfExternalServer("requires encryption key config")

	// First save one
	resp := s.Client.POST(s.orgURL("/google-ai/credentials"),
		append(s.withOrgAuth(), testutil.WithJSONBody(map[string]string{
			"apiKey": "key-to-delete",
		}))...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	// Delete it
	resp = s.Client.DELETE(s.orgURL("/google-ai/credentials"),
		s.withOrgAuth()...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var result map[string]string
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	s.Equal("deleted", result["status"])

	// List should now be empty
	resp = s.Client.GET(s.orgURL("/credentials"),
		s.withOrgAuth()...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var creds []map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &creds))
	s.Empty(creds)
}

// =============================================================================
// Test: Project-level policy override flow (task 5.3)
// =============================================================================

func (s *ProviderTestSuite) TestSetAndGetProjectPolicy_Organization() {
	// Set policy to "organization" (use org credentials)
	resp := s.Client.PUT(s.projectURL("/google-ai/policy"),
		append(s.withProjectAuth(), testutil.WithJSONBody(map[string]any{
			"policy": "organization",
		}))...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "set policy: %s", resp.String())

	var result map[string]string
	s.Require().NoError(json.Unmarshal(resp.Body, &result))
	s.Equal("saved", result["status"])

	// Read it back
	resp = s.Client.GET(s.projectURL("/google-ai/policy"),
		s.withProjectAuth()...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "get policy: %s", resp.String())

	var policy map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &policy))
	s.Equal(s.ProjectID, policy["projectId"])
	s.Equal("google-ai", policy["provider"])
	s.Equal("organization", policy["policy"])
}

func (s *ProviderTestSuite) TestSetAndGetProjectPolicy_ProjectLevel() {
	s.SkipIfExternalServer("requires encryption key config")

	// Set policy to "project" with its own API key
	resp := s.Client.PUT(s.projectURL("/google-ai/policy"),
		append(s.withProjectAuth(), testutil.WithJSONBody(map[string]any{
			"policy":          "project",
			"apiKey":          "project-specific-api-key",
			"generativeModel": "gemini-2.0-flash",
			"embeddingModel":  "gemini-embedding-001",
		}))...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "set project policy: %s", resp.String())

	// Read it back — verify metadata returned, not the encrypted key
	resp = s.Client.GET(s.projectURL("/google-ai/policy"),
		s.withProjectAuth()...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "get policy: %s", resp.String())

	var policy map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &policy))
	s.Equal("project", policy["policy"])
	s.Equal("gemini-2.0-flash", policy["generativeModel"])
	s.Equal("gemini-embedding-001", policy["embeddingModel"])

	// Encrypted credential must not be exposed
	_, hasAPIKey := policy["apiKey"]
	s.False(hasAPIKey, "apiKey must not be exposed in GET response")
	_, hasCiphertext := policy["encryptedCredential"]
	s.False(hasCiphertext, "encryptedCredential must not be exposed")
}

func (s *ProviderTestSuite) TestSetProjectPolicy_ProjectLevel_MissingAPIKey() {
	// policy=project without apiKey should return 400
	resp := s.Client.PUT(s.projectURL("/google-ai/policy"),
		append(s.withProjectAuth(), testutil.WithJSONBody(map[string]any{
			"policy": "project",
			// apiKey intentionally omitted
		}))...,
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProviderTestSuite) TestSetProjectPolicy_InvalidPolicy() {
	resp := s.Client.PUT(s.projectURL("/google-ai/policy"),
		append(s.withProjectAuth(), testutil.WithJSONBody(map[string]any{
			"policy": "unknown-policy-value",
		}))...,
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ProviderTestSuite) TestGetProjectPolicy_NotFound() {
	// No policy has been set yet for vertex-ai on this project
	resp := s.Client.GET(s.projectURL("/vertex-ai/policy"),
		s.withProjectAuth()...,
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// =============================================================================
// Test: List project policies
// =============================================================================

func (s *ProviderTestSuite) TestListProjectPolicies() {
	// Set two policies
	for _, p := range []struct {
		path   string
		policy string
	}{
		{"/google-ai/policy", "organization"},
		{"/vertex-ai/policy", "none"},
	} {
		resp := s.Client.PUT(s.projectURL(p.path),
			append(s.withProjectAuth(), testutil.WithJSONBody(map[string]any{
				"policy": p.policy,
			}))...,
		)
		s.Require().Equal(http.StatusOK, resp.StatusCode, "set %s policy", p.path)
	}

	// List all policies for the project
	resp := s.Client.GET(s.projectURL("/policies"),
		s.withProjectAuth()...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var policies []map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &policies))
	s.Len(policies, 2)

	policyMap := make(map[string]string)
	for _, p := range policies {
		policyMap[p["provider"].(string)] = p["policy"].(string)
	}
	s.Equal("organization", policyMap["google-ai"])
	s.Equal("none", policyMap["vertex-ai"])
}

// =============================================================================
// Test: Usage summary endpoints (empty, no crash)
// =============================================================================

func (s *ProviderTestSuite) TestGetProjectUsageSummary_Empty() {
	resp := s.Client.GET(
		fmt.Sprintf("/api/v1/projects/%s/usage", s.ProjectID),
		s.withProjectAuth()...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "usage summary: %s", resp.String())

	var summary map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &summary))
	s.NotEmpty(summary["note"])
	data, ok := summary["data"].([]any)
	s.True(ok || summary["data"] == nil, "data should be array or null")
	if ok {
		s.Empty(data, "no usage events expected for fresh project")
	}
}

func (s *ProviderTestSuite) TestGetOrgUsageSummary_Empty() {
	resp := s.Client.GET(
		fmt.Sprintf("/api/v1/organizations/%s/usage", s.OrgID),
		s.withOrgAuth()...,
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "org usage summary: %s", resp.String())

	var summary map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &summary))
	s.NotEmpty(summary["note"])
}
