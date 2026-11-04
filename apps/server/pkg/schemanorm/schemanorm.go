// Package schemanorm normalises the two storage formats used for the
// object_type_schemas JSON column (and, conceptually, the relationship
// counterpart) in kb.graph_schemas.
//
// Both domain/schemas (compiled types / registry) and domain/graph (runtime
// schema provider) must interpret this column identically. A duplicated copy in
// domain/graph previously reconstructed array-format entries with only
// properties/label/description, dropping the object-driven work configuration
// (boardEnabled/allowedStatuses and the operational skip flags) and silently
// disabling runtime work-status enforcement. The shared parser lives here so the
// two paths cannot drift again. domain/schemas imports domain/graph, so the
// shared code cannot live in either domain without an import cycle.
package schemanorm

import (
	"bytes"
	"encoding/json"
)

// ObjectTypeSchemasToMap converts the stored objectTypeSchemas JSON into a map of
// typeName → raw JSON schema, supporting both storage formats:
//
//   - Array format (user files): [{name, label, description, properties, ...}, ...]
//     The "properties" sub-object becomes the registered json_schema for each type.
//
//   - Map format (blueprint seeds): {typeName: {label, description, properties, ...}, ...}
//
// The array branch reconstructs a JSON Schema-style object for each type and
// preserves the optional scope-key declaration and the object-driven work
// configuration (boardEnabled/allowedStatuses/skipEmbeddings/skipExtraction/
// excludeFromSearch), matching what the extraction runtime normalisation already
// preserves. Returns nil on empty or invalid input.
func ObjectTypeSchemasToMap(data json.RawMessage) map[string]json.RawMessage {
	if len(data) == 0 {
		return nil
	}

	// Try array format first (natural user file format).
	var arr []struct {
		Name          string          `json:"name"`
		Label         string          `json:"label"`
		Description   string          `json:"description"`
		Properties    json.RawMessage `json:"properties"`
		UI            json.RawMessage `json:"ui"`
		ScopeKey      json.RawMessage `json:"scopeKey"`
		ScopeKeySnake json.RawMessage `json:"scope_key"`
		// Object-driven work configuration (P4). These must survive the
		// array→map normalisation so the compiled-types and graph runtime paths
		// return the same board/skip config the extraction runtime normalisation
		// (extraction.normalizeSchemaToMap) already preserves. Dropping them
		// here made the gateway silently fall back to canonical board lanes and
		// made runtime work-status enforcement inert.
		BoardEnabled      json.RawMessage `json:"boardEnabled"`
		AllowedStatuses   json.RawMessage `json:"allowedStatuses"`
		SkipEmbeddings    json.RawMessage `json:"skipEmbeddings"`
		SkipExtraction    json.RawMessage `json:"skipExtraction"`
		ExcludeFromSearch json.RawMessage `json:"excludeFromSearch"`
	}
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		result := make(map[string]json.RawMessage, len(arr))
		for _, item := range arr {
			if item.Name == "" {
				continue
			}
			// Reconstruct a JSON Schema-style object for this type so that
			// mergeSchemas and the registry can work with it uniformly.
			schema := map[string]json.RawMessage{}
			if len(item.Properties) > 0 {
				schema["properties"] = item.Properties
			}
			if len(item.UI) > 0 && !isNullJSON(item.UI) {
				schema["ui"] = item.UI
			}
			// Carry the optional scope-key declaration through to the registry
			// JSON schema so entity-query can enforce it (issue #1148).
			if len(item.ScopeKey) > 0 && !isNullJSON(item.ScopeKey) {
				schema["scopeKey"] = item.ScopeKey
			} else if len(item.ScopeKeySnake) > 0 && !isNullJSON(item.ScopeKeySnake) {
				schema["scope_key"] = item.ScopeKeySnake
			}
			// Carry the object-driven work configuration through unchanged so the
			// compiled-types path, the graph runtime path, and the registry see
			// the same fields the array entry declared (parity with the
			// extraction runtime normalisation).
			for key, raw := range map[string]json.RawMessage{
				"boardEnabled":      item.BoardEnabled,
				"allowedStatuses":   item.AllowedStatuses,
				"skipEmbeddings":    item.SkipEmbeddings,
				"skipExtraction":    item.SkipExtraction,
				"excludeFromSearch": item.ExcludeFromSearch,
			} {
				if len(raw) > 0 && !isNullJSON(raw) {
					schema[key] = raw
				}
			}
			if item.Label != "" {
				lb, _ := json.Marshal(item.Label)
				schema["label"] = lb
			}
			if item.Description != "" {
				desc, _ := json.Marshal(item.Description)
				schema["description"] = desc
			}
			schemaBytes, err := json.Marshal(schema)
			if err != nil {
				continue
			}
			result[item.Name] = schemaBytes
		}
		if len(result) > 0 {
			return result
		}
	}

	// Fall back to map format (blueprint seeds).
	var objMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &objMap); err == nil && len(objMap) > 0 {
		return objMap
	}

	return nil
}

// isNullJSON reports whether raw is the JSON literal null, ignoring surrounding
// whitespace. An explicit "ui": null must be treated as absent so it neither
// blocks the ui_configs fallback nor leaks "ui":null into the compiled output.
func isNullJSON(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
