package extraction

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/emergent-company/emergent/domain/extraction/agents"
	"github.com/emergent-company/emergent/internal/config"
)

func TestConvertToObjectSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected agents.ObjectSchema
	}{
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: agents.ObjectSchema{},
		},
		{
			name: "with name only",
			input: map[string]any{
				"name": "Person",
			},
			expected: agents.ObjectSchema{
				Name: "Person",
			},
		},
		{
			name: "with name and description",
			input: map[string]any{
				"name":        "Organization",
				"description": "A company or institution",
			},
			expected: agents.ObjectSchema{
				Name:        "Organization",
				Description: "A company or institution",
			},
		},
		{
			name: "with properties",
			input: map[string]any{
				"name": "Person",
				"properties": map[string]any{
					"age": map[string]any{
						"type":        "integer",
						"description": "Age in years",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Full name",
					},
				},
			},
			expected: agents.ObjectSchema{
				Name: "Person",
				Properties: map[string]agents.PropertyDef{
					"age": {
						Type:        "integer",
						Description: "Age in years",
					},
					"name": {
						Type:        "string",
						Description: "Full name",
					},
				},
			},
		},
		{
			name: "with required fields",
			input: map[string]any{
				"name":     "Document",
				"required": []any{"title", "content"},
			},
			expected: agents.ObjectSchema{
				Name:     "Document",
				Required: []string{"title", "content"},
			},
		},
		{
			name: "with all fields",
			input: map[string]any{
				"name":        "Event",
				"description": "A calendar event",
				"properties": map[string]any{
					"date": map[string]any{
						"type":        "date",
						"description": "Event date",
					},
				},
				"required": []any{"date"},
			},
			expected: agents.ObjectSchema{
				Name:        "Event",
				Description: "A calendar event",
				Properties: map[string]agents.PropertyDef{
					"date": {
						Type:        "date",
						Description: "Event date",
					},
				},
				Required: []string{"date"},
			},
		},
		{
			name: "with invalid property type (not map)",
			input: map[string]any{
				"name": "Test",
				"properties": map[string]any{
					"invalid": "not a map",
				},
			},
			expected: agents.ObjectSchema{
				Name:       "Test",
				Properties: map[string]agents.PropertyDef{},
			},
		},
		{
			name: "with invalid required type",
			input: map[string]any{
				"name":     "Test",
				"required": []any{123, "valid", true},
			},
			expected: agents.ObjectSchema{
				Name:     "Test",
				Required: []string{"valid"},
			},
		},
		{
			name: "with wrong types for name/description",
			input: map[string]any{
				"name":        123,
				"description": true,
			},
			expected: agents.ObjectSchema{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToObjectSchema(tt.input)

			if result.Name != tt.expected.Name {
				t.Errorf("Name = %v, want %v", result.Name, tt.expected.Name)
			}
			if result.Description != tt.expected.Description {
				t.Errorf("Description = %v, want %v", result.Description, tt.expected.Description)
			}

			// Check properties
			if len(result.Properties) != len(tt.expected.Properties) {
				t.Errorf("Properties len = %v, want %v", len(result.Properties), len(tt.expected.Properties))
			}
			for k, v := range tt.expected.Properties {
				if got, ok := result.Properties[k]; !ok {
					t.Errorf("Missing property %q", k)
				} else if got != v {
					t.Errorf("Property[%q] = %v, want %v", k, got, v)
				}
			}

			// Check required
			if len(result.Required) != len(tt.expected.Required) {
				t.Errorf("Required len = %v, want %v", len(result.Required), len(tt.expected.Required))
			}
			for i, v := range tt.expected.Required {
				if i < len(result.Required) && result.Required[i] != v {
					t.Errorf("Required[%d] = %v, want %v", i, result.Required[i], v)
				}
			}
		})
	}
}

func TestConvertToRelationshipSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected agents.RelationshipSchema
	}{
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: agents.RelationshipSchema{},
		},
		{
			name: "with name only",
			input: map[string]any{
				"name": "WORKS_FOR",
			},
			expected: agents.RelationshipSchema{
				Name: "WORKS_FOR",
			},
		},
		{
			name: "with name and description",
			input: map[string]any{
				"name":        "MANAGES",
				"description": "Management relationship",
			},
			expected: agents.RelationshipSchema{
				Name:        "MANAGES",
				Description: "Management relationship",
			},
		},
		{
			name: "with source_types",
			input: map[string]any{
				"name":         "OWNS",
				"source_types": []any{"Person", "Organization"},
			},
			expected: agents.RelationshipSchema{
				Name:        "OWNS",
				SourceTypes: []string{"Person", "Organization"},
			},
		},
		{
			name: "with target_types",
			input: map[string]any{
				"name":         "OWNS",
				"target_types": []any{"Asset", "Property"},
			},
			expected: agents.RelationshipSchema{
				Name:        "OWNS",
				TargetTypes: []string{"Asset", "Property"},
			},
		},
		{
			name: "with extraction_guidelines",
			input: map[string]any{
				"name":                  "LOCATED_IN",
				"extraction_guidelines": "Extract geographic relationships",
			},
			expected: agents.RelationshipSchema{
				Name:                 "LOCATED_IN",
				ExtractionGuidelines: "Extract geographic relationships",
			},
		},
		{
			name: "with all fields",
			input: map[string]any{
				"name":                  "EMPLOYS",
				"description":           "Employment relationship",
				"source_types":          []any{"Organization"},
				"target_types":          []any{"Person"},
				"extraction_guidelines": "Look for employment verbs",
			},
			expected: agents.RelationshipSchema{
				Name:                 "EMPLOYS",
				Description:          "Employment relationship",
				SourceTypes:          []string{"Organization"},
				TargetTypes:          []string{"Person"},
				ExtractionGuidelines: "Look for employment verbs",
			},
		},
		{
			name: "with invalid source_types values",
			input: map[string]any{
				"name":         "TEST",
				"source_types": []any{123, "Valid", false},
			},
			expected: agents.RelationshipSchema{
				Name:        "TEST",
				SourceTypes: []string{"Valid"},
			},
		},
		{
			name: "with invalid target_types values",
			input: map[string]any{
				"name":         "TEST",
				"target_types": []any{nil, "Valid"},
			},
			expected: agents.RelationshipSchema{
				Name:        "TEST",
				TargetTypes: []string{"Valid"},
			},
		},
		{
			name: "with wrong types for name/description",
			input: map[string]any{
				"name":        []string{"wrong"},
				"description": 123,
			},
			expected: agents.RelationshipSchema{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToRelationshipSchema(tt.input)

			if result.Name != tt.expected.Name {
				t.Errorf("Name = %v, want %v", result.Name, tt.expected.Name)
			}
			if result.Description != tt.expected.Description {
				t.Errorf("Description = %v, want %v", result.Description, tt.expected.Description)
			}
			if result.ExtractionGuidelines != tt.expected.ExtractionGuidelines {
				t.Errorf("ExtractionGuidelines = %v, want %v", result.ExtractionGuidelines, tt.expected.ExtractionGuidelines)
			}

			// Check source_types
			if len(result.SourceTypes) != len(tt.expected.SourceTypes) {
				t.Errorf("SourceTypes len = %v, want %v", len(result.SourceTypes), len(tt.expected.SourceTypes))
			}
			for i, v := range tt.expected.SourceTypes {
				if i < len(result.SourceTypes) && result.SourceTypes[i] != v {
					t.Errorf("SourceTypes[%d] = %v, want %v", i, result.SourceTypes[i], v)
				}
			}

			// Check target_types
			if len(result.TargetTypes) != len(tt.expected.TargetTypes) {
				t.Errorf("TargetTypes len = %v, want %v", len(result.TargetTypes), len(tt.expected.TargetTypes))
			}
			for i, v := range tt.expected.TargetTypes {
				if i < len(result.TargetTypes) && result.TargetTypes[i] != v {
					t.Errorf("TargetTypes[%d] = %v, want %v", i, result.TargetTypes[i], v)
				}
			}
		})
	}
}

