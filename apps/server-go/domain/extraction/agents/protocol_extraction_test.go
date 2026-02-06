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

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/adk"
	"github.com/emergent/emergent-core/pkg/kreuzberg"
)

// ProtocolGroundTruth represents the ground truth data from doc-processing-suite.
type ProtocolGroundTruth struct {
	DocumentID      string                   `json:"documentId"`
	Category        string                   `json:"category"`
	Generator       string                   `json:"generator"`
	TemplateVariant string                   `json:"templateVariant"`
	GeneratedAt     string                   `json:"generatedAt"`
	PDFPath         string                   `json:"pdfPath"`
	PDFHash         string                   `json:"pdfHash"`
	InputData       ProtocolGroundTruthInput `json:"inputData"`
}

// ProtocolGroundTruthInput is the actual extracted data we expect.
type ProtocolGroundTruthInput struct {
	DocumentID      string                  `json:"documentId"`
	Category        string                  `json:"category"`
	TemplateVariant string                  `json:"templateVariant"`
	MeetingType     string                  `json:"meetingType"`
	MeetingNumber   string                  `json:"meetingNumber"`
	Date            string                  `json:"date"`
	StartTime       string                  `json:"startTime"`
	EndTime         string                  `json:"endTime"`
	Location        string                  `json:"location"`
	Chairman        GroundTruthPerson       `json:"chairman"`
	Secretary       GroundTruthPerson       `json:"secretary"`
	Attendees       []GroundTruthPerson     `json:"attendees"`
	Absentees       []GroundTruthPerson     `json:"absentees"`
	AgendaItems     []string                `json:"agendaItems"`
	Resolutions     []GroundTruthResolution `json:"resolutions"`
	Notes           string                  `json:"notes"`
	Signatures      []GroundTruthSignature  `json:"signatures"`
}

// GroundTruthPerson represents a person in the ground truth.
type GroundTruthPerson struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Email string `json:"email"`
}

// GroundTruthResolution represents a resolution in the ground truth.
type GroundTruthResolution struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	VotesFor     int    `json:"votesFor"`
	VotesAgainst int    `json:"votesAgainst"`
	Abstentions  int    `json:"abstentions"`
	Passed       bool   `json:"passed"`
}

