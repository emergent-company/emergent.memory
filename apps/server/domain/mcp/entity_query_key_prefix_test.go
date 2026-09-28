package mcp

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/jackc/pgx/v5"
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

// TestKeyPrefixRangeClause pins the UTF-8-safe upper bound used to turn a
// key_prefix into an indexable range (issue #1191). The bound must stay valid
// UTF-8 for every prefix — a bytewise increment of a prefix ending in 0x7F or a
// 0xBF continuation byte emits invalid UTF-8 and either errors (SQLSTATE 22021)
// or silently returns the wrong rows.
func TestKeyPrefixRangeClause(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		wantUpper  string
		wantFallbk bool
	}{
		{name: "alphanumeric suffix", prefix: "lov/1997-06-13-44#kapittel-2", wantUpper: "lov/1997-06-13-44#kapittel-3"},
		{name: "punctuation suffix", prefix: "lov/1997-06-13-44#", wantUpper: "lov/1997-06-13-44$"},
		{name: "plain word", prefix: "abc", wantUpper: "abd"},
		{name: "trailing DEL (0x7f)", prefix: "adv\x7f", wantUpper: "adv\u0080"},
		{name: "trailing U+00BF continuation byte", prefix: "adv\u00bf", wantUpper: "adv\u00c0"},
		{name: "trailing U+00FF", prefix: "adv\u00ff", wantUpper: "adv\u0100"},
		{name: "multibyte last rune", prefix: "adv\u65e5", wantUpper: "adv\u65e6"},
		{name: "carry past trailing max rune", prefix: "a\U0010FFFF", wantUpper: "b"},
		{name: "only max rune has no successor", prefix: "\U0010FFFF", wantFallbk: true},
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
			require.Len(t, args, 2)
			upper, ok := args[1].(string)
			require.True(t, ok)
			assert.Equal(t, tc.wantUpper, upper)
			assert.True(t, utf8.ValidString(upper), "upper bound %q must be valid UTF-8", upper)
			assert.True(t, upper > tc.prefix, "upper bound %q must exceed prefix %q", upper, tc.prefix)
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

// legacyBytewiseUpperBound reproduces the pre-#1197-follow-up bound (increment
// the last byte) so the invalid-UTF-8 regression can be pinned in a test.
func legacyBytewiseUpperBound(prefix string) string {
	b := []byte(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xFF {
			b[i]++
			return string(b[:i+1])
		}
	}
	return ""
}

// TestExecuteQueryEntities_KeyPrefixNonASCIIUpperBound is the #1197 follow-up
// regression: for prefixes that are valid UTF-8 but end in a byte whose
// increment is invalid UTF-8 (0x7F, the 0xBF continuation byte of U+00BF, …),
// the derived range must stay valid UTF-8 and return exactly the starts_with
// row set — not error and not over/under-match.
func TestExecuteQueryEntities_KeyPrefixNonASCIIUpperBound(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	for _, k := range []string{
		"adv\x7f#p1", "adv\x7f#p2",
		"adv\u00bf#p1", "adv\u00bf#p2",
		"adv\u00ff#p1",
		"adv\u65e5#p1",
		"adv\u0100#p1",
		"adv#p1",
		"other#p1",
	} {
		insertQueryEntity(t, db, projectID, k, map[string]any{"name": "x"})
	}

	const countBase = `
		SELECT COUNT(*) AS n
		FROM kb.graph_objects go
		WHERE go.deleted_at IS NULL AND go.project_id = ?::uuid
			AND go.supersedes_id IS NULL AND go.branch_id IS NULL
			AND go.type = 'LegalParagraph'`

	for _, prefix := range []string{"adv\x7f", "adv\u00bf", "adv\u00ff", "adv\u65e5", "adv\u0100", "adv", "\U0010FFFF"} {
		clause, args := keyPrefixRangeClause(prefix)
		if len(args) == 2 {
			require.True(t, utf8.ValidString(args[1].(string)), "bound for %q must be valid UTF-8", prefix)
		}
		var viaRange, viaStarts int
		require.NoError(t, db.NewRaw(countBase+clause, append([]any{projectID}, args...)...).Scan(context.Background(), &viaRange),
			"range must not error for prefix %q", prefix)
		require.NoError(t, db.NewRaw(countBase+" AND starts_with(go.key, ?)", projectID, prefix).Scan(context.Background(), &viaStarts))
		assert.Equal(t, viaStarts, viaRange, "prefix %q: range must equal starts_with", prefix)
	}
}

// TestKeyPrefixLegacyBoundRejectedAsInvalidUTF8 proves the pre-fix bound was the
// defect: on a text-protocol client it trips SQLSTATE 22021, while the new
// rune-increment bound is accepted.
func TestKeyPrefixLegacyBoundRejectedAsInvalidUTF8(t *testing.T) {
	dsn, ok := testdb.URL()
	if !ok {
		testdb.SkipOrFatal(t, "TEST_DATABASE_URL not set; 22021 check needs a database")
	}
	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	for _, prefix := range []string{"adv\x7f", "adv\u00bf"} {
		t.Run(prefix, func(t *testing.T) {
			legacy := legacyBytewiseUpperBound(prefix)
			require.False(t, utf8.ValidString(legacy), "legacy bound %q should be invalid UTF-8", legacy)
			var s string
			err := conn.QueryRow(context.Background(), "SELECT $1::text", legacy).Scan(&s)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "22021")

			_, args := keyPrefixRangeClause(prefix)
			require.Len(t, args, 2)
			require.True(t, utf8.ValidString(args[1].(string)))
			require.NoError(t, conn.QueryRow(context.Background(), "SELECT $1::text", args[1].(string)).Scan(&s))
		})
	}
}

// TestExecuteQueryEntities_RelationshipEnrichmentErrorIsSurfaced pins the
// fail-loud enrichment path: an include_relationships=true call whose enrichment
// query fails must return an error, not ok:true with relationships silently
// dropped (#1187 class).
func TestExecuteQueryEntities_RelationshipEnrichmentErrorIsSurfaced(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	insertQueryEntity(t, db, projectID, "law-1#p1", map[string]any{"name": "x"})

	if _, err := db.ExecContext(context.Background(), "ALTER TABLE kb.graph_relationships RENAME TO graph_relationships_hidden"); err != nil {
		t.Fatalf("hide relationships table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "ALTER TABLE kb.graph_relationships_hidden RENAME TO kb.graph_relationships")
	})

	svc := &Service{db: db}
	res, err := svc.executeQueryEntities(context.Background(), projectID, map[string]any{
		"type_name":             "LegalParagraph",
		"include_relationships": true,
	})
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "enrich relationships")
}
