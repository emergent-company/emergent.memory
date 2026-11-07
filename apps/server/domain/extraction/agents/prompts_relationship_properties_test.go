package agents

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// TestBuildRelationshipPrompt_RendersDeclaredProperties is the regression lock
// for #264: a relationship type's declared `properties`/`required` were carried
// into the write path but never rendered, so the model could not emit them (and
// a required edge property failed every extraction-produced edge).
func TestBuildRelationshipPrompt_RendersDeclaredProperties(t *testing.T) {
	zero, one := 0.0, 1.0
	schemas := map[string]RelationshipSchema{
		"MEMBER_OF": {
			Name:        "MEMBER_OF",
			Description: "Person belongs to an organization",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Organization"},
			Properties: map[string]PropertyDef{
				"role":          {Type: "string", Description: "Role within the organization"},
				"tenure_years":  {Type: "number", Description: "Years of membership", Minimum: &zero, Maximum: &one},
				"contact_email": {Type: "string", Description: "Contact address", Format: "email"},
			},
			Required: []string{"role"},
		},
	}

	prompt := BuildRelationshipPrompt(
		[]InternalEntity{
			{TempID: "person_alice", Name: "Alice", Type: "Person"},
			{TempID: "org_acme", Name: "Acme", Type: "Organization"},
		},
		schemas,
		"Alice has been a board member at Acme for years.",
		nil,
		nil,
	)

	assert.Contains(t, prompt, "**Properties** (stored in this relationship's `properties` object):")
	assert.Contains(t, prompt, "- `role` (string) (required): Role within the organization",
		"a required property must be rendered and marked required")
	assert.Contains(t, prompt, "- `tenure_years` (number, min 0, max 1): Years of membership",
		"declared numeric bounds must reach the model")
	assert.Contains(t, prompt, "- `contact_email` (string, format email): Contact address",
		"a declared format must reach the model")

	// Sorted by name so the same schema always produces the same prompt.
	assert.Less(t, strings.Index(prompt, "`contact_email`"), strings.Index(prompt, "`role`"))

	// The output format must declare the field the prompt now asks for.
	assert.Contains(t, prompt, "- properties (object, optional): Relationship-level attributes")
	assert.Contains(t, prompt, `"properties": {"role": "apostle"}`)
}

// A relationship type without declared properties must not gain an empty
// section (which would spend prompt budget telling the model nothing).
func TestBuildRelationshipPrompt_NoPropertySectionWithoutProperties(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"KNOWS": {Name: "KNOWS", Description: "Person knows person"},
	}

	prompt := BuildRelationshipPrompt(
		[]InternalEntity{{TempID: "person_a", Name: "A", Type: "Person"}},
		schemas,
		"Document.",
		nil,
		nil,
	)

	assert.NotContains(t, prompt, "**Properties** (stored in this relationship's `properties` object):")
}

// relationshipExtractionItemSchema returns the relationship item schema for
// both the generic and the pack-derived output schema, so the properties field
// is locked for each.
func relationshipExtractionItemSchema(t *testing.T) map[string]map[string]*genai.Schema {
	t.Helper()
	generic := RelationshipExtractionSchema()
	dynamic := BuildRelationshipSchemaFromMemorySchema(map[string]RelationshipSchema{
		"KNOWS": {Name: "KNOWS"},
	})
	require.NotNil(t, generic.Properties["relationships"].Items)
	require.NotNil(t, dynamic.Properties["relationships"].Items)
	return map[string]map[string]*genai.Schema{
		"generic": generic.Properties["relationships"].Items.Properties,
		"dynamic": dynamic.Properties["relationships"].Items.Properties,
	}
}

// TestRelationshipOutputSchema_DeclaresProperties locks the structured-output
// half of #264: with an ADK OutputSchema the model cannot emit a field the
// schema omits, so the prompt alone would not be enough.
func TestRelationshipOutputSchema_DeclaresProperties(t *testing.T) {
	for name, props := range relationshipExtractionItemSchema(t) {
		prop, ok := props["properties"]
		require.True(t, ok, "%s relationship output schema must declare a properties field", name)
		assert.Equal(t, genai.TypeObject, prop.Type, name)
	}
}

// TestParseRelationshipExtractionOutput_CarriesProperties proves an emitted
// edge property is decoded into the typed relationship rather than dropped by
// the JSON conversion.
func TestParseRelationshipExtractionOutput_CarriesProperties(t *testing.T) {
	raw := `{
		"relationships": [
			{
				"source_ref": "person_alice",
				"target_ref": "org_acme",
				"type": "MEMBER_OF",
				"description": "Alice chairs the board",
				"properties": {"role": "chair", "term_count": 2}
			}
		]
	}`

	output, err := ParseRelationshipExtractionOutput(raw)
	require.NoError(t, err)
	require.Len(t, output.Relationships, 1)

	rel := output.Relationships[0]
	assert.Equal(t, map[string]any{"role": "chair", "term_count": float64(2)}, rel.Properties,
		"an emitted relationship property must survive the typed decode")
}

// TestConvertToRelationshipSchema_CarriesPropertiesAndRequired locks the
// generic-map decoder used when relationship schemas arrive through session
// state: rendering the prompt from a schema that lost its properties would
// reintroduce #264 on that path.
func TestConvertToRelationshipSchema_CarriesPropertiesAndRequired(t *testing.T) {
	schema := convertToRelationshipSchema(map[string]any{
		"name": "MEMBER_OF",
		"properties": map[string]any{
			"role": map[string]any{"type": "string", "description": "Role within the organization"},
		},
		"required": []any{"role"},
	})

	require.Contains(t, schema.Properties, "role")
	assert.Equal(t, "string", schema.Properties["role"].Type)
	assert.Equal(t, "Role within the organization", schema.Properties["role"].Description)
	assert.Equal(t, []string{"role"}, schema.Required)
}
