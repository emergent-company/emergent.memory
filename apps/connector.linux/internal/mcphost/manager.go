package mcphost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// clientName and clientVersion identify the connector to hosted MCP servers
// during the MCP initialize handshake.
const (
	clientName    = "memory-connector"
	clientVersion = "0.1.0"
)

// mcpClient is the subset of *client.Client the Manager uses. *client.Client
// satisfies it; tests inject a fake.
type mcpClient interface {
	Start(ctx context.Context) error
	Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

// ToolStatus describes one discovered hosted tool under its local namespaced
// name (<serverName>_<toolName>), as reported by Manager.Status.
type ToolStatus struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ServerStatus is the live state of one configured hosted server. Disabled
// servers appear with Enabled=false and Connected=false.
type ServerStatus struct {
	Name      string       `json:"name"`
	Transport Transport    `json:"transport"`
	Enabled   bool         `json:"enabled"`
	Connected bool         `json:"connected"`
	Error     string       `json:"error,omitempty"`
	ToolCount int          `json:"toolCount"`
	Tools     []ToolStatus `json:"tools"`
}

// Manager hosts a set of local MCP servers and registers a selected subset of
// their tools into a toolreg.Registry. Registering into the existing registry
// means hosted tools flow through the relay exactly like the built-in tools:
// the hub names them <instanceID>_<serverName>_<toolName>, while the connector
// registry key is the namespaced local name <serverName>_<toolName>.
type Manager struct {
	servers []ServerConfig
	dial    func(ServerConfig) (mcpClient, error)

	mu      sync.Mutex
	clients []mcpClient
	status  []ServerStatus
	closed  bool
}

// NewManager returns a Manager for the given server configs. Nothing connects
// until Start is called.
func NewManager(servers []ServerConfig) *Manager {
	return &Manager{servers: servers, dial: dialServer}
}

// Start validates the config, connects each enabled server, runs the MCP
// initialize handshake, lists its tools, and registers every tool that is not
// on that server's disabled_tools list under <serverName>_<toolName>.
//
// A server that fails to connect or initialize is logged and skipped so one
// broken server never takes down the others. Duplicate server names and
// duplicate resulting tool names are hard errors (fail fast).
func (m *Manager) Start(ctx context.Context, reg *toolreg.Registry, log *slog.Logger) error {
	if reg == nil {
		return fmt.Errorf("mcphost: tool registry is required")
	}
	if log == nil {
		log = slog.Default()
	}
	if err := ValidateServers(m.servers); err != nil {
		return fmt.Errorf("mcphost: %w", err)
	}

	m.mu.Lock()
	m.status = make([]ServerStatus, 0, len(m.servers))
	m.mu.Unlock()

	for _, cfg := range m.servers {
		if !cfg.IsEnabled() {
			log.Debug("mcphost: server disabled, skipping", "server", cfg.Name)
			m.appendStatus(ServerStatus{
				Name:      cfg.Name,
				Transport: cfg.Transport,
				Enabled:   false,
				Tools:     []ToolStatus{},
			})
			continue
		}
		if err := m.startServer(ctx, cfg, reg, log); err != nil {
			return err
		}
	}
	return nil
}

// appendStatus records the final state of one server, preserving config order.
func (m *Manager) appendStatus(st ServerStatus) {
	m.mu.Lock()
	m.status = append(m.status, st)
	m.mu.Unlock()
}

// Status returns a snapshot of every configured server's state, in config
// order. It is safe to call while servers are being started (entries appear as
// each server is processed).
func (m *Manager) Status() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerStatus, len(m.status))
	for i, st := range m.status {
		out[i] = st
		if st.Tools != nil {
			tools := make([]ToolStatus, len(st.Tools))
			copy(tools, st.Tools)
			out[i].Tools = tools
		}
	}
	return out
}

