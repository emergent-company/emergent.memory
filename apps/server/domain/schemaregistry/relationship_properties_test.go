package schemaregistry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A relationship type round-trip through the registry DTO must not drop
// property-definition keys the write validator now enforces.
func TestRelationshipSchemaPropertiesRoundTripPreservesEnum(t *testing.T) {
	raw := json.RawMessage(`{
		"MEMBER_OF":{
			"sourceTypes":["Person"],
			"targetTypes":["Org"],
			"properties":{
				"role":{"type":"string","enum":["owner","member"]},
				"tags":{"type":"array","items":{"type":"string","enum":["a","b"]}}
			}
		}
	}`)

	var schemas map[string]RelationshipSchema
	require.NoError(t, json.Unmarshal(raw, &schemas))
	out, err := json.Marshal(schemas)
	require.NoError(t, err)

	var decoded map[string]struct {
		Properties map[string]map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(out, &decoded))
	props := decoded["MEMBER_OF"].Properties
	assert.Equal(t, []any{"owner", "member"}, props["role"]["enum"])
	items := props["tags"]["items"].(map[string]any)
	assert.Equal(t, []any{"a", "b"}, items["enum"])
}
