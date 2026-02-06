package agents

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/adk"
)

// MeetingGroundTruth defines the expected entities and relationships for testing.
type MeetingGroundTruth struct {
	Entities      []ExpectedEntity
	Relationships []ExpectedRelationship
}

type ExpectedEntity struct {
	Name       string
	Type       string
	Properties map[string]any
}

type ExpectedRelationship struct {
	SourceName string
	TargetName string
	Type       string
}

// getMeetingSchemas returns object and relationship schemas for meeting extraction.
func getMeetingSchemas() (map[string]ObjectSchema, map[string]RelationshipSchema) {
	objectSchemas := map[string]ObjectSchema{
		"Meeting": {
			Name:        "Meeting",
			Description: "A business meeting or gathering where people discuss topics and make decisions",
			Properties: map[string]PropertyDef{
				"title":        {Type: "string", Description: "The title or subject of the meeting"},
				"date":         {Type: "string", Description: "The date of the meeting (YYYY-MM-DD format)"},
				"start_time":   {Type: "string", Description: "Start time of the meeting (HH:MM format)"},
				"end_time":     {Type: "string", Description: "End time of the meeting (HH:MM format)"},
				"duration":     {Type: "string", Description: "Duration of the meeting (e.g., '1 hour', '90 minutes')"},
				"meeting_type": {Type: "string", Description: "Type of meeting (e.g., 'standup', 'planning', 'retrospective', 'one-on-one')"},
			},
			Required:             []string{"title", "date"},
			ExtractionGuidelines: "Extract the main meeting as a single entity. Look for meeting title in headers or subject lines.",
		},
		"Person": {
			Name:        "Person",
			Description: "A person who attended, organized, or was mentioned in the meeting",
			Properties: map[string]PropertyDef{
				"role":       {Type: "string", Description: "Role in the meeting (e.g., 'organizer', 'attendee', 'presenter')"},
				"department": {Type: "string", Description: "Department or team the person belongs to"},
				"title":      {Type: "string", Description: "Job title of the person"},
			},
			ExtractionGuidelines: "Extract all people mentioned as attendees, organizers, or assigned to action items.",
		},
		"Location": {
			Name:        "Location",
			Description: "Physical or virtual location where the meeting takes place",
			Properties: map[string]PropertyDef{
				"location_type": {Type: "string", Description: "Type of location (e.g., 'conference_room', 'virtual', 'office')"},
				"address":       {Type: "string", Description: "Physical address if applicable"},
				"meeting_link":  {Type: "string", Description: "Virtual meeting URL if applicable"},
			},
			ExtractionGuidelines: "Extract meeting rooms, virtual platforms (Zoom, Teams), or physical locations.",
		},
		"ActionItem": {
			Name:        "ActionItem",
			Description: "A task or action that needs to be completed after the meeting",
			Properties: map[string]PropertyDef{
				"description": {Type: "string", Description: "What needs to be done"},
				"due_date":    {Type: "string", Description: "When the action item is due"},
				"priority":    {Type: "string", Description: "Priority level (high, medium, low)"},
				"status":      {Type: "string", Description: "Current status (pending, in_progress, completed)"},
			},
			Required:             []string{"description"},
			ExtractionGuidelines: "Extract all action items, tasks, or follow-ups mentioned in the meeting notes.",
		},
		"AgendaItem": {
			Name:        "AgendaItem",
			Description: "A topic or item on the meeting agenda",
			Properties: map[string]PropertyDef{
				"topic":    {Type: "string", Description: "The agenda topic"},
				"duration": {Type: "string", Description: "Allocated time for this topic"},
				"outcome":  {Type: "string", Description: "Decision or outcome from discussing this topic"},
			},
			ExtractionGuidelines: "Extract agenda items, discussion topics, and their outcomes.",
		},
		"Decision": {
			Name:        "Decision",
			Description: "A decision made during the meeting",
			Properties: map[string]PropertyDef{
				"description": {Type: "string", Description: "What was decided"},
				"rationale":   {Type: "string", Description: "Why this decision was made"},
			},
			ExtractionGuidelines: "Extract any decisions, agreements, or conclusions reached during the meeting.",
		},
	}

	relationshipSchemas := map[string]RelationshipSchema{
		"ATTENDED": {
			Name:        "ATTENDED",
			Description: "Person attended the meeting",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Meeting"},
		},
		"ORGANIZED": {
			Name:        "ORGANIZED",
			Description: "Person organized or led the meeting",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"Meeting"},
		},
		"HELD_AT": {
			Name:        "HELD_AT",
			Description: "Meeting was held at a location",
			SourceTypes: []string{"Meeting"},
			TargetTypes: []string{"Location"},
		},
		"ASSIGNED_TO": {
			Name:        "ASSIGNED_TO",
			Description: "Action item is assigned to a person",
			SourceTypes: []string{"ActionItem"},
			TargetTypes: []string{"Person"},
		},
		"RESULTED_FROM": {
			Name:        "RESULTED_FROM",
			Description: "Action item or decision resulted from the meeting",
			SourceTypes: []string{"ActionItem", "Decision"},
			TargetTypes: []string{"Meeting"},
		},
		"DISCUSSED_IN": {
			Name:        "DISCUSSED_IN",
			Description: "Agenda item was discussed in the meeting",
			SourceTypes: []string{"AgendaItem"},
			TargetTypes: []string{"Meeting"},
		},
		"PRESENTED": {
			Name:        "PRESENTED",
			Description: "Person presented an agenda item",
			SourceTypes: []string{"Person"},
			TargetTypes: []string{"AgendaItem"},
		},
		"MADE_DECISION": {
			Name:        "MADE_DECISION",
			Description: "Meeting resulted in a decision",
			SourceTypes: []string{"Meeting"},
			TargetTypes: []string{"Decision"},
		},
	}

	return objectSchemas, relationshipSchemas
}

