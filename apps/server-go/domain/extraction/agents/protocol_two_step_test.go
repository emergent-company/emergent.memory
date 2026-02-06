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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/adk"
	"github.com/emergent/emergent-core/pkg/kreuzberg"
)

type TwoStepEntity struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

type TwoStepRelationship struct {
	SourceID    string `json:"source_id"`
	TargetID    string `json:"target_id"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type TwoStepEntitiesOutput struct {
	Entities []TwoStepEntity `json:"entities"`
}

type TwoStepRelationshipsOutput struct {
	Relationships []TwoStepRelationship `json:"relationships"`
}

func getTwoStepEntitySchema() string {
	return `{
  "$schema": "https://json-schema.org/draft-07/schema",
  "title": "ExtractedEntities",
  "type": "object",
  "properties": {
    "entities": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string", "description": "Unique identifier for the entity (e.g., 'person_1', 'resolution_1')"},
          "type": {"type": "string", "enum": ["Person", "Organization", "Location", "Meeting", "Resolution", "AgendaItem"], "description": "Entity type"},
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

func getTwoStepRelationshipSchema() string {
	return `{
  "$schema": "https://json-schema.org/draft-07/schema",
  "title": "ExtractedRelationships",
  "type": "object",
  "properties": {
    "relationships": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "source_id": {"type": "string", "description": "ID of the source entity"},
          "target_id": {"type": "string", "description": "ID of the target entity"},
          "type": {"type": "string", "enum": ["CHAIRED", "SECRETARY_OF", "ATTENDED", "ABSENT_FROM", "HELD_AT", "HAD_AGENDA", "PASSED_RESOLUTION", "REJECTED_RESOLUTION", "VOTED_FOR", "VOTED_AGAINST", "ABSTAINED", "ORGANIZED_BY"], "description": "Relationship type"},
          "description": {"type": "string", "description": "Optional description"}
        },
        "required": ["source_id", "target_id", "type"]
      }
    }
  },
  "required": ["relationships"]
}`
}

func getEntityExtractionPrompt(schema, documentText string) string {
	return fmt.Sprintf(`You are a legal document specialist. Extract all entities from this meeting protocol.

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

## JSON Schema
%s

## Document
%s

Return ONLY the JSON object.`, schema, documentText)
}

func getRelationshipExtractionPrompt(schema, documentText string, entities []TwoStepEntity) string {
	entitiesJSON, _ := json.MarshalIndent(entities, "", "  ")

	return fmt.Sprintf(`You are a legal document specialist. Given the extracted entities and the original document, identify relationships between entities.

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

## JSON Schema
%s

## Extracted Entities
%s

## Original Document
%s

Return ONLY the JSON object with relationships.`, schema, string(entitiesJSON), documentText)
}

func callLLM(ctx context.Context, llm adkmodel.LLM, prompt string, generateConfig *genai.GenerateContentConfig) (string, error) {
	llmRequest := &adkmodel.LLMRequest{
		Contents: []*genai.Content{
			{
				Role:  "user",
				Parts: []*genai.Part{{Text: prompt}},
			},
		},
		Config: generateConfig,
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

	return cleanJSONResponse(responseText), nil
}

func cleanJSONResponse(response string) string {
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

func TestProtocolTwoStepE2E(t *testing.T) {
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

	groundTruth, err := loadGroundTruth(groundTruthPath)
	require.NoError(t, err, "Failed to load ground truth")
	t.Logf("Ground truth: %s, %d attendees, %d resolutions",
		groundTruth.InputData.MeetingType,
		len(groundTruth.InputData.Attendees),
		len(groundTruth.InputData.Resolutions))

	documentText, err := extractTextFromPDFTwoStep(ctx, t, pdfPath)
	require.NoError(t, err, "Failed to extract text from PDF")
	t.Logf("Extracted %d characters from PDF", len(documentText))

	llmConfig := &config.LLMConfig{
		GCPProjectID:     projectID,
		VertexAILocation: "us-central1",
		Model:            "gemini-2.0-flash",
		MaxOutputTokens:  8192,
		Temperature:      0,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	modelFactory := adk.NewModelFactory(llmConfig, logger)

	llm, err := modelFactory.CreateModel(ctx)
	require.NoError(t, err, "Failed to create model")

	generateConfig := modelFactory.ExtractionGenerateConfig()

	// STEP 1: Extract entities
	t.Log("\n=== STEP 1: ENTITY EXTRACTION ===")
	entityPrompt := getEntityExtractionPrompt(getTwoStepEntitySchema(), documentText)
	t.Logf("Entity prompt length: %d chars", len(entityPrompt))

	entityResponse, err := callLLM(ctx, llm, entityPrompt, generateConfig)
	require.NoError(t, err, "Entity extraction failed")

	var entitiesOutput TwoStepEntitiesOutput
	err = json.Unmarshal([]byte(entityResponse), &entitiesOutput)
	require.NoError(t, err, "Failed to parse entity response: %s", entityResponse[:min(500, len(entityResponse))])

	t.Logf("Extracted %d entities", len(entitiesOutput.Entities))
	for _, e := range entitiesOutput.Entities {
		t.Logf("  [%s] %s (%s)", e.ID, e.Name, e.Type)
	}

	// STEP 2: Extract relationships
	t.Log("\n=== STEP 2: RELATIONSHIP EXTRACTION ===")
	relationshipPrompt := getRelationshipExtractionPrompt(
		getTwoStepRelationshipSchema(),
		documentText,
		entitiesOutput.Entities,
	)
	t.Logf("Relationship prompt length: %d chars", len(relationshipPrompt))

	relationshipResponse, err := callLLM(ctx, llm, relationshipPrompt, generateConfig)
	require.NoError(t, err, "Relationship extraction failed")

	var relationshipsOutput TwoStepRelationshipsOutput
	err = json.Unmarshal([]byte(relationshipResponse), &relationshipsOutput)
	require.NoError(t, err, "Failed to parse relationship response: %s", relationshipResponse[:min(500, len(relationshipResponse))])

	t.Logf("Extracted %d relationships", len(relationshipsOutput.Relationships))
	for _, r := range relationshipsOutput.Relationships {
		t.Logf("  %s -[%s]-> %s", r.SourceID, r.Type, r.TargetID)
	}

	// Calculate metrics
	metrics := calculateTwoStepMetrics(entitiesOutput.Entities, relationshipsOutput.Relationships, &groundTruth.InputData)

	t.Log("\n=== TWO-STEP EXTRACTION RESULTS ===")
	t.Logf("Entities: extracted=%d, expected_persons=%d, matched_persons=%d",
		len(entitiesOutput.Entities),
		len(groundTruth.InputData.Attendees)+2, // +2 for chairman and secretary
		metrics.MatchedPersons)
	t.Logf("Relationships: extracted=%d", len(relationshipsOutput.Relationships))
	t.Logf("Orphan entities: %d (%.1f%%)", metrics.OrphanCount, metrics.OrphanRate*100)
	t.Logf("Entity precision: %.1f%%", metrics.EntityPrecision*100)
	t.Logf("Entity recall: %.1f%%", metrics.EntityRecall*100)

	// Assertions
	assert.GreaterOrEqual(t, len(entitiesOutput.Entities), 10, "Should extract at least 10 entities")
	assert.GreaterOrEqual(t, len(relationshipsOutput.Relationships), 5, "Should extract at least 5 relationships")
	assert.LessOrEqual(t, metrics.OrphanRate, 0.5, "Orphan rate should be <= 50%")
}

type TwoStepMetrics struct {
	MatchedPersons  int
	EntityPrecision float64
	EntityRecall    float64
	OrphanCount     int
	OrphanRate      float64
}

func calculateTwoStepMetrics(entities []TwoStepEntity, relationships []TwoStepRelationship, expected *ProtocolGroundTruthInput) *TwoStepMetrics {
	metrics := &TwoStepMetrics{}

	expectedPersons := make(map[string]bool)
	expectedPersons[strings.ToLower(expected.Chairman.Name)] = true
	expectedPersons[strings.ToLower(expected.Secretary.Name)] = true
	for _, a := range expected.Attendees {
		expectedPersons[strings.ToLower(a.Name)] = true
	}
	for _, a := range expected.Absentees {
		expectedPersons[strings.ToLower(a.Name)] = true
	}

	extractedPersons := 0
	for _, e := range entities {
		if e.Type == "Person" {
			extractedPersons++
			if expectedPersons[strings.ToLower(e.Name)] {
				metrics.MatchedPersons++
			}
		}
	}

	if extractedPersons > 0 {
		metrics.EntityPrecision = float64(metrics.MatchedPersons) / float64(extractedPersons)
	}
	if len(expectedPersons) > 0 {
		metrics.EntityRecall = float64(metrics.MatchedPersons) / float64(len(expectedPersons))
	}

	connectedIDs := make(map[string]bool)
	for _, r := range relationships {
		connectedIDs[r.SourceID] = true
		connectedIDs[r.TargetID] = true
	}

	for _, e := range entities {
		if !connectedIDs[e.ID] {
			metrics.OrphanCount++
		}
	}

	if len(entities) > 0 {
		metrics.OrphanRate = float64(metrics.OrphanCount) / float64(len(entities))
	}

	return metrics
}

func extractTextFromPDFTwoStep(ctx context.Context, t *testing.T, pdfPath string) (string, error) {
	pdfData, err := os.ReadFile(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF: %w", err)
	}

	kreuzbergURL := os.Getenv("KREUZBERG_URL")
	if kreuzbergURL == "" {
		kreuzbergURL = "http://localhost:8787"
	}

	cfg := &config.Config{
		Kreuzberg: config.KreuzbergConfig{
			Enabled:    true,
			ServiceURL: kreuzbergURL,
			TimeoutMs:  60000,
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client := kreuzberg.NewClient(cfg, logger)

	if !client.IsEnabled() {
		t.Log("Kreuzberg not enabled, using fallback document text")
		return getFallbackProtocolText(), nil
	}

	result, err := client.ExtractText(ctx, pdfData, filepath.Base(pdfPath), "application/pdf", nil)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "unavailable") {
			t.Log("Kreuzberg service not available, using fallback document text")
			return getFallbackProtocolText(), nil
		}
		return "", fmt.Errorf("Kreuzberg extraction failed: %w", err)
	}

	return result.Content, nil
}
