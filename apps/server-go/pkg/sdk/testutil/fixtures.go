package testutil

import (
	"time"

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

// FixtureAPIToken returns a sample API token (without full token value).
func FixtureAPIToken() *apitokens.APIToken {
	return &apitokens.APIToken{
		ID:        "token_test123",
		Name:      "Test Token",
		Prefix:    "emt_test",
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
func FixtureDocument() *documents.DocumentDTO {
	return &documents.DocumentDTO{
		ID:          "doc_test123",
		Title:       "Test Document",
		SourceType:  "upload",
		SourceURL:   "https://example.com/doc.pdf",
		ContentType: "application/pdf",
		CreatedAt:   time.Now().Add(-24 * time.Hour),
		UpdatedAt:   time.Now(),
	}
}

// FixtureDocuments returns a list of sample documents.
func FixtureDocuments() []documents.DocumentDTO {
	return []documents.DocumentDTO{
		*FixtureDocument(),
		{
			ID:          "doc_test456",
			Title:       "Another Document",
			SourceType:  "url",
			ContentType: "text/html",
			CreatedAt:   time.Now().Add(-48 * time.Hour),
			UpdatedAt:   time.Now().Add(-24 * time.Hour),
		},
	}
}
