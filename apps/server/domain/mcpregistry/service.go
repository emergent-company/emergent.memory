package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Service handles business logic for the MCP server registry.
type Service struct {
	repo       *Repository
	mcpService *mcp.Service
	proxy      *ProxyManager
	catalog    catalog
	encryptor  *crypto.Encryptor // nil if encryption key not configured
	oauthCfg   OAuthConfig
	log        *slog.Logger

	// Track registered builtin servers per project to avoid re-registration
	mu                sync.Mutex
	builtinRegistered map[string]bool

	// ToolPool invalidation callback (registered via RegisterToolPoolInvalidator
	// to break the mcpregistry ↔ agents circular dependency)
	toolPoolInvalidator ToolPoolInvalidator

	// DeviceConfigPusher is the in-process bridge to the relay for pushing
	// resolved device configuration. Registered via RegisterDeviceConfigPusher;
	// nil-safe (pushes become no-ops).
	deviceConfigPusher DeviceConfigPusher
}

// Sentinel errors returned by the registry browse/install flow, mapped to
// distinct HTTP statuses by the handler.
var (
	// ErrUnsupportedRegistryTransport marks a registry entry whose transport
	// cannot be installed (e.g. stdio-only). Handler maps it to 400.
	ErrUnsupportedRegistryTransport = errors.New("unsupported registry transport")
	// ErrRegistryRemoteNotAllowed marks a registry remote URL rejected by the
	// SSRF guard. Handler maps it to 400, not 502, because it is a bad entry.
	ErrRegistryRemoteNotAllowed = errors.New("registry remote URL not allowed")
	// ErrServerNameConflict marks an attempted create whose name already exists
	// in the project. Handler maps it to 409.
	ErrServerNameConflict = errors.New("mcp server name already exists")
)

// NewService creates a new MCP registry service.
// The ToolPool invalidator is wired post-construction via RegisterToolPoolInvalidator
// (nil-safe) to break the mcpregistry ↔ agents circular dependency.
// encryptor may be nil when LLM_ENCRYPTION_KEY is not configured; secret values
// can then not be stored or read.
// It fails fast if the embedded registry catalog cannot be parsed (a broken
// build artifact) — browse/install has no network dependency, so the catalog is
// the sole source of entries.
func NewService(repo *Repository, mcpService *mcp.Service, encryptor *crypto.Encryptor, log *slog.Logger) (*Service, error) {
	cat, err := loadCatalog()
	if err != nil {
		return nil, err
	}
	return &Service{
		repo:              repo,
		mcpService:        mcpService,
		proxy:             NewProxyManager(repo, encryptor, log),
		catalog:           cat,
		encryptor:         encryptor,
		log:               log.With(logger.Scope("mcpregistry.svc")),
		builtinRegistered: make(map[string]bool),
	}, nil
}

// NewServiceWithOAuth is NewService plus the deployment's MCP OAuth connect
// configuration. It exists so existing callers (and tests) keep the four-argument
// constructor while the fx module supplies the configured variant.
func NewServiceWithOAuth(repo *Repository, mcpService *mcp.Service, encryptor *crypto.Encryptor, oauthCfg OAuthConfig, log *slog.Logger) (*Service, error) {
	svc, err := NewService(repo, mcpService, encryptor, log)
	if err != nil {
		return nil, err
	}
	svc.oauthCfg = oauthCfg
	return svc, nil
}

// RegisterToolPoolInvalidator wires the ToolPool invalidation callback after
// construction. Deferred to fx.Invoke to break the mcpregistry ↔ agents constructor cycle.
func (s *Service) RegisterToolPoolInvalidator(invalidator ToolPoolInvalidator) {
	s.toolPoolInvalidator = invalidator
}

