package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/emergent/emergent-core/domain/mcp"
	"github.com/emergent/emergent-core/pkg/logger"
)

// Service handles business logic for the MCP server registry.
type Service struct {
	repo           *Repository
	mcpService     *mcp.Service
	proxy          *ProxyManager
	registryClient *RegistryClient
	log            *slog.Logger

	// Track registered builtin servers per project to avoid re-registration
	mu                sync.Mutex
	builtinRegistered map[string]bool

	// ToolPool invalidation callback (set via SetToolPoolInvalidator to break circular import)
	toolPoolInvalidator ToolPoolInvalidator
}

// NewService creates a new MCP registry service.
func NewService(repo *Repository, mcpService *mcp.Service, registryClient *RegistryClient, log *slog.Logger) *Service {
	return &Service{
		repo:              repo,
		mcpService:        mcpService,
		proxy:             NewProxyManager(repo, log),
		registryClient:    registryClient,
		log:               log.With(logger.Scope("mcpregistry.svc")),
		builtinRegistered: make(map[string]bool),
	}
}

// SetToolPoolInvalidator sets the callback used to invalidate the ToolPool cache
// when MCP server configurations change. Called after construction via fx.Invoke
// to break the circular dependency (mcpregistry cannot import agents).
func (s *Service) SetToolPoolInvalidator(inv ToolPoolInvalidator) {
	s.toolPoolInvalidator = inv
}

// invalidateToolPool notifies the ToolPool to rebuild its cache for a project.
// No-op if no invalidator is set.
func (s *Service) invalidateToolPool(projectID string) {
	if s.toolPoolInvalidator != nil {
		s.toolPoolInvalidator.InvalidateCache(projectID)
		s.log.Debug("invalidated tool pool cache",
			slog.String("project_id", projectID),
		)
	}
}

