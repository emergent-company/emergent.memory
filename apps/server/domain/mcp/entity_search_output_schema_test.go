package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

// resolveDeclaredOutputSchema turns a tool's declared OutputSchema into a
// resolved JSON-Schema validator, so a test can assert that a real tool result
// validates against exactly what tools/list advertises to MCP clients.
func resolveDeclaredOutputSchema(t *testing.T, schema *InputSchema) *jsonschema.Resolved {
	t.Helper()
	require.NotNil(t, schema, "tool must declare an outputSchema")
	raw, err := json.Marshal(schema)
	require.NoError(t, err)
	var js jsonschema.Schema
	require.NoError(t, json.Unmarshal(raw, &js))
	resolved, err := js.Resolve(nil)
	require.NoError(t, err)
	return resolved
}

// structuredContentAsWireJSON round-trips structuredContent through JSON, the
// same transformation an MCP client applies to the tools/call result before it
// validates structuredContent against the advertised output schema. Without it
// the in-process payload still holds Go structs, which the validator cannot
// introspect.
func structuredContentAsWireJSON(t *testing.T, structuredContent map[string]any) any {
	t.Helper()
	raw, err := json.Marshal(structuredContent)
	require.NoError(t, err)
	var wire any
	require.NoError(t, json.Unmarshal(raw, &wire))
	return wire
}

// TestEntityActionsMatchDeclaredOutputSchema is the #702 regression: the
// consolidated `entity` tool advertises the shared envelope output schema (its
// first action, entity-query, returns {ok,data}), but action=search returned a
// bare SearchEntitiesResult. MCP clients validate structuredContent against the
// advertised schema and rejected search with:
//
//	"data must have required property 'ok'/"data'"
//
// Every action result must validate against the schema the tool actually
// declares. query is covered alongside search to pin the shared contract.
func TestEntityActionsMatchDeclaredOutputSchema(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := &Service{db: db}

	entityTool := toolDefByName(t, svc, "entity")
	resolved := resolveDeclaredOutputSchema(t, entityTool.OutputSchema)

	insertSearchEntity(t, db, projectID, "Person", "person/nodio", "", "Nodio", "Company", time.Now())

	t.Run("search", func(t *testing.T) {
		res, err := svc.executeSearchEntities(context.Background(), projectID,
			map[string]any{"query": "Nodio", "limit": float64(3)})
		require.NoError(t, err)
		require.NotNil(t, res.StructuredContent)
		require.NoError(t, resolved.Validate(structuredContentAsWireJSON(t, res.StructuredContent)),
			"entity action=search structuredContent must match the declared output schema")
		require.Equal(t, true, res.StructuredContent["ok"])
		require.Contains(t, res.StructuredContent, "data")
	})

	t.Run("query", func(t *testing.T) {
		res, err := svc.executeQueryEntities(context.Background(), projectID,
			map[string]any{"type_name": "Person", "limit": float64(3)})
		require.NoError(t, err)
		require.NotNil(t, res.StructuredContent)
		require.NoError(t, resolved.Validate(structuredContentAsWireJSON(t, res.StructuredContent)),
			"entity action=query structuredContent must match the declared output schema")
	})
}

// TestEntitySearchDeclaresEnvelopeOutputSchema is the database-free half of the
// #702 guard: the entity-search definition must carry the envelope contract
// (required ok/data), not the permissive root-object schema that let the bare
// search payload slip through.
func TestEntitySearchDeclaresEnvelopeOutputSchema(t *testing.T) {
	svc := &Service{}
	def := toolDefByName(t, svc, "entity-search")
	require.NotNil(t, def.OutputSchema)
	require.Contains(t, def.OutputSchema.Required, "ok")
	require.Contains(t, def.OutputSchema.Required, "data")
	require.Contains(t, def.OutputSchema.Properties, "ok")
	require.Contains(t, def.OutputSchema.Properties, "data")
}
