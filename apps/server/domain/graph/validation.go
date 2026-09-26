package graph

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

func coerceToNumber(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, fmt.Errorf("empty string cannot be converted to number")
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid number format: %s", v)
		}
		return parsed, nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to number", value)
	}
}

func coerceToBoolean(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		trimmed := strings.ToLower(strings.TrimSpace(v))
		switch trimmed {
		case "true", "t", "yes", "y", "1":
			return true, nil
		case "false", "f", "no", "n", "0", "":
			return false, nil
		default:
			return false, fmt.Errorf("invalid boolean format: %s", v)
		}
	case int, int64, int32, float64, float32:
		return fmt.Sprintf("%v", v) != "0", nil
	default:
		return false, fmt.Errorf("cannot convert %T to boolean", value)
	}
}

func coerceToDate(value any) (string, error) {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", fmt.Errorf("empty string cannot be converted to date")
		}

		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"01/02/2006",
			"02-01-2006",
		}

		for _, format := range formats {
			t, err := time.Parse(format, trimmed)
			if err == nil {
				return t.Format(time.RFC3339), nil
			}
		}

		return "", fmt.Errorf("invalid date format: %s (expected ISO 8601 or common formats)", v)

	case time.Time:
		return v.Format(time.RFC3339), nil

	default:
		return "", fmt.Errorf("cannot convert %T to date", value)
	}
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationResult struct {
	Valid   bool              `json:"valid"`
	Coerced map[string]any    `json:"coerced,omitempty"`
	Errors  []ValidationError `json:"errors,omitempty"`
}

type PropertyValidator struct{}

func NewPropertyValidator() *PropertyValidator {
	return &PropertyValidator{}
}

func (v *PropertyValidator) ValidateProperties(
	props map[string]any,
	schema *agents.ObjectSchema,
) *ValidationResult {
	if schema == nil || len(schema.Properties) == 0 {
		return &ValidationResult{
			Valid:   true,
			Coerced: props,
			Errors:  []ValidationError{},
		}
	}

	validated := make(map[string]any)
	var errors []ValidationError

	for key, value := range props {
		propDef, hasDef := schema.Properties[key]
		if !hasDef {
			validated[key] = value
			continue
		}

		if value == nil {
			validated[key] = nil
			continue
		}

		coerced, err := v.coerceValue(value, propDef.Type)
		if err != nil {
			errors = append(errors, ValidationError{
				Field:   key,
				Message: fmt.Sprintf("coercion failed: %v", err),
			})
		} else {
			validated[key] = coerced
		}
	}

	for _, required := range schema.Required {
		if _, ok := validated[required]; !ok {
			errors = append(errors, ValidationError{
				Field:   required,
				Message: "missing required field",
			})
		}
	}

	return &ValidationResult{
		Valid:   len(errors) == 0,
		Coerced: validated,
		Errors:  errors,
	}
}

func (v *PropertyValidator) coerceValue(value any, targetType string) (any, error) {
	switch targetType {
	case "number":
		return coerceToNumber(value)
	case "boolean":
		return coerceToBoolean(value)
	case "date":
		return coerceToDate(value)
	case "array":
		if _, ok := value.([]any); !ok {
			return nil, fmt.Errorf("expected array, got %T", value)
		}
		return value, nil
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return nil, fmt.Errorf("expected object, got %T", value)
		}
		return value, nil
	default:
		return value, nil
	}
}

// ValidateAndCoerceProperties validates and coerces properties according to schema.
// This is exported for use by migration tools.
func ValidateAndCoerceProperties(
	props map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	return validateProperties(props, schema)
}

func validateProperties(
	props map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	if len(schema.Properties) == 0 {
		return props, nil
	}

	validated := make(map[string]any)
	var validationErrors []string

	for key, value := range props {
		propDef, hasDef := schema.Properties[key]
		if !hasDef {
			// Unknown properties are passed through as-is. The schema defines
			// known properties for type coercion and validation, but does not
			// act as an allowlist — users may store arbitrary metadata keys.
			validated[key] = value
			continue
		}

		if value == nil {
			validated[key] = nil
			continue
		}

		switch propDef.Type {
		case "number":
			coerced, err := coerceToNumber(value)
			if err != nil {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", key, err))
			} else {
				validated[key] = coerced
			}

		case "boolean":
			coerced, err := coerceToBoolean(value)
			if err != nil {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", key, err))
			} else {
				validated[key] = coerced
			}

		case "date":
			coerced, err := coerceToDate(value)
			if err != nil {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", key, err))
			} else {
				validated[key] = coerced
			}

		case "array":
			if _, ok := value.([]any); !ok {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: expected array, got %T", key, value))
			} else {
				validated[key] = value
			}

		case "object":
			if _, ok := value.(map[string]any); !ok {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: expected object, got %T", key, value))
			} else {
				validated[key] = value
			}

		default:
			validated[key] = value
		}
	}

	for _, required := range schema.Required {
		if _, ok := validated[required]; !ok {
			validationErrors = append(validationErrors, fmt.Sprintf("missing required field: %s", required))
		}
	}

	if len(validationErrors) > 0 {
		return nil, fmt.Errorf("property validation failed: %s", strings.Join(validationErrors, "; "))
	}

	return validated, nil
}

