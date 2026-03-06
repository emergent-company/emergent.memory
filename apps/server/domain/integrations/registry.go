package integrations

// IntegrationRegistry provides information about available integration types
type IntegrationRegistry struct {
	integrations map[string]AvailableIntegrationDTO
}

// NewIntegrationRegistry creates a new registry with built-in integrations
func NewIntegrationRegistry() *IntegrationRegistry {
	registry := &IntegrationRegistry{
		integrations: make(map[string]AvailableIntegrationDTO),
	}

	// Register built-in integrations
	registry.Register(AvailableIntegrationDTO{
		Name:        "clickup",
		DisplayName: "ClickUp",
		Description: "Import tasks and documents from ClickUp workspaces",
		Capabilities: IntegrationCapabilitiesDTO{
			SupportsImport:            true,
			SupportsWebhooks:          true,
			SupportsBidirectionalSync: false,
			RequiresOAuth:             false,
			SupportsIncrementalSync:   true,
		},
		RequiredSettings: []string{"api_key"},
		OptionalSettings: map[string]interface{}{
			"workspace_id":     "Workspace ID to sync from",
			"include_subtasks": "Include subtasks in sync",
		},
	})

	registry.Register(AvailableIntegrationDTO{
		Name:        "github",
		DisplayName: "GitHub",
		Description: "Import issues, PRs, and discussions from GitHub repositories",
		Capabilities: IntegrationCapabilitiesDTO{
			SupportsImport:            true,
			SupportsWebhooks:          true,
			SupportsBidirectionalSync: false,
			RequiresOAuth:             true,
			SupportsIncrementalSync:   true,
		},
		RequiredSettings: []string{"access_token", "repository"},
		OptionalSettings: map[string]interface{}{
			"include_issues":      "Import issues",
			"include_prs":         "Import pull requests",
			"include_discussions": "Import discussions",
		},
	})

	registry.Register(AvailableIntegrationDTO{
		Name:        "notion",
		DisplayName: "Notion",
		Description: "Import pages and databases from Notion workspaces",
		Capabilities: IntegrationCapabilitiesDTO{
			SupportsImport:            true,
			SupportsWebhooks:          false,
			SupportsBidirectionalSync: false,
			RequiresOAuth:             true,
			SupportsIncrementalSync:   true,
		},
		RequiredSettings: []string{"access_token"},
		OptionalSettings: map[string]interface{}{
			"workspace_id": "Specific workspace to sync",
		},
	})

	registry.Register(AvailableIntegrationDTO{
		Name:        "slack",
		DisplayName: "Slack",
		Description: "Import messages and threads from Slack channels",
		Capabilities: IntegrationCapabilitiesDTO{
			SupportsImport:            true,
			SupportsWebhooks:          true,
			SupportsBidirectionalSync: false,
			RequiresOAuth:             true,
			SupportsIncrementalSync:   true,
		},
		RequiredSettings: []string{"bot_token"},
		OptionalSettings: map[string]interface{}{
			"channels": "Specific channels to sync",
		},
	})

	return registry
}

// Register adds an integration type to the registry
func (r *IntegrationRegistry) Register(integration AvailableIntegrationDTO) {
	r.integrations[integration.Name] = integration
}

// List returns all available integrations
func (r *IntegrationRegistry) List() []AvailableIntegrationDTO {
	result := make([]AvailableIntegrationDTO, 0, len(r.integrations))
	for _, integration := range r.integrations {
		result = append(result, integration)
	}
	return result
}

// Get returns an integration by name
func (r *IntegrationRegistry) Get(name string) (AvailableIntegrationDTO, bool) {
	integration, ok := r.integrations[name]
	return integration, ok
}

// Exists checks if an integration type exists
func (r *IntegrationRegistry) Exists(name string) bool {
	_, ok := r.integrations[name]
	return ok
}
