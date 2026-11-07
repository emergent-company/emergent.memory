package chunking_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/chunking"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// AutoProfileSuite proves that per-document shape detection selects the
// chunking parameters when the project opts in, and that opting in leaves the
// project-wide settings behind instead of blending with them.
type AutoProfileSuite struct {
	testutil.BaseSuite
}

func TestAutoProfileSuite(t *testing.T) {
	suite.Run(t, new(AutoProfileSuite))
}

func (s *AutoProfileSuite) SetupSuite() {
	s.SetDBSuffix("chunking_autoprofile")
	s.BaseSuite.SetupSuite()
}

// setProjectChunkingConfig writes kb.projects.chunking_config directly, which
// is what the project settings endpoint stores.
func (s *AutoProfileSuite) setProjectChunkingConfig(cfg map[string]any) {
	raw, err := json.Marshal(cfg)
	s.Require().NoError(err)
	_, err = s.DB().NewRaw(
		"UPDATE kb.projects SET chunking_config = ?::jsonb, updated_at = now() WHERE id = ?",
		string(raw), s.ProjectID,
	).Exec(s.Ctx)
	s.Require().NoError(err)
}

func (s *AutoProfileSuite) createDocument(content string) uuid.UUID {
	docID := uuid.New()
	s.Require().NoError(testutil.CreateTestDocument(s.Ctx, s.DB(), testutil.TestDocument{
		ID:        docID.String(),
		ProjectID: s.ProjectID,
		Filename:  testutil.StringPtr("sample.md"),
		Content:   testutil.StringPtr(content),
	}))
	return docID
}

func (s *AutoProfileSuite) recreateChunks(docID uuid.UUID) chunking.RecreateChunksResponse {
	resp := s.Client.POST("/api/documents/"+docID.String()+"/recreate-chunks",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode, "recreate-chunks failed: %s", resp.String())

	var out chunking.RecreateChunksResponse
	s.Require().NoError(resp.JSON(&out))
	return out
}

// TestAutoProfileSelectsShapeStrategy proves a markdown document gets the
// structure-aware strategy even though the project's stored strategy is the
// character splitter.
func (s *AutoProfileSuite) TestAutoProfileSelectsShapeStrategy() {
	s.setProjectChunkingConfig(map[string]any{
		"strategy":     "recursive_character",
		"maxChunkSize": 5000,
		"overlap":      500,
		"parentChild":  false,
		"autoProfile":  true,
	})

	docID := s.createDocument("# Introduction\n\nOpening paragraph.\n\n" +
		"## Details\n\nThe details of the thing are explained here.\n\n" +
		"## Usage\n\nHow to use the thing.\n")
	out := s.recreateChunks(docID)

	s.True(out.Summary.AutoProfile)
	s.Equal("markdown", out.Summary.Shape)
	s.Equal("markdown_structure", out.Summary.Strategy)
	// The project's 5000-char budget must not survive auto-profile.
	s.Equal(float64(1200), out.Summary.Config["chunkSize"])

	var shape string
	s.Require().NoError(s.DB().NewSelect().
		TableExpr("kb.chunks").
		ColumnExpr("metadata->>'shape'").
		Where("document_id = ?", docID).
		OrderExpr("chunk_index ASC").
		Limit(1).
		Scan(s.Ctx, &shape))
	s.Equal("markdown", shape, "chunk metadata must record the detected shape")
}

// TestAutoProfileProseKeepsDefault proves the prose profile is a no-op, so
// enabling auto-profile does not change documents with no structure.
func (s *AutoProfileSuite) TestAutoProfileProseKeepsDefault() {
	s.setProjectChunkingConfig(map[string]any{"autoProfile": true})

	docID := s.createDocument("The quick brown fox jumps over the lazy dog. " +
		"It was a bright cold day in April and the clocks were striking thirteen. " +
		"The hallway smelt of boiled cabbage and old rag mats.")
	out := s.recreateChunks(docID)

	s.Equal("prose", out.Summary.Shape)
	s.Equal("recursive_character", out.Summary.Strategy)
	s.Equal(float64(1000), out.Summary.Config["chunkSize"])
	s.Equal(float64(200), out.Summary.Config["chunkOverlap"])
}

// TestAutoProfileOffKeepsProjectConfig is the compatibility guard: without the
// opt-in, the project's own strategy and size still win and no shape is
// recorded.
func (s *AutoProfileSuite) TestAutoProfileOffKeepsProjectConfig() {
	s.setProjectChunkingConfig(map[string]any{
		"strategy":     "markdown_structure",
		"maxChunkSize": 1500,
		"overlap":      0,
		"parentChild":  false,
	})

	docID := s.createDocument("# Title\n\nBody text.\n\n## Section\n\nMore body text.\n")
	out := s.recreateChunks(docID)

	s.False(out.Summary.AutoProfile)
	s.Empty(out.Summary.Shape)
	s.Equal("markdown_structure", out.Summary.Strategy)
	s.Equal(float64(1500), out.Summary.Config["chunkSize"])

	var count int
	s.Require().NoError(s.DB().NewSelect().
		TableExpr("kb.chunks").
		ColumnExpr("COUNT(*)").
		Where("document_id = ?", docID).
		Where("metadata->>'shape' IS NULL").
		Scan(s.Ctx, &count))
	s.Equal(count, out.Summary.NewChunks, "no chunk should carry a shape when auto-profile is off")
}
