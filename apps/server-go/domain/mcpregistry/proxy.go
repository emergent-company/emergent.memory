package mcpregistry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/emergent/emergent-core/domain/mcp"
	"github.com/emergent/emergent-core/pkg/logger"
)

// ProxyManager manages connections to external MCP servers and proxies
// tool calls to them. It maintains a connection pool keyed by server ID,
// with lazy connect and automatic reconnection.
//
// The proxy strips the server name prefix from tool names before forwarding
// (e.g. "myserver_search" → calls "search" on the server named "myserver").
type ProxyManager struct {
	repo *Repository
	log  *slog.Logger

	mu    sync.RWMutex
	conns map[string]*mcpConnection // keyed by server ID
}

// mcpConnection wraps an mcp-go client with metadata for lifecycle management.
type mcpConnection struct {
	client      *mcpclient.Client
	serverID    string
	serverName  string
	serverType  MCPServerType
	connectedAt time.Time
	mu          sync.Mutex // guards connect/close operations
}

// NewProxyManager creates a new ProxyManager.
func NewProxyManager(repo *Repository, log *slog.Logger) *ProxyManager {
	return &ProxyManager{
		repo:  repo,
		log:   log.With(logger.Scope("mcpregistry.proxy")),
		conns: make(map[string]*mcpConnection),
	}
}

// CallTool connects to the appropriate external MCP server and forwards a tool call.
// The prefixedToolName includes the server name prefix (e.g. "myserver_search").
// This method:
//  1. Parses the prefix to identify the server name and real tool name
//  2. Looks up the server config from the database
//  3. Gets or creates a connection to the server
//  4. Forwards the tools/call request with the unprefixed tool name
//  5. Converts the response to our internal mcp.ToolResult format
func (pm *ProxyManager) CallTool(ctx context.Context, projectID, prefixedToolName string, args map[string]any) (*mcp.ToolResult, error) {
	// Parse prefix: "servername_toolname" → ("servername", "toolname")
	serverName, toolName, err := ParsePrefixedToolName(prefixedToolName)
	if err != nil {
		return nil, fmt.Errorf("parsing prefixed tool name: %w", err)
	}

	// Look up server config
	server, err := pm.repo.FindServerByName(ctx, projectID, serverName)
	if err != nil {
		return nil, fmt.Errorf("looking up server %q: %w", serverName, err)
	}
	if server == nil {
		return nil, fmt.Errorf("MCP server %q not found in project", serverName)
	}
	if !server.Enabled {
		return nil, fmt.Errorf("MCP server %q is disabled", serverName)
	}

	// Get or create connection
	conn, err := pm.getOrConnect(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("connecting to MCP server %q: %w", serverName, err)
	}

	// Forward the tool call with the unprefixed name
	pm.log.Debug("proxying tool call",
		slog.String("server", serverName),
		slog.String("tool", toolName),
		slog.String("prefixed", prefixedToolName),
	)

	result, err := conn.client.CallTool(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	})
	if err != nil {
		// On call failure, evict the connection so next call reconnects
		pm.evict(server.ID)
		return nil, fmt.Errorf("calling tool %q on server %q: %w", toolName, serverName, err)
	}

	// Convert mcp-go CallToolResult to our internal mcp.ToolResult
	return convertCallToolResult(result), nil
}

// DiscoverTools connects to an external MCP server and calls tools/list to discover
// available tools. Returns the discovered tools for syncing to the database.
func (pm *ProxyManager) DiscoverTools(ctx context.Context, server *MCPServer) ([]DiscoveredTool, error) {
	conn, err := pm.getOrConnect(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("connecting to MCP server %q: %w", server.Name, err)
	}

	result, err := conn.client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		pm.evict(server.ID)
		return nil, fmt.Errorf("listing tools on server %q: %w", server.Name, err)
	}

	tools := make([]DiscoveredTool, 0, len(result.Tools))
	for _, t := range result.Tools {
		dt := DiscoveredTool{
			Name:        t.Name,
			InputSchema: convertToolInputSchema(t.InputSchema),
		}
		if t.Description != "" {
			desc := t.Description
			dt.Description = &desc
		}
		tools = append(tools, dt)
	}

	pm.log.Info("discovered tools from MCP server",
		slog.String("server", server.Name),
		slog.String("server_id", server.ID),
		slog.Int("tool_count", len(tools)),
	)

	return tools, nil
}

