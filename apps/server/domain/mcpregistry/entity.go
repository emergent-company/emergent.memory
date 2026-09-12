package mcpregistry

import (
	"sort"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/httputil"
	"github.com/uptrace/bun"
)

// EncryptedSecret holds an AES-GCM ciphertext and its nonce for a single secret
// value. Go's encoding/json marshals []byte as base64, so both fields are stored
// as base64 strings inside the JSONB columns.
type EncryptedSecret struct {
	Ciphertext []byte `json:"ct"`
	Nonce      []byte `json:"nonce"`
}

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

	ID          string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID   string         `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Name        string         `bun:"name,notnull" json:"name"`
	Description *string        `bun:"description" json:"description,omitempty"`
	Enabled     bool           `bun:"enabled,notnull,default:true" json:"enabled"`
	Type        MCPServerType  `bun:"type,notnull" json:"type"`
	Command     *string        `bun:"command" json:"command,omitempty"`               // for stdio
	Args        []string       `bun:"args,array,default:'{}'" json:"args"`            // for stdio
	Env         map[string]any `bun:"env,type:jsonb,default:'{}'" json:"env"`         // environment vars
	URL         *string        `bun:"url" json:"url,omitempty"`                       // for sse/http
	Headers     map[string]any `bun:"headers,type:jsonb,default:'{}'" json:"headers"` // for sse/http
	// SecretEnv / SecretHeaders hold encrypted secret values. json:"-" ensures
	// they are never serialized; the DTO exposes only key names.
	SecretEnv     map[string]EncryptedSecret `bun:"secret_env,type:jsonb,default:'{}'" json:"-"`
	SecretHeaders map[string]EncryptedSecret `bun:"secret_headers,type:jsonb,default:'{}'" json:"-"`
	CreatedAt     time.Time                  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt     time.Time                  `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`

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
	Config      map[string]any `bun:"config,type:jsonb" json:"config,omitempty"`
	// ConfigKeys lists setup-time configuration keys required by this tool (e.g. ["api_key"]).
	ConfigKeys []string  `bun:"config_keys,array,default:'{}'" json:"configKeys,omitempty"`
	CreatedAt  time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`

	// Relations
	Server *MCPServer `bun:"rel:belongs-to,join:server_id=id" json:"-"`
}

// --- DTOs ---

// MCPServerDTO is the response DTO for an MCP server.
type MCPServerDTO struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"projectId"`
	Name        string         `json:"name"`
	Description *string        `json:"description,omitempty"`
	Enabled     bool           `json:"enabled"`
	Type        MCPServerType  `json:"type"`
	Command     *string        `json:"command,omitempty"`
	Args        []string       `json:"args,omitempty"`
	Env         map[string]any `json:"env,omitempty"`
	URL         *string        `json:"url,omitempty"`
	Headers     map[string]any `json:"headers,omitempty"`
	// SecretEnvKeys/SecretHeadersKeys list the keys stored encrypted at rest.
	// The corresponding values are never returned by the API.
	SecretEnvKeys     []string           `json:"secretEnvKeys"`
	SecretHeadersKeys []string           `json:"secretHeadersKeys"`
	ToolCount         int                `json:"toolCount"`
	Tools             []MCPServerToolDTO `json:"tools,omitempty"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
}

// MCPServerToolDTO is the response DTO for an MCP server tool.
type MCPServerToolDTO struct {
	ID            string         `json:"id"`
	ServerID      string         `json:"serverId"`
	ToolName      string         `json:"toolName"`
	Description   *string        `json:"description,omitempty"`
	InputSchema   map[string]any `json:"inputSchema,omitempty"`
	Enabled       bool           `json:"enabled"`
	Config        map[string]any `json:"config,omitempty"`
	ConfigKeys    []string       `json:"configKeys,omitempty"`
	InheritedFrom string         `json:"inheritedFrom,omitempty"` // "project", "org", or "global"
	CreatedAt     time.Time      `json:"createdAt"`
}

// MCPServerDetailDTO includes the server and its tools.
type MCPServerDetailDTO struct {
	MCPServerDTO
	Tools []MCPServerToolDTO `json:"tools"`
}