// getMeetingGroundTruth returns the expected entities and relationships.
func getMeetingGroundTruth() MeetingGroundTruth {
	return MeetingGroundTruth{
		Entities: []ExpectedEntity{
			{
				Name: "Q1 Planning Meeting",
				Type: "Meeting",
				Properties: map[string]any{
					"date":         "2026-02-10",
					"start_time":   "10:00",
					"end_time":     "11:30",
					"duration":     "90 minutes",
					"meeting_type": "planning",
				},
			},
			{
				Name: "Sarah Johnson",
				Type: "Person",
				Properties: map[string]any{
					"role":       "organizer",
					"department": "Engineering",
					"title":      "Engineering Manager",
				},
			},
			{
				Name: "Michael Chen",
				Type: "Person",
				Properties: map[string]any{
					"role":       "attendee",
					"department": "Engineering",
					"title":      "Senior Developer",
				},
			},
			{
				Name: "Emily Rodriguez",
				Type: "Person",
				Properties: map[string]any{
					"role":       "attendee",
					"department": "Product",
					"title":      "Product Manager",
				},
			},
			{
				Name: "Conference Room A",
				Type: "Location",
				Properties: map[string]any{
					"location_type": "conference_room",
				},
			},
			{
				Name: "Sprint velocity review",
				Type: "AgendaItem",
				Properties: map[string]any{
					"duration": "20 minutes",
					"outcome":  "Team velocity stable at 42 points",
				},
			},
			{
				Name: "Feature prioritization",
				Type: "AgendaItem",
				Properties: map[string]any{
					"duration": "40 minutes",
					"outcome":  "Decided to prioritize user authentication",
				},
			},
			{
				Name: "Resource allocation",
				Type: "AgendaItem",
				Properties: map[string]any{
					"duration": "30 minutes",
				},
			},
			{
				Name: "Set up authentication service",
				Type: "ActionItem",
				Properties: map[string]any{
					"due_date": "2026-02-17",
					"priority": "high",
				},
			},
			{
				Name: "Create user stories for login flow",
				Type: "ActionItem",
				Properties: map[string]any{
					"due_date": "2026-02-14",
					"priority": "high",
				},
			},
			{
				Name: "Review infrastructure costs",
				Type: "ActionItem",
				Properties: map[string]any{
					"due_date": "2026-02-12",
					"priority": "medium",
				},
			},
			{
				Name: "Prioritize user authentication feature",
				Type: "Decision",
				Properties: map[string]any{
					"rationale": "Critical for Q1 launch",
				},
			},
		},
		Relationships: []ExpectedRelationship{
			{SourceName: "Sarah Johnson", TargetName: "Q1 Planning Meeting", Type: "ORGANIZED"},
			{SourceName: "Michael Chen", TargetName: "Q1 Planning Meeting", Type: "ATTENDED"},
			{SourceName: "Emily Rodriguez", TargetName: "Q1 Planning Meeting", Type: "ATTENDED"},
			{SourceName: "Sarah Johnson", TargetName: "Q1 Planning Meeting", Type: "ATTENDED"},
			{SourceName: "Q1 Planning Meeting", TargetName: "Conference Room A", Type: "HELD_AT"},
			{SourceName: "Sprint velocity review", TargetName: "Q1 Planning Meeting", Type: "DISCUSSED_IN"},
			{SourceName: "Feature prioritization", TargetName: "Q1 Planning Meeting", Type: "DISCUSSED_IN"},
			{SourceName: "Resource allocation", TargetName: "Q1 Planning Meeting", Type: "DISCUSSED_IN"},
			{SourceName: "Set up authentication service", TargetName: "Michael Chen", Type: "ASSIGNED_TO"},
			{SourceName: "Create user stories for login flow", TargetName: "Emily Rodriguez", Type: "ASSIGNED_TO"},
			{SourceName: "Review infrastructure costs", TargetName: "Sarah Johnson", Type: "ASSIGNED_TO"},
			{SourceName: "Set up authentication service", TargetName: "Q1 Planning Meeting", Type: "RESULTED_FROM"},
			{SourceName: "Create user stories for login flow", TargetName: "Q1 Planning Meeting", Type: "RESULTED_FROM"},
			{SourceName: "Review infrastructure costs", TargetName: "Q1 Planning Meeting", Type: "RESULTED_FROM"},
			{SourceName: "Q1 Planning Meeting", TargetName: "Prioritize user authentication feature", Type: "MADE_DECISION"},
			{SourceName: "Sarah Johnson", TargetName: "Sprint velocity review", Type: "PRESENTED"},
			{SourceName: "Emily Rodriguez", TargetName: "Feature prioritization", Type: "PRESENTED"},
		},
	}
}

