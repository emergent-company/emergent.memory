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

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/adk"
	"github.com/emergent-company/emergent/pkg/kreuzberg"
)

// EntityOnlyExtractionOutput represents the expected flat JSON output.
// This matches the doc-processing-suite protocol schema.
type EntityOnlyExtractionOutput struct {
	DocumentID    string                 `json:"documentId"`
	Category      string                 `json:"category"`
	MeetingType   string                 `json:"meetingType"`
	MeetingNumber string                 `json:"meetingNumber"`
	Date          string                 `json:"date"`
	StartTime     string                 `json:"startTime"`
	EndTime       string                 `json:"endTime"`
	Location      string                 `json:"location"`
	Chairman      EntityOnlyPerson       `json:"chairman"`
	Secretary     EntityOnlyPerson       `json:"secretary"`
	Attendees     []EntityOnlyPerson     `json:"attendees"`
	Absentees     []EntityOnlyPerson     `json:"absentees"`
	AgendaItems   []string               `json:"agendaItems"`
	Resolutions   []EntityOnlyResolution `json:"resolutions"`
	Notes         string                 `json:"notes,omitempty"`
}

type EntityOnlyPerson struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Email string `json:"email,omitempty"`
}

type EntityOnlyResolution struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	VotesFor     int    `json:"votesFor"`
	VotesAgainst int    `json:"votesAgainst"`
	Abstentions  int    `json:"abstentions"`
	Passed       bool   `json:"passed"`
}

// EntityOnlyMetrics holds comparison metrics for entity-only extraction.
type EntityOnlyMetrics struct {
	TotalFields    int
	MatchedFields  int
	MismatchFields int
	MissingFields  int
	Accuracy       float64

	FieldResults map[string]FieldResult
}

type FieldResult struct {
	Field    string
	Match    bool
	Expected interface{}
	Got      interface{}
	Reason   string
}

// getEntityOnlySchema returns a flat JSON schema matching doc-processing-suite protocol schema.
func getEntityOnlySchema() string {
	return `{
  "$schema": "https://json-schema.org/draft-07/schema",
  "title": "ProtocolDocument",
  "description": "Meeting protocol document data schema",
  "type": "object",
  "properties": {
    "documentId": {"type": "string", "description": "Document identifier"},
    "category": {"type": "string", "const": "protocol"},
    "meetingType": {
      "type": "string", 
      "enum": ["board_meeting", "shareholder_meeting", "annual_general_meeting", "extraordinary_general_meeting"],
      "description": "Type of meeting"
    },
    "meetingNumber": {"type": "string", "description": "Meeting reference number (e.g., '2026-4')"},
    "date": {"type": "string", "pattern": "^\\d{4}-\\d{2}-\\d{2}$", "description": "Date in YYYY-MM-DD format"},
    "startTime": {"type": "string", "pattern": "^\\d{2}:\\d{2}$", "description": "Start time in HH:MM format"},
    "endTime": {"type": "string", "pattern": "^\\d{2}:\\d{2}$", "description": "End time in HH:MM format"},
    "location": {"type": "string", "description": "Meeting location"},
    "chairman": {
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "title": {"type": "string"},
        "email": {"type": "string"}
      },
      "required": ["name"]
    },
    "secretary": {
      "type": "object", 
      "properties": {
        "name": {"type": "string"},
        "title": {"type": "string"},
        "email": {"type": "string"}
      },
      "required": ["name"]
    },
    "attendees": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": {"type": "string"},
          "title": {"type": "string"},
          "email": {"type": "string"}
        },
        "required": ["name"]
      }
    },
    "absentees": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": {"type": "string"},
          "title": {"type": "string"},
          "email": {"type": "string"}
        },
        "required": ["name"]
      }
    },
    "agendaItems": {
      "type": "array",
      "items": {"type": "string"},
      "description": "List of agenda items"
    },
    "resolutions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "number": {"type": "integer"},
          "title": {"type": "string"},
          "description": {"type": "string"},
          "votesFor": {"type": "integer"},
          "votesAgainst": {"type": "integer"},
          "abstentions": {"type": "integer"},
          "passed": {"type": "boolean"}
        },
        "required": ["number", "title", "description", "votesFor", "votesAgainst", "abstentions", "passed"]
      }
    },
    "notes": {"type": "string", "description": "Additional meeting notes"}
  },
  "required": ["category", "meetingType", "meetingNumber", "date", "startTime", "endTime", "location", "chairman", "secretary", "attendees", "agendaItems", "resolutions"]
}`
}

