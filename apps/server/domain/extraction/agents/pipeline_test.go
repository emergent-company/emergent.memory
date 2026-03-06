package agents

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateOrphanRate(t *testing.T) {
	tests := []struct {
		name          string
		entities      []InternalEntity
		relationships []ExtractedRelationship
		expectedRate  float64
	}{
		{
			name:          "empty entities returns 0",
			entities:      []InternalEntity{},
			relationships: []ExtractedRelationship{},
			expectedRate:  0,
		},
		{
			name: "all entities connected returns 0",
			entities: []InternalEntity{
				{TempID: "person_john", Name: "John", Type: "Person"},
				{TempID: "org_company", Name: "Company", Type: "Organization"},
			},
			relationships: []ExtractedRelationship{
				{SourceRef: "person_john", TargetRef: "org_company", Type: "WORKS_AT"},
			},
			expectedRate: 0,
		},
		{
			name: "one orphan out of two returns 0.5",
			entities: []InternalEntity{
				{TempID: "person_john", Name: "John", Type: "Person"},
				{TempID: "person_jane", Name: "Jane", Type: "Person"},
			},
			relationships: []ExtractedRelationship{
				{SourceRef: "person_john", TargetRef: "person_john", Type: "SELF_REF"},
			},
			expectedRate: 0.5,
		},
		{
			name: "all orphans returns 1.0",
			entities: []InternalEntity{
				{TempID: "person_john", Name: "John", Type: "Person"},
				{TempID: "person_jane", Name: "Jane", Type: "Person"},
			},
			relationships: []ExtractedRelationship{},
			expectedRate:  1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := CalculateOrphanRate(tt.entities, tt.relationships)
			assert.Equal(t, tt.expectedRate, rate)
		})
	}
}

func TestGetOrphanTempIDs(t *testing.T) {
	entities := []InternalEntity{
		{TempID: "person_john", Name: "John", Type: "Person"},
		{TempID: "person_jane", Name: "Jane", Type: "Person"},
		{TempID: "org_acme", Name: "Acme", Type: "Organization"},
	}
	relationships := []ExtractedRelationship{
		{SourceRef: "person_john", TargetRef: "org_acme", Type: "WORKS_AT"},
	}

	orphans := GetOrphanTempIDs(entities, relationships)

	assert.Len(t, orphans, 1)
	assert.Contains(t, orphans, "person_jane")
}

func TestGenerateTempID(t *testing.T) {
	existing := make(map[string]bool)

	// First ID should be straightforward
	id1 := generateTempID("John Smith", "Person", existing)
	assert.Equal(t, "person_john_smith", id1)
	existing[id1] = true

	// Duplicate should get a suffix
	id2 := generateTempID("John Smith", "Person", existing)
	assert.Equal(t, "person_john_smith_1", id2)
	existing[id2] = true

	// Another duplicate
	id3 := generateTempID("John Smith", "Person", existing)
	assert.Equal(t, "person_john_smith_2", id3)
}

func TestGenerateTempID_LongNames(t *testing.T) {
	existing := make(map[string]bool)

	// Very long name should be truncated
	longName := "This Is A Very Long Name That Should Be Truncated To Fit Within Limits"
	id := generateTempID(longName, "SomeVeryLongEntityTypeName", existing)

	// Should be truncated
	assert.LessOrEqual(t, len(id), 45) // 20 + 1 + 20 + some suffix room
}

func TestBuildEntityExtractionPrompt(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {
			Name:        "Person",
			Description: "A human being",
			Properties: map[string]PropertyDef{
				"occupation": {Type: "string", Description: "The person's job"},
			},
		},
	}

	prompt := BuildEntityExtractionPrompt(
		"John works at Acme Corp.",
		schemas,
		[]string{"Person"},
		nil,
	)

	assert.Contains(t, prompt, "John works at Acme Corp.")
	assert.Contains(t, prompt, "Person")
	assert.Contains(t, prompt, "A human being")
	assert.Contains(t, prompt, "occupation")
}

func TestBuildRelationshipPrompt(t *testing.T) {
	entities := []InternalEntity{
		{TempID: "person_john", Name: "John", Type: "Person"},
		{TempID: "org_acme", Name: "Acme Corp", Type: "Organization"},
	}

	schemas := map[string]RelationshipSchema{
		"WORKS_AT": {
			Name:        "WORKS_AT",
			Description: "Person works at organization",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Organization"},
		},
	}

	prompt := BuildRelationshipPrompt(
		entities,
		schemas,
		"John works at Acme Corp.",
		nil,
		nil,
	)

	assert.Contains(t, prompt, "person_john")
	assert.Contains(t, prompt, "org_acme")
	assert.Contains(t, prompt, "WORKS_AT")
	assert.Contains(t, prompt, "Person works at organization")
}

func TestBuildRelationshipPrompt_WithOrphans(t *testing.T) {
	entities := []InternalEntity{
		{TempID: "person_john", Name: "John", Type: "Person"},
		{TempID: "person_jane", Name: "Jane", Type: "Person"},
	}

	orphans := []string{"person_jane"}

	prompt := BuildRelationshipPrompt(
		entities,
		nil,
		"John and Jane work together.",
		nil,
		orphans,
	)

	assert.Contains(t, prompt, "PRIORITY")
	assert.Contains(t, prompt, "person_jane")
	assert.Contains(t, prompt, "orphan")
}