// InspectServer creates a fresh, ephemeral connection to an MCP server (not pooled),
// captures the InitializeResult (server name, version, capabilities), then enumerates
// tools, prompts, and resources based on what the server advertises. The connection
// is closed when done. This is a diagnostic/test operation.
func (pm *ProxyManager) InspectServer(ctx context.Context, server *MCPServer) (*MCPServerInspectDTO, error) {
	startTime := time.Now()

	result := &MCPServerInspectDTO{
		ServerID:          server.ID,
		ServerName:        server.Name,
		ServerType:        server.Type,
		Tools:             []InspectToolDTO{},
		Prompts:           []InspectPromptDTO{},
		Resources:         []InspectResourceDTO{},
		ResourceTemplates: []InspectResourceTemplateDTO{},
	}

	// Create a fresh, ephemeral connection (not pooled)
	client, initResult, err := pm.connectAndInitialize(ctx, server)
	latencyMs := time.Since(startTime).Milliseconds()
	result.LatencyMs = latencyMs

	if err != nil {
		result.Status = "error"
		errMsg := err.Error()
		result.Error = &errMsg
		return result, nil // Return the DTO with error info, not a Go error
	}
	defer client.Close()

	result.Status = "ok"

	// Populate server info from InitializeResult
	if initResult != nil {
		result.ServerInfo = &InspectServerInfoDTO{
			Name:            initResult.ServerInfo.Name,
			Version:         initResult.ServerInfo.Version,
			ProtocolVersion: initResult.ProtocolVersion,
			Instructions:    initResult.Instructions,
		}

		caps := &InspectCapabilitiesDTO{
			Tools:       initResult.Capabilities.Tools != nil,
			Prompts:     initResult.Capabilities.Prompts != nil,
			Resources:   initResult.Capabilities.Resources != nil,
			Logging:     initResult.Capabilities.Logging != nil,
			Completions: initResult.Capabilities.Completions != nil,
		}
		result.Capabilities = caps

		// Enumerate tools if supported
		if caps.Tools {
			toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
			if err != nil {
				pm.log.Warn("inspect: failed to list tools",
					slog.String("server", server.Name),
					slog.String("error", err.Error()),
				)
			} else {
				for _, t := range toolsResult.Tools {
					it := InspectToolDTO{
						Name:        t.Name,
						Description: t.Description,
						InputSchema: convertToolInputSchema(t.InputSchema),
					}
					result.Tools = append(result.Tools, it)
				}
			}
		}

		// Enumerate prompts if supported
		if caps.Prompts {
			promptsResult, err := client.ListPrompts(ctx, mcpgo.ListPromptsRequest{})
			if err != nil {
				pm.log.Warn("inspect: failed to list prompts",
					slog.String("server", server.Name),
					slog.String("error", err.Error()),
				)
			} else {
				for _, p := range promptsResult.Prompts {
					ip := InspectPromptDTO{
						Name:        p.Name,
						Description: p.Description,
					}
					for _, a := range p.Arguments {
						ip.Arguments = append(ip.Arguments, InspectPromptArgDTO{
							Name:        a.Name,
							Description: a.Description,
							Required:    a.Required,
						})
					}
					if ip.Arguments == nil {
						ip.Arguments = []InspectPromptArgDTO{}
					}
					result.Prompts = append(result.Prompts, ip)
				}
			}
		}

		// Enumerate resources if supported
		if caps.Resources {
			resourcesResult, err := client.ListResources(ctx, mcpgo.ListResourcesRequest{})
			if err != nil {
				pm.log.Warn("inspect: failed to list resources",
					slog.String("server", server.Name),
					slog.String("error", err.Error()),
				)
			} else {
				for _, r := range resourcesResult.Resources {
					result.Resources = append(result.Resources, InspectResourceDTO{
						URI:         r.URI,
						Name:        r.Name,
						Description: r.Description,
						MimeType:    r.MIMEType,
					})
				}
			}

			// Also list resource templates
			templatesResult, err := client.ListResourceTemplates(ctx, mcpgo.ListResourceTemplatesRequest{})
			if err != nil {
				pm.log.Warn("inspect: failed to list resource templates",
					slog.String("server", server.Name),
					slog.String("error", err.Error()),
				)
			} else {
				for _, rt := range templatesResult.ResourceTemplates {
					uriStr := ""
					if rt.URITemplate != nil {
						uriStr = rt.URITemplate.Raw()
					}
					result.ResourceTemplates = append(result.ResourceTemplates, InspectResourceTemplateDTO{
						URITemplate: uriStr,
						Name:        rt.Name,
						Description: rt.Description,
						MimeType:    rt.MIMEType,
					})
				}
			}
		}
	}

	pm.log.Info("inspected MCP server",
		slog.String("server", server.Name),
		slog.String("server_id", server.ID),
		slog.String("status", result.Status),
		slog.Int64("latency_ms", result.LatencyMs),
		slog.Int("tools", len(result.Tools)),
		slog.Int("prompts", len(result.Prompts)),
		slog.Int("resources", len(result.Resources)),
	)

	return result, nil
}

