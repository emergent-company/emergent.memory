package modelconfig_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/modelconfig"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ModelConfigUpsertSuite exercises the model-config write surface through the
// full in-process server: PUT is a partial update (an omitted model keeps its
// stored value, #687 item 2), DELETE still clears the row, and an unknown
// provider is rejected at write time instead of failing per call later (#690
// item 3).
type ModelConfigUpsertSuite struct {
	testutil.BaseSuite
}

func TestModelConfigUpsertSuite(t *testing.T) {
	suite.Run(t, new(ModelConfigUpsertSuite))
}

func (s *ModelConfigUpsertSuite) SetupSuite() {
	s.SetDBSuffix("modelconfig_upsert")
	s.BaseSuite.SetupSuite()
}

func (s *ModelConfigUpsertSuite) putModelConfig(body map[string]any) *testutil.HTTPResponse {
	resp := s.Client.PUT("/api/v1/projects/"+s.ProjectID+"/model-config",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(body))
	s.Require().Equal(http.StatusOK, resp.StatusCode, "PUT model-config failed: %s", resp.String())
	return resp
}

func (s *ModelConfigUpsertSuite) getModelConfig() modelconfig.ModelConfigResponse {
	resp := s.Client.GET("/api/v1/projects/"+s.ProjectID+"/model-config",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusOK, resp.StatusCode, "GET model-config failed: %s", resp.String())
	var cfg modelconfig.ModelConfigResponse
	s.Require().NoError(resp.JSON(&cfg))
	return cfg
}

func (s *ModelConfigUpsertSuite) TestPartialUpdatePreservesOmittedModel() {
	// Seed both models.
	s.putModelConfig(map[string]any{
		"generativeModel": "deepseek/deepseek-v4-flash",
		"embeddingModel":  "google/gemini-embedding-001",
	})

	// Generative-only: the embedding model must survive.
	resp := s.putModelConfig(map[string]any{"generativeModel": "deepseek/deepseek-chat"})
	var updated modelconfig.ModelConfigResponse
	s.Require().NoError(resp.JSON(&updated))
	s.Equal("deepseek/deepseek-chat", updated.GenerativeModel)
	s.Equal("google/gemini-embedding-001", updated.EmbeddingModel, "generative-only PUT cleared the embedding model")

	stored := s.getModelConfig()
	s.Equal("deepseek/deepseek-chat", stored.GenerativeModel)
	s.Equal("google/gemini-embedding-001", stored.EmbeddingModel)

	// Embedding-only: the generative model must survive.
	resp = s.putModelConfig(map[string]any{"embeddingModel": "google-vertex/text-embedding-005"})
	s.Require().NoError(resp.JSON(&updated))
	s.Equal("deepseek/deepseek-chat", updated.GenerativeModel, "embedding-only PUT cleared the generative model")
	s.Equal("google-vertex/text-embedding-005", updated.EmbeddingModel)

	// DELETE remains the way to clear the whole config.
	del := s.Client.DELETE("/api/v1/projects/"+s.ProjectID+"/model-config",
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusNoContent, del.StatusCode, "DELETE model-config failed: %s", del.String())

	cleared := s.getModelConfig()
	s.Empty(cleared.GenerativeModel)
	s.Empty(cleared.EmbeddingModel)
}

func (s *ModelConfigUpsertSuite) TestPartialClearRemovesOneModel() {
	// Seed both models.
	s.putModelConfig(map[string]any{
		"generativeModel": "deepseek/deepseek-v4-flash",
		"embeddingModel":  "google/gemini-embedding-001",
	})

	// clearGenerative removes only that field; the update stays partial.
	resp := s.putModelConfig(map[string]any{"clearGenerative": true})
	var updated modelconfig.ModelConfigResponse
	s.Require().NoError(resp.JSON(&updated))
	s.Empty(updated.GenerativeModel, "clearGenerative kept the generative model")
	s.Equal("google/gemini-embedding-001", updated.EmbeddingModel, "clearGenerative removed the embedding model")

	stored := s.getModelConfig()
	s.Empty(stored.GenerativeModel)
	s.Equal("google/gemini-embedding-001", stored.EmbeddingModel)

	// Setting a model and clearing the same field is a 400.
	bad := s.Client.PUT("/api/v1/projects/"+s.ProjectID+"/model-config",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"generativeModel": "deepseek/deepseek-chat",
			"clearGenerative": true,
		}))
	s.Require().Equal(http.StatusBadRequest, bad.StatusCode,
		"set plus clear on one field must be rejected, got %d: %s", bad.StatusCode, bad.String())
}

func (s *ModelConfigUpsertSuite) TestUnknownProviderRejected() {
	resp := s.Client.PUT("/api/v1/projects/"+s.ProjectID+"/model-config",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"embeddingModel": "typo/gemini-embedding-001",
		}))
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode,
		"unknown provider must be a 400 validation error, got %d: %s", resp.StatusCode, resp.String())
	s.True(strings.Contains(resp.String(), "unknown provider"), "error body = %s", resp.String())
}
