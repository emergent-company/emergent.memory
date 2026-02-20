package testutil

import (
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/agents"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/apitokens"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/documents"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/health"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/orgs"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/users"
)

// Fixtures provides common test data.

// FixtureProject returns a sample project for testing.
func FixtureProject() *projects.Project {
	purpose := "Test knowledge base"
	template := "You are a helpful assistant"
	autoExtract := true
	return &projects.Project{
		ID:                 "proj_test123",
		Name:               "Test Project",
		OrgID:              "org_test456",
		KBPurpose:          &purpose,
		ChatPromptTemplate: &template,
		AutoExtractObjects: &autoExtract,
		AutoExtractConfig: map[string]interface{}{
			"enabled": true,
		},
	}
}

// FixtureProjects returns a list of sample projects.
func FixtureProjects() []projects.Project {
	return []projects.Project{
		*FixtureProject(),
		{
			ID:    "proj_test789",
			Name:  "Another Project",
			OrgID: "org_test456",
		},
	}
}

// FixtureOrganization returns a sample organization for testing.
func FixtureOrganization() *orgs.Organization {
	return &orgs.Organization{
		ID:   "org_test456",
		Name: "Test Organization",
	}
}

// FixtureOrganizations returns a list of sample organizations.
func FixtureOrganizations() []orgs.Organization {
	return []orgs.Organization{
		*FixtureOrganization(),
		{
			ID:   "org_test789",
			Name: "Another Org",
		},
	}
}

// FixtureUserProfile returns a sample user profile for testing.
func FixtureUserProfile() *users.UserProfile {
	displayName := "Test User"
	firstName := "Test"
	lastName := "User"
	avatarURL := "https://example.com/avatar.jpg"
	phone := "+1234567890"

	return &users.UserProfile{
		ID:          "user_test123",
		Email:       "test@example.com",
		DisplayName: &displayName,
		FirstName:   &firstName,
		LastName:    &lastName,
		AvatarURL:   &avatarURL,
		PhoneE164:   &phone,
	}
}

// FixtureAPIToken returns a sample API token (with full token value for get responses).
func FixtureAPIToken() *apitokens.APIToken {
	return &apitokens.APIToken{
		ID:        "token_test123",
		Name:      "Test Token",
		Prefix:    "emt_test",
		Token:     "emt_test_full_token_value_here",
		Scopes:    []string{"documents:read", "documents:write"},
		CreatedAt: time.Now().Format(time.RFC3339),
		RevokedAt: nil,
	}
}

// FixtureCreateTokenResponse returns a sample token creation response.
func FixtureCreateTokenResponse() *apitokens.CreateTokenResponse {
	return &apitokens.CreateTokenResponse{
		ID:        "token_test123",
		Name:      "Test Token",
		Token:     "emt_test_full_token_value_here",
		Prefix:    "emt_test",
		Scopes:    []string{"documents:read", "documents:write"},
		CreatedAt: time.Now().Format(time.RFC3339),
	}
}

// FixtureHealthResponse returns a sample health check response.
func FixtureHealthResponse() *health.HealthResponse {
	return &health.HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
		Uptime:    "2h30m15s",
		Version:   "1.0.0",
		Checks: map[string]health.Check{
			"database": {
				Status:  "healthy",
				Message: "",
			},
		},
	}
}

// FixtureProjectMember returns a sample project member.
func FixtureProjectMember() *projects.ProjectMember {
	displayName := "Test User"
	firstName := "Test"
	lastName := "User"
	avatarURL := "https://example.com/avatar.jpg"

	return &projects.ProjectMember{
		ID:          "user_test123",
		Email:       "test@example.com",
		DisplayName: &displayName,
		FirstName:   &firstName,
		LastName:    &lastName,
		AvatarURL:   &avatarURL,
		Role:        "project_admin",
		JoinedAt:    time.Now().Format(time.RFC3339),
	}
}

// FixtureDocument returns a sample document for testing.
func FixtureDocument() *documents.Document {
	filename := "Test Document"
	sourceType := "upload"
	sourceURL := "https://example.com/doc.pdf"
	mimeType := "application/pdf"
	return &documents.Document{
		ID:         "doc_test123",
		Filename:   &filename,
		SourceType: &sourceType,
		SourceURL:  &sourceURL,
		MimeType:   &mimeType,
		CreatedAt:  time.Now().Add(-24 * time.Hour),
		UpdatedAt:  time.Now(),
	}
}

// FixtureDocuments returns a list of documents.Document for testing.
func FixtureDocuments() []documents.Document {
	filename2 := "Another Document"
	sourceType2 := "url"
	mimeType2 := "text/html"
	return []documents.Document{
		*FixtureDocument(),
		{
			ID:         "doc_test456",
			Filename:   &filename2,
			SourceType: &sourceType2,
			MimeType:   &mimeType2,
			CreatedAt:  time.Now().Add(-48 * time.Hour),
			UpdatedAt:  time.Now().Add(-24 * time.Hour),
		},
	}
}

// FixtureAgent returns a sample agent for testing.
func FixtureAgent() *agents.Agent {
	prompt := "You are a helpful agent"
	description := "Test agent"
	lastRunStatus := "completed"
	lastRunAt := time.Now().Add(-1 * time.Hour)
	return &agents.Agent{
		ID:            "agent_test123",
		ProjectID:     "proj_test123",
		Name:          "Test Agent",
		StrategyType:  "graph_object_processor",
		Prompt:        &prompt,
		CronSchedule:  "0 */5 * * * *",
		Enabled:       true,
		TriggerType:   "schedule",
		ExecutionMode: "async",
		Description:   &description,
		LastRunStatus: &lastRunStatus,
		LastRunAt:     &lastRunAt,
		CreatedAt:     time.Now().Add(-24 * time.Hour),
		UpdatedAt:     time.Now(),
	}
}