// CreateMCPServerDTO is the request DTO for creating an MCP server.
type CreateMCPServerDTO struct {
	Name        string         `json:"name" validate:"required"`
	Description *string        `json:"description"`
	Type        MCPServerType  `json:"type" validate:"required,oneof=stdio sse http"`
	Enabled     *bool          `json:"enabled"`
	Command     *string        `json:"command"`
	Args        []string       `json:"args"`
	Env         map[string]any `json:"env"`
	URL         *string        `json:"url"`
	Headers     map[string]any `json:"headers"`
	// SecretEnvKeys/SecretHeadersKeys name the entries in Env/Headers whose
	// values must be encrypted at rest instead of stored in plaintext.
	SecretEnvKeys     []string `json:"secretEnvKeys"`
	SecretHeadersKeys []string `json:"secretHeadersKeys"`
}

// UpdateMCPServerDTO is the request DTO for updating an MCP server.
type UpdateMCPServerDTO struct {
	Name        *string        `json:"name"`
	Description *string        `json:"description"`
	Enabled     *bool          `json:"enabled"`
	Command     *string        `json:"command"`
	Args        []string       `json:"args"`
	Env         map[string]any `json:"env"`
	URL         *string        `json:"url"`
	Headers     map[string]any `json:"headers"`
	// SecretEnvKeys/SecretHeadersKeys name the entries in Env/Headers whose
	// values must be encrypted at rest. A key listed with an empty/omitted
	// value keeps its previously stored ciphertext; keys no longer listed are
	// removed.
	SecretEnvKeys     []string `json:"secretEnvKeys"`
	SecretHeadersKeys []string `json:"secretHeadersKeys"`
}

// UpdateMCPServerToolDTO is the request DTO for toggling a tool and/or updating its config.
type UpdateMCPServerToolDTO struct {
	Enabled *bool           `json:"enabled"`
	Config  *map[string]any `json:"config,omitempty"`
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
	tools := make([]MCPServerToolDTO, 0, len(s.Tools))
	for _, t := range s.Tools {
		toolCount++
		tools = append(tools, *t.ToDTO())
	}
	return &MCPServerDTO{
		ID:                s.ID,
		ProjectID:         s.ProjectID,
		Name:              s.Name,
		Description:       s.Description,
		Enabled:           s.Enabled,
		Type:              s.Type,
		Command:           s.Command,
		Args:              s.Args,
		Env:               stripSecretKeys(s.Env, s.SecretEnv),
		URL:               s.URL,
		Headers:           stripSecretKeys(s.Headers, s.SecretHeaders),
		SecretEnvKeys:     sortedSecretKeys(s.SecretEnv),
		SecretHeadersKeys: sortedSecretKeys(s.SecretHeaders),
		ToolCount:         toolCount,
		Tools:             tools,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
	}
}

// stripSecretKeys returns a copy of values with every key present in secrets
// removed. It returns nil when values is nil, and an empty (non-nil) map when
// all entries were secret, so callers can distinguish "no values" from "all
// values were secret".
func stripSecretKeys(values map[string]any, secrets map[string]EncryptedSecret) map[string]any {
	if values == nil {
		return nil
	}
	if len(secrets) == 0 {
		return values
	}
	out := make(map[string]any, len(values))
	for k, v := range values {
		if _, secret := secrets[k]; secret {
			continue
		}
		out[k] = v
	}
	return out
}

// sortedSecretKeys returns the keys of an encrypted-secret map in deterministic
// (lexicographic) order. Always non-nil so JSON emits [] rather than null.
func sortedSecretKeys(secrets map[string]EncryptedSecret) []string {
	keys := make([]string, 0, len(secrets))
	for k := range secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
		Config:      t.Config,
		ConfigKeys:  t.ConfigKeys,
		CreatedAt:   t.CreatedAt,
	}
}

// --- API Response wrappers ---

// APIResponse wraps API responses with success flag.
// Alias for httputil.APIResponse — single source of truth in pkg/httputil.
type APIResponse[T any] = httputil.APIResponse[T]

// SuccessResponse creates a successful API response.
func SuccessResponse[T any](data T) APIResponse[T] {
	return httputil.NewSuccessResponse(data)
}

// ErrorResponse creates an error API response.
func ErrorResponse[T any](err string) APIResponse[T] {
	return httputil.NewErrorResponse[T](err)
}
