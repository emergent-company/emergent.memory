package graph

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/stretchr/testify/assert"
)

func TestCoerceToNumber(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		expected  float64
		expectErr bool
	}{
		{"string integer", "42", 42.0, false},
		{"string float", "3.14", 3.14, false},
		{"string negative", "-10.5", -10.5, false},
		{"actual number", 25.0, 25.0, false},
		{"actual int", 25, 25.0, false},
		{"boolean true", true, 1.0, false},
		{"boolean false", false, 0.0, false},
		{"invalid string", "not-a-number", 0, true},
		{"empty string", "", 0, true},
		{"null", nil, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := coerceToNumber(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestCoerceToBoolean(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		expected  bool
		expectErr bool
	}{
		{"string true", "true", true, false},
		{"string t", "t", true, false},
		{"string T", "T", true, false},
		{"string yes", "yes", true, false},
		{"string YES", "YES", true, false},
		{"string y", "y", true, false},
		{"string 1", "1", true, false},
		{"string false", "false", false, false},
		{"string f", "f", false, false},
		{"string no", "no", false, false},
		{"string n", "n", false, false},
		{"string 0", "0", false, false},
		{"empty string", "", false, false},
		{"actual bool true", true, true, false},
		{"actual bool false", false, false, false},
		{"number 1", 1, true, false},
		{"number 0", 0, false, false},
		{"invalid string", "maybe", false, true},
		{"null", nil, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := coerceToBoolean(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestCoerceToDate(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		expected  string
		expectErr bool
	}{
		{"ISO date", "2024-02-10", "2024-02-10T00:00:00Z", false},
		{"ISO datetime", "2024-02-10T15:30:00Z", "2024-02-10T15:30:00Z", false},
		{"ISO datetime with offset", "2024-02-10T15:30:00+01:00", "2024-02-10T15:30:00+01:00", false},
		{"US format", "02/10/2024", "2024-02-10T00:00:00Z", false},
		{"datetime with space", "2024-02-10 15:30:00", "2024-02-10T15:30:00Z", false},
		{"invalid format", "not-a-date", "", true},
		{"empty string", "", "", true},
		{"null", nil, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := coerceToDate(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValidateProperties(t *testing.T) {
	schema := agents.ObjectSchema{
		Name: "TestObject",
		Properties: map[string]agents.PropertyDef{
			"age": {
				Type:        "number",
				Description: "Age in years",
			},
			"active": {
				Type:        "boolean",
				Description: "Active status",
			},
			"birth_date": {
				Type:        "date",
				Description: "Date of birth",
			},
			"name": {
				Type:        "string",
				Description: "Full name",
			},
		},
		Required: []string{"name", "age"},
	}

	t.Run("valid properties with coercion", func(t *testing.T) {
		props := map[string]any{
			"name":       "John Doe",
			"age":        "25",
			"active":     "true",
			"birth_date": "2024-02-10",
		}

		result, err := validateProperties(props, schema)
		assert.NoError(t, err)
		assert.Equal(t, "John Doe", result["name"])
		assert.Equal(t, 25.0, result["age"])
		assert.Equal(t, true, result["active"])
		assert.Equal(t, "2024-02-10T00:00:00Z", result["birth_date"])
	})

	t.Run("missing required property", func(t *testing.T) {
		props := map[string]any{
			"name": "John Doe",
		}

		_, err := validateProperties(props, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "age")
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("invalid number", func(t *testing.T) {
		props := map[string]any{
			"name": "John Doe",
			"age":  "not-a-number",
		}

		_, err := validateProperties(props, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "age")
	})

	t.Run("invalid boolean", func(t *testing.T) {
		props := map[string]any{
			"name":   "John Doe",
			"age":    25,
			"active": "maybe",
		}

		_, err := validateProperties(props, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "active")
	})

	t.Run("invalid date", func(t *testing.T) {
		props := map[string]any{
			"name":       "John Doe",
			"age":        25,
			"birth_date": "not-a-date",
		}

		_, err := validateProperties(props, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "birth_date")
	})

	t.Run("unknown properties rejected when schema has properties", func(t *testing.T) {
		props := map[string]any{
			"name":            "John Doe",
			"age":             25,
			"unknown_prop":    "some value",
			"another_unknown": 123,
		}

		_, err := validateProperties(props, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown property")
	})

	t.Run("unknown properties allowed when schema has no properties", func(t *testing.T) {
		emptySchema := agents.ObjectSchema{
			Name:       "AnyObject",
			Properties: map[string]agents.PropertyDef{},
			Required:   []string{},
		}
		props := map[string]any{
			"whatever": "goes",
			"anything": 42,
		}

		result, err := validateProperties(props, emptySchema)
		assert.NoError(t, err)
		assert.Equal(t, props, result)
	})

	t.Run("multiple validation errors", func(t *testing.T) {
		props := map[string]any{
			"age":        "not-a-number",
			"active":     "maybe",
			"birth_date": "invalid-date",
		}

		_, err := validateProperties(props, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "age")
		assert.Contains(t, err.Error(), "active")
		assert.Contains(t, err.Error(), "birth_date")
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("empty properties with no required fields", func(t *testing.T) {
		emptySchema := agents.ObjectSchema{
			Name:       "EmptyObject",
			Properties: map[string]agents.PropertyDef{},
			Required:   []string{},
		}

		result, err := validateProperties(map[string]any{}, emptySchema)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestValidateRelationship(t *testing.T) {
	schemas := &ExtractionSchemas{
		RelationshipSchemas: map[string]agents.RelationshipSchema{
			"WORKS_AT": {
				Name:        "WORKS_AT",
				SourceTypes: []string{"Person"},
				TargetTypes: []string{"Company"},
				Properties: map[string]agents.PropertyDef{
					"since": {Type: "number", Description: "Year started"},
				},
				Required: []string{"since"},
			},
		},
	}

	t.Run("nil schemas passes through", func(t *testing.T) {
		err := validateRelationship("ANY_TYPE", "Foo", "Bar", nil, nil)
		assert.NoError(t, err)
	})

	t.Run("nil relationship schemas passes through", func(t *testing.T) {
		err := validateRelationship("ANY_TYPE", "Foo", "Bar", nil, &ExtractionSchemas{})
		assert.NoError(t, err)
	})

	t.Run("relationship type not in schema returns error", func(t *testing.T) {
		err := validateRelationship("UNKNOWN_REL", "Person", "Company", map[string]any{"since": 2020}, schemas)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "relationship_type_not_allowed")
	})

	t.Run("fromTypes mismatch returns error", func(t *testing.T) {
		err := validateRelationship("WORKS_AT", "Animal", "Company", map[string]any{"since": 2020}, schemas)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "relationship_source_type_not_allowed")
	})

	t.Run("toTypes mismatch returns error", func(t *testing.T) {
		err := validateRelationship("WORKS_AT", "Person", "School", map[string]any{"since": 2020}, schemas)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "relationship_target_type_not_allowed")
	})

	t.Run("unknown property returns error", func(t *testing.T) {
		err := validateRelationship("WORKS_AT", "Person", "Company", map[string]any{"since": 2020, "extra": "nope"}, schemas)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown property")
	})

	t.Run("missing required property returns error", func(t *testing.T) {
		err := validateRelationship("WORKS_AT", "Person", "Company", map[string]any{}, schemas)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "since")
	})

	t.Run("valid relationship passes", func(t *testing.T) {
		err := validateRelationship("WORKS_AT", "Person", "Company", map[string]any{"since": 2020}, schemas)
		assert.NoError(t, err)
	})
}

func TestValidatePatchProperties(t *testing.T) {
	schema := agents.ObjectSchema{
		Name: "Person",
		Properties: map[string]agents.PropertyDef{
			"name":  {Type: "string"},
			"age":   {Type: "number"},
			"since": {Type: "number"},
		},
		Required: []string{"name"},
	}

	t.Run("known property in delta passes", func(t *testing.T) {
		out, err := validatePatchProperties(map[string]any{"name": "Alice"}, schema)
		assert.NoError(t, err)
		assert.Equal(t, "Alice", out["name"])
	})

	t.Run("unknown property in delta is rejected", func(t *testing.T) {
		_, err := validatePatchProperties(map[string]any{"legacy_field": "old"}, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown property: legacy_field")
	})

	t.Run("required field missing from delta does NOT cause error (delta-only check)", func(t *testing.T) {
		// name is required in schema but is not in this patch — should pass because
		// required is only enforced at Create time, not on subsequent patches.
		_, err := validatePatchProperties(map[string]any{"age": 30}, schema)
		assert.NoError(t, err)
	})

	t.Run("nil value (delete) always allowed even if property would be unknown", func(t *testing.T) {
		// Deleting a key that isn't in the schema is allowed (it may have been written
		// under an older schema version).
		out, err := validatePatchProperties(map[string]any{"legacy_field": nil}, schema)
		assert.NoError(t, err)
		assert.Nil(t, out["legacy_field"])
	})

	t.Run("empty schema passes everything through", func(t *testing.T) {
		_, err := validatePatchProperties(map[string]any{"anything": "value"}, agents.ObjectSchema{})
		assert.NoError(t, err)
	})

	t.Run("type coercion works on patch delta", func(t *testing.T) {
		out, err := validatePatchProperties(map[string]any{"age": "42"}, schema)
		assert.NoError(t, err)
		assert.Equal(t, float64(42), out["age"])
	})
}