// GroundTruthSignature represents a signature in the ground truth.
type GroundTruthSignature struct {
	Role       string `json:"role"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	SignedDate string `json:"signedDate"`
}

// getProtocolSchemas returns object and relationship schemas for protocol extraction.
func getProtocolSchemas() (map[string]ObjectSchema, map[string]RelationshipSchema) {
	objectSchemas := map[string]ObjectSchema{
		"MeetingProtocol": {
			Name:        "MeetingProtocol",
			Description: "A formal meeting protocol document recording decisions, attendance, and resolutions",
			Properties: map[string]PropertyDef{
				"meeting_type":   {Type: "string", Description: "Type of meeting (board_meeting, shareholder_meeting, annual_general_meeting, extraordinary_general_meeting)"},
				"meeting_number": {Type: "string", Description: "Meeting reference number (e.g., '2026-4')"},
				"date":           {Type: "string", Description: "Date of the meeting (YYYY-MM-DD)"},
				"start_time":     {Type: "string", Description: "Start time of the meeting (HH:MM)"},
				"end_time":       {Type: "string", Description: "End time of the meeting (HH:MM)"},
				"notes":          {Type: "string", Description: "Additional notes from the meeting"},
			},
			Required:             []string{"date", "meeting_type"},
			ExtractionGuidelines: "Extract the main meeting protocol as a single entity. Look for title, date, time, and meeting type in the header.",
		},
		"Person": {
			Name:        "Person",
			Description: "A person involved in the meeting (chairman, secretary, attendee, or absentee)",
			Properties: map[string]PropertyDef{
				"title":        {Type: "string", Description: "Job title or role of the person"},
				"email":        {Type: "string", Description: "Email address if provided"},
				"meeting_role": {Type: "string", Description: "Role in the meeting (chairman, secretary, attendee, absentee)"},
			},
			ExtractionGuidelines: "Extract all people mentioned: chairman, secretary, attendees, and absentees. Include their job titles.",
		},
		"Location": {
			Name:        "Location",
			Description: "The location where the meeting was held",
			Properties: map[string]PropertyDef{
				"address":       {Type: "string", Description: "Full address if provided"},
				"location_type": {Type: "string", Description: "Type: headquarters, office, virtual, etc."},
			},
			ExtractionGuidelines: "Extract the meeting location. Look for venue, address, or virtual meeting info.",
		},
		"AgendaItem": {
			Name:        "AgendaItem",
			Description: "An item on the meeting agenda",
			Properties: map[string]PropertyDef{
				"item_number": {Type: "string", Description: "Agenda item number"},
				"description": {Type: "string", Description: "Full description of the agenda item"},
			},
			ExtractionGuidelines: "Extract each agenda item discussed in the meeting. Usually numbered (1., 2., etc.).",
		},
		"Resolution": {
			Name:        "Resolution",
			Description: "A formal resolution voted on during the meeting",
			Properties: map[string]PropertyDef{
				"resolution_number": {Type: "integer", Description: "Resolution number"},
				"description":       {Type: "string", Description: "Full description of the resolution"},
				"votes_for":         {Type: "integer", Description: "Number of votes in favor"},
				"votes_against":     {Type: "integer", Description: "Number of votes against"},
				"abstentions":       {Type: "integer", Description: "Number of abstentions"},
				"passed":            {Type: "boolean", Description: "Whether the resolution passed"},
			},
			Required:             []string{"resolution_number"},
			ExtractionGuidelines: "Extract all resolutions with their voting results. Look for 'Resolution', 'Vedtak', or numbered decisions.",
		},
	}

	relationshipSchemas := map[string]RelationshipSchema{
		"CHAIRED": {
			Name:        "CHAIRED",
			Description: "Person served as chairman of the meeting",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"MeetingProtocol"},
		},
		"SECRETARY_OF": {
			Name:        "SECRETARY_OF",
			Description: "Person served as secretary of the meeting",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"MeetingProtocol"},
		},
		"ATTENDED": {
			Name:        "ATTENDED",
			Description: "Person attended the meeting",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"MeetingProtocol"},
		},
		"ABSENT_FROM": {
			Name:        "ABSENT_FROM",
			Description: "Person was absent from the meeting",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"MeetingProtocol"},
		},
		"HELD_AT": {
			Name:        "HELD_AT",
			Description: "Meeting was held at a location",
			SourceTypes: []string{"MeetingProtocol"},
			TargetTypes: []string{"Location"},
		},
		"HAD_AGENDA": {
			Name:        "HAD_AGENDA",
			Description: "Meeting included this agenda item",
			SourceTypes: []string{"MeetingProtocol"},
			TargetTypes: []string{"AgendaItem"},
		},
		"VOTED_ON": {
			Name:        "VOTED_ON",
			Description: "Meeting voted on this resolution",
			SourceTypes: []string{"MeetingProtocol"},
			TargetTypes: []string{"Resolution"},
		},
	}

	return objectSchemas, relationshipSchemas
}

// ExtractionMetrics holds comparison metrics between extracted and expected data.
type ExtractionMetrics struct {
	// Entity metrics
	ExpectedEntityCount  int
	ExtractedEntityCount int
	MatchedEntities      int
	MissedEntities       []string
	ExtraEntities        []string
	EntityPrecision      float64
	EntityRecall         float64

	// Relationship metrics
	ExpectedRelCount  int
	ExtractedRelCount int
	OrphanRate        float64

	// Field-level metrics for key fields
	FieldMatches map[string]bool
	FieldDetails map[string]string
}

// calculateMetrics compares extraction results against ground truth.
func calculateMetrics(
	extracted *ExtractionPipelineOutput,
	groundTruth *ProtocolGroundTruthInput,
) *ExtractionMetrics {
	metrics := &ExtractionMetrics{
		FieldMatches: make(map[string]bool),
		FieldDetails: make(map[string]string),
	}

	// Count expected entities from ground truth
	// 1 protocol + 1 chairman + 1 secretary + N attendees + N absentees + N agenda items + N resolutions + 1 location
	metrics.ExpectedEntityCount = 1 + // protocol
		1 + // chairman
		1 + // secretary
		len(groundTruth.Attendees) +
		len(groundTruth.Absentees) +
		len(groundTruth.AgendaItems) +
		len(groundTruth.Resolutions) +
		1 // location

	metrics.ExtractedEntityCount = len(extracted.Entities)
	metrics.ExtractedRelCount = len(extracted.Relationships)

	// Calculate orphan rate
	metrics.OrphanRate = CalculateOrphanRate(extracted.Entities, extracted.Relationships)

	// Expected relationships:
	// - 1 CHAIRED (chairman -> protocol)
	// - 1 SECRETARY_OF (secretary -> protocol)
	// - N ATTENDED (attendees -> protocol)
	// - N ABSENT_FROM (absentees -> protocol)
	// - 1 HELD_AT (protocol -> location)
	// - N HAD_AGENDA (protocol -> agenda items)
	// - N VOTED_ON (protocol -> resolutions)
	metrics.ExpectedRelCount = 1 + 1 +
		len(groundTruth.Attendees) +
		len(groundTruth.Absentees) +
		1 +
		len(groundTruth.AgendaItems) +
		len(groundTruth.Resolutions)

	// Build lookup maps for extracted entities
	extractedByType := make(map[string][]InternalEntity)
	extractedNames := make(map[string]bool)
	for _, e := range extracted.Entities {
		extractedByType[e.Type] = append(extractedByType[e.Type], e)
		extractedNames[strings.ToLower(e.Name)] = true
	}

	// Check for expected people
	expectedPeople := []string{groundTruth.Chairman.Name, groundTruth.Secretary.Name}
	for _, a := range groundTruth.Attendees {
		expectedPeople = append(expectedPeople, a.Name)
	}
	for _, a := range groundTruth.Absentees {
		expectedPeople = append(expectedPeople, a.Name)
	}

	for _, name := range expectedPeople {
		found := false
		for extractedName := range extractedNames {
			if strings.Contains(extractedName, strings.ToLower(name)) ||
				strings.Contains(strings.ToLower(name), extractedName) {
				found = true
				metrics.MatchedEntities++
				break
			}
		}
		if !found {
			metrics.MissedEntities = append(metrics.MissedEntities, name)
		}
	}

	// Check for protocol entity
	protocols := extractedByType["MeetingProtocol"]
	if len(protocols) > 0 {
		metrics.MatchedEntities++
		protocol := protocols[0]

		// Check protocol properties
		if props := protocol.Properties; props != nil {
			if date, ok := props["date"].(string); ok {
				metrics.FieldMatches["date"] = date == groundTruth.Date
				metrics.FieldDetails["date"] = fmt.Sprintf("expected=%s, got=%s", groundTruth.Date, date)
			}
			if meetingType, ok := props["meeting_type"].(string); ok {
				matches := strings.Contains(strings.ToLower(meetingType), strings.ToLower(groundTruth.MeetingType)) ||
					strings.Contains(strings.ToLower(groundTruth.MeetingType), strings.ToLower(meetingType))
				metrics.FieldMatches["meeting_type"] = matches
				metrics.FieldDetails["meeting_type"] = fmt.Sprintf("expected=%s, got=%s", groundTruth.MeetingType, meetingType)
			}
		}
	} else {
		metrics.MissedEntities = append(metrics.MissedEntities, "MeetingProtocol")
	}

	// Check for location
	locations := extractedByType["Location"]
	if len(locations) > 0 {
		metrics.MatchedEntities++
		loc := locations[0]
		locName := strings.ToLower(loc.Name)
		expectedLoc := strings.ToLower(groundTruth.Location)
		metrics.FieldMatches["location"] = strings.Contains(locName, expectedLoc[:min(20, len(expectedLoc))]) ||
			strings.Contains(expectedLoc, locName[:min(20, len(locName))])
		metrics.FieldDetails["location"] = fmt.Sprintf("expected=%s, got=%s", groundTruth.Location, loc.Name)
	} else {
		metrics.MissedEntities = append(metrics.MissedEntities, "Location: "+groundTruth.Location)
	}

	// Check for resolutions
	resolutions := extractedByType["Resolution"]
	metrics.FieldMatches["resolutions_count"] = len(resolutions) == len(groundTruth.Resolutions)
	metrics.FieldDetails["resolutions_count"] = fmt.Sprintf("expected=%d, got=%d",
		len(groundTruth.Resolutions), len(resolutions))

	// Calculate precision and recall
	if metrics.ExtractedEntityCount > 0 {
		metrics.EntityPrecision = float64(metrics.MatchedEntities) / float64(metrics.ExtractedEntityCount)
	}
	if metrics.ExpectedEntityCount > 0 {
		metrics.EntityRecall = float64(metrics.MatchedEntities) / float64(metrics.ExpectedEntityCount)
	}

	return metrics
}

// loadGroundTruth loads a ground truth JSON file.
func loadGroundTruth(path string) (*ProtocolGroundTruth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read ground truth file: %w", err)
	}

	var gt ProtocolGroundTruth
	if err := json.Unmarshal(data, &gt); err != nil {
		return nil, fmt.Errorf("failed to parse ground truth JSON: %w", err)
	}

	return &gt, nil
}

// TestProtocolExtractionE2E tests extraction against doc-processing-suite ground truth.
// This test requires:
// - VERTEX_PROJECT_ID environment variable
// - Kreuzberg service running
// - PDF files from doc-processing-suite in expected location
func TestProtocolExtractionE2E(t *testing.T) {
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
	documentText, err := extractTextFromPDF(ctx, t, pdfPath)
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

	// Create model factory
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	modelFactory := adk.NewModelFactory(llmConfig, logger)

	// Create trace logger
	traceLogger, err := NewExtractionTraceLogger(TraceLoggerConfig{
		JobID:     "test_protocol_extraction",
		ProjectID: projectID,
		LogDir:    "../../../../logs/extractions",
	})
	require.NoError(t, err, "Failed to create trace logger")
	defer func() {
		logPath := traceLogger.LogFilePath()
		traceLogger.Close()
		t.Logf("\n=== TRACE LOG FILE ===\n%s\n======================", logPath)
	}()

	// Get schemas
	objectSchemas, relationshipSchemas := getProtocolSchemas()

	traceLogger.LogSchemas(objectSchemas, relationshipSchemas)
	traceLogger.LogDocumentText(documentText)

	// Create extraction pipeline
	pipeline, err := NewExtractionPipeline(ExtractionPipelineConfig{
		ModelFactory:    modelFactory,
		OrphanThreshold: 0.3,
		MaxRetries:      3,
		Logger:          logger,
		TraceLogger:     traceLogger,
	})
	require.NoError(t, err)

	// Run extraction
	result, err := pipeline.Run(ctx, ExtractionPipelineInput{
		DocumentText:        documentText,
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relationshipSchemas,
	})
	require.NoError(t, err)

	traceLogger.LogEntities(result.Entities)
	traceLogger.LogRelationships(result.Relationships)

	// Calculate metrics
	metrics := calculateMetrics(result, &groundTruth.InputData)

	// Log results
	t.Logf("\n=== EXTRACTION RESULTS ===")
	t.Logf("Entities: expected=%d, extracted=%d, matched=%d",
		metrics.ExpectedEntityCount, metrics.ExtractedEntityCount, metrics.MatchedEntities)
	t.Logf("Relationships: expected=%d, extracted=%d",
		metrics.ExpectedRelCount, metrics.ExtractedRelCount)
	t.Logf("Orphan rate: %.2f%%", metrics.OrphanRate*100)
	t.Logf("Entity Precision: %.2f%%", metrics.EntityPrecision*100)
	t.Logf("Entity Recall: %.2f%%", metrics.EntityRecall*100)

	t.Logf("\n=== FIELD MATCHES ===")
	for field, matched := range metrics.FieldMatches {
		status := "PASS"
		if !matched {
			status = "FAIL"
		}
		t.Logf("  %s: %s (%s)", field, status, metrics.FieldDetails[field])
	}

	if len(metrics.MissedEntities) > 0 {
		t.Logf("\n=== MISSED ENTITIES ===")
		for _, name := range metrics.MissedEntities {
			t.Logf("  - %s", name)
		}
	}

	t.Logf("\n=== EXTRACTED ENTITIES ===")
	for _, e := range result.Entities {
		t.Logf("  [%s] %s (%s)", e.TempID, e.Name, e.Type)
		if len(e.Properties) > 0 {
			propsJSON, _ := json.Marshal(e.Properties)
			t.Logf("    Properties: %s", string(propsJSON))
		}
	}

	t.Logf("\n=== EXTRACTED RELATIONSHIPS ===")
	for _, r := range result.Relationships {
		t.Logf("  %s -[%s]-> %s", r.SourceRef, r.Type, r.TargetRef)
	}

	// Assertions
	assert.GreaterOrEqual(t, len(result.Entities), 5, "Expected at least 5 entities")
	assert.GreaterOrEqual(t, len(result.Relationships), 3, "Expected at least 3 relationships")
	assert.LessOrEqual(t, metrics.OrphanRate, 0.5, "Orphan rate should be below 50%")

	// Check for key entity types
	entityTypes := make(map[string]int)
	for _, e := range result.Entities {
		entityTypes[e.Type]++
	}
	assert.Contains(t, entityTypes, "MeetingProtocol", "Should extract MeetingProtocol entity")
	assert.Contains(t, entityTypes, "Person", "Should extract Person entities")

	// Check that we found the chairman
	foundChairman := false
	chairmanName := strings.ToLower(groundTruth.InputData.Chairman.Name)
	for _, e := range result.Entities {
		if e.Type == "Person" && strings.Contains(strings.ToLower(e.Name), chairmanName[:min(10, len(chairmanName))]) {
			foundChairman = true
			break
		}
	}
	assert.True(t, foundChairman, "Should find chairman: %s", groundTruth.InputData.Chairman.Name)
}

// extractTextFromPDF extracts text from a PDF using Kreuzberg.
func extractTextFromPDF(ctx context.Context, t *testing.T, pdfPath string) (string, error) {
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
			TimeoutMs:  60000, // 60 seconds in milliseconds
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client := kreuzberg.NewClient(cfg, logger)

	if !client.IsEnabled() {
		// Fallback: return a message indicating Kreuzberg is not available
		t.Log("Kreuzberg not enabled, using fallback document text")
		return getFallbackProtocolText(), nil
	}

	// Extract text
	result, err := client.ExtractText(ctx, pdfData, filepath.Base(pdfPath), "application/pdf", nil)
	if err != nil {
		// Check if it's a connection error
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "unavailable") {
			t.Log("Kreuzberg service not available, using fallback document text")
			return getFallbackProtocolText(), nil
		}
		return "", fmt.Errorf("Kreuzberg extraction failed: %w", err)
	}

	return result.Content, nil
}

// getFallbackProtocolText returns synthetic protocol text matching ground truth structure.
// This is used when Kreuzberg is not available for testing.
func getFallbackProtocolText() string {
	return `MEETING PROTOCOL
Extraordinary General Meeting

Meeting Number: 2026-4
Date: 2025-05-22
Time: 12:00 - 15:00
Location: Schowalter LLC Headquarters, East Adolfoport

ATTENDEES
---------
Name                           | Title
-------------------------------|------------------------------------------
Joseph Hand I                  | International Data Specialist (Chairman)
Rachael Beier                  | Lead Web Manager (Secretary)
Charlotte Bernhard             | Chief Group Producer
Ida Roberts                    | Corporate Paradigm Manager
Bernard Keebler                | Lead Communications Orchestrator
Milton Hane                    | Investor Program Representative
Veronica Schiller              | Investor Factors Architect
Jim Schumm                     | Customer Applications Engineer
Mrs. Lora Friesen              | Customer Functionality Developer

ABSENT
------
Olive Casper                   | Global Accountability Producer

AGENDA
------
1. deploy virtual convergence
2. enable scalable solutions
3. extend best-of-breed methodologies

RESOLUTIONS
-----------
Resolution 1: implement decentralized AI
Tabella verecundia taedium vestigium illum absconditus quam artificiose. 
Alter vulticulus conturbo strues. Vesper bonus ventus sordeo tibi cado.

Votes For: 6
Votes Against: 0
Abstentions: 1
PASSED

Resolution 2: deliver innovative communities
Terga calco auctus theatrum cursus video nihil attollo voluptate combibo. 
Tres apparatus temporibus decerno damno cavus via crinis amaritudo. Asporto arguo abduco.

Votes For: 3
Votes Against: 4
Abstentions: 0
REJECTED

ADDITIONAL NOTES
----------------
Aedificium utilis vae conturbo ullus suus solum causa cura. Corpus tripudio barba. 
Desolo curiositas sublime quas derideo demo eaque vicissitudo.

SIGNATURES
----------
Chairman: Joseph Hand I (International Data Specialist) - Signed: 2025-05-22
Secretary: Rachael Beier (Lead Web Manager) - Signed: 2025-05-22
`
}

// TestProtocolSchemas verifies the schema definitions are valid.
func TestProtocolSchemas(t *testing.T) {
	objectSchemas, relationshipSchemas := getProtocolSchemas()

	// Verify object schemas
	assert.Len(t, objectSchemas, 5, "Should have 5 object schemas")
	assert.Contains(t, objectSchemas, "MeetingProtocol")
	assert.Contains(t, objectSchemas, "Person")
	assert.Contains(t, objectSchemas, "Location")
	assert.Contains(t, objectSchemas, "AgendaItem")
	assert.Contains(t, objectSchemas, "Resolution")

	// Verify MeetingProtocol schema
	protocol := objectSchemas["MeetingProtocol"]
	assert.NotEmpty(t, protocol.Description)
	assert.Contains(t, protocol.Properties, "meeting_type")
	assert.Contains(t, protocol.Properties, "date")
	assert.Contains(t, protocol.Required, "date")

	// Verify Resolution schema
	resolution := objectSchemas["Resolution"]
	assert.Contains(t, resolution.Properties, "votes_for")
	assert.Contains(t, resolution.Properties, "votes_against")
	assert.Contains(t, resolution.Properties, "passed")

	// Verify relationship schemas
	assert.Len(t, relationshipSchemas, 7, "Should have 7 relationship schemas")
	assert.Contains(t, relationshipSchemas, "CHAIRED")
	assert.Contains(t, relationshipSchemas, "SECRETARY_OF")
	assert.Contains(t, relationshipSchemas, "ATTENDED")
	assert.Contains(t, relationshipSchemas, "ABSENT_FROM")
	assert.Contains(t, relationshipSchemas, "HELD_AT")
	assert.Contains(t, relationshipSchemas, "HAD_AGENDA")
	assert.Contains(t, relationshipSchemas, "VOTED_ON")

	// Verify relationship type constraints
	chaired := relationshipSchemas["CHAIRED"]
	assert.Contains(t, chaired.SourceTypes, "Person")
	assert.Contains(t, chaired.TargetTypes, "MeetingProtocol")
}

// TestProtocolFallbackDocument verifies the fallback document contains expected content.
func TestProtocolFallbackDocument(t *testing.T) {
	doc := getFallbackProtocolText()

	// Verify document contains key elements
	assert.Contains(t, doc, "MEETING PROTOCOL")
	assert.Contains(t, doc, "Extraordinary General Meeting")
	assert.Contains(t, doc, "2025-05-22")
	assert.Contains(t, doc, "Joseph Hand I")
	assert.Contains(t, doc, "Rachael Beier")
	assert.Contains(t, doc, "Resolution 1")
	assert.Contains(t, doc, "Resolution 2")
	assert.Contains(t, doc, "PASSED")
	assert.Contains(t, doc, "REJECTED")
	assert.Contains(t, doc, "Schowalter LLC Headquarters")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
