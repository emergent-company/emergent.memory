package schemas

import (
	"encoding/json"
	"fmt"
)

// validateMigrationHints validates a SchemaMigrationHints block against the schema
// it belongs to. Returns a list of human-readable error messages; nil slice means valid.
//
// Rules (all checked against THIS schema, i.e. the target/new version):
//  1. from_version must be non-empty when the block is present.
//  2. type_renames.to, property_renames.type_name, and removed_properties.type_name
//     must exist in the schema's object or relationship type list. `from` names
//     reference the previous version's schema and are not validated here.
//  3. property_renames.to must exist in the referenced type's property set.
//     removed_properties.name is intentionally absent from this schema (no check).
func validateMigrationHints(hints *SchemaMigrationHints, objectTypeSchemas, relTypeSchemas json.RawMessage) []string {
	if hints == nil {
		return nil
	}

	var errs []string

	// Rule 1: from_version required
	if hints.FromVersion == "" {
		errs = append(errs, "migrations.from_version is required when a migrations block is present")
	}

	// Build lookup maps from the schema types
	// objectTypeProps: typeName → set of property names
	objectTypeProps := buildObjectTypePropMap(objectTypeSchemas)
	relTypeNames := buildRelTypeNameSet(relTypeSchemas)

	// Combined type name set (object types + relationship types)
	allTypeNames := make(map[string]bool, len(objectTypeProps)+len(relTypeNames))
	for k := range objectTypeProps {
		allTypeNames[k] = true
	}
	for k := range relTypeNames {
		allTypeNames[k] = true
	}

	// Rule 2: type_renames.to must exist in this (new) schema.
	// `from` is the old name from the from_version schema, not present here.
	for _, tr := range hints.TypeRenames {
		if !allTypeNames[tr.To] {
			errs = append(errs, fmt.Sprintf("migrations.type_renames: type %q does not exist in the schema", tr.To))
		}
	}

	// Rule 2 + 3: property_renames
	for _, pr := range hints.PropertyRenames {
		if !allTypeNames[pr.TypeName] {
			errs = append(errs, fmt.Sprintf("migrations.property_renames: type %q does not exist in the schema", pr.TypeName))
			continue
		}
		// Rule 3: check the new property name (`to`) exists in the type.
		// `from` is the old property name from the previous version.
		props, hasProps := objectTypeProps[pr.TypeName]
		if hasProps && len(props) > 0 && !props[pr.To] {
			errs = append(errs, fmt.Sprintf("migrations.property_renames: property %q does not exist in type %q", pr.To, pr.TypeName))
		}
	}

	// Rule 2: removed_properties.type_name must exist in this schema.
	// `name` is the property dropped from the previous version, so it is
	// intentionally absent from the new schema — no existence check.
	for _, rp := range hints.RemovedProperties {
		if !allTypeNames[rp.TypeName] {
			errs = append(errs, fmt.Sprintf("migrations.removed_properties: type %q does not exist in the schema", rp.TypeName))
			continue
		}
	}

	return errs
}

// buildObjectTypePropMap parses objectTypeSchemas into a map of
// typeName → set of property names. The schemas may be stored as an array or
// as a map (both formats are handled via parseObjectTypeSchemasToMap).
// Returns an empty map if input is nil/empty.
func buildObjectTypePropMap(data json.RawMessage) map[string]map[string]bool {
	if len(data) == 0 {
		return map[string]map[string]bool{}
	}

	typeMap := parseObjectTypeSchemasToMap(data)
	result := make(map[string]map[string]bool, len(typeMap))
	for typeName, schemaRaw := range typeMap {
		propSet := make(map[string]bool)
		// schemaRaw is a JSON object that may have a "properties" key
		var schemaObj map[string]json.RawMessage
		if err := json.Unmarshal(schemaRaw, &schemaObj); err == nil {
			if propsRaw, ok := schemaObj["properties"]; ok {
				var props map[string]json.RawMessage
				if err := json.Unmarshal(propsRaw, &props); err == nil {
					for propName := range props {
						propSet[propName] = true
					}
				}
			}
		}
		result[typeName] = propSet
	}
	return result
}

// buildRelTypeNameSet parses relationship_type_schemas and returns a set of type names.
func buildRelTypeNameSet(data json.RawMessage) map[string]bool {
	if len(data) == 0 {
		return map[string]bool{}
	}
	result := make(map[string]bool)

	// Try map format first
	var objMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &objMap); err == nil {
		for name := range objMap {
			result[name] = true
		}
		return result
	}

	// Try array format
	var arr []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &arr); err == nil {
		for _, item := range arr {
			if item.Name != "" {
				result[item.Name] = true
			}
		}
	}

	return result
}