// getSyntheticMeetingDocument returns a natural language meeting notes document.
func getSyntheticMeetingDocument() string {
	return `# Q1 Planning Meeting

**Date:** February 10, 2026
**Time:** 10:00 AM - 11:30 AM (90 minutes)
**Location:** Conference Room A
**Meeting Type:** Planning

## Attendees

- **Sarah Johnson** (Engineering Manager, Organizer) - Engineering Department
- **Michael Chen** (Senior Developer) - Engineering Department  
- **Emily Rodriguez** (Product Manager) - Product Department

## Agenda

### 1. Sprint velocity review (20 minutes)
Presented by Sarah Johnson.

We reviewed the team's velocity over the past 3 sprints. The team velocity has been stable at 42 story points per sprint. No major blockers identified.

**Outcome:** Team velocity stable at 42 points.

### 2. Feature prioritization (40 minutes)
Presented by Emily Rodriguez.

Emily walked through the product roadmap and upcoming features. After discussion, the team agreed that user authentication should be the top priority for Q1.

**Outcome:** Decided to prioritize user authentication.
**Decision:** Prioritize user authentication feature for Q1 launch. This is critical for the Q1 launch timeline.

### 3. Resource allocation (30 minutes)

Discussed how to allocate engineering resources for the upcoming sprint. Michael will lead the authentication implementation.

## Action Items

1. **Set up authentication service** - Assigned to Michael Chen
   - Due: February 17, 2026
   - Priority: High

2. **Create user stories for login flow** - Assigned to Emily Rodriguez
   - Due: February 14, 2026
   - Priority: High

3. **Review infrastructure costs** - Assigned to Sarah Johnson
   - Due: February 12, 2026
   - Priority: Medium

## Next Meeting

Follow-up meeting scheduled for February 17, 2026.
`
}

