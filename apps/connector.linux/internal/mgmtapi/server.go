// Package mgmtapi exposes a loopback-only JSON management API for the
// connector's hosted MCP servers. A companion app or local web UI can list,
// create, update, enable/disable, and delete mcp_servers entries; each change
// is validated, persisted to the connector's YAML config, and applied live via
// a reload callback (no process restart).
//
// SECURITY: the API is intended to be bound to 127.0.0.1 only and performs NO
// authentication — any local process or browser page that can reach the port
// has full control over the connector's hosted servers (including secrets such
// as env vars and headers). This mirrors the local-companion trust model; do
// not bind it to a non-loopback address.
package mgmtapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
)

// reloadTimeout bounds one live reload (close old manager, start new servers).
const reloadTimeout = 30 * time.Second

// Reloader applies a just-persisted config live and returns the resulting
// per-server status. It is supplied by the caller (the relay command), which
// rebuilds the registry + mcphost.Manager and re-registers with the hub.
type Reloader func(ctx context.Context, cfg *config.Config) ([]mcphost.ServerStatus, error)

// RuntimeStatus is the live relay state reported by GET /api/status.
type RuntimeStatus struct {
	Connected bool
	ToolCount int
}

// RuntimeStatusFunc reports live relay state. Optional; defaults are zero.
type RuntimeStatusFunc func() RuntimeStatus

// Server is the management API state: the config file path, the in-memory
// config, the reload callback, and the latest per-server status.
type Server struct {
	mu         sync.Mutex
	configPath string
	cfg        *config.Config
	log        *slog.Logger
	reload     Reloader
	statuses   []mcphost.ServerStatus
	runtime    RuntimeStatusFunc
}

// NewServer builds a management API server. reload must be non-nil; the caller
// normally also calls SetStatuses with the initial mcphost.Manager.Status and
// SetRuntimeStatus with the relay client state.
func NewServer(configPath string, cfg *config.Config, log *slog.Logger, reload Reloader) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{configPath: configPath, cfg: cfg, log: log, reload: reload}
}

// SetStatuses replaces the cached per-server status (initial start or reload).
func (s *Server) SetStatuses(statuses []mcphost.ServerStatus) {
	s.mu.Lock()
	s.statuses = statuses
	s.mu.Unlock()
}

// SetRuntimeStatus installs the live relay-state provider used by
// GET /api/status.
func (s *Server) SetRuntimeStatus(fn RuntimeStatusFunc) {
	s.mu.Lock()
	s.runtime = fn
	s.mu.Unlock()
}

// Handler returns a mux with every route registered. Convenience wrapper
// around RegisterRoutes for callers that do not already own a mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	return mux
}

// RegisterRoutes registers the management API routes on mux. Each route is
// method-scoped, so the mux answers 405 for mismatched methods.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/mcp-servers", s.handleList)
	mux.HandleFunc("POST /api/mcp-servers", s.handleCreate)
	mux.HandleFunc("GET /api/mcp-servers/{name}", s.handleGet)
	mux.HandleFunc("PUT /api/mcp-servers/{name}/config", s.handleUpdateConfig)
	mux.HandleFunc("PUT /api/mcp-servers/{name}/enabled", s.handleSetEnabled)
	mux.HandleFunc("DELETE /api/mcp-servers/{name}", s.handleDelete)
	mux.HandleFunc("GET /api/mcp-servers/{name}/tools", s.handleTools)
	mux.HandleFunc("GET /api/status", s.handleStatus)
}

// --- request/response DTOs ---------------------------------------------------