func TestParseEntityExtractionOutput(t *testing.T) {
	jsonOutput := `{
		"entities": [
			{
				"name": "John Smith",
				"type": "Person",
				"description": "A software engineer"
			}
		]
	}`

	output, err := ParseEntityExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Len(t, output.Entities, 1)
	assert.Equal(t, "John Smith", output.Entities[0].Name)
	assert.Equal(t, "Person", output.Entities[0].Type)
}

func TestParseEntityExtractionOutput_WithCodeBlock(t *testing.T) {
	jsonOutput := "```json\n{\"entities\": [{\"name\": \"Test\", \"type\": \"Person\"}]}\n```"

	output, err := ParseEntityExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Len(t, output.Entities, 1)
	assert.Equal(t, "Test", output.Entities[0].Name)
}

func TestParseRelationshipExtractionOutput(t *testing.T) {
	jsonOutput := `{
		"relationships": [
			{
				"source_ref": "person_john",
				"target_ref": "org_acme",
				"type": "WORKS_AT",
				"description": "John is employed by Acme"
			}
		]
	}`

	output, err := ParseRelationshipExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Len(t, output.Relationships, 1)
	assert.Equal(t, "person_john", output.Relationships[0].SourceRef)
	assert.Equal(t, "org_acme", output.Relationships[0].TargetRef)
	assert.Equal(t, "WORKS_AT", output.Relationships[0].Type)
}

func TestBuildEntityExtractionPrompt_NoAllowedTypes(t *testing.T) {
	// When allowedTypes is empty, should extract types from schemas
	schemas := map[string]ObjectSchema{
		"Person": {
			Name:        "Person",
			Description: "A human being",
		},
		"Organization": {
			Name:        "Organization",
			Description: "A company or group",
		},
	}

	prompt := BuildEntityExtractionPrompt(
		"Document text here.",
		schemas,
		nil, // empty allowedTypes
		nil,
	)

	// Should contain both types from schemas
	assert.Contains(t, prompt, "Document text here.")
	// At least one type should be mentioned
	assert.True(t, strings.Contains(prompt, "Person") || strings.Contains(prompt, "Organization"))
}

func TestBuildEntityExtractionPrompt_WithExistingEntities(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {
			Name:        "Person",
			Description: "A human being",
		},
	}

	existingEntities := []ExistingEntityContext{
		{
			ID:          "uuid-123",
			Name:        "John Smith",
			TypeName:    "Person",
			Description: "A software engineer who works at Acme Corp.",
			Similarity:  0.95,
		},
	}

	prompt := BuildEntityExtractionPrompt(
		"John Smith joined the meeting.",
		schemas,
		[]string{"Person"},
		existingEntities,
	)

	assert.Contains(t, prompt, "John Smith")
	assert.Contains(t, prompt, "uuid-123")
	assert.Contains(t, prompt, "95%") // similarity formatted as percentage
	assert.Contains(t, prompt, "CONTEXT-AWARE")
	assert.Contains(t, prompt, "existing_entity_id")
}

func TestBuildEntityExtractionPrompt_WithExistingEntities_TruncatesLongDescription(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {Name: "Person"},
	}

	// Description longer than 100 characters
	longDesc := strings.Repeat("A", 150)
	existingEntities := []ExistingEntityContext{
		{
			ID:          "uuid-123",
			Name:        "Test Entity",
			TypeName:    "Person",
			Description: longDesc,
		},
	}

	prompt := BuildEntityExtractionPrompt(
		"Document text.",
		schemas,
		[]string{"Person"},
		existingEntities,
	)

	// Description should be truncated to 100 chars
	assert.Contains(t, prompt, "Test Entity")
	assert.NotContains(t, prompt, longDesc) // full description not present
}

func TestBuildEntityExtractionPrompt_WithManyExistingEntities(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {Name: "Person"},
	}

	// Create more than maxPerType (10) entities
	existingEntities := make([]ExistingEntityContext, 15)
	for i := 0; i < 15; i++ {
		existingEntities[i] = ExistingEntityContext{
			ID:       fmt.Sprintf("uuid-%d", i),
			Name:     fmt.Sprintf("Person %d", i),
			TypeName: "Person",
		}
	}

	prompt := BuildEntityExtractionPrompt(
		"Document text.",
		schemas,
		[]string{"Person"},
		existingEntities,
	)

	// Should show "and X more" message
	assert.Contains(t, prompt, "and 5 more")
}

func TestBuildEntityExtractionPrompt_SchemaWithRequiredProperty(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {
			Name:        "Person",
			Description: "A human being",
			Properties: map[string]PropertyDef{
				"occupation": {Type: "string", Description: "Job title"},
				"age":        {Type: "number", Description: "Age in years"},
			},
			Required: []string{"occupation"},
		},
	}

	prompt := BuildEntityExtractionPrompt(
		"John is a developer.",
		schemas,
		[]string{"Person"},
		nil,
	)

	assert.Contains(t, prompt, "occupation")
	assert.Contains(t, prompt, "(required)")
	assert.Contains(t, prompt, "age")
}

func TestBuildEntityExtractionPrompt_SchemaWithEmptyPropertyType(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Event": {
			Name: "Event",
			Properties: map[string]PropertyDef{
				"location": {Description: "Where the event occurred"}, // empty Type
			},
		},
	}

	prompt := BuildEntityExtractionPrompt(
		"A meeting happened.",
		schemas,
		[]string{"Event"},
		nil,
	)

	// Should default to "string" type
	assert.Contains(t, prompt, "(string)")
	assert.Contains(t, prompt, "location")
}

