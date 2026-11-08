package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inlineBatchResult decodes the entity-create batch fields the inline
// relationship tests exercise (issue #401).
type inlineBatchResult struct {
	OK   bool `json:"ok"`
	Data struct {
		Results []struct {
			OK     bool `json:"ok"`
			Index  int  `json:"index"`
			Entity struct {
				ID                  string               `json:"id"`
				Key                 string               `json:"key"`
				Relationships       []inlineRelationship `json:"relationships"`
				RelationshipsFailed int                  `json:"relationships_failed"`
				RelationshipResults []struct {
					OK           bool                `json:"ok"`
					Index        int                 `json:"index"`
					Reason       string              `json:"reason"`
					Relationship *inlineRelationship `json:"relationship,omitempty"`
				} `json:"relationship_results"`
			} `json:"entity"`
		} `json:"results"`
	} `json:"data"`
	Meta struct {
		Created             int `json:"created"`
		Failed              int `json:"failed"`
		SimilarCount        int `json:"similar_count"`
		RelationshipsFailed int `json:"relationships_failed"`
		RelationshipsTotal  int `json:"relationships_total"`
		Similar             []struct {
			RelationshipsSkipped int `json:"relationships_skipped"`
		} `json:"similar"`
	} `json:"meta"`
}

func decodeInlineBatch(t *testing.T, text string) inlineBatchResult {
	t.Helper()
	var out inlineBatchResult
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	return out
}

// TestEntityCreateInlineRelationshipsSecondPassResolvesForwardKey proves the
// inline relationships are applied after every entity in the batch exists, so a
// spec may reference a key that appears later in the same call.
func TestEntityCreateInlineRelationshipsSecondPassResolvesForwardKey(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	ctx := context.Background()

	res, err := svc.executeBatchCreateEntities(ctx, projectID, map[string]any{
		"entities": []any{
			map[string]any{
				"type": "Law",
				"key":  "law-source",
				"relationships": []any{map[string]any{
					"type": "CITES", "target_id": "law-target",
				}},
			},
			map[string]any{"type": "Law", "key": "law-target"},
		},
	})
	require.NoError(t, err)
	out := decodeInlineBatch(t, toolResultText(t, res, nil))

	assert.True(t, out.OK, "every entity and inline relationship was created")
	assert.Equal(t, 2, out.Meta.Created)
	assert.Equal(t, 1, out.Meta.RelationshipsTotal)
	assert.Zero(t, out.Meta.RelationshipsFailed)

	require.Len(t, out.Data.Results, 2)
	require.Len(t, out.Data.Results[0].Entity.Relationships, 1)
	edge := out.Data.Results[0].Entity.Relationships[0]
	// Relationship types are canonicalised to lower_snake_case by the graph
	// write path (normalizeRelationType), and relationship-create reports that
	// canonical value too. Canonicalisation is existing product behaviour.
	assert.Equal(t, "cites", edge.Type)
	assert.NotEmpty(t, edge.ID)
	assert.Equal(t, out.Data.Results[0].Entity.ID, edge.SourceID, "the new entity is the edge source")
	assert.Equal(t, out.Data.Results[1].Entity.ID, edge.TargetID, "the later key resolved to the second entity")
	assert.Zero(t, out.Data.Results[0].Entity.RelationshipsFailed)

	// The #401 contract is that the per-spec result reports the edge that was
	// actually created, not merely that the batch counts it.
	require.Len(t, out.Data.Results[0].Entity.RelationshipResults, 1)
	result := out.Data.Results[0].Entity.RelationshipResults[0]
	assert.True(t, result.OK)
	require.NotNil(t, result.Relationship)
	assert.Equal(t, edge, *result.Relationship, "the per-relationship result reports the created edge")
}

// TestEntityCreateInlineRelationshipsMalformedSpecIsReported proves a
// non-array `relationships` value is surfaced as a failed spec instead of being
// silently ignored.
func TestEntityCreateInlineRelationshipsMalformedSpecIsReported(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	ctx := context.Background()

	res, err := svc.executeBatchCreateEntities(ctx, projectID, map[string]any{
		"entities": []any{map[string]any{
			"type":          "Law",
			"key":           "law-malformed",
			"relationships": "CITES some-other-law",
		}},
	})
	require.NoError(t, err)
	out := decodeInlineBatch(t, toolResultText(t, res, nil))

	assert.False(t, out.OK)
	assert.Equal(t, 1, out.Meta.Created, "the entity itself is still created")
	assert.Equal(t, 1, out.Meta.RelationshipsFailed)
	assert.Equal(t, 1, out.Meta.RelationshipsTotal)
	require.Len(t, out.Data.Results, 1)
	assert.True(t, out.Data.Results[0].OK)
	assert.Equal(t, 1, out.Data.Results[0].Entity.RelationshipsFailed)
	require.Len(t, out.Data.Results[0].Entity.RelationshipResults, 1)
	assert.Equal(t, inlineRelReasonInvalidSpec, out.Data.Results[0].Entity.RelationshipResults[0].Reason)
}

// TestEntityCreateInlineRelationshipsSimilarSkipReportsSkipped proves a
// near-duplicate entity that carries inline relationship specs reports how many
// were skipped on the similar entry, and that they are not counted as attempted
// relationships at batch level.
func TestEntityCreateInlineRelationshipsSimilarSkipReportsSkipped(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedProject(t, db)
	_ = seedNoteObject(t, db, projectID, orgID)
	svc := newEntityCreateDedupService(t, db)

	res, err := svc.executeBatchCreateEntities(ctx, projectID, map[string]any{
		"entities": []any{map[string]any{
			"type":       "Note",
			"properties": map[string]any{"content": "The user really likes ice cream."},
			"relationships": []any{
				map[string]any{"type": "CITES", "target_id": "some-note"},
				map[string]any{"type": "OWNS", "target_id": "another-note"},
			},
		}},
		"check_similar": true,
	})
	require.NoError(t, err)
	out := decodeInlineBatch(t, toolResultText(t, res, nil))

	assert.True(t, out.OK, "a near-duplicate is not a failure")
	assert.Equal(t, 0, out.Meta.Created)
	assert.Equal(t, 1, out.Meta.SimilarCount)
	assert.Zero(t, out.Meta.RelationshipsTotal, "skipped specs were never attempted")
	require.Len(t, out.Meta.Similar, 1)
	assert.Equal(t, 2, out.Meta.Similar[0].RelationshipsSkipped)
}
