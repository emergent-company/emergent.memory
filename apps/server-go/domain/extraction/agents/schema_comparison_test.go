package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/adk"
	"github.com/emergent/emergent-core/pkg/kreuzberg"
)

// SchemaVariant represents the different ways to provide schema to Gemini
type SchemaVariant string

const (
	// SchemaInPrompt embeds the JSON schema as text in the prompt
	SchemaInPrompt SchemaVariant = "schema_in_prompt"
	// ResponseSchema uses genai.Schema object in GenerateContentConfig
	ResponseSchema SchemaVariant = "response_schema"
	// ResponseJsonSchema uses raw JSON schema in GenerateContentConfig
	ResponseJsonSchema SchemaVariant = "response_json_schema"
)

// ComparisonResult holds the results from one extraction run
type ComparisonResult struct {
	Variant           SchemaVariant
	EntityCount       int
	RelationshipCount int
	EntityDuration    time.Duration
	RelDuration       time.Duration
	TotalDuration     time.Duration
	EntityPrecision   float64
	EntityRecall      float64
	OrphanRate        float64
	ParseError        error
	ExtractionError   error
}

// Entity types for the protocol document
var protocolEntityTypes = []string{"Person", "Organization", "Location", "Meeting", "Resolution", "AgendaItem"}

// Relationship types for the protocol document
var protocolRelTypes = []string{
	"CHAIRED", "SECRETARY_OF", "ATTENDED", "ABSENT_FROM",
	"HELD_AT", "HAD_AGENDA", "PASSED_RESOLUTION", "REJECTED_RESOLUTION",
	"VOTED_FOR", "VOTED_AGAINST", "ABSTAINED", "ORGANIZED_BY",
}

// getEntitySchemaForPrompt returns JSON schema as a string for embedding in prompt
func getEntitySchemaForPrompt() string {
	return `{
  "$schema": "https://json-schema.org/draft-07/schema",
  "type": "object",
  "properties": {
    "entities": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string", "description": "Unique identifier (e.g., 'person_1', 'resolution_1')"},
          "type": {"type": "string", "enum": ["Person", "Organization", "Location", "Meeting", "Resolution", "AgendaItem"]},
          "name": {"type": "string", "description": "Human-readable name"},
          "description": {"type": "string", "description": "Brief description"},
          "properties": {"type": "object", "description": "Type-specific properties"}
        },
        "required": ["id", "type", "name"]
      }
    }
  },
  "required": ["entities"]
}`
}

// getRelationshipSchemaForPrompt returns relationship JSON schema as string
func getRelationshipSchemaForPrompt() string {
	return `{
  "$schema": "https://json-schema.org/draft-07/schema",
  "type": "object",
  "properties": {
    "relationships": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "source_id": {"type": "string", "description": "ID of the source entity"},
          "target_id": {"type": "string", "description": "ID of the target entity"},
          "type": {"type": "string", "enum": ["CHAIRED", "SECRETARY_OF", "ATTENDED", "ABSENT_FROM", "HELD_AT", "HAD_AGENDA", "PASSED_RESOLUTION", "REJECTED_RESOLUTION", "VOTED_FOR", "VOTED_AGAINST", "ABSTAINED", "ORGANIZED_BY"]},
          "description": {"type": "string", "description": "Optional description"}
        },
        "required": ["source_id", "target_id", "type"]
      }
    }
  },
  "required": ["relationships"]
}`
}

// getEntityGenaiSchema returns a genai.Schema for ResponseSchema config
func getEntityGenaiSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"entities": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"id": {
							Type:        genai.TypeString,
							Description: "Unique identifier (e.g., 'person_1', 'resolution_1')",
						},
						"type": {
							Type:        genai.TypeString,
							Enum:        protocolEntityTypes,
							Description: "Entity type",
						},
						"name": {
							Type:        genai.TypeString,
							Description: "Human-readable name",
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Brief description",
						},
						"properties": {
							Type:        genai.TypeObject,
							Description: "Type-specific properties",
						},
					},
					Required: []string{"id", "type", "name"},
				},
			},
		},
		Required: []string{"entities"},
	}
}