// connectAndInitialize creates a fresh mcp-go client and initializes it,
// returning both the client and the InitializeResult. Unlike initializeClient(),
// this captures the InitializeResult for inspection purposes.
func (pm *ProxyManager) connectAndInitialize(ctx context.Context, server *MCPServer) (*mcpclient.Client, *mcpgo.InitializeResult, error) {
	var client *mcpclient.Client
	var err error

	switch server.Type {
	case ServerTypeStdio:
		if server.Command == nil || *server.Command == "" {
			return nil, nil, fmt.Errorf("command is required for stdio server")
		}
		env := envMapToSlice(server.Env)
		client, err = mcpclient.NewStdioMCPClient(*server.Command, env, server.Args...)
		if err != nil {
			return nil, nil, fmt.Errorf("creating stdio client: %w", err)
		}

	case ServerTypeSSE:
		if server.URL == nil || *server.URL == "" {
			return nil, nil, fmt.Errorf("url is required for SSE server")
		}
		var opts []transport.ClientOption
		headers := headersMapToStringMap(server.Headers)
		if len(headers) > 0 {
			opts = append(opts, transport.WithHeaders(headers))
		}
		client, err = mcpclient.NewSSEMCPClient(*server.URL, opts...)
		if err != nil {
			return nil, nil, fmt.Errorf("creating SSE client: %w", err)
		}
		if err := client.Start(ctx); err != nil {
			client.Close()
			return nil, nil, fmt.Errorf("starting SSE client: %w", err)
		}

	case ServerTypeHTTP:
		if server.URL == nil || *server.URL == "" {
			return nil, nil, fmt.Errorf("url is required for HTTP server")
		}
		var opts []transport.StreamableHTTPCOption
		headers := headersMapToStringMap(server.Headers)
		if len(headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(headers))
		}
		t, err := transport.NewStreamableHTTP(*server.URL, opts...)
		if err != nil {
			return nil, nil, fmt.Errorf("creating HTTP transport: %w", err)
		}
		client = mcpclient.NewClient(t)
		if err := client.Start(ctx); err != nil {
			client.Close()
			return nil, nil, fmt.Errorf("starting HTTP client: %w", err)
		}

	default:
		return nil, nil, fmt.Errorf("unsupported server type for inspect: %s", server.Type)
	}

	// Initialize and capture the result
	initResult, err := client.Initialize(ctx, mcpgo.InitializeRequest{
		Params: mcpgo.InitializeParams{
			ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcpgo.Implementation{
				Name:    "emergent",
				Version: "1.0.0",
			},
		},
	})
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("initializing MCP session with %q: %w", server.Name, err)
	}

	return client, initResult, nil
}

// Close closes all active connections.
func (pm *ProxyManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for id, conn := range pm.conns {
		if err := conn.client.Close(); err != nil {
			pm.log.Warn("error closing MCP connection",
				slog.String("server_id", id),
				slog.String("server_name", conn.serverName),
				slog.String("error", err.Error()),
			)
		}
	}
	pm.conns = make(map[string]*mcpConnection)
	pm.log.Info("closed all MCP proxy connections")
}

