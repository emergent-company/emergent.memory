package graph

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

// Issue #322: two packs declaring a relationship type with the same name but
// different property schemas used to collapse into one registry entry, so an
// edge was validated against whichever pack the (unordered) assignment scan
// returned last. Relationship schemas are now keyed by
// (name, source type, target type), the identity the compiled-types view and the
// CLI validator use, so `owns` Person→Vehicle (share_percent) and
// `owns` Person→Account (holder_role) each validate against their own schema.

func relationshipPack(version, relationshipTypesJSON string) projectSchemaPack {
	return projectSchemaPack{
		Version:                 version,
		ObjectTypeSchemas:       json.RawMessage(`[]`),
		RelationshipTypeSchemas: json.RawMessage(relationshipTypesJSON),
	}
}

// ownsVehicleAsset declares `owns` Person→Vehicle with a required share_percent.
const ownsVehicleAsset = `[{
	"name": "owns",
	"sourceType": "Person",
	"targetType": "Vehicle",
	"properties": {"share_percent": {"type": "number"}},
	"required": ["share_percent"]
}]`

// ownsAccountBanking declares `owns` Person→Account with a required holder_role.
const ownsAccountBanking = `[{
	"name": "owns",
	"sourceType": "Person",
	"targetType": "Account",
	"properties": {"holder_role": {"type": "string"}},
	"required": ["holder_role"]
}]`

func names(props map[string]agents.PropertyDef) []string {
	out := make([]string, 0, len(props))
	for name := range props {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestSameNamedRelationshipFromTwoPacksValidatesPerEndpoint is the primary
// regression for #322. Pre-fix, RelationshipSchemas held a single `owns` entry:
// whichever pack was scanned last won, so one of the two edges below was
// validated against the wrong schema and failed its required property.
func TestSameNamedRelationshipFromTwoPacksValidatesPerEndpoint(t *testing.T) {
	// Oldest install first, as the provider orders assignments.
	schemas := projectSchemasFromPacks([]projectSchemaPack{
		relationshipPack("1.0.0", ownsVehicleAsset),
		relationshipPack("2.0.0", ownsAccountBanking),
	})

	vehicleSchema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, "owns", "Person", "Vehicle")
	require.True(t, ok, "owns Person→Vehicle must be registered")
	assert.Equal(t, []string{"share_percent"}, names(vehicleSchema.Properties))

	accountSchema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, "owns", "Person", "Account")
	require.True(t, ok, "owns Person→Account must be registered")
	assert.Equal(t, []string{"holder_role"}, names(accountSchema.Properties))

	t.Run("Person owns Vehicle validates against share_percent", func(t *testing.T) {
		validated, err := validateRelationshipProperties("owns", "Person", "Vehicle",
			map[string]any{"share_percent": 40}, schemas)
		require.NoError(t, err)
		// Coerced against the vehicle schema ("40" would stay a string if the
		// account schema were used, since holder_role is a string).
		validated, err = validateRelationshipProperties("owns", "Person", "Vehicle",
			map[string]any{"share_percent": "40"}, schemas)
		require.NoError(t, err)
		assert.Equal(t, 40.0, validated["share_percent"])
	})

	t.Run("Person owns Account validates against holder_role", func(t *testing.T) {
		_, err := validateRelationshipProperties("owns", "Person", "Account",
			map[string]any{"holder_role": "primary"}, schemas)
		require.NoError(t, err)
	})

	t.Run("each endpoint rejects the other's required property", func(t *testing.T) {
		_, err := validateRelationshipProperties("owns", "Person", "Vehicle",
			map[string]any{"holder_role": "primary"}, schemas)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "share_percent")

		_, err = validateRelationshipProperties("owns", "Person", "Account",
			map[string]any{"share_percent": 40}, schemas)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "holder_role")
	})
}

// TestSameNamedRelationshipWithinOnePackValidatesPerEndpoint covers the
// "bites within one blueprint" half of #322: a single pack's array-format
// relationshipTypes can repeat a name for different endpoint pairs, and the
// parser used to fold them into one schema that kept the first entry's
// properties for every pair.
func TestSameNamedRelationshipWithinOnePackValidatesPerEndpoint(t *testing.T) {
	schemas := projectSchemasFromPacks([]projectSchemaPack{
		relationshipPack("1.0.0", `[
			{"name":"owns","sourceType":"Person","targetType":"Vehicle",
			 "properties":{"share_percent":{"type":"number"}},"required":["share_percent"]},
			{"name":"owns","sourceType":"Person","targetType":"Account",
			 "properties":{"holder_role":{"type":"string"}},"required":["holder_role"]}
		]`),
	})

	_, err := validateRelationshipProperties("owns", "Person", "Vehicle",
		map[string]any{"share_percent": 40}, schemas)
	require.NoError(t, err, "Person→Vehicle must use the share_percent schema")

	_, err = validateRelationshipProperties("owns", "Person", "Account",
		map[string]any{"holder_role": "primary"}, schemas)
	require.NoError(t, err, "Person→Account must use the holder_role schema")

	_, err = validateRelationshipProperties("owns", "Person", "Vehicle",
		map[string]any{"holder_role": "primary"}, schemas)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "share_percent")
}

