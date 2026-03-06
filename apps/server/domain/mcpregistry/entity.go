package mcpregistry

import (
	"time"

	"github.com/uptrace/bun"
)

// ToolPoolInvalidator is implemented by agents.ToolPool to invalidate cached
// tool definitions when MCP server configurations change.
// This interface breaks the circular dependency (mcpregistry cannot import agents).
type ToolPoolInvalidator interface {
	// InvalidateCache removes the cached tool pool for a project,
	// forcing a rebuild on the next agent tool resolution.
	InvalidateCache(projectID string)
}

// MCPServerType defines the transport type for an MCP server.
type MCPServerType string

const (
	ServerTypeBuiltin MCPServerType = "builtin"
	ServerTypeStdio   MCPServerType = "stdio"
	ServerTypeSSE     MCPServerType = "sse"
	ServerTypeHTTP    MCPServerType = "http"
)

// MCPServer represents a registered MCP server configuration.
// Table: kb.mcp_servers
type MCPServer struct {
	bun.BaseModel `bun:"table:kb.mcp_servers,alias:ms"`

	ID        string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID string         `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Name      string         `bun:"name,notnull" json:"name"`
	Enabled   bool           `bun:"enabled,notnull,default:true" json:"enabled"`
	Type      MCPServerType  `bun:"type,notnull" json:"type"`
	Command   *string        `bun:"command" json:"command,omitempty"`               // for stdio
	Args      []string       `bun:"args,array,default:'{}'" json:"args"`            // for stdio
	Env       map[string]any `bun:"env,type:jsonb,default:'{}'" json:"env"`         // environment vars
	URL       *string        `bun:"url" json:"url,omitempty"`                       // for sse/http
	Headers   map[string]any `bun:"headers,type:jsonb,default:'{}'" json:"headers"` // for sse/http
	CreatedAt time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`

	// Relations
	Tools []*MCPServerTool `bun:"rel:has-many,join:id=server_id" json:"tools,omitempty"`
}

// MCPServerTool represents a cached tool definition from an MCP server.
// Table: kb.mcp_server_tools
type MCPServerTool struct {
	bun.BaseModel `bun:"table:kb.mcp_server_tools,alias:mst"`

	ID          string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ServerID    string         `bun:"server_id,type:uuid,notnull" json:"serverId"`
	ToolName    string         `bun:"tool_name,notnull" json:"toolName"`
	Description *string        `bun:"description" json:"description,omitempty"`
	InputSchema map[string]any `bun:"input_schema,type:jsonb,default:'{}'" json:"inputSchema"`
	Enabled     bool           `bun:"enabled,notnull,default:true" json:"enabled"`
	CreatedAt   time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	// Relations
	Server *MCPServer `bun:"rel:belongs-to,join:server_id=id" json:"-"`
}

// --- DTOs ---

// MCPServerDTO is the response DTO for an MCP server.
type MCPServerDTO struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"projectId"`
	Name      string         `json:"name"`
	Enabled   bool           `json:"enabled"`
	Type      MCPServerType  `json:"type"`
	Command   *string        `json:"command,omitempty"`
	Args      []string       `json:"args,omitempty"`
	Env       map[string]any `json:"env,omitempty"`
	URL       *string        `json:"url,omitempty"`
	Headers   map[string]any `json:"headers,omitempty"`
	ToolCount int            `json:"toolCount"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// MCPServerToolDTO is the response DTO for an MCP server tool.
