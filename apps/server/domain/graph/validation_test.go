package graph

import (
	"strings"
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

func TestCoerceToURL(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		expected  string
		expectErr bool
	}{
		{"https url", "https://example.com/docs?x=1", "https://example.com/docs?x=1", false},
		{"http url", "http://example.com", "http://example.com", false},
		{"trimmed", "  https://example.com/a  ", "https://example.com/a", false},
		{"missing scheme", "example.com/docs", "", true},
		{"non-http scheme", "ftp://example.com/file", "", true},
		{"missing host", "https:///path", "", true},
		{"empty string", "", "", true},
		{"non-string", 42, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := coerceToURL(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestCoerceToHash(t *testing.T) {
	sha256Value := "3b1f2e4c5d6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e"
	tests := []struct {
		name      string
		kind      string
		input     any
		expected  string
		expectErr bool
	}{
		{"md5 lower", "md5", "d41d8cd98f00b204e9800998ecf8427e", "d41d8cd98f00b204e9800998ecf8427e", false},
		{"sha1 upper normalised", "sha1", "DA39A3EE5E6B4B0D3255BFEF95601890AFD80709", "da39a3ee5e6b4b0d3255bfef95601890afd80709", false},
		{"sha256", "sha256", sha256Value, sha256Value, false},
		{"trimmed", "md5", "  d41d8cd98f00b204e9800998ecf8427e  ", "d41d8cd98f00b204e9800998ecf8427e", false},
		{"wrong length", "md5", "d41d8cd98f00b204e9800998ecf8427", "", true},
		{"non-hex", "md5", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "", true},
		{"sha512 wrong length", "sha512", sha256Value, "", true},
		{"unknown kind", "crc32", "d41d8cd9", "", true},
		{"non-string", "md5", 123, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := coerceToHash(tt.input, tt.kind)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValidatePropertiesURLAndHash(t *testing.T) {
	schema := agents.ObjectSchema{
		Name: "Link",
		Properties: map[string]agents.PropertyDef{
			"homepage": {Type: "url"},
			"checksum": {Type: "sha256"},
		},
	}
	sum := "3b1f2e4c5d6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e"

	t.Run("valid values are coerced", func(t *testing.T) {
		out, err := validateProperties(map[string]any{
			"homepage": " https://example.com ",
			"checksum": strings.ToUpper(sum),
		}, schema)
		assert.NoError(t, err)
		assert.Equal(t, "https://example.com", out["homepage"])
		assert.Equal(t, sum, out["checksum"])
	})

	t.Run("invalid url is rejected", func(t *testing.T) {
		_, err := validateProperties(map[string]any{"homepage": "not a url"}, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "homepage")
	})

	t.Run("invalid hash is rejected", func(t *testing.T) {
		_, err := validateProperties(map[string]any{"checksum": "short"}, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checksum")
	})
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

	t.Run("unknown properties passed through when schema has properties", func(t *testing.T) {
		// Schema is not an allowlist — unknown properties are stored as-is.
		props := map[string]any{
			"name":            "John Doe",
			"age":             25,
			"unknown_prop":    "some value",
			"another_unknown": 123,
		}

		out, err := validateProperties(props, schema)
		assert.NoError(t, err)
		assert.Equal(t, "some value", out["unknown_prop"])
		assert.Equal(t, 123, out["another_unknown"])
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

	t.Run("relationship type not in schema is passed through", func(t *testing.T) {
		// Schema is not an allowlist — unknown relationship types are allowed.
		// Users may create relationships with any type name.
		err := validateRelationship("UNKNOWN_REL", "Person", "Company", map[string]any{"since": 2020}, schemas)
		assert.NoError(t, err)
	})

	t.Run("fromTypes mismatch is allowed (source types are informational)", func(t *testing.T) {
		// SourceTypes in the schema are informational — they document the intended
		// source object types but do not act as hard enforcement gates. Users may
		// connect any object types via a known relationship type.
		err := validateRelationship("WORKS_AT", "Animal", "Company", map[string]any{"since": 2020}, schemas)
		assert.NoError(t, err)
	})

	t.Run("toTypes mismatch is allowed (target types are informational)", func(t *testing.T) {
		// TargetTypes in the schema are informational — they document the intended
		// target object types but do not act as hard enforcement gates.
		err := validateRelationship("WORKS_AT", "Person", "School", map[string]any{"since": 2020}, schemas)
		assert.NoError(t, err)
	})

	t.Run("unknown property is passed through", func(t *testing.T) {
		// Schema is not an allowlist — unknown properties are stored as-is.
		err := validateRelationship("WORKS_AT", "Person", "Company", map[string]any{"since": 2020, "extra": "fine"}, schemas)
		assert.NoError(t, err)
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

	t.Run("unknown property in delta is passed through", func(t *testing.T) {
		// Schema is not an allowlist — unknown properties are stored as-is.
		out, err := validatePatchProperties(map[string]any{"legacy_field": "old"}, schema)
		assert.NoError(t, err)
		assert.Equal(t, "old", out["legacy_field"])
	})

	t.Run("required field missing from delta does NOT cause error (object patch)", func(t *testing.T) {
		// name is required in schema but is not in this patch — should pass because
		// the object patch path validates only the delta and does not enforce required.
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

func TestValidateRelationshipPatchProperties(t *testing.T) {
	schema := agents.ObjectSchema{
		Name: "Person",
		Properties: map[string]agents.PropertyDef{
			"name":  {Type: "string"},
			"age":   {Type: "number"},
			"since": {Type: "number"},
		},
		Required: []string{"name"},
	}

	t.Run("required field present in merged result passes even if absent from delta", func(t *testing.T) {
		// "name" is required and already stored; the patch only touches "age".
		// Required enforcement is on the merged result, so this passes.
		_, err := validateRelationshipPatchProperties(map[string]any{"age": 30}, map[string]any{"name": "Alice", "age": 30}, schema)
		assert.NoError(t, err)
	})

	t.Run("required field absent from merged result fails", func(t *testing.T) {
		// The relationship has no "name" and the patch doesn't add it — rejected.
		_, err := validateRelationshipPatchProperties(map[string]any{"age": 30}, map[string]any{"age": 30}, schema)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "name")
		}
	})

	t.Run("clearing a required field fails", func(t *testing.T) {
		// Patch sets "name" to null (delete) — the merged result drops it, so it fails.
		_, err := validateRelationshipPatchProperties(map[string]any{"name": nil}, map[string]any{"age": 30}, schema)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "name")
		}
	})

	t.Run("delta with wrong type fails", func(t *testing.T) {
		// "age" is a number; patching it with a non-coercible string fails.
		_, err := validateRelationshipPatchProperties(map[string]any{"age": "not-a-number"}, map[string]any{"name": "Alice", "age": "not-a-number"}, schema)
		if assert.Error(t, err) {
			assert.Contains(t, err.Error(), "age")
		}
	})

	t.Run("unknown property in delta is passed through", func(t *testing.T) {
		out, err := validateRelationshipPatchProperties(map[string]any{"legacy_field": "old"}, map[string]any{"name": "Alice", "legacy_field": "old"}, schema)
		assert.NoError(t, err)
		assert.Equal(t, "old", out["legacy_field"])
	})

	t.Run("type coercion works on patch delta", func(t *testing.T) {
		out, err := validateRelationshipPatchProperties(map[string]any{"age": "42"}, map[string]any{"name": "Alice", "age": "42"}, schema)
		assert.NoError(t, err)
		assert.Equal(t, float64(42), out["age"])
	})
}

func TestApplySchemaDefaults(t *testing.T) {
	schema := agents.ObjectSchema{
		Properties: map[string]agents.PropertyDef{
			"source":       {Type: "string", Default: "inferred"},
			"confidence":   {Type: "number"},
			"tier":         {Type: "string", Default: "archival"},
			"use_count":    {Type: "number", Default: float64(0)},
			"needs_review": {Type: "boolean", Default: false},
		},
	}

	tests := []struct {
		name     string
		props    map[string]any
		wantConf float64
	}{
		{"explicit source", map[string]any{"source": "explicit"}, 1.0},
		{"corrected source", map[string]any{"source": "corrected"}, 0.9},
		{"inferred source", map[string]any{"source": "inferred"}, 0.7},
		{"no source defaults to inferred", map[string]any{}, 0.7},
		{"preserves existing confidence", map[string]any{"confidence": 0.42}, 0.42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applySchemaDefaults(tt.props, schema)

			assert.Equal(t, tt.wantConf, tt.props["confidence"])
			assert.Equal(t, "archival", tt.props["tier"])
			assert.Equal(t, float64(0), tt.props["use_count"])
			assert.Equal(t, false, tt.props["needs_review"])
		})
	}

	t.Run("schema without confidence leaves props untouched", func(t *testing.T) {
		props := map[string]any{"name": "x"}
		applySchemaDefaults(props, agents.ObjectSchema{
			Properties: map[string]agents.PropertyDef{"name": {Type: "string"}},
		})
		assert.Equal(t, "x", props["name"])
		assert.NotContains(t, props, "confidence")
	})
}

// validatePatchProperties and validateRelationshipPatchProperties share the
// delta coercion switch with validateProperties; these pin the url/hash arms so
// the three copies cannot drift.
func TestValidatePatchPropertiesURLAndHash(t *testing.T) {
	schema := agents.ObjectSchema{
		Name: "Link",
		Properties: map[string]agents.PropertyDef{
			"homepage": {Type: "url"},
			"checksum": {Type: "sha256"},
			"note":     {Type: "string"},
		},
	}
	sum := "3b1f2e4c5d6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e"

	t.Run("valid delta coerced and normalised", func(t *testing.T) {
		out, err := validatePatchProperties(map[string]any{
			"homepage": "  https://example.com/docs  ",
			"checksum": strings.ToUpper(sum),
		}, schema)
		assert.NoError(t, err)
		assert.Equal(t, "https://example.com/docs", out["homepage"])
		assert.Equal(t, sum, out["checksum"])
	})

	t.Run("invalid url in the delta is rejected", func(t *testing.T) {
		_, err := validatePatchProperties(map[string]any{"homepage": "not a url"}, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "homepage")
	})

	t.Run("invalid digest in the delta is rejected", func(t *testing.T) {
		_, err := validatePatchProperties(map[string]any{"checksum": "abc"}, schema)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checksum")
	})

	t.Run("null clears without coercion", func(t *testing.T) {
		out, err := validatePatchProperties(map[string]any{"homepage": nil}, schema)
		assert.NoError(t, err)
		value, ok := out["homepage"]
		if !ok || value != nil {
			t.Errorf("null = %v (present %v), want nil", value, ok)
		}
	})

	t.Run("unrelated delta is not blocked by a stale value", func(t *testing.T) {
		// A value stored before the type existed may be malformed; a patch that
		// does not touch it must not re-validate it.
		out, err := validateRelationshipPatchProperties(
			map[string]any{"note": "hello"},
			map[string]any{"homepage": "not-a-url", "checksum": "short"},
			schema,
		)
		assert.NoError(t, err)
		assert.Equal(t, "hello", out["note"])
		if _, ok := out["homepage"]; ok {
			t.Errorf("untouched key must not be echoed back: %v", out)
		}
	})
}

// PropertyValidator has its own coercion switch (domain/graph/service.go's
// wrapper path); it must validate the new types too.
func TestPropertyValidatorURLAndHash(t *testing.T) {
	sum := "3b1f2e4c5d6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e"
	schema := &agents.ObjectSchema{
		Name: "Link",
		Properties: map[string]agents.PropertyDef{
			"homepage": {Type: "url"},
			"checksum": {Type: "sha256"},
		},
	}
	v := NewPropertyValidator()

	ok := v.ValidateProperties(map[string]any{
		"homepage": " https://example.com ",
		"checksum": strings.ToUpper(sum),
	}, schema)
	assert.True(t, ok.Valid, "errors: %v", ok.Errors)
	assert.Equal(t, "https://example.com", ok.Coerced["homepage"])
	assert.Equal(t, sum, ok.Coerced["checksum"])

	bad := v.ValidateProperties(map[string]any{
		"homepage": "not a url",
		"checksum": "abc",
	}, schema)
	assert.False(t, bad.Valid)
	fields := make([]string, 0, len(bad.Errors))
	for _, e := range bad.Errors {
		fields = append(fields, e.Field)
	}
	assert.ElementsMatch(t, []string{"homepage", "checksum"}, fields)
}
