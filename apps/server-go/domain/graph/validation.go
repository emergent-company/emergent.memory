package graph

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emergent/emergent-core/domain/extraction/agents"
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