// EnsureBuiltinServer registers or updates the builtin MCP server for a project.
// This creates a single "builtin" type server entry with all current MCP tool
// definitions from domain/mcp/service.go.
//
// Called lazily on first access per project (not on startup for all projects).
func (s *Service) EnsureBuiltinServer(ctx context.Context, projectID string) error {
	s.mu.Lock()
	if s.builtinRegistered[projectID] {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	const builtinServerName = "builtin"

	// Check if builtin server already exists
	existing, err := s.repo.FindServerByName(ctx, projectID, builtinServerName)
	if err != nil {
		return fmt.Errorf("checking builtin server: %w", err)
	}

	var serverID string
	if existing == nil {
		// Create the builtin server
		server := &MCPServer{
			ProjectID: projectID,
			Name:      builtinServerName,
			Enabled:   true,
			Type:      ServerTypeBuiltin,
		}
		if err := s.repo.CreateServer(ctx, server); err != nil {
			return fmt.Errorf("creating builtin server: %w", err)
		}
		serverID = server.ID
		s.log.Info("created builtin MCP server entry",
			slog.String("project_id", projectID),
			slog.String("server_id", serverID),
		)
	} else {
		serverID = existing.ID
	}

	// Sync all current tool definitions
	builtinDefs := s.mcpService.GetToolDefinitions()
	tools := make([]*MCPServerTool, 0, len(builtinDefs))
	for _, td := range builtinDefs {
		inputSchema := schemaToMap(td.InputSchema)
		desc := td.Description
		tools = append(tools, &MCPServerTool{
			ServerID:    serverID,
			ToolName:    td.Name,
			Description: &desc,
			InputSchema: inputSchema,
			Enabled:     true,
		})
	}

	if err := s.repo.BulkUpsertTools(ctx, tools); err != nil {
		return fmt.Errorf("syncing builtin tools: %w", err)
	}

	// Clean up tools that no longer exist in the builtin set
	currentNames := make([]string, len(builtinDefs))
	for i, td := range builtinDefs {
		currentNames[i] = td.Name
	}
	staleCount, err := s.repo.DeleteStaleTools(ctx, serverID, currentNames)
	if err != nil {
		return fmt.Errorf("cleaning stale builtin tools: %w", err)
	}
	if staleCount > 0 {
		s.log.Info("removed stale builtin tools",
			slog.String("project_id", projectID),
			slog.Int("removed", staleCount),
		)
	}

	s.mu.Lock()
	s.builtinRegistered[projectID] = true
	s.mu.Unlock()

	s.log.Debug("ensured builtin MCP server",
		slog.String("project_id", projectID),
		slog.String("server_id", serverID),
		slog.Int("tool_count", len(builtinDefs)),
	)

	return nil
}

// --- CRUD for MCP Servers ---

// ListServers returns all MCP servers for a project.
func (s *Service) ListServers(ctx context.Context, projectID string) ([]*MCPServer, error) {
	return s.repo.FindAllServers(ctx, projectID)
}

// GetServer returns an MCP server by ID.
func (s *Service) GetServer(ctx context.Context, id string, projectID *string) (*MCPServer, error) {
	return s.repo.FindServerByIDWithTools(ctx, id, projectID)
}

// CreateServer creates a new external MCP server.
func (s *Service) CreateServer(ctx context.Context, projectID string, dto *CreateMCPServerDTO) (*MCPServer, error) {
	// Validate: cannot create builtin type via API
	if dto.Type == ServerTypeBuiltin {
		return nil, fmt.Errorf("cannot create servers of type 'builtin' via API")
	}

	// Validate transport-specific fields
	switch dto.Type {
	case ServerTypeStdio:
		if dto.Command == nil || *dto.Command == "" {
			return nil, fmt.Errorf("command is required for stdio-type servers")
		}
	case ServerTypeSSE, ServerTypeHTTP:
		if dto.URL == nil || *dto.URL == "" {
			return nil, fmt.Errorf("url is required for %s-type servers", dto.Type)
		}
	}

	// Check for name collision
	existing, err := s.repo.FindServerByName(ctx, projectID, dto.Name)
	if err != nil {
		return nil, fmt.Errorf("checking name uniqueness: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("server with name %q already exists in this project", dto.Name)
	}

	enabled := true
	if dto.Enabled != nil {
		enabled = *dto.Enabled
	}

	server := &MCPServer{
		ProjectID: projectID,
		Name:      dto.Name,
		Enabled:   enabled,
		Type:      dto.Type,
		Command:   dto.Command,
		Args:      dto.Args,
		Env:       dto.Env,
		URL:       dto.URL,
		Headers:   dto.Headers,
	}

	if err := s.repo.CreateServer(ctx, server); err != nil {
		return nil, fmt.Errorf("creating server: %w", err)
	}

	s.log.Info("created MCP server",
		slog.String("project_id", projectID),
		slog.String("server_id", server.ID),
		slog.String("name", server.Name),
		slog.String("type", string(server.Type)),
	)

	s.invalidateToolPool(projectID)

	return server, nil
}

// UpdateServer updates an existing MCP server.
func (s *Service) UpdateServer(ctx context.Context, id string, projectID string, dto *UpdateMCPServerDTO) (*MCPServer, error) {
	server, err := s.repo.FindServerByID(ctx, id, &projectID)
	if err != nil {
		return nil, fmt.Errorf("fetching server: %w", err)
	}
	if server == nil {
		return nil, nil
	}

	// Cannot modify builtin servers (except enable/disable)
	if server.Type == ServerTypeBuiltin {
		if dto.Name != nil || dto.Command != nil || dto.URL != nil || dto.Args != nil || dto.Env != nil || dto.Headers != nil {
			return nil, fmt.Errorf("cannot modify builtin server configuration — only enable/disable is allowed")
		}
	}

	if dto.Name != nil {
		// Check for name collision
		existing, err := s.repo.FindServerByName(ctx, projectID, *dto.Name)
		if err != nil {
			return nil, fmt.Errorf("checking name uniqueness: %w", err)
		}
		if existing != nil && existing.ID != id {
			return nil, fmt.Errorf("server with name %q already exists in this project", *dto.Name)
		}
		server.Name = *dto.Name
	}
	if dto.Enabled != nil {
		server.Enabled = *dto.Enabled
	}
	if dto.Command != nil {
		server.Command = dto.Command
	}
	if dto.Args != nil {
		server.Args = dto.Args
	}
	if dto.Env != nil {
		server.Env = dto.Env
	}
	if dto.URL != nil {
		server.URL = dto.URL
	}
	if dto.Headers != nil {
		server.Headers = dto.Headers
	}

	if err := s.repo.UpdateServer(ctx, server); err != nil {
		return nil, fmt.Errorf("updating server: %w", err)
	}

	// Evict proxy connection so next call uses updated config
	s.proxy.CloseServer(id)

	s.invalidateToolPool(projectID)

	return server, nil
}

// DeleteServer deletes an MCP server.
func (s *Service) DeleteServer(ctx context.Context, id string, projectID string) error {
	server, err := s.repo.FindServerByID(ctx, id, &projectID)
	if err != nil {
		return fmt.Errorf("fetching server: %w", err)
	}
	if server == nil {
		return fmt.Errorf("server not found")
	}

	// Cannot delete builtin server
	if server.Type == ServerTypeBuiltin {
		return fmt.Errorf("cannot delete builtin server")
	}

	if err := s.repo.DeleteServer(ctx, id); err != nil {
		return fmt.Errorf("deleting server: %w", err)
	}

	// Close any active proxy connection
	s.proxy.CloseServer(id)

	s.log.Info("deleted MCP server",
		slog.String("project_id", projectID),
		slog.String("server_id", id),
		slog.String("name", server.Name),
	)

	s.invalidateToolPool(projectID)

	return nil
}

// --- Tool management ---

// ListTools returns all tools for a server.
func (s *Service) ListTools(ctx context.Context, serverID string) ([]*MCPServerTool, error) {
	return s.repo.FindToolsByServerID(ctx, serverID)
}

// ToggleTool enables or disables a specific tool.
func (s *Service) ToggleTool(ctx context.Context, toolID string, enabled bool) error {
	tool, err := s.repo.FindToolByID(ctx, toolID)
	if err != nil {
		return fmt.Errorf("fetching tool: %w", err)
	}
	if tool == nil {
		return fmt.Errorf("tool not found")
	}

	if err := s.repo.UpdateToolEnabled(ctx, toolID, enabled); err != nil {
		return err
	}

	// Look up the server to get project ID for cache invalidation
	server, err := s.repo.FindServerByID(ctx, tool.ServerID, nil)
	if err == nil && server != nil {
		s.invalidateToolPool(server.ProjectID)
	}

	return nil
}

// SyncServerTools discovers tools from an external MCP server and updates the registry.
// For now this accepts tools directly (caller is responsible for connecting and calling tools/list).
// In the future, the proxy layer will handle the connection.
func (s *Service) SyncServerTools(ctx context.Context, serverID string, discoveredTools []DiscoveredTool) error {
	// Upsert discovered tools
	tools := make([]*MCPServerTool, 0, len(discoveredTools))
	currentNames := make([]string, 0, len(discoveredTools))

	for _, dt := range discoveredTools {
		tools = append(tools, &MCPServerTool{
			ServerID:    serverID,
			ToolName:    dt.Name,
			Description: dt.Description,
			InputSchema: dt.InputSchema,
			Enabled:     true,
		})
		currentNames = append(currentNames, dt.Name)
	}

	if err := s.repo.BulkUpsertTools(ctx, tools); err != nil {
		return fmt.Errorf("upserting discovered tools: %w", err)
	}

	// Remove tools that no longer exist
	staleCount, err := s.repo.DeleteStaleTools(ctx, serverID, currentNames)
	if err != nil {
		return fmt.Errorf("cleaning stale tools: %w", err)
	}

	s.log.Info("synced server tools",
		slog.String("server_id", serverID),
		slog.Int("discovered", len(discoveredTools)),
		slog.Int("stale_removed", staleCount),
	)

	// Invalidate ToolPool cache — look up server to get project ID
	server, err := s.repo.FindServerByID(ctx, serverID, nil)
	if err == nil && server != nil {
		s.invalidateToolPool(server.ProjectID)
	}

	return nil
}

// GetEnabledToolsForProject returns all enabled tools from enabled external
// (non-builtin) servers for a project. Used by ToolPool.buildCache().
func (s *Service) GetEnabledToolsForProject(ctx context.Context, projectID string) ([]*EnabledServerTool, error) {
	return s.repo.FindAllEnabledTools(ctx, projectID)
}

// --- External Tool Proxy ---

// InspectServer performs a diagnostic test-connection to an MCP server.
// It creates a fresh ephemeral connection (not pooled), captures the
// InitializeResult, and enumerates tools/prompts/resources based on
// what the server advertises. The connection is closed when done.
func (s *Service) InspectServer(ctx context.Context, serverID string, projectID string) (*MCPServerInspectDTO, error) {
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return nil, fmt.Errorf("fetching server: %w", err)
	}
	if server == nil {
		return nil, nil
	}

	// Builtin servers don't have external connections to inspect
	if server.Type == ServerTypeBuiltin {
		return &MCPServerInspectDTO{
			ServerID:          server.ID,
			ServerName:        server.Name,
			ServerType:        server.Type,
			Status:            "ok",
			LatencyMs:         0,
			Tools:             []InspectToolDTO{},
			Prompts:           []InspectPromptDTO{},
			Resources:         []InspectResourceDTO{},
			ResourceTemplates: []InspectResourceTemplateDTO{},
			ServerInfo: &InspectServerInfoDTO{
				Name:            "emergent-builtin",
				Version:         "1.0.0",
				ProtocolVersion: "N/A",
			},
			Capabilities: &InspectCapabilitiesDTO{
				Tools: true,
			},
		}, nil
	}

	return s.proxy.InspectServer(ctx, server)
}

// CallExternalTool connects to the appropriate external MCP server and forwards
// a tool call. The prefixedToolName includes the server name prefix (e.g.
// "myserver_search"). The proxy strips the prefix and forwards to the real server.
func (s *Service) CallExternalTool(ctx context.Context, projectID, prefixedToolName string, args map[string]any) (*mcp.ToolResult, error) {
	return s.proxy.CallTool(ctx, projectID, prefixedToolName, args)
}

// DiscoverAndSyncTools connects to an external MCP server, calls tools/list,
// and syncs the discovered tools to the database. This replaces the manual
// SyncServerTools flow with automatic discovery.
func (s *Service) DiscoverAndSyncTools(ctx context.Context, serverID string, projectID string) ([]DiscoveredTool, error) {
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return nil, fmt.Errorf("fetching server: %w", err)
	}
	if server == nil {
		return nil, fmt.Errorf("server not found")
	}
	if server.Type == ServerTypeBuiltin {
		return nil, fmt.Errorf("cannot discover tools from builtin server — use EnsureBuiltinServer instead")
	}

	// Connect and discover tools
	discovered, err := s.proxy.DiscoverTools(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("discovering tools: %w", err)
	}

	// Sync discovered tools to database
	if err := s.SyncServerTools(ctx, serverID, discovered); err != nil {
		return nil, fmt.Errorf("syncing discovered tools: %w", err)
	}

	return discovered, nil
}

