package graph_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/migrations"
)

// applyFTSIdentifierMigration applies the Up half of migration 00174's function
// definitions to the throwaway test database. The test database is built from
// the embedded schema.sql snapshot, which still carries the migration 00016
// trigger, so without this the assertions below would exercise the old index
// shape. Only the function definitions are taken (the CONCURRENTLY index
// rebuild stays in the migration proper and is exercised on a scratch database).
func applyFTSIdentifierMigration(t *testing.T, ctx context.Context, db bun.IDB) {
	t.Helper()
	raw, err := migrations.FS.ReadFile("00174_graph_objects_fts_identifiers.sql")
	require.NoError(t, err)

	src := string(raw)
	start := strings.Index(src, "-- +goose Up")
	require.GreaterOrEqual(t, start, 0, "migration is missing the goose Up marker")
	const stopAt = "DROP INDEX CONCURRENTLY IF EXISTS kb.idx_graph_objects_fts"
	end := strings.Index(src, stopAt)
	require.Greater(t, end, start, "migration layout changed; cannot isolate the function definitions")

	_, err = db.ExecContext(ctx, src[start+len("-- +goose Up"):end])
	require.NoError(t, err, "applying migration 00174 function definitions")
}

// countStrictMatchesDual counts objects matched by the production strict query,
// i.e. both text search configurations OR'd together.
func countStrictMatchesDual(t *testing.T, ctx context.Context, db bun.IDB, projectID uuid.UUID, query string) int {
	t.Helper()
	var rows []struct {
		Count int `bun:"count"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT count(*) AS count FROM kb.graph_objects
		WHERE project_id = uuid(?)
		  AND (fts @@ websearch_to_tsquery('simple', ?)
		       OR fts @@ websearch_to_tsquery('norwegian', ?))
	`, projectID.String(), query, query).Scan(ctx, &rows))
	require.Len(t, rows, 1)
	return rows[0].Count
}

// TestFTSIdentifierIndexMakesCompositeKeySearchable is the regression test for
// issue #706: a composite key must be reachable in component form, and the
// query-side dual configuration must match it on the strict pass (so the
// ftsquery.Relax fallback is not what produces the result).
func TestFTSIdentifierIndexMakesCompositeKeySearchable(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	applyFTSIdentifierMigration(t, ctx, db)
	repo := graph.NewRepository(db, slog.Default(), cfg)

	lawID := insertKeyedObject(t, ctx, db, projectID, "Law", "lov/1997-06-13-44",
		`{"title":"Lov om aksjeselskaper (aksjeloven)","description":"Lov om aksjeselskaper og vedtekter"}`)

	// The issue's motivating query: strict dual-config must match directly.
	assert.Equal(t, 1, countStrictMatchesDual(t, ctx, db, projectID, "aksjeloven lov 1997-06-13-44"),
		"strict query should match the composite key without the relaxed fallback")

	cases := []string{
		"aksjeloven lov 1997-06-13-44", // prose + hyphenated identifier
		"lov 1997 06 13 44",            // component terms only
		"lov/1997-06-13-44",            // raw form
		"aksjeselskaper",               // norwegian stemming
	}
	for _, q := range cases {
		results, err := repo.FTSSearch(ctx, graph.FTSSearchParams{ProjectID: projectID, Query: q, Limit: 10})
		require.NoError(t, err, "query %q", q)
		require.Len(t, results, 1, "query %q should return the law", q)
		assert.Equal(t, lawID, results[0].Object.ID, "query %q", q)
	}
}

// TestFTSIdentifierIndexExcludesNonWhitelistedProperties guards the bounded
// whitelist: a distinctive value stored under a property that is not
// title/name/description must not be lexically searchable. A regression that
// indexes the whole properties blob again would fail this assertion, while the
// positive control confirms the object was indexed at all.
func TestFTSIdentifierIndexExcludesNonWhitelistedProperties(t *testing.T) {
	ctx, db, projectID, _ := setupFTSRelaxTest(t)
	applyFTSIdentifierMigration(t, ctx, db)

	insertKeyedObject(t, ctx, db, projectID, "Document", "plain/2020-01-01-1",
		`{"custom_field":"zzquuxzorpmarker","title":"Ordinary title"}`)

	// Positive control: the whitelisted title is searchable, so the object really
	// is indexed and the negative assertion below is not vacuous.
	assert.Equal(t, 1, countStrictMatchesDual(t, ctx, db, projectID, "Ordinary title"),
		"whitelisted title should be searchable")

	// Negative assertion: the non-whitelisted property must not leak into fts.
	assert.Zero(t, countStrictMatchesDual(t, ctx, db, projectID, "zzquuxzorpmarker"),
		"a non-whitelisted property value must not be lexically searchable")
}

// TestFTSIdentifierIndexBoundsPositions guards the second defect: large objects
// must not exhaust the tsvector position budget. The migration indexes a bounded
// whitelist, so MAXENTRYPOS (16383) must never appear in the built vector.
func TestFTSIdentifierIndexBoundsPositions(t *testing.T) {
	ctx, db, projectID, _ := setupFTSRelaxTest(t)
	applyFTSIdentifierMigration(t, ctx, db)

	// A description with far more distinct tokens than MAXENTRYPOS positions.
	var b strings.Builder
	for i := 0; i < 40000; i++ {
		b.WriteString("w")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" ")
	}
	props, err := json.Marshal(map[string]string{"description": b.String()})
	require.NoError(t, err)

	insertKeyedObject(t, ctx, db, projectID, "Document", "huge/2020-01-02-3", string(props))

	var rows []struct {
		Saturated bool `bun:"saturated"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT (fts::text LIKE '%16383%') AS saturated
		FROM kb.graph_objects
		WHERE project_id = uuid(?) AND key = 'huge/2020-01-02-3'
	`, projectID.String()).Scan(ctx, &rows))
	require.Len(t, rows, 1)
	assert.False(t, rows[0].Saturated, "fts positions must stay below MAXENTRYPOS")
}