func TestBuildEntityExtractionPrompt_MaxTotalEntities(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person":       {Name: "Person"},
		"Organization": {Name: "Organization"},
	}

	// Create more than maxTotal (50) entities across types
	existingEntities := make([]ExistingEntityContext, 0, 60)
	for i := 0; i < 30; i++ {
		existingEntities = append(existingEntities, ExistingEntityContext{
			ID:       fmt.Sprintf("person-uuid-%d", i),
			Name:     fmt.Sprintf("Person %d", i),
			TypeName: "Person",
		})
	}
	for i := 0; i < 30; i++ {
		existingEntities = append(existingEntities, ExistingEntityContext{
			ID:       fmt.Sprintf("org-uuid-%d", i),
			Name:     fmt.Sprintf("Organization %d", i),
			TypeName: "Organization",
		})
	}

	prompt := BuildEntityExtractionPrompt(
		"Document text.",
		schemas,
		[]string{"Person", "Organization"},
		existingEntities,
	)

	// Should not show all 60 entities due to maxTotal limit
	// Count how many entity references appear (approximate check)
	personCount := strings.Count(prompt, "person-uuid-")
	orgCount := strings.Count(prompt, "org-uuid-")
	totalShown := personCount + orgCount

	// maxPerType=10, maxTotal=50, so we should see ~20 entities max
	// (10 per type, but could hit maxTotal earlier)
	assert.LessOrEqual(t, totalShown, 50, "Should not exceed maxTotal entities")
}

func TestBuildEntityExtractionPrompt_MaxTotalBreaksLoop(t *testing.T) {
	// Create 6 types with 10 entities each = 60 total
	// With maxTotal=50, we should hit the outer break after 5 full types (50 entities)
	schemas := map[string]ObjectSchema{}
	existingEntities := make([]ExistingEntityContext, 0, 60)

	typeNames := []string{"Type1", "Type2", "Type3", "Type4", "Type5", "Type6"}
	for _, typeName := range typeNames {
		schemas[typeName] = ObjectSchema{Name: typeName}
		for i := 0; i < 10; i++ {
			existingEntities = append(existingEntities, ExistingEntityContext{
				ID:       fmt.Sprintf("%s-uuid-%d", typeName, i),
				Name:     fmt.Sprintf("%s Entity %d", typeName, i),
				TypeName: typeName,
			})
		}
	}

	prompt := BuildEntityExtractionPrompt(
		"Document text.",
		schemas,
		typeNames,
		existingEntities,
	)

	// Count entity references
	totalShown := 0
	for _, typeName := range typeNames {
		totalShown += strings.Count(prompt, fmt.Sprintf("%s-uuid-", typeName))
	}

	// Should be exactly maxTotal (50) or less
	assert.LessOrEqual(t, totalShown, 50, "Should not exceed maxTotal entities")
	// Should show at least 40 (4 full types at 10 each)
	assert.GreaterOrEqual(t, totalShown, 40, "Should show substantial entities before cutoff")
}

func TestBuildEntityExtractionPrompt_MaxTotalBreaksInMiddleOfType(t *testing.T) {
	// Create scenario where maxTotal (50) is hit in the MIDDLE of a type's entities
	// Need: type1 with 10 entities (shown), type2 with 10, type3 with 10, type4 with 10, type5 with 10, type6 with 15
	// After 5 types: 50 entities shown exactly
	// But we need to hit maxTotal=50 while still in a type's loop

	// Strategy: Create 6 types, but make one type have many entities
	// and ensure the break happens mid-type
	schemas := map[string]ObjectSchema{}
	existingEntities := make([]ExistingEntityContext, 0, 100)

	// Add 5 types with exactly 9 entities each = 45 total
	for i := 1; i <= 5; i++ {
		typeName := fmt.Sprintf("Type%d", i)
		schemas[typeName] = ObjectSchema{Name: typeName}
		for j := 0; j < 9; j++ {
			existingEntities = append(existingEntities, ExistingEntityContext{
				ID:       fmt.Sprintf("%s-uuid-%d", typeName, j),
				Name:     fmt.Sprintf("%s Entity %d", typeName, j),
				TypeName: typeName,
			})
		}
	}

	// Add type6 with 10 entities - this will cause the inner break at entity 5 (45+5=50)
	typeName := "Type6"
	schemas[typeName] = ObjectSchema{Name: typeName}
	for j := 0; j < 10; j++ {
		existingEntities = append(existingEntities, ExistingEntityContext{
			ID:       fmt.Sprintf("%s-uuid-%d", typeName, j),
			Name:     fmt.Sprintf("%s Entity %d", typeName, j),
			TypeName: typeName,
		})
	}

	prompt := BuildEntityExtractionPrompt(
		"Document text.",
		schemas,
		[]string{"Type1", "Type2", "Type3", "Type4", "Type5", "Type6"},
		existingEntities,
	)

	// Count Type6 entity references - should be 5 (50 - 45 = 5)
	type6Count := strings.Count(prompt, "Type6-uuid-")
	assert.LessOrEqual(t, type6Count, 5, "Type6 should be truncated at maxTotal")

	// Count total
	totalShown := 0
	for i := 1; i <= 6; i++ {
		totalShown += strings.Count(prompt, fmt.Sprintf("Type%d-uuid-", i))
	}
	assert.LessOrEqual(t, totalShown, 50, "Should not exceed maxTotal")
}