// CloseProxyConnection closes the proxy connection for a specific server.
// Call this when server config changes to force a reconnect on next use.
func (s *Service) CloseProxyConnection(serverID string) {
	s.proxy.CloseServer(serverID)
}

// Close shuts down the service and all proxy connections.
func (s *Service) Close() {
	s.proxy.Close()
}

// IsExternalTool returns true if the tool name is a prefixed external tool
// (i.e. it contains at least one underscore separating server name from tool name).
func IsExternalTool(toolName string) bool {
	_, _, err := ParsePrefixedToolName(toolName)
	return err == nil
}

// --- Official Registry Browse/Install ---

// SearchRegistry searches the official MCP registry for servers.
func (s *Service) SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*RegistrySearchResultDTO, error) {
	resp, err := s.registryClient.SearchServers(ctx, query, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("searching registry: %w", err)
	}

	servers := make([]RegistryServerDTO, 0, len(resp.Servers))
	for _, entry := range resp.Servers {
		servers = append(servers, registryServerToDTO(entry.Server))
	}

	return &RegistrySearchResultDTO{
		Servers:    servers,
		NextCursor: resp.Metadata.NextCursor,
		Count:      resp.Metadata.Count,
	}, nil
}

// GetRegistryServer fetches a single server from the official MCP registry.
func (s *Service) GetRegistryServer(ctx context.Context, name string) (*RegistryServerDTO, error) {
	resp, err := s.registryClient.GetServer(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("fetching registry server: %w", err)
	}

	dto := registryServerToDTO(resp.Server)
	return &dto, nil
}

