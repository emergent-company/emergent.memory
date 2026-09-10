package provider_test

// provider_e2e_test.go exercises the full provider configuration flow against
// live APIs: DeepSeek (generative) + Google AI (embedding).
//
// Required environment variables (loaded from .env / .env.local):
//   DEEPSEEK_API_KEY      — DeepSeek API key
//   GOOGLE_API_KEY        — Google AI (Gemini) API key
//   LLM_ENCRYPTION_KEY    — AES-256 key for encrypting stored credentials
//
// Tests are skipped automatically when any of the above is absent.
//
// Run with:
//   POSTGRES_PASSWORD=local-test-password POSTGRES_PORT=5436 \
//     go test ./domain/provider/... -v -run TestProviderE2ESuite -timeout 3m

import (
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ProviderE2ESuite tests the real provider configuration flow using live APIs.
type ProviderE2ESuite struct {
	testutil.BaseSuite

	deepseekKey string
	googleKey   string
}

func TestProviderE2ESuite(t *testing.T) {
	suite.Run(t, new(ProviderE2ESuite))
}

func (s *ProviderE2ESuite) SetupSuite() {
	// Load .env / .env.local before BaseSuite so LLM_ENCRYPTION_KEY is visible
	// to config.NewConfig() when the test DB + server are created.
	testutil.LoadEnvFiles()

	s.deepseekKey = os.Getenv("DEEPSEEK_API_KEY")
	s.googleKey = os.Getenv("GOOGLE_API_KEY")
	encKey := os.Getenv("LLM_ENCRYPTION_KEY")

	if s.deepseekKey == "" || s.googleKey == "" || encKey == "" {
		s.T().Skip("DEEPSEEK_API_KEY, GOOGLE_API_KEY, and LLM_ENCRYPTION_KEY must all be set to run provider e2e tests")
	}

	s.SetDBSuffix("provider_e2e")
	s.BaseSuite.SetupSuite()
}

// helpers ──────────────────────────────────────────────────────────────────────

func (s *ProviderE2ESuite) putProvider(provider string, body map[string]any) *testutil.HTTPResponse {
	return s.Client.PUT(
		"/api/v1/projects/"+s.ProjectID+"/providers/"+provider,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
		testutil.WithJSONBody(body),
	)
}

func (s *ProviderE2ESuite) getProvider(provider string) *testutil.HTTPResponse {
	return s.Client.GET(
		"/api/v1/projects/"+s.ProjectID+"/providers/"+provider,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
}

func (s *ProviderE2ESuite) testProvider(provider string) *testutil.HTTPResponse {
	return s.Client.POST(
		"/api/v1/projects/"+s.ProjectID+"/providers/"+provider+"/test",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
}

// tests ────────────────────────────────────────────────────────────────────────

// TestConfigureDeepSeek_Generative configures DeepSeek with deepseek-v4-flash as the
// generative model, verifies the config is stored, and runs the live test endpoint.
func (s *ProviderE2ESuite) TestConfigureDeepSeek_Generative() {
	resp := s.putProvider("deepseek", map[string]any{
		"apiKey":          s.deepseekKey,
		"generativeModel": "deepseek-v4-flash",
	})
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())

	var cfg map[string]any
	s.Require().NoError(resp.JSON(&cfg))
	s.Equal("deepseek", cfg["provider"])
	s.Equal("deepseek-v4-flash", cfg["generativeModel"])
	s.Empty(cfg["embeddingModel"], "DeepSeek should have no embedding model")

	// Verify persisted via GET.
	get := s.getProvider("deepseek")
	s.Require().Equal(http.StatusOK, get.StatusCode, get.String())
	var stored map[string]any
	s.Require().NoError(get.JSON(&stored))
	s.Equal("deepseek-v4-flash", stored["generativeModel"])

	// Live test: generate call should succeed.
	test := s.testProvider("deepseek")
	s.Require().Equal(http.StatusOK, test.StatusCode, test.String())
	var testResult map[string]any
	s.Require().NoError(test.JSON(&testResult))
	s.Equal("deepseek-v4-flash", testResult["model"])
	s.NotEmpty(testResult["reply"], "expected a non-empty reply from DeepSeek")
}

// TestConfigureGoogle_EmbeddingOnly configures Google AI with gemini-embedding-001
// as the embedding model only (no generative model). Verifies the embedding test passes
// and the config is stored correctly.
func (s *ProviderE2ESuite) TestConfigureGoogle_EmbeddingOnly() {
	resp := s.putProvider("google", map[string]any{
		"apiKey":         s.googleKey,
		"embeddingModel": "gemini-embedding-001",
	})
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())

	var cfg map[string]any
	s.Require().NoError(resp.JSON(&cfg))
	s.Equal("google", cfg["provider"])
	s.Equal("gemini-embedding-001", cfg["embeddingModel"])
	// New project — generative model left empty since not specified.
	s.Empty(cfg["generativeModel"], "generative model should be empty for embedding-only config on new project")

	// Verify persisted.
	get := s.getProvider("google")
	s.Require().Equal(http.StatusOK, get.StatusCode, get.String())
	var stored map[string]any
	s.Require().NoError(get.JSON(&stored))
	s.Equal("gemini-embedding-001", stored["embeddingModel"])
}

// TestConfigureBothProviders sets up DeepSeek (generative) and Google AI (embedding)
// as separate providers on the same project — the typical production setup.
func (s *ProviderE2ESuite) TestConfigureBothProviders() {
	// Configure DeepSeek for generation.
	dsResp := s.putProvider("deepseek", map[string]any{
		"apiKey":          s.deepseekKey,
		"generativeModel": "deepseek-v4-flash",
	})
	s.Require().Equal(http.StatusOK, dsResp.StatusCode, dsResp.String())

	// Configure Google AI for embeddings.
	gResp := s.putProvider("google", map[string]any{
		"apiKey":         s.googleKey,
		"embeddingModel": "gemini-embedding-001",
	})
	s.Require().Equal(http.StatusOK, gResp.StatusCode, gResp.String())

	// List all providers for the project — both must appear.
	list := s.Client.GET(
		"/api/v1/projects/"+s.ProjectID+"/providers",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithOrgID(s.OrgID),
	)
	s.Require().Equal(http.StatusOK, list.StatusCode, list.String())
	var providers []map[string]any
	s.Require().NoError(list.JSON(&providers))
	s.Len(providers, 2, "expected exactly 2 configured providers")

	providerNames := make([]string, len(providers))
	for i, p := range providers {
		providerNames[i], _ = p["provider"].(string)
	}
	s.Contains(providerNames, "deepseek")
	s.Contains(providerNames, "google")

	// Live test for DeepSeek generative.
	dsTest := s.testProvider("deepseek")
	s.Require().Equal(http.StatusOK, dsTest.StatusCode, dsTest.String())
	var dsResult map[string]any
	s.Require().NoError(dsTest.JSON(&dsResult))
	s.Equal("deepseek-v4-flash", dsResult["model"])

	// Live test for Google embedding.
	gTest := s.testProvider("google")
	s.Require().Equal(http.StatusOK, gTest.StatusCode, gTest.String())
	var gResult map[string]any
	s.Require().NoError(gTest.JSON(&gResult))
	s.Equal("gemini-embedding-001", gResult["embeddingModel"])
}

// TestDeepSeek_RejectsEmbeddingModel verifies that specifying an embedding model
// for DeepSeek (which has no embedding API) returns a 400 error.
func (s *ProviderE2ESuite) TestDeepSeek_RejectsEmbeddingModel() {
	resp := s.putProvider("deepseek", map[string]any{
		"apiKey":          s.deepseekKey,
		"generativeModel": "deepseek-v4-flash",
		"embeddingModel":  "some-embed-model",
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode,
		"DeepSeek should reject embedding model: %s", resp.String())
}

// TestConfigureGoogle_WrongModelForRole verifies that passing an embedding-only
// model as the generative model is rejected because the generate test will fail.
func (s *ProviderE2ESuite) TestConfigureGoogle_WrongModelForRole() {
	resp := s.putProvider("google", map[string]any{
		"apiKey":          s.googleKey,
		"generativeModel": "gemini-embedding-001", // embedding model used as generative
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode,
		"should reject embedding model when configured as generative: %s", resp.String())
}

// TestConfigureDeepSeek_BadAPIKey verifies that a wrong API key is rejected.
// The generate test call fails and the config is never stored.
func (s *ProviderE2ESuite) TestConfigureDeepSeek_BadAPIKey() {
	resp := s.putProvider("deepseek", map[string]any{
		"apiKey":          "sk-invalid-key-000000000000000000000000000000",
		"generativeModel": "deepseek-v4-flash",
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode,
		"bad DeepSeek API key should fail: %s", resp.String())

	// Config must not be stored.
	get := s.getProvider("deepseek")
	s.Equal(http.StatusNotFound, get.StatusCode,
		"provider config should not be persisted after failed setup")
}

// TestConfigureGoogle_BadAPIKey verifies that a wrong Google AI key is rejected.
func (s *ProviderE2ESuite) TestConfigureGoogle_BadAPIKey() {
	resp := s.putProvider("google", map[string]any{
		"apiKey":         "AIzaSy-invalid-key-000000000000000000000",
		"embeddingModel": "gemini-embedding-001",
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode,
		"bad Google API key should fail: %s", resp.String())

	// Config must not be stored.
	get := s.getProvider("google")
	s.Equal(http.StatusNotFound, get.StatusCode,
		"provider config should not be persisted after failed setup")
}

// TestConfigureDeepSeek_UnknownModel verifies that a model name not in the
// catalog is rejected (catalog sync succeeds but the requested model is absent).
func (s *ProviderE2ESuite) TestConfigureDeepSeek_UnknownModel() {
	resp := s.putProvider("deepseek", map[string]any{
		"apiKey":          s.deepseekKey,
		"generativeModel": "deepseek-does-not-exist-v99",
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode,
		"unknown model name should be rejected: %s", resp.String())
}

// TestConfigureGoogle_UnknownEmbeddingModel verifies that an embedding model
// name not in the Google catalog is rejected.
func (s *ProviderE2ESuite) TestConfigureGoogle_UnknownEmbeddingModel() {
	resp := s.putProvider("google", map[string]any{
		"apiKey":         s.googleKey,
		"embeddingModel": "gemini-embedding-does-not-exist-v99",
	})
	s.Equal(http.StatusBadRequest, resp.StatusCode,
		"unknown embedding model name should be rejected: %s", resp.String())
}
