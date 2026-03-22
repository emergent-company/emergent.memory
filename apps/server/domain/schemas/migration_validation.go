package schemas

import (
	"encoding/json"
	"fmt"
)

// validateMigrationHints validates a SchemaMigrationHints block against the schema
// it belongs to. Returns a list of human-readable error messages; nil slice means valid.
//
// Rules:
//  1. from_version must be non-empty when the block is present.
//  2. All type names in type_renames.from, property_renames.type_name, and
//     removed_properties.type_name must exist in the schema's object or
//     relationship type list.
//  3. All property names in property_renames.from and removed_properties.name
//     must exist in the referenced type's property set.
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

	// Rule 2 + 3: type_renames.from
	for _, tr := range hints.TypeRenames {
		if !allTypeNames[tr.From] {
			errs = append(errs, fmt.Sprintf("migrations.type_renames: type %q does not exist in the schema", tr.From))
		}
	}

	// Rule 2 + 3: property_renames
	for _, pr := range hints.PropertyRenames {
		if !allTypeNames[pr.TypeName] {
			errs = append(errs, fmt.Sprintf("migrations.property_renames: type %q does not exist in the schema", pr.TypeName))
			continue
		}
		// Rule 3: check property exists in the type
		props, hasProps := objectTypeProps[pr.TypeName]
		if hasProps && len(props) > 0 && !props[pr.From] {
			errs = append(errs, fmt.Sprintf("migrations.property_renames: property %q does not exist in type %q", pr.From, pr.TypeName))
		}
	}

	// Rule 2 + 3: removed_properties
	for _, rp := range hints.RemovedProperties {
		if !allTypeNames[rp.TypeName] {
			errs = append(errs, fmt.Sprintf("migrations.removed_properties: type %q does not exist in the schema", rp.TypeName))
			continue
		}
		// Rule 3: check property exists in the type
		props, hasProps := objectTypeProps[rp.TypeName]
		if hasProps && len(props) > 0 && !props[rp.Name] {
			errs = append(errs, fmt.Sprintf("migrations.removed_properties: property %q does not exist in type %q", rp.Name, rp.TypeName))
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