// RegisterDeviceConfigPusher wires the relay push bridge after construction
// (mirrors RegisterToolPoolInvalidator). Nil-safe: with no pusher, device-config
// pushes and owner lookups become no-ops.
func (s *Service) RegisterDeviceConfigPusher(pusher DeviceConfigPusher) {
	s.deviceConfigPusher = pusher
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
	// The builtin registry backs the in-process agent tool pool, whose whitelists,
	// banned-tool lists and per-tool enable flags are keyed on per-verb names.
	// Seed it from the internal (legacy) catalog so those controls keep their
	// granularity; the external MCP surface is the consolidated catalog.
	builtinDefs := s.mcpService.GetInternalToolDefinitions()
	tools := make([]*MCPServerTool, 0, len(builtinDefs))
	for _, td := range builtinDefs {
		inputSchema := schemaToMap(td.InputSchema)
		desc := td.Description
		tools = append(tools, &MCPServerTool{
			ServerID:     serverID,
			ToolName:     td.Name,
			Description:  &desc,
			InputSchema:  inputSchema,
			OutputSchema: schemaPtrToMap(td.OutputSchema),
			Enabled:      true,
			ConfigKeys:   td.ConfigKeys,
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
	return s.repo.FindAllServersWithTools(ctx, projectID)
}

// GetServer returns an MCP server by ID.
func (s *Service) GetServer(ctx context.Context, id string, projectID *string) (*MCPServer, error) {
	return s.repo.FindServerByIDWithTools(ctx, id, projectID)
}

// ListDeviceServers returns the enabled device-targeted servers of a project.
// A nil instanceID returns every device row; a non-nil instanceID returns
// broadcast rows plus rows targeting that instance. Server-targeted rows are
// always excluded.
func (s *Service) ListDeviceServers(ctx context.Context, projectID string, instanceID *string) ([]*MCPServer, error) {
	return s.repo.FindDeviceServers(ctx, projectID, instanceID)
}

// CreateServer creates a new external MCP server.
// guardOutboundFetchURL validates a caller-supplied remote MCP server URL
// before the server opens any outbound connection to it (create, update, the
// save-less /test probe, and stored-server inspect). Stdio servers have no URL
// and are skipped. It wraps the shared SSRF guard and returns
// ErrOutboundURLNotAllowed so handlers map the rejection to 400.
func guardOutboundFetchURL(ctx context.Context, serverType MCPServerType, rawURL *string) error {
	if serverType != ServerTypeSSE && serverType != ServerTypeHTTP {
		return nil
	}
	if rawURL == nil || strings.TrimSpace(*rawURL) == "" {
		return nil // transport-required validation reports the empty case
	}
	if err := validateOutboundFetchURL(ctx, *rawURL); err != nil {
		return fmt.Errorf("%w: %v", ErrOutboundURLNotAllowed, err)
	}
	return nil
}

func (s *Service) CreateServer(ctx context.Context, projectID string, dto *CreateMCPServerDTO) (*MCPServer, error) {
	// Validate: cannot create builtin type via API
	if dto.Type == ServerTypeBuiltin {
		return nil, fmt.Errorf("cannot create servers of type 'builtin' via API")
	}

	// Normalize + validate the delivery target (defaults to 'server').
	target := dto.Target
	if target == "" {
		target = TargetServer
	}
	if !ValidTarget(target) {
		return nil, fmt.Errorf("invalid target %q: must be 'server' or 'device'", target)
	}
	// A builtin server cannot be device-targeted (already rejected above, but
	// guard the invariant explicitly for clarity).
	if target == TargetDevice && dto.Type == ServerTypeBuiltin {
		return nil, fmt.Errorf("cannot target a builtin server at a device")
	}
	// An instance id is only meaningful for a device target.
	if target == TargetServer && dto.TargetInstanceID != nil {
		return nil, fmt.Errorf("target_instance_id is only valid when target is 'device'")
	}

	// Authorize device-targeted writes: only the target device's owner or a
	// project admin may create a device-targeted row.
	if target == TargetDevice {
		if err := s.authorizeDeviceWrite(ctx, projectID, dto.TargetInstanceID); err != nil {
			return nil, err
		}
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

	// SSRF guard: the server may later connect outbound to this URL (discovery,
	// inspect, tool calls), so reject internal targets at write time.
	if err := guardOutboundFetchURL(ctx, dto.Type, dto.URL); err != nil {
		return nil, err
	}

	// Check for name collision
	existing, err := s.repo.FindServerByName(ctx, projectID, dto.Name)
	if err != nil {
		return nil, fmt.Errorf("checking name uniqueness: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: server with name %q already exists in this project", ErrServerNameConflict, dto.Name)
	}

	enabled := true
	if dto.Enabled != nil {
		enabled = *dto.Enabled
	}

	// Move secret keys out of the plaintext env/headers maps and encrypt them.
	env, secretEnv, err := splitSecrets(s.encryptor, dto.Env, dto.SecretEnvKeys, nil, "env")
	if err != nil {
		return nil, err
	}
	headers, secretHeaders, err := splitSecrets(s.encryptor, dto.Headers, dto.SecretHeadersKeys, nil, "header")
	if err != nil {
		return nil, err
	}

	// Required config defaults to an empty list so the non-null column is
	// written with [] rather than NULL (manual creates carry no required keys).
	required := dto.RequiredConfig
	if required == nil {
		required = []RequiredConfigKey{}
	}

	server := &MCPServer{
		ProjectID:             projectID,
		Name:                  dto.Name,
		Description:           dto.Description,
		Enabled:               enabled,
		Type:                  dto.Type,
		Command:               dto.Command,
		Args:                  dto.Args,
		Env:                   env,
		URL:                   dto.URL,
		Headers:               headers,
		SecretEnv:             secretEnv,
		SecretHeaders:         secretHeaders,
		RequiredConfig:        required,
		Target:                target,
		TargetInstanceID:      dto.TargetInstanceID,
		RegistryName:          dto.RegistryName,
		RegistryRepositoryURL: dto.RegistryRepositoryURL,
	}

	if err := s.repo.CreateServer(ctx, server); err != nil {
		return nil, fmt.Errorf("creating server: %w", err)
	}

	s.log.Info("created MCP server",
		slog.String("project_id", projectID),
		slog.String("server_id", server.ID),
		slog.String("name", server.Name),
		slog.String("type", string(server.Type)),
		slog.String("target", string(server.Target)),
	)

	s.invalidateToolPool(projectID)

	// Push device-targeted config to the matching instance(s), non-blocking.
	if server.Target == TargetDevice {
		s.pushAfterDeviceWrite(ctx, projectID, server.TargetInstanceID)
	}

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
	if server.Type == ServerTypeBuiltin || server.Type == ServerTypeSource {
		if dto.Name != nil || dto.Command != nil || dto.URL != nil || dto.Args != nil || dto.Env != nil || dto.Headers != nil ||
			dto.SecretEnvKeys != nil || dto.SecretHeadersKeys != nil || dto.Type != nil ||
			dto.Target != nil || dto.TargetInstanceID != nil {
			return nil, fmt.Errorf("cannot modify builtin server configuration — only enable/disable is allowed")
		}
	}

	// Transport change: the transport may be switched after registration. When
	// it changes, clear the connection fields of the transport being LEFT so
	// stale config never leaks into the new transport. Command/args/env (stdio)
	// and url/headers (sse/http) are each overwritten by the per-field handlers
	// below only when the DTO carries them, so the swap must nil out the other
	// transport's fields explicitly.
	if dto.Type != nil && *dto.Type != server.Type {
		switch *dto.Type {
		case ServerTypeStdio, ServerTypeSSE, ServerTypeHTTP:
			// supported
		default:
			return nil, fmt.Errorf("unsupported transport type %q", *dto.Type)
		}
		switch *dto.Type {
		case ServerTypeStdio:
			// Leaving a remote transport: drop the remote connection fields
			// (command/args/env are set from the DTO below).
			server.URL = nil
			server.Headers = nil
			server.SecretHeaders = nil
		case ServerTypeSSE, ServerTypeHTTP:
			// Leaving stdio: drop the stdio process fields (url/headers are set
			// from the DTO below).
			server.Command = nil
			server.Args = nil
			server.Env = nil
			server.SecretEnv = nil
		}
		server.Type = *dto.Type
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
	if dto.Description != nil {
		server.Description = dto.Description
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
		env, secretEnv, splitErr := splitSecrets(s.encryptor, dto.Env, dto.SecretEnvKeys, server.SecretEnv, "env")
		if splitErr != nil {
			return nil, splitErr
		}
		server.Env = env
		server.SecretEnv = secretEnv
	}
	if dto.URL != nil {
		server.URL = dto.URL
	}
	if dto.Headers != nil {
		headers, secretHeaders, splitErr := splitSecrets(s.encryptor, dto.Headers, dto.SecretHeadersKeys, server.SecretHeaders, "header")
		if splitErr != nil {
			return nil, splitErr
		}
		server.Headers = headers
		server.SecretHeaders = secretHeaders
	}

	// Enable gate: reject enabling while a declared required key is blank. Run
	// after the Env/Headers blocks so a same-request credential fill counts, and
	// before the update so a rejected enable never mutates the row.
	if dto.Enabled != nil && *dto.Enabled {
		if missing := missingRequiredConfigKeys(server); len(missing) > 0 {
			return nil, &ErrRequiredConfigMissing{Keys: missing}
		}
	}

	// Delivery target changes: validate the resulting target and authorize
	// device-targeted writes.
	if dto.Target != nil {
		if !ValidTarget(*dto.Target) {
			return nil, fmt.Errorf("invalid target %q: must be 'server' or 'device'", *dto.Target)
		}
		server.Target = *dto.Target
	}
	if dto.TargetInstanceID != nil {
		server.TargetInstanceID = dto.TargetInstanceID
	}
	if server.Target == "" {
		server.Target = TargetServer
	}
	// A device-targeted row must not be a builtin server.
	if server.Target == TargetDevice && server.Type == ServerTypeBuiltin {
		return nil, fmt.Errorf("cannot target a builtin server at a device")
	}
	// An instance id is only meaningful for a device target.
	if server.Target == TargetServer && server.TargetInstanceID != nil {
		return nil, fmt.Errorf("target_instance_id is only valid when target is 'device'")
	}
	if server.Target == TargetDevice {
		if err := s.authorizeDeviceWrite(ctx, projectID, server.TargetInstanceID); err != nil {
			return nil, err
		}
	}

	// Transport-required validation covers both a type change and a same-type
	// partial update (e.g. clearing the command on a stdio server). The builtin
	// type has no case, so enable/disable-only builtin updates still pass.
	switch server.Type {
	case ServerTypeStdio:
		if server.Command == nil || *server.Command == "" {
			return nil, fmt.Errorf("command is required for stdio-type servers")
		}
	case ServerTypeSSE, ServerTypeHTTP:
		if server.URL == nil || *server.URL == "" {
			return nil, fmt.Errorf("url is required for %s-type servers", server.Type)
		}
	}

	// SSRF guard: reject a resulting URL that points at internal space before
	// persisting it and before the proxy is asked to connect.
	if err := guardOutboundFetchURL(ctx, server.Type, server.URL); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateServer(ctx, server); err != nil {
		return nil, fmt.Errorf("updating server: %w", err)
	}

	// Evict proxy connection so next call uses updated config
	s.proxy.CloseServer(id)

	s.invalidateToolPool(projectID)

	// Push device-targeted config to the matching instance(s), non-blocking.
	if server.Target == TargetDevice {
		s.pushAfterDeviceWrite(ctx, projectID, server.TargetInstanceID)
	}

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

	// Authorize device-targeted writes: only the device's owner or a project
	// admin may delete a device-targeted row.
	if server.Target == TargetDevice {
		if err := s.authorizeDeviceWrite(ctx, projectID, server.TargetInstanceID); err != nil {
			return err
		}
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

	// Push the reduced device config to the matching instance(s), non-blocking.
	if server.Target == TargetDevice {
		s.pushAfterDeviceWrite(ctx, projectID, server.TargetInstanceID)
	}

	return nil
}

// --- Device configuration push + authorization ---

// devicePushResolveTimeout bounds the resolve step of a device-config push so a
// slow DB or decrypt never leaks a goroutine.
const devicePushResolveTimeout = 30 * time.Second

// authorizeDeviceWrite enforces D2/D6 authorization for a device-targeted write.
// A project admin may target any device, including a broadcast (instance id
// nil). A non-admin must target a specific device they own (the device's
// recorded owner subject must match the caller). Broadcasts are admin-only: a
// broadcast row persists with no owner identity, so it is hydrated to every
// opted-in device of the project on (re)connect — allowing a non-admin to
// create one would be a cross-member RCE (a member's command executing on
// another member's device).
func (s *Service) authorizeDeviceWrite(ctx context.Context, projectID string, instanceID *string) error {
	user, err := auth.RequireUser(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeviceTargetForbidden, err)
	}

	// A project admin may target any device, including a broadcast that fans out
	// to every opted-in device of the project.
	isAdmin, err := s.repo.IsProjectAdmin(ctx, projectID, user.ID)
	if err != nil {
		return fmt.Errorf("checking project admin: %w", err)
	}
	if isAdmin {
		return nil
	}

	// Broadcasts are admin-only (see doc comment): a non-admin must name a
	// specific device they own.
	if instanceID == nil {
		return ErrDeviceTargetForbidden
	}

	// Owner check: the device's recorded owner subject must match the caller.
	if s.deviceConfigPusher != nil {
		if owner, ok := s.deviceConfigPusher.OwnerSubject(projectID, *instanceID); ok && owner != "" && owner == user.ID {
			return nil
		}
	}
	return ErrDeviceTargetForbidden
}

// resolveDeviceConfig resolves and decrypts the enabled device-targeted rows of
// a project into the wire DeviceServerConfig list. instanceID non-nil scopes to
// a single device (broadcast rows + that instance's rows); nil returns the
// broadcast rows only. Decryption happens here — the only holder of the
// encryptor — and the decrypted values never flow through ToDTO/stripSecretKeys
// or the tool-pool caches.
func (s *Service) resolveDeviceConfig(ctx context.Context, projectID string, instanceID *string) ([]DeviceServerConfig, error) {
	servers, err := s.repo.FindDeviceServers(ctx, projectID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("listing device servers: %w", err)
	}
	out := make([]DeviceServerConfig, 0, len(servers))
	for _, srv := range servers {
		env, err := mergeDecrypted(s.encryptor, srv.Env, srv.SecretEnv, "env")
		if err != nil {
			return nil, fmt.Errorf("decrypting env for device server %q: %w", srv.Name, err)
		}
		headers, err := mergeDecrypted(s.encryptor, srv.Headers, srv.SecretHeaders, "header")
		if err != nil {
			return nil, fmt.Errorf("decrypting headers for device server %q: %w", srv.Name, err)
		}
		// A device cannot refresh an OAuth token it does not own: resolve a
		// valid bearer here (refreshing if needed) and push it with the config.
		// The value stays inside DeviceServerConfig — never logged, never DTO'd.
		if auth, authErr := s.proxy.oauthAuthorizationHeader(ctx, srv); authErr != nil {
			s.log.Warn("device server OAuth token unavailable",
				slog.String("server_id", srv.ID),
				slog.String("error", authErr.Error()),
			)
		} else if auth != "" {
			headers = withDeviceAuthorization(headers, auth)
		}
		out = append(out, DeviceServerConfig{
			Name:      srv.Name,
			Transport: deviceTransport(srv.Type),
			Enabled:   srv.Enabled,
			Command:   ptrString(srv.Command),
			Args:      srv.Args,
			Env:       env,
			URL:       ptrString(srv.URL),
			Headers:   headers,
		})
	}
	return out, nil
}

// resolveForProject returns the per-instance resolver used by the relay to
// resolve each target device's own config during a broadcast push.
func (s *Service) resolveForProject(projectID string) func(ctx context.Context, instanceID string) ([]DeviceServerConfig, error) {
	return func(ctx context.Context, instanceID string) ([]DeviceServerConfig, error) {
		return s.resolveDeviceConfig(ctx, projectID, &instanceID)
	}
}

// pushAfterDeviceWrite fans out the resolved device config after a
// device-targeted create/update/delete. Non-blocking: a slow/offline device can
// never stall the registry write.
func (s *Service) pushAfterDeviceWrite(ctx context.Context, projectID string, instanceID *string) {
	if s.deviceConfigPusher == nil {
		return
	}
	ownerSubject := ""
	admin := false
	if u, err := auth.RequireUser(ctx); err == nil {
		ownerSubject = u.ID
		if a, aerr := s.repo.IsProjectAdmin(ctx, projectID, u.ID); aerr == nil {
			admin = a
		}
	}
	s.pushDeviceConfig(projectID, instanceID, ownerSubject, admin)
}

// pushDeviceConfig launches a best-effort push on its own goroutine with a
// bounded context. Never logs the resolved frame or decrypted values.
func (s *Service) pushDeviceConfig(projectID string, instanceID *string, ownerSubject string, admin bool) {
	instanceIDCopy := instanceID
	go func() {
		pushCtx, cancel := context.WithTimeout(context.Background(), devicePushResolveTimeout)
		defer cancel()

		if instanceIDCopy != nil {
			servers, err := s.resolveDeviceConfig(pushCtx, projectID, instanceIDCopy)
			if err != nil {
				s.log.Warn("device config resolve failed", slog.String("error", err.Error()))
				return
			}
			s.deviceConfigPusher.PushConfigToInstance(pushCtx, projectID, *instanceIDCopy, servers)
			return
		}
		s.deviceConfigPusher.PushConfigToProject(pushCtx, projectID, ownerSubject, admin, s.resolveForProject(projectID))
	}()
}

// HydrateDevice pushes the current resolved device config to a single instance.
// Called by the relay's post-ack hydration hook (mcprelay.NotifyRegistered),
// never before the register ack is written. Non-blocking.
func (s *Service) HydrateDevice(projectID, instanceID string) {
	if s.deviceConfigPusher == nil {
		return
	}
	id := instanceID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), devicePushResolveTimeout)
		defer cancel()
		servers, err := s.resolveDeviceConfig(ctx, projectID, &id)
		if err != nil {
			s.log.Warn("device hydration resolve failed", slog.String("error", err.Error()))
			return
		}
		s.deviceConfigPusher.PushConfigToInstance(ctx, projectID, id, servers)
	}()
}

// mergeDecrypted merges the plaintext map with the decrypted secret values.
// A nil encryptor with any ciphertext present is an error (cannot decrypt).
func mergeDecrypted(enc *crypto.Encryptor, plain map[string]any, secrets map[string]EncryptedSecret, kind string) (map[string]any, error) {
	out := make(map[string]any, len(plain)+len(secrets))
	for k, v := range plain {
		out[k] = v
	}
	for k, sec := range secrets {
		if enc == nil {
			return nil, fmt.Errorf("credential encryption not configured (cannot decrypt secret %s %q)", kind, k)
		}
		pt, err := enc.Decrypt(sec.Ciphertext, sec.Nonce)
		if err != nil {
			return nil, fmt.Errorf("decrypting secret %s %q: %w", kind, k, err)
		}
		out[k] = string(pt)
	}
	return out, nil
}

// deviceTransport maps an MCPServerType to the relay wire transport name.
func deviceTransport(t MCPServerType) string {
	switch t {
	case ServerTypeHTTP:
		return "http"
	case ServerTypeSSE:
		return "sse"
	default:
		return "stdio"
	}
}

// ptrString dereferences a nullable string, returning "" for nil.
func ptrString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// --- Tool management ---

// ListTools returns all tools for a server.
func (s *Service) ListTools(ctx context.Context, serverID string) ([]*MCPServerTool, error) {
	return s.repo.FindToolsByServerID(ctx, serverID)
}

// ToggleTool enables or disables a specific tool in the caller's project.
func (s *Service) ToggleTool(ctx context.Context, projectID, toolID string, enabled bool) error {
	return s.UpdateTool(ctx, projectID, toolID, &enabled, nil)
}

// UpdateTool updates enabled and/or config for a tool in the caller's project
// and invalidates the tool pool cache. The tool must belong to the caller's
// project: a foreign or missing tool returns ErrToolNotFound (issue #978).
// Pass nil for fields that should not be changed.
func (s *Service) UpdateTool(ctx context.Context, projectID, toolID string, enabled *bool, config *map[string]any) error {
	tool, err := s.repo.FindToolByIDForProject(ctx, projectID, toolID)
	if err != nil {
		return fmt.Errorf("fetching tool: %w", err)
	}
	if tool == nil {
		return ErrToolNotFound
	}

	if err := s.repo.UpdateToolForProject(ctx, projectID, toolID, enabled, config); err != nil {
		return err
	}

	s.invalidateToolPool(projectID)

	return nil
}

// SyncServerTools upserts discovered tools for a server and removes tools that
// no longer exist. The server must belong to the caller's project: a foreign or
// missing server returns ErrServerNotFound before any mutation (issue #978), so
// kb.mcp_server_tools can never be mutated without an ownership predicate.
// For now this accepts tools directly (caller is responsible for connecting and
// calling tools/list). In the future, the proxy layer will handle the connection.
func (s *Service) SyncServerTools(ctx context.Context, projectID, serverID string, discoveredTools []DiscoveredTool) error {
	// Enforce server ownership before any mutation (issue #978).
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return fmt.Errorf("fetching server: %w", err)
	}
	if server == nil {
		return ErrServerNotFound
	}

	// Device-targeted rows are never tool-synced server-side: their tools are
	// served by the device through the relay.
	if server.Target == TargetDevice {
		return ErrDeviceServerNotOutbound
	}

	// Upsert discovered tools
	tools := make([]*MCPServerTool, 0, len(discoveredTools))
	currentNames := make([]string, 0, len(discoveredTools))

	for _, dt := range discoveredTools {
		tools = append(tools, &MCPServerTool{
			ServerID:     serverID,
			ToolName:     dt.Name,
			Description:  dt.Description,
			InputSchema:  dt.InputSchema,
			OutputSchema: dt.OutputSchema,
			Enabled:      true,
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

	s.invalidateToolPool(projectID)

	return nil
}

// GetEnabledToolsForProject returns all enabled tools from enabled external
// (non-builtin) servers for a project. Used by ToolPool.buildCache().
func (s *Service) GetEnabledToolsForProject(ctx context.Context, projectID string) ([]*EnabledServerTool, error) {
	return s.repo.FindAllEnabledTools(ctx, projectID)
}

// RefreshBuiltinServer invalidates the per-project once-only guard and re-seeds
// the builtin tool catalog. Used when a capability adds or removes tools after a
// project's builtin server already exists (e.g. a GitHub App installation).
func (s *Service) RefreshBuiltinServer(ctx context.Context, projectID string) error {
	s.mu.Lock()
	delete(s.builtinRegistered, projectID)
	s.mu.Unlock()
	return s.EnsureBuiltinServer(ctx, projectID)
}

// GetEnabledBuiltinToolsForProject calls EnsureBuiltinServer (so all tools exist in DB)
// then returns only the enabled builtin tools for a project from the DB.
// This lets per-project enable/disable overrides be honoured by ToolPool.
func (s *Service) GetEnabledBuiltinToolsForProject(ctx context.Context, projectID string) ([]*EnabledServerTool, error) {
	if err := s.EnsureBuiltinServer(ctx, projectID); err != nil {
		return nil, fmt.Errorf("ensuring builtin server: %w", err)
	}
	return s.repo.FindAllEnabledBuiltinTools(ctx, projectID)
}

// ListBuiltinToolsForProject returns all builtin tools for a project with their
// resolved enabled state and config inheritance source. This is the authoritative
// list used by GET /api/admin/builtin-tools.
func (s *Service) ListBuiltinToolsForProject(ctx context.Context, projectID string) ([]*MCPServerToolDTO, error) {
	if err := s.EnsureBuiltinServer(ctx, projectID); err != nil {
		return nil, fmt.Errorf("ensuring builtin server: %w", err)
	}

	server, err := s.repo.FindServerByName(ctx, projectID, "builtin")
	if err != nil {
		return nil, fmt.Errorf("finding builtin server: %w", err)
	}
	if server == nil {
		return nil, fmt.Errorf("builtin server not found for project %s", projectID)
	}

	tools, err := s.repo.FindToolsByServerID(ctx, server.ID)
	if err != nil {
		return nil, fmt.Errorf("listing builtin tools: %w", err)
	}

	dtos := make([]*MCPServerToolDTO, 0, len(tools))
	for _, t := range tools {
		dto := t.ToDTO()
		resolvedEnabled, _, source, resolveErr := s.ResolveBuiltinToolSettings(ctx, projectID, t.ToolName)
		if resolveErr == nil {
			dto.Enabled = resolvedEnabled
			dto.InheritedFrom = source
		}
		dtos = append(dtos, dto)
	}

	return dtos, nil
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

	// Device-targeted rows are never connected outbound by the server.
	if server.Target == TargetDevice {
		return nil, ErrDeviceServerNotOutbound
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

	// SSRF guard: inspect opens an outbound connection to the stored URL.
	if err := guardOutboundFetchURL(ctx, server.Type, server.URL); err != nil {
		return nil, err
	}

	return s.proxy.InspectServer(ctx, server)
}

// TestServerConfig probes a draft (create or edit) server configuration without
// persisting it. The draft's secret values arrive in plaintext and are placed
// directly into Env/Headers (never encrypted, never logged): the proxy's decrypt
// helpers are no-ops when the Secret maps are empty. When serverID is non-empty,
// required keys left blank in the draft are backfilled from the stored server's
// secrets (edit-mode test). The probe never writes to the repository.
func (s *Service) TestServerConfig(ctx context.Context, projectID string, dto *CreateMCPServerDTO, serverID *string) (*MCPServerInspectDTO, error) {
	// Validate transport-specific fields exactly like CreateServer.
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

	// SSRF guard: the probe dials the draft URL, so it must not reach internal
	// space. Checked before the repo lookup and before any transient server is
	// built.
	if err := guardOutboundFetchURL(ctx, dto.Type, dto.URL); err != nil {
		return nil, err
	}

	server := buildTransientServer(dto)

	// Edit mode: for each required key left blank in the draft, copy the stored
	// server's secret entries verbatim into the transient server's secret maps.
	// A missing/foreign serverID is not fatal (no backfill); a stored device
	// row is rejected (no outbound connection).
	if serverID != nil && *serverID != "" {
		stored, err := s.repo.FindServerByID(ctx, *serverID, &projectID)
		if err != nil {
			return nil, fmt.Errorf("fetching server for test: %w", err)
		}
		if stored != nil {
			if stored.Target == TargetDevice {
				return nil, ErrDeviceServerNotOutbound
			}
			backfillTransientSecrets(server, stored)
		}
	}

	// Device-targeted drafts are never connected outbound by the server.
	if server.Target == TargetDevice {
		return nil, ErrDeviceServerNotOutbound
	}

	return s.proxy.InspectServer(ctx, server)
}

// buildTransientServer builds a transient MCPServer for a save-less test probe.
// The draft's Env/Headers plaintext values (including draft secrets) go directly
// into Env/Headers; the Secret maps stay empty so the proxy's decrypt helpers are
// no-ops and nothing is ever encrypted, persisted, or logged. The returned
// server is never written to the repository.
func buildTransientServer(dto *CreateMCPServerDTO) *MCPServer {
	name := dto.Name
	if name == "" {
		name = "test"
	}
	target := dto.Target
	if target == "" {
		target = TargetServer
	}
	return &MCPServer{
		Name:    name,
		Type:    dto.Type,
		Command: dto.Command,
		Args:    dto.Args,
		URL:     dto.URL,
		Env:     dto.Env,
		Headers: dto.Headers,
		Target:  target,
	}
}

// backfillTransientSecrets copies the stored server's secret entries for any
// required key left blank in the draft into the transient server's secret maps,
// so an edit-mode test reuses the saved credential. Ciphertext is copied
// verbatim (never decrypted here); a draft value always wins.
func backfillTransientSecrets(transient, stored *MCPServer) {
	if transient == nil || stored == nil {
		return
	}
	if transient.SecretEnv == nil {
		transient.SecretEnv = make(map[string]EncryptedSecret)
	}
	if transient.SecretHeaders == nil {
		transient.SecretHeaders = make(map[string]EncryptedSecret)
	}
	for _, key := range stored.RequiredConfig {
		switch key.Kind {
		case "header":
			if strings.TrimSpace(stringify(transient.Headers[key.Name])) != "" {
				continue
			}
			if sec, ok := stored.SecretHeaders[key.Name]; ok {
				transient.SecretHeaders[key.Name] = sec
			}
		case "env":
			if strings.TrimSpace(stringify(transient.Env[key.Name])) != "" {
				continue
			}
			if sec, ok := stored.SecretEnv[key.Name]; ok {
				transient.SecretEnv[key.Name] = sec
			}
		}
	}
}

// CallExternalTool connects to the appropriate external MCP server and forwards
// a tool call. The prefixedToolName includes the server name prefix (e.g.
// "myserver_search"). The proxy strips the prefix and forwards to the real server.
func (s *Service) CallExternalTool(ctx context.Context, projectID, prefixedToolName string, args map[string]any) (*mcp.ToolResult, error) {
	return s.proxy.CallTool(ctx, projectID, prefixedToolName, args)
}

// mcpToolCallTimeout bounds a single tool invocation (builtin or external) so a
// hung upstream cannot pin a request handler. External MCP servers may be remote
// and slow; 30s balances user patience against dropping a legitimate
// long-running tool.
const mcpToolCallTimeout = 30 * time.Second

// CallToolOnServer invokes a single tool on a specific MCP server, resolved by
// ID scoped to the project. It enforces server and tool enablement before
// dispatch, then routes builtin servers to mcp.Service.ExecuteTool and external
// (stdio/sse/http) servers through the proxy by raw server row (no name-prefix
// inference). Builtin dispatch is gated by the transport's per-tool authority
// check (AuthorizeToolCall) using the caller's scopes, and — like every MCP HTTP
// transport — the caller's share-instance tool allowlist (deny-by-default) is
// enforced before any dispatch. Errors are typed so the handler can map
// not-found → 404, disabled → 409, forbidden → 403, and upstream/connection
// failures → 502.
func (s *Service) CallToolOnServer(ctx context.Context, projectID, serverID, toolName string, args map[string]any, scopes []string, apiTokenID string) (*mcp.ToolResult, error) {
	server, err := s.repo.FindServerByID(ctx, serverID, &projectID)
	if err != nil {
		return nil, fmt.Errorf("fetching server: %w", err)
	}
	if server == nil {
		return nil, ErrServerNotFound
	}
	if !server.Enabled {
		return nil, ErrServerDisabled
	}

	// Device-targeted rows are never connected outbound by the server.
	if server.Target == TargetDevice {
		return nil, ErrDeviceServerNotOutbound
	}

	tool, err := s.repo.FindToolByServerAndName(ctx, serverID, toolName)
	if err != nil {
		return nil, fmt.Errorf("fetching tool: %w", err)
	}
	if tool == nil {
		return nil, ErrToolNotFound
	}
	if !tool.Enabled {
		return nil, ErrToolDisabled
	}

	// Share-instance tool allowlist: the same deny-by-default check the MCP HTTP
	// transports apply before dispatch (handler.go / sse_handler.go /
	// streamable_http_handler.go). A token bound to a restricted share instance
	// must not reach a tool outside its allowlist through this admin invoke route.
	// A nil scope (unrestricted/legacy) or a resolver failure that yields an
	// error is fail-closed.
	instanceScope, err := s.mcpService.ResolveInstanceScope(ctx, apiTokenID)
	if err != nil {
		return nil, fmt.Errorf("resolving MCP share-instance scope: %w", err)
	}
	if mcp.InstanceDeniesTool(instanceScope, toolName) {
		return nil, ErrToolForbidden
	}

	if server.Type == ServerTypeBuiltin {
		if err := s.mcpService.AuthorizeToolCall(ctx, toolName, scopes); err != nil {
			if errors.Is(err, mcp.ErrToolForbidden) {
				return nil, ErrToolForbidden
			}
			return nil, fmt.Errorf("authorizing tool %q: %w", toolName, err)
		}

		execCtx, cancel := context.WithTimeout(ctx, mcpToolCallTimeout)
		defer cancel()
		// The per-tool authority was already enforced above (AgentOnly /
		// RequiredScope / SuperadminOnly), mirroring the HTTP transport. Mark the
		// call transport-enforced so ExecuteTool's in-process gate (which covers
		// the untrusted agent-run path) does not re-fire for an authenticated REST
		// client. This is NOT the trusted-internal marker. The resolved
		// share-instance scope is also carried so ExecuteTool's own
		// InstanceDeniesTool defense-in-depth fires (belt-and-braces with the
		// explicit check above).
		execCtx = mcp.ContextWithTransportEnforced(execCtx)
		execCtx = mcp.WithInstanceScope(execCtx, instanceScope)
		return s.mcpService.ExecuteTool(execCtx, projectID, toolName, args)
	}

	callCtx, cancel := context.WithTimeout(ctx, mcpToolCallTimeout)
	defer cancel()
	result, err := s.proxy.CallToolOnServer(callCtx, server, toolName, args)
	if err != nil {
		// Log the full detail server-side (stdio command lines / URLs), but never
		// surface it to the caller — the handler returns a sanitized 502 message.
		s.log.Error("upstream MCP tool call failed",
			slog.String("server_id", serverID),
			slog.String("tool", toolName),
			logger.Error(err),
		)
		return nil, fmt.Errorf("%w: %v", ErrUpstreamFailure, err)
	}
	return result, nil
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
	if server.Target == TargetDevice {
		return nil, fmt.Errorf("%w: device-targeted servers have no server-side tools to discover", ErrDeviceServerNotOutbound)
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
	if err := s.SyncServerTools(ctx, projectID, serverID, discovered); err != nil {
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

// SearchRegistry searches the curated MCP registry for servers. It is served
// entirely from the embedded catalog — no network — so it cannot fail at
// request time. The catalog is small (<30 entries), so pagination is
// unnecessary: the cursor is ignored and no next page is produced.
func (s *Service) SearchRegistry(ctx context.Context, query string, limit int, cursor string) (*RegistrySearchResultDTO, error) {
	entries := s.catalog.search(query, limit)
	servers := make([]RegistryServerDTO, 0, len(entries))
	for _, entry := range entries {
		servers = append(servers, registryServerToDTO(entry))
	}

	return &RegistrySearchResultDTO{
		Servers:    servers,
		NextCursor: "",
		Count:      len(servers),
	}, nil
}

// GetRegistryServer fetches a single server from the curated MCP registry. It
// is served from the embedded catalog; an unknown name returns
// ErrRegistryServerNotFound.
func (s *Service) GetRegistryServer(ctx context.Context, name string) (*RegistryServerDTO, error) {
	entry, ok := s.catalog.get(name)
	if !ok {
		return nil, ErrRegistryServerNotFound
	}

	dto := registryServerToDTO(entry)
	return &dto, nil
}

// InstallFromRegistry installs a server from the curated MCP registry into the
// local project. It resolves the server definition from the embedded catalog,
// selects the best transport (remote-first: streamable-http > sse > stdio
// packages), creates an entry in kb.mcp_servers, and triggers tool discovery
// for remote transports.
func (s *Service) InstallFromRegistry(ctx context.Context, projectID string, dto *InstallFromRegistryDTO) (*InstallResultDTO, error) {
	// Resolve the server definition from the embedded catalog (no network).
	// The registry version is curated at build time, so dto.Version is ignored.
	regServer, ok := s.catalog.get(dto.RegistryName)
	if !ok {
		return nil, ErrRegistryServerNotFound
	}

	// Determine the local name for this server
	localName := dto.Name
	if localName == "" {
		localName = deriveLocalName(regServer.Name)
	}

	// Select the install path. A device install runs the entry's local package
	// on a connected connector (stdio translation, pushed over the relay);
	// anything else installs an outbound server on the shared host. Both paths
	// resolve the full DTO before any write, so a rejected install never leaves
	// a partially-created server behind.
	var (
		createDTO *CreateMCPServerDTO
		err       error
	)
	if dto.Target == TargetDevice {
		createDTO, err = buildDeviceInstallDTO(regServer, localName)
	} else {
		createDTO, err = selectTransport(ctx, regServer, localName)
	}
	if err != nil {
		return nil, fmt.Errorf("selecting transport: %w", err)
	}
	if dto.Target == TargetDevice {
		createDTO.Target = TargetDevice
		createDTO.TargetInstanceID = dto.TargetInstanceID
	}

	// Record where this install came from so the configure form can link to the
	// provider's docs when a credential is still needed.
	createDTO.RegistryName = &dto.RegistryName
	if regServer.Repository != nil && regServer.Repository.URL != "" {
		repoURL := regServer.Repository.URL
		createDTO.RegistryRepositoryURL = &repoURL
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
	// (which requires env vars to be configured first). When the install
	// resolved required configuration (unfilled credentials), skip discovery:
	// the server is created disabled and the connection would fail.
	requiresAuthorization := false
	requiresTokenHeader := false
	if len(createDTO.RequiredConfig) == 0 && (createDTO.Type == ServerTypeSSE || createDTO.Type == ServerTypeHTTP) {
		discovered, err := s.DiscoverAndSyncTools(ctx, server.ID, projectID)
		if err != nil {
			// Don't fail the install, just log the warning.
			// The user can manually sync tools later once env vars are configured.
			s.log.Warn("auto-discover failed after install (env vars may need configuration)",
				slog.String("server_id", server.ID),
				slog.String("error", err.Error()),
			)
			// An OAuth rejection is not a credential typo: the server needs the
			// connect flow, and the install result says so rather than leaving
			// the user with a server that silently has no tools.
			if isAuthRejection(err) {
				requiresAuthorization, requiresTokenHeader = classifyInstallAuth(ctx, server, err)
			}
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
	if requiresAuthorization {
		message = fmt.Sprintf("Server installed, but it requires OAuth authorization before its tools are available. Connect it from the MCP servers page (POST /api/admin/mcp-servers/%s/oauth/authorize to start the flow).", server.ID)
	}
	if requiresTokenHeader {
		message = fmt.Sprintf("Server installed, but it requires authorization and its provider publishes no discoverable OAuth metadata, so the browser connect flow cannot be used — %s", oauthMetadataUnavailableRemedy)
	}

	return &InstallResultDTO{
		Server:                result.ToDetailDTO(),
		RequiredEnvVars:       envVars,
		Message:               message,
		RequiredConfig:        createDTO.RequiredConfig,
		RequiresAuthorization: requiresAuthorization,
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
//
// The selected remote URL is validated by validateRegistryRemoteURL before it
// is accepted: registry entries are community-authored, so a URL pointing at
// loopback/private/link-local/metadata addresses (or using userinfo, or a
// non-http(s) scheme) is rejected as ErrRegistryRemoteNotAllowed.
func selectTransport(ctx context.Context, regServer RegistryServer, localName string) (*CreateMCPServerDTO, error) {
	// Try remotes (prefer streamable-http over sse)
	for _, remote := range regServer.Remotes {
		if remote.Type == "streamable-http" {
			if err := validateRegistryRemoteURL(ctx, remote.URL); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrRegistryRemoteNotAllowed, err)
			}
			return buildRemoteDTO(localName, ServerTypeHTTP, remote), nil
		}
	}
	for _, remote := range regServer.Remotes {
		if remote.Type == "sse" {
			if err := validateRegistryRemoteURL(ctx, remote.URL); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrRegistryRemoteNotAllowed, err)
			}
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
			"%w: server %q only offers stdio-based packages (%s); only remote transports (HTTP/SSE) are allowed — "+
				"you can manually create a stdio server via the CRUD API if needed",
			ErrUnsupportedRegistryTransport, regServer.Name, strings.Join(pkgTypes, ", "),
		)
	}

	return nil, fmt.Errorf("%w: no supported transport found for server %q (no remotes or packages available)", ErrUnsupportedRegistryTransport, regServer.Name)
}

// buildDeviceInstallDTO translates a registry entry into a stdio
// CreateMCPServerDTO for delivery to a connected connector. It selects the first
// locally-runnable package (npm → npx, pypi → uvx) — the same preference order
// the connector applies when it resolves an entry itself — and never falls back
// to a remote transport: a device install runs the server on the device, so an
// entry with only remotes has nothing to install there. Required-or-secret env
// keys are declared as empty placeholders (their names recorded in
// SecretEnvKeys so a later PATCH encrypts the real value) and the server is
// created disabled until they are filled, exactly as the remote path does.
func buildDeviceInstallDTO(entry RegistryServer, localName string) (*CreateMCPServerDTO, error) {
	for _, pkg := range entry.Packages {
		runner, ok := registryPackageRunner(pkg.RegistryType)
		if !ok {
			continue
		}

		args := make([]string, 0, 2+len(pkg.RuntimeArguments)+len(pkg.PackageArguments))
		if runner == "npx" {
			// npx prompts before installing a package it has not cached; -y keeps
			// the connector's supervised process from blocking on a prompt.
			args = append(args, "-y")
		}
		args = append(args, registryPackageSpec(pkg.Identifier, registryPackageVersion(pkg.Version, entry.Version)))
		args = append(args, renderArguments(pkg.RuntimeArguments)...)
		args = append(args, renderArguments(pkg.PackageArguments)...)

		command := runner
		dto := &CreateMCPServerDTO{
			Name:    localName,
			Type:    ServerTypeStdio,
			Command: &command,
			Args:    args,
		}

		if len(pkg.EnvironmentVariables) > 0 {
			env := make(map[string]any, len(pkg.EnvironmentVariables))
			for _, ev := range pkg.EnvironmentVariables {
				env[ev.Name] = ""
				if ev.IsSecret {
					dto.SecretEnvKeys = append(dto.SecretEnvKeys, ev.Name)
				}
			}
			dto.Env = env
		}

		if required := collectPackageRequiredConfig(pkg); len(required) > 0 {
			dto.RequiredConfig = required
			enabled := false
			dto.Enabled = &enabled
		}

		return dto, nil
	}

	if len(entry.Packages) == 0 {
		return nil, fmt.Errorf(
			"%w: server %q declares no local package; a device install runs an npm or pypi package on the connector",
			ErrUnsupportedRegistryTransport, entry.Name,
		)
	}
	pkgTypes := make([]string, 0, len(entry.Packages))
	for _, pkg := range entry.Packages {
		pkgTypes = append(pkgTypes, pkg.RegistryType)
	}
	return nil, fmt.Errorf(
		"%w: server %q declares no locally-runnable package (%s); a device install needs an npm or pypi package",
		ErrUnsupportedRegistryTransport, entry.Name, strings.Join(pkgTypes, ", "),
	)
}

// registryPackageRunner maps a registry package type to the local runner that
// executes it: npm packages run under npx, pypi under uvx. Mirrors the
// connector's own registry translation so a UI-driven device install behaves
// identically to one resolved connector-side.
func registryPackageRunner(registryType string) (string, bool) {
	switch registryType {
	case "npm":
		return "npx", true
	case "pypi":
		return "uvx", true
	default:
		return "", false
	}
}

// registryPackageSpec pins a package identifier to a version when one is known.
func registryPackageSpec(identifier, version string) string {
	if version == "" {
		return identifier
	}
	return identifier + "@" + version
}

// registryPackageVersion prefers the package's own version over the entry's,
// so a package-level pin always wins over the server-level one.
func registryPackageVersion(pkgVersion, entryVersion string) string {
	if pkgVersion != "" {
		return pkgVersion
	}
	return entryVersion
}

// collectPackageRequiredConfig lists the required-or-secret env keys a local
// package declares, in sorted name order. The remote path's counterpart is
// collectRegistryRequiredConfig; both feed RequiredConfig so the configure form
// and install flash treat a device install like any other.
func collectPackageRequiredConfig(pkg RegistryPackage) []RequiredConfigKey {
	env := make(map[string]RequiredConfigKey)
	for _, ev := range pkg.EnvironmentVariables {
		if !ev.IsRequired && !ev.IsSecret {
			continue
		}
		env[ev.Name] = RequiredConfigKey{
			Name:        ev.Name,
			Kind:        "env",
			IsSecret:    ev.IsSecret,
			IsRequired:  ev.IsRequired || ev.IsSecret,
			Description: strings.TrimSpace(ev.Description),
		}
	}
	if len(env) == 0 {
		return nil
	}
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]RequiredConfigKey, 0, len(names))
	for _, name := range names {
		out = append(out, env[name])
	}
	return out
}

// buildRemoteDTO builds a CreateMCPServerDTO for a remote transport. Headers and
// env vars the registry flags as secret are recorded in the corresponding
// Secret*Keys lists so they are encrypted at rest rather than stored plaintext.
func buildRemoteDTO(name string, serverType MCPServerType, remote RegistryRemote) *CreateMCPServerDTO {
	url := remote.URL
	dto := &CreateMCPServerDTO{
		Name: name,
		Type: serverType,
		URL:  &url,
	}

	// Resolve required configuration from the selected transport's declaration.
	// When any required-or-secret key is declared, the install is created
	// disabled so a server that cannot authenticate is never enabled (and no
	// discovery is attempted) until the user configures it.
	required := collectRegistryRequiredConfig(remote)
	if len(required) > 0 {
		dto.RequiredConfig = required
		enabled := false
		dto.Enabled = &enabled
	}

	// Extract headers as key-value pairs
	if len(remote.Headers) > 0 {
		headers := make(map[string]any, len(remote.Headers))
		for _, h := range remote.Headers {
			if h.IsSecret {
				// Secret headers keep only a named, empty placeholder: the value
				// (e.g. "Bearer {api_key}") is a template, not a usable secret, so
				// storing it as plaintext is wrong and encrypting it is pointless.
				// Listing the key lets a later PATCH encrypt the real value.
				headers[h.Name] = ""
				dto.SecretHeadersKeys = append(dto.SecretHeadersKeys, h.Name)
				continue
			}
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
			if ev.IsSecret {
				dto.SecretEnvKeys = append(dto.SecretEnvKeys, ev.Name)
			}
		}
		dto.Env = env
	}

	return dto
}

// collectRegistryRequiredConfig gathers the required configuration keys declared
// by a remote transport: every header and environment variable marked
// isRequired or isSecret. A key marked isSecret counts as required even when
// isRequired is absent (a secret with no value is unusable). Deduped by
// (kind, name); deterministic — headers first, then env, each sorted by name.
//
// Each key also carries the registry's description and (for headers) its value
// template, so the configuration UI can show what to enter rather than only the
// key name.
func collectRegistryRequiredConfig(remote RegistryRemote) []RequiredConfigKey {
	headers := make(map[string]RequiredConfigKey)
	for _, h := range remote.Headers {
		if !h.IsRequired && !h.IsSecret {
			continue
		}
		headers[h.Name] = RequiredConfigKey{
			Name:        h.Name,
			Kind:        "header",
			IsSecret:    h.IsSecret,
			IsRequired:  h.IsRequired || h.IsSecret,
			Description: strings.TrimSpace(h.Description),
			ValueHint:   strings.TrimSpace(h.Value),
		}
	}
	env := make(map[string]RequiredConfigKey)
	for _, ev := range remote.EnvironmentVariables {
		if !ev.IsRequired && !ev.IsSecret {
			continue
		}
		env[ev.Name] = RequiredConfigKey{
			Name:        ev.Name,
			Kind:        "env",
			IsSecret:    ev.IsSecret,
			IsRequired:  ev.IsRequired || ev.IsSecret,
			Description: strings.TrimSpace(ev.Description),
		}
	}

	if len(headers) == 0 && len(env) == 0 {
		return []RequiredConfigKey{}
	}

	headerNames := make([]string, 0, len(headers))
	for name := range headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)

	envNames := make([]string, 0, len(env))
	for name := range env {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)

	out := make([]RequiredConfigKey, 0, len(headerNames)+len(envNames))
	for _, name := range headerNames {
		out = append(out, headers[name])
	}
	for _, name := range envNames {
		out = append(out, env[name])
	}
	return out
}

// collectRegistryEnvVars gathers all unique environment variables from a registry
// server definition (across both packages and remotes) and returns them as DTOs.
func collectRegistryEnvVars(regServer RegistryServer) []RegistryEnvVarDTO {
	seen := make(map[string]RegistryEnvVarDTO)
	for _, pkg := range regServer.Packages {
		for _, ev := range pkg.EnvironmentVariables {
			seen[ev.Name] = RegistryEnvVarDTO(ev)
		}
	}
	for _, remote := range regServer.Remotes {
		for _, ev := range remote.EnvironmentVariables {
			seen[ev.Name] = RegistryEnvVarDTO(ev)
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
			RegistryType:     pkg.RegistryType,
			Name:             pkg.Name,
			Identifier:       pkg.Identifier,
			Version:          pkg.Version,
			Transport:        pkg.Transport.Type,
			RuntimeArguments: renderArguments(pkg.RuntimeArguments),
			PackageArguments: renderArguments(pkg.PackageArguments),
		})
		for _, ev := range pkg.EnvironmentVariables {
			envVarMap[ev.Name] = RegistryEnvVarDTO(ev)
		}
	}

	// Collect all unique headers across remotes (packages do not declare
	// headers). Dedupe by name, preserving first; the value may be a template
	// like "Bearer {api_key}".
	headerMap := make(map[string]RegistryHeaderDTO)
	for _, remote := range s.Remotes {
		dto.Remotes = append(dto.Remotes, RegistryRemoteDTO{
			Type: remote.Type,
			URL:  remote.URL,
		})
		for _, ev := range remote.EnvironmentVariables {
			envVarMap[ev.Name] = RegistryEnvVarDTO(ev)
		}
		for _, h := range remote.Headers {
			if _, seen := headerMap[h.Name]; seen {
				continue
			}
			headerMap[h.Name] = RegistryHeaderDTO(h)
		}
	}

	if len(envVarMap) > 0 {
		envVars := make([]RegistryEnvVarDTO, 0, len(envVarMap))
		for _, ev := range envVarMap {
			envVars = append(envVars, ev)
		}
		dto.EnvVars = envVars
	}
	if len(headerMap) > 0 {
		headers := make([]RegistryHeaderDTO, 0, len(headerMap))
		for _, h := range headerMap {
			headers = append(headers, h)
		}
		dto.Headers = headers
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
	if dto.Headers == nil {
		dto.Headers = []RegistryHeaderDTO{}
	}

	return dto
}

// renderArguments renders a list of registry arguments to command tokens. For
// each argument: if Name is set, emit "--"+Name; then emit Value (falling back
// to Default) when non-empty. Positional (unnamed) arguments emit only their
// value; empty/omitted arguments emit nothing.
func renderArguments(args []RegistryArgument) []string {
	out := make([]string, 0, len(args)*2)
	for _, a := range args {
		if a.Name != "" {
			out = append(out, "--"+a.Name)
		}
		val := a.Value
		if val == "" {
			val = a.Default
		}
		if val != "" {
			out = append(out, val)
		}
	}
	return out
}

// --- Helpers ---

// DiscoveredTool represents a tool discovered from an external MCP server via tools/list.
type DiscoveredTool struct {
	Name         string         `json:"name"`
	Description  *string        `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema,omitempty"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
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

// schemaPtrToMap is schemaToMap for an optional schema (e.g. a tool's
// outputSchema): it returns nil when the pointer is nil, so no outputSchema is
// persisted rather than an empty object.
func schemaPtrToMap(schema *mcp.InputSchema) map[string]any {
	if schema == nil {
		return nil
	}
	return schemaToMap(*schema)
}

// splitSecrets separates secret entries from a plaintext env/headers map and
// encrypts them at rest. It returns the plaintext map with the secret keys
// removed, plus the merged encrypted-secret map.
//
// Semantics:
//   - A key listed in secretKeys whose value is a non-empty string is encrypted
//     and stored. A nil encryptor in this case is an error.
//   - A key listed in secretKeys whose value is empty/omitted retains the prior
//     ciphertext from existing (used by partial updates). On create existing is
//     nil, so nothing is stored for empty values.
//   - Keys not listed in secretKeys are removed from the encrypted map, even if
//     they previously had ciphertext.
//
// kind is used only for error messages ("env" or "header").
func splitSecrets(enc *crypto.Encryptor, values map[string]any, secretKeys []string, existing map[string]EncryptedSecret, kind string) (map[string]any, map[string]EncryptedSecret, error) {
	keySet := make(map[string]struct{}, len(secretKeys))
	for _, k := range secretKeys {
		keySet[k] = struct{}{}
	}

	plaintext := values
	if values != nil && len(keySet) > 0 {
		plaintext = make(map[string]any, len(values))
		for k, v := range values {
			if _, secret := keySet[k]; secret {
				continue
			}
			plaintext[k] = v
		}
	}

	secrets := make(map[string]EncryptedSecret, len(keySet))
	for _, k := range secretKeys {
		raw := ""
		if values != nil {
			if v, ok := values[k]; ok {
				raw = secretValueString(v)
			}
		}
		if raw != "" {
			if enc == nil {
				return nil, nil, fmt.Errorf("credential encryption not configured (LLM_ENCRYPTION_KEY missing)")
			}
			ct, nonce, err := enc.Encrypt([]byte(raw))
			if err != nil {
				return nil, nil, fmt.Errorf("encrypting secret %s %q: %w", kind, k, err)
			}
			secrets[k] = EncryptedSecret{Ciphertext: ct, Nonce: nonce}
			continue
		}
		// Empty/omitted value: retain previously stored ciphertext, if any.
		if prev, ok := existing[k]; ok {
			secrets[k] = prev
		}
	}

	return plaintext, secrets, nil
}

// secretValueString renders an env/header value as the string that will be
// encrypted. nil renders as empty; strings pass through unchanged; anything
// else uses the same %v formatting as the stdio env encoder.
func secretValueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// ============================================================================
// Tool Settings Inheritance Resolver
// ============================================================================

// ResolveBuiltinToolSettings resolves the effective enabled/config for a builtin
// tool for a given project, applying three-tier inheritance:
//
//  1. Project-level override (explicit `enabled_override` or `config` on the
//     builtin server's kb.mcp_server_tools row)
//  2. Org-level default (kb.org_tool_settings)
//  3. Global env fallback — enabled=true, config=nil
//
// The returned source is one of "project", "org", or "global". A project row
// that was bulk-upserted by EnsureBuiltinServer carries no explicit override
// (enabled_override is NULL), so it does NOT shadow an org-level default
// (issue #988).
func (s *Service) ResolveBuiltinToolSettings(ctx context.Context, projectID, toolName string) (enabled bool, config map[string]any, source string, err error) {
	// 1. Project level — only an explicit override (enabled_override or config)
	//    counts. A plain bulk-upserted row does not.
	projectTool, err := s.repo.FindBuiltinToolByProjectAndName(ctx, projectID, toolName)
	if err != nil {
		return false, nil, "", fmt.Errorf("resolve builtin tool project level: %w", err)
	}

	// 2. Org level — resolved once, and only consumed when no project enabled
	//    override decides the outcome.
	var orgSetting *orgToolSettingRow
	orgID, err := s.repo.FindOrgIDByProjectID(ctx, projectID)
	if err != nil {
		s.log.Warn("failed to find org for project, falling back to global",
			slog.String("projectID", projectID), slog.String("error", err.Error()))
	} else if orgID != "" {
		setting, ferr := s.repo.FindOrgToolSetting(ctx, orgID, toolName)
		if ferr != nil {
			s.log.Warn("failed to find org tool setting",
				slog.String("orgID", orgID), slog.String("error", ferr.Error()))
		}
		orgSetting = setting
	}

	projectEnabledOverride := projectTool != nil && projectTool.EnabledOverride != nil
	projectConfigOverride := projectTool != nil && projectTool.Config != nil

	// enabled: explicit project override → org default → builtin default (true).
	switch {
	case projectEnabledOverride:
		enabled = *projectTool.EnabledOverride
		source = "project"
	case orgSetting != nil:
		enabled = orgSetting.Enabled
		source = "org"
	default:
		enabled = true
		source = "global"
	}

	// config: project → org → nil. A config-only project override still marks the
	// tool as explicitly configured at the project level.
	if projectConfigOverride {
		config = projectTool.Config
		source = "project"
	} else if orgSetting != nil {
		config = orgSetting.Config
	}

	return enabled, config, source, nil
}

// ResolveBuiltinToolConfig is a convenience wrapper that returns only the
// effective config map and source for a builtin tool in a project.
// Used by tool executors that only need config (e.g. api_key injection).
func (s *Service) ResolveBuiltinToolConfig(ctx context.Context, projectID, toolName string) (config map[string]any, source string, err error) {
	_, cfg, src, err := s.ResolveBuiltinToolSettings(ctx, projectID, toolName)
	return cfg, src, err
}