func TestBuildEntityExtractionPrompt_TypeNotInByType(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person":       {Name: "Person"},
		"Organization": {Name: "Organization"},
	}

	// Only provide entities for Person, not Organization
	existingEntities := []ExistingEntityContext{
		{ID: "person-1", Name: "Person 1", TypeName: "Person"},
	}

	prompt := BuildEntityExtractionPrompt(
		"Document text.",
		schemas,
		[]string{"Person", "Organization"}, // Both types requested
		existingEntities,                   // But only Person entities exist
	)

	// Should still work and contain Person info
	assert.Contains(t, prompt, "Person 1")
	// Organization header should still be in the allowed types section
	assert.Contains(t, prompt, "Organization")
}

func TestBuildEntityExtractionPrompt_SchemaWithUnderscoreProperty(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {
			Name: "Person",
			Properties: map[string]PropertyDef{
				"name":       {Type: "string", Description: "Name"}, // top-level, should be excluded
				"_internal":  {Type: "string", Description: "Internal field"},
				"occupation": {Type: "string", Description: "Job"},
			},
		},
	}

	prompt := BuildEntityExtractionPrompt(
		"Test document.",
		schemas,
		[]string{"Person"},
		nil,
	)

	// _internal should be excluded (underscore prefix)
	// name should be excluded (top-level field)
	assert.Contains(t, prompt, "occupation")
	assert.NotContains(t, prompt, "_internal")
}

func TestBuildEntityExtractionPrompt_UnknownType(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {Name: "Person", Description: "A human"},
	}

	prompt := BuildEntityExtractionPrompt(
		"Document text.",
		schemas,
		[]string{"Person", "UnknownType"}, // UnknownType not in schemas
		nil,
	)

	// Should still include UnknownType in allowed types
	assert.Contains(t, prompt, "UnknownType")
	assert.Contains(t, prompt, "### UnknownType")
}

func TestBuildRelationshipPrompt_WithLongDescription(t *testing.T) {
	// Description longer than 80 chars should be truncated
	longDesc := strings.Repeat("A", 100)
	entities := []InternalEntity{
		{TempID: "person_john", Name: "John", Type: "Person", Description: longDesc},
	}

	prompt := BuildRelationshipPrompt(
		entities,
		nil,
		"John works.",
		nil,
		nil,
	)

	// Description should be truncated with "..."
	assert.Contains(t, prompt, "...")
	assert.NotContains(t, prompt, longDesc) // full description not present
}

func TestBuildRelationshipPrompt_WithExtractionGuidelines(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"WORKS_AT": {
			Name:                 "WORKS_AT",
			Description:          "Employment relationship",
			SourceTypes:          []string{"Person"},
			TargetTypes:          []string{"Organization"},
			ExtractionGuidelines: "Look for employment indicators like 'works at', 'employed by'",
		},
	}

	prompt := BuildRelationshipPrompt(
		[]InternalEntity{{TempID: "test", Name: "Test", Type: "Person"}},
		schemas,
		"Document.",
		nil,
		nil,
	)

	assert.Contains(t, prompt, "Guidelines")
	assert.Contains(t, prompt, "employment indicators")
}

func TestBuildRelationshipPrompt_WithEmptySourceTargetTypes(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"RELATED_TO": {
			Name:        "RELATED_TO",
			Description: "Generic relationship",
			// No source/target type constraints
		},
	}

	prompt := BuildRelationshipPrompt(
		[]InternalEntity{{TempID: "test", Name: "Test", Type: "Person"}},
		schemas,
		"Document.",
		nil,
		nil,
	)

	// Should not show "Valid entity types" section when no constraints
	assert.Contains(t, prompt, "RELATED_TO")
	assert.Contains(t, prompt, "Generic relationship")
}

func TestBuildRelationshipPrompt_WithSourceTypesOnly(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"ACTS_IN": {
			Name:        "ACTS_IN",
			Description: "Actor acts in movie",
			SourceTypes: []string{"Person"},
			// No target type constraints
		},
	}

	prompt := BuildRelationshipPrompt(
		[]InternalEntity{{TempID: "test", Name: "Test", Type: "Person"}},
		schemas,
		"Document.",
		nil,
		nil,
	)

	assert.Contains(t, prompt, "Person → any")
}

func TestGetOrphanTempIDs_Empty(t *testing.T) {
	orphans := GetOrphanTempIDs([]InternalEntity{}, []ExtractedRelationship{})
	assert.Empty(t, orphans)
}

func TestGetOrphanTempIDs_AllConnected(t *testing.T) {
	entities := []InternalEntity{
		{TempID: "a", Name: "A", Type: "Person"},
		{TempID: "b", Name: "B", Type: "Person"},
	}
	relationships := []ExtractedRelationship{
		{SourceRef: "a", TargetRef: "b", Type: "KNOWS"},
	}

	orphans := GetOrphanTempIDs(entities, relationships)
	assert.Empty(t, orphans)
}

func TestParseEntityExtractionOutput_InvalidJSON(t *testing.T) {
	_, err := ParseEntityExtractionOutput("not valid json")
	assert.Error(t, err)
}

func TestParseEntityExtractionOutput_EmptyEntities(t *testing.T) {
	jsonOutput := `{"entities": []}`
	output, err := ParseEntityExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Empty(t, output.Entities)
}