// serverRequest is the create/replace payload. name is ignored on replace
// (the path wins).
type serverRequest struct {
	Name          string            `json:"name"`
	Transport     mcphost.Transport `json:"transport"`
	Enabled       *bool             `json:"enabled"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	DisabledTools []string          `json:"disabled_tools"`
}

func (r serverRequest) toConfig(name string) mcphost.ServerConfig {
	return mcphost.ServerConfig{
		Name:          name,
		Transport:     r.Transport,
		Enabled:       r.Enabled,
		Command:       r.Command,
		Args:          r.Args,
		Env:           r.Env,
		URL:           r.URL,
		Headers:       r.Headers,
		DisabledTools: r.DisabledTools,
	}
}

type serverSummary struct {
	Name      string            `json:"name"`
	Transport mcphost.Transport `json:"transport"`
	Enabled   bool              `json:"enabled"`
	Connected bool              `json:"connected"`
	Error     string            `json:"error,omitempty"`
	ToolCount int               `json:"toolCount"`
}

type serverRuntime struct {
	Connected bool                 `json:"connected"`
	Error     string               `json:"error,omitempty"`
	ToolCount int                  `json:"toolCount"`
	Tools     []mcphost.ToolStatus `json:"tools"`
}

type serverDetail struct {
	Name          string            `json:"name"`
	Transport     mcphost.Transport `json:"transport"`
	Enabled       bool              `json:"enabled"`
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	URL           string            `json:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	DisabledTools []string          `json:"disabled_tools,omitempty"`
	Status        serverRuntime     `json:"status"`
}

type statusResponse struct {
	InstanceID string          `json:"instanceID"`
	ServerURL  string          `json:"serverURL"`
	Connected  bool            `json:"connected"`
	ToolCount  int             `json:"toolCount"`
	Servers    []serverSummary `json:"servers"`
}

// --- handlers ----------------------------------------------------------------

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.summariesLocked())
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := r.PathValue("name")
	cfg, ok := s.findLocked(name)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mcp server %q not found", name))
		return
	}
	writeJSON(w, http.StatusOK, s.detailLocked(cfg))
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req serverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.findLocked(name); ok {
		writeError(w, http.StatusConflict, fmt.Sprintf("mcp server %q already exists", name))
		return
	}
	next := append(s.serversCopyLocked(), req.toConfig(name))
	statuses, err := s.commitLocked(next)
	if err != nil {
		writeCommitError(w, err)
		return
	}
	cfg, _ := s.findLocked(name)
	writeJSON(w, http.StatusCreated, detailFrom(cfg, statusFor(statuses, name)))
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req serverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := r.PathValue("name")

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.findLocked(name); !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mcp server %q not found", name))
		return
	}
	next := replaceServer(s.serversCopyLocked(), req.toConfig(name))
	statuses, err := s.commitLocked(next)
	if err != nil {
		writeCommitError(w, err)
		return
	}
	cfg, _ := s.findLocked(name)
	writeJSON(w, http.StatusOK, detailFrom(cfg, statusFor(statuses, name)))
}

func (s *Server) handleSetEnabled(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, `field "enabled" is required`)
		return
	}
	name := r.PathValue("name")

	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.findLocked(name)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mcp server %q not found", name))
		return
	}
	updated := cfg
	updated.Enabled = body.Enabled
	next := replaceServer(s.serversCopyLocked(), updated)
	statuses, err := s.commitLocked(next)
	if err != nil {
		writeCommitError(w, err)
		return
	}
	cfg, _ = s.findLocked(name)
	writeJSON(w, http.StatusOK, detailFrom(cfg, statusFor(statuses, name)))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.findLocked(name); !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mcp server %q not found", name))
		return
	}
	next := make([]mcphost.ServerConfig, 0, len(s.cfg.MCPServers))
	for _, sc := range s.cfg.MCPServers {
		if sc.Name != name {
			next = append(next, sc)
		}
	}
	if _, err := s.commitLocked(next); err != nil {
		writeCommitError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.findLocked(name); !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("mcp server %q not found", name))
		return
	}
	tools := statusFor(s.statuses, name).Tools
	if tools == nil {
		tools = []mcphost.ToolStatus{}
	}
	writeJSON(w, http.StatusOK, tools)
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := RuntimeStatus{}
	if s.runtime != nil {
		rt = s.runtime()
	}
	writeJSON(w, http.StatusOK, statusResponse{
		InstanceID: s.cfg.InstanceID,
		ServerURL:  s.cfg.ServerURL,
		Connected:  rt.Connected,
		ToolCount:  rt.ToolCount,
		Servers:    s.summariesLocked(),
	})
}

// --- config mutation ---------------------------------------------------------

// badRequest marks a validation failure (HTTP 400). commitLocked returns it
// before touching disk, so an invalid change never mutates config or state.
type badRequest struct{ msg string }

func (e *badRequest) Error() string { return e.msg }

