package blueprints

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImportFromGitHub_PluralRelationshipSourceTypes is the end-to-end
// regression test for the plural sourceTypes/targetTypes import failure. A pack
// declaring a relationship with plural `sourceTypes: [Alpha, Beta]` and a
// singular `targetType: Gamma` previously failed during apply with:
//
//	400 bad_request: invalid schema definitions: relationshipTypeSchemas [0]
//	("delivered_in") is missing required field 'sourceType'
//
// because the server-side manifest only understood the singular fields and
// silently dropped the plural array. This test drives the full import → apply
// flow and asserts the schemas layer receives TWO singular entries (Alpha→Gamma
// and Beta→Gamma), not one def with an empty sourceType.
func TestImportFromGitHub_PluralRelationshipSourceTypes(t *testing.T) {
	db := connectTestDB(t)
	svc, _, schemasRepo := newMigrationService(t, db)
	ctx := context.Background()

	_, projectID := seedProject(t, db)

	packName := uniqueName("plural-rel")
	packYAML := `name: ` + packName + `
version: 1.0.0
objectTypes:
  - name: Alpha
    label: Alpha
  - name: Beta
    label: Beta
  - name: Gamma
    label: Gamma
relationshipTypes:
  - name: delivered_in
    sourceTypes: [Alpha, Beta]
    targetType: Gamma
`

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: packYAML},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	// Import creates + publishes the blueprint (scoped to the project).
	bp, err := svc.ImportFromGitHub(ctx, projectID, &ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)
	require.Equal(t, StatusPublished, bp.Status)

	// The stored manifest must preserve the plural declaration (round-trip).
	stored, err := svc.repo.GetByID(ctx, projectID, bp.ID)
	require.NoError(t, err)
	var m BlueprintManifest
	require.NoError(t, json.Unmarshal(stored.Manifest, &m))
	require.Len(t, m.Packs, 1)
	require.Len(t, m.Packs[0].RelationshipTypes, 1)
	assert.Equal(t, []string{"Alpha", "Beta"}, m.Packs[0].RelationshipTypes[0].SourceTypes,
		"plural sourceTypes must survive the manifest round-trip")
	assert.Equal(t, "Gamma", m.Packs[0].RelationshipTypes[0].TargetType)

	// Apply materializes the pack. This is the step that previously 400'd.
	_, err = svc.Apply(ctx, bp.ID, projectID, uuid.NewString(), ApplyOptions{})
	require.NoError(t, err, "apply must accept plural relationship source types")

	// The schemas layer must have received TWO singular entries, not one def
	// with an empty sourceType.
	pack, err := schemasRepo.GetPackByNameVersion(ctx, packName, "1.0.0")
	require.NoError(t, err)

	var rels []struct {
		Name       string `json:"name"`
		SourceType string `json:"sourceType"`
		TargetType string `json:"targetType"`
	}
	require.NoError(t, json.Unmarshal(pack.RelationshipTypeSchemas, &rels))
	require.Len(t, rels, 2, "plural source types must expand to two singular relationship schemas")

	sources := map[string]bool{}
	for _, r := range rels {
		assert.Equal(t, "delivered_in", r.Name)
		assert.Equal(t, "Gamma", r.TargetType)
		sources[r.SourceType] = true
	}
	assert.True(t, sources["Alpha"], "expected a singular Alpha→Gamma entry, got %v", sources)
	assert.True(t, sources["Beta"], "expected a singular Beta→Gamma entry, got %v", sources)
}

// TestExpandRelationshipTypes locks the expansion semantics without a database:
// singular pass-through, plural cross-product, and the deliberate "one empty
// entry" behaviour for a def with neither source nor target.
func TestExpandRelationshipTypes(t *testing.T) {
	t.Run("singular pass-through", func(t *testing.T) {
		in := []RelationshipTypeDef{{Name: "knows", SourceType: "Person", TargetType: "Person"}}
		out := expandRelationshipTypes(in)
		require.Len(t, out, 1)
		assert.Equal(t, "knows", out[0].Name)
		assert.Equal(t, "Person", out[0].SourceType)
		assert.Equal(t, "Person", out[0].TargetType)
	})

	t.Run("plural source cross-product", func(t *testing.T) {
		in := []RelationshipTypeDef{{Name: "delivered_in", SourceTypes: []string{"Alpha", "Beta"}, TargetType: "Gamma"}}
		out := expandRelationshipTypes(in)
		require.Len(t, out, 2)
		assert.Equal(t, "Alpha", out[0].SourceType)
		assert.Equal(t, "Gamma", out[0].TargetType)
		assert.Equal(t, "Beta", out[1].SourceType)
		assert.Equal(t, "Gamma", out[1].TargetType)
	})

	t.Run("plural source and target cross-product", func(t *testing.T) {
		in := []RelationshipTypeDef{{Name: "links", SourceTypes: []string{"A", "B"}, TargetTypes: []string{"C", "D"}}}
		out := expandRelationshipTypes(in)
		require.Len(t, out, 4)
	})

	t.Run("neither source nor target yields one empty entry", func(t *testing.T) {
		in := []RelationshipTypeDef{{Name: "orphan"}}
		out := expandRelationshipTypes(in)
		require.Len(t, out, 1)
		assert.Equal(t, "orphan", out[0].Name)
		assert.Empty(t, out[0].SourceType)
		assert.Empty(t, out[0].TargetType)
	})

	t.Run("preserves label description and properties", func(t *testing.T) {
		in := []RelationshipTypeDef{{
			Name:        "delivered_in",
			Label:       "Delivered In",
			Description: "Where an item is delivered",
			SourceTypes: []string{"Alpha", "Beta"},
			TargetType:  "Gamma",
			Properties:  map[string]any{"weight": 1},
		}}
		out := expandRelationshipTypes(in)
		require.Len(t, out, 2)
		for _, o := range out {
			assert.Equal(t, "Delivered In", o.Label)
			assert.Equal(t, "Where an item is delivered", o.Description)
			assert.Equal(t, map[string]any{"weight": 1}, o.Properties)
			assert.Empty(t, o.SourceTypes, "expanded entries must be singular-only")
			assert.Empty(t, o.TargetTypes, "expanded entries must be singular-only")
		}
	})
}
