package mcp

import (
	"context"
	"fmt"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ============================================================================
// Provider Tool Definitions
// ============================================================================

func providerToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "provider-list-org",
			Description: "List all LLM provider configurations for an organization. Returns provider name, model selections, and credential source.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"org_id": {
						Type:        "string",
						Description: "UUID of the organization. Optional — defaults to the caller's organization.",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "provider-configure-org",
			Description: "Configure or update an LLM provider at the organization level (e.g. set Google AI API key).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"org_id": {
						Type:        "string",
						Description: "UUID of the organization. Optional — defaults to the caller's organization.",
					},
					"provider_name": {
						Type:        "string",
						Description: "Provider identifier (e.g. 'google', 'vertex')",
					},
					"api_key": {
						Type:        "string",
						Description: "API key for the provider",
					},
					"service_account_json": {
						Type:        "string",
						Description: "Service account JSON for Vertex AI (alternative to api_key)",
					},
					"gcp_project": {
						Type:        "string",
						Description: "GCP project ID for Vertex AI",
					},
					"location": {
						Type:        "string",
						Description: "GCP region for Vertex AI (e.g. 'us-central1')",
					},
				},
				Required: []string{"provider_name"},
			},
		},
		{
			Name:        "provider-configure-project",
			Description: "Configure or update an LLM provider at the project level, overriding the org-level config.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"provider_name": {
						Type:        "string",
						Description: "Provider identifier (e.g. 'google', 'vertex')",
					},
					"api_key": {
						Type:        "string",
						Description: "API key for the provider",
					},
					"service_account_json": {
						Type:        "string",
						Description: "Service account JSON for Vertex AI (alternative to api_key)",
					},
					"gcp_project": {
						Type:        "string",
						Description: "GCP project ID for Vertex AI",
					},
					"location": {
						Type:        "string",
						Description: "GCP region for Vertex AI (e.g. 'us-central1')",
					},
				},
				Required: []string{"provider_name"},
			},
		},
		{
			Name:        "provider-models-list",
			Description: "List available models for a given LLM provider. Optionally filter by model type (generative or embedding).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"provider_name": {
						Type:        "string",
						Description: "Provider identifier (e.g. 'google', 'vertex')",
					},
					"model_type": {
						Type:        "string",
						Description: "Filter by model type",
						Enum:        []string{"generative", "embedding"},
					},
				},
				Required: []string{"provider_name"},
			},
		},
		{
			Name:        "provider-test",
			Description: "Test an LLM provider configuration by sending a minimal generation request. Returns the model used and the response.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"provider_name": {
						Type:        "string",
						Description: "Provider identifier to test (e.g. 'google', 'vertex')",
					},
				},
				Required: []string{"provider_name"},
			},
		},
		{
			Name:        "provider-usage-get",
			Description: "Get LLM usage statistics (token counts, costs) for the organization.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"org_id": {
						Type:        "string",
						Description: "UUID of the organization. Optional — defaults to the caller's organization.",
					},
				},
				Required: []string{},
			},
		},
	}
}

// ============================================================================
// Provider Tool Handlers
// ============================================================================

func (s *Service) executeListOrgProviders(ctx context.Context, args map[string]any) (*ToolResult, error) {
	orgID, _ := args["org_id"].(string)
	if orgID == "" {
		orgID = resolveOrgID(ctx, s)
	}
	if orgID == "" {
		return nil, fmt.Errorf("list_org_providers: 'org_id' is required")
	}
	configs, err := s.providerCredSvc.ListOrgConfigs(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list_org_providers: %w", err)
	}
	return s.wrapResult(configs)
}