// commitLocked validates next, persists it, then reloads live. The caller must
// hold s.mu. On validation failure nothing is written; on save/reload failure
// the in-memory config is left unchanged (the save error path) or already
// consistent with the file (the reload error path).
func (s *Server) commitLocked(next []mcphost.ServerConfig) ([]mcphost.ServerStatus, error) {
	if err := mcphost.ValidateServers(next); err != nil {
		return nil, &badRequest{msg: err.Error()}
	}
	snapshot := *s.cfg
	snapshot.MCPServers = next
	if err := snapshot.Save(s.configPath); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}
	// Persisted: make the in-memory config match the file before reloading so
	// the reloader sees the new server set. On reload failure the saved config
	// and in-memory config stay consistent (the file change is already durable).
	s.cfg.MCPServers = next
	ctx, cancel := context.WithTimeout(context.Background(), reloadTimeout)
	defer cancel()
	statuses, err := s.reload(ctx, s.cfg)
	if err != nil {
		return nil, fmt.Errorf("apply config: %w", err)
	}
	s.statuses = statuses
	return statuses, nil
}

// findLocked returns the configured server named name.
func (s *Server) findLocked(name string) (mcphost.ServerConfig, bool) {
	for _, sc := range s.cfg.MCPServers {
		if sc.Name == name {
			return sc, true
		}
	}
	return mcphost.ServerConfig{}, false
}

// serversCopyLocked returns a copy of the configured servers so callers can
// mutate their working set without touching s.cfg.
func (s *Server) serversCopyLocked() []mcphost.ServerConfig {
	out := make([]mcphost.ServerConfig, len(s.cfg.MCPServers))
	copy(out, s.cfg.MCPServers)
	return out
}

// summariesLocked builds the ordered per-server summary list, synthesizing an
// "unknown/not started" entry for any configured server without a status yet.
func (s *Server) summariesLocked() []serverSummary {
	out := make([]serverSummary, 0, len(s.cfg.MCPServers))
	for _, sc := range s.cfg.MCPServers {
		st := statusFor(s.statuses, sc.Name)
		out = append(out, serverSummary{
			Name:      sc.Name,
			Transport: sc.Transport,
			Enabled:   sc.IsEnabled(),
			Connected: st.Connected,
			Error:     st.Error,
			ToolCount: st.ToolCount,
		})
	}
	return out
}

// detailLocked builds the full config + runtime status for one server.
func (s *Server) detailLocked(cfg mcphost.ServerConfig) serverDetail {
	return detailFrom(cfg, statusFor(s.statuses, cfg.Name))
}

// detailFrom assembles a serverDetail; a missing status yields a zero runtime
// with an empty (non-null) tools list.
func detailFrom(cfg mcphost.ServerConfig, st mcphost.ServerStatus) serverDetail {
	tools := st.Tools
	if tools == nil {
		tools = []mcphost.ToolStatus{}
	}
	return serverDetail{
		Name:          cfg.Name,
		Transport:     cfg.Transport,
		Enabled:       cfg.IsEnabled(),
		Command:       cfg.Command,
		Args:          cfg.Args,
		Env:           cfg.Env,
		URL:           cfg.URL,
		Headers:       cfg.Headers,
		DisabledTools: cfg.DisabledTools,
		Status: serverRuntime{
			Connected: st.Connected,
			Error:     st.Error,
			ToolCount: st.ToolCount,
			Tools:     tools,
		},
	}
}

// replaceServer swaps the server with the same name into servers, or appends
// when absent (returning the possibly-grown slice).
func replaceServer(servers []mcphost.ServerConfig, cfg mcphost.ServerConfig) []mcphost.ServerConfig {
	for i, sc := range servers {
		if sc.Name == cfg.Name {
			servers[i] = cfg
			return servers
		}
	}
	return append(servers, cfg)
}

// statusFor returns the status for name, or a zero value when absent.
func statusFor(statuses []mcphost.ServerStatus, name string) mcphost.ServerStatus {
	for _, st := range statuses {
		if st.Name == name {
			return st
		}
	}
	return mcphost.ServerStatus{}
}

// --- HTTP helpers ------------------------------------------------------------

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// writeCommitError maps a commitLocked error to an HTTP response: validation
// failures are 400, everything else (save/reload) is 500.
func writeCommitError(w http.ResponseWriter, err error) {
	var br *badRequest
	if errors.As(err, &br) {
		writeError(w, http.StatusBadRequest, br.msg)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
