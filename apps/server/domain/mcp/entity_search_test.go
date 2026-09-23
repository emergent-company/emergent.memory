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

// fts00174Builder is the migration 00174 `kb.graph_object_fts` tsvector builder
// verbatim. It lives here because connectTestDB clones internal/testdb/schema.sql,
// which still embeds the pre-00174 trigger (a `simple` vector over the raw key,
// type and every property value). Replacing the functions on the throwaway test
// database makes these tests exercise the production-normalised vector: raw +
// separator-normalised key, type, and a bounded `norwegian` prose space.
const fts00174Builder = `
CREATE OR REPLACE FUNCTION kb.graph_object_fts(
    p_key text,
    p_type text,
    p_properties jsonb
) RETURNS tsvector
    LANGUAGE sql
    IMMUTABLE
    PARALLEL SAFE
    AS $$
    SELECT
        setweight(to_tsvector('simple', coalesce(p_key, '')), 'A')
        || setweight(to_tsvector('simple',
               translate(coalesce(p_key, ''), '/-#', '   ')
               || ' '
               || replace(replace(replace(coalesce(p_key, ''), '/', ' '), '#', ' '), '-', ' -')
           ), 'A')
        || setweight(to_tsvector('simple', coalesce(p_type, '')), 'B')
        || setweight(to_tsvector('norwegian',
               left(coalesce(p_properties ->> 'title', ''), 4000)
               || ' ' || left(coalesce(p_properties ->> 'name', ''), 4000)
               || ' ' || left(coalesce(p_properties ->> 'description', ''), 4000)
           ), 'C');
$$`

// fts00174Trigger is the migration 00174 `kb.update_graph_objects_fts` trigger
// body verbatim.
const fts00174Trigger = `
CREATE OR REPLACE FUNCTION kb.update_graph_objects_fts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
       NEW.key IS NOT DISTINCT FROM OLD.key AND
       NEW.type IS NOT DISTINCT FROM OLD.type AND
       NEW.properties IS NOT DISTINCT FROM OLD.properties THEN
        RETURN NEW;
    END IF;

    NEW.fts := kb.graph_object_fts(NEW.key, NEW.type, NEW.properties);
    RETURN NEW;
END;
$$`

// connectFTSearchTestDB returns a throwaway test database whose fts trigger
// matches production (migration 00174). connectTestDB alone clones a schema
// snapshot whose trigger predates 00174, so component-token and norwegian-prose
// assertions would silently test the wrong vector.
func connectFTSearchTestDB(t *testing.T) *bun.DB {
	t.Helper()
	db := connectTestDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, fts00174Builder)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, fts00174Trigger)
	require.NoError(t, err)
	return db
}

// insertSearchEntity inserts a HEAD graph object with an explicit key and
// properties so that kb.update_graph_objects_fts builds the tsvector from the
// same inputs as production. Returns the object id.
func insertSearchEntity(t *testing.T, db bun.IDB, projectID, typ, key, title, name, description string, createdAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	props := map[string]any{}
	if title != "" {
		props["title"] = title
	}
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
	db := connectFTSearchTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	id := insertSearchEntity(t, db, projectID, "Person", "person/alice", "", "Alice Anderson", "Engineer", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "Alice"})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, id, out.Entities[0].ID)
	assert.Equal(t, "Alice Anderson", out.Entities[0].Name)
	assert.Equal(t, "Person", out.Entities[0].Type)
}

// TestEntitySearchExactKeyMatch verifies a composite key is found by searching
// its exact form. There is no `key = ?` predicate — the raw key is a lexeme in
// the fts vector — so this also pins that the indexed path resolves it.
func TestEntitySearchExactKeyMatch(t *testing.T) {
	db := connectFTSearchTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	key := "lov/1997-06-13-44"
	id := insertSearchEntity(t, db, projectID, "Law", key, "", "Lov om aksjeselskaper", "", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": key})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, id, out.Entities[0].ID)
	assert.Equal(t, key, out.Entities[0].Key)
}

// TestEntitySearchComponentKeyMatch verifies a composite key is reachable by its
// component tokens. This is the regression guard for migration 00174: the
// pre-00174 vector indexed "lov/1997-06-13-44" as one opaque lexeme, so the
// component query returned nothing.
func TestEntitySearchComponentKeyMatch(t *testing.T) {
	db := connectFTSearchTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	id := insertSearchEntity(t, db, projectID, "Law", "lov/1997-06-13-44", "", "Lov om aksjeselskaper", "", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "lov 1997 06 13 44"})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, id, out.Entities[0].ID)
}

// TestEntitySearchRelaxesUnsatisfiableIdentifier verifies the strict→relaxed
// retry: a hyphenated identifier that websearch_to_tsquery turns into an
// unsatisfiable phrase zeroes the strict clause, and the relaxed form (which
// drops the purely numeric run) still finds the text match.
func TestEntitySearchRelaxesUnsatisfiableIdentifier(t *testing.T) {
	db := connectFTSearchTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	id := insertSearchEntity(t, db, projectID, "Law", "aksjeloven/1", "", "Aksjeloven Test", "aksjeloven", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "aksjeloven 9999-99-99-99"})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, id, out.Entities[0].ID)
}

// TestEntitySearchTitleMatch verifies the `title` property is searchable. This
// is a deliberate widening versus the old ILIKE predicate, which only scanned
// key, name and description.
func TestEntitySearchTitleMatch(t *testing.T) {
	db := connectFTSearchTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	id := insertSearchEntity(t, db, projectID, "Person", "person/titled", "Skjult Tittel", "Ordinary Name", "", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "Skjult"})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, id, out.Entities[0].ID)
}

// TestEntitySearchTypeFilter verifies the type_name filter narrows results to a
// single type.
func TestEntitySearchTypeFilter(t *testing.T) {
	db := connectFTSearchTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	personID := insertSearchEntity(t, db, projectID, "Person", "person/alice", "", "Alice Anderson", "", time.Now())
	_ = insertSearchEntity(t, db, projectID, "Organization", "org/alice-corp", "", "Alice Corporation", "", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "Alice", "type_name": "Person"})
	require.Len(t, out.Entities, 1)
	assert.Equal(t, personID, out.Entities[0].ID)
	assert.Equal(t, "Person", out.Entities[0].Type)
}

// TestEntitySearchEmptyResult verifies a query matching nothing returns an
// empty entity list and a zero count.
func TestEntitySearchEmptyResult(t *testing.T) {
	db := connectFTSearchTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	insertSearchEntity(t, db, projectID, "Person", "person/alice", "", "Alice Anderson", "", time.Now())

	out := runEntitySearch(t, svc, projectID, map[string]any{"query": "zzz-nomatch-zzz"})
	assert.Empty(t, out.Entities)
	assert.Equal(t, 0, out.Count)
}
