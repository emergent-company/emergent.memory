package workspace

import (
	"encoding/json"
	"time"
)

// --- Workspace Creation ---

// CreateWorkspaceRequest is the request DTO for creating a workspace.
type CreateWorkspaceRequest struct {
	ContainerType  ContainerType   `json:"container_type" validate:"required"`
	Provider       string          `json:"provider,omitempty"` // "firecracker", "e2b", "gvisor", or "auto"
	RepositoryURL  string          `json:"repository_url,omitempty"`
	Branch         string          `json:"branch,omitempty"`
	DeploymentMode string          `json:"deployment_mode,omitempty"` // "managed" or "self-hosted"
	ResourceLimits *ResourceLimits `json:"resource_limits,omitempty"`
	WarmStart      bool            `json:"warm_start,omitempty"`
	// MCP-specific fields
	MCPConfig *MCPConfig `json:"mcp_config,omitempty"`
}

// --- Workspace Responses ---

// WorkspaceResponse is the response DTO for a workspace.
type WorkspaceResponse struct {
	ID                  string          `json:"id"`
	AgentSessionID      *string         `json:"agent_session_id,omitempty"`
	ContainerType       ContainerType   `json:"container_type"`
	Provider            ProviderType    `json:"provider"`
	ProviderWorkspaceID string          `json:"provider_workspace_id"`
	RepositoryURL       *string         `json:"repository_url,omitempty"`
	Branch              *string         `json:"branch,omitempty"`
	DeploymentMode      DeploymentMode  `json:"deployment_mode"`
	Lifecycle           Lifecycle       `json:"lifecycle"`
	Status              Status          `json:"status"`
	CreatedAt           string          `json:"created_at"`
	LastUsedAt          string          `json:"last_used_at"`
	ExpiresAt           *string         `json:"expires_at,omitempty"`
	ResourceLimits      *ResourceLimits `json:"resource_limits,omitempty"`
	SnapshotID          *string         `json:"snapshot_id,omitempty"`
	MCPConfig           *MCPConfig      `json:"mcp_config,omitempty"`
	Metadata            map[string]any  `json:"metadata,omitempty"`
}

// ToResponse converts an AgentWorkspace entity to a WorkspaceResponse.
func ToResponse(ws *AgentWorkspace) *WorkspaceResponse {
	resp := &WorkspaceResponse{
		ID:                  ws.ID,
		AgentSessionID:      ws.AgentSessionID,
		ContainerType:       ws.ContainerType,
		Provider:            ws.Provider,
		ProviderWorkspaceID: ws.ProviderWorkspaceID,
		RepositoryURL:       ws.RepositoryURL,
		Branch:              ws.Branch,
		DeploymentMode:      ws.DeploymentMode,
		Lifecycle:           ws.Lifecycle,
		Status:              ws.Status,
		CreatedAt:           ws.CreatedAt.Format(time.RFC3339Nano),
		LastUsedAt:          ws.LastUsedAt.Format(time.RFC3339Nano),
		ResourceLimits:      ws.ResourceLimits,
		SnapshotID:          ws.SnapshotID,
		MCPConfig:           ws.MCPConfig,
		Metadata:            ws.Metadata,
	}
	if ws.ExpiresAt != nil {
		exp := ws.ExpiresAt.Format(time.RFC3339Nano)
		resp.ExpiresAt = &exp
	}
	return resp
}

// ToResponseList converts a slice of AgentWorkspace entities to WorkspaceResponses.
func ToResponseList(workspaces []*AgentWorkspace) []*WorkspaceResponse {
	result := make([]*WorkspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		result[i] = ToResponse(ws)
	}
	return result
}

// --- Tool Requests/Responses ---