// InstallFromRegistry installs a server from the official MCP registry into the
// local project. It fetches the server definition, selects the best transport
// (remote-first: streamable-http > sse > stdio packages), creates an entry in
// kb.mcp_servers, and triggers tool discovery for remote transports.
func (s *Service) InstallFromRegistry(ctx context.Context, projectID string, dto *InstallFromRegistryDTO) (*InstallResultDTO, error) {
	version := dto.Version
	if version == "" {
		version = "latest"
	}

	// Fetch the server definition from the official registry
	resp, err := s.registryClient.GetServerVersion(ctx, dto.RegistryName, version)
	if err != nil {
		return nil, fmt.Errorf("fetching from registry: %w", err)
	}

	regServer := resp.Server

	// Determine the local name for this server
	localName := dto.Name
	if localName == "" {
		localName = deriveLocalName(regServer.Name)
	}

	// Select the best transport and build the CreateMCPServerDTO
	createDTO, err := selectTransport(regServer, localName)
	if err != nil {
		return nil, fmt.Errorf("selecting transport: %w", err)
	}

	// Create the server in the local registry
	server, err := s.CreateServer(ctx, projectID, createDTO)
	if err != nil {
		return nil, fmt.Errorf("creating server: %w", err)
	}

	s.log.Info("installed MCP server from registry",
		slog.String("project_id", projectID),
		slog.String("server_id", server.ID),
		slog.String("registry_name", dto.RegistryName),
		slog.String("local_name", localName),
		slog.String("type", string(createDTO.Type)),
	)

	// For remote transports (sse/http), attempt tool discovery immediately.
	// For stdio, tools can only be discovered after the process starts
	// (which requires env vars to be configured first).
	if createDTO.Type == ServerTypeSSE || createDTO.Type == ServerTypeHTTP {
		discovered, err := s.DiscoverAndSyncTools(ctx, server.ID, projectID)
		if err != nil {
			// Don't fail the install, just log the warning.
			// The user can manually sync tools later once env vars are configured.
			s.log.Warn("auto-discover failed after install (env vars may need configuration)",
				slog.String("server_id", server.ID),
				slog.String("error", err.Error()),
			)
		} else {
			s.log.Info("auto-discovered tools after install",
				slog.String("server_id", server.ID),
				slog.Int("tool_count", len(discovered)),
			)
		}
	}

	// Re-fetch the server with tools to return complete data
	result, err := s.GetServer(ctx, server.ID, &projectID)
	if err != nil {
		// Fall back to the original server without tools
		result = server
	}

	// Collect env var metadata from the registry definition
	envVars := collectRegistryEnvVars(regServer)

	// Build a helpful status message
	message := ""
	if len(envVars) > 0 {
		names := make([]string, len(envVars))
		for i, ev := range envVars {
			names[i] = ev.Name
		}
		message = fmt.Sprintf("Server installed. Configure required environment variables before use: %s. Use the update_mcp_server tool or PATCH /api/admin/mcp-servers/%s to set env values.", strings.Join(names, ", "), server.ID)
	}

	return &InstallResultDTO{
		Server:          result.ToDetailDTO(),
		RequiredEnvVars: envVars,
		Message:         message,
	}, nil
}