// FixtureAgents returns a list of sample agents.
func FixtureAgents() []agents.Agent {
	return []agents.Agent{
		*FixtureAgent(),
		{
			ID:            "agent_test456",
			ProjectID:     "proj_test123",
			Name:          "Another Agent",
			StrategyType:  "custom",
			CronSchedule:  "",
			Enabled:       false,
			TriggerType:   "manual",
			ExecutionMode: "sync",
			CreatedAt:     time.Now().Add(-48 * time.Hour),
			UpdatedAt:     time.Now().Add(-24 * time.Hour),
		},
	}
}

// FixtureAgentRun returns a sample agent run.
func FixtureAgentRun() *agents.AgentRun {
	completedAt := time.Now()
	durationMs := 1500
	return &agents.AgentRun{
		ID:          "run_test123",
		AgentID:     "agent_test123",
		Status:      "completed",
		StartedAt:   time.Now().Add(-2 * time.Minute),
		CompletedAt: &completedAt,
		DurationMs:  &durationMs,
		Summary:     map[string]any{"processed": 10},
		StepCount:   5,
	}
}

// FixtureAgentRuns returns a list of sample agent runs.
func FixtureAgentRuns() []agents.AgentRun {
	return []agents.AgentRun{
		*FixtureAgentRun(),
		{
			ID:        "run_test456",
			AgentID:   "agent_test123",
			Status:    "running",
			StartedAt: time.Now().Add(-30 * time.Second),
			StepCount: 2,
		},
	}
}

// FixtureAgentQuestion returns a sample agent question.
func FixtureAgentQuestion() *agents.AgentQuestion {
	return &agents.AgentQuestion{
		ID:        "question_test123",
		RunID:     "run_test123",
		AgentID:   "agent_test123",
		ProjectID: "proj_test123",
		Question:  "What color would you like?",
		Options: []agents.AgentQuestionOption{
			{Label: "Red", Value: "red", Description: "The color red"},
			{Label: "Blue", Value: "blue", Description: "The color blue"},
		},
		Status:    "pending",
		CreatedAt: time.Now().Add(-5 * time.Minute),
		UpdatedAt: time.Now().Add(-5 * time.Minute),
	}
}

// FixtureAgentQuestions returns a list of sample agent questions.
func FixtureAgentQuestions() []agents.AgentQuestion {
	respondedAt := time.Now().Add(-10 * time.Minute)
	response := "Blue"
	respondedBy := "user_test123"
	return []agents.AgentQuestion{
		*FixtureAgentQuestion(),
		{
			ID:          "question_test456",
			RunID:       "run_test456",
			AgentID:     "agent_test123",
			ProjectID:   "proj_test123",
			Question:    "What size?",
			Options:     []agents.AgentQuestionOption{{Label: "Small", Value: "small"}, {Label: "Large", Value: "large"}},
			Status:      "answered",
			Response:    &response,
			RespondedBy: &respondedBy,
			RespondedAt: &respondedAt,
			CreatedAt:   time.Now().Add(-15 * time.Minute),
			UpdatedAt:   respondedAt,
		},
	}
}

// FixtureAgentRunMessage returns a sample agent run message.
func FixtureAgentRunMessage() *agents.AgentRunMessage {
	return &agents.AgentRunMessage{
		ID:         "msg_test123",
		RunID:      "run_test123",
		Role:       "assistant",
		Content:    map[string]any{"text": "Hello, how can I help?"},
		StepNumber: 1,
		CreatedAt:  time.Now().Add(-5 * time.Minute),
	}
}

// FixtureAgentRunMessages returns a list of sample agent run messages.
func FixtureAgentRunMessages() []agents.AgentRunMessage {
	return []agents.AgentRunMessage{
		*FixtureAgentRunMessage(),
		{
			ID:         "msg_test456",
			RunID:      "run_test123",
			Role:       "user",
			Content:    map[string]any{"text": "I need help"},
			StepNumber: 0,
			CreatedAt:  time.Now().Add(-6 * time.Minute),
		},
	}
}

// FixtureAgentRunToolCall returns a sample agent run tool call.
func FixtureAgentRunToolCall() *agents.AgentRunToolCall {
	durationMs := 250
	messageID := "msg_test123"
	return &agents.AgentRunToolCall{
		ID:         "tool_test123",
		RunID:      "run_test123",
		MessageID:  &messageID,
		ToolName:   "search_documents",
		Input:      map[string]any{"query": "test"},
		Output:     map[string]any{"results": []string{"doc1", "doc2"}},
		Status:     "completed",
		DurationMs: &durationMs,
		StepNumber: 2,
		CreatedAt:  time.Now().Add(-4 * time.Minute),
	}
}

// FixtureAgentRunToolCalls returns a list of sample agent run tool calls.
func FixtureAgentRunToolCalls() []agents.AgentRunToolCall {
	return []agents.AgentRunToolCall{
		*FixtureAgentRunToolCall(),
		{
			ID:         "tool_test456",
			RunID:      "run_test123",
			ToolName:   "create_object",
			Input:      map[string]any{"type": "document"},
			Output:     map[string]any{"id": "doc_new123"},
			Status:     "completed",
			StepNumber: 3,
			CreatedAt:  time.Now().Add(-3 * time.Minute),
		},
	}
}
