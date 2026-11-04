package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// These tests pin #2377: the schemas tool group described a surface that did not
// exist. `schema-list` advertised `include_deprecated` and `page` that nothing
// read (while hiding the `offset` it does read), `schema-create` documented
// `author`, `ui_configs` and `extraction_prompts` that were never written into
// the columns that hold them, and `schema-get` promised "UI configs and
// extraction prompts" it never returned. Each assertion below fails against the
// previous code.

// newSchemaToolsService builds the minimum Service the schema tool handlers need:
// all three read s.db directly, and the log is used only for warnings.
func newSchemaToolsService(db *testdb.TestDB) *Service {
	return &Service{
		db:  db.DB,
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestCreateSchemaPersistsAuthorAndConfigsDB proves the three declared knobs are
// written to kb.graph_schemas and read back by schema-get, so the value a caller
// passes is the value a caller can retrieve.
func TestCreateSchemaPersistsAuthorAndConfigsDB(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode (#2377)")
	}
	// These tests drive ExecuteTool the way an agent run does, so they carry the
	// trusted-internal marker the authority gate requires; without it every tool
	// with a RequiredScope is refused before the handler the test is about.
	ctx := ContextWithTrustedInternal(context.Background(), true)
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mcp_schema_create_configs")
	t.Cleanup(tdb.Close)
	_, projectID := seedProject(t, tdb.DB)
	svc := newSchemaToolsService(tdb)

	_, err := svc.ExecuteTool(ctx, projectID, "schema-create", map[string]any{
		"name":    "configs-persisted",
		"version": "1.2.3",
		"author":  "Team Memory",
		"object_type_schemas": map[string]any{
			"Person": map[string]any{
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		},
		"ui_configs": map[string]any{
			"Person": map[string]any{"icon": "user", "color": "#3B82F6"},
		},
		"extraction_prompts": map[string]any{
			"Person": "Extract people mentioned by name.",
		},
	})
	require.NoError(t, err)

	var row struct {
		ID                string         `bun:"id"`
		Author            string         `bun:"author"`
		UIConfigs         map[string]any `bun:"ui_configs,type:jsonb"`
		ExtractionPrompts map[string]any `bun:"extraction_prompts,type:jsonb"`
	}
	err = tdb.DB.NewRaw(`
		SELECT id, COALESCE(author, '') AS author, ui_configs, extraction_prompts
		FROM kb.graph_schemas
		WHERE project_id = ? AND name = ?
	`, projectID, "configs-persisted").Scan(ctx, &row)
	require.NoError(t, err, "the schema row must exist")
	require.Equal(t, "Team Memory", row.Author,
		"author was documented on schema-create and never written")
	personUIConfig, ok := row.UIConfigs["Person"].(map[string]any)
	require.True(t, ok, "ui_configs must be persisted per type, got %#v", row.UIConfigs)
	require.Equal(t, "user", personUIConfig["icon"],
		"ui_configs was documented on schema-create and never written")
	require.Equal(t, "Extract people mentioned by name.", row.ExtractionPrompts["Person"],
		"extraction_prompts was documented on schema-create and never written")

	// schema-get's description promises "all type definitions, UI configs, and
	// extraction prompts", so the write has to be readable through the API too —
	// a column nobody can read back is not much better than one nobody writes.
	res, err := svc.ExecuteTool(ctx, projectID, "schema-get", map[string]any{"schema_id": row.ID})
	require.NoError(t, err)
	require.NotNil(t, res)

	schemaDoc, ok := res.StructuredContent["schema"].(map[string]any)
	require.True(t, ok, "schema-get must return a schema object, got %#v", res.StructuredContent)
	require.Equal(t, "Team Memory", schemaDoc["author"], "schema-get must return the author")
	returnedUIConfigs, ok := schemaDoc["ui_configs"].(map[string]any)
	require.True(t, ok, "schema-get must return ui_configs, got %#v", schemaDoc["ui_configs"])
	returnedPerson, ok := returnedUIConfigs["Person"].(map[string]any)
	require.True(t, ok, "schema-get must return ui_configs per type")
	require.Equal(t, "user", returnedPerson["icon"])
	returnedPrompts, ok := schemaDoc["extraction_prompts"].(map[string]any)
	require.True(t, ok, "schema-get must return extraction_prompts")
	require.Equal(t, "Extract people mentioned by name.", returnedPrompts["Person"])
}

// TestListSchemasDeprecatedFilterAndOffsetDB proves the two `schema-list`
// arguments behave as declared: deprecated schemas are excluded unless
// `include_deprecated` is set (and `total` counts the same set the page does),
// and `offset` — the argument the handler actually reads — is advertised and
// echoes back.
func TestListSchemasDeprecatedFilterAndOffsetDB(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode (#2377)")
	}
	// These tests drive ExecuteTool the way an agent run does, so they carry the
	// trusted-internal marker the authority gate requires; without it every tool
	// with a RequiredScope is refused before the handler the test is about.
	ctx := ContextWithTrustedInternal(context.Background(), true)
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mcp_schema_list_deprecated")
	t.Cleanup(tdb.Close)
	_, projectID := seedProject(t, tdb.DB)
	svc := newSchemaToolsService(tdb)

	// Two schemas, one deprecated, written directly: schema-create has no
	// deprecated knob and this test is about reading the flag, not setting it.
	_, err := tdb.DB.ExecContext(ctx, `
		INSERT INTO kb.graph_schemas
			(id, project_id, name, version, description, source, object_type_schemas, relationship_type_schemas, deprecated_at)
		VALUES
			(gen_random_uuid(), ?, 'live-one',    '1.0.0', '', 'custom', '{}'::jsonb, '{}'::jsonb, NULL),
			(gen_random_uuid(), ?, 'retired-one', '1.0.0', '', 'custom', '{}'::jsonb, '{}'::jsonb, now())
	`, projectID, projectID)
	require.NoError(t, err)

	names := func(res *ToolResult) []string {
		schemas, ok := res.StructuredContent["schemas"].([]any)
		require.True(t, ok, "schema-list must return a schemas array")
		out := make([]string, 0, len(schemas))
		for _, s := range schemas {
			row, ok := s.(map[string]any)
			require.True(t, ok)
			name, _ := row["name"].(string)
			out = append(out, name)
		}
		return out
	}

	res, err := svc.ExecuteTool(ctx, projectID, "schema-list", map[string]any{})
	require.NoError(t, err)
	require.Equal(t, []string{"live-one"}, names(res),
		"deprecated schemas must be excluded by default (include_deprecated defaults to false)")
	require.EqualValues(t, 1, res.StructuredContent["total"],
		"total must describe the same set the page does, not every row")

	res, err = svc.ExecuteTool(ctx, projectID, "schema-list", map[string]any{"include_deprecated": true})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"live-one", "retired-one"}, names(res),
		"include_deprecated=true must return the deprecated rows too")
	require.EqualValues(t, 2, res.StructuredContent["total"])

	// `offset` is the pagination argument the handler reads; the response echoes
	// it alongside limit and total so a caller can page without guessing.
	// Numbers arrive as float64 through JSON (the MCP transport and the ADK
	// toolpool both decode the model's arguments that way), which is the type the
	// handler reads; passing an int here would exercise a shape no caller sends.
	res, err = svc.ExecuteTool(ctx, projectID, "schema-list", map[string]any{
		"include_deprecated": true, "limit": float64(1), "offset": float64(1),
	})
	require.NoError(t, err)
	require.Len(t, names(res), 1, "limit=1 must return exactly one row")
	require.EqualValues(t, 1, res.StructuredContent["offset"], "the response must echo the offset applied")
	require.EqualValues(t, 1, res.StructuredContent["limit"])
	require.EqualValues(t, 2, res.StructuredContent["total"],
		"total is the size of the filtered set, independent of limit/offset")
}

// TestTemplatesCatalogResourceDeprecatedFlagDB covers #2383 for the MCP
// resource. It used to call executeListSchemas with an empty argument map, so
// teaching that tool to hide deprecated schemas by default would have taken the
// resource's only route to them: a resource has no argument surface, so the URI
// query string is the whole opt-in.
func TestTemplatesCatalogResourceDeprecatedFlagDB(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode (#2383)")
	}
	ctx := ContextWithTrustedInternal(context.Background(), true)
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mcp_schema_catalog_resource")
	t.Cleanup(tdb.Close)
	_, projectID := seedProject(t, tdb.DB)
	svc := newSchemaToolsService(tdb)

	_, err := tdb.DB.ExecContext(ctx, `
		INSERT INTO kb.graph_schemas
			(id, project_id, name, version, description, source, object_type_schemas, relationship_type_schemas, deprecated_at)
		VALUES
			(gen_random_uuid(), ?, 'live-pack',    '1.0.0', '', 'custom', '{}'::jsonb, '{}'::jsonb, NULL),
			(gen_random_uuid(), ?, 'retired-pack', '1.0.0', '', 'custom', '{}'::jsonb, '{}'::jsonb, now())
	`, projectID, projectID)
	require.NoError(t, err)

	read := func(t *testing.T, uri string) (map[string]any, string) {
		t.Helper()
		res, err := svc.ReadResource(ctx, projectID, uri)
		require.NoError(t, err)
		require.Len(t, res.Contents, 1)
		// The reader double-encodes the tool payload: Contents.Text is a JSON
		// string holding the JSON document.
		var inner string
		require.NoError(t, json.Unmarshal([]byte(res.Contents[0].Text), &inner))
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(inner), &payload))
		return payload, res.Contents[0].URI
	}

	namesIn := func(t *testing.T, payload map[string]any) []string {
		t.Helper()
		schemas, ok := payload["schemas"].([]any)
		require.True(t, ok, "the catalog must return a schemas array, got %#v", payload)
		out := make([]string, 0, len(schemas))
		for _, s := range schemas {
			row, ok := s.(map[string]any)
			require.True(t, ok)
			name, _ := row["name"].(string)
			out = append(out, name)
		}
		return out
	}

	payload, uri := read(t, "memory://templates/catalog")
	require.Equal(t, []string{"live-pack"}, namesIn(t, payload),
		"the catalog hides deprecated schemas by default, matching schema-list")
	require.EqualValues(t, 1, payload["total"])
	require.Equal(t, "memory://templates/catalog", uri)

	payload, uri = read(t, "memory://templates/catalog?include_deprecated=true")
	require.ElementsMatch(t, []string{"live-pack", "retired-pack"}, namesIn(t, payload),
		"?include_deprecated=true is the resource's only way to see retired schemas")
	require.EqualValues(t, 2, payload["total"])
	require.Equal(t, "memory://templates/catalog?include_deprecated=true", uri,
		"the response must echo the URI that was asked for, not a canonical one")

	// A value that is not a boolean is refused rather than read as false: a
	// caller who asked for deprecated schemas and silently got the hidden set
	// has no way to notice.
	_, err = svc.ReadResource(ctx, projectID, "memory://templates/catalog?include_deprecated=nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), "include_deprecated")
}
