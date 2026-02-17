package workspace

import (
	"time"

	"github.com/uptrace/bun"
)

// ContainerType distinguishes between agent workspaces and MCP server containers.
type ContainerType string

const (
	ContainerTypeAgentWorkspace ContainerType = "agent_workspace"
	ContainerTypeMCPServer      ContainerType = "mcp_server"
)

// ProviderType identifies the sandbox provider.
type ProviderType string

const (
	ProviderFirecracker ProviderType = "firecracker"
	ProviderE2B         ProviderType = "e2b"
	ProviderGVisor      ProviderType = "gvisor"
)

// DeploymentMode indicates managed vs self-hosted.
type DeploymentMode string

const (
	DeploymentManaged    DeploymentMode = "managed"
	DeploymentSelfHosted DeploymentMode = "self-hosted"
)

// Lifecycle distinguishes ephemeral (session-scoped) from persistent (daemon) containers.
type Lifecycle string

const (
	LifecycleEphemeral  Lifecycle = "ephemeral"
	LifecyclePersistent Lifecycle = "persistent"
)

// Status tracks the current state of a workspace or MCP server container.
type Status string

const (
	StatusCreating Status = "creating"
	StatusReady    Status = "ready"
	StatusStopping Status = "stopping"
	StatusStopped  Status = "stopped"
	StatusError    Status = "error"
)

// ResourceLimits defines CPU, memory, and disk constraints for a workspace.
type ResourceLimits struct {
	CPU    string `json:"cpu,omitempty"`    // e.g. "2"
	Memory string `json:"memory,omitempty"` // e.g. "4G"
	Disk   string `json:"disk,omitempty"`   // e.g. "10G"
}

// MCPConfig holds MCP server-specific configuration.
type MCPConfig struct {
	Name          string            `json:"name,omitempty"`
	Image         string            `json:"image,omitempty"`
	StdioBridge   bool              `json:"stdio_bridge,omitempty"`
	RestartPolicy string            `json:"restart_policy,omitempty"` // "always", "on-failure", "never"
	Environment   map[string]string `json:"environment,omitempty"`
	Volumes       []string          `json:"volumes,omitempty"`
}

// AgentWorkspace represents a row in the kb.agent_workspaces table.
type AgentWorkspace struct {
	bun.BaseModel `bun:"table:kb.agent_workspaces,alias:aw"`

	ID                  string          `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AgentSessionID      *string         `bun:"agent_session_id,type:uuid" json:"agent_session_id,omitempty"`
	ContainerType       ContainerType   `bun:"container_type,notnull" json:"container_type"`
	Provider            ProviderType    `bun:"provider,notnull" json:"provider"`
	ProviderWorkspaceID string          `bun:"provider_workspace_id,notnull" json:"provider_workspace_id"`
	RepositoryURL       *string         `bun:"repository_url" json:"repository_url,omitempty"`
	Branch              *string         `bun:"branch" json:"branch,omitempty"`
	DeploymentMode      DeploymentMode  `bun:"deployment_mode,notnull" json:"deployment_mode"`
	Lifecycle           Lifecycle       `bun:"lifecycle,notnull" json:"lifecycle"`
	Status              Status          `bun:"status,notnull" json:"status"`
	CreatedAt           time.Time       `bun:"created_at,notnull,default:current_timestamp" json:"created_at"`
	LastUsedAt          time.Time       `bun:"last_used_at,notnull,default:current_timestamp" json:"last_used_at"`
	ExpiresAt           *time.Time      `bun:"expires_at" json:"expires_at,omitempty"`
	ResourceLimits      *ResourceLimits `bun:"resource_limits,type:jsonb" json:"resource_limits,omitempty"`
	SnapshotID          *string         `bun:"snapshot_id" json:"snapshot_id,omitempty"`
	MCPConfig           *MCPConfig      `bun:"mcp_config,type:jsonb" json:"mcp_config,omitempty"`
	Metadata            map[string]any  `bun:"metadata,type:jsonb" json:"metadata,omitempty"`
}