// TestMeetingExtractionE2E tests the full extraction pipeline with a synthetic meeting document.
// This test requires VERTEX_PROJECT_ID environment variable to be set.
func TestMeetingExtractionE2E(t *testing.T) {
	projectID := os.Getenv("VERTEX_PROJECT_ID")
	if projectID == "" {
		t.Skip("VERTEX_PROJECT_ID not set, skipping E2E test")
	}

	ctx := context.Background()

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

	traceLogger, err := NewExtractionTraceLogger(TraceLoggerConfig{
		JobID:     "test_meeting_extraction",
		ProjectID: projectID,
		LogDir:    "../../../../logs/extractions",
	})
	require.NoError(t, err, "Failed to create trace logger")
	defer func() {
		logPath := traceLogger.LogFilePath()
		traceLogger.Close()
		t.Logf("\n=== TRACE LOG FILE ===\n%s\n======================", logPath)
	}()

	// Get schemas and ground truth
	objectSchemas, relationshipSchemas := getMeetingSchemas()
	groundTruth := getMeetingGroundTruth()
	document := getSyntheticMeetingDocument()

	traceLogger.LogSchemas(objectSchemas, relationshipSchemas)
	traceLogger.LogDocumentText(document)

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
		DocumentText:        document,
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relationshipSchemas,
	})
	require.NoError(t, err)

	traceLogger.LogEntities(result.Entities)
	traceLogger.LogRelationships(result.Relationships)

	t.Logf("Extracted %d entities and %d relationships", len(result.Entities), len(result.Relationships))
	t.Logf("Ground truth: %d entities and %d relationships", len(groundTruth.Entities), len(groundTruth.Relationships))

	// Check that we got a reasonable number of entities
	assert.GreaterOrEqual(t, len(result.Entities), 8, "Expected at least 8 entities")
	assert.GreaterOrEqual(t, len(result.Relationships), 10, "Expected at least 10 relationships")

	// Verify key entity types are present
	entityTypes := make(map[string]int)
	for _, e := range result.Entities {
		entityTypes[e.Type]++
		t.Logf("Entity: %s (%s)", e.Name, e.Type)
	}

	assert.Contains(t, entityTypes, "Meeting", "Should extract Meeting entity")
	assert.Contains(t, entityTypes, "Person", "Should extract Person entities")
	assert.Contains(t, entityTypes, "ActionItem", "Should extract ActionItem entities")

	// Verify specific entities by name matching
	entityNames := make(map[string]bool)
	for _, e := range result.Entities {
		entityNames[e.Name] = true
	}

	// Check for key entities from ground truth
	expectedNames := []string{
		"Sarah Johnson",
		"Michael Chen",
		"Emily Rodriguez",
	}
	for _, name := range expectedNames {
		found := false
		for entityName := range entityNames {
			if containsIgnoreCase(entityName, name) || containsIgnoreCase(name, entityName) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find entity containing: %s", name)
	}

	// Verify relationships
	relTypes := make(map[string]int)
	for _, r := range result.Relationships {
		relTypes[r.Type]++
		t.Logf("Relationship: %s -[%s]-> %s", r.SourceRef, r.Type, r.TargetRef)
	}

	// Check for key relationship types
	assert.Contains(t, relTypes, "ATTENDED", "Should have ATTENDED relationships")
	assert.Contains(t, relTypes, "ASSIGNED_TO", "Should have ASSIGNED_TO relationships")

	// Verify orphan rate is acceptable
	orphanRate := CalculateOrphanRate(result.Entities, result.Relationships)
	t.Logf("Orphan rate: %.2f%%", orphanRate*100)
	assert.LessOrEqual(t, orphanRate, 0.5, "Orphan rate should be below 50%")
}

// TestMeetingSchemas verifies the schema definitions are valid.
func TestMeetingSchemas(t *testing.T) {
	objectSchemas, relationshipSchemas := getMeetingSchemas()

	// Verify object schemas
	assert.Len(t, objectSchemas, 6, "Should have 6 object schemas")
	assert.Contains(t, objectSchemas, "Meeting")
	assert.Contains(t, objectSchemas, "Person")
	assert.Contains(t, objectSchemas, "Location")
	assert.Contains(t, objectSchemas, "ActionItem")
	assert.Contains(t, objectSchemas, "AgendaItem")
	assert.Contains(t, objectSchemas, "Decision")

	// Verify Meeting schema has required properties
	meeting := objectSchemas["Meeting"]
	assert.NotEmpty(t, meeting.Description)
	assert.Contains(t, meeting.Properties, "title")
	assert.Contains(t, meeting.Properties, "date")
	assert.Contains(t, meeting.Required, "title")
	assert.Contains(t, meeting.Required, "date")

	// Verify relationship schemas
	assert.Len(t, relationshipSchemas, 8, "Should have 8 relationship schemas")
	assert.Contains(t, relationshipSchemas, "ATTENDED")
	assert.Contains(t, relationshipSchemas, "ORGANIZED")
	assert.Contains(t, relationshipSchemas, "HELD_AT")
	assert.Contains(t, relationshipSchemas, "ASSIGNED_TO")

	// Verify relationship type constraints
	attended := relationshipSchemas["ATTENDED"]
	assert.Contains(t, attended.SourceTypes, "Person")
	assert.Contains(t, attended.TargetTypes, "Meeting")
}

