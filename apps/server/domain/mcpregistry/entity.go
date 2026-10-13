package mcpregistry

import (
	"fmt"
	"sort"
	"strings"
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

// RequiredConfigKey describes a single credential key (header or env) a server
// needs before it can be enabled. Kind is "header" or "env". IsRequired is true
// when the registry declared the key required or secret (a secret with no value
// is unusable).
//
// Description and ValueHint carry the registry's own guidance for the key so
// the configuration UI can tell the user what to enter (e.g. GitHub's
// Authorization header: "Authorization header with authentication token (PAT
// or App token)"). Both are best-effort: registry entries may omit them.
type RequiredConfigKey struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"` // "header" | "env"
	IsSecret    bool   `json:"isSecret"`
	IsRequired  bool   `json:"isRequired"`
	Description string `json:"description,omitempty"`
	// ValueHint is the registry-declared value template (header transports
	// only), e.g. "Bearer {NETDATA_CLOUD_API_TOKEN}". It is surfaced as an
	// input placeholder, never prefilled, so it never leaks a credential.
	ValueHint string `json:"valueHint,omitempty"`
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

// MCPServerTarget defines whether a registry row is connected outbound by the
// server ('server') or delivered to a connected relay device ('device').
type MCPServerTarget string

const (
	TargetServer MCPServerTarget = "server"
	TargetDevice MCPServerTarget = "device"
)

// ValidTarget reports whether t is a known target.
func ValidTarget(t MCPServerTarget) bool {
	return t == TargetServer || t == TargetDevice
}

// MCPServer represents a registered MCP server configuration.
// Table: kb.mcp_servers
type MCPServer struct {
	bun.BaseModel `bun:"table:kb.mcp_servers,alias:ms"`

	ID          string  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ProjectID   string  `bun:"project_id,type:uuid,notnull" json:"projectId"`
	Name        string  `bun:"name,notnull" json:"name"`
	Description *string `bun:"description" json:"description,omitempty"`
	// No bun `default:true` tag: bun omits a column from INSERT when its value is
	// the Go zero value and the tag declares a SQL default, so an explicit
	// Enabled=false (e.g. a registry install with required config) would be
	// silently replaced by the column default `true`. The column keeps its DB
	// default for raw inserts; every code path sets Enabled explicitly.
	Enabled bool           `bun:"enabled,notnull" json:"enabled"`
	Type    MCPServerType  `bun:"type,notnull" json:"type"`
	Command *string        `bun:"command" json:"command,omitempty"`               // for stdio
	Args    []string       `bun:"args,array,default:'{}'" json:"args"`            // for stdio
	Env     map[string]any `bun:"env,type:jsonb,default:'{}'" json:"env"`         // environment vars
	URL     *string        `bun:"url" json:"url,omitempty"`                       // for sse/http
	Headers map[string]any `bun:"headers,type:jsonb,default:'{}'" json:"headers"` // for sse/http
	// SecretEnv / SecretHeaders hold encrypted secret values. json:"-" ensures
	// they are never serialized; the DTO exposes only key names.
	SecretEnv     map[string]EncryptedSecret `bun:"secret_env,type:jsonb,default:'{}'" json:"-"`
	SecretHeaders map[string]EncryptedSecret `bun:"secret_headers,type:jsonb,default:'{}'" json:"-"`
	// RequiredConfig records the credential keys (header/env) the server needs
	// before it can be enabled. Populated on registry install from the selected
	// transport's declaration; empty for manual creates. JSONB:
	// [{"name","kind","isSecret","isRequired"}].
	RequiredConfig []RequiredConfigKey `bun:"required_config,type:jsonb,default:'[]'" json:"requiredConfig"`
	// Target selects the delivery mode: 'server' (connected outbound) or
	// 'device' (pushed over the relay). TargetInstanceID is the relay instance_id
	// for a device row; nil means every eligible device of the project.
	Target           MCPServerTarget `bun:"target,notnull,default:'server'" json:"target"`
	TargetInstanceID *string         `bun:"target_instance_id" json:"targetInstanceId,omitempty"`
	// RegistryName/RegistryRepositoryURL record where a registry-installed server
	// came from. Populated only by registry installs; nil for manual creates.
	// RepositoryURL backs the configure form's "where to get a token" link.
	RegistryName          *string   `bun:"registry_name" json:"registryName,omitempty"`
	RegistryRepositoryURL *string   `bun:"registry_repository_url" json:"registryRepositoryUrl,omitempty"`
	CreatedAt             time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt             time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updatedAt"`

	// Relations
	Tools []*MCPServerTool `bun:"rel:has-many,join:id=server_id" json:"tools,omitempty"`
}

// MCPServerTool represents a cached tool definition from an MCP server.
// Table: kb.mcp_server_tools
type MCPServerTool struct {
	bun.BaseModel `bun:"table:kb.mcp_server_tools,alias:mst"`

	ID           string         `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	ServerID     string         `bun:"server_id,type:uuid,notnull" json:"serverId"`
	ToolName     string         `bun:"tool_name,notnull" json:"toolName"`
	Description  *string        `bun:"description" json:"description,omitempty"`
	InputSchema  map[string]any `bun:"input_schema,type:jsonb,default:'{}'" json:"inputSchema"`
	OutputSchema map[string]any `bun:"output_schema,type:jsonb" json:"outputSchema,omitempty"`
	Enabled      bool           `bun:"enabled,notnull,default:true" json:"enabled"`
	// EnabledOverride marks an explicit project-level toggle (issue #988). Nil means
	// "no project override" and falls through to org default, then the builtin
	// default; non-nil means the project explicitly enabled/disabled this tool.
	EnabledOverride *bool          `bun:"enabled_override" json:"enabledOverride,omitempty"`
	Config          map[string]any `bun:"config,type:jsonb" json:"config,omitempty"`
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
	Target            MCPServerTarget    `json:"target"`
	TargetInstanceID  *string            `json:"targetInstanceId,omitempty"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
	// RequiredConfig lists the credential keys (header/env) the server needs
	// before it can be enabled; NeedsConfiguration is true while any required
	// key is still missing; MissingConfigKeys names the still-missing ones.
	RequiredConfig     []RequiredConfigKey `json:"requiredConfig"`
	NeedsConfiguration bool                `json:"needsConfiguration"`
	MissingConfigKeys  []string            `json:"missingConfigKeys"`
	// RegistryName/RegistryRepositoryURL identify the registry entry a server was
	// installed from (empty for manual creates); the client uses the repository
	// URL to link to provider docs for credential setup.
	RegistryName          *string `json:"registryName,omitempty"`
	RegistryRepositoryURL *string `json:"registryRepositoryUrl,omitempty"`
}

// MCPServerToolDTO is the response DTO for an MCP server tool.
type MCPServerToolDTO struct {
	ID            string         `json:"id"`
	ServerID      string         `json:"serverId"`
	ToolName      string         `json:"toolName"`
	Description   *string        `json:"description,omitempty"`
	InputSchema   map[string]any `json:"inputSchema,omitempty"`
	OutputSchema  map[string]any `json:"outputSchema,omitempty"`
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
	// Target selects the delivery mode ('server' default | 'device').
	Target MCPServerTarget `json:"target"`
	// TargetInstanceID is the relay instance_id for a device row; nil = broadcast.
	TargetInstanceID *string `json:"targetInstanceId,omitempty"`
	// RequiredConfig lists the credential keys (header/env) the server needs
	// before it can be enabled. Populated by registry install; manual creates
	// may omit it (stored as an empty list).
	RequiredConfig []RequiredConfigKey `json:"requiredConfig"`
	// RegistryName/RegistryRepositoryURL record registry provenance on an
	// install; manual creates leave them nil.
	RegistryName          *string `json:"registryName,omitempty"`
	RegistryRepositoryURL *string `json:"registryRepositoryUrl,omitempty"`
}

// UpdateMCPServerDTO is the request DTO for updating an MCP server. Unlike
// CreateMCPServerDTO, Type is a pointer: a nil Type leaves the transport
// unchanged, while a non-nil Type may switch the server's transport (clearing
// the connection fields of the transport being left behind).
type UpdateMCPServerDTO struct {
	Name        *string        `json:"name"`
	Description *string        `json:"description"`
	Type        *MCPServerType `json:"type"`
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
	// Target, when non-nil, switches the delivery mode ('server' | 'device').
	Target *MCPServerTarget `json:"target"`
	// TargetInstanceID is the relay instance_id for a device row; nil = broadcast.
	TargetInstanceID *string `json:"targetInstanceId,omitempty"`
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
	// Target selects the delivery mode: 'server' (default) installs an outbound
	// server on the shared host; 'device' installs the entry's local package on
	// a connected connector and pushes the config over the relay.
	Target MCPServerTarget `json:"target,omitempty"`
	// TargetInstanceID names the relay instance_id to install to; nil with
	// Target 'device' broadcasts to every opted-in device (project admins only).
	TargetInstanceID *string `json:"targetInstanceId,omitempty"`
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
	Headers     []RegistryHeaderDTO  `json:"headers,omitempty"`
}

// RegistryRepoDTO is the response DTO for a registry server's repository.
type RegistryRepoDTO struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// RegistryPackageDTO is the response DTO for a registry package.
type RegistryPackageDTO struct {
	RegistryType     string   `json:"registryType"`
	Name             string   `json:"name,omitempty"`
	Identifier       string   `json:"identifier,omitempty"`
	Version          string   `json:"version,omitempty"`
	Transport        string   `json:"transport"`
	RuntimeArguments []string `json:"runtimeArguments,omitempty"`
	PackageArguments []string `json:"packageArguments,omitempty"`
}

// RegistryRemoteDTO is the response DTO for a registry remote. Headers and
// EnvironmentVariables are the credentials this specific remote declares, so
// clients can preview what an install will require before installing.
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

// RegistryHeaderDTO is the response DTO for a registry-declared HTTP header.
type RegistryHeaderDTO struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
	Value       string `json:"value,omitempty"`
}

