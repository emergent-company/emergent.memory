package discoveryjobs

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func testObjectSchema(properties ...string) map[string]any {
	props := make(map[string]any, len(properties))
	for _, name := range properties {
		props[name] = map[string]any{"type": "string"}
	}
	return map[string]any{"type": "object", "properties": props}
}

func typeNames(types []DiscoveredType) []string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.TypeName
	}
	sort.Strings(names)
	return names
}

func schemaKeys(schemas JSONMap) []string {
	keys := make([]string, 0, len(schemas))
	for key := range schemas {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestReconcileDiscoveredSchemaCollapsesCasePluralAndSynonyms(t *testing.T) {
	svc := &Service{}
	current := &DiscoveredSchemaSnapshot{ObjectTypeSchemas: JSONMap{
		"Organization": testObjectSchema("name"),
		"Organisation": testObjectSchema("registrationNumber"),
	}}
	types := []DiscoveredType{
		{TypeName: "Organisations", Properties: map[string]any{"name": map[string]any{"type": "string"}}},
		{TypeName: "Places", Properties: map[string]any{"address": map[string]any{"type": "string"}}},
		{TypeName: "Location", Properties: map[string]any{"latitude": map[string]any{"type": "number"}}},
	}

	got := svc.reconcileDiscoveredSchema([]string{"Organization"}, current, types, nil)

	require.Equal(t, []string{"Location", "Organization"}, typeNames(got.Types))
	require.Equal(t, []string{"Location", "Organization"}, schemaKeys(got.ObjectTypeSchemas))

	org := got.ObjectTypeSchemas["Organization"].(map[string]any)
	properties := org["properties"].(map[string]any)
	for _, property := range []string{"name", "registrationNumber"} {
		require.Containsf(t, properties, property, "merged Organization must keep %s", property)
	}

	location := got.ObjectTypeSchemas["Location"].(map[string]any)
	locationProperties := location["properties"].(map[string]any)
	require.Contains(t, locationProperties, "address")
	require.Contains(t, locationProperties, "latitude")
}

func TestReconcileDiscoveredSchemaConvergesAcrossRuns(t *testing.T) {
	svc := &Service{}
	firstTypes := []DiscoveredType{
		{TypeName: "Organization", Properties: map[string]any{"name": map[string]any{"type": "string"}}, Frequency: 2},
		{TypeName: "Person", Properties: map[string]any{"email": map[string]any{"type": "string"}}, Frequency: 1},
	}
	first := svc.reconcileDiscoveredSchema(nil, nil, firstTypes, nil)
	require.Equal(t, []string{"Organization", "Person"}, schemaKeys(first.ObjectTypeSchemas))

	// Simulate the second run over the same corpus with the spelling drift that
	// used to append a whole new schema version.
	current := &DiscoveredSchemaSnapshot{
		ObjectTypeSchemas:       first.ObjectTypeSchemas,
		RelationshipTypeSchemas: first.RelationshipTypeSchemas,
		UIConfigs:               first.UIConfigs,
	}
	secondTypes := []DiscoveredType{
		{TypeName: "Organizations", Properties: map[string]any{"name": map[string]any{"type": "string"}}, Frequency: 2},
		{TypeName: "people", Properties: map[string]any{"email": map[string]any{"type": "string"}}, Frequency: 1},
	}
	second := svc.reconcileDiscoveredSchema(nil, current, secondTypes, nil)

	require.Equal(t, schemaKeys(first.ObjectTypeSchemas), schemaKeys(second.ObjectTypeSchemas),
		"repeat discovery over the same type set must not change the discovered type set")
	require.Equal(t, []string{"Organization", "Person"}, typeNames(second.Types))
}

func TestReconcileDiscoveredSchemaCapturesGenuinelyNewType(t *testing.T) {
	svc := &Service{}
	first := svc.reconcileDiscoveredSchema(nil, nil, []DiscoveredType{
		{TypeName: "Organization", Properties: map[string]any{"name": map[string]any{"type": "string"}}},
	}, nil)
	current := &DiscoveredSchemaSnapshot{ObjectTypeSchemas: first.ObjectTypeSchemas}
	second := svc.reconcileDiscoveredSchema(nil, current, []DiscoveredType{
		{TypeName: "Organization", Properties: map[string]any{"name": map[string]any{"type": "string"}}},
		{TypeName: "Gadget", Properties: map[string]any{"serial": map[string]any{"type": "string"}}},
	}, nil)

	require.Equal(t, []string{"Gadget", "Organization"}, schemaKeys(second.ObjectTypeSchemas))
	require.Contains(t, typeNames(second.Types), "Gadget")
}

func TestReconcileDiscoveredSchemaKeepsExistingRelationshipTypes(t *testing.T) {
	svc := &Service{}
	current := &DiscoveredSchemaSnapshot{
		RelationshipTypeSchemas: JSONMap{
			"works_at": map[string]any{
				"sourceTypes": []any{"Person"},
				"targetTypes": []any{"Organisation"},
			},
		},
	}
	got := svc.reconcileDiscoveredSchema([]string{"Organization"}, current, nil, nil)

	definition := got.RelationshipTypeSchemas["works_at"].(map[string]any)
	require.Equal(t, []any{"Organization"}, definition["targetTypes"])
}

func TestMergeDiscoveredTypesUnionsRequiredProperties(t *testing.T) {
	merged := mergeDiscoveredTypes([]DiscoveredType{
		{
			TypeName:           "Person",
			Confidence:         0.4,
			Properties:         map[string]any{"name": map[string]any{"type": "string"}},
			RequiredProperties: []string{"name", "missing"},
			Frequency:          1,
		},
		{
			TypeName:           "People",
			Confidence:         0.9,
			Description:        "a human",
			Properties:         map[string]any{"email": map[string]any{"type": "string"}},
			RequiredProperties: []string{"email"},
			Frequency:          3,
		},
	})

	require.Len(t, merged, 1)
	require.Equal(t, "People", merged[0].TypeName, "highest-confidence member supplies the name")
	require.Equal(t, "a human", merged[0].Description)
	require.Equal(t, []string{"email", "name"}, merged[0].RequiredProperties,
		"required fields survive when a merged property defines them; undefined ones are dropped")
	require.Equal(t, 4, merged[0].Frequency)
}

func TestDedupeDiscoveredRelationshipsKeepsHighestConfidence(t *testing.T) {
	deduped := dedupeDiscoveredRelationships([]DiscoveredRelationship{
		{SourceType: "Person", TargetType: "Organization", RelationType: "works_at", Confidence: 0.4},
		{SourceType: "person", TargetType: "Organization", RelationType: "works_at", Confidence: 0.9},
	})

	require.Len(t, deduped, 1)
	require.Equal(t, float32(0.9), deduped[0].Confidence)
}
