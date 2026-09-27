package schemas

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ScopeKeyDeclaration is the optional schema-level declaration that a type is
// scoped to a parent/identity document. It is stored as the top-level
// "scopeKey" field of an object type's schema object (the JSON kept in
// kb.project_object_schema_registry.json_schema, and the shape of a pack's
// objectTypeSchemas entry):
//
//	{
//	  "properties": { "law_ref_id": {...}, "chapter_id": {...}, "section_id": {...} },
//	  "scopeKey": {
//	    "property": "law_ref_id",
//	    "referencesType": "Law",
//	    "referencesProperty": "ref_id",
//	    "identityProperty": "section_id"
//	  }
//	}
//
// When absent, the type keeps its previous behaviour exactly. When present,
// entity-query refuses a property filter that is neither the scope property nor
// the identity property unless the scope property is also supplied, so a
// non-unique property filter (e.g. chapter_id) can never silently match across
// documents (issue #1148, option 2).
type ScopeKeyDeclaration struct {
	// Property is the property on this type that scopes it to a parent
	// document (e.g. "law_ref_id"). Required when the declaration is present.
	Property string `json:"property"`

	// ReferencesType / ReferencesProperty name the target object type and the
	// property on it that Property values reference (e.g. Law.ref_id). Both are
	// optional together, but when present they must resolve to a known object
	// type and a property on it.
	ReferencesType     string `json:"referencesType,omitempty"`
	ReferencesProperty string `json:"referencesProperty,omitempty"`

	// IdentityProperty names the property that identifies an entity within the
	// scope (e.g. "section_id"). Optional; filtering on it alone is permitted.
	IdentityProperty string `json:"identityProperty,omitempty"`
}

// IsZero reports whether the declaration is absent/empty.
func (d *ScopeKeyDeclaration) IsZero() bool {
	return d == nil || strings.TrimSpace(d.Property) == ""
}

// scopeKeyWire accepts camelCase and snake_case aliases so blueprint files in
// either convention are honoured.
type scopeKeyWire struct {
	Property                string `json:"property"`
	ReferencesType          string `json:"referencesType"`
	ReferencesProperty      string `json:"referencesProperty"`
	IdentityProperty        string `json:"identityProperty"`
	ReferencesTypeSnake     string `json:"references_type"`
	ReferencesPropertySnake string `json:"references_property"`
	IdentityPropertySnake   string `json:"identity_property"`
}

// ParseScopeKey extracts the optional scope-key declaration from a type schema
// object. It accepts both "scopeKey" and "scope_key". Returns (nil, nil) when
// no declaration is present, and an error only when the field exists but is not
// a JSON object.
func ParseScopeKey(typeSchema json.RawMessage) (*ScopeKeyDeclaration, error) {
	if len(typeSchema) == 0 {
		return nil, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(typeSchema, &top); err != nil {
		return nil, nil
	}
	raw, ok := top["scopeKey"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		raw, ok = top["scope_key"]
	}
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	return parseScopeKeyValue(raw)
}

// parseScopeKeyValue parses the scopeKey value object itself (camelCase and
// snake_case aliases accepted).
func parseScopeKeyValue(raw json.RawMessage) (*ScopeKeyDeclaration, error) {
	var wire scopeKeyWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("scopeKey must be an object: %w", err)
	}
	return &ScopeKeyDeclaration{
		Property:           strings.TrimSpace(wire.Property),
		ReferencesType:     firstNonEmpty(wire.ReferencesType, wire.ReferencesTypeSnake),
		ReferencesProperty: firstNonEmpty(wire.ReferencesProperty, wire.ReferencesPropertySnake),
		IdentityProperty:   firstNonEmpty(wire.IdentityProperty, wire.IdentityPropertySnake),
	}, nil
}

// propertyNamesFromProperties returns the set of keys in a raw JSON properties
// object (the value of a type schema's "properties").
func propertyNamesFromProperties(rawProperties json.RawMessage) map[string]struct{} {
	names := map[string]struct{}{}
	if len(rawProperties) == 0 {
		return names
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(rawProperties, &props); err != nil {
		return names
	}
	for k := range props {
		names[k] = struct{}{}
	}
	return names
}

// ValidateScopeKey checks a declaration against the type's own properties and,
// when a reference target is given, against the known object types and their
// properties. It fails closed: an unresolved target, a missing property, or a
// half-specified reference is an error. knownTypes maps type name → property
// name set; pass nil when the target set is unavailable (only the self-checks
// then run). Returns a list of actionable error strings (empty = valid).
func ValidateScopeKey(typeName string, decl *ScopeKeyDeclaration, ownProps map[string]struct{}, knownTypes map[string]map[string]struct{}) []string {
	if decl == nil {
		return nil
	}
	var errs []string

	if decl.Property == "" {
		errs = append(errs, fmt.Sprintf("type %s: scopeKey.property is required", typeName))
	} else if _, ok := ownProps[decl.Property]; !ok {
		errs = append(errs, fmt.Sprintf(
			"type %s: scopeKey.property %q is not a declared property of the type", typeName, decl.Property))
	}

	if decl.IdentityProperty != "" {
		if _, ok := ownProps[decl.IdentityProperty]; !ok {
			errs = append(errs, fmt.Sprintf(
				"type %s: scopeKey.identityProperty %q is not a declared property of the type", typeName, decl.IdentityProperty))
		}
	}

	hasRefType := decl.ReferencesType != ""
	hasRefProp := decl.ReferencesProperty != ""
	switch {
	case hasRefType != hasRefProp:
		errs = append(errs, fmt.Sprintf(
			"type %s: scopeKey.referencesType and scopeKey.referencesProperty must be set together", typeName))
	case hasRefType && hasRefProp:
		if knownTypes != nil {
			targetProps, ok := knownTypes[decl.ReferencesType]
			if !ok {
				errs = append(errs, fmt.Sprintf(
					"type %s: scopeKey.referencesType %q does not resolve to a known object type",
					typeName, decl.ReferencesType))
			} else if _, ok := targetProps[decl.ReferencesProperty]; !ok {
				errs = append(errs, fmt.Sprintf(
					"type %s: scopeKey.referencesProperty %q is not a declared property of type %s",
					typeName, decl.ReferencesProperty, decl.ReferencesType))
			}
		}
	}

	return errs
}

// TypeSchemaPropertyNames returns the declared property names of a single type
// schema object (its "properties" keys).
func TypeSchemaPropertyNames(typeSchema json.RawMessage) map[string]struct{} {
	if len(typeSchema) == 0 {
		return map[string]struct{}{}
	}
	var top struct {
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(typeSchema, &top); err != nil {
		return map[string]struct{}{}
	}
	return propertyNamesFromProperties(top.Properties)
}

// ValidateTypeScopeKey validates the optional scopeKey declaration embedded in a
// single type schema object (the shape stored in the registry json_schema)
// against its own properties and, when a reference target is declared, the
// known object types. Pass knownTypes=nil to run only the self-checks. Returns a
// list of actionable error strings (empty = valid).
func ValidateTypeScopeKey(typeName string, typeSchema json.RawMessage, knownTypes map[string]map[string]struct{}) []string {
	decl, err := ParseScopeKey(typeSchema)
	if err != nil {
		return []string{fmt.Sprintf("type %s: %v", typeName, err)}
	}
	if decl == nil {
		return nil
	}
	return ValidateScopeKey(typeName, decl, TypeSchemaPropertyNames(typeSchema), knownTypes)
}
