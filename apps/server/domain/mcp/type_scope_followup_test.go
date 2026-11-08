package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// =============================================================================
// Item A — WRITE-path enforcement
// =============================================================================

func TestTypeScopeFollowup_EntityCreateRejectsExcludedType(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	ctx := scopedCtx([]string{"Law"}, nil)

	res, err := svc.executeBatchCreateEntities(ctx, projectID, map[string]any{
		"entities": []any{map[string]any{"type": "Person", "key": "p1"}},
	})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)
	assert.NotContains(t, text, "Person")

	var out struct {
		OK   bool `json:"ok"`
		Meta struct {
			Created int `json:"created"`
			Failed  int `json:"failed"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.False(t, out.OK)
	assert.Equal(t, 0, out.Meta.Created)
	assert.Equal(t, 1, out.Meta.Failed)
}

func TestTypeScopeFollowup_EntityCreateAllowsScopedType(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	ctx := scopedCtx([]string{"Law"}, nil)

	res, err := svc.executeBatchCreateEntities(ctx, projectID, map[string]any{
		"entities": []any{map[string]any{"type": "Law", "key": "law-1"}},
	})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var out struct {
		OK   bool `json:"ok"`
		Meta struct {
			Created int `json:"created"`
			Failed  int `json:"failed"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.True(t, out.OK)
	assert.Equal(t, 1, out.Meta.Created)
	assert.Equal(t, 0, out.Meta.Failed)
}

func TestTypeScopeFollowup_EntityCreateUnrestrictedCreatesPerson(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	ctx := context.Background()

	res, err := svc.executeBatchCreateEntities(ctx, projectID, map[string]any{
		"entities": []any{map[string]any{"type": "Person", "key": "p1"}},
	})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)
	assert.Contains(t, text, "Person")

	var out struct {
		OK   bool `json:"ok"`
		Meta struct {
			Created int `json:"created"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.True(t, out.OK)
	assert.Equal(t, 1, out.Meta.Created)
}

func TestTypeScopeFollowup_RelationshipCreateRejectsExcludedType(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}
	ctx := scopedCtx(nil, []string{"CITES"})

	res, err := svc.executeBatchCreateRelationships(ctx, projectID, map[string]any{
		"relationships": []any{map[string]any{
			"type":      "OWNS",
			"source_id": uuid.NewString(),
			"target_id": uuid.NewString(),
		}},
	})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)
	assert.NotContains(t, text, "OWNS")

	var out struct {
		OK   bool `json:"ok"`
		Meta struct {
			Created int `json:"created"`
			Failed  int `json:"failed"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.False(t, out.OK)
	assert.Equal(t, 0, out.Meta.Created)
	assert.Equal(t, 1, out.Meta.Failed)
}

func TestTypeScopeFollowup_UpdateEntityRejectsExcludedType(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	personID := insertGraphObject(t, db, projectID, "Person", "person-1")
	svc := newEntityCreateDedupService(t, db)
	ctx := scopedCtx([]string{"Law"}, nil)

	_, err := svc.executeUpdateEntity(ctx, projectID, map[string]any{
		"entity_id":  personID,
		"properties": map[string]any{"name": "x"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object type is not allowed for this agent's scope")
}

// entity-restore echoes the restored object (incl. its type), so it is gated
// too. The row is soft-deleted, exercising the include-deleted lookup. Removing
// the gate makes the NotEqual/NoError path fail (the restore succeeds).
func TestTypeScopeFollowup_EntityRestoreRejectsExcludedType(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	personID := insertGraphObject(t, db, projectID, "Person", "person-restore")
	svc := newEntityCreateDedupService(t, db)

	// Soft-delete the Person so it can be restored.
	require.NoError(t, svc.graphService.Delete(context.Background(), uuid.MustParse(projectID), uuid.MustParse(personID), nil, nil, nil))

	ctx := scopedCtx([]string{"Law"}, nil)
	_, err := svc.executeRestoreEntity(ctx, projectID, map[string]any{"entity_id": personID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object type is not allowed for this agent's scope")

	// An empty scope leaves restore unchanged.
	res, err := svc.executeRestoreEntity(context.Background(), projectID, map[string]any{"entity_id": personID})
	require.NoError(t, err)
	assert.Contains(t, toolResultText(t, res, nil), "Person")
}

func TestTypeScopeFollowup_UpdateRelationshipRejectsExcludedType(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	src := insertGraphObject(t, db, projectID, "Law", "law-1")
	dst := insertGraphObject(t, db, projectID, "Law", "law-2")
	relID := insertRelationship(t, db, projectID, "KNOWS", src, dst)
	svc := newEntityCreateDedupService(t, db)
	ctx := scopedCtx([]string{"Law"}, []string{"CITES"})

	_, err := svc.executeUpdateRelationship(ctx, projectID, map[string]any{
		"relationship_id": relID,
		"properties":      map[string]any{"note": "x"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relationship type is not allowed for this agent's scope")
}

// Item D: with an OBJECT-only scope (relationships unrestricted) an edge whose
// endpoint is an excluded object type must still be dropped. This is the exact
// scenario the issue calls out ("no returned edge references an object id outside
// the object allowlist"): removing the object-axis endpoint cross-check makes it
// fail.
func TestTypeScopeFollowup_TraverseObjectOnlyDropsDanglingEdge(t *testing.T) {
	scope := NewTypeScope([]string{"Law"}, nil) // object-only: relationship axis unrestricted
	law := uuid.New()
	lawC := uuid.New()
	person := uuid.New()
	personC := uuid.New()

	res := &graph.TraverseGraphResponse{
		Nodes: []*graph.TraverseNode{
			{ID: law, CanonicalID: lawC, Type: "Law"},
			{ID: person, CanonicalID: personC, Type: "Person"},
		},
		Edges: []*graph.TraverseEdge{
			{Type: "REFERENCES", SrcID: law, DstID: law},    // both allowed → kept
			{Type: "REFERENCES", SrcID: law, DstID: person}, // excluded endpoint → dropped
		},
	}

	out := filterTraverseResponseByScope(scope, res)
	require.Len(t, out.Nodes, 1)
	require.Len(t, out.Edges, 1)
	assert.Equal(t, law, out.Edges[0].DstID)
	assert.NotEqual(t, person, out.Edges[0].DstID)
}

// An edge whose endpoint is off the paginated node page has an UNKNOWN type and
// must be kept — the unscoped traverse service keeps an edge when either
// endpoint is on the page, so dropping it here would silently lose legitimate
// cross-page edges for scoped agents. A fix that drops unknown endpoints makes
// this fail.
func TestTypeScopeFollowup_TraverseUnknownEndpointEdgeKept(t *testing.T) {
	scope := NewTypeScope([]string{"Law"}, nil)
	law := uuid.New()
	offPage := uuid.New() // reachable, but not in this page's node set

	res := &graph.TraverseGraphResponse{
		Nodes: []*graph.TraverseNode{{ID: law, CanonicalID: law, Type: "Law"}},
		Edges: []*graph.TraverseEdge{{Type: "REFERENCES", SrcID: law, DstID: offPage}},
	}

	out := filterTraverseResponseByScope(scope, res)
	require.Len(t, out.Edges, 1, "an off-page endpoint has unknown type and must not drop the edge")
}

// Inline relationships declared on an entity-create spec whose relationship type
// is excluded must be reported as a failed per-item result (issue #401), not
// silently dropped. The entity is still created, but the batch must report
// ok=false with a machine-readable reason so the caller knows the write was
// partial. Removing the inline guard creates the OWNS relationship and reports
// success, failing both assertions.
func TestTypeScopeFollowup_EntityCreateSkipsExcludedInlineRelationship(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	targetID := insertGraphObject(t, db, projectID, "Law", "law-target")
	svc := newEntityCreateDedupService(t, db)
	ctx := scopedCtx([]string{"Law"}, []string{"CITES"})

	res, err := svc.executeBatchCreateEntities(ctx, projectID, map[string]any{
		"entities": []any{map[string]any{
			"type": "Law",
			"key":  "law-source",
			"relationships": []any{map[string]any{
				"type":      "OWNS", // excluded relationship type
				"target_id": targetID,
			}},
		}},
	})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Results []struct {
				OK     bool `json:"ok"`
				Entity struct {
					Relationships       []any `json:"relationships"`
					RelationshipsFailed int   `json:"relationships_failed"`
					RelationshipResults []struct {
						OK     bool   `json:"ok"`
						Index  int    `json:"index"`
						Reason string `json:"reason"`
					} `json:"relationship_results"`
				} `json:"entity"`
			} `json:"results"`
		} `json:"data"`
		Meta struct {
			Created             int `json:"created"`
			Failed              int `json:"failed"`
			RelationshipsFailed int `json:"relationships_failed"`
			RelationshipsTotal  int `json:"relationships_total"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &out))

	assert.False(t, out.OK, "a batch with a failed inline relationship must not report ok=true")
	assert.Equal(t, 1, out.Meta.Created, "the allowed object is still created")
	assert.Equal(t, 1, out.Meta.RelationshipsFailed)
	assert.Equal(t, 1, out.Meta.RelationshipsTotal)

	require.Len(t, out.Data.Results, 1)
	item := out.Data.Results[0]
	assert.True(t, item.OK, "the entity itself was created")
	assert.Empty(t, item.Entity.Relationships, "an excluded inline relationship must not be created")
	assert.Equal(t, 1, item.Entity.RelationshipsFailed)
	require.Len(t, item.Entity.RelationshipResults, 1)
	assert.False(t, item.Entity.RelationshipResults[0].OK)
	assert.Equal(t, inlineRelReasonTypeNotAllowed, item.Entity.RelationshipResults[0].Reason)
}

// An edge whose endpoint object type is outside the agent's object scope must
// be refused by relationship-create, exactly as entity-create's inline path
// refuses it. The two write paths must agree (#401 review).
func TestTypeScopeFollowup_RelationshipCreateRejectsOutOfScopeEndpoint(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	inScope := insertGraphObject(t, db, projectID, "Law", "law-in-scope")
	outOfScope := insertGraphObject(t, db, projectID, "Person", "person-out-of-scope")
	svc := newEntityCreateDedupService(t, db)
	ctx := scopedCtx([]string{"Law"}, []string{"CITES"})

	res, err := svc.executeBatchCreateRelationships(ctx, projectID, map[string]any{
		"relationships": []any{map[string]any{
			"type":      "CITES",
			"source_id": inScope,
			"target_id": outOfScope,
		}},
	})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)

	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Results []struct {
				OK     bool   `json:"ok"`
				Reason string `json:"reason"`
			} `json:"results"`
		} `json:"data"`
		Meta struct {
			Created int `json:"created"`
			Failed  int `json:"failed"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.False(t, out.OK)
	assert.Equal(t, 0, out.Meta.Created)
	assert.Equal(t, 1, out.Meta.Failed)
	require.Len(t, out.Data.Results, 1)
	assert.False(t, out.Data.Results[0].OK)
	assert.Equal(t, inlineRelReasonTypeNotAllowed, out.Data.Results[0].Reason)
}

// =============================================================================
// Item C — relationship-list pagination metadata reflects the filtered set
// =============================================================================

func TestTypeScopeFollowup_ListRelationshipsFilteredPagination(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	a := insertGraphObject(t, db, projectID, "Law", "law-a")
	b := insertGraphObject(t, db, projectID, "Law", "law-b")
	c := insertGraphObject(t, db, projectID, "Law", "law-c")

	// Seed excluded KNOWS rows BEFORE the allowed CITES rows, so a naive
	// post-filter (filter after limit+1) would fill the page with excluded rows
	// and under-report the true allowed count. Each KNOWS uses a distinct
	// destination to satisfy the head-main uniqueness constraint.
	for i := 0; i < 20; i++ {
		dst := insertGraphObject(t, db, projectID, "Law", "law-knows-dst-"+uuid.NewString())
		insertRelationship(t, db, projectID, "KNOWS", a, dst)
	}
	insertRelationship(t, db, projectID, "CITES", a, b)
	insertRelationship(t, db, projectID, "CITES", b, c)

	svc := newEntityCreateDedupService(t, db)
	ctx := scopedCtx(nil, []string{"CITES"})

	res, err := svc.executeListRelationships(ctx, projectID, map[string]any{"limit": float64(10)})
	require.NoError(t, err)
	text := toolResultText(t, res, nil)
	assert.NotContains(t, text, "KNOWS")

	m := parseResultMap(t, res)
	assert.Equal(t, float64(2), m["total"])
	assert.Equal(t, false, m["has_more"])
}

// =============================================================================
// Item E — entity-history is type-scoped
// =============================================================================

func TestTypeScopeFollowup_EntityHistoryRejectsExcludedType(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	personID := insertGraphObject(t, db, projectID, "Person", "person-1")
	lawID := insertGraphObject(t, db, projectID, "Law", "law-1")
	svc := &Service{db: db}
	ctx := scopedCtx([]string{"Law"}, nil)

	_, err := svc.executeEntityHistory(ctx, projectID, map[string]any{"entity_id": personID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object type is not allowed for this agent's scope")

	res, err := svc.executeEntityHistory(ctx, projectID, map[string]any{"entity_id": lawID})
	require.NoError(t, err)
	m := parseResultMap(t, res)
	assert.Equal(t, float64(1), m["count"])
}