// InstallResultDTO wraps the install response with the created server and
// metadata from the registry (e.g. required env vars that need to be configured).
type InstallResultDTO struct {
	Server          *MCPServerDetailDTO `json:"server"`
	RequiredEnvVars []RegistryEnvVarDTO `json:"requiredEnvVars"`
	Message         string              `json:"message,omitempty"`
	// RequiredConfig lists the credential keys (header/env) resolved from the
	// selected transport; the UI uses it to guide the user through configuration.
	RequiredConfig []RequiredConfigKey `json:"requiredConfig"`
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
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema,omitempty"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
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
	missing := missingRequiredConfigKeys(s)
	return &MCPServerDTO{
		ID:                 s.ID,
		ProjectID:          s.ProjectID,
		Name:               s.Name,
		Description:        s.Description,
		Enabled:            s.Enabled,
		Type:               s.Type,
		Command:            s.Command,
		Args:               s.Args,
		Env:                stripSecretKeys(s.Env, s.SecretEnv),
		URL:                s.URL,
		Headers:            stripSecretKeys(s.Headers, s.SecretHeaders),
		SecretEnvKeys:      sortedSecretKeys(s.SecretEnv),
		SecretHeadersKeys:  sortedSecretKeys(s.SecretHeaders),
		ToolCount:          toolCount,
		Tools:              tools,
		Target:             s.Target,
		TargetInstanceID:   s.TargetInstanceID,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
		RequiredConfig:     nonNilRequiredConfig(s.RequiredConfig),
		NeedsConfiguration: len(missing) > 0,
		MissingConfigKeys:  missing,

		RegistryName:          s.RegistryName,
		RegistryRepositoryURL: s.RegistryRepositoryURL,
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

// nonNilRequiredConfig returns required as-is when non-nil, and an empty (non-nil)
// slice otherwise, so JSON emits [] rather than null.
func nonNilRequiredConfig(required []RequiredConfigKey) []RequiredConfigKey {
	if required == nil {
		return []RequiredConfigKey{}
	}
	return required
}

// stringify renders a map[string]any value as a string: nil → "", strings pass
// through unchanged, anything else uses the same %v formatting as the stdio env
// encoder.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// missingRequiredConfigKeys returns the names of a server's required config keys
// whose value is still blank and that have no stored secret. Deterministic:
// names are sorted and deduped. Env/Headers are map[string]any; nil maps and
// non-string values are handled via stringify.
//
// A key is missing when:
//   - kind == "header": Headers[name] is blank AND SecretHeaders[name] is absent
//     (or holds an empty ciphertext).
//   - kind == "env": Env[name] is blank AND SecretEnv[name] is absent.
func missingRequiredConfigKeys(s *MCPServer) []string {
	if s == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(s.RequiredConfig))
	missing := make([]string, 0, len(s.RequiredConfig))
	for _, key := range s.RequiredConfig {
		if key.Name == "" {
			continue
		}
		if _, dup := seen[key.Name]; dup {
			continue
		}
		seen[key.Name] = struct{}{}
		if !requiredKeyMissing(s, key) {
			continue
		}
		missing = append(missing, key.Name)
	}
	sort.Strings(missing)
	return missing
}

// requiredKeyMissing reports whether a single required key is still blank.
func requiredKeyMissing(s *MCPServer, key RequiredConfigKey) bool {
	switch key.Kind {
	case "header":
		if strings.TrimSpace(stringify(s.Headers[key.Name])) != "" {
			return false
		}
		sec, ok := s.SecretHeaders[key.Name]
		return !ok || len(sec.Ciphertext) == 0
	case "env":
		if strings.TrimSpace(stringify(s.Env[key.Name])) != "" {
			return false
		}
		_, ok := s.SecretEnv[key.Name]
		return !ok
	default:
		return false
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
		ID:           t.ID,
		ServerID:     t.ServerID,
		ToolName:     t.ToolName,
		Description:  t.Description,
		InputSchema:  t.InputSchema,
		OutputSchema: t.OutputSchema,
		Enabled:      t.Enabled,
		Config:       t.Config,
		ConfigKeys:   t.ConfigKeys,
		CreatedAt:    t.CreatedAt,
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
