package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ============================================================================
// API Token Tool Definitions
// ============================================================================

func tokenToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "token-list",
			Description: "List all API tokens for the current project. Returns token metadata (id, name, prefix, scopes, created at) but not the raw token value.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "token-create",
			Description: "Create a new API token for the current project. Returns the token id, name, scopes, and the raw token value (shown once only).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name": {
						Type:        "string",
						Description: "Human-readable name for this token (1–255 chars)",
					},
					"scopes": {
						Type:        "string",
						Description: "Comma-separated list of scopes. Valid values: schema:read, data:read, data:write, agents:read, agents:write, projects:read, projects:write",
					},
				},
				Required: []string{"name", "scopes"},
			},
		},
		{
			Name:        "token-get",
			Description: "Get a project API token by its ID. Returns metadata and the encrypted token value if available.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"token_id": {
						Type:        "string",
						Description: "UUID of the API token",
					},
				},
				Required: []string{"token_id"},
			},
		},
		{
			Name:        "token-revoke",
			Description: "Revoke (permanently disable) a project API token. This cannot be undone.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"token_id": {
						Type:        "string",
						Description: "UUID of the API token to revoke",
					},
				},
				Required: []string{"token_id"},
			},
		},
	}
}

// ============================================================================
// API Token Tool Handlers
// ============================================================================

func (s *Service) executeListProjectAPITokens(ctx context.Context, projectID string) (*ToolResult, error) {
	result, err := s.apitokenSvc.ListByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list_project_api_tokens: %w", err)
	}
	return s.wrapResult(result)
}

func (s *Service) executeCreateProjectAPIToken(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("create_project_api_token: 'name' is required")
	}
	scopesStr, _ := args["scopes"].(string)
	if scopesStr == "" {
		return nil, fmt.Errorf("create_project_api_token: 'scopes' is required")
	}
	scopes := splitScopes(scopesStr)

	userID := ""
	if u := auth.UserFromContext(ctx); u != nil {
		userID = u.ID
	}
	result, err := s.apitokenSvc.Create(ctx, projectID, userID, name, scopes)
	if err != nil {
		return nil, fmt.Errorf("create_project_api_token: %w", err)
	}
	return s.wrapResult(result)
}

func (s *Service) executeGetProjectAPIToken(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		return nil, fmt.Errorf("get_project_api_token: 'token_id' is required")
	}
	result, err := s.apitokenSvc.GetByID(ctx, tokenID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get_project_api_token: %w", err)
	}
	return s.wrapResult(result)
}

func (s *Service) executeRevokeProjectAPIToken(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		return nil, fmt.Errorf("revoke_project_api_token: 'token_id' is required")
	}
	userID := ""
	if u := auth.UserFromContext(ctx); u != nil {
		userID = u.ID
	}
	if err := s.apitokenSvc.Revoke(ctx, tokenID, projectID, userID); err != nil {
		return nil, fmt.Errorf("revoke_project_api_token: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "token_id": tokenID})
}

// splitScopes splits a comma-separated scope string into a slice.
func splitScopes(scopesStr string) []string {
	var scopes []string
	for _, s := range strings.Split(scopesStr, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			scopes = append(scopes, s)
		}
	}
	return scopes
}
