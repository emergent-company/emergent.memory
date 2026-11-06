package graph_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// staticSchemaProvider serves a fixed ExtractionSchemas to the service, so a
// test can prove which exact registry key the write path consulted.
type staticSchemaProvider struct {
	schemas *graph.ExtractionSchemas
}

func (p staticSchemaProvider) GetProjectSchemas(context.Context, string) (*graph.ExtractionSchemas, error) {
	return p.schemas, nil
}

func (p staticSchemaProvider) InvalidateProjectCache(string) {}

// recordingInverseProvider records every type key the service asks it about and
// declares no inverse, so the test can assert the lookup key is canonical.
type recordingInverseProvider struct {
	seen []string
}

func (p *recordingInverseProvider) GetInverseType(_ context.Context, _, relType string) (string, bool) {
	p.seen = append(p.seen, relType)
	return "", false
}

// TestCreateSubgraphNormalisesRelationshipType is the mutation test for #176:
// a subgraph push in mixed case must be stored the way every ordinary write
// stores it (lower_snake_case), so the type-filtered HTTP read finds it and an
// ordinary create of the same edge dedupes onto it instead of minting a twin.
func TestCreateSubgraphNormalisesRelationshipType(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	resp, err := svc.CreateSubgraph(ctx, projectID, &graph.CreateSubgraphRequest{
		Objects: []graph.SubgraphObjectRequest{
			{Ref: "ada", Type: "Person", Key: strPtr("reltype-subgraph-ada")},
			{Ref: "bob", Type: "Person", Key: strPtr("reltype-subgraph-bob")},
		},
		Relationships: []graph.SubgraphRelationshipRequest{{
			Type:   "KNOWS",
			SrcRef: "ada",
			DstRef: "bob",
		}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Objects, 2)
	require.Len(t, resp.Relationships, 1)
	rel := resp.Relationships[0]

	// The stored row is lower_snake_case, not the caller's verbatim "KNOWS".
	var stored string
	require.NoError(t, db.NewRaw(
		`SELECT type FROM kb.graph_relationships WHERE id = ?`, rel.ID,
	).Scan(ctx, &stored))
	assert.Equal(t, "knows", stored, "CreateSubgraph stored the raw casing")

	// The HTTP read path lowercases ?type=knows and matches exactly.
	h := graph.NewHandler(svc, &config.Config{})
	rec := getWithAuth(t, h, projectID, "/api/graph/relationships/search?type=knows")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed struct {
		Items []struct {
			ID   uuid.UUID `json:"id"`
			Type string    `json:"type"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed), rec.Body.String())
	require.Len(t, listed.Items, 1, "GET /api/graph/relationships?type=knows did not find the pushed edge")
	assert.Equal(t, rel.ID, listed.Items[0].ID)
	assert.Equal(t, "knows", listed.Items[0].Type)

	// An ordinary create of the same edge resolves onto the pushed row rather
	// than inserting a normalised twin.
	srcID := resp.Objects[0].CanonicalID
	dstID := resp.Objects[1].CanonicalID
	existing, err := svc.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
		Type:       "knows",
		SrcID:      srcID,
		DstID:      dstID,
		Properties: map[string]any{},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, rel.CanonicalID, existing.CanonicalID, "the ordinary create minted a second head row")

	var heads int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.graph_relationships
		WHERE project_id = ? AND src_id = ? AND dst_id = ? AND supersedes_id IS NULL
	`, projectID, srcID, dstID).Scan(ctx, &heads))
	assert.Equal(t, 1, heads, "the same edge has more than one head row")
}

// TestCreateRelationshipValidatesAgainstNormalisedSchema is the mutation test
// for the second half of #176: the schema lookup must see the same key the row
// is stored under. A schema declared as `knows` requiring `since` must reject a
// `KNOWS` write that omits it, exactly as it rejects a `knows` write.
func TestCreateRelationshipValidatesAgainstNormalisedSchema(t *testing.T) {
	ctx, _, repo, svc, projectID := setupProvenanceTest(t)

	// A registry keyed the way the blueprints declare relationship types —
	// lower_snake_case (e.g. blueprints/code-memory-blueprint/schemas/*.yaml).
	provider := staticSchemaProvider{schemas: &graph.ExtractionSchemas{
		RelationshipSchemas: map[string]agents.RelationshipSchema{
			"knows": {
				Name:     "knows",
				Required: []string{"since"},
				Properties: map[string]agents.PropertyDef{
					"since": {Type: "number"},
				},
			},
		},
	}}
	validating := graph.NewService(repo, slog.Default(), provider, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)

	ada, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "Person", Key: strPtr("reltype-schema-ada")}, nil)
	require.NoError(t, err)
	bob, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "Person", Key: strPtr("reltype-schema-bob")}, nil)
	require.NoError(t, err)

	_, err = validating.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
		Type:       "KNOWS",
		SrcID:      ada.CanonicalID,
		DstID:      bob.CanonicalID,
		Properties: map[string]any{}, // missing required "since"
	}, nil)
	require.Error(t, err, "a KNOWS write skipped the `knows` relationship schema")
	assert.Contains(t, err.Error(), "since")

	// The same write in canonical casing is rejected too — so the two casings
	// now behave identically.
	_, err = validating.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
		Type:       "knows",
		SrcID:      ada.CanonicalID,
		DstID:      bob.CanonicalID,
		Properties: map[string]any{},
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "since")
}

// TestCreateRelationshipNormalisesTypeForInverseLookup covers the audit item in
// #176: maybeCreateInverse resolves the inverse from a map keyed by canonical
// lower_snake_case type, so a raw "KNOWS" lookup silently found nothing. Both
// the pre-load and the create-time lookup must ask for "knows".
func TestCreateRelationshipNormalisesTypeForInverseLookup(t *testing.T) {
	ctx, _, repo, svc, projectID := setupProvenanceTest(t)

	ada, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "Person", Key: strPtr("reltype-inverse-ada")}, nil)
	require.NoError(t, err)
	bob, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "Person", Key: strPtr("reltype-inverse-bob")}, nil)
	require.NoError(t, err)

	inverse := &recordingInverseProvider{}
	wired := graph.NewService(repo, slog.Default(), nil, inverse, nil, nil, nil, graph.NoopEventSink{}, nil, nil)

	_, err = wired.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
		Type:       "KNOWS",
		SrcID:      ada.CanonicalID,
		DstID:      bob.CanonicalID,
		Properties: map[string]any{},
	}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, inverse.seen)
	for _, seen := range inverse.seen {
		assert.Equal(t, "knows", seen, "inverse lookup used a raw type key")
	}
}

// getWithAuth issues a GET against a handler with an authenticated user for the
// test project, mirroring the auth middleware and the server error handler.
func getWithAuth(t *testing.T, h *graph.Handler, projectID uuid.UUID, path string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	user := &auth.AuthUser{ID: uuid.NewString(), ProjectID: projectID.String()}
	req = req.WithContext(auth.InjectAuthContext(req.Context(), user))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), user)
	if err := h.ListRelationships(c); err != nil {
		t.Fatalf("%s: unexpected handler error %v", path, err)
	}
	return rec
}
