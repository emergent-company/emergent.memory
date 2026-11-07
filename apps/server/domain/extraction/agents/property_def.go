package agents

import (
	"bytes"
	"encoding/json"
)

// UnmarshalJSON decodes a property definition from its JSON object form. JSON
// Schema also allows a non-object `items` — a tuple array of schemas, or the
// boolean `false` — which carries no element type this codebase interprets, so
// it decodes to an empty definition instead of failing the decode of the whole
// type (which would drop every property in a typed path such as
// parseObjectSchemasToAgentMap).
func (p *PropertyDef) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		*p = PropertyDef{}
		return nil
	}
	type propertyDef PropertyDef
	var decoded propertyDef
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return err
	}
	*p = PropertyDef(decoded)
	return nil
}

// PropertyDefFromMap builds a PropertyDef from one property definition decoded
// as a generic JSON object (map[string]any). That is the shape schema packs take
// when they are read from storage or a blueprint rather than unmarshalled
// directly into PropertyDef, so this is the single decoder for those paths.
//
// It copies the fields the validator interprets — type, description, and an
// array's element schema, recursively, so `items: {type: array, items: {…}}`
// keeps its nested element type. Default is deliberately not copied here:
// callers that apply schema defaults read it themselves (schema_provider),
// while paths that must not apply defaults would silently start doing so.
func PropertyDefFromMap(m map[string]any) PropertyDef {
	def := PropertyDef{}
	if t, ok := m["type"].(string); ok {
		def.Type = t
	}
	if d, ok := m["description"].(string); ok {
		def.Description = d
	}
	if f, ok := m["format"].(string); ok {
		def.Format = f
	}
	// JSON numbers decode as float64 here; a bound may legitimately be 0, so
	// only a decoded number sets the pointer.
	if v, ok := m["minimum"].(float64); ok {
		def.Minimum = &v
	}
	if v, ok := m["maximum"].(float64); ok {
		def.Maximum = &v
	}
	if items, ok := m["items"].(map[string]any); ok {
		itemDef := PropertyDefFromMap(items)
		def.Items = &itemDef
	}
	return def
}
