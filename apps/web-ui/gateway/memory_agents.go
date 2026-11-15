package main

import (
	"context"
	"net/http"
	"net/url"
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
	// GitTokenSecretRef names a server-side git credential (not a literal); the
	// server resolves it at provision time.
	GitTokenSecretRef string `json:"git_token_secret_ref,omitempty"`
	// GitToken is the legacy plaintext git token; memory redacts it on read so the
	// gateway only ever sees the redaction placeholder.
	GitToken string `json:"git_token,omitempty"`
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
	Registered  bool   `json:"registered"`
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
	sanitizeAgentWrite(in)
	var env successEnvelope[AgentDefinition]
	if err := m.do(ctx, http.MethodPost, "/api/projects/"+m.projectIDFor(ctx)+"/agent-definitions", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) UpdateAgentDefinition(ctx context.Context, id string, in *AgentDefinition) (*AgentDefinition, error) {
	sanitizeAgentWrite(in)
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

// RuntimeAgent is the execution row a trigger binds to (kb.agents).
type RuntimeAgent struct {
	ID                string `json:"id"`
	ProjectID         string `json:"projectId"`
	Name              string `json:"name"`
	AgentDefinitionID string `json:"agentDefinitionId,omitempty"`
	Enabled           bool   `json:"enabled"`
}

// ListRuntimeAgents returns the project's kb.agents rows.
func (m *MemoryClient) ListRuntimeAgents(ctx context.Context, projectID string) ([]RuntimeAgent, error) {
	var env successEnvelope[[]RuntimeAgent]
	path := "/api/projects/" + url.PathEscape(projectID) + "/agents"
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

type CreateRuntimeAgentRequest struct {
	ProjectID         string `json:"projectId"`
	Name              string `json:"name"`
	StrategyType      string `json:"strategyType"`
	CronSchedule      string `json:"cronSchedule"`
	TriggerType       string `json:"triggerType,omitempty"`
	AgentDefinitionID string `json:"agentDefinitionId,omitempty"`
	Enabled           bool   `json:"enabled"`
}

// CreateRuntimeAgent creates the kb.agents row that source triggers resolve.
func (m *MemoryClient) CreateRuntimeAgent(ctx context.Context, in *CreateRuntimeAgentRequest) (*RuntimeAgent, error) {
	var env successEnvelope[RuntimeAgent]
	path := "/api/projects/" + url.PathEscape(in.ProjectID) + "/agents"
	if err := m.doH(ctx, http.MethodPost, path, in, m.documentHeaders(ctx), &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// AgentQueue is a named work queue (GET /api/projects/:projectId/agent-queues).
type AgentQueue struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Concurrency int    `json:"concurrency"`
	Enabled     bool   `json:"enabled"`
}

func (m *MemoryClient) ListAgentQueues(ctx context.Context, projectID string) ([]AgentQueue, error) {
	var env successEnvelope[[]AgentQueue]
	path := "/api/projects/" + url.PathEscape(projectID) + "/agent-queues"
	if err := m.doH(ctx, http.MethodGet, path, nil, m.documentHeaders(ctx), &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

func (m *MemoryClient) CreateAgentQueue(ctx context.Context, projectID, name string) (*AgentQueue, error) {
	var env successEnvelope[AgentQueue]
	path := "/api/projects/" + url.PathEscape(projectID) + "/agent-queues"
	body := map[string]any{"name": name, "concurrency": 1}
	if err := m.doH(ctx, http.MethodPost, path, body, m.documentHeaders(ctx), &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (m *MemoryClient) EnableRuntimeAgent(ctx context.Context, projectID, agentID string) error {
	path := "/api/projects/" + url.PathEscape(projectID) + "/agents/" + url.PathEscape(agentID) + "/enable"
	return m.doH(ctx, http.MethodPost, path, nil, m.documentHeaders(ctx), nil)
}