// --- Helpers for Registry Install ---

// selectTransport picks the best remote transport from a registry server definition
// and constructs a CreateMCPServerDTO.
//
// Only remote transports (HTTP/SSE) are supported for registry installs.
// Stdio-based packages (npm/pypi/oci) are blocked because they spawn arbitrary
// processes on the host. Stdio servers can still be created manually via the
// CRUD API for controlled environments.
//
// Priority:
//  1. Remote with streamable-http → type=http
//  2. Remote with sse → type=sse
func selectTransport(regServer RegistryServer, localName string) (*CreateMCPServerDTO, error) {
	// Try remotes (prefer streamable-http over sse)
	for _, remote := range regServer.Remotes {
		if remote.Type == "streamable-http" {
			return buildRemoteDTO(localName, ServerTypeHTTP, remote), nil
		}
	}
	for _, remote := range regServer.Remotes {
		if remote.Type == "sse" {
			return buildRemoteDTO(localName, ServerTypeSSE, remote), nil
		}
	}

	// No remote transport available — stdio packages are not supported for registry installs
	hasPackages := len(regServer.Packages) > 0
	if hasPackages {
		pkgTypes := make([]string, 0, len(regServer.Packages))
		for _, pkg := range regServer.Packages {
			pkgTypes = append(pkgTypes, pkg.RegistryType)
		}
		return nil, fmt.Errorf(
			"server %q only offers stdio-based packages (%s) which are not supported for registry installs — "+
				"only remote transports (HTTP/SSE) are allowed. "+
				"You can manually create a stdio server via the CRUD API if needed",
			regServer.Name, strings.Join(pkgTypes, ", "),
		)
	}

	return nil, fmt.Errorf("no supported transport found for server %q (no remotes or packages available)", regServer.Name)
}