// getRelationshipGenaiSchema returns a genai.Schema for relationship extraction
func getRelationshipGenaiSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"relationships": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"source_id": {
							Type:        genai.TypeString,
							Description: "ID of the source entity",
						},
						"target_id": {
							Type:        genai.TypeString,
							Description: "ID of the target entity",
						},
						"type": {
							Type:        genai.TypeString,
							Enum:        protocolRelTypes,
							Description: "Relationship type",
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Optional description",
						},
					},
					Required: []string{"source_id", "target_id", "type"},
				},
			},
		},
		Required: []string{"relationships"},
	}
}

// getEntityJsonSchemaMap returns raw JSON schema as map for ResponseJsonSchema
func getEntityJsonSchemaMap() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"entities": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Unique identifier (e.g., 'person_1', 'resolution_1')",
						},
						"type": map[string]any{
							"type":        "string",
							"enum":        protocolEntityTypes,
							"description": "Entity type",
						},
						"name": map[string]any{
							"type":        "string",
							"description": "Human-readable name",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Brief description",
						},
						"properties": map[string]any{
							"type":        "object",
							"description": "Type-specific properties",
						},
					},
					"required": []string{"id", "type", "name"},
				},
			},
		},
		"required": []string{"entities"},
	}
}

// getRelationshipJsonSchemaMap returns raw JSON schema for relationships
func getRelationshipJsonSchemaMap() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"relationships": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"source_id": map[string]any{
							"type":        "string",
							"description": "ID of the source entity",
						},
						"target_id": map[string]any{
							"type":        "string",
							"description": "ID of the target entity",
						},
						"type": map[string]any{
							"type":        "string",
							"enum":        protocolRelTypes,
							"description": "Relationship type",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Optional description",
						},
					},
					"required": []string{"source_id", "target_id", "type"},
				},
			},
		},
		"required": []string{"relationships"},
	}
}

// buildEntityPrompt builds the entity extraction prompt
func buildEntityPrompt(variant SchemaVariant, documentText string) string {
	basePrompt := `You are a legal document specialist. Extract all entities from this meeting protocol.

## Entity Types
- Person: People mentioned (chairman, secretary, attendees, absentees)
- Organization: Companies or organizations mentioned
- Location: Places mentioned
- Meeting: The meeting itself
- Resolution: Resolutions voted on
- AgendaItem: Agenda items discussed

## Rules
1. Give each entity a unique ID (e.g., "person_1", "meeting_1", "resolution_1")
2. For Person entities, include their role/title in the properties
3. Do NOT create separate entities for job titles - include them as properties of Person entities
4. Include vote counts for resolutions in properties
`

	switch variant {
	case SchemaInPrompt:
		return fmt.Sprintf(`%s
## JSON Schema
%s

## Document
%s

Return ONLY the JSON object.`, basePrompt, getEntitySchemaForPrompt(), documentText)

	case ResponseSchema, ResponseJsonSchema:
		// When using native schema, we don't need to include it in prompt
		return fmt.Sprintf(`%s
## Document
%s

Return the extracted entities as JSON.`, basePrompt, documentText)
	}

	return ""
}

// buildRelationshipPrompt builds the relationship extraction prompt
func buildRelationshipPrompt(variant SchemaVariant, documentText string, entities []TwoStepEntity) string {
	entitiesJSON, _ := json.MarshalIndent(entities, "", "  ")

	basePrompt := `You are a legal document specialist. Given the extracted entities and the original document, identify relationships between entities.

## Relationship Types
- CHAIRED: Person chaired the meeting
- SECRETARY_OF: Person was secretary of the meeting
- ATTENDED: Person attended the meeting
- ABSENT_FROM: Person was absent from the meeting
- HELD_AT: Meeting was held at a location
- HAD_AGENDA: Meeting had an agenda item
- PASSED_RESOLUTION: Meeting passed a resolution
- REJECTED_RESOLUTION: Meeting rejected a resolution
- VOTED_FOR: Person voted for a resolution
- VOTED_AGAINST: Person voted against a resolution
- ABSTAINED: Person abstained from voting
- ORGANIZED_BY: Meeting was organized by an organization

## Rules
1. Only create relationships between entities that exist in the provided list
2. Use the exact entity IDs from the list
3. Every entity should be connected to at least one other entity
4. If an Organization is mentioned, connect it to the meeting using ORGANIZED_BY
`

	switch variant {
	case SchemaInPrompt:
		return fmt.Sprintf(`%s
## JSON Schema
%s

## Extracted Entities
%s

## Original Document
%s

Return ONLY the JSON object with relationships.`, basePrompt, getRelationshipSchemaForPrompt(), string(entitiesJSON), documentText)

	case ResponseSchema, ResponseJsonSchema:
		return fmt.Sprintf(`%s
## Extracted Entities
%s

## Original Document
%s

Return the relationships as JSON.`, basePrompt, string(entitiesJSON), documentText)
	}

	return ""
}