// CloseServer closes the connection to a specific server (e.g. when server config changes).
func (pm *ProxyManager) CloseServer(serverID string) {
	pm.evict(serverID)
}

// --- Internal ---

// getOrConnect returns an existing connection or creates a new one.
func (pm *ProxyManager) getOrConnect(ctx context.Context, server *MCPServer) (*mcpConnection, error) {
	// Fast path: read lock
	pm.mu.RLock()
	if conn, ok := pm.conns[server.ID]; ok {
		pm.mu.RUnlock()
		return conn, nil
	}
	pm.mu.RUnlock()

	// Slow path: write lock
	pm.mu.Lock()
	// Double-check
	if conn, ok := pm.conns[server.ID]; ok {
		pm.mu.Unlock()
		return conn, nil
	}

	// Create placeholder connection entry (release pool lock during connect)
	conn := &mcpConnection{
		serverID:   server.ID,
		serverName: server.Name,
		serverType: server.Type,
	}
	pm.conns[server.ID] = conn
	pm.mu.Unlock()

	// Lock the connection itself for the connect operation
	conn.mu.Lock()
	defer conn.mu.Unlock()

	// If someone else connected while we waited, return
	if conn.client != nil {
		return conn, nil
	}

	client, err := pm.connect(ctx, server)
	if err != nil {
		// Remove failed placeholder
		pm.mu.Lock()
		delete(pm.conns, server.ID)
		pm.mu.Unlock()
		return nil, err
	}

	conn.client = client
	conn.connectedAt = time.Now()

	pm.log.Info("connected to MCP server",
		slog.String("server", server.Name),
		slog.String("server_id", server.ID),
		slog.String("type", string(server.Type)),
	)

	return conn, nil
}

// connect creates a new mcp-go client for the given server configuration.
func (pm *ProxyManager) connect(ctx context.Context, server *MCPServer) (*mcpclient.Client, error) {
	switch server.Type {
	case ServerTypeStdio:
		return pm.connectStdio(ctx, server)
	case ServerTypeSSE:
		return pm.connectSSE(ctx, server)
	case ServerTypeHTTP:
		return pm.connectHTTP(ctx, server)
	default:
		return nil, fmt.Errorf("unsupported server type: %s", server.Type)
	}
}

// connectStdio creates a stdio MCP client that launches a subprocess.
func (pm *ProxyManager) connectStdio(ctx context.Context, server *MCPServer) (*mcpclient.Client, error) {
	if server.Command == nil || *server.Command == "" {
		return nil, fmt.Errorf("command is required for stdio server")
	}

	// Convert env map to []string{"KEY=value"} format
	env := envMapToSlice(server.Env)

	c, err := mcpclient.NewStdioMCPClient(*server.Command, env, server.Args...)
	if err != nil {
		return nil, fmt.Errorf("creating stdio client: %w", err)
	}

	// Initialize the MCP session
	if err := pm.initializeClient(ctx, c, server.Name); err != nil {
		c.Close()
		return nil, err
	}

	return c, nil
}

// connectSSE creates an SSE MCP client.
func (pm *ProxyManager) connectSSE(ctx context.Context, server *MCPServer) (*mcpclient.Client, error) {
	if server.URL == nil || *server.URL == "" {
		return nil, fmt.Errorf("url is required for SSE server")
	}

	var opts []transport.ClientOption
	headers := headersMapToStringMap(server.Headers)
	if len(headers) > 0 {
		opts = append(opts, transport.WithHeaders(headers))
	}

	c, err := mcpclient.NewSSEMCPClient(*server.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating SSE client: %w", err)
	}

	if err := c.Start(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("starting SSE client: %w", err)
	}

	if err := pm.initializeClient(ctx, c, server.Name); err != nil {
		c.Close()
		return nil, err
	}

	return c, nil
}