// buildRemoteDTO builds a CreateMCPServerDTO for a remote transport.
func buildRemoteDTO(name string, serverType MCPServerType, remote RegistryRemote) *CreateMCPServerDTO {
	url := remote.URL
	dto := &CreateMCPServerDTO{
		Name: name,
		Type: serverType,
		URL:  &url,
	}

	// Extract headers as key-value pairs
	if len(remote.Headers) > 0 {
		headers := make(map[string]any, len(remote.Headers))
		for _, h := range remote.Headers {
			// Store the header with its value template (e.g. "Bearer {api_key}")
			// The admin must fill in actual values later
			headers[h.Name] = h.Value
		}
		dto.Headers = headers
	}

	// Extract env vars from remote
	if len(remote.EnvironmentVariables) > 0 {
		env := make(map[string]any, len(remote.EnvironmentVariables))
		for _, ev := range remote.EnvironmentVariables {
			// Store with empty string as placeholder — admin must fill in
			env[ev.Name] = ""
		}
		dto.Env = env
	}

	return dto
}

// collectRegistryEnvVars gathers all unique environment variables from a registry
// server definition (across both packages and remotes) and returns them as DTOs.
func collectRegistryEnvVars(regServer RegistryServer) []RegistryEnvVarDTO {
	seen := make(map[string]RegistryEnvVarDTO)
	for _, pkg := range regServer.Packages {
		for _, ev := range pkg.EnvironmentVariables {
			seen[ev.Name] = RegistryEnvVarDTO{
				Name:        ev.Name,
				Description: ev.Description,
				IsRequired:  ev.IsRequired,
				IsSecret:    ev.IsSecret,
			}
		}
	}
	for _, remote := range regServer.Remotes {
		for _, ev := range remote.EnvironmentVariables {
			seen[ev.Name] = RegistryEnvVarDTO{
				Name:        ev.Name,
				Description: ev.Description,
				IsRequired:  ev.IsRequired,
				IsSecret:    ev.IsSecret,
			}
		}
	}
	if len(seen) == 0 {
		return []RegistryEnvVarDTO{}
	}
	result := make([]RegistryEnvVarDTO, 0, len(seen))
	for _, ev := range seen {
		result = append(result, ev)
	}
	return result
}