// TestMeetingDocumentGeneration verifies the synthetic document contains expected content.
func TestMeetingDocumentGeneration(t *testing.T) {
	doc := getSyntheticMeetingDocument()
	groundTruth := getMeetingGroundTruth()

	// Verify document contains all person names
	for _, entity := range groundTruth.Entities {
		if entity.Type == "Person" {
			assert.Contains(t, doc, entity.Name, "Document should contain person: %s", entity.Name)
		}
	}

	// Verify document contains meeting date
	assert.Contains(t, doc, "February 10, 2026")
	assert.Contains(t, doc, "10:00")
	assert.Contains(t, doc, "11:30")

	// Verify document contains action items
	assert.Contains(t, doc, "authentication service")
	assert.Contains(t, doc, "user stories")
	assert.Contains(t, doc, "infrastructure costs")
}

// TestBuildEntityPromptWithMeetingSchemas tests prompt generation with meeting schemas.
func TestBuildEntityPromptWithMeetingSchemas(t *testing.T) {
	objectSchemas, _ := getMeetingSchemas()
	document := getSyntheticMeetingDocument()

	prompt := BuildEntityExtractionPrompt(
		document,
		objectSchemas,
		nil, // all types
		nil, // no existing entities
	)

	// Verify prompt contains schema information
	assert.Contains(t, prompt, "Meeting")
	assert.Contains(t, prompt, "Person")
	assert.Contains(t, prompt, "ActionItem")
	assert.Contains(t, prompt, "AgendaItem")
	assert.Contains(t, prompt, "Decision")
	assert.Contains(t, prompt, "Location")

	// Verify prompt contains property definitions
	assert.Contains(t, prompt, "date")
	assert.Contains(t, prompt, "start_time")
	assert.Contains(t, prompt, "due_date")
	assert.Contains(t, prompt, "priority")

	// Verify prompt contains the document text
	assert.Contains(t, prompt, "Sarah Johnson")
	assert.Contains(t, prompt, "Q1 Planning Meeting")
}

// TestBuildRelationshipPromptWithMeetingSchemas tests relationship prompt generation.
func TestBuildRelationshipPromptWithMeetingSchemas(t *testing.T) {
	_, relationshipSchemas := getMeetingSchemas()

	entities := []InternalEntity{
		{TempID: "meeting_q1_planning", Name: "Q1 Planning Meeting", Type: "Meeting"},
		{TempID: "person_sarah_johnson", Name: "Sarah Johnson", Type: "Person"},
		{TempID: "person_michael_chen", Name: "Michael Chen", Type: "Person"},
		{TempID: "actionitem_setup_auth", Name: "Set up authentication service", Type: "ActionItem"},
	}

	prompt := BuildRelationshipPrompt(
		entities,
		relationshipSchemas,
		"Meeting notes content...",
		nil,
		nil,
	)

	// Verify prompt contains entity information
	assert.Contains(t, prompt, "meeting_q1_planning")
	assert.Contains(t, prompt, "person_sarah_johnson")
	assert.Contains(t, prompt, "person_michael_chen")
	assert.Contains(t, prompt, "actionitem_setup_auth")

	// Verify prompt contains relationship types
	assert.Contains(t, prompt, "ATTENDED")
	assert.Contains(t, prompt, "ORGANIZED")
	assert.Contains(t, prompt, "ASSIGNED_TO")
	assert.Contains(t, prompt, "RESULTED_FROM")
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