// getEntityOnlyExtractionPrompt returns the extraction prompt.
func getEntityOnlyExtractionPrompt(schema, documentText string) string {
	return fmt.Sprintf(`You are a legal document specialist trained in extracting information from corporate meeting protocols.

Extract meeting protocol data from the following document.

## JSON Schema
%s

## Document Text (Protocol)
%s

## Extraction Guidelines for Protocols
1. **Meeting Details**: Extract meeting type, number, date, start/end times, and location
2. **Participants**: List all attendees with their roles (chairman, secretary, attendees, absentees)
3. **Agenda Items**: Extract each agenda item (just the text, without numbers if possible)
4. **Resolutions**: For each resolution, extract:
   - Resolution number and title
   - Description of what was resolved
   - Vote counts (for, against, abstentions)
   - Whether it passed

### Important Notes
- Meeting types: board_meeting, shareholder_meeting, annual_general_meeting, extraordinary_general_meeting
- Dates must be in YYYY-MM-DD format
- Times must be in HH:MM format
- Set documentId to the meeting number if no explicit document ID is present
- Set category to "protocol"
- Email fields may be empty if not visible in the document

Return ONLY the JSON object, no additional text or markdown formatting.`, schema, documentText)
}

// calculateEntityOnlyMetrics compares extracted data against ground truth.
func calculateEntityOnlyMetrics(extracted *EntityOnlyExtractionOutput, expected *ProtocolGroundTruthInput) *EntityOnlyMetrics {
	metrics := &EntityOnlyMetrics{
		FieldResults: make(map[string]FieldResult),
	}

	// Helper to add a field result
	addResult := func(field string, match bool, expected, got interface{}, reason string) {
		metrics.TotalFields++
		if match {
			metrics.MatchedFields++
		} else if expected != nil && got == nil {
			metrics.MissingFields++
		} else {
			metrics.MismatchFields++
		}
		metrics.FieldResults[field] = FieldResult{
			Field:    field,
			Match:    match,
			Expected: expected,
			Got:      got,
			Reason:   reason,
		}
	}

	// Compare simple fields
	addResult("category", extracted.Category == "protocol", "protocol", extracted.Category, "")

	// Meeting type - normalize comparison
	meetingTypeMatch := strings.EqualFold(extracted.MeetingType, expected.MeetingType) ||
		strings.Contains(strings.ToLower(extracted.MeetingType), strings.ToLower(expected.MeetingType))
	addResult("meetingType", meetingTypeMatch, expected.MeetingType, extracted.MeetingType, "")

	addResult("meetingNumber", extracted.MeetingNumber == expected.MeetingNumber, expected.MeetingNumber, extracted.MeetingNumber, "")
	addResult("date", extracted.Date == expected.Date, expected.Date, extracted.Date, "")
	addResult("startTime", extracted.StartTime == expected.StartTime, expected.StartTime, extracted.StartTime, "")
	addResult("endTime", extracted.EndTime == expected.EndTime, expected.EndTime, extracted.EndTime, "")
	addResult("location", strings.Contains(strings.ToLower(extracted.Location), strings.ToLower(expected.Location)[:min(20, len(expected.Location))]) ||
		strings.Contains(strings.ToLower(expected.Location), strings.ToLower(extracted.Location)[:min(20, len(extracted.Location))]),
		expected.Location, extracted.Location, "")

	// Chairman
	chairmanMatch := strings.EqualFold(extracted.Chairman.Name, expected.Chairman.Name)
	addResult("chairman.name", chairmanMatch, expected.Chairman.Name, extracted.Chairman.Name, "")

	// Secretary
	secretaryMatch := strings.EqualFold(extracted.Secretary.Name, expected.Secretary.Name)
	addResult("secretary.name", secretaryMatch, expected.Secretary.Name, extracted.Secretary.Name, "")

	// Attendees count
	addResult("attendees.count", len(extracted.Attendees) == len(expected.Attendees),
		len(expected.Attendees), len(extracted.Attendees), "")

	// Check attendee names (unordered match)
	attendeeMatches := 0
	for _, exp := range expected.Attendees {
		for _, ext := range extracted.Attendees {
			if strings.EqualFold(ext.Name, exp.Name) {
				attendeeMatches++
				break
			}
		}
	}
	addResult("attendees.names", attendeeMatches == len(expected.Attendees),
		len(expected.Attendees), attendeeMatches, fmt.Sprintf("%d of %d matched", attendeeMatches, len(expected.Attendees)))

	// Absentees count
	addResult("absentees.count", len(extracted.Absentees) == len(expected.Absentees),
		len(expected.Absentees), len(extracted.Absentees), "")

	// Agenda items count
	addResult("agendaItems.count", len(extracted.AgendaItems) == len(expected.AgendaItems),
		len(expected.AgendaItems), len(extracted.AgendaItems), "")

	// Resolutions count
	addResult("resolutions.count", len(extracted.Resolutions) == len(expected.Resolutions),
		len(expected.Resolutions), len(extracted.Resolutions), "")

	// Check resolution details
	for i, expRes := range expected.Resolutions {
		if i < len(extracted.Resolutions) {
			extRes := extracted.Resolutions[i]
			prefix := fmt.Sprintf("resolutions[%d]", i)

			addResult(prefix+".number", extRes.Number == expRes.Number, expRes.Number, extRes.Number, "")
			addResult(prefix+".votesFor", extRes.VotesFor == expRes.VotesFor, expRes.VotesFor, extRes.VotesFor, "")
			addResult(prefix+".votesAgainst", extRes.VotesAgainst == expRes.VotesAgainst, expRes.VotesAgainst, extRes.VotesAgainst, "")
			addResult(prefix+".abstentions", extRes.Abstentions == expRes.Abstentions, expRes.Abstentions, extRes.Abstentions, "")
			addResult(prefix+".passed", extRes.Passed == expRes.Passed, expRes.Passed, extRes.Passed, "")
		}
	}

	// Calculate accuracy
	if metrics.TotalFields > 0 {
		metrics.Accuracy = float64(metrics.MatchedFields) / float64(metrics.TotalFields)
	}

	return metrics
}