// deriveLocalName extracts a human-friendly name from a registry server name.
// E.g. "io.github.github/github-mcp-server" → "github-mcp-server"
// E.g. "io.github.user/my-server" → "my-server"
func deriveLocalName(registryName string) string {
	// Find the last "/" in the name and take everything after it
	for i := len(registryName) - 1; i >= 0; i-- {
		if registryName[i] == '/' {
			return registryName[i+1:]
		}
	}
	// No "/" found, use the full name
	return registryName
}

// registryServerToDTO converts a RegistryServer to the response DTO.
func registryServerToDTO(s RegistryServer) RegistryServerDTO {
	dto := RegistryServerDTO{
		Name:        s.Name,
		Title:       s.Title,
		Description: s.Description,
		Version:     s.Version,
		HasRemotes:  len(s.Remotes) > 0,
		HasPackages: len(s.Packages) > 0,
	}

	if s.Repository != nil {
		dto.Repository = &RegistryRepoDTO{
			URL:    s.Repository.URL,
			Source: s.Repository.Source,
		}
	}

	// Collect all unique env vars across packages and remotes
	envVarMap := make(map[string]RegistryEnvVarDTO)
	for _, pkg := range s.Packages {
		dto.Packages = append(dto.Packages, RegistryPackageDTO{
			RegistryType: pkg.RegistryType,
			Name:         pkg.Name,
			Identifier:   pkg.Identifier,
			Transport:    pkg.Transport.Type,
		})
		for _, ev := range pkg.EnvironmentVariables {
			envVarMap[ev.Name] = RegistryEnvVarDTO{
				Name:        ev.Name,
				Description: ev.Description,
				IsRequired:  ev.IsRequired,
				IsSecret:    ev.IsSecret,
			}
		}
	}

	for _, remote := range s.Remotes {
		dto.Remotes = append(dto.Remotes, RegistryRemoteDTO{
			Type: remote.Type,
			URL:  remote.URL,
		})
		for _, ev := range remote.EnvironmentVariables {
			envVarMap[ev.Name] = RegistryEnvVarDTO{
				Name:        ev.Name,
				Description: ev.Description,
				IsRequired:  ev.IsRequired,
				IsSecret:    ev.IsSecret,
			}
		}
	}

	if len(envVarMap) > 0 {
		envVars := make([]RegistryEnvVarDTO, 0, len(envVarMap))
		for _, ev := range envVarMap {
			envVars = append(envVars, ev)
		}
		dto.EnvVars = envVars
	}

	// Initialize empty slices for JSON serialization
	if dto.Packages == nil {
		dto.Packages = []RegistryPackageDTO{}
	}
	if dto.Remotes == nil {
		dto.Remotes = []RegistryRemoteDTO{}
	}
	if dto.EnvVars == nil {
		dto.EnvVars = []RegistryEnvVarDTO{}
	}

	return dto
}

// --- Helpers ---

// DiscoveredTool represents a tool discovered from an external MCP server via tools/list.
type DiscoveredTool struct {
	Name        string         `json:"name"`
	Description *string        `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// schemaToMap converts an mcp.InputSchema to a generic map for JSONB storage.
func schemaToMap(schema mcp.InputSchema) map[string]any {
	// Marshal and unmarshal to get a clean map
	data, err := json.Marshal(schema)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]any{}
	}
	return result
}
