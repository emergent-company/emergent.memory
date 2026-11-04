package blueprints

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/schemas"
)

// TestObjectTypeDefScopeKeySnakeAlias verifies the top-level snake_case alias
// (`scope_key`) in a blueprint manifest is accepted and normalised into the
// canonical declaration, so it reaches the schema validator/registry instead of
// being dropped. RED before the alias existed: `scope_key` decoded to nil and
// the pack was created without a scope key, silently disabling enforcement.
func TestObjectTypeDefScopeKeySnakeAlias(t *testing.T) {
	t.Run("manifest scope_key reaches the validator as a declaration", func(t *testing.T) {
		raw := []byte(`{"packs":[{"name":"law","version":"1.0.0","objectTypes":[
			{"name":"LegalParagraph","properties":{"law_ref_id":{"type":"string"}},
			 "scope_key":{"property":"law_ref_id","references_type":"Law","references_property":"ref_id"}}
		]}]}`)

		var m BlueprintManifest
		require.NoError(t, json.Unmarshal(raw, &m))
		require.Len(t, m.Packs, 1)
		require.Len(t, m.Packs[0].ObjectTypes, 1)

		ot := m.Packs[0].ObjectTypes[0]
		require.NotNil(t, ot.ScopeKey, "scope_key must be promoted to a declaration")
		assert.Equal(t, "law_ref_id", ot.ScopeKey["property"])
		assert.Nil(t, ot.ScopeKeySnake, "alias must be cleared after promotion")

		// The apply path marshals ObjectTypes to the array CreatePack receives;
		// it must carry the declaration in canonical form.
		marshalled, err := json.Marshal(m.Packs[0].ObjectTypes)
		require.NoError(t, err)
		assert.Contains(t, string(marshalled), `"scopeKey":{`)

		var entries []json.RawMessage
		require.NoError(t, json.Unmarshal(marshalled, &entries))
		decl, err := schemas.ParseScopeKey(entries[0])
		require.NoError(t, err)
		require.NotNil(t, decl, "declaration must survive to the schema validator boundary")
		assert.Equal(t, "law_ref_id", decl.Property)
		assert.Equal(t, "Law", decl.ReferencesType)
		assert.Equal(t, "ref_id", decl.ReferencesProperty)
	})

	t.Run("canonical scopeKey wins when both are present", func(t *testing.T) {
		raw := []byte(`{"packs":[{"name":"law","version":"1.0.0","objectTypes":[
			{"name":"T","properties":{"a":{"type":"string"},"b":{"type":"string"}},
			 "scopeKey":{"property":"a"},"scope_key":{"property":"b"}}
		]}]}`)

		var m BlueprintManifest
		require.NoError(t, json.Unmarshal(raw, &m))
		ot := m.Packs[0].ObjectTypes[0]
		require.NotNil(t, ot.ScopeKey)
		assert.Equal(t, "a", ot.ScopeKey["property"], "canonical scopeKey must win")
		assert.Nil(t, ot.ScopeKeySnake)
	})
}

// TestObjectTypeDefRoundTripPreservesBehaviouralFields verifies that object
// type behavioural keys (labels, embedding, extraction, ui) and relationship
// type properties survive a manifest JSON round-trip. This is the losslessness
// guarantee behind routing schema-carrying packs through blueprint apply.
func TestObjectTypeDefRoundTripPreservesBehaviouralFields(t *testing.T) {
	m := BlueprintManifest{
		Packs: []PackManifest{{
			Name:    "agent-notes",
			Version: "2.0.0",
			ObjectTypes: []ObjectTypeDef{{
				Name:        "Note",
				Label:       "Note",
				Description: "an observation",
				Properties: map[string]any{
					"content": map[string]any{"type": "string", "required": true},
				},
				Labels:     []string{"note", "{category}"},
				Embedding:  map[string]any{"mode": "field", "field": "content"},
				Extraction: map[string]any{"enabled": false},
				UI:         map[string]any{"icon": "📝", "color": "#4F46E5"},
			}},
			RelationshipTypes: []RelationshipTypeDef{{
				Name:       "annotates",
				Label:      "Annotates",
				SourceType: "Note",
				TargetType: "*",
				Properties: map[string]any{"description": "links a note to its target"},
			}},
		}},
	}

	raw, err := json.Marshal(m)
	require.NoError(t, err)

	var back BlueprintManifest
	require.NoError(t, json.Unmarshal(raw, &back))

	ot := back.Packs[0].ObjectTypes[0]
	assert.Equal(t, []string{"note", "{category}"}, ot.Labels, "labels must round-trip")
	assert.Equal(t, "field", ot.Embedding["mode"], "embedding.mode must round-trip")
	assert.Equal(t, "content", ot.Embedding["field"], "embedding.field must round-trip")
	assert.Equal(t, false, ot.Extraction["enabled"], "extraction.enabled:false must survive (not dropped by omitempty)")
	assert.Equal(t, "📝", ot.UI["icon"], "ui.icon must round-trip")
	assert.Equal(t, true, ot.Properties["content"].(map[string]any)["required"], "properties must round-trip")

	rt := back.Packs[0].RelationshipTypes[0]
	assert.Equal(t, "links a note to its target", rt.Properties["description"], "relationship properties must round-trip")
}

// TestObjectTypeDefMarshalsBoardFields verifies the object-driven work fields
// (boardEnabled/allowedStatuses/skipEmbeddings) survive the manifest → pack
// object_type_schemas round-trip that applyPacks performs (json.Marshal of the
// ObjectTypes slice). The schema registry's parseObjectTypeSchemas reads these
// exact keys, so a missing/renamed tag would silently drop the board config.
func TestObjectTypeDefMarshalsBoardFields(t *testing.T) {
	m := BlueprintManifest{
		Packs: []PackManifest{{
			Name:    "board",
			Version: "1.0.0",
			ObjectTypes: []ObjectTypeDef{{
				Name:            "Task",
				Label:           "Task",
				Properties:      map[string]any{"title": map[string]any{"type": "string"}},
				BoardEnabled:    true,
				AllowedStatuses: []string{"todo", "doing", "done"},
				SkipEmbeddings:  true,
			}},
		}},
	}

	raw, err := json.Marshal(m.Packs[0].ObjectTypes)
	require.NoError(t, err)
	s := string(raw)

	assert.Contains(t, s, `"boardEnabled":true`, "boardEnabled must marshal into the pack JSON")
	assert.Contains(t, s, `"allowedStatuses":["todo","doing","done"]`, "allowedStatuses must marshal")
	assert.Contains(t, s, `"skipEmbeddings":true`, "skipEmbeddings must marshal")
	// Zero-valued omitempty fields must stay absent (not emitted as false).
	assert.NotContains(t, s, "skipExtraction")
	assert.NotContains(t, s, "excludeFromSearch")

	// The registry parse reads the same keys.
	var entries []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &entries))
	var parsed struct {
		BoardEnabled    bool     `json:"boardEnabled"`
		AllowedStatuses []string `json:"allowedStatuses"`
		SkipEmbeddings  bool     `json:"skipEmbeddings"`
	}
	require.NoError(t, json.Unmarshal(entries[0], &parsed))
	assert.True(t, parsed.BoardEnabled)
	assert.Equal(t, []string{"todo", "doing", "done"}, parsed.AllowedStatuses)
	assert.True(t, parsed.SkipEmbeddings)
}
