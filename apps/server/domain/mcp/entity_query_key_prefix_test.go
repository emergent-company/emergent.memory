package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// TestEntityQueryLimitSchemaStatesFullCap pins the #1188 schema fix: the limit
// property's description (and Maximum) must state the lower full-strategy cap
// so the advertised max of 200 does not contradict the runtime clamp.
func TestEntityQueryLimitSchemaStatesFullCap(t *testing.T) {
	t.Run("default cap", func(t *testing.T) {
		svc := &Service{}
		limit := toolDefByName(t, svc.GetToolDefinitions(), "entity-query").InputSchema.Properties["limit"]
		assert.Contains(t, limit.Description, `field_strategy="full"`)
		assert.Contains(t, limit.Description, "25")
	})
	t.Run("configured cap is reflected", func(t *testing.T) {
		svc := &Service{entityQueryFullMaxLimit: 7}
		limit := toolDefByName(t, svc.GetToolDefinitions(), "entity-query").InputSchema.Properties["limit"]
		assert.Contains(t, limit.Description, "7")
		assert.NotContains(t, limit.Description, "capped to 25")
	})
}

// TestKeyPrefixRangeClause pins the bytewise bound used to turn a key_prefix
// into an indexable range (issue #1191).
func TestKeyPrefixRangeClause(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		wantUpper  string
		wantFallbk bool
	}{
		{name: "alphanumeric suffix", prefix: "lov/1997-06-13-44#kapittel-2", wantUpper: "lov/1997-06-13-44#kapittel-3"},
		{name: "punctuation suffix still bounded bytewise", prefix: "lov/1997-06-13-44#", wantUpper: "lov/1997-06-13-44$"},
		{name: "plain word", prefix: "abc", wantUpper: "abd"},
		{name: "carry past trailing 0xff", prefix: "a\xff", wantUpper: "b"},
		{name: "all 0xff has no successor", prefix: "\xff\xff", wantFallbk: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clause, args := keyPrefixRangeClause(tc.prefix)
			if tc.wantFallbk {
				assert.Equal(t, " AND starts_with(go.key, ?)", clause)
				assert.Equal(t, []any{tc.prefix}, args)
				return
			}
			assert.Equal(t, ` AND go.key COLLATE "C" >= ? AND go.key COLLATE "C" < ?`, clause)
			assert.Equal(t, []any{tc.prefix, tc.wantUpper}, args)
		})
	}
}

// seedKeyPrefixVolume bulk-inserts laws*chapters*paragraphs LegalParagraph head
// rows with canonical keys of the form lov/law-<l>#kapittel-<c>#paragraf-<p>.
func seedKeyPrefixVolume(t *testing.T, db bun.IDB, projectID string, laws, chapters, paras int) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at)
		SELECT
			gen_random_uuid(), ?::uuid, NULL, gen_random_uuid(), NULL, 1, 'LegalParagraph',
			'lov/law-' || l || '#kapittel-' || c || '#paragraf-' || p, 'active',
			jsonb_build_object('name', 'p-'||l||'-'||c||'-'||p, 'content', repeat('x', 256),
				'law_ref_id', 'lov/law-'||l, 'chapter_id', 'kapittel-'||c),
			'{}'::text[], now() - (random() * interval '2 hours'), now()
		FROM generate_series(1, ?) l,
		     generate_series(1, ?) c,
		     generate_series(1, ?) p
	`, projectID, laws, chapters, paras)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(), "ANALYZE kb.graph_objects")
	require.NoError(t, err)
}

func keyPrefixExplain(t *testing.T, db bun.IDB, sql string, args ...any) string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "EXPLAIN (ANALYZE, BUFFERS)"+sql, args...)
	require.NoError(t, err)
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestExecuteQueryEntities_KeyPrefixUsesBytewiseIndex is the #1191 regression:
// the key_prefix predicate must be an indexable byte range served by the
// partial idx_graph_objects_project_type_key_c index (migration 00198), not a
// full heap scan, and it must return exactly the starts_with rows.
func TestExecuteQueryEntities_KeyPrefixUsesBytewiseIndex(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	seedKeyPrefixVolume(t, db, projectID, 1000, 5, 10) // 50k rows

	const base = `
		SELECT go.id, go.key, go.created_at
		FROM kb.graph_objects go
		WHERE go.deleted_at IS NULL AND go.project_id = ?::uuid
			AND go.supersedes_id IS NULL AND go.branch_id IS NULL
			AND go.type = 'LegalParagraph'`
	const countBase = `
		SELECT COUNT(*) AS n
		FROM kb.graph_objects go
		WHERE go.deleted_at IS NULL AND go.project_id = ?::uuid
			AND go.supersedes_id IS NULL AND go.branch_id IS NULL
			AND go.type = 'LegalParagraph'`

	prefix := "lov/law-500#kapittel-3"
	clause, boundArgs := keyPrefixRangeClause(prefix)
	plan := keyPrefixExplain(t, db, base+clause+" ORDER BY go.created_at DESC LIMIT 25 OFFSET 0",
		append([]any{projectID}, boundArgs...)...)
	require.Contains(t, plan, "Index Scan using idx_graph_objects_project_type_key_c", plan)
	require.NotContains(t, plan, "Seq Scan", plan)

	var viaRange, viaStarts int
	require.NoError(t, db.NewRaw(countBase+clause, append([]any{projectID}, boundArgs...)...).Scan(context.Background(), &viaRange))
	require.NoError(t, db.NewRaw(countBase+" AND starts_with(go.key, ?)", projectID, prefix).Scan(context.Background(), &viaStarts))
	assert.Equal(t, viaStarts, viaRange, "bytewise range must match starts_with exactly")
	require.NotZero(t, viaStarts)

	svc := &Service{db: db}
	out := runEntityQuery(t, svc, projectID, map[string]any{
		"type_name":      "LegalParagraph",
		"key_prefix":     prefix,
		"field_strategy": "full",
		"limit":          float64(25),
	})
	require.Len(t, out.Entities, viaStarts)
	for _, e := range out.Entities {
		assert.True(t, strings.HasPrefix(e.Key, prefix), "key %q must start with %q", e.Key, prefix)
	}
}