// getGenerateConfig returns the appropriate GenerateContentConfig for the variant
func getGenerateConfig(variant SchemaVariant, isEntity bool) *genai.GenerateContentConfig {
	temp := float32(0.0)
	maxTokens := int32(8192)

	switch variant {
	case SchemaInPrompt:
		return &genai.GenerateContentConfig{
			Temperature:     &temp,
			MaxOutputTokens: maxTokens,
		}

	case ResponseSchema:
		var schema *genai.Schema
		if isEntity {
			schema = getEntityGenaiSchema()
		} else {
			schema = getRelationshipGenaiSchema()
		}
		return &genai.GenerateContentConfig{
			Temperature:      &temp,
			MaxOutputTokens:  maxTokens,
			ResponseMIMEType: "application/json",
			ResponseSchema:   schema,
		}

	case ResponseJsonSchema:
		var jsonSchema map[string]any
		if isEntity {
			jsonSchema = getEntityJsonSchemaMap()
		} else {
			jsonSchema = getRelationshipJsonSchemaMap()
		}
		return &genai.GenerateContentConfig{
			Temperature:        &temp,
			MaxOutputTokens:    maxTokens,
			ResponseMIMEType:   "application/json",
			ResponseJsonSchema: jsonSchema,
		}
	}

	return nil
}

// runExtraction runs the two-step extraction for a given variant
func runExtraction(
	ctx context.Context,
	t *testing.T,
	llm adkmodel.LLM,
	variant SchemaVariant,
	documentText string,
	groundTruth *ProtocolGroundTruthInput,
) ComparisonResult {
	result := ComparisonResult{Variant: variant}
	totalStart := time.Now()

	// Step 1: Entity extraction
	entityStart := time.Now()
	entityPrompt := buildEntityPrompt(variant, documentText)
	entityConfig := getGenerateConfig(variant, true)

	t.Logf("  [%s] Entity prompt: %d chars", variant, len(entityPrompt))

	entityResponse, err := callComparisonLLM(ctx, llm, entityPrompt, entityConfig)
	result.EntityDuration = time.Since(entityStart)

	if err != nil {
		result.ExtractionError = fmt.Errorf("entity extraction failed: %w", err)
		return result
	}

	var entitiesOutput TwoStepEntitiesOutput
	if err := json.Unmarshal([]byte(entityResponse), &entitiesOutput); err != nil {
		result.ParseError = fmt.Errorf("entity parse failed: %w (response: %s)", err, truncate(entityResponse, 200))
		return result
	}
	result.EntityCount = len(entitiesOutput.Entities)

	// Step 2: Relationship extraction
	relStart := time.Now()
	relPrompt := buildRelationshipPrompt(variant, documentText, entitiesOutput.Entities)
	relConfig := getGenerateConfig(variant, false)

	t.Logf("  [%s] Relationship prompt: %d chars", variant, len(relPrompt))

	relResponse, err := callComparisonLLM(ctx, llm, relPrompt, relConfig)
	result.RelDuration = time.Since(relStart)

	if err != nil {
		result.ExtractionError = fmt.Errorf("relationship extraction failed: %w", err)
		return result
	}

	var relsOutput TwoStepRelationshipsOutput
	if err := json.Unmarshal([]byte(relResponse), &relsOutput); err != nil {
		result.ParseError = fmt.Errorf("relationship parse failed: %w (response: %s)", err, truncate(relResponse, 200))
		return result
	}
	result.RelationshipCount = len(relsOutput.Relationships)

	result.TotalDuration = time.Since(totalStart)

	// Calculate metrics
	metrics := calculateComparisonMetrics(entitiesOutput.Entities, relsOutput.Relationships, groundTruth)
	result.EntityPrecision = metrics.EntityPrecision
	result.EntityRecall = metrics.EntityRecall
	result.OrphanRate = metrics.OrphanRate

	return result
}