func TestStringPtr(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "simple string",
			input: "hello",
		},
		{
			name:  "string with spaces",
			input: "hello world",
		},
		{
			name:  "string with special chars",
			input: "hello\nworld\ttab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringPtr(tt.input)
			if result == nil {
				t.Error("stringPtr() returned nil")
				return
			}
			if *result != tt.input {
				t.Errorf("*stringPtr(%q) = %q, want %q", tt.input, *result, tt.input)
			}
		})
	}
}

func TestVectorToString(t *testing.T) {
	tests := []struct {
		name     string
		input    []float32
		expected string
	}{
		{
			name:     "empty slice",
			input:    []float32{},
			expected: "[]",
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: "[]",
		},
		{
			name:     "single element",
			input:    []float32{1.0},
			expected: "[1.000000]",
		},
		{
			name:     "multiple elements",
			input:    []float32{1.0, 2.5, 3.75},
			expected: "[1.000000,2.500000,3.750000]",
		},
		{
			name:     "negative values",
			input:    []float32{-1.5, 0, 1.5},
			expected: "[-1.500000,0.000000,1.500000]",
		},
		{
			name:     "very small values",
			input:    []float32{0.001, 0.0001},
			expected: "[0.001000,0.000100]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vectorToString(tt.input)
			if result != tt.expected {
				t.Errorf("vectorToString(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPtrToString(t *testing.T) {
	tests := []struct {
		name     string
		input    *string
		expected string
	}{
		{
			name:     "nil pointer",
			input:    nil,
			expected: "",
		},
		{
			name:     "empty string pointer",
			input:    strPtr(""),
			expected: "",
		},
		{
			name:     "simple string pointer",
			input:    strPtr("hello"),
			expected: "hello",
		},
		{
			name:     "string with spaces",
			input:    strPtr("hello world"),
			expected: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ptrToString(tt.input)
			if result != tt.expected {
				t.Errorf("ptrToString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}

func TestParseObjectTypeSchemas(t *testing.T) {
	tests := []struct {
		name     string
		input    JSON
		expected map[string]agents.ObjectSchema
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: map[string]agents.ObjectSchema{},
		},
		{
			name:     "empty JSON",
			input:    JSON{},
			expected: map[string]agents.ObjectSchema{},
		},
		{
			name: "single schema",
			input: JSON{
				"Person": map[string]any{
					"description": "A human being",
				},
			},
			expected: map[string]agents.ObjectSchema{
				"Person": {
					Name:        "Person",
					Description: "A human being",
				},
			},
		},
		{
			name: "schema with properties",
			input: JSON{
				"Document": map[string]any{
					"description": "A text document",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "Document title",
						},
					},
					"required": []any{"title"},
				},
			},
			expected: map[string]agents.ObjectSchema{
				"Document": {
					Name:        "Document",
					Description: "A text document",
					Properties: map[string]agents.PropertyDef{
						"title": {
							Type:        "string",
							Description: "Document title",
						},
					},
					Required: []string{"title"},
				},
			},
		},
		{
			name: "schema with extraction_guidelines",
			input: JSON{
				"Event": map[string]any{
					"description":           "A calendar event",
					"extraction_guidelines": "Extract dates and times",
				},
			},
			expected: map[string]agents.ObjectSchema{
				"Event": {
					Name:                 "Event",
					Description:          "A calendar event",
					ExtractionGuidelines: "Extract dates and times",
				},
			},
		},
		{
			name: "multiple schemas",
			input: JSON{
				"Person": map[string]any{
					"description": "A person",
				},
				"Organization": map[string]any{
					"description": "An organization",
				},
			},
			expected: map[string]agents.ObjectSchema{
				"Person": {
					Name:        "Person",
					Description: "A person",
				},
				"Organization": {
					Name:        "Organization",
					Description: "An organization",
				},
			},
		},
		{
			name: "invalid schema value (not map)",
			input: JSON{
				"Invalid": "not a map",
			},
			expected: map[string]agents.ObjectSchema{},
		},
		{
			name: "invalid property value (not map)",
			input: JSON{
				"Test": map[string]any{
					"properties": map[string]any{
						"invalid": "not a map",
					},
				},
			},
			expected: map[string]agents.ObjectSchema{
				"Test": {
					Name:       "Test",
					Properties: map[string]agents.PropertyDef{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseObjectTypeSchemas(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("len(result) = %d, want %d", len(result), len(tt.expected))
				return
			}

			for k, expected := range tt.expected {
				got, ok := result[k]
				if !ok {
					t.Errorf("Missing schema %q", k)
					continue
				}
				if got.Name != expected.Name {
					t.Errorf("Schema[%q].Name = %v, want %v", k, got.Name, expected.Name)
				}
				if got.Description != expected.Description {
					t.Errorf("Schema[%q].Description = %v, want %v", k, got.Description, expected.Description)
				}
				if got.ExtractionGuidelines != expected.ExtractionGuidelines {
					t.Errorf("Schema[%q].ExtractionGuidelines = %v, want %v", k, got.ExtractionGuidelines, expected.ExtractionGuidelines)
				}
			}
		})
	}
}

func TestParseRelationshipTypeSchemas(t *testing.T) {
	tests := []struct {
		name     string
		input    JSON
		expected map[string]agents.RelationshipSchema
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: map[string]agents.RelationshipSchema{},
		},
		{
			name:     "empty JSON",
			input:    JSON{},
			expected: map[string]agents.RelationshipSchema{},
		},
		{
			name: "single schema",
			input: JSON{
				"WORKS_FOR": map[string]any{
					"description": "Employment relationship",
				},
			},
			expected: map[string]agents.RelationshipSchema{
				"WORKS_FOR": {
					Name:        "WORKS_FOR",
					Description: "Employment relationship",
				},
			},
		},
		{
			name: "schema with source and target types",
			input: JSON{
				"MANAGES": map[string]any{
					"description":  "Management relationship",
					"source_types": []any{"Person"},
					"target_types": []any{"Person", "Team"},
				},
			},
			expected: map[string]agents.RelationshipSchema{
				"MANAGES": {
					Name:        "MANAGES",
					Description: "Management relationship",
					SourceTypes: []string{"Person"},
					TargetTypes: []string{"Person", "Team"},
				},
			},
		},
		{
			name: "schema with extraction_guidelines",
			input: JSON{
				"OWNS": map[string]any{
					"extraction_guidelines": "Look for ownership verbs",
				},
			},
			expected: map[string]agents.RelationshipSchema{
				"OWNS": {
					Name:                 "OWNS",
					ExtractionGuidelines: "Look for ownership verbs",
				},
			},
		},
		{
			name: "invalid schema value (not map)",
			input: JSON{
				"Invalid": []string{"not", "a", "map"},
			},
			expected: map[string]agents.RelationshipSchema{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRelationshipTypeSchemas(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("len(result) = %d, want %d", len(result), len(tt.expected))
				return
			}

			for k, expected := range tt.expected {
				got, ok := result[k]
				if !ok {
					t.Errorf("Missing schema %q", k)
					continue
				}
				if got.Name != expected.Name {
					t.Errorf("Schema[%q].Name = %v, want %v", k, got.Name, expected.Name)
				}
				if got.Description != expected.Description {
					t.Errorf("Schema[%q].Description = %v, want %v", k, got.Description, expected.Description)
				}
				if got.ExtractionGuidelines != expected.ExtractionGuidelines {
					t.Errorf("Schema[%q].ExtractionGuidelines = %v, want %v", k, got.ExtractionGuidelines, expected.ExtractionGuidelines)
				}
			}
		})
	}
}

func TestApplySchemaOverrides(t *testing.T) {
	tests := []struct {
		name      string
		base      agents.ObjectSchema
		overrides any
		expected  agents.ObjectSchema
	}{
		{
			name: "nil overrides",
			base: agents.ObjectSchema{
				Name:        "Person",
				Description: "Original description",
			},
			overrides: nil,
			expected: agents.ObjectSchema{
				Name:        "Person",
				Description: "Original description",
			},
		},
		{
			name: "non-map overrides",
			base: agents.ObjectSchema{
				Name: "Person",
			},
			overrides: "not a map",
			expected: agents.ObjectSchema{
				Name: "Person",
			},
		},
		{
			name: "override description",
			base: agents.ObjectSchema{
				Name:        "Person",
				Description: "Original",
			},
			overrides: map[string]any{
				"description": "Updated description",
			},
			expected: agents.ObjectSchema{
				Name:        "Person",
				Description: "Updated description",
			},
		},
		{
			name: "override required",
			base: agents.ObjectSchema{
				Name:     "Person",
				Required: []string{"name"},
			},
			overrides: map[string]any{
				"required": []any{"name", "email"},
			},
			expected: agents.ObjectSchema{
				Name:     "Person",
				Required: []string{"name", "email"},
			},
		},
		{
			name: "override extraction_guidelines",
			base: agents.ObjectSchema{
				Name:                 "Event",
				ExtractionGuidelines: "Original guidelines",
			},
			overrides: map[string]any{
				"extraction_guidelines": "New guidelines",
			},
			expected: agents.ObjectSchema{
				Name:                 "Event",
				ExtractionGuidelines: "New guidelines",
			},
		},
		{
			name: "add new property to empty properties",
			base: agents.ObjectSchema{
				Name: "Person",
			},
			overrides: map[string]any{
				"properties": map[string]any{
					"age": map[string]any{
						"type":        "integer",
						"description": "Age in years",
					},
				},
			},
			expected: agents.ObjectSchema{
				Name: "Person",
				Properties: map[string]agents.PropertyDef{
					"age": {
						Type:        "integer",
						Description: "Age in years",
					},
				},
			},
		},
		{
			name: "merge properties",
			base: agents.ObjectSchema{
				Name: "Person",
				Properties: map[string]agents.PropertyDef{
					"name": {
						Type:        "string",
						Description: "Full name",
					},
				},
			},
			overrides: map[string]any{
				"properties": map[string]any{
					"age": map[string]any{
						"type":        "integer",
						"description": "Age",
					},
				},
			},
			expected: agents.ObjectSchema{
				Name: "Person",
				Properties: map[string]agents.PropertyDef{
					"name": {
						Type:        "string",
						Description: "Full name",
					},
					"age": {
						Type:        "integer",
						Description: "Age",
					},
				},
			},
		},
		{
			name: "update existing property",
			base: agents.ObjectSchema{
				Name: "Person",
				Properties: map[string]agents.PropertyDef{
					"name": {
						Type:        "string",
						Description: "Original",
					},
				},
			},
			overrides: map[string]any{
				"properties": map[string]any{
					"name": map[string]any{
						"description": "Updated description",
					},
				},
			},
			expected: agents.ObjectSchema{
				Name: "Person",
				Properties: map[string]agents.PropertyDef{
					"name": {
						Type:        "string",
						Description: "Updated description",
					},
				},
			},
		},
		{
			name: "invalid property override (not map)",
			base: agents.ObjectSchema{
				Name: "Person",
				Properties: map[string]agents.PropertyDef{
					"name": {Type: "string"},
				},
			},
			overrides: map[string]any{
				"properties": map[string]any{
					"invalid": "not a map",
				},
			},
			expected: agents.ObjectSchema{
				Name: "Person",
				Properties: map[string]agents.PropertyDef{
					"name": {Type: "string"},
				},
			},
		},
		{
			name: "invalid required override (non-string values)",
			base: agents.ObjectSchema{
				Name:     "Person",
				Required: []string{"name"},
			},
			overrides: map[string]any{
				"required": []any{123, "email", true},
			},
			expected: agents.ObjectSchema{
				Name:     "Person",
				Required: []string{"email"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applySchemaOverrides(tt.base, tt.overrides)

			if result.Name != tt.expected.Name {
				t.Errorf("Name = %v, want %v", result.Name, tt.expected.Name)
			}
			if result.Description != tt.expected.Description {
				t.Errorf("Description = %v, want %v", result.Description, tt.expected.Description)
			}
			if result.ExtractionGuidelines != tt.expected.ExtractionGuidelines {
				t.Errorf("ExtractionGuidelines = %v, want %v", result.ExtractionGuidelines, tt.expected.ExtractionGuidelines)
			}

			// Check properties
			if len(result.Properties) != len(tt.expected.Properties) {
				t.Errorf("Properties len = %d, want %d", len(result.Properties), len(tt.expected.Properties))
			}
			for k, v := range tt.expected.Properties {
				if got, ok := result.Properties[k]; !ok {
					t.Errorf("Missing property %q", k)
				} else if got.Type != v.Type || got.Description != v.Description {
					t.Errorf("Property[%q] = %+v, want %+v", k, got, v)
				}
			}

			// Check required
			if len(result.Required) != len(tt.expected.Required) {
				t.Errorf("Required len = %d, want %d", len(result.Required), len(tt.expected.Required))
			}
		})
	}
}

func TestJSONScan(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		expectNil   bool
		expectError bool
		checkResult func(*JSON) bool
	}{
		{
			name:      "nil input",
			input:     nil,
			expectNil: true,
		},
		{
			name:        "non-bytes input",
			input:       "not bytes",
			expectNil:   false,
			expectError: false,
		},
		{
			name:  "valid JSON object",
			input: []byte(`{"key": "value", "num": 42}`),
			checkResult: func(j *JSON) bool {
				return (*j)["key"] == "value" && (*j)["num"] == float64(42)
			},
		},
		{
			name:  "empty JSON object",
			input: []byte(`{}`),
			checkResult: func(j *JSON) bool {
				return len(*j) == 0
			},
		},
		{
			name:        "invalid JSON",
			input:       []byte(`{invalid json`),
			expectError: true,
		},
		{
			name:  "nested object",
			input: []byte(`{"outer": {"inner": "value"}}`),
			checkResult: func(j *JSON) bool {
				outer, ok := (*j)["outer"].(map[string]interface{})
				return ok && outer["inner"] == "value"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSON
			err := j.Scan(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Scan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
				return
			}

			if tt.expectNil && j != nil {
				t.Errorf("Scan() expected nil, got %v", j)
				return
			}

			if tt.checkResult != nil && !tt.checkResult(&j) {
				t.Errorf("Scan() result check failed")
			}
		})
	}
}

func TestJSONArrayScan(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		expectNil   bool
		expectError bool
		checkResult func(*JSONArray) bool
	}{
		{
			name:      "nil input",
			input:     nil,
			expectNil: true,
		},
		{
			name:        "non-bytes input",
			input:       123,
			expectNil:   false,
			expectError: false,
		},
		{
			name:  "valid JSON array",
			input: []byte(`["a", "b", "c"]`),
			checkResult: func(j *JSONArray) bool {
				return len(*j) == 3 && (*j)[0] == "a" && (*j)[1] == "b" && (*j)[2] == "c"
			},
		},
		{
			name:  "empty JSON array",
			input: []byte(`[]`),
			checkResult: func(j *JSONArray) bool {
				return len(*j) == 0
			},
		},
		{
			name:        "invalid JSON",
			input:       []byte(`[invalid`),
			expectError: true,
		},
		{
			name:  "mixed types array",
			input: []byte(`[1, "two", true, null]`),
			checkResult: func(j *JSONArray) bool {
				return len(*j) == 4 && (*j)[0] == float64(1) && (*j)[1] == "two" && (*j)[2] == true && (*j)[3] == nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSONArray
			err := j.Scan(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Scan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
				return
			}

			if tt.expectNil && j != nil {
				t.Errorf("Scan() expected nil, got %v", j)
				return
			}

			if tt.checkResult != nil && !tt.checkResult(&j) {
				t.Errorf("Scan() result check failed")
			}
		})
	}
}

func TestTemplatePackCustomizationsScan(t *testing.T) {
	tests := []struct {
		name        string
		input       interface{}
		expectError bool
		checkResult func(*TemplatePackCustomizations) bool
	}{
		{
			name:  "nil input",
			input: nil,
		},
		{
			name:  "valid JSON bytes",
			input: []byte(`{"disabledTypes": ["Person", "Organization"]}`),
			checkResult: func(c *TemplatePackCustomizations) bool {
				return len(c.DisabledTypes) == 2 && c.DisabledTypes[0] == "Person"
			},
		},
		{
			name:  "valid JSON string",
			input: `{"schemaOverrides": {"Event": {"description": "Custom event"}}}`,
			checkResult: func(c *TemplatePackCustomizations) bool {
				return c.SchemaOverrides != nil && c.SchemaOverrides["Event"] != nil
			},
		},
		{
			name:        "invalid JSON",
			input:       []byte(`{invalid`),
			expectError: true,
		},
		{
			name:        "unsupported type",
			input:       123,
			expectError: true,
		},
		{
			name:  "empty object",
			input: []byte(`{}`),
			checkResult: func(c *TemplatePackCustomizations) bool {
				return len(c.DisabledTypes) == 0 && len(c.SchemaOverrides) == 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c TemplatePackCustomizations
			err := c.Scan(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Scan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
				return
			}

			if tt.checkResult != nil && !tt.checkResult(&c) {
				t.Errorf("Scan() result check failed")
			}
		})
	}
}

func TestTemplatePackCustomizationsValue(t *testing.T) {
	tests := []struct {
		name        string
		input       TemplatePackCustomizations
		expectError bool
	}{
		{
			name:  "empty customizations",
			input: TemplatePackCustomizations{},
		},
		{
			name: "with disabled types",
			input: TemplatePackCustomizations{
				DisabledTypes: []string{"Person", "Event"},
			},
		},
		{
			name: "with schema overrides",
			input: TemplatePackCustomizations{
				SchemaOverrides: map[string]any{
					"Document": map[string]any{
						"description": "Custom document",
					},
				},
			},
		},
		{
			name: "with both fields",
			input: TemplatePackCustomizations{
				DisabledTypes: []string{"Person"},
				SchemaOverrides: map[string]any{
					"Event": map[string]any{"description": "Custom"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()

			if tt.expectError {
				if err == nil {
					t.Error("Value() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Value() unexpected error: %v", err)
				return
			}

			// The result should be valid JSON
			bytes, ok := val.([]byte)
			if !ok {
				t.Errorf("Value() expected []byte, got %T", val)
				return
			}

			// Verify it's valid JSON by parsing it back
			var parsed TemplatePackCustomizations
			if err := json.Unmarshal(bytes, &parsed); err != nil {
				t.Errorf("Value() produced invalid JSON: %v", err)
			}
		})
	}
}

func TestTruncateError(t *testing.T) {
	// Note: This tests the truncateError in graph_embedding_jobs.go which has a 1000 char limit
	tests := []struct {
		name        string
		inputLen    int
		expectedLen int
	}{
		{
			name:        "empty string",
			inputLen:    0,
			expectedLen: 0,
		},
		{
			name:        "short string",
			inputLen:    11, // "short error"
			expectedLen: 11,
		},
		{
			name:        "exactly 1000 chars",
			inputLen:    1000,
			expectedLen: 1000,
		},
		{
			name:        "longer than 1000 chars",
			inputLen:    1200,
			expectedLen: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := make([]byte, tt.inputLen)
			for i := range input {
				input[i] = 'a'
			}
			result := truncateError(string(input))
			if len(result) != tt.expectedLen {
				t.Errorf("truncateError() len = %d, want %d", len(result), tt.expectedLen)
			}
		})
	}
}

func TestDefaultChunkEmbeddingConfig(t *testing.T) {
	cfg := DefaultChunkEmbeddingConfig()

	if cfg.BaseRetryDelaySec != 60 {
		t.Errorf("BaseRetryDelaySec = %d, want 60", cfg.BaseRetryDelaySec)
	}
	if cfg.MaxRetryDelaySec != 3600 {
		t.Errorf("MaxRetryDelaySec = %d, want 3600", cfg.MaxRetryDelaySec)
	}
	if cfg.WorkerIntervalMs != 5000 {
		t.Errorf("WorkerIntervalMs = %d, want 5000", cfg.WorkerIntervalMs)
	}
	if cfg.WorkerBatchSize != 10 {
		t.Errorf("WorkerBatchSize = %d, want 10", cfg.WorkerBatchSize)
	}
	if cfg.EnableAdaptiveScaling != false {
		t.Errorf("EnableAdaptiveScaling = %v, want false", cfg.EnableAdaptiveScaling)
	}
	if cfg.MinConcurrency != 5 {
		t.Errorf("MinConcurrency = %d, want 5", cfg.MinConcurrency)
	}
	if cfg.MaxConcurrency != 50 {
		t.Errorf("MaxConcurrency = %d, want 50", cfg.MaxConcurrency)
	}
}

func TestChunkEmbeddingConfigWorkerInterval(t *testing.T) {
	tests := []struct {
		name             string
		workerIntervalMs int
		expectedMs       int
	}{
		{"5000ms", 5000, 5000},
		{"1000ms", 1000, 1000},
		{"100ms", 100, 100},
		{"zero", 0, 0},
		{"60000ms (1 minute)", 60000, 60000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ChunkEmbeddingConfig{WorkerIntervalMs: tt.workerIntervalMs}
			result := cfg.WorkerInterval()
			if result.Milliseconds() != int64(tt.expectedMs) {
				t.Errorf("WorkerInterval() = %v, want %dms", result, tt.expectedMs)
			}
		})
	}
}

func TestDefaultGraphEmbeddingConfig(t *testing.T) {
	cfg := DefaultGraphEmbeddingConfig()

	if cfg.BaseRetryDelaySec != 60 {
		t.Errorf("BaseRetryDelaySec = %d, want 60", cfg.BaseRetryDelaySec)
	}
	if cfg.MaxRetryDelaySec != 3600 {
		t.Errorf("MaxRetryDelaySec = %d, want 3600", cfg.MaxRetryDelaySec)
	}
	if cfg.WorkerIntervalMs != 5000 {
		t.Errorf("WorkerIntervalMs = %d, want 5000", cfg.WorkerIntervalMs)
	}
	if cfg.WorkerBatchSize != 200 {
		t.Errorf("WorkerBatchSize = %d, want 200", cfg.WorkerBatchSize)
	}
	if cfg.EnableAdaptiveScaling != false {
		t.Errorf("EnableAdaptiveScaling = %v, want false", cfg.EnableAdaptiveScaling)
	}
	if cfg.MinConcurrency != 50 {
		t.Errorf("MinConcurrency = %d, want 50", cfg.MinConcurrency)
	}
	if cfg.MaxConcurrency != 500 {
		t.Errorf("MaxConcurrency = %d, want 500", cfg.MaxConcurrency)
	}
}

func TestGraphEmbeddingConfigWorkerInterval(t *testing.T) {
	tests := []struct {
		name             string
		workerIntervalMs int
		expectedMs       int
	}{
		{"5000ms", 5000, 5000},
		{"1000ms", 1000, 1000},
		{"100ms", 100, 100},
		{"zero", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &GraphEmbeddingConfig{WorkerIntervalMs: tt.workerIntervalMs}
			result := cfg.WorkerInterval()
			if result.Milliseconds() != int64(tt.expectedMs) {
				t.Errorf("WorkerInterval() = %v, want %dms", result, tt.expectedMs)
			}
		})
	}
}

func TestDefaultDocumentParsingConfig(t *testing.T) {
	cfg := DefaultDocumentParsingConfig()

	if cfg.BaseRetryDelayMs != 10000 {
		t.Errorf("BaseRetryDelayMs = %d, want 10000", cfg.BaseRetryDelayMs)
	}
	if cfg.MaxRetryDelayMs != 300000 {
		t.Errorf("MaxRetryDelayMs = %d, want 300000", cfg.MaxRetryDelayMs)
	}
	if cfg.RetryMultiplier != 3.0 {
		t.Errorf("RetryMultiplier = %f, want 3.0", cfg.RetryMultiplier)
	}
	if cfg.DefaultMaxRetries != 3 {
		t.Errorf("DefaultMaxRetries = %d, want 3", cfg.DefaultMaxRetries)
	}
	if cfg.WorkerIntervalMs != 5000 {
		t.Errorf("WorkerIntervalMs = %d, want 5000", cfg.WorkerIntervalMs)
	}
	if cfg.WorkerBatchSize != 5 {
		t.Errorf("WorkerBatchSize = %d, want 5", cfg.WorkerBatchSize)
	}
	if cfg.StaleThresholdMinutes != 10 {
		t.Errorf("StaleThresholdMinutes = %d, want 10", cfg.StaleThresholdMinutes)
	}
}

func TestDocumentParsingConfigWorkerInterval(t *testing.T) {
	tests := []struct {
		name             string
		workerIntervalMs int
		expectedMs       int
	}{
		{"5000ms", 5000, 5000},
		{"1000ms", 1000, 1000},
		{"zero", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &DocumentParsingConfig{WorkerIntervalMs: tt.workerIntervalMs}
			result := cfg.WorkerInterval()
			if result.Milliseconds() != int64(tt.expectedMs) {
				t.Errorf("WorkerInterval() = %v, want %dms", result, tt.expectedMs)
			}
		})
	}
}

func TestDefaultObjectExtractionConfig(t *testing.T) {
	cfg := DefaultObjectExtractionConfig()

	if cfg.DefaultMaxRetries != 3 {
		t.Errorf("DefaultMaxRetries = %d, want 3", cfg.DefaultMaxRetries)
	}
	if cfg.WorkerIntervalMs != 5000 {
		t.Errorf("WorkerIntervalMs = %d, want 5000", cfg.WorkerIntervalMs)
	}
	if cfg.WorkerBatchSize != 5 {
		t.Errorf("WorkerBatchSize = %d, want 5", cfg.WorkerBatchSize)
	}
	if cfg.StaleThresholdMinutes != 30 {
		t.Errorf("StaleThresholdMinutes = %d, want 30", cfg.StaleThresholdMinutes)
	}
}

func TestChunkEnqueueOptions(t *testing.T) {
	opts := ChunkEnqueueOptions{
		ChunkID:  "chunk-123",
		Priority: 5,
	}

	if opts.ChunkID != "chunk-123" {
		t.Errorf("ChunkID = %s, want chunk-123", opts.ChunkID)
	}
	if opts.Priority != 5 {
		t.Errorf("Priority = %d, want 5", opts.Priority)
	}
	if opts.ScheduleAt != nil {
		t.Error("ScheduleAt should be nil by default")
	}
}

func TestDefaultObjectExtractionWorkerConfig(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		cfg := DefaultObjectExtractionWorkerConfig()

		if cfg.PollInterval != 5*time.Second {
			t.Errorf("PollInterval = %v, want 5s", cfg.PollInterval)
		}
		if cfg.OrphanThreshold != 0.3 {
			t.Errorf("OrphanThreshold = %v, want 0.3", cfg.OrphanThreshold)
		}
		if cfg.MaxRetries != 3 {
			t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
		}
	})

	t.Run("returns non-nil pointer", func(t *testing.T) {
		cfg := DefaultObjectExtractionWorkerConfig()
		if cfg == nil {
			t.Error("DefaultObjectExtractionWorkerConfig() returned nil")
		}
	})

	t.Run("each call returns new instance", func(t *testing.T) {
		cfg1 := DefaultObjectExtractionWorkerConfig()
		cfg2 := DefaultObjectExtractionWorkerConfig()
		if cfg1 == cfg2 {
			t.Error("DefaultObjectExtractionWorkerConfig() should return new instances")
		}
	})
}

func TestObjectExtractionWorkerConfigStruct(t *testing.T) {
	t.Run("custom values", func(t *testing.T) {
		cfg := &ObjectExtractionWorkerConfig{
			PollInterval:    10 * time.Second,
			OrphanThreshold: 0.5,
			MaxRetries:      5,
		}

		if cfg.PollInterval != 10*time.Second {
			t.Errorf("PollInterval = %v, want 10s", cfg.PollInterval)
		}
		if cfg.OrphanThreshold != 0.5 {
			t.Errorf("OrphanThreshold = %v, want 0.5", cfg.OrphanThreshold)
		}
		if cfg.MaxRetries != 5 {
			t.Errorf("MaxRetries = %d, want 5", cfg.MaxRetries)
		}
	})

	t.Run("zero values", func(t *testing.T) {
		cfg := &ObjectExtractionWorkerConfig{}

		if cfg.PollInterval != 0 {
			t.Errorf("PollInterval = %v, want 0", cfg.PollInterval)
		}
		if cfg.OrphanThreshold != 0 {
			t.Errorf("OrphanThreshold = %v, want 0", cfg.OrphanThreshold)
		}
		if cfg.MaxRetries != 0 {
			t.Errorf("MaxRetries = %d, want 0", cfg.MaxRetries)
		}
	})
}

// =============================================================================
// GraphEmbeddingWorker Tests
// =============================================================================

func TestGraphEmbeddingWorker_Metrics(t *testing.T) {
	t.Run("initial metrics are zero", func(t *testing.T) {
		w := &GraphEmbeddingWorker{}
		m := w.Metrics()

		if m.Processed != 0 {
			t.Errorf("Processed = %d, want 0", m.Processed)
		}
		if m.Succeeded != 0 {
			t.Errorf("Succeeded = %d, want 0", m.Succeeded)
		}
		if m.Failed != 0 {
			t.Errorf("Failed = %d, want 0", m.Failed)
		}
	})

	t.Run("incrementSuccess updates counters", func(t *testing.T) {
		w := &GraphEmbeddingWorker{}

		w.incrementSuccess()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("Processed = %d, want 1", m.Processed)
		}
		if m.Succeeded != 1 {
			t.Errorf("Succeeded = %d, want 1", m.Succeeded)
		}
		if m.Failed != 0 {
			t.Errorf("Failed = %d, want 0", m.Failed)
		}

		w.incrementSuccess()
		w.incrementSuccess()
		m = w.Metrics()
		if m.Processed != 3 {
			t.Errorf("Processed = %d, want 3", m.Processed)
		}
		if m.Succeeded != 3 {
			t.Errorf("Succeeded = %d, want 3", m.Succeeded)
		}
	})

	t.Run("incrementFailure updates counters", func(t *testing.T) {
		w := &GraphEmbeddingWorker{}

		w.incrementFailure()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("Processed = %d, want 1", m.Processed)
		}
		if m.Succeeded != 0 {
			t.Errorf("Succeeded = %d, want 0", m.Succeeded)
		}
		if m.Failed != 1 {
			t.Errorf("Failed = %d, want 1", m.Failed)
		}

		w.incrementFailure()
		m = w.Metrics()
		if m.Processed != 2 {
			t.Errorf("Processed = %d, want 2", m.Processed)
		}
		if m.Failed != 2 {
			t.Errorf("Failed = %d, want 2", m.Failed)
		}
	})

	t.Run("mixed success and failure", func(t *testing.T) {
		w := &GraphEmbeddingWorker{}

		w.incrementSuccess()
		w.incrementSuccess()
		w.incrementFailure()
		w.incrementSuccess()
		w.incrementFailure()

		m := w.Metrics()
		if m.Processed != 5 {
			t.Errorf("Processed = %d, want 5", m.Processed)
		}
		if m.Succeeded != 3 {
			t.Errorf("Succeeded = %d, want 3", m.Succeeded)
		}
		if m.Failed != 2 {
			t.Errorf("Failed = %d, want 2", m.Failed)
		}
	})
}

func TestGraphEmbeddingWorker_Metrics_Concurrent(t *testing.T) {
	w := &GraphEmbeddingWorker{}

	// Run concurrent increments
	const goroutines = 100
	const incrementsPerGoroutine = 100

	done := make(chan bool)

	// Half do success, half do failure
	for i := 0; i < goroutines/2; i++ {
		go func() {
			for j := 0; j < incrementsPerGoroutine; j++ {
				w.incrementSuccess()
			}
			done <- true
		}()
	}
	for i := 0; i < goroutines/2; i++ {
		go func() {
			for j := 0; j < incrementsPerGoroutine; j++ {
				w.incrementFailure()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}

	m := w.Metrics()
	expectedTotal := int64(goroutines * incrementsPerGoroutine)
	expectedEach := int64(goroutines / 2 * incrementsPerGoroutine)

	if m.Processed != expectedTotal {
		t.Errorf("Processed = %d, want %d", m.Processed, expectedTotal)
	}
	if m.Succeeded != expectedEach {
		t.Errorf("Succeeded = %d, want %d", m.Succeeded, expectedEach)
	}
	if m.Failed != expectedEach {
		t.Errorf("Failed = %d, want %d", m.Failed, expectedEach)
	}
}

func TestGraphEmbeddingWorker_IsRunning(t *testing.T) {
	t.Run("initial state is not running", func(t *testing.T) {
		w := &GraphEmbeddingWorker{}
		if w.IsRunning() {
			t.Error("IsRunning() = true, want false")
		}
	})

	t.Run("running state can be set", func(t *testing.T) {
		w := &GraphEmbeddingWorker{running: true}
		if !w.IsRunning() {
			t.Error("IsRunning() = false, want true")
		}
	})
}

func TestGraphEmbeddingWorkerMetrics_Struct(t *testing.T) {
	m := GraphEmbeddingWorkerMetrics{
		Processed: 100,
		Succeeded: 90,
		Failed:    10,
	}

	if m.Processed != 100 {
		t.Errorf("Processed = %d, want 100", m.Processed)
	}
	if m.Succeeded != 90 {
		t.Errorf("Succeeded = %d, want 90", m.Succeeded)
	}
	if m.Failed != 10 {
		t.Errorf("Failed = %d, want 10", m.Failed)
	}
}

func TestGraphEmbeddingWorker_ExtractText(t *testing.T) {
	w := &GraphEmbeddingWorker{}

	tests := []struct {
		name     string
		obj      *graphObjectRow
		expected string
	}{
		{
			name: "type only",
			obj: &graphObjectRow{
				Type: "Person",
			},
			expected: "Person",
		},
		{
			name: "type and key",
			obj: &graphObjectRow{
				Type: "Person",
				Key:  strPtr("john-doe"),
			},
			expected: "Person john-doe",
		},
		{
			name: "with string properties",
			obj: &graphObjectRow{
				Type: "Person",
				Properties: map[string]interface{}{
					"name": "John Doe",
					"city": "New York",
				},
			},
			expected: "Person John Doe New York",
		},
		{
			name: "with number properties",
			obj: &graphObjectRow{
				Type: "Product",
				Properties: map[string]interface{}{
					"price": float64(99.99),
				},
			},
			expected: "Product 99.99",
		},
		{
			name: "with boolean properties",
			obj: &graphObjectRow{
				Type: "Feature",
				Properties: map[string]interface{}{
					"enabled": true,
					"visible": false,
				},
			},
			expected: "Feature true false",
		},
		{
			name: "with array properties",
			obj: &graphObjectRow{
				Type: "Document",
				Properties: map[string]interface{}{
					"tags": []interface{}{"golang", "testing"},
				},
			},
			expected: "Document golang testing",
		},
		{
			name: "with nested object properties",
			obj: &graphObjectRow{
				Type: "Event",
				Properties: map[string]interface{}{
					"metadata": map[string]interface{}{
						"source": "web",
						"count":  float64(42),
					},
				},
			},
			expected: "Event web 42",
		},
		{
			name: "with nil properties",
			obj: &graphObjectRow{
				Type: "Item",
				Properties: map[string]interface{}{
					"name":  "test",
					"value": nil,
				},
			},
			expected: "Item test",
		},
		{
			name: "full example",
			obj: &graphObjectRow{
				Type: "Company",
				Key:  strPtr("acme-corp"),
				Properties: map[string]interface{}{
					"name":     "Acme Corporation",
					"industry": "Technology",
					"founded":  float64(2020),
				},
			},
			expected: "Company acme-corp Acme Corporation Technology 2020",
		},
		{
			name: "empty properties",
			obj: &graphObjectRow{
				Type:       "Empty",
				Properties: map[string]interface{}{},
			},
			expected: "Empty",
		},
		{
			name: "nil properties map",
			obj: &graphObjectRow{
				Type:       "Minimal",
				Properties: nil,
			},
			expected: "Minimal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := w.extractText(tt.obj)
			// Note: map iteration order is not guaranteed, so we check length and words
			resultWords := make(map[string]bool)
			for _, word := range splitWords(result) {
				resultWords[word] = true
			}
			expectedWords := make(map[string]bool)
			for _, word := range splitWords(tt.expected) {
				expectedWords[word] = true
			}

			// First word should always be the type
			if !startsWithWord(result, tt.obj.Type) {
				t.Errorf("extractText() should start with type %q, got %q", tt.obj.Type, result)
			}

			// Check all expected words are present
			for word := range expectedWords {
				if !resultWords[word] {
					t.Errorf("extractText() missing word %q in result %q", word, result)
				}
			}

			// Check word counts match
			if len(resultWords) != len(expectedWords) {
				t.Errorf("extractText() word count = %d, want %d (result: %q, expected: %q)",
					len(resultWords), len(expectedWords), result, tt.expected)
			}
		})
	}
}

// Helper function to split string into words
func splitWords(s string) []string {
	var words []string
	current := ""
	for _, c := range s {
		if c == ' ' {
			if current != "" {
				words = append(words, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

// Helper function to check if string starts with a word
func startsWithWord(s, word string) bool {
	if len(s) < len(word) {
		return false
	}
	return s[:len(word)] == word && (len(s) == len(word) || s[len(word)] == ' ')
}

// =============================================================================
// ChunkEmbeddingWorker Tests
// =============================================================================

func TestChunkEmbeddingWorker_Metrics(t *testing.T) {
	t.Run("initial metrics are zero", func(t *testing.T) {
		w := &ChunkEmbeddingWorker{}
		m := w.Metrics()

		if m.Processed != 0 {
			t.Errorf("Processed = %d, want 0", m.Processed)
		}
		if m.Succeeded != 0 {
			t.Errorf("Succeeded = %d, want 0", m.Succeeded)
		}
		if m.Failed != 0 {
			t.Errorf("Failed = %d, want 0", m.Failed)
		}
	})

	t.Run("incrementSuccess updates counters", func(t *testing.T) {
		w := &ChunkEmbeddingWorker{}

		w.incrementSuccess()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("Processed = %d, want 1", m.Processed)
		}
		if m.Succeeded != 1 {
			t.Errorf("Succeeded = %d, want 1", m.Succeeded)
		}
		if m.Failed != 0 {
			t.Errorf("Failed = %d, want 0", m.Failed)
		}

		w.incrementSuccess()
		w.incrementSuccess()
		m = w.Metrics()
		if m.Processed != 3 {
			t.Errorf("Processed = %d, want 3", m.Processed)
		}
		if m.Succeeded != 3 {
			t.Errorf("Succeeded = %d, want 3", m.Succeeded)
		}
	})

	t.Run("incrementFailure updates counters", func(t *testing.T) {
		w := &ChunkEmbeddingWorker{}

		w.incrementFailure()
		m := w.Metrics()
		if m.Processed != 1 {
			t.Errorf("Processed = %d, want 1", m.Processed)
		}
		if m.Succeeded != 0 {
			t.Errorf("Succeeded = %d, want 0", m.Succeeded)
		}
		if m.Failed != 1 {
			t.Errorf("Failed = %d, want 1", m.Failed)
		}

		w.incrementFailure()
		m = w.Metrics()
		if m.Processed != 2 {
			t.Errorf("Processed = %d, want 2", m.Processed)
		}
		if m.Failed != 2 {
			t.Errorf("Failed = %d, want 2", m.Failed)
		}
	})

	t.Run("mixed success and failure", func(t *testing.T) {
		w := &ChunkEmbeddingWorker{}

		w.incrementSuccess()
		w.incrementSuccess()
		w.incrementFailure()
		w.incrementSuccess()
		w.incrementFailure()

		m := w.Metrics()
		if m.Processed != 5 {
			t.Errorf("Processed = %d, want 5", m.Processed)
		}
		if m.Succeeded != 3 {
			t.Errorf("Succeeded = %d, want 3", m.Succeeded)
		}
		if m.Failed != 2 {
			t.Errorf("Failed = %d, want 2", m.Failed)
		}
	})
}

func TestChunkEmbeddingWorker_Metrics_Concurrent(t *testing.T) {
	w := &ChunkEmbeddingWorker{}

	// Run concurrent increments
	const goroutines = 100
	const incrementsPerGoroutine = 100

	done := make(chan bool)

	// Half do success, half do failure
	for i := 0; i < goroutines/2; i++ {
		go func() {
			for j := 0; j < incrementsPerGoroutine; j++ {
				w.incrementSuccess()
			}
			done <- true
		}()
	}
	for i := 0; i < goroutines/2; i++ {
		go func() {
			for j := 0; j < incrementsPerGoroutine; j++ {
				w.incrementFailure()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}

	m := w.Metrics()
	expectedTotal := int64(goroutines * incrementsPerGoroutine)
	expectedEach := int64(goroutines / 2 * incrementsPerGoroutine)

	if m.Processed != expectedTotal {
		t.Errorf("Processed = %d, want %d", m.Processed, expectedTotal)
	}
	if m.Succeeded != expectedEach {
		t.Errorf("Succeeded = %d, want %d", m.Succeeded, expectedEach)
	}
	if m.Failed != expectedEach {
		t.Errorf("Failed = %d, want %d", m.Failed, expectedEach)
	}
}

func TestChunkEmbeddingWorker_IsRunning(t *testing.T) {
	t.Run("initial state is not running", func(t *testing.T) {
		w := &ChunkEmbeddingWorker{}
		if w.IsRunning() {
			t.Error("IsRunning() = true, want false")
		}
	})

	t.Run("running state can be set", func(t *testing.T) {
		w := &ChunkEmbeddingWorker{running: true}
		if !w.IsRunning() {
			t.Error("IsRunning() = false, want true")
		}
	})
}

func TestChunkEmbeddingWorkerMetrics_Struct(t *testing.T) {
	m := ChunkEmbeddingWorkerMetrics{
		Processed: 100,
		Succeeded: 90,
		Failed:    10,
	}

	if m.Processed != 100 {
		t.Errorf("Processed = %d, want 100", m.Processed)
	}
	if m.Succeeded != 90 {
		t.Errorf("Succeeded = %d, want 90", m.Succeeded)
	}
	if m.Failed != 10 {
		t.Errorf("Failed = %d, want 10", m.Failed)
	}
}

func TestExtractionSchemas(t *testing.T) {
	t.Run("empty schemas", func(t *testing.T) {
		schemas := &ExtractionSchemas{
			ObjectSchemas:       map[string]agents.ObjectSchema{},
			RelationshipSchemas: map[string]agents.RelationshipSchema{},
		}

		if len(schemas.ObjectSchemas) != 0 {
			t.Errorf("ObjectSchemas len = %d, want 0", len(schemas.ObjectSchemas))
		}
		if len(schemas.RelationshipSchemas) != 0 {
			t.Errorf("RelationshipSchemas len = %d, want 0", len(schemas.RelationshipSchemas))
		}
	})

	t.Run("with schemas", func(t *testing.T) {
		schemas := &ExtractionSchemas{
			ObjectSchemas: map[string]agents.ObjectSchema{
				"Person": {
					Name:        "Person",
					Description: "A human being",
				},
				"Organization": {
					Name:        "Organization",
					Description: "A company or institution",
				},
			},
			RelationshipSchemas: map[string]agents.RelationshipSchema{
				"WORKS_FOR": {
					Name:        "WORKS_FOR",
					Description: "Employment relationship",
				},
			},
		}

		if len(schemas.ObjectSchemas) != 2 {
			t.Errorf("ObjectSchemas len = %d, want 2", len(schemas.ObjectSchemas))
		}
		if len(schemas.RelationshipSchemas) != 1 {
			t.Errorf("RelationshipSchemas len = %d, want 1", len(schemas.RelationshipSchemas))
		}

		// Check specific schema
		person, ok := schemas.ObjectSchemas["Person"]
		if !ok {
			t.Error("Missing Person schema")
		} else if person.Description != "A human being" {
			t.Errorf("Person.Description = %q, want %q", person.Description, "A human being")
		}
	})
}

// =============================================================================
// NewExtractionConfig Tests
// =============================================================================

func TestNewExtractionConfig(t *testing.T) {
	t.Run("creates config with all components", func(t *testing.T) {
		appCfg := &config.Config{}
		cfg := NewExtractionConfig(appCfg)

		if cfg == nil {
			t.Fatal("NewExtractionConfig returned nil")
		}
		if cfg.GraphEmbedding == nil {
			t.Error("GraphEmbedding config should not be nil")
		}
		if cfg.ChunkEmbedding == nil {
			t.Error("ChunkEmbedding config should not be nil")
		}
		if cfg.DocumentParsing == nil {
			t.Error("DocumentParsing config should not be nil")
		}
		if cfg.ObjectExtraction == nil {
			t.Error("ObjectExtraction config should not be nil")
		}
	})

	t.Run("uses default configs", func(t *testing.T) {
		appCfg := &config.Config{}
		cfg := NewExtractionConfig(appCfg)

		// Verify it uses defaults from DefaultGraphEmbeddingConfig
		expectedGraphCfg := DefaultGraphEmbeddingConfig()
		if cfg.GraphEmbedding.WorkerIntervalMs != expectedGraphCfg.WorkerIntervalMs {
			t.Errorf("GraphEmbedding.WorkerIntervalMs = %d, want %d", cfg.GraphEmbedding.WorkerIntervalMs, expectedGraphCfg.WorkerIntervalMs)
		}

		// Verify it uses defaults from DefaultChunkEmbeddingConfig
		expectedChunkCfg := DefaultChunkEmbeddingConfig()
		if cfg.ChunkEmbedding.WorkerIntervalMs != expectedChunkCfg.WorkerIntervalMs {
			t.Errorf("ChunkEmbedding.WorkerIntervalMs = %d, want %d", cfg.ChunkEmbedding.WorkerIntervalMs, expectedChunkCfg.WorkerIntervalMs)
		}

		// Verify it uses defaults from DefaultDocumentParsingConfig
		expectedDocCfg := DefaultDocumentParsingConfig()
		if cfg.DocumentParsing.WorkerIntervalMs != expectedDocCfg.WorkerIntervalMs {
			t.Errorf("DocumentParsing.WorkerIntervalMs = %d, want %d", cfg.DocumentParsing.WorkerIntervalMs, expectedDocCfg.WorkerIntervalMs)
		}

		// Verify it uses defaults from DefaultObjectExtractionConfig
		expectedObjCfg := DefaultObjectExtractionConfig()
		if cfg.ObjectExtraction.WorkerIntervalMs != expectedObjCfg.WorkerIntervalMs {
			t.Errorf("ObjectExtraction.WorkerIntervalMs = %d, want %d", cfg.ObjectExtraction.WorkerIntervalMs, expectedObjCfg.WorkerIntervalMs)
		}
	})
}

func TestDocumentParsingJobsService_calculateRetryDelay(t *testing.T) {
	tests := []struct {
		name             string
		baseRetryDelayMs int
		maxRetryDelayMs  int
		retryMultiplier  float64
		retryCount       int
		expected         int
	}{
		{
			name:             "first retry",
			baseRetryDelayMs: 1000,
			maxRetryDelayMs:  60000,
			retryMultiplier:  2.0,
			retryCount:       0,
			expected:         1000, // 1000 * 2^0 = 1000
		},
		{
			name:             "second retry",
			baseRetryDelayMs: 1000,
			maxRetryDelayMs:  60000,
			retryMultiplier:  2.0,
			retryCount:       1,
			expected:         2000, // 1000 * 2^1 = 2000
		},
		{
			name:             "third retry",
			baseRetryDelayMs: 1000,
			maxRetryDelayMs:  60000,
			retryMultiplier:  2.0,
			retryCount:       2,
			expected:         4000, // 1000 * 2^2 = 4000
		},
		{
			name:             "fourth retry",
			baseRetryDelayMs: 1000,
			maxRetryDelayMs:  60000,
			retryMultiplier:  2.0,
			retryCount:       3,
			expected:         8000, // 1000 * 2^3 = 8000
		},
		{
			name:             "capped at max delay",
			baseRetryDelayMs: 1000,
			maxRetryDelayMs:  5000,
			retryMultiplier:  2.0,
			retryCount:       10, // 1000 * 2^10 = 1024000, but capped at 5000
			expected:         5000,
		},
		{
			name:             "with multiplier 3",
			baseRetryDelayMs: 10000,
			maxRetryDelayMs:  300000,
			retryMultiplier:  3.0,
			retryCount:       2,
			expected:         90000, // 10000 * 3^2 = 90000
		},
		{
			name:             "default config values first retry",
			baseRetryDelayMs: 10000,
			maxRetryDelayMs:  300000,
			retryMultiplier:  3.0,
			retryCount:       0,
			expected:         10000, // 10000 * 3^0 = 10000
		},
		{
			name:             "default config values capped",
			baseRetryDelayMs: 10000,
			maxRetryDelayMs:  300000,
			retryMultiplier:  3.0,
			retryCount:       5, // 10000 * 3^5 = 2430000, capped at 300000
			expected:         300000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &DocumentParsingJobsService{
				cfg: &DocumentParsingConfig{
					BaseRetryDelayMs: tt.baseRetryDelayMs,
					MaxRetryDelayMs:  tt.maxRetryDelayMs,
					RetryMultiplier:  tt.retryMultiplier,
				},
			}
			result := svc.calculateRetryDelay(tt.retryCount)
			if result != tt.expected {
				t.Errorf("calculateRetryDelay(%d) = %d, want %d", tt.retryCount, result, tt.expected)
			}
		})
	}
}

func TestDocumentParsingJobsService_calculateRetryDelay_ExponentialGrowth(t *testing.T) {
	svc := &DocumentParsingJobsService{
		cfg: &DocumentParsingConfig{
			BaseRetryDelayMs: 1000,
			MaxRetryDelayMs:  1000000, // High cap so we can see growth
			RetryMultiplier:  2.0,
		},
	}

	// Verify exponential growth (each retry doubles the delay)
	prev := 0
	for i := 0; i < 5; i++ {
		delay := svc.calculateRetryDelay(i)
		if i > 0 && delay != prev*2 {
			t.Errorf("retry %d: expected %d (2x previous), got %d", i, prev*2, delay)
		}
		prev = delay
	}
}


