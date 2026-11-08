package chat

import (
	"encoding/json"

	"github.com/emergent-company/emergent.memory/domain/chat/citations"
	"github.com/emergent-company/emergent.memory/domain/schemas"
)

// compiledTypeMeta maps each compiled object/relationship type name to its
// display metadata (declared label and ui accent color) so citations built in
// this domain can carry type_label/type_color. Only a declared label or color
// is emitted — a type with neither is left out, so citations for it keep empty
// (omitted) label/color and the client falls back to its own humanizer/map.
//
// The first non-shadowed declaration for a name wins; object types are
// processed before relationship types, so an object type takes precedence when
// a name appears in both lists — the same rule the gateway's embedded type map
// follows. Relationship types declare no ui block, so they contribute a label
// only.
func compiledTypeMeta(compiled *schemas.CompiledTypesResponse) map[string]citations.TypeMeta {
	if compiled == nil {
		return nil
	}
	m := make(map[string]citations.TypeMeta, len(compiled.ObjectTypes)+len(compiled.RelationshipTypes))
	add := func(name, label, color string) {
		if name == "" || (label == "" && color == "") {
			return
		}
		if _, ok := m[name]; ok {
			return
		}
		m[name] = citations.TypeMeta{Label: label, Color: color}
	}
	for _, t := range compiled.ObjectTypes {
		if t.Shadowed {
			continue
		}
		add(t.Name, t.Label, uiColor(t.UI))
	}
	for _, t := range compiled.RelationshipTypes {
		if t.Shadowed {
			continue
		}
		add(t.Name, t.Label, "")
	}
	return m
}

// uiColor reads the schema-declared "color" out of an object type's ui block
// (e.g. {"icon":"lucide--note","color":"#4F46E5"}). Returns "" when absent or
// malformed.
func uiColor(ui json.RawMessage) string {
	if len(ui) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(ui, &m); err != nil {
		return ""
	}
	if v, ok := m["color"].(string); ok {
		return v
	}
	return ""
}