func (s *Service) executeConfigureOrgProvider(ctx context.Context, args map[string]any) (*ToolResult, error) {
	orgID, _ := args["org_id"].(string)
	if orgID == "" {
		orgID = resolveOrgID(ctx, s)
	}
	if orgID == "" {
		return nil, fmt.Errorf("configure_org_provider: 'org_id' is required")
	}
	providerName, _ := args["provider_name"].(string)
	if providerName == "" {
		return nil, fmt.Errorf("configure_org_provider: 'provider_name' is required")
	}
	req := buildUpsertRequest(args)
	cfg, err := s.providerCredSvc.UpsertOrgConfig(ctx, orgID, provider.ProviderType(providerName), req)
	if err != nil {
		return nil, fmt.Errorf("configure_org_provider: %w", err)
	}
	return s.wrapResult(cfg)
}

func (s *Service) executeConfigureProjectProvider(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	providerName, _ := args["provider_name"].(string)
	if providerName == "" {
		return nil, fmt.Errorf("configure_project_provider: 'provider_name' is required")
	}
	req := buildUpsertRequest(args)
	cfg, err := s.providerCredSvc.UpsertProjectConfig(ctx, projectID, provider.ProviderType(providerName), req)
	if err != nil {
		return nil, fmt.Errorf("configure_project_provider: %w", err)
	}
	return s.wrapResult(cfg)
}

func (s *Service) executeListProviderModels(ctx context.Context, args map[string]any) (*ToolResult, error) {
	providerName, _ := args["provider_name"].(string)
	if providerName == "" {
		return nil, fmt.Errorf("list_provider_models: 'provider_name' is required")
	}
	var modelTypePtr *provider.ModelType
	if mt, ok := args["model_type"].(string); ok && mt != "" {
		mt2 := provider.ModelType(mt)
		modelTypePtr = &mt2
	}
	models, err := s.providerCatalogSvc.ListModels(ctx, provider.ProviderType(providerName), modelTypePtr)
	if err != nil {
		return nil, fmt.Errorf("list_provider_models: %w", err)
	}
	return s.wrapResult(models)
}

func (s *Service) executeTestProvider(ctx context.Context, args map[string]any) (*ToolResult, error) {
	providerName, _ := args["provider_name"].(string)
	if providerName == "" {
		return nil, fmt.Errorf("test_provider: 'provider_name' is required")
	}
	cred, err := s.providerCredSvc.Resolve(ctx, provider.ProviderType(providerName))
	if err != nil {
		return nil, fmt.Errorf("test_provider: failed to resolve credentials: %w", err)
	}
	model, reply, err := s.providerCatalogSvc.TestGenerate(ctx, provider.ProviderType(providerName), cred)
	if err != nil {
		return nil, fmt.Errorf("test_provider: %w", err)
	}
	return s.wrapResult(map[string]any{
		"model": model,
		"reply": reply,
	})
}

func (s *Service) executeGetProviderUsage(ctx context.Context, args map[string]any) (*ToolResult, error) {
	// Provider usage is not directly in CredentialService — it's a separate endpoint.
	// For now return a not-implemented placeholder with a useful message.
	return s.wrapResult(map[string]any{
		"message": "Provider usage statistics are available at GET /api/v1/organizations/:orgId/usage",
	})
}

// resolveOrgID returns the org ID from auth context, falling back to a project lookup.
func resolveOrgID(ctx context.Context, s *Service) string {
	orgID := auth.OrgIDFromContext(ctx)
	if orgID == "" {
		if projectID := auth.ProjectIDFromContext(ctx); projectID != "" {
			var id string
			_ = s.db.NewRaw(
				"SELECT organization_id FROM kb.projects WHERE id = ? LIMIT 1",
				projectID,
			).Scan(ctx, &id)
			orgID = id
		}
	}
	return orgID
}

// buildUpsertRequest converts MCP tool args to a provider.UpsertProviderConfigRequest.
func buildUpsertRequest(args map[string]any) provider.UpsertProviderConfigRequest {
	req := provider.UpsertProviderConfigRequest{}
	if v, ok := args["api_key"].(string); ok {
		req.APIKey = v
	}
	if v, ok := args["service_account_json"].(string); ok {
		req.ServiceAccountJSON = v
	}
	if v, ok := args["gcp_project"].(string); ok {
		req.GCPProject = v
	}
	if v, ok := args["location"].(string); ok {
		req.Location = v
	}
	return req
}
