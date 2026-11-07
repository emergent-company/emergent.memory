package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// These two tests pin the #308 remaining-read work: the include_relationships=true
// relationship-enrichment/count paths of entity-type-list and entity-query read
// the run's branch scope exactly like the already-scoped object-side reads. They
// prove that a scoped run sees a branch-only relationship (main-preferred), while
// a main-only run does not.

// scopedUnrestrictedContext returns a context carrying an unrestricted unmerged
// branch scope (any origin, any name), so the seedBranch manual branch is in
// scope without declaring an origin allowlist.
func scopedUnrestrictedContext(t *testing.T) context.Context {
	t.Helper()
	scope, err := branchscope.New(branchscope.Config{Unmerged: true})
	require.NoError(t, err)
	return branchscope.ContextWith(context.Background(), scope)
}

// TestExecuteListEntityTypes_RelationshipTypesRespectBranchScope pins the
// relationship-type count half: a branch-only relationship must be counted by a
// scoped run and hidden from a main-only run.
func TestExecuteListEntityTypes_RelationshipTypesRespectBranchScope(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	srcID := insertGraphObject(t, db, projectID, "Person", "alice")
	branchID := seedBranch(t, db, projectID, "plan/q3")
	dstID := insertBranchGraphObject(t, db, projectID, branchID, "Person", "bob")
	insertBranchRelationship(t, db, projectID, branchID, "KNOWS", srcID, dstID)

	svc := &Service{db: db}

	// A scoped run counts the branch-only relationship.
	scoped := runListEntityTypes(t, svc, scopedUnrestrictedContext(t), projectID,
		map[string]any{"include_relationships": true})
	require.Len(t, scoped.Relationships, 1, "scoped run must count the branch relationship")
	rel := scoped.Relationships[0]
	assert.Equal(t, "KNOWS", rel.Type)
	assert.Equal(t, "Person", rel.FromType)
	assert.Equal(t, "Person", rel.ToType)
	assert.Equal(t, 1, rel.Count)

	// A main-only run (installed nil scope) does not see it.
	mainOnly := branchscope.ContextWith(context.Background(), nil)
	mainOnlyResult := runListEntityTypes(t, svc, mainOnly, projectID,
		map[string]any{"include_relationships": true})
	assert.Empty(t, mainOnlyResult.Relationships, "main-only run must not see branch relationships")
}

// TestExecuteQueryEntities_RelationshipEnrichmentRespectsBranchScope pins the
// enrichment half: a branch-only outgoing relationship must enrich an entity for
// a scoped run and be absent for a main-only run.
func TestExecuteQueryEntities_RelationshipEnrichmentRespectsBranchScope(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	srcID := insertQueryEntity(t, db, projectID, "src-key", map[string]any{"name": "src"})
	branchID := seedBranch(t, db, projectID, "plan/q3")
	// The target lives on the branch and is a DIFFERENT type than the queried
	// source, so the object-side read never surfaces it and the enrichment is
	// the only path that can reach it.
	dstID := insertBranchGraphObject(t, db, projectID, branchID, "Note", "dst-key")
	insertBranchRelationship(t, db, projectID, branchID, "RELATES_TO", srcID, dstID)

	svc := &Service{db: db}
	args := map[string]any{"type_name": "LegalParagraph", "include_relationships": true}

	// A scoped run enriches the source entity with the branch relationship.
	scoped := runEnrichedEntityQuery(t, svc, scopedUnrestrictedContext(t), projectID, args)
	require.Len(t, scoped, 1, "only the source entity is returned")
	require.Len(t, scoped[0].Relationships, 1, "scoped run must enrich with the branch relationship")
	assert.Equal(t, dstID, scoped[0].Relationships[0].TargetID)
	assert.Equal(t, "RELATES_TO", scoped[0].Relationships[0].Type)

	// A main-only run returns the entity but with no relationships.
	mainOnly := branchscope.ContextWith(context.Background(), nil)
	mainOnlyEntities := runEnrichedEntityQuery(t, svc, mainOnly, projectID, args)
	require.Len(t, mainOnlyEntities, 1, "the source entity must be returned")
	assert.Empty(t, mainOnlyEntities[0].Relationships, "main-only run must not enrich with branch relationships")
}

// TestExecuteQueryEntities_RelationshipEnrichmentExplicitBranch pins the
// explicit-branch enrichment path (branch argument set). It also guards the
// placeholder ordering: the dst branch clause lives in the JOIN, before the
// WHERE's src IN / project placeholders, so its args must precede them.
func TestExecuteQueryEntities_RelationshipEnrichmentExplicitBranch(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	branchID := seedBranch(t, db, projectID, "plan/q3")
	srcID := insertBranchGraphObject(t, db, projectID, branchID, "LegalParagraph", "src-key")
	dstID := insertBranchGraphObject(t, db, projectID, branchID, "Note", "dst-key")
	insertBranchRelationship(t, db, projectID, branchID, "RELATES_TO", srcID, dstID)

	svc := &Service{db: db}
	args := map[string]any{"type_name": "LegalParagraph", "include_relationships": true, "branch": "plan/q3"}

	entities := runEnrichedEntityQuery(t, svc, scopedUnrestrictedContext(t), projectID, args)
	require.Len(t, entities, 1, "only the branch source entity is returned")
	require.Len(t, entities[0].Relationships, 1, "explicit-branch run must enrich with the branch relationship")
	assert.Equal(t, dstID, entities[0].Relationships[0].TargetID)
	assert.Equal(t, "RELATES_TO", entities[0].Relationships[0].Type)
}

// runListEntityTypes executes entity-type-list under the given context and
// decodes the wrapped EntityTypesResult payload.
func runListEntityTypes(t *testing.T, svc *Service, ctx context.Context, projectID string, args map[string]any) EntityTypesResult {
	t.Helper()
	res, err := svc.executeListEntityTypes(ctx, projectID, args)
	require.NoError(t, err)
	text := toolResultText(t, res, nil)
	var result EntityTypesResult
	require.NoError(t, json.Unmarshal([]byte(text), &result))
	return result
}

// enrichedEntity mirrors the enriched result shape entity-query returns when
// include_relationships=true (the entityWithRels wrapper).
type enrichedEntity struct {
	ID            string `json:"id"`
	Relationships []struct {
		Type       string `json:"type"`
		TargetID   string `json:"target_id"`
		TargetType string `json:"target_type"`
		TargetKey  string `json:"target_key"`
	} `json:"relationships"`
}

// runEnrichedEntityQuery executes entity-query with include_relationships and
// decodes the enriched entities (id + relationships) from the envelope.
func runEnrichedEntityQuery(t *testing.T, svc *Service, ctx context.Context, projectID string, args map[string]any) []enrichedEntity {
	t.Helper()
	res, err := svc.executeQueryEntities(ctx, projectID, args)
	require.NoError(t, err)
	text := toolResultText(t, res, nil)
	var env struct {
		Data struct {
			Entities []enrichedEntity `json:"entities"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &env))
	return env.Data.Entities
}
