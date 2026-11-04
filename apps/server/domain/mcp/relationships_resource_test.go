package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// relationshipsPayload mirrors the JSON the resource hands to the caller. The
// field names are intentionally the Go field names (there are no json tags), so
// this asserts the output contract is unchanged.
type relationshipsPayload struct {
	ProjectID     string `json:"project_id"`
	Relationships []struct {
		Type     string `json:"Type"`
		FromType string `json:"FromType"`
		ToType   string `json:"ToType"`
		Count    int    `json:"Count"`
	} `json:"relationships"`
	Total     int    `json:"total"`
	Timestamp string `json:"timestamp"`
}

func decodeRelationshipsResource(t *testing.T, text string) relationshipsPayload {
	t.Helper()
	var payload relationshipsPayload
	require.NoError(t, json.Unmarshal([]byte(text), &payload))
	return payload
}

// TestReadRelationshipsResourceQueryUsesRealColumns is an always-on regression
// test for issue #1393. It executes the resource over a sqlmock-backed bun.DB
// and captures the generated SQL, proving the query references only real
// kb.graph_relationships columns (r.type) and derives the endpoint types by
// joining kb.graph_objects (as executeListEntityTypes does). Before the fix the
// resource emitted `r.relationship_type`, `r.from_type`, `r.to_type` — columns
// that do not exist — so the expected-rows matcher never fired and the test
// failed.
func TestReadRelationshipsResourceQueryUsesRealColumns(t *testing.T) {
	var captured []string
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		captured = append(captured, actualSQL)
		re, err := regexp.Compile(expectedSQL)
		if err != nil {
			return fmt.Errorf("compile expected pattern: %w", err)
		}
		if !re.MatchString(actualSQL) {
			return fmt.Errorf("actual query %q does not match expected pattern %q", actualSQL, expectedSQL)
		}
		return nil
	})

	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, pgdialect.New())

	// The expectation deliberately requires the join onto kb.graph_objects (both
	// endpoints) and the real aggregate. The old query touched neither.
	mock.ExpectQuery(`(?s)FROM kb\.graph_relationships\s+r\b.*JOIN kb\.graph_objects\s+src ON r\.src_id = src\.id.*JOIN kb\.graph_objects\s+dst ON r\.dst_id = dst\.id`).
		WillReturnRows(sqlmock.NewRows([]string{"type", "from_type", "to_type", "count"}).
			AddRow("works_for", "Person", "Company", 3).
			AddRow("knows", "Person", "Person", 7))

	svc := &Service{db: db}
	res, err := svc.readRelationshipsResource(context.Background(), uuid.New().String())
	require.NoError(t, err, "resource read must not fail on the fake DB")
	require.Len(t, res.Contents, 1)
	require.Equal(t, "memory://schema/relationships", res.Contents[0].URI)
	require.Equal(t, "application/json", res.Contents[0].MimeType)

	payload := decodeRelationshipsResource(t, res.Contents[0].Text)
	require.Equal(t, 2, payload.Total)
	require.Len(t, payload.Relationships, 2)
	require.Equal(t, "works_for", payload.Relationships[0].Type)
	require.Equal(t, "Person", payload.Relationships[0].FromType)
	require.Equal(t, "Company", payload.Relationships[0].ToType)
	require.Equal(t, 3, payload.Relationships[0].Count)
	require.Equal(t, "knows", payload.Relationships[1].Type)

	require.Len(t, captured, 1, "expected exactly one relationship query")
	// Strip identifier quotes so the phantom-column assertions catch both the
	// quoted (`"r"."relationship_type"`) and unquoted spellings.
	query := strings.ReplaceAll(captured[0], `"`, "")
	require.NotContains(t, query, "r.relationship_type",
		"query must not reference the non-existent kb.graph_relationships.relationship_type column")
	require.NotContains(t, query, "r.from_type",
		"query must not reference the non-existent kb.graph_relationships.from_type column")
	require.NotContains(t, query, "r.to_type",
		"query must not reference the non-existent kb.graph_relationships.to_type column")
	require.Contains(t, query, "r.type", "query must group by the real r.type column")
	require.Contains(t, query, "src.type", "from-type must be derived from the source object")
	require.Contains(t, query, "dst.type", "to-type must be derived from the target object")

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestReadRelationshipsResourceDB executes the resource against a real
// PostgreSQL (throwaway database migrated to head) with seeded graph objects
// and a relationship, proving the join returns the derived endpoint types. It
// skips when no test database is configured, matching the package convention.
func TestReadRelationshipsResourceDB(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mcp_relationships_resource")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	projectID := uuid.New()
	orgID := uuid.New()
	_, err := db.NewRaw(`
		INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'relationships-resource-org', now(), now())
	`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`
		INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'relationships-resource-project', ?, now(), now())
	`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)

	srcID, dstID := uuid.New(), uuid.New()
	for _, row := range []struct {
		id  uuid.UUID
		typ string
		key string
	}{
		{srcID, "Person", "alice"},
		{dstID, "Company", "acme"},
	} {
		_, err := db.NewRaw(`
			INSERT INTO kb.graph_objects (id, project_id, type, canonical_id, key, properties, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, '{}'::jsonb, now(), now())
		`, row.id, projectID, row.typ, row.id, row.key).Exec(ctx)
		require.NoError(t, err)
	}

	relID := uuid.New()
	_, err = db.NewRaw(`
		INSERT INTO kb.graph_relationships (id, project_id, type, src_id, dst_id, canonical_id, version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, now())
	`, relID, projectID, "works_for", srcID, dstID, relID).Exec(ctx)
	require.NoError(t, err)

	svc := &Service{db: db}
	res, err := svc.readRelationshipsResource(ctx, projectID.String())
	require.NoError(t, err)
	require.Len(t, res.Contents, 1)

	payload := decodeRelationshipsResource(t, res.Contents[0].Text)
	require.Equal(t, projectID.String(), payload.ProjectID)
	require.Equal(t, 1, payload.Total)
	require.Len(t, payload.Relationships, 1)
	require.Equal(t, "works_for", payload.Relationships[0].Type)
	require.Equal(t, "Person", payload.Relationships[0].FromType)
	require.Equal(t, "Company", payload.Relationships[0].ToType)
	require.Equal(t, 1, payload.Relationships[0].Count)
}
