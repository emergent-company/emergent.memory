package main

import (
	"context"
	"net/http"
)

// AgentSandboxConfig mirrors memory's per-agent workspace config (JSONB
// workspace_config). Field names are snake_case on the wire.
type AgentSandboxConfig struct {
	Enabled        bool              `json:"enabled"`
	Provider       string            `json:"provider,omitempty"`
	RepoSource     *RepoSourceConfig `json:"repo_source,omitempty"`
	Tools          []string          `json:"tools,omitempty"`
	ResourceLimits *ResourceLimits   `json:"resource_limits,omitempty"`
	BaseImage      string            `json:"base_image,omitempty"`
	SetupCommands  []string          `json:"setup_commands,omitempty"`
	EnvVars        map[string]string `json:"env_vars,omitempty"`
}

type RepoSourceConfig struct {
	Type   string `json:"type"`
	URL    string `json:"url,omitempty"`
	Branch string `json:"branch,omitempty"`
}

type ResourceLimits struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
	Disk   string `json:"disk,omitempty"`
}

type SandboxProvider struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Healthy     bool   `json:"healthy"`
	Message     string `json:"message"`
	ActiveCount int    `json:"active_count"`
}

type SandboxImage struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Status   string `json:"status"`
}

func (m *MemoryClient) ListAgentDefinitions(ctx context.Context) ([]AgentDefinitionSummary, error) {
	var env successEnvelope[[]AgentDefinitionSummary]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

func (m *MemoryClient) GetAgentDefinition(ctx context.Context, id string) (*AgentDefinition, error) {
	var env successEnvelope[AgentDefinition]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions/"+id, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) CreateAgentDefinition(ctx context.Context, in *AgentDefinition) (*AgentDefinition, error) {
	var env successEnvelope[AgentDefinition]
	if err := m.do(ctx, http.MethodPost, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) UpdateAgentDefinition(ctx context.Context, id string, in *AgentDefinition) (*AgentDefinition, error) {
	var env successEnvelope[AgentDefinition]
	if err := m.do(ctx, http.MethodPatch, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions/"+id, in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) DeleteAgentDefinition(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions/"+id, nil, nil)
}

func (m *MemoryClient) GetAgentSandboxConfig(ctx context.Context, id string) (*AgentSandboxConfig, error) {
	var env successEnvelope[AgentSandboxConfig]
	if err := m.do(ctx, http.MethodGet, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions/"+id+"/sandbox-config", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) SetAgentSandboxConfig(ctx context.Context, id string, cfg *AgentSandboxConfig) (*AgentSandboxConfig, error) {
	var env successEnvelope[AgentSandboxConfig]
	if err := m.do(ctx, http.MethodPut, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions/"+id+"/sandbox-config", cfg, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) ListSandboxProviders(ctx context.Context) ([]SandboxProvider, error) {
	var out []SandboxProvider
	if err := m.do(ctx, http.MethodGet, "/api/v1/agent/sandboxes/providers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *MemoryClient) ListSandboxImages(ctx context.Context) ([]SandboxImage, error) {
	var env successEnvelope[[]SandboxImage]
	if err := m.doH(ctx, http.MethodGet, "/api/admin/sandbox-images", nil, map[string]string{"X-Project-ID": m.projectIDFor(ctx)}, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}