// callComparisonLLM calls the LLM with the given prompt and config
func callComparisonLLM(ctx context.Context, llm adkmodel.LLM, prompt string, config *genai.GenerateContentConfig) (string, error) {
	llmRequest := &adkmodel.LLMRequest{
		Contents: []*genai.Content{
			{
				Role:  "user",
				Parts: []*genai.Part{{Text: prompt}},
			},
		},
		Config: config,
	}

	var responseText string
	var lastErr error
	for resp, err := range llm.GenerateContent(ctx, llmRequest, false) {
		if err != nil {
			lastErr = err
			break
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText += part.Text
				}
			}
		}
	}

	if lastErr != nil {
		return "", lastErr
	}

	return cleanComparisonJSONResponse(responseText), nil
}

// cleanComparisonJSONResponse cleans markdown code blocks from response
func cleanComparisonJSONResponse(response string) string {
	cleaned := strings.TrimSpace(response)
	if strings.HasPrefix(cleaned, "```json") {
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
	} else if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
	}
	return cleaned
}

// ComparisonMetrics holds calculated metrics
type ComparisonMetrics struct {
	EntityPrecision float64
	EntityRecall    float64
	OrphanRate      float64
	MatchedPersons  int
	OrphanCount     int
}

// calculateComparisonMetrics calculates precision/recall metrics
func calculateComparisonMetrics(entities []TwoStepEntity, relationships []TwoStepRelationship, groundTruth *ProtocolGroundTruthInput) ComparisonMetrics {
	metrics := ComparisonMetrics{}

	// Build set of expected person names
	expectedPersons := make(map[string]bool)
	if groundTruth.Chairman.Name != "" {
		expectedPersons[strings.ToLower(groundTruth.Chairman.Name)] = true
	}
	if groundTruth.Secretary.Name != "" {
		expectedPersons[strings.ToLower(groundTruth.Secretary.Name)] = true
	}
	for _, a := range groundTruth.Attendees {
		expectedPersons[strings.ToLower(a.Name)] = true
	}
	for _, a := range groundTruth.Absentees {
		expectedPersons[strings.ToLower(a.Name)] = true
	}

	// Count matched persons
	extractedPersonCount := 0
	for _, e := range entities {
		if e.Type == "Person" {
			extractedPersonCount++
			nameLower := strings.ToLower(e.Name)
			if expectedPersons[nameLower] {
				metrics.MatchedPersons++
			}
		}
	}

	// Calculate precision and recall
	if extractedPersonCount > 0 {
		metrics.EntityPrecision = float64(metrics.MatchedPersons) / float64(extractedPersonCount)
	}
	if len(expectedPersons) > 0 {
		metrics.EntityRecall = float64(metrics.MatchedPersons) / float64(len(expectedPersons))
	}

	// Calculate orphan rate
	connectedEntities := make(map[string]bool)
	for _, r := range relationships {
		connectedEntities[r.SourceID] = true
		connectedEntities[r.TargetID] = true
	}

	for _, e := range entities {
		if !connectedEntities[e.ID] {
			metrics.OrphanCount++
		}
	}

	if len(entities) > 0 {
		metrics.OrphanRate = float64(metrics.OrphanCount) / float64(len(entities))
	}

	return metrics
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TestSchemaVariantComparison runs all three schema variants and compares results
func TestSchemaVariantComparison(t *testing.T) {
	projectID := os.Getenv("VERTEX_PROJECT_ID")
	if projectID == "" {
		t.Skip("VERTEX_PROJECT_ID not set, skipping E2E test")
	}

	groundTruthPath := "/root/doc-processing-suite/output/ground_truth/protocol/protocol-001-en.json"
	pdfPath := "/root/doc-processing-suite/output/pdfs/protocol/protocol-001-en.pdf"

	if _, err := os.Stat(groundTruthPath); os.IsNotExist(err) {
		t.Skipf("Ground truth file not found: %s", groundTruthPath)
	}
	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		t.Skipf("PDF file not found: %s", pdfPath)
	}

	ctx := context.Background()

	// Load ground truth
	groundTruth, err := loadGroundTruth(groundTruthPath)
	require.NoError(t, err, "Failed to load ground truth")

	// Extract text from PDF
	kreuzbergURL := os.Getenv("KREUZBERG_URL")
	if kreuzbergURL == "" {
		kreuzbergURL = "http://localhost:8000"
	}

	cfg := &config.Config{
		Kreuzberg: config.KreuzbergConfig{
			Enabled:    true,
			ServiceURL: kreuzbergURL,
			TimeoutMs:  60000,
		},
	}
	kLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client := kreuzberg.NewClient(cfg, kLogger)

	pdfBytes, err := os.ReadFile(pdfPath)
	require.NoError(t, err, "Failed to read PDF")

	result, err := client.ExtractText(ctx, pdfBytes, filepath.Base(pdfPath), "application/pdf", nil)
	require.NoError(t, err, "Failed to extract PDF")

	documentText := result.Content
	t.Logf("Extracted %d characters from PDF", len(documentText))
	t.Logf("Ground truth: %d attendees, %d resolutions",
		len(groundTruth.InputData.Attendees),
		len(groundTruth.InputData.Resolutions))

	// Create model
	llmConfig := &config.LLMConfig{
		GCPProjectID:     projectID,
		VertexAILocation: "us-central1",
		Model:            "gemini-2.0-flash",
		MaxOutputTokens:  8192,
		Temperature:      0,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	modelFactory := adk.NewModelFactory(llmConfig, logger)

	llm, err := modelFactory.CreateModel(ctx)
	require.NoError(t, err, "Failed to create model")

	// Run all three variants
	variants := []SchemaVariant{SchemaInPrompt, ResponseSchema, ResponseJsonSchema}
	results := make([]ComparisonResult, len(variants))

	for i, variant := range variants {
		t.Logf("\n=== Running variant: %s ===", variant)
		results[i] = runExtraction(ctx, t, llm, variant, documentText, &groundTruth.InputData)

		if results[i].ExtractionError != nil {
			t.Logf("  ERROR: %v", results[i].ExtractionError)
		} else if results[i].ParseError != nil {
			t.Logf("  PARSE ERROR: %v", results[i].ParseError)
		} else {
			t.Logf("  Entities: %d, Relationships: %d", results[i].EntityCount, results[i].RelationshipCount)
			t.Logf("  Entity time: %v, Rel time: %v, Total: %v",
				results[i].EntityDuration.Round(time.Millisecond),
				results[i].RelDuration.Round(time.Millisecond),
				results[i].TotalDuration.Round(time.Millisecond))
		}
	}

	// Print comparison table
	t.Log("\n" + strings.Repeat("=", 100))
	t.Log("SCHEMA VARIANT COMPARISON RESULTS")
	t.Log(strings.Repeat("=", 100))
	t.Logf("%-25s | %8s | %8s | %10s | %10s | %10s | %8s | %8s",
		"Variant", "Entities", "Rels", "Entity(ms)", "Rel(ms)", "Total(ms)", "Precision", "Recall")
	t.Log(strings.Repeat("-", 100))

	for _, r := range results {
		if r.ExtractionError != nil || r.ParseError != nil {
			errMsg := "extraction error"
			if r.ParseError != nil {
				errMsg = "parse error"
			}
			t.Logf("%-25s | %s", r.Variant, errMsg)
			continue
		}

		t.Logf("%-25s | %8d | %8d | %10d | %10d | %10d | %7.1f%% | %7.1f%%",
			r.Variant,
			r.EntityCount,
			r.RelationshipCount,
			r.EntityDuration.Milliseconds(),
			r.RelDuration.Milliseconds(),
			r.TotalDuration.Milliseconds(),
			r.EntityPrecision*100,
			r.EntityRecall*100)
	}

	t.Log(strings.Repeat("=", 100))

	// Additional metrics
	t.Log("\nAdditional Metrics:")
	t.Logf("%-25s | %8s | %10s",
		"Variant", "Orphans", "OrphanRate")
	t.Log(strings.Repeat("-", 50))

	for _, r := range results {
		if r.ExtractionError != nil || r.ParseError != nil {
			continue
		}
		orphanCount := int(r.OrphanRate * float64(r.EntityCount))
		t.Logf("%-25s | %8d | %9.1f%%",
			r.Variant,
			orphanCount,
			r.OrphanRate*100)
	}
}