func TestParseRelationshipExtractionOutput_InvalidJSON(t *testing.T) {
	_, err := ParseRelationshipExtractionOutput("not valid json")
	assert.Error(t, err)
}

func TestParseRelationshipExtractionOutput_EmptyRelationships(t *testing.T) {
	jsonOutput := `{"relationships": []}`
	output, err := ParseRelationshipExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Empty(t, output.Relationships)
}

func TestParseRelationshipExtractionOutput_WithCodeBlock(t *testing.T) {
	jsonOutput := "```json\n{\"relationships\": [{\"source_ref\": \"a\", \"target_ref\": \"b\", \"type\": \"KNOWS\"}]}\n```"

	output, err := ParseRelationshipExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Len(t, output.Relationships, 1)
	assert.Equal(t, "a", output.Relationships[0].SourceRef)
}

func TestGenerateTempID_SpecialCharacters(t *testing.T) {
	existing := make(map[string]bool)

	// Name with special characters
	id := generateTempID("John (CEO) @ Acme", "Person", existing)
	// Should be normalized
	assert.Contains(t, id, "person")
	assert.NotContains(t, id, "(")
	assert.NotContains(t, id, "@")
}

func TestGenerateTempID_EmptyName(t *testing.T) {
	existing := make(map[string]bool)

	id := generateTempID("", "Person", existing)
	assert.Contains(t, id, "person")
}

