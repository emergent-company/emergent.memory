package graph

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	_ "github.com/uptrace/bun/driver/pgdriver"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// newRenderRepo builds a Repository backed by a non-connecting *bun.DB, so the
// predicate builders can be rendered to SQL without a live Postgres. sql.Open
// never dials; it only needs the "pg" driver name to be registered (the blank
// pgdriver import above), so no connection — and no shared database — is touched.
func newRenderRepo(t *testing.T) *Repository {
	t.Helper()
	sqldb, err := sql.Open("pg", "postgres://127.0.0.1:1/none?sslmode=disable")
	require.NoError(t, err)
	db := bun.NewDB(sqldb, pgdialect.New())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	return NewRepository(db, log, cfg)
}

// splitTopLevelAND splits a bun WHERE clause into its top-level predicate
// fragments, respecting parentheses so nested IN(...) lists and the id IN
// (subselect) cap stay intact. Each fragment has its single surrounding bun
// parenthesis stripped.
func splitTopLevelAND(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ' ':
			if depth == 0 && strings.HasPrefix(s[i:], " AND ") {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				i += 4
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	for i, p := range parts {
		if len(p) >= 2 && p[0] == '(' && p[len(p)-1] == ')' {
			parts[i] = p[1 : len(p)-1]
		}
	}
	return parts
}

// extractInnerOuterWhere renders an applyBulkFilterToUpdate update query and
// returns (outer predicates, inner subselect predicates). The outer list is the
// WHERE the UPDATE would accept; the inner list is the WHERE of the
// id IN (SELECT ...) cap that the LIMIT truncates. The invariant under test is
// that these two lists are identical.
func extractInnerOuterWhere(t *testing.T, sql string) (outer, inner []string) {
	t.Helper()
	const whereMark = " WHERE "
	whereIdx := strings.Index(sql, whereMark)
	require.True(t, whereIdx >= 0, "no WHERE in %q", sql)
	outerRegion := sql[whereIdx+len(whereMark):]

	const capMark = " AND (id IN ("
	capIdx := strings.Index(outerRegion, capMark)
	require.True(t, capIdx >= 0, "no id IN cap in %q", outerRegion)
	outer = splitTopLevelAND(outerRegion[:capIdx])

	cap := outerRegion[capIdx:]
	innerWhere := strings.Index(cap, " WHERE ")
	require.True(t, innerWhere >= 0, "no inner WHERE in %q", cap)
	innerRegion := cap[innerWhere+len(" WHERE "):]
	orderIdx := strings.Index(innerRegion, " ORDER BY ")
	require.True(t, orderIdx >= 0, "no ORDER BY in %q", innerRegion)
	inner = splitTopLevelAND(innerRegion[:orderIdx])
	return outer, inner
}

// TestBulkActionFilterInnerOuterPredicatesAgree asserts the invariant that the
// #798 fix depends on: the predicate set the LIMIT-enforcing inner subselect
// uses must equal, predicate-for-predicate, the predicate set the outer UPDATE
// applies. With the single bulkFilterPredicates builder both sides share one
// source, but this test renders the finished query and compares the two sides
// directly so a future edit that reintroduces a divergence fails loudly.
func TestBulkActionFilterInnerOuterPredicatesAgree(t *testing.T) {
	repo := newRenderRepo(t)
	pid := uuid.New()
	ca := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	ns := "default"

	cases := []struct {
		name            string
		filter          BulkActionFilter
		resolvedFilters []PropertyFilter
	}{
		{"empty", BulkActionFilter{}, nil},
		{"types", BulkActionFilter{Types: []string{"a", "b"}}, nil},
		{"labels", BulkActionFilter{Labels: []string{"x", "y"}}, nil},
		{"namespace", BulkActionFilter{Namespace: &ns}, nil},
		{"created_after", BulkActionFilter{CreatedAfter: &ca}, nil},
		{"property_eq", BulkActionFilter{}, []PropertyFilter{{Path: "name", Op: "eq", Value: "alice"}}},
		{"property_neq", BulkActionFilter{}, []PropertyFilter{{Path: "name", Op: "neq", Value: "alice"}}},
		{"property_gt", BulkActionFilter{}, []PropertyFilter{{Path: "age", Op: "gt", Value: 21}}},
		{"property_gte", BulkActionFilter{}, []PropertyFilter{{Path: "age", Op: "gte", Value: 21}}},
		{"property_lt", BulkActionFilter{}, []PropertyFilter{{Path: "age", Op: "lt", Value: 21}}},
		{"property_lte", BulkActionFilter{}, []PropertyFilter{{Path: "age", Op: "lte", Value: 21}}},
		{"property_contains", BulkActionFilter{}, []PropertyFilter{{Path: "bio", Op: "contains", Value: "dev"}}},
		{"property_exists", BulkActionFilter{}, []PropertyFilter{{Path: "address.city", Op: "exists"}}},
		{"property_in", BulkActionFilter{}, []PropertyFilter{{Path: "status", Op: "in", Value: []interface{}{"a", "b"}}}},
		{"combined", BulkActionFilter{Types: []string{"T"}, Labels: []string{"L"}, Namespace: &ns, CreatedAfter: &ca}, []PropertyFilter{{Path: "p", Op: "eq", Value: "v"}, {Path: "n", Op: "gte", Value: 3}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := repo.applyBulkFilterToUpdate(
				repo.db.NewUpdate().TableExpr("kb.graph_objects").Set("status = ?", "x"),
				pid, tc.filter, tc.resolvedFilters, 5)
			outer, inner := extractInnerOuterWhere(t, q.String())
			require.NotEmpty(t, outer, "outer predicate set must not be empty")
			require.NotEmpty(t, inner, "inner predicate set must not be empty")
			assert.Equal(t, outer, inner, "inner subselect and outer update predicate sets must agree")
		})
	}
}

// TestBulkActionFilterPredicatesSharedByAllExpansions renders the count query,
// the hard-delete subselect, and the update inner subselect and asserts all four
// expansion points (count, hard_delete subselect, outer UPDATE, inner subselect)
// expand to the same predicate set. The outer UPDATE and inner subselect are
// compared via extractInnerOuterWhere above; the count query and hard-delete
// subselect are rendered directly here from the shared builder and checked
// against the update's outer predicate set.
func TestBulkActionFilterPredicatesSharedByAllExpansions(t *testing.T) {
	repo := newRenderRepo(t)
	pid := uuid.New()
	ca := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	ns := "default"
	filter := BulkActionFilter{Types: []string{"T"}, Labels: []string{"L"}, Namespace: &ns, CreatedAfter: &ca}
	resolved := []PropertyFilter{{Path: "p", Op: "eq", Value: "v"}}

	// Update: render outer + inner via the production helper.
	upd := repo.applyBulkFilterToUpdate(
		repo.db.NewUpdate().TableExpr("kb.graph_objects").Set("status = ?", "x"),
		pid, filter, resolved, 5)
	outer, inner := extractInnerOuterWhere(t, upd.String())

	// Count query: build exactly as BulkActionByFilter does.
	countQ := repo.db.NewSelect().TableExpr("kb.graph_objects").ColumnExpr("COUNT(*) AS cnt")
	countQ = applyPredicates(countQ, bulkFilterPredicates(pid, filter, resolved))
	countSQL := countQ.String()
	countRegion := countSQL[strings.Index(countSQL, " WHERE ")+len(" WHERE "):]
	count := splitTopLevelAND(countRegion)

	// Hard-delete subselect: build exactly as BulkActionByFilter does.
	subQ := repo.db.NewSelect().TableExpr("kb.graph_objects").ColumnExpr("id").
		OrderExpr("created_at ASC, id ASC").Limit(5)
	subQ = applyPredicates(subQ, bulkFilterPredicates(pid, filter, resolved))
	subSQL := subQ.String()
	subRegion := subSQL[strings.Index(subSQL, " WHERE ")+len(" WHERE "):]
	subRegion = subRegion[:strings.Index(subRegion, " ORDER BY ")]
	hardDel := splitTopLevelAND(subRegion)

	assert.Equal(t, outer, inner, "outer UPDATE and inner subselect must agree")
	assert.Equal(t, outer, count, "count query and outer UPDATE must agree")
	assert.Equal(t, outer, hardDel, "hard-delete subselect and outer UPDATE must agree")
}

// insertBulkFilterObject inserts a HEAD graph object with explicit type, status,
// namespace, labels, properties, and created_at, so filter dimensions other than
// type (which the earlier deterministic-limit tests ignore) can be exercised.
func insertBulkFilterObject(t *testing.T, db *bun.DB, projectID, id uuid.UUID, typ, ns string, labels []string, props, status string, createdAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, namespace, content_hash, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, ?, ?::jsonb, ?::text[], ?, ?, ?, ?)
	`, id, projectID, id, typ, status, props, formatTextArray(labels), ns, fmt.Sprintf("hash-%s", id), createdAt, createdAt)
	require.NoError(t, err)
}

// TestBulkActionUpdate_LimitCountsExactlyAcceptedRows proves, against a real
// database, that the LIMIT-enforcing inner subselect accepts exactly the same
// rows the outer UPDATE would accept when the filter spans every dimension
// (type, label, namespace, created_after, and a property filter). If the inner
// and outer predicate sets ever diverge, the LIMIT would cap a different set
// than the update accepts, and `affected`/`archived` would not equal the 2
// oldest matching objects.
func TestBulkActionUpdate_LimitCountsExactlyAcceptedRows(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	pid := uuid.New()
	seedProject(t, db, pid)

	ns := "ns"
	cutoff := time.Now().UTC().Truncate(time.Microsecond).Add(-1 * time.Hour)

	matching := descendingUUIDs(6)
	expectedTwo := []uuid.UUID{matching[0], matching[1]} // created_at strictly increasing with index
	for i, id := range matching {
		createdAt := cutoff.Add(1*time.Hour + time.Duration(i)*time.Minute)
		insertBulkFilterObject(t, db, pid, id, "T", ns, []string{"keep"}, `{"p":"v"}`, "active", createdAt)
	}

	// Controls: each fails exactly one filter dimension, after the cutoff so
	// created_after alone would still admit them.
	insertBulkFilterObject(t, db, pid, uuid.New(), "Other", ns, []string{"keep"}, `{"p":"v"}`, "active", cutoff.Add(2*time.Hour))
	insertBulkFilterObject(t, db, pid, uuid.New(), "T", "other", []string{"keep"}, `{"p":"v"}`, "active", cutoff.Add(2*time.Hour))
	insertBulkFilterObject(t, db, pid, uuid.New(), "T", ns, []string{"drop"}, `{"p":"v"}`, "active", cutoff.Add(2*time.Hour))
	insertBulkFilterObject(t, db, pid, uuid.New(), "T", ns, []string{"keep"}, `{"p":"w"}`, "active", cutoff.Add(2*time.Hour))
	// Fails created_after only.
	insertBulkFilterObject(t, db, pid, uuid.New(), "T", ns, []string{"keep"}, `{"p":"v"}`, "active", cutoff.Add(-2*time.Hour))

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", pid)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", pid)
	})

	matched, affected, err := repo.BulkActionByFilter(ctx, BulkActionParams{
		ProjectID: pid,
		Action:    BulkActionUpdateStatus,
		Value:     "archived",
		Limit:     2,
		Filter: BulkActionFilter{
			Types:           []string{"T"},
			Labels:          []string{"keep"},
			Namespace:       &ns,
			CreatedAfter:    &cutoff,
			PropertyFilters: []PropertyFilter{{Path: "p", Op: "eq", Value: "v"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 6, matched, "exactly the 6 matching objects must be counted")
	assert.Equal(t, 2, affected, "limit must cap at exactly 2")

	var archived []uuid.UUID
	err = db.NewSelect().TableExpr("kb.graph_objects").Column("id").
		Where("project_id = ?", pid).
		Where("status = ?", "archived").
		OrderExpr("created_at ASC, id ASC").
		Scan(ctx, &archived)
	require.NoError(t, err)
	assert.Equal(t, expectedTwo, archived, "exactly the 2 oldest matching objects must be archived")
}