// BashRequest is the request DTO for executing a bash command.
type BashRequest struct {
	Command   string `json:"command" validate:"required"`
	Workdir   string `json:"workdir,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// BashResponse is the response DTO for a bash command execution.
type BashResponse struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// ReadRequest is the request DTO for reading a file.
type ReadRequest struct {
	FilePath string `json:"file_path" validate:"required"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// ReadResponse is the response DTO for a file read.
type ReadResponse struct {
	Content    string `json:"content"`
	IsDir      bool   `json:"is_dir,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	IsBinary   bool   `json:"is_binary,omitempty"`
}

// WriteRequest is the request DTO for writing a file.
type WriteRequest struct {
	FilePath string `json:"file_path" validate:"required"`
	Content  string `json:"content"`
}

// EditRequest is the request DTO for string-replacement editing.
type EditRequest struct {
	FilePath   string `json:"file_path" validate:"required"`
	OldString  string `json:"old_string" validate:"required"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// EditResponse is the response DTO for an edit operation.
type EditResponse struct {
	Success      bool `json:"success"`
	LinesChanged int  `json:"lines_changed"`
	Replacements int  `json:"replacements,omitempty"`
}

// GlobRequest is the request DTO for file pattern matching.
type GlobRequest struct {
	Pattern string `json:"pattern" validate:"required"`
	Path    string `json:"path,omitempty"`
}

// GrepRequest is the request DTO for content search.
type GrepRequest struct {
	Pattern string `json:"pattern" validate:"required"`
	Path    string `json:"path,omitempty"`
	Include string `json:"include,omitempty"` // File pattern filter e.g. "*.ts"
}

// GrepMatch represents a single search match.
type GrepMatch struct {
	FilePath   string `json:"file_path"`
	LineNumber int    `json:"line_number"`
	Line       string `json:"line"`
}

// GrepResponse is the response DTO for a content search.
type GrepResponse struct {
	Matches []GrepMatch `json:"matches"`
}

// GitRequest is the request DTO for git operations.
type GitRequest struct {
	Action  string   `json:"action" validate:"required"` // status, diff, commit, push, pull, checkout
	Message string   `json:"message,omitempty"`          // For commit
	Files   []string `json:"files,omitempty"`            // For commit (files to stage)
	Branch  string   `json:"branch,omitempty"`           // For checkout
}

// GitResponse is the response DTO for git operations.
type GitResponse struct {
	Output string `json:"output"`
}

// --- Provider Status ---

// ProviderStatusResponse describes the status of a workspace provider.
type ProviderStatusResponse struct {
	Name         string                `json:"name"`
	Type         ProviderType          `json:"type"`
	Healthy      bool                  `json:"healthy"`
	Message      string                `json:"message,omitempty"`
	Capabilities *ProviderCapabilities `json:"capabilities"`
	ActiveCount  int                   `json:"active_count"`
}

// --- Workspace Attachment ---

// AttachSessionRequest is the request DTO for attaching an agent session to a workspace.
type AttachSessionRequest struct {
	AgentSessionID string `json:"agent_session_id" validate:"required"`
}

// --- Workspace Snapshots ---

// SnapshotResponse is the response DTO after creating a workspace snapshot.
type SnapshotResponse struct {
	SnapshotID  string `json:"snapshot_id"`
	WorkspaceID string `json:"workspace_id"`
	Provider    string `json:"provider"`
	CreatedAt   string `json:"created_at"`
}

// CreateFromSnapshotRequest is the request DTO for creating a workspace from a snapshot.
type CreateFromSnapshotRequest struct {
	SnapshotID     string          `json:"snapshot_id" validate:"required"`
	Provider       string          `json:"provider,omitempty"` // Must match snapshot provider; defaults to original
	DeploymentMode string          `json:"deployment_mode,omitempty"`
	ResourceLimits *ResourceLimits `json:"resource_limits,omitempty"`
}

// --- MCP Hosting ---

// RegisterMCPServerRequest is the request DTO for registering an MCP server for hosting.
type RegisterMCPServerRequest struct {
	Name           string            `json:"name" validate:"required"`
	Image          string            `json:"image" validate:"required"`
	Cmd            []string          `json:"cmd,omitempty"`
	StdioBridge    bool              `json:"stdio_bridge"`
	RestartPolicy  string            `json:"restart_policy,omitempty"` // "always" (default), "on-failure", "never"
	Environment    map[string]string `json:"environment,omitempty"`
	Volumes        []string          `json:"volumes,omitempty"`         // Persistent mount paths (e.g. "/data")
	ResourceLimits *ResourceLimits   `json:"resource_limits,omitempty"` // Default: 0.5 CPU, 512MB memory, 1GB disk
}

// MCPCallRequest is the request DTO for calling an MCP method via the stdio bridge.
type MCPCallRequest struct {
	Method    string `json:"method" validate:"required"` // JSON-RPC method (e.g. "tools/call", "tools/list")
	Params    any    `json:"params,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"` // Default: 30000ms
}

// MCPCallResponse is the response DTO for an MCP method call.
type MCPCallResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *JSONRPCError   `json:"error,omitempty"`
}

// MCPServerStatus is the response DTO for MCP server status.
type MCPServerStatus struct {
	WorkspaceID     string          `json:"workspace_id"`
	Name            string          `json:"name"`
	Image           string          `json:"image"`
	Status          Status          `json:"status"`
	Provider        ProviderType    `json:"provider"`
	StdioBridge     bool            `json:"stdio_bridge"`
	BridgeConnected bool            `json:"bridge_connected"`
	RestartPolicy   string          `json:"restart_policy"`
	RestartCount    int             `json:"restart_count"`
	LastCrash       *string         `json:"last_crash,omitempty"`
	Uptime          string          `json:"uptime,omitempty"`
	Volumes         []string        `json:"volumes,omitempty"`
	ResourceLimits  *ResourceLimits `json:"resource_limits,omitempty"`
	CreatedAt       string          `json:"created_at"`
	LastUsedAt      string          `json:"last_used_at"`
}