type MCPServerToolDTO struct {
	ID          string         `json:"id"`
	ServerID    string         `json:"serverId"`
	ToolName    string         `json:"toolName"`
	Description *string        `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// MCPServerDetailDTO includes the server and its tools.
type MCPServerDetailDTO struct {
	MCPServerDTO
	Tools []MCPServerToolDTO `json:"tools"`
}

// CreateMCPServerDTO is the request DTO for creating an MCP server.
type CreateMCPServerDTO struct {
	Name    string         `json:"name" validate:"required"`
	Type    MCPServerType  `json:"type" validate:"required,oneof=stdio sse http"`
	Enabled *bool          `json:"enabled"`
	Command *string        `json:"command"`
	Args    []string       `json:"args"`
	Env     map[string]any `json:"env"`
	URL     *string        `json:"url"`
	Headers map[string]any `json:"headers"`
}

// UpdateMCPServerDTO is the request DTO for updating an MCP server.
type UpdateMCPServerDTO struct {
	Name    *string        `json:"name"`
	Enabled *bool          `json:"enabled"`
	Command *string        `json:"command"`
	Args    []string       `json:"args"`
	Env     map[string]any `json:"env"`
	URL     *string        `json:"url"`
	Headers map[string]any `json:"headers"`
}

// UpdateMCPServerToolDTO is the request DTO for toggling a tool.
type UpdateMCPServerToolDTO struct {
	Enabled *bool `json:"enabled" validate:"required"`
}

// InstallFromRegistryDTO is the request DTO for installing a server from the official MCP registry.
type InstallFromRegistryDTO struct {
	// RegistryName is the server name in the registry (e.g. "io.github.github/github-mcp-server").
	RegistryName string `json:"registryName" validate:"required"`
	// Version is the registry version to install (default: "latest").
	Version string `json:"version,omitempty"`
	// Name overrides the server name in kb.mcp_servers (default: derived from registry name).
	Name string `json:"name,omitempty"`
}

// RegistryServerDTO is the response DTO for a registry server (for search/browse results).
type RegistryServerDTO struct {
	Name        string               `json:"name"`
	Title       string               `json:"title,omitempty"`
	Description string               `json:"description"`
	Version     string               `json:"version"`
	Repository  *RegistryRepoDTO     `json:"repository,omitempty"`
	HasRemotes  bool                 `json:"hasRemotes"`
	HasPackages bool                 `json:"hasPackages"`
	Packages    []RegistryPackageDTO `json:"packages,omitempty"`
	Remotes     []RegistryRemoteDTO  `json:"remotes,omitempty"`
	EnvVars     []RegistryEnvVarDTO  `json:"envVars,omitempty"`
}

// RegistryRepoDTO is the response DTO for a registry server's repository.
type RegistryRepoDTO struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// RegistryPackageDTO is the response DTO for a registry package.
type RegistryPackageDTO struct {
	RegistryType string `json:"registryType"`
	Name         string `json:"name,omitempty"`
	Identifier   string `json:"identifier,omitempty"`
	Transport    string `json:"transport"`
}

// RegistryRemoteDTO is the response DTO for a registry remote.
type RegistryRemoteDTO struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// RegistryEnvVarDTO is the response DTO for a registry environment variable.
type RegistryEnvVarDTO struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
}

// InstallResultDTO wraps the install response with the created server and
// metadata from the registry (e.g. required env vars that need to be configured).
type InstallResultDTO struct {
	Server          *MCPServerDetailDTO `json:"server"`
	RequiredEnvVars []RegistryEnvVarDTO `json:"requiredEnvVars"`
	Message         string              `json:"message,omitempty"`
}

// RegistrySearchResultDTO wraps search results from the official MCP registry.
type RegistrySearchResultDTO struct {
	Servers    []RegistryServerDTO `json:"servers"`
	NextCursor string              `json:"nextCursor,omitempty"`
	Count      int                 `json:"count"`
}

// --- Inspect DTOs ---

// MCPServerInspectDTO is the response for the inspect/test-connection endpoint.
// It captures everything discoverable about an MCP server in a single call:
// connection status, server metadata, capabilities, and enumerations of
// tools, prompts, and resources.
type MCPServerInspectDTO struct {
	// Server identity
	ServerID   string        `json:"serverId"`
	ServerName string        `json:"serverName"`
	ServerType MCPServerType `json:"serverType"`

	// Connection result
	Status    string  `json:"status"` // "ok" or "error"
	Error     *string `json:"error,omitempty"`
	LatencyMs int64   `json:"latencyMs"`

	// Server info from InitializeResult
	ServerInfo *InspectServerInfoDTO `json:"serverInfo,omitempty"`

	// Capabilities advertised by the server
	Capabilities *InspectCapabilitiesDTO `json:"capabilities,omitempty"`

	// Enumerated items (only populated when the server advertises the capability)
	Tools             []InspectToolDTO             `json:"tools"`
	Prompts           []InspectPromptDTO           `json:"prompts"`
	Resources         []InspectResourceDTO         `json:"resources"`
	ResourceTemplates []InspectResourceTemplateDTO `json:"resourceTemplates"`
}

// InspectServerInfoDTO carries the server's self-reported identity.
type InspectServerInfoDTO struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
	Instructions    string `json:"instructions,omitempty"`
}

// InspectCapabilitiesDTO is a simplified view of what the server supports.
type InspectCapabilitiesDTO struct {
	Tools       bool `json:"tools"`
	Prompts     bool `json:"prompts"`
	Resources   bool `json:"resources"`
	Logging     bool `json:"logging"`
	Completions bool `json:"completions"`
}

// InspectToolDTO describes a tool discovered during inspect.
type InspectToolDTO struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// InspectPromptDTO describes a prompt discovered during inspect.
type InspectPromptDTO struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Arguments   []InspectPromptArgDTO `json:"arguments,omitempty"`
}

// InspectPromptArgDTO describes a prompt argument.
type InspectPromptArgDTO struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// InspectResourceDTO describes a resource discovered during inspect.
type InspectResourceDTO struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// InspectResourceTemplateDTO describes a resource template discovered during inspect.
type InspectResourceTemplateDTO struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// --- ToDTO methods ---

// ToDTO converts an MCPServer entity to MCPServerDTO.
func (s *MCPServer) ToDTO() *MCPServerDTO {
	toolCount := 0
	if s.Tools != nil {
		toolCount = len(s.Tools)
	}
	return &MCPServerDTO{
		ID:        s.ID,
		ProjectID: s.ProjectID,
		Name:      s.Name,
		Enabled:   s.Enabled,
		Type:      s.Type,
		Command:   s.Command,
		Args:      s.Args,
		Env:       s.Env,
		URL:       s.URL,
		Headers:   s.Headers,
		ToolCount: toolCount,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// ToDetailDTO converts an MCPServer entity (with loaded tools) to MCPServerDetailDTO.
func (s *MCPServer) ToDetailDTO() *MCPServerDetailDTO {
	dto := &MCPServerDetailDTO{
		MCPServerDTO: *s.ToDTO(),
		Tools:        make([]MCPServerToolDTO, 0),
	}
	for _, t := range s.Tools {
		dto.Tools = append(dto.Tools, *t.ToDTO())
	}
	return dto
}

// ToDTO converts an MCPServerTool entity to MCPServerToolDTO.
func (t *MCPServerTool) ToDTO() *MCPServerToolDTO {
	return &MCPServerToolDTO{
		ID:          t.ID,
		ServerID:    t.ServerID,
		ToolName:    t.ToolName,
		Description: t.Description,
		InputSchema: t.InputSchema,
		Enabled:     t.Enabled,
		CreatedAt:   t.CreatedAt,
	}
}

// --- API Response wrappers ---

// APIResponse wraps API responses with success flag.
type APIResponse[T any] struct {
	Success bool    `json:"success"`
	Data    T       `json:"data,omitempty"`
	Error   *string `json:"error,omitempty"`
	Message *string `json:"message,omitempty"`
}

// SuccessResponse creates a successful API response.
func SuccessResponse[T any](data T) APIResponse[T] {
	return APIResponse[T]{
		Success: true,
		Data:    data,
	}
}

// ErrorResponse creates an error API response.
func ErrorResponse[T any](err string) APIResponse[T] {
	return APIResponse[T]{
		Success: false,
		Error:   &err,
	}
}
