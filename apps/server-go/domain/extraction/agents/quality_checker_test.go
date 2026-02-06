package agents

import (
	"errors"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockState implements session.State interface for testing
type mockState struct {
	data map[string]any
	err  error
}

func (m *mockState) Get(key string) (any, error) {
	if m.err != nil {
		return nil, m.err
	}
	val, ok := m.data[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	return val, nil
}

func (m *mockState) Set(key string, value any) error {
	if m.data == nil {
		m.data = make(map[string]any)
	}
	m.data[key] = value
	return nil
}

func (m *mockState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range m.data {
			if !yield(k, v) {
				return
			}
		}
	}
}

func TestGetString(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]any
		key      string
		expected string
	}{
		{
			name:     "key exists with string value",
			m:        map[string]any{"name": "John"},
			key:      "name",
			expected: "John",
		},
		{
			name:     "key does not exist",
			m:        map[string]any{"name": "John"},
			key:      "age",
			expected: "",
		},
		{
			name:     "key exists but value is not string (int)",
			m:        map[string]any{"age": 25},
			key:      "age",
			expected: "",
		},
		{
			name:     "key exists but value is not string (nil)",
			m:        map[string]any{"name": nil},
			key:      "name",
			expected: "",
		},
		{
			name:     "key exists but value is not string (bool)",
			m:        map[string]any{"active": true},
			key:      "active",
			expected: "",
		},
		{
			name:     "empty map",
			m:        map[string]any{},
			key:      "name",
			expected: "",
		},
		{
			name:     "empty string value",
			m:        map[string]any{"name": ""},
			key:      "name",
			expected: "",
		},
		{
			name:     "value is float",
			m:        map[string]any{"score": 3.14},
			key:      "score",
			expected: "",
		},
		{
			name:     "value is slice",
			m:        map[string]any{"tags": []string{"a", "b"}},
			key:      "tags",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getString(tt.m, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetString_NilMap(t *testing.T) {
	// This tests nil map safety - getString should handle this gracefully
	// In Go, accessing a nil map returns the zero value, so this should return ""
	var m map[string]any
	result := getString(m, "any_key")
	assert.Equal(t, "", result)
}

func TestConvertToObjectSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected ObjectSchema
	}{
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: ObjectSchema{},
		},
		{
			name: "with name only",
			input: map[string]any{
				"name": "Person",
			},
			expected: ObjectSchema{
				Name: "Person",
			},
		},
		{
			name: "with name and description",
			input: map[string]any{
				"name":        "Organization",
				"description": "A company or institution",
			},
			expected: ObjectSchema{
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
				},
			},
			expected: ObjectSchema{
				Name: "Person",
				Properties: map[string]PropertyDef{
					"age": {
						Type:        "integer",
						Description: "Age in years",
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
			expected: ObjectSchema{
				Name:     "Document",
				Required: []string{"title", "content"},
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
			expected: ObjectSchema{
				Name:       "Test",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			name: "with invalid required values",
			input: map[string]any{
				"name":     "Test",
				"required": []any{123, "valid", true},
			},
			expected: ObjectSchema{
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
			expected: ObjectSchema{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToObjectSchema(tt.input)

			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.Description, result.Description)
			assert.Equal(t, len(tt.expected.Properties), len(result.Properties))
			assert.Equal(t, len(tt.expected.Required), len(result.Required))

			for k, v := range tt.expected.Properties {
				assert.Equal(t, v, result.Properties[k])
			}
		})
	}
}

// =============================================================================
// getEntitiesFromState Tests
// =============================================================================

func TestGetEntitiesFromState(t *testing.T) {
	tests := []struct {
		name        string
		stateData   map[string]any
		stateErr    error
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name: "returns InternalEntity slice directly",
			stateData: map[string]any{
				"extracted_entities": []InternalEntity{
					{TempID: "t1", Name: "Entity1", Type: "Person"},
					{TempID: "t2", Name: "Entity2", Type: "Company"},
				},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "converts []any with InternalEntity items",
			stateData: map[string]any{
				"extracted_entities": []any{
					InternalEntity{TempID: "t1", Name: "Entity1", Type: "Person"},
				},
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "converts []any with map items",
			stateData: map[string]any{
				"extracted_entities": []any{
					map[string]any{
						"temp_id":     "t1",
						"name":        "John Doe",
						"type":        "Person",
						"description": "A person entity",
					},
					map[string]any{
						"temp_id": "t2",
						"name":    "Acme Corp",
						"type":    "Organization",
					},
				},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "skips map items without temp_id",
			stateData: map[string]any{
				"extracted_entities": []any{
					map[string]any{
						"temp_id": "t1",
						"name":    "Valid",
						"type":    "Person",
					},
					map[string]any{
						"name": "Invalid - no temp_id",
						"type": "Person",
					},
				},
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "handles empty []any",
			stateData: map[string]any{
				"extracted_entities": []any{},
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:        "returns error when state.Get fails",
			stateData:   map[string]any{},
			stateErr:    errors.New("storage error"),
			wantErr:     true,
			errContains: "storage error",
		},
		{
			name: "returns error for unexpected type",
			stateData: map[string]any{
				"extracted_entities": "not a slice",
			},
			wantErr:     true,
			errContains: "unexpected type",
		},
		{
			name: "handles mixed valid and invalid items in []any",
			stateData: map[string]any{
				"extracted_entities": []any{
					map[string]any{"temp_id": "t1", "name": "Valid", "type": "Person"},
					"invalid string item",
					123,
					map[string]any{"temp_id": "t2", "name": "Also Valid", "type": "Company"},
				},
			},
			wantCount: 2,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &mockState{data: tt.stateData, err: tt.stateErr}
			entities, err := getEntitiesFromState(state)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.Len(t, entities, tt.wantCount)
		})
	}
}

func TestGetEntitiesFromState_EntityValues(t *testing.T) {
	// Test that entity values are correctly extracted from maps
	stateData := map[string]any{
		"extracted_entities": []any{
			map[string]any{
				"temp_id":     "person-1",
				"name":        "John Doe",
				"type":        "Person",
				"description": "A software engineer",
			},
		},
	}

	state := &mockState{data: stateData}
	entities, err := getEntitiesFromState(state)

	assert.NoError(t, err)
	assert.Len(t, entities, 1)
	assert.Equal(t, "person-1", entities[0].TempID)
	assert.Equal(t, "John Doe", entities[0].Name)
	assert.Equal(t, "Person", entities[0].Type)
	assert.Equal(t, "A software engineer", entities[0].Description)
}

// =============================================================================
// getRelationshipsFromState Tests
// =============================================================================

func TestGetRelationshipsFromState(t *testing.T) {
	tests := []struct {
		name        string
		stateData   map[string]any
		stateErr    error
		wantCount   int
		wantErr     bool
		errContains string
	}{
		{
			name: "returns ExtractedRelationship slice directly",
			stateData: map[string]any{
				"extracted_relationships": []ExtractedRelationship{
					{SourceRef: "t1", TargetRef: "t2", Type: "KNOWS"},
				},
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "extracts from RelationshipExtractionOutput pointer",
			stateData: map[string]any{
				"extracted_relationships": &RelationshipExtractionOutput{
					Relationships: []ExtractedRelationship{
						{SourceRef: "t1", TargetRef: "t2", Type: "WORKS_FOR"},
						{SourceRef: "t2", TargetRef: "t3", Type: "MANAGES"},
					},
				},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "converts []any with ExtractedRelationship items",
			stateData: map[string]any{
				"extracted_relationships": []any{
					ExtractedRelationship{SourceRef: "t1", TargetRef: "t2", Type: "KNOWS"},
				},
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "converts []any with map items",
			stateData: map[string]any{
				"extracted_relationships": []any{
					map[string]any{
						"source_ref":  "t1",
						"target_ref":  "t2",
						"type":        "WORKS_FOR",
						"description": "Employment relationship",
					},
				},
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "skips map items without source_ref or target_ref",
			stateData: map[string]any{
				"extracted_relationships": []any{
					map[string]any{
						"source_ref": "t1",
						"target_ref": "t2",
						"type":       "VALID",
					},
					map[string]any{
						"source_ref": "t1",
						// missing target_ref
						"type": "INVALID",
					},
					map[string]any{
						// missing source_ref
						"target_ref": "t2",
						"type":       "ALSO_INVALID",
					},
				},
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "handles empty []any",
			stateData: map[string]any{
				"extracted_relationships": []any{},
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:        "returns error when state.Get fails",
			stateData:   map[string]any{},
			stateErr:    errors.New("storage error"),
			wantErr:     true,
			errContains: "storage error",
		},
		{
			name: "returns error for unexpected type",
			stateData: map[string]any{
				"extracted_relationships": 12345,
			},
			wantErr:     true,
			errContains: "unexpected type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &mockState{data: tt.stateData, err: tt.stateErr}
			relationships, err := getRelationshipsFromState(state)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.Len(t, relationships, tt.wantCount)
		})
	}
}

func TestGetRelationshipsFromState_RelationshipValues(t *testing.T) {
	// Test that relationship values are correctly extracted from maps
	stateData := map[string]any{
		"extracted_relationships": []any{
			map[string]any{
				"source_ref":  "person-1",
				"target_ref":  "company-1",
				"type":        "WORKS_FOR",
				"description": "Employment since 2020",
			},
		},
	}

	state := &mockState{data: stateData}
	relationships, err := getRelationshipsFromState(state)

	assert.NoError(t, err)
	assert.Len(t, relationships, 1)
	assert.Equal(t, "person-1", relationships[0].SourceRef)
	assert.Equal(t, "company-1", relationships[0].TargetRef)
	assert.Equal(t, "WORKS_FOR", relationships[0].Type)
	assert.Equal(t, "Employment since 2020", relationships[0].Description)
}

// =============================================================================
// convertToRelationshipSchema Tests
// =============================================================================

func TestConvertToRelationshipSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected RelationshipSchema
	}{
		{
			name:     "empty map",
			input:    map[string]any{},
			expected: RelationshipSchema{},
		},
		{
			name: "with name only",
			input: map[string]any{
				"name": "WORKS_FOR",
			},
			expected: RelationshipSchema{
				Name: "WORKS_FOR",
			},
		},
		{
			name: "with name and description",
			input: map[string]any{
				"name":        "MANAGES",
				"description": "Management relationship",
			},
			expected: RelationshipSchema{
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
			expected: RelationshipSchema{
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
			expected: RelationshipSchema{
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
			expected: RelationshipSchema{
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
			expected: RelationshipSchema{
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
			expected: RelationshipSchema{
				Name:        "TEST",
				SourceTypes: []string{"Valid"},
			},
		},
		{
			name: "with wrong types for name/description",
			input: map[string]any{
				"name":        []string{"wrong"},
				"description": 123,
			},
			expected: RelationshipSchema{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToRelationshipSchema(tt.input)

			assert.Equal(t, tt.expected.Name, result.Name)
			assert.Equal(t, tt.expected.Description, result.Description)
			assert.Equal(t, tt.expected.ExtractionGuidelines, result.ExtractionGuidelines)
			assert.Equal(t, tt.expected.SourceTypes, result.SourceTypes)
			assert.Equal(t, tt.expected.TargetTypes, result.TargetTypes)
		})
	}
}