// TestRelationshipSchemaSelectionIgnoresScanOrder pins the determinism the bug
// was about: the resolved schema for an edge must depend on the edge's endpoints,
// not on the order the packs were read in. The pre-fix implementation keyed a
// single schema per name, so swapping the scan order swapped which shape
// validated — the "whichever row Postgres returned last" behaviour.
func TestRelationshipSchemaSelectionIgnoresScanOrder(t *testing.T) {
	vehicle := relationshipPack("1.0.0", ownsVehicleAsset)
	account := relationshipPack("2.0.0", ownsAccountBanking)

	orders := map[string][]projectSchemaPack{
		"newest last":  {vehicle, account},
		"newest first": {account, vehicle},
	}

	for orderName, packs := range orders {
		t.Run(orderName, func(t *testing.T) {
			schemas := projectSchemasFromPacks(packs)

			for _, edge := range []struct {
				srcType  string
				dstType  string
				property string
			}{
				{"Person", "Vehicle", "share_percent"},
				{"Person", "Account", "holder_role"},
			} {
				schema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, "owns", edge.srcType, edge.dstType)
				require.True(t, ok, "owns %s→%s must be registered", edge.srcType, edge.dstType)
				assert.Equal(t, []string{edge.property}, names(schema.Properties),
					"owns %s→%s must resolve to its own schema whatever order the packs were read in",
					edge.srcType, edge.dstType)
			}
		})
	}

	// Repeated loads of the same rows must produce identical registry entries.
	first := projectSchemasFromPacks([]projectSchemaPack{vehicle, account})
	second := projectSchemasFromPacks([]projectSchemaPack{vehicle, account})
	assert.Equal(t, first.RelationshipSchemas, second.RelationshipSchemas)
}

// TestRelationshipSchemaIdentityCoversDeclaredForms checks the endpoint forms a
// pack can declare: a pair, a source-only wildcard, and a map-format multi-target
// declaration. Each must be resolvable by its own endpoints.
func TestRelationshipSchemaIdentityCoversDeclaredForms(t *testing.T) {
	t.Run("source-only declaration applies to any target", func(t *testing.T) {
		schemas := projectSchemasFromPacks([]projectSchemaPack{
			relationshipPack("1.0.0", `[{"name":"owns","sourceType":"Person",
				"properties":{"share_percent":{"type":"number"}}}]`),
		})
		_, ok := relationshipSchemaFor(schemas.RelationshipSchemas, "owns", "Person", "Vehicle")
		assert.True(t, ok)

		_, err := validateRelationshipProperties("owns", "Person", "Yacht",
			map[string]any{"share_percent": 100}, schemas)
		assert.NoError(t, err)
	})

	t.Run("map format multi-target declaration keys every pair", func(t *testing.T) {
		schemas := projectSchemasFromPacks([]projectSchemaPack{
			relationshipPack("1.0.0", `{"owns":{"sourceTypes":["Person"],
				"targetTypes":["Vehicle","Account"],
				"properties":{"stake":{"type":"number"}}}}`),
		})
		for _, target := range []string{"Vehicle", "Account"} {
			schema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, "owns", "Person", target)
			require.True(t, ok, "owns Person→%s must be registered", target)
			assert.Equal(t, []string{"stake"}, names(schema.Properties))
		}
	})

	t.Run("undeclared endpoints fall back to the name-wide default", func(t *testing.T) {
		// Documented fallback: an edge whose pair no pack declares still gets the
		// name-wide default (the newest install's first declaration), which is
		// deterministic — unlike the arbitrary postgres row order of #322.
		schemas := projectSchemasFromPacks([]projectSchemaPack{
			relationshipPack("1.0.0", ownsVehicleAsset),
			relationshipPack("2.0.0", ownsAccountBanking),
		})
		schema, ok := relationshipSchemaFor(schemas.RelationshipSchemas, "owns", "Person", "Watch")
		require.True(t, ok)
		assert.Equal(t, []string{"holder_role"}, names(schema.Properties),
			"the newest install defines the name-wide default")
	})
}
