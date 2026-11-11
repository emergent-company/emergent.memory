package mgmtapi

import (
	"encoding/json"
	"net/http"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcpimport"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpconfig"
)

// importRequest is the body of POST /api/mcp-servers/import. Config is a
// Claude-format MCP document (mcpServers object or a single bare server); it is
// sent as JSON, never as a filesystem path, so the loopback API cannot be used
// to read arbitrary files.
type importRequest struct {
	Config    json.RawMessage `json:"config"`
	Overwrite bool            `json:"overwrite"`
	DryRun    bool            `json:"dryRun"`
}

// handleImport handles POST /api/mcp-servers/import. It merges the parsed
// Claude servers into the connector's local config, persisting and applying
// the result live unless DryRun is set. Memory-managed servers are read-only
// and always skipped; an overwrite preserves the existing per-server
// disabled_tools and enabled state.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Config) == 0 {
		writeError(w, http.StatusBadRequest, `field "config" is required`)
		return
	}

	parsed, err := mcpconfig.ParseClaudeConfig(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next, result := mcpimport.Apply(s.serversCopyLocked(), parsed, req.Overwrite, s.isManagedLocked)
	result.DryRun = req.DryRun

	if req.DryRun || !result.Changed() {
		writeJSON(w, http.StatusOK, result)
		return
	}

	if _, err := s.commitLocked(next); err != nil {
		writeCommitError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
