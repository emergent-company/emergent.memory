package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// seedEntityQuerySortVolume bulk-inserts LegalParagraph HEAD rows shaped like
// the statute corpus used to reproduce #1206: keys
// lov/law-<l>#kapittel-<c>-paragraf-<p>, distinct `name` properties, and a
// boundary-sized content blob. branchArg selects the branch (nil = main).
func seedEntityQuerySortVolume(t *testing.T, db bun.IDB, projectID string, branchArg any, laws, chapters, paras int) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at)
		SELECT
			gen_random_uuid(), ?::uuid, ?::uuid, gen_random_uuid(), NULL, 1, 'LegalParagraph',
			'lov/law-' || l || '#kapittel-' || c || '-paragraf-' || p, 'active',
			jsonb_build_object('name', 'p-'||l||'-'||c||'-'||p,
				'content', repeat('x', 256),
				'law_ref_id', 'lov/law-'||l, 'chapter_id', 'kapittel-'||c),
			'{}'::text[], now() - (random() * interval '2 hours'), now()
		FROM generate_series(1, ?) l,
		     generate_series(1, ?) c,
		     generate_series(1, ?) p
	`, projectID, branchArg, laws, chapters, paras)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), "ANALYZE kb.graph_objects")
	require.NoError(t, err)
}

const sortNameQuery = `
	SELECT go.id, go.key, COALESCE(go.properties->>'name','') AS name
	FROM kb.graph_objects go
	WHERE go.deleted_at IS NULL AND go.project_id = ?::uuid
		AND go.supersedes_id IS NULL
		AND go.branch_id IS NULL
		AND go.type = 'LegalParagraph'
	ORDER BY go.properties->>'name' DESC NULLS LAST
	LIMIT ? OFFSET 0`

// TestExecuteQueryEntities_NameSortUsesExpressionIndex is the #1206 ordering
// regression: a whole-type sort_by=name (no narrowing key_prefix) must be
// served by idx_graph_objects_project_type_name as an ordered index scan, with
// no Sort node and no scan of the graph_objects heap. Before the index the
// planner materialised every HEAD row of the type and sorted it.
func TestExecuteQueryEntities_NameSortUsesExpressionIndex(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	seedEntityQuerySortVolume(t, db, projectID, nil, 1000, 5, 10) // 50k rows

	plan := keyPrefixExplain(t, db, sortNameQuery, projectID, 25)
	require.Contains(t, plan, "Index Scan using idx_graph_objects_project_type_name", plan)
	require.NotContains(t, plan, "Seq Scan on graph_objects", plan)
	require.NotContains(t, plan, "Sort", plan)

	type nameRow struct {
		ID   uuid.UUID `bun:"id"`
		Key  string    `bun:"key"`
		Name string    `bun:"name"`
	}
	var expected []nameRow
	require.NoError(t, db.NewRaw(sortNameQuery, projectID, 25).Scan(context.Background(), &expected))
	require.Len(t, expected, 25)

	svc := &Service{db: db}
	out := runEntityQuery(t, svc, projectID, map[string]any{
		"type_name": "LegalParagraph",
		"sort_by":   "name",
		"limit":     float64(25),
	})
	require.Len(t, out.Entities, 25)
	for i, want := range expected {
		assert.Equal(t, want.Key, out.Entities[i].Key, "row %d key", i)
		assert.Equal(t, want.Name, out.Entities[i].Name, "row %d name", i)
		if i > 0 {
			assert.GreaterOrEqual(t, out.Entities[i-1].Name, out.Entities[i].Name,
				"names must be sorted descending: %q before %q", out.Entities[i-1].Name, out.Entities[i].Name)
		}
	}
}

const branchKeyPrefixQuery = `
	SELECT go.id, go.key, COALESCE(go.properties->>'name','') AS name
	FROM kb.graph_objects go
	WHERE go.deleted_at IS NULL AND go.project_id = ?::uuid
		AND go.supersedes_id IS NULL
		AND go.branch_id = ?::uuid
		AND go.type = 'LegalParagraph'
		AND go.key COLLATE "C" >= ? AND go.key COLLATE "C" < ?`

// TestExecuteQueryEntities_BranchKeyPrefixUsesBranchIndex pins the branch-scoped
// key_prefix residual: the head-main partial index (00198) cannot serve a branch
// query, so without the COLLATE "C" branch index the range degrades to a scan of
// the branch's rows of the type. The index must serve both bounds as an index
// condition and the tool must still scope results to the prefix.
func TestExecuteQueryEntities_BranchKeyPrefixUsesBranchIndex(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)

	branchID := uuid.NewString()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO kb.branches (id, project_id, name) VALUES (?::uuid, ?::uuid, ?)`,
		branchID, projectID, "sort-branch")
	require.NoError(t, err)
	seedEntityQuerySortVolume(t, db, projectID, branchID, 1000, 5, 10) // 50k branch rows

	prefix := "lov/law-500#kapittel-3-"
	_, boundArgs := keyPrefixRangeClause(prefix)
	plan := keyPrefixExplain(t, db,
		branchKeyPrefixQuery+" LIMIT 25",
		append([]any{projectID, branchID}, boundArgs...)...)
	require.Contains(t, plan, "Index Scan using idx_graph_objects_project_branch_type_key_c", plan)
	require.NotContains(t, plan, "Seq Scan on graph_objects", plan)
	require.Contains(t, plan, "Index Cond", plan)

	svc := &Service{db: db}
	out := runEntityQuery(t, svc, projectID, map[string]any{
		"type_name":      "LegalParagraph",
		"branch":         "sort-branch",
		"key_prefix":     prefix,
		"field_strategy": "full",
		"limit":          float64(25),
	})
	require.NotEmpty(t, out.Entities)
	for _, e := range out.Entities {
		assert.True(t, strings.HasPrefix(e.Key, prefix), "key %q must start with %q", e.Key, prefix)
	}
	assert.Empty(t, out.Warning, "key_prefix must be a recognized parameter (no warning)")
}
