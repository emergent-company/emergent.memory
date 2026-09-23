package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// insertSearchEntity inserts a HEAD graph object with an explicit key and
// properties so that kb.update_graph_objects_fts builds the tsvector from the
// same inputs as production. Returns the object id.
func insertSearchEntity(t *testing.T, db bun.IDB, projectID, typ, key, name, description string, createdAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	props := map[string]any{}
	if name != "" {
		props["name"] = name
	}
	if description != "" {
		props["description"] = description
	}
	propsJSON, err := json.Marshal(props)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, ?, 'active', ?::jsonb, '{}'::text[], ?, ?)
	`, id, projectID, id, typ, key, string(propsJSON), createdAt, createdAt)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE id = ?", id)
	})
	return id
}

// runEntitySearch executes executeSearchEntities and decodes the JSON text
// response into a SearchEntitiesResult.
func runEntitySearch(t *testing.T, svc *Service, projectID string, args map[string]any) SearchEntitiesResult {
	t.Helper()
	res, err := svc.executeSearchEntities(context.Background(), projectID, args)
	text := toolResultText(t, res, err)

	var out SearchEntitiesResult
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	return out
}

// TestEntitySearchFreeTextNameMatch verifies a free-text query matches an
// object by its name property through the fts index (not an ILIKE scan).
func TestEntitySearchFreeTextNameMatch(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	id := insertSearchEntity(t, db, projectID, "Person", "person/alice", "Alice Anderson", "Engineer", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "Alice"})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, id, out.Entities[0].ID)
	assert.Equal(t, "Alice Anderson", out.Entities[0].Name)
	assert.Equal(t, "Person", out.Entities[0].Type)
}

// TestEntitySearchExactKeyMatch verifies a composite key is found via the exact
// key equality branch.
func TestEntitySearchExactKeyMatch(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	key := "lov/1997-06-13-44"
	id := insertSearchEntity(t, db, projectID, "Law", key, "Lov om aksjeselskaper", "", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": key})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, id, out.Entities[0].ID)
	assert.Equal(t, key, out.Entities[0].Key)
}

// TestEntitySearchTypeFilter verifies the type_name filter narrows results to a
// single type.
func TestEntitySearchTypeFilter(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	personID := insertSearchEntity(t, db, projectID, "Person", "person/alice", "Alice Anderson", "", time.Now())
	orgID := insertSearchEntity(t, db, projectID, "Organization", "org/alice-corp", "Alice Corporation", "", time.Now())
	_ = personID
	_ = orgID

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "Alice", "type_name": "Person"})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, personID, out.Entities[0].ID)
	assert.Equal(t, "Person", out.Entities[0].Type)
}

// TestEntitySearchEmptyResult verifies a query matching nothing returns an
// empty entity list and a zero count.
func TestEntitySearchEmptyResult(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	insertSearchEntity(t, db, projectID, "Person", "person/alice", "Alice Anderson", "", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "zzz-nomatch-zzz"})
	assert.Empty(t, out.Entities)
	assert.Equal(t, 0, out.Count)
}