// connectHTTP creates a Streamable HTTP MCP client.
func (pm *ProxyManager) connectHTTP(ctx context.Context, server *MCPServer) (*mcpclient.Client, error) {
	if server.URL == nil || *server.URL == "" {
		return nil, fmt.Errorf("url is required for HTTP server")
	}

	var opts []transport.StreamableHTTPCOption
	headers := headersMapToStringMap(server.Headers)
	if len(headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(headers))
	}

	t, err := transport.NewStreamableHTTP(*server.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP transport: %w", err)
	}

	c := mcpclient.NewClient(t)
	if err := c.Start(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("starting HTTP client: %w", err)
	}

	if err := pm.initializeClient(ctx, c, server.Name); err != nil {
		c.Close()
		return nil, err
	}

	return c, nil
}

// initializeClient sends the MCP Initialize request.
func (pm *ProxyManager) initializeClient(ctx context.Context, c *mcpclient.Client, serverName string) error {
	_, err := c.Initialize(ctx, mcpgo.InitializeRequest{
		Params: mcpgo.InitializeParams{
			ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcpgo.Implementation{
				Name:    "emergent",
				Version: "1.0.0",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("initializing MCP session with %q: %w", serverName, err)
	}
	return nil
}

// evict removes and closes a connection from the pool.
func (pm *ProxyManager) evict(serverID string) {
	pm.mu.Lock()
	conn, ok := pm.conns[serverID]
	if ok {
		delete(pm.conns, serverID)
	}
	pm.mu.Unlock()

	if ok && conn.client != nil {
		if err := conn.client.Close(); err != nil {
			pm.log.Warn("error closing evicted MCP connection",
				slog.String("server_id", serverID),
				slog.String("server_name", conn.serverName),
				slog.String("error", err.Error()),
			)
		}
		pm.log.Info("evicted MCP connection",
			slog.String("server_id", serverID),
			slog.String("server_name", conn.serverName),
		)
	}
}

// --- Helpers ---

// ParsePrefixedToolName splits a prefixed tool name "servername_toolname" into its parts.
// The server name is everything before the first underscore, the tool name is the rest.
func ParsePrefixedToolName(prefixed string) (serverName, toolName string, err error) {
	idx := strings.Index(prefixed, "_")
	if idx < 0 || idx == 0 || idx == len(prefixed)-1 {
		return "", "", fmt.Errorf("invalid prefixed tool name %q: expected format 'servername_toolname'", prefixed)
	}
	return prefixed[:idx], prefixed[idx+1:], nil
}

// convertCallToolResult converts an mcp-go CallToolResult to our internal mcp.ToolResult.
// Our internal ContentBlock only supports Type+Text, so non-text content is converted
// to a text description. This is sufficient since the LLM pipeline processes text.
func convertCallToolResult(result *mcpgo.CallToolResult) *mcp.ToolResult {
	if result == nil {
		return &mcp.ToolResult{}
	}

	tr := &mcp.ToolResult{
		IsError: result.IsError,
	}

	for _, content := range result.Content {
		block := mcp.ContentBlock{Type: "text"}

		if tc, ok := mcpgo.AsTextContent(content); ok {
			block.Text = tc.Text
		} else if _, ok := mcpgo.AsImageContent(content); ok {
			block.Text = "[image content]"
		} else if _, ok := mcpgo.AsAudioContent(content); ok {
			block.Text = "[audio content]"
		} else {
			block.Text = fmt.Sprintf("[unsupported content type: %T]", content)
		}

		tr.Content = append(tr.Content, block)
	}

	return tr
}

// convertToolInputSchema converts an mcp-go ToolInputSchema to a map[string]any for storage.
func convertToolInputSchema(schema mcpgo.ToolInputSchema) map[string]any {
	m := map[string]any{
		"type": schema.Type,
	}
	if schema.Properties != nil {
		m["properties"] = schema.Properties
	}
	if len(schema.Required) > 0 {
		m["required"] = schema.Required
	}
	if schema.AdditionalProperties != nil {
		m["additionalProperties"] = schema.AdditionalProperties
	}
	return m
}

// envMapToSlice converts a map[string]any (from JSONB) to []string{"KEY=value"} format
// for use with the stdio transport.
func envMapToSlice(env map[string]any) []string {
	if len(env) == 0 {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%v", k, v))
	}
	return result
}

// headersMapToStringMap converts a map[string]any (from JSONB) to map[string]string
// for use with HTTP/SSE transports.
func headersMapToStringMap(headers map[string]any) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		result[k] = fmt.Sprintf("%v", v)
	}
	return result
}