// TestProtocolEntityOnlyE2E tests entity-only extraction (no relationships).
// This directly compares against doc-processing-suite's flat JSON extraction.
func TestProtocolEntityOnlyE2E(t *testing.T) {
	projectID := os.Getenv("VERTEX_PROJECT_ID")
	if projectID == "" {
		t.Skip("VERTEX_PROJECT_ID not set, skipping E2E test")
	}

	// Check for ground truth and PDF files
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
	t.Logf("Loaded ground truth for: %s", groundTruth.DocumentID)
	t.Logf("  Meeting type: %s", groundTruth.InputData.MeetingType)
	t.Logf("  Date: %s", groundTruth.InputData.Date)
	t.Logf("  Chairman: %s", groundTruth.InputData.Chairman.Name)
	t.Logf("  Attendees: %d", len(groundTruth.InputData.Attendees))
	t.Logf("  Resolutions: %d", len(groundTruth.InputData.Resolutions))

	// Extract text from PDF using Kreuzberg
	documentText, err := extractTextFromPDFEntityOnly(ctx, t, pdfPath)
	require.NoError(t, err, "Failed to extract text from PDF")
	t.Logf("Extracted %d characters from PDF", len(documentText))

	// Create LLM config
	llmConfig := &config.LLMConfig{
		GCPProjectID:     projectID,
		VertexAILocation: "us-central1",
		Model:            "gemini-2.0-flash",
		MaxOutputTokens:  8192,
		Temperature:      0,
	}

	// Create model factory and model
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	modelFactory := adk.NewModelFactory(llmConfig, logger)

	llm, err := modelFactory.CreateModel(ctx)
	require.NoError(t, err, "Failed to create model")

	// Build the extraction prompt
	schema := getEntityOnlySchema()
	prompt := getEntityOnlyExtractionPrompt(schema, documentText)

	t.Logf("\n=== EXTRACTION PROMPT (first 500 chars) ===\n%s...\n", prompt[:min(500, len(prompt))])

	// Call the model directly for flat JSON extraction using ADK's iterator-based API
	generateConfig := modelFactory.ExtractionGenerateConfig()

	// Build LLMRequest with the prompt as user content
	llmRequest := &adkmodel.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					{Text: prompt},
				},
			},
		},
		Config: generateConfig,
	}

	// Call GenerateContent (non-streaming mode)
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
	require.NoError(t, lastErr, "Model generation failed")

	t.Logf("\n=== RAW RESPONSE ===\n%s\n", responseText)

	// Clean up the response (remove markdown code blocks if present)
	cleanedResponse := strings.TrimSpace(responseText)
	if strings.HasPrefix(cleanedResponse, "```json") {
		cleanedResponse = strings.TrimPrefix(cleanedResponse, "```json")
		cleanedResponse = strings.TrimSuffix(cleanedResponse, "```")
		cleanedResponse = strings.TrimSpace(cleanedResponse)
	} else if strings.HasPrefix(cleanedResponse, "```") {
		cleanedResponse = strings.TrimPrefix(cleanedResponse, "```")
		cleanedResponse = strings.TrimSuffix(cleanedResponse, "```")
		cleanedResponse = strings.TrimSpace(cleanedResponse)
	}

	// Parse the extracted data
	var extracted EntityOnlyExtractionOutput
	err = json.Unmarshal([]byte(cleanedResponse), &extracted)
	require.NoError(t, err, "Failed to parse extraction response")

	// Calculate metrics
	metrics := calculateEntityOnlyMetrics(&extracted, &groundTruth.InputData)

	// Log results
	t.Logf("\n=== ENTITY-ONLY EXTRACTION RESULTS ===")
	t.Logf("Total Fields: %d", metrics.TotalFields)
	t.Logf("Matched: %d", metrics.MatchedFields)
	t.Logf("Mismatched: %d", metrics.MismatchFields)
	t.Logf("Missing: %d", metrics.MissingFields)
	t.Logf("Accuracy: %.2f%%", metrics.Accuracy*100)

	t.Logf("\n=== FIELD RESULTS ===")
	for field, result := range metrics.FieldResults {
		status := "PASS"
		if !result.Match {
			status = "FAIL"
		}
		if result.Reason != "" {
			t.Logf("  %s: %s (expected=%v, got=%v) - %s", field, status, result.Expected, result.Got, result.Reason)
		} else {
			t.Logf("  %s: %s (expected=%v, got=%v)", field, status, result.Expected, result.Got)
		}
	}

	t.Logf("\n=== EXTRACTED DATA ===")
	extractedJSON, _ := json.MarshalIndent(extracted, "", "  ")
	t.Logf("%s", string(extractedJSON))

	// Assertions
	assert.GreaterOrEqual(t, metrics.Accuracy, 0.7, "Accuracy should be at least 70%")
	assert.True(t, metrics.FieldResults["chairman.name"].Match, "Chairman name should match")
	assert.True(t, metrics.FieldResults["date"].Match, "Date should match")
	assert.True(t, metrics.FieldResults["meetingType"].Match, "Meeting type should match")
}

// extractTextFromPDFEntityOnly extracts text from a PDF using Kreuzberg.
func extractTextFromPDFEntityOnly(ctx context.Context, t *testing.T, pdfPath string) (string, error) {
	// Read PDF file
	pdfData, err := os.ReadFile(pdfPath)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF: %w", err)
	}

	// Check if Kreuzberg is available
	kreuzbergURL := os.Getenv("KREUZBERG_URL")
	if kreuzbergURL == "" {
		kreuzbergURL = "http://localhost:8787"
	}

	// Create a minimal config for Kreuzberg client
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

	// Extract text
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

// TestEntityOnlySchemaValidation verifies the schema is valid JSON.
func TestEntityOnlySchemaValidation(t *testing.T) {
	schema := getEntityOnlySchema()

	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(schema), &parsed)
	require.NoError(t, err, "Schema should be valid JSON")

	assert.Equal(t, "ProtocolDocument", parsed["title"])
	assert.Contains(t, parsed, "properties")
	assert.Contains(t, parsed, "required")
}