// startServer connects one server and registers its selected tools. Connection
// and listing failures are recorded in Status and swallowed (returns nil);
// registration conflicts are recorded and returned as errors.
func (m *Manager) startServer(ctx context.Context, cfg ServerConfig, reg *toolreg.Registry, log *slog.Logger) error {
	st := ServerStatus{
		Name:      cfg.Name,
		Transport: cfg.Transport,
		Enabled:   true,
		Tools:     []ToolStatus{},
	}

	// fail records an unavailable server in Status and returns nil so one bad
	// server never aborts the others.
	fail := func(err error) {
		st.Connected = false
		st.Error = err.Error()
		st.ToolCount = 0
		st.Tools = []ToolStatus{}
		m.appendStatus(st)
		log.Warn("mcphost: server unavailable, skipping", "server", cfg.Name, "err", err)
	}

	cl, err := m.dial(cfg)
	if err != nil {
		fail(err)
		return nil
	}

	if err := cl.Start(ctx); err != nil {
		_ = cl.Close()
		fail(err)
		return nil
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: clientName, Version: clientVersion}
	if _, err := cl.Initialize(ctx, initReq); err != nil {
		_ = cl.Close()
		fail(err)
		return nil
	}

	listed, err := cl.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = cl.Close()
		fail(err)
		return nil
	}

	disabled := make(map[string]struct{}, len(cfg.DisabledTools))
	for _, name := range cfg.DisabledTools {
		disabled[name] = struct{}{}
	}

	// hardErr records a registration conflict and returns it as a hard error.
	hardErr := func(err error) error {
		_ = cl.Close()
		st.Connected = false
		st.Error = err.Error()
		st.ToolCount = 0
		st.Tools = []ToolStatus{}
		m.appendStatus(st)
		return err
	}

	// Build every candidate first so a conflict registers nothing from this
	// server (partial registration would leave a confusing half-state).
	pending := make([]toolreg.Tool, 0, len(listed.Tools))
	tools := make([]ToolStatus, 0, len(listed.Tools))
	batch := make(map[string]struct{}, len(listed.Tools))
	for _, t := range listed.Tools {
		if _, skip := disabled[t.Name]; skip {
			log.Debug("mcphost: tool disabled by config", "server", cfg.Name, "tool", t.Name)
			continue
		}
		localName := cfg.Name + "_" + t.Name
		if _, dup := batch[localName]; dup {
			return hardErr(fmt.Errorf("server %q exposes duplicate tool name %q (registers as %q)", cfg.Name, t.Name, localName))
		}
		if _, exists := reg.Lookup(localName); exists {
			return hardErr(fmt.Errorf("server %q tool %q registers as %q which is already registered", cfg.Name, t.Name, localName))
		}
		batch[localName] = struct{}{}
		schema := toolInputSchema(t)
		pending = append(pending, newHostedTool(cfg.Name, t, schema, cl))
		tools = append(tools, ToolStatus{Name: localName, Description: t.Description, InputSchema: schema})
	}

	for _, tool := range pending {
		if err := reg.Register(tool); err != nil {
			return hardErr(fmt.Errorf("register tool %q from server %q: %w", tool.Name, cfg.Name, err))
		}
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return hardErr(fmt.Errorf("manager is closed"))
	}
	m.clients = append(m.clients, cl)
	m.mu.Unlock()

	st.Connected = true
	st.Error = ""
	st.Tools = tools
	st.ToolCount = len(tools)
	m.appendStatus(st)

	log.Info("mcphost: server started", "server", cfg.Name, "tools", len(pending))
	return nil
}

// newHostedTool builds a registry tool that forwards calls to cl. toolName is
// the server's own (un-namespaced) tool name; the registry key is namespaced.
func newHostedTool(serverName string, t mcp.Tool, schema map[string]any, cl mcpClient) toolreg.Tool {
	remoteName := t.Name
	return toolreg.Tool{
		Name:        serverName + "_" + remoteName,
		Description: t.Description,
		InputSchema: schema,
		Handler: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			req := mcp.CallToolRequest{}
			req.Params.Name = remoteName
			req.Params.Arguments = args
			res, err := cl.CallTool(ctx, req)
			if err != nil {
				return nil, fmt.Errorf("mcphost: server %q tool %q: %w", serverName, remoteName, err)
			}
			return callResultToMap(res)
		},
	}
}

// toolInputSchema extracts a JSON-schema map from an mcp.Tool, honoring a raw
// schema when the server set one. A tool with no schema yields a valid empty
// object schema so registration never fails on schema shape.
func toolInputSchema(t mcp.Tool) map[string]any {
	empty := map[string]any{"type": "object", "properties": map[string]any{}}
	raw, err := json.Marshal(t)
	if err != nil {
		return empty
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return empty
	}
	schema, ok := decoded["inputSchema"].(map[string]any)
	if !ok || len(schema) == 0 {
		return empty
	}
	return schema
}

// callResultToMap converts an MCP tool result into the map[string]any shape
// the relay tool handlers return (the map is sent verbatim as the relay
// response payload). Result content is preserved as
// {"content":[...],"isError":...}.
func callResultToMap(res *mcp.CallToolResult) (map[string]any, error) {
	if res == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("mcphost: marshal tool result: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("mcphost: decode tool result: %w", err)
	}
	return out, nil
}

// Close closes every hosted client. Safe to call more than once.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	clients := m.clients
	m.clients = nil
	m.mu.Unlock()

	for _, cl := range clients {
		_ = cl.Close()
	}
}

// dialServer constructs a live MCP client for cfg. Stdio clients auto-start
// their transport; Manager.Start still calls Start for every client, which is
// idempotent per the mcp-go contract.
func dialServer(cfg ServerConfig) (mcpClient, error) {
	switch cfg.Transport {
	case TransportStdio:
		return client.NewStdioMCPClient(cfg.Command, envSlice(cfg.Env), cfg.Args...)
	case TransportHTTP:
		return client.NewStreamableHttpClient(cfg.URL, transport.WithHTTPHeaders(cfg.Headers))
	case TransportSSE:
		return client.NewSSEMCPClient(cfg.URL, transport.WithHeaders(cfg.Headers))
	default:
		return nil, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
}

// envSlice flattens an env map into sorted KEY=VALUE entries for a stdio
// subprocess.
func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(env))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}