// applySchemaDefaults fills schema-declared default values for properties
// absent from the supplied map, then derives a confidence value from the
// "source" property when the schema declares both "confidence" and "source"
// (e.g. Note). Confidence decay/gain was previously maintained by dedicated
// note tools; this centralizes it in the schema-driven create path.
func applySchemaDefaults(props map[string]any, schema agents.ObjectSchema) {
	if props == nil || len(schema.Properties) == 0 {
		return
	}

	for name, def := range schema.Properties {
		if _, present := props[name]; present {
			continue
		}
		if def.Default != nil {
			props[name] = def.Default
		}
	}

	if _, present := props["confidence"]; present {
		return
	}
	if _, hasConf := schema.Properties["confidence"]; !hasConf {
		return
	}
	srcDef, hasSrc := schema.Properties["source"]
	if !hasSrc {
		return
	}
	src, _ := props["source"].(string)
	if src == "" {
		if s, ok := srcDef.Default.(string); ok {
			src = s
		}
	}
	confidence := 0.7
	switch src {
	case "explicit":
		confidence = 1.0
	case "corrected":
		confidence = 0.9
	}
	props["confidence"] = confidence
}

// validatePatchProperties validates only the properties being set or added by a patch request
// against the current schema. Unlike validateProperties it does NOT enforce required fields,
// because the object already exists and may have been created under an older schema version
// where those fields were not required (or didn't exist yet). Only keys present in patchProps
// (i.e. the delta) are checked; keys already stored on the object that are not touched by the
// patch are intentionally ignored.
func validatePatchProperties(
	patchProps map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	return validatePatchDelta(patchProps, nil, schema)
}

// validateRelationshipPatchProperties validates a relationship patch's property
// delta and enforces required fields against the merged result (existing
// properties plus the patch delta).
//
// Unlike validatePatchProperties (the object patch path), required fields ARE
// enforced here against mergedProps, so a patch can neither clear nor omit a
// required relationship property and still succeed (issue #989). Type coercion
// remains delta-only: keys already stored on the relationship that are not
// touched by the patch are left alone, because they may predate the schema
// version that declared their type.
//
// Unknown keys are passed through as-is (the schema is not an allowlist).
func validateRelationshipPatchProperties(
	patchProps map[string]any,
	mergedProps map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	return validatePatchDelta(patchProps, mergedProps, schema)
}

// validatePatchDelta type-coerces the patch delta keys. When mergedProps is
// non-nil it also enforces required fields against that merged result, so a
// patch cannot leave a required property absent (whether omitted or cleared).
func validatePatchDelta(
	patchProps map[string]any,
	mergedProps map[string]any,
	schema agents.ObjectSchema,
) (map[string]any, error) {
	if len(schema.Properties) == 0 && len(schema.Required) == 0 {
		return patchProps, nil
	}

	validated := make(map[string]any)
	var validationErrors []string

	for key, value := range patchProps {
		// null means "delete this property" — allowed at the delta level. When
		// mergedProps is non-nil and the deleted key is required, the merged
		// required check below rejects the patch.
		if value == nil {
			validated[key] = nil
			continue
		}

		propDef, hasDef := schema.Properties[key]
		if !hasDef {
			// Unknown properties are passed through as-is (same policy as validateProperties).
			validated[key] = value
			continue
		}

		switch propDef.Type {
		case "number":
			coerced, err := coerceToNumber(value)
			if err != nil {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", key, err))
			} else {
				validated[key] = coerced
			}

		case "boolean":
			coerced, err := coerceToBoolean(value)
			if err != nil {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", key, err))
			} else {
				validated[key] = coerced
			}

		case "date":
			coerced, err := coerceToDate(value)
			if err != nil {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", key, err))
			} else {
				validated[key] = coerced
			}

		case "array":
			if _, ok := value.([]any); !ok {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: expected array, got %T", key, value))
			} else {
				validated[key] = value
			}

		case "object":
			if _, ok := value.(map[string]any); !ok {
				validationErrors = append(validationErrors, fmt.Sprintf("%s: expected object, got %T", key, value))
			} else {
				validated[key] = value
			}

		default:
			validated[key] = value
		}
	}

	if mergedProps != nil {
		for _, required := range schema.Required {
			if v, ok := mergedProps[required]; !ok || v == nil {
				validationErrors = append(validationErrors, fmt.Sprintf("missing required field: %s", required))
			}
		}
	}

	if len(validationErrors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(validationErrors, "; "))
	}

	return validated, nil
}

// validateRelationship validates a relationship against the installed schema.
// It checks: type is allowed, source/destination object types satisfy fromTypes/toTypes constraints,
// and relationship properties are valid. Returns nil if the schema has no relationship schemas
// (nil RelationshipSchemas map), meaning no schema is installed.
func validateRelationship(
	relType string,
	srcObjType string,
	dstObjType string,
	props map[string]any,
	schemas *ExtractionSchemas,
) error {
	if schemas == nil || schemas.RelationshipSchemas == nil {
		return nil
	}

	relSchema, ok := schemas.RelationshipSchemas[relType]
	if !ok {
		// Unknown relationship types are allowed — the schema defines constraints
		// for known types but does not act as an allowlist. Users may create
		// relationships with any type name, including domain-specific ones like
		// requires, calls, has_step, etc.
		return nil
	}

	// SourceTypes and TargetTypes in the schema are informational — they document
	// the intended source/target object types but do not act as hard enforcement
	// gates. Users may connect any object types via a known relationship type
	// (e.g. Scenario --contains--> Action) without needing every combination
	// pre-registered in the schema.

	if len(relSchema.Properties) > 0 || len(relSchema.Required) > 0 {
		objSchema := agents.ObjectSchema{
			Properties: relSchema.Properties,
			Required:   relSchema.Required,
		}
		if _, err := validateProperties(props, objSchema); err != nil {
			return err
		}
	}

	return nil
}
