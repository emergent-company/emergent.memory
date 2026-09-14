package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// --- relay-node registry (settings KV) ---
//
// The registry remembers every external MCP relay node the gateway has observed
// for the project, so a node that disconnects stays visible as "offline" with a
// last-seen time (and its cached tool snapshot). It mirrors the device-key
// registry: one settings entry (category mcp_relay_nodes, key registry) whose
// value is {"nodes": {"<instanceID>": {...}}} — Memory has no list-by-category
// route, so per-node keys could never be enumerated.
//
// The read-modify-write is unlocked (same accepted single-admin risk as the
// device registry): concurrent page loads could drop a node record, which is
// acceptable here.
const (
	relayRegistryCategory = "mcp_relay_nodes"
	relayRegistryKey      = "registry"
)

// relayNodeRecord is one observed relay node as persisted in the registry. It
// is the gateway-side identity of a node (the backend session is in-memory and
// vanishes on disconnect); InstanceID is the registry key.
type relayNodeRecord struct {
	InstanceID string
	Version    string
	ToolCount  int
	Tools      []RelayTool
	FirstSeen  time.Time
	LastSeen   time.Time
}

// loadRelayRegistry reads the relay-node registry. A missing setting
// (GetProjectSetting maps 404 → (nil, nil)) yields an empty registry, and a
// corrupt/partial value degrades to the parseable records rather than an error
// — a bad write must never wedge the page. A non-nil memory error is surfaced.
func (s *Server) loadRelayRegistry(ctx context.Context) (map[string]relayNodeRecord, error) {
	ps, err := s.memory.GetProjectSetting(ctx, relayRegistryCategory, relayRegistryKey)
	if err != nil {
		return nil, err
	}
	nodes := map[string]relayNodeRecord{}
	if ps == nil {
		return nodes, nil
	}
	raw, _ := ps.Value["nodes"].(map[string]any)
	for id, v := range raw {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		nodes[id] = relayRecordFromMap(id, entry)
	}
	return nodes, nil
}

// relayRecordFromMap decodes one stored entry. Missing or mistyped fields
// degrade to zero values; entries without a usable instance id are keyed by the
// registry map key instead.
func relayRecordFromMap(id string, m map[string]any) relayNodeRecord {
	rec := relayNodeRecord{InstanceID: id}
	rec.Version, _ = m["version"].(string)
	switch n := m["toolCount"].(type) {
	case int:
		rec.ToolCount = n
	case float64:
		rec.ToolCount = int(n)
	}
	rec.FirstSeen = parseRelayTime(m["firstSeen"])
	rec.LastSeen = parseRelayTime(m["lastSeen"])
	if arr, ok := m["tools"].([]any); ok {
		for _, item := range arr {
			tm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := tm["name"].(string)
			if strings.TrimSpace(name) == "" {
				continue
			}
			desc, _ := tm["description"].(string)
			rec.Tools = append(rec.Tools, RelayTool{Name: name, Description: desc})
		}
	}
	return rec
}

// relayRecordToMap encodes one record for the settings value. Absent timestamps
// and a nil tool snapshot are omitted so the stored shape stays minimal.
func relayRecordToMap(rec relayNodeRecord) map[string]any {
	m := map[string]any{
		"version":   rec.Version,
		"toolCount": rec.ToolCount,
	}
	if rec.Tools != nil {
		tools := make([]any, 0, len(rec.Tools))
		for _, t := range rec.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description})
		}
		m["tools"] = tools
	}
	if !rec.FirstSeen.IsZero() {
		m["firstSeen"] = rec.FirstSeen.UTC().Format(time.RFC3339)
	}
	if !rec.LastSeen.IsZero() {
		m["lastSeen"] = rec.LastSeen.UTC().Format(time.RFC3339)
	}
	return m
}

// parseRelayTime parses a stored RFC3339 timestamp, returning the zero time for
// absent/garbage values (the UI renders "last seen unknown" rather than failing).
func parseRelayTime(v any) time.Time {
	s, _ := v.(string)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// saveRelayRegistry writes the whole registry back as one settings value.
func (s *Server) saveRelayRegistry(ctx context.Context, nodes map[string]relayNodeRecord) error {
	raw := make(map[string]any, len(nodes))
	for id, rec := range nodes {
		raw[id] = relayRecordToMap(rec)
	}
	return s.memory.SetProjectSetting(ctx, relayRegistryCategory, relayRegistryKey, map[string]any{"nodes": raw})
}

// upsertRelayNode writes rec into the registry, keyed by rec.InstanceID. It
// refreshes version/toolCount/tools/lastSeen while preserving the existing
// FirstSeen (so "first seen" survives every re-observation). A nil rec.Tools
// leaves the stored snapshot untouched, so a transient per-node tools-fetch
// failure cannot erase a previously cached snapshot.
func (s *Server) upsertRelayNode(ctx context.Context, rec relayNodeRecord) error {
	nodes, err := s.loadRelayRegistry(ctx)
	if err != nil {
		return err
	}
	if existing, ok := nodes[rec.InstanceID]; ok {
		if rec.FirstSeen.IsZero() {
			rec.FirstSeen = existing.FirstSeen
		}
		if rec.Tools == nil {
			rec.Tools = existing.Tools
		}
	}
	if rec.LastSeen.IsZero() {
		rec.LastSeen = time.Now().UTC()
	}
	if rec.FirstSeen.IsZero() {
		rec.FirstSeen = rec.LastSeen
	}
	nodes[rec.InstanceID] = rec
	return s.saveRelayRegistry(ctx, nodes)
}

// removeRelayNode deletes instanceID from the registry. Removing an id that is
// not stored is tolerated as a no-op (nothing is written back), matching the
// "unknown id is fine" contract of the remove route.
func (s *Server) removeRelayNode(ctx context.Context, instanceID string) error {
	nodes, err := s.loadRelayRegistry(ctx)
	if err != nil {
		return err
	}
	if _, ok := nodes[instanceID]; !ok {
		return nil
	}
	delete(nodes, instanceID)
	return s.saveRelayRegistry(ctx, nodes)
}

// uiMCPNodesRemove handles the per-node Remove form (PRG → POST
// /settings/mcp-nodes/remove). The form field instance_id is the registry key.
// Removing a still-connected node is allowed and expected to reappear on the
// next page load, since the live session is re-observed.
func (s *Server) uiMCPNodesRemove(c echo.Context) error {
	id := strings.TrimSpace(c.FormValue("instance_id"))
	if id == "" {
		return redirectWithError(c, "/settings/mcp-nodes", fmt.Errorf("instance id is required"))
	}
	if err := s.removeRelayNode(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/settings/mcp-nodes", err)
	}
	return c.Redirect(http.StatusSeeOther, "/settings/mcp-nodes?updated=1")
}