func TestParseEntityExtractionOutput_NilInput(t *testing.T) {
	_, err := ParseEntityExtractionOutput(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestParseEntityExtractionOutput_AlreadyCorrectType(t *testing.T) {
	expected := &EntityExtractionOutput{
		Entities: []ExtractedEntity{
			{Name: "Test", Type: "Person"},
		},
	}

	result, err := ParseEntityExtractionOutput(expected)
	assert.NoError(t, err)
	assert.Same(t, expected, result) // Should be the same pointer
}

func TestParseEntityExtractionOutput_MapInput(t *testing.T) {
	// Test with map input (type conversion via marshal/unmarshal)
	input := map[string]any{
		"entities": []any{
			map[string]any{
				"name": "John",
				"type": "Person",
			},
		},
	}

	result, err := ParseEntityExtractionOutput(input)
	assert.NoError(t, err)
	assert.Len(t, result.Entities, 1)
	assert.Equal(t, "John", result.Entities[0].Name)
}

func TestParseRelationshipExtractionOutput_NilInput(t *testing.T) {
	_, err := ParseRelationshipExtractionOutput(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestParseRelationshipExtractionOutput_AlreadyCorrectType(t *testing.T) {
	expected := &RelationshipExtractionOutput{
		Relationships: []ExtractedRelationship{
			{SourceRef: "a", TargetRef: "b", Type: "KNOWS"},
		},
	}

	result, err := ParseRelationshipExtractionOutput(expected)
	assert.NoError(t, err)
	assert.Same(t, expected, result)
}

func TestParseRelationshipExtractionOutput_MapInput(t *testing.T) {
	input := map[string]any{
		"relationships": []any{
			map[string]any{
				"source_ref": "person_john",
				"target_ref": "org_acme",
				"type":       "WORKS_AT",
			},
		},
	}

	result, err := ParseRelationshipExtractionOutput(input)
	assert.NoError(t, err)
	assert.Len(t, result.Relationships, 1)
	assert.Equal(t, "person_john", result.Relationships[0].SourceRef)
}

func TestParseEntityExtractionOutput_MarshalError(t *testing.T) {
	// Test with unmarshallable input (channel)
	input := map[string]any{
		"entities": make(chan int), // channels cannot be JSON marshalled
	}

	_, err := ParseEntityExtractionOutput(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal")
}

func TestParseRelationshipExtractionOutput_MarshalError(t *testing.T) {
	// Test with unmarshallable input (channel)
	input := map[string]any{
		"relationships": make(chan int), // channels cannot be JSON marshalled
	}

	_, err := ParseRelationshipExtractionOutput(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal")
}

func TestParseEntityExtractionOutput_WithEntityAction(t *testing.T) {
	jsonOutput := `{
		"entities": [
			{
				"name": "Test Entity",
				"type": "Person",
				"action": "enrich",
				"existing_entity_id": "uuid-123"
			}
		]
	}`

	output, err := ParseEntityExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Len(t, output.Entities, 1)
	assert.Equal(t, EntityActionEnrich, output.Entities[0].Action)
	assert.Equal(t, "uuid-123", output.Entities[0].ExistingEntityID)
}

func TestParseEntityExtractionOutput_WithProperties(t *testing.T) {
	jsonOutput := `{
		"entities": [
			{
				"name": "John Smith",
				"type": "Person",
				"description": "A software developer",
				"properties": {
					"occupation": "Developer",
					"age": 30
				}
			}
		]
	}`

	output, err := ParseEntityExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Len(t, output.Entities, 1)
	assert.Equal(t, "Developer", output.Entities[0].Properties["occupation"])
	assert.Equal(t, float64(30), output.Entities[0].Properties["age"])
}

func TestParseRelationshipExtractionOutput_WithDescription(t *testing.T) {
	jsonOutput := `{
		"relationships": [
			{
				"source_ref": "a",
				"target_ref": "b",
				"type": "KNOWS",
				"description": "They are friends"
			}
		]
	}`

	output, err := ParseRelationshipExtractionOutput(jsonOutput)
	assert.NoError(t, err)
	assert.Len(t, output.Relationships, 1)
	assert.Equal(t, "They are friends", output.Relationships[0].Description)
}

func TestEntityExtractionSchema(t *testing.T) {
	schema := EntityExtractionSchema()

	// Verify top-level schema
	assert.NotNil(t, schema)
	assert.Equal(t, "Output containing extracted entities from the document", schema.Description)
	assert.Contains(t, schema.Required, "entities")

	// Verify entities array property
	entitiesSchema := schema.Properties["entities"]
	assert.NotNil(t, entitiesSchema)
	assert.Equal(t, "Array of extracted entities", entitiesSchema.Description)

	// Verify entity item schema
	itemSchema := entitiesSchema.Items
	assert.NotNil(t, itemSchema)
	assert.Contains(t, itemSchema.Required, "name")
	assert.Contains(t, itemSchema.Required, "type")

	// Verify entity properties exist
	assert.Contains(t, itemSchema.Properties, "name")
	assert.Contains(t, itemSchema.Properties, "type")
	assert.Contains(t, itemSchema.Properties, "description")
	assert.Contains(t, itemSchema.Properties, "properties")
	assert.Contains(t, itemSchema.Properties, "action")
	assert.Contains(t, itemSchema.Properties, "existing_entity_id")

	// Verify action enum values
	actionSchema := itemSchema.Properties["action"]
	assert.Equal(t, []string{"create", "enrich", "reference"}, actionSchema.Enum)
}

func TestRelationshipExtractionSchema(t *testing.T) {
	schema := RelationshipExtractionSchema()

	// Verify top-level schema
	assert.NotNil(t, schema)
	assert.Equal(t, "Output containing extracted relationships between entities", schema.Description)
	assert.Contains(t, schema.Required, "relationships")

	// Verify relationships array property
	relSchema := schema.Properties["relationships"]
	assert.NotNil(t, relSchema)
	assert.Equal(t, "Array of extracted relationships", relSchema.Description)

	// Verify relationship item schema
	itemSchema := relSchema.Items
	assert.NotNil(t, itemSchema)
	assert.Contains(t, itemSchema.Required, "source_ref")
	assert.Contains(t, itemSchema.Required, "target_ref")
	assert.Contains(t, itemSchema.Required, "type")

	// Verify relationship properties exist
	assert.Contains(t, itemSchema.Properties, "source_ref")
	assert.Contains(t, itemSchema.Properties, "target_ref")
	assert.Contains(t, itemSchema.Properties, "type")
	assert.Contains(t, itemSchema.Properties, "description")
}

func TestEntityActionConstants(t *testing.T) {
	// Verify the constant values are as expected
	assert.Equal(t, EntityAction("create"), EntityActionCreate)
	assert.Equal(t, EntityAction("enrich"), EntityActionEnrich)
	assert.Equal(t, EntityAction("reference"), EntityActionReference)
}

func TestExtractedEntityStruct(t *testing.T) {
	entity := ExtractedEntity{
		Name:             "John Doe",
		Type:             "Person",
		Description:      "A software engineer",
		Properties:       map[string]any{"occupation": "developer"},
		Action:           EntityActionCreate,
		ExistingEntityID: "",
	}

	assert.Equal(t, "John Doe", entity.Name)
	assert.Equal(t, "Person", entity.Type)
	assert.Equal(t, "A software engineer", entity.Description)
	assert.Equal(t, "developer", entity.Properties["occupation"])
	assert.Equal(t, EntityActionCreate, entity.Action)
	assert.Empty(t, entity.ExistingEntityID)
}

func TestExtractedEntityStruct_WithExistingID(t *testing.T) {
	entity := ExtractedEntity{
		Name:             "Acme Corp",
		Type:             "Organization",
		Action:           EntityActionEnrich,
		ExistingEntityID: "uuid-123-456",
	}

	assert.Equal(t, EntityActionEnrich, entity.Action)
	assert.Equal(t, "uuid-123-456", entity.ExistingEntityID)
}

func TestExtractedRelationshipStruct(t *testing.T) {
	rel := ExtractedRelationship{
		SourceRef:   "person_john",
		TargetRef:   "org_acme",
		Type:        "WORKS_FOR",
		Description: "John works at Acme since 2020",
	}

	assert.Equal(t, "person_john", rel.SourceRef)
	assert.Equal(t, "org_acme", rel.TargetRef)
	assert.Equal(t, "WORKS_FOR", rel.Type)
	assert.Equal(t, "John works at Acme since 2020", rel.Description)
}

func TestEntityExtractionOutputStruct(t *testing.T) {
	output := EntityExtractionOutput{
		Entities: []ExtractedEntity{
			{Name: "Entity1", Type: "Type1"},
			{Name: "Entity2", Type: "Type2"},
		},
	}

	assert.Len(t, output.Entities, 2)
	assert.Equal(t, "Entity1", output.Entities[0].Name)
	assert.Equal(t, "Entity2", output.Entities[1].Name)
}

func TestRelationshipExtractionOutputStruct(t *testing.T) {
	output := RelationshipExtractionOutput{
		Relationships: []ExtractedRelationship{
			{SourceRef: "a", TargetRef: "b", Type: "REL1"},
			{SourceRef: "b", TargetRef: "c", Type: "REL2"},
		},
	}

	assert.Len(t, output.Relationships, 2)
	assert.Equal(t, "REL1", output.Relationships[0].Type)
	assert.Equal(t, "REL2", output.Relationships[1].Type)
}

// TestParseEntityExtractionOutput_UnmarshalError tests the unmarshal error path
// when input marshals to valid JSON but cannot be unmarshalled into EntityExtractionOutput.
func TestParseEntityExtractionOutput_UnmarshalError(t *testing.T) {
	// Create a struct where "entities" is a string instead of an array
	// This marshals fine but cannot unmarshal into []ExtractedEntity
	input := struct {
		Entities string `json:"entities"`
	}{
		Entities: "not an array",
	}

	_, err := ParseEntityExtractionOutput(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal output")
}

// TestParseRelationshipExtractionOutput_UnmarshalError tests the unmarshal error path
// when input marshals to valid JSON but cannot be unmarshalled into RelationshipExtractionOutput.
func TestParseRelationshipExtractionOutput_UnmarshalError(t *testing.T) {
	// Create a struct where "relationships" is a string instead of an array
	// This marshals fine but cannot unmarshal into []ExtractedRelationship
	input := struct {
		Relationships string `json:"relationships"`
	}{
		Relationships: "not an array",
	}

	_, err := ParseRelationshipExtractionOutput(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal output")
}

// =============================================================================
// Tests for BuildEntitySchemaFromTemplatePack
// =============================================================================

func TestBuildEntitySchemaFromTemplatePack_EmptySchemas(t *testing.T) {
	// When objectSchemas is empty, should return the default EntityExtractionSchema
	schema := BuildEntitySchemaFromTemplatePack(map[string]ObjectSchema{})

	assert.NotNil(t, schema)
	assert.Equal(t, "Output containing extracted entities from the document", schema.Description)
	assert.Contains(t, schema.Required, "entities")

	// Check entities array property
	entitiesSchema := schema.Properties["entities"]
	assert.NotNil(t, entitiesSchema)

	// The type field should NOT have an Enum constraint (default schema)
	typeSchema := entitiesSchema.Items.Properties["type"]
	assert.NotNil(t, typeSchema)
	assert.Empty(t, typeSchema.Enum, "default schema should not have enum constraint")
}

func TestBuildEntitySchemaFromTemplatePack_SingleType(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {
			Name:        "Person",
			Description: "A human being",
		},
	}

	schema := BuildEntitySchemaFromTemplatePack(schemas)

	assert.NotNil(t, schema)
	assert.Equal(t, "Output containing extracted entities from the document", schema.Description)

	// Check that type field has enum constraint with single value
	typeSchema := schema.Properties["entities"].Items.Properties["type"]
	assert.NotNil(t, typeSchema)
	assert.Len(t, typeSchema.Enum, 1)
	assert.Contains(t, typeSchema.Enum, "Person")
}

func TestBuildEntitySchemaFromTemplatePack_MultipleTypes(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {
			Name:        "Person",
			Description: "A human being",
		},
		"Organization": {
			Name:        "Organization",
			Description: "A company or group",
		},
		"Location": {
			Name:        "Location",
			Description: "A geographic place",
		},
	}

	schema := BuildEntitySchemaFromTemplatePack(schemas)

	assert.NotNil(t, schema)

	// Check that type field has enum constraint with all types
	typeSchema := schema.Properties["entities"].Items.Properties["type"]
	assert.NotNil(t, typeSchema)
	assert.Len(t, typeSchema.Enum, 3)
	assert.Contains(t, typeSchema.Enum, "Person")
	assert.Contains(t, typeSchema.Enum, "Organization")
	assert.Contains(t, typeSchema.Enum, "Location")
}

func TestBuildEntitySchemaFromTemplatePack_PreservesSchemaStructure(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person": {Name: "Person"},
	}

	schema := BuildEntitySchemaFromTemplatePack(schemas)

	// Verify top-level structure
	assert.NotNil(t, schema)
	assert.Contains(t, schema.Required, "entities")

	// Verify entity item schema has all required fields
	itemSchema := schema.Properties["entities"].Items
	assert.Contains(t, itemSchema.Required, "name")
	assert.Contains(t, itemSchema.Required, "type")

	// Verify all entity properties exist
	assert.Contains(t, itemSchema.Properties, "name")
	assert.Contains(t, itemSchema.Properties, "type")
	assert.Contains(t, itemSchema.Properties, "description")
	assert.Contains(t, itemSchema.Properties, "properties")
	assert.Contains(t, itemSchema.Properties, "action")
	assert.Contains(t, itemSchema.Properties, "existing_entity_id")

	// Verify action enum values are preserved
	actionSchema := itemSchema.Properties["action"]
	assert.Equal(t, []string{"create", "enrich", "reference"}, actionSchema.Enum)
}

func TestBuildEntitySchemaFromTemplatePack_NilSchemas(t *testing.T) {
	// nil input should be treated same as empty
	schema := BuildEntitySchemaFromTemplatePack(nil)

	assert.NotNil(t, schema)
	assert.Equal(t, "Output containing extracted entities from the document", schema.Description)

	// Should return default schema (no enum constraint on type)
	typeSchema := schema.Properties["entities"].Items.Properties["type"]
	assert.Empty(t, typeSchema.Enum)
}

// =============================================================================
// Tests for BuildRelationshipSchemaFromTemplatePack
// =============================================================================

func TestBuildRelationshipSchemaFromTemplatePack_EmptySchemas(t *testing.T) {
	// When relationshipSchemas is empty, should return the default RelationshipExtractionSchema
	schema := BuildRelationshipSchemaFromTemplatePack(map[string]RelationshipSchema{})

	assert.NotNil(t, schema)
	assert.Equal(t, "Output containing extracted relationships between entities", schema.Description)
	assert.Contains(t, schema.Required, "relationships")

	// Check relationships array property
	relSchema := schema.Properties["relationships"]
	assert.NotNil(t, relSchema)

	// The type field should NOT have an Enum constraint (default schema)
	typeSchema := relSchema.Items.Properties["type"]
	assert.NotNil(t, typeSchema)
	assert.Empty(t, typeSchema.Enum, "default schema should not have enum constraint")
}

func TestBuildRelationshipSchemaFromTemplatePack_SingleType(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"WORKS_AT": {
			Name:        "WORKS_AT",
			Description: "Employment relationship",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Organization"},
		},
	}

	schema := BuildRelationshipSchemaFromTemplatePack(schemas)

	assert.NotNil(t, schema)
	assert.Equal(t, "Output containing extracted relationships between entities", schema.Description)

	// Check that type field has enum constraint with single value
	typeSchema := schema.Properties["relationships"].Items.Properties["type"]
	assert.NotNil(t, typeSchema)
	assert.Len(t, typeSchema.Enum, 1)
	assert.Contains(t, typeSchema.Enum, "WORKS_AT")
}

func TestBuildRelationshipSchemaFromTemplatePack_MultipleTypes(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"WORKS_AT": {
			Name:        "WORKS_AT",
			Description: "Employment relationship",
		},
		"LOCATED_IN": {
			Name:        "LOCATED_IN",
			Description: "Geographic location",
		},
		"PARENT_OF": {
			Name:        "PARENT_OF",
			Description: "Parental relationship",
		},
	}

	schema := BuildRelationshipSchemaFromTemplatePack(schemas)

	assert.NotNil(t, schema)

	// Check that type field has enum constraint with all types
	typeSchema := schema.Properties["relationships"].Items.Properties["type"]
	assert.NotNil(t, typeSchema)
	assert.Len(t, typeSchema.Enum, 3)
	assert.Contains(t, typeSchema.Enum, "WORKS_AT")
	assert.Contains(t, typeSchema.Enum, "LOCATED_IN")
	assert.Contains(t, typeSchema.Enum, "PARENT_OF")
}

func TestBuildRelationshipSchemaFromTemplatePack_PreservesSchemaStructure(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"KNOWS": {Name: "KNOWS"},
	}

	schema := BuildRelationshipSchemaFromTemplatePack(schemas)

	// Verify top-level structure
	assert.NotNil(t, schema)
	assert.Contains(t, schema.Required, "relationships")

	// Verify relationship item schema has all required fields
	itemSchema := schema.Properties["relationships"].Items
	assert.Contains(t, itemSchema.Required, "source_ref")
	assert.Contains(t, itemSchema.Required, "target_ref")
	assert.Contains(t, itemSchema.Required, "type")

	// Verify all relationship properties exist
	assert.Contains(t, itemSchema.Properties, "source_ref")
	assert.Contains(t, itemSchema.Properties, "target_ref")
	assert.Contains(t, itemSchema.Properties, "type")
	assert.Contains(t, itemSchema.Properties, "description")
}

func TestBuildRelationshipSchemaFromTemplatePack_NilSchemas(t *testing.T) {
	// nil input should be treated same as empty
	schema := BuildRelationshipSchemaFromTemplatePack(nil)

	assert.NotNil(t, schema)
	assert.Equal(t, "Output containing extracted relationships between entities", schema.Description)

	// Should return default schema (no enum constraint on type)
	typeSchema := schema.Properties["relationships"].Items.Properties["type"]
	assert.Empty(t, typeSchema.Enum)
}

func TestBuildRelationshipSchemaFromTemplatePack_ManyTypes(t *testing.T) {
	// Test with many relationship types
	schemas := make(map[string]RelationshipSchema)
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("REL_TYPE_%d", i)
		schemas[name] = RelationshipSchema{
			Name:        name,
			Description: fmt.Sprintf("Relationship type %d", i),
		}
	}

	schema := BuildRelationshipSchemaFromTemplatePack(schemas)

	assert.NotNil(t, schema)

	// Check that type field has all 20 enum values
	typeSchema := schema.Properties["relationships"].Items.Properties["type"]
	assert.NotNil(t, typeSchema)
	assert.Len(t, typeSchema.Enum, 20)
}

func TestBuildEntitySchemaFromTemplatePack_ManyTypes(t *testing.T) {
	// Test with many entity types
	schemas := make(map[string]ObjectSchema)
	for i := 0; i < 15; i++ {
		name := fmt.Sprintf("EntityType%d", i)
		schemas[name] = ObjectSchema{
			Name:        name,
			Description: fmt.Sprintf("Entity type %d", i),
		}
	}

	schema := BuildEntitySchemaFromTemplatePack(schemas)

	assert.NotNil(t, schema)

	// Check that type field has all 15 enum values
	typeSchema := schema.Properties["entities"].Items.Properties["type"]
	assert.NotNil(t, typeSchema)
	assert.Len(t, typeSchema.Enum, 15)
}
