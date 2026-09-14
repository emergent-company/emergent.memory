package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// newTestEcho wires the MCP nodes routes onto a fresh echo instance.
func newTestEcho(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/settings/mcp-nodes", s.uiMCPNodes)
	e.POST("/settings/mcp-nodes/remove", s.uiMCPNodesRemove)
	return e
}

// getRec performs a GET and returns the recorder.
func getRec(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// --- relay-node registry: storage + reconciliation tests (tasks 1.x, 2.x, 3.1) ---

// TestLoadRelayRegistryTolerant covers missing/404, corrupt and partial values:
// all must degrade to the parseable records instead of erroring.
func TestLoadRelayRegistryTolerant(t *testing.T) {
	// missing → empty, no error
	f := &fakeMemory{}
	s := &Server{memory: f}
	nodes, err := s.loadRelayRegistry(t.Context())
	if err != nil || len(nodes) != 0 {
		t.Fatalf("missing registry: nodes=%v err=%v, want empty", nodes, err)
	}

	// corrupt top-level shape → empty, no error
	f.settings = map[string]map[string]map[string]any{
		relayRegistryCategory: {relayRegistryKey: {"nodes": "not-a-map"}},
	}
	nodes, err = s.loadRelayRegistry(t.Context())
	if err != nil || len(nodes) != 0 {
		t.Fatalf("corrupt registry: nodes=%v err=%v, want empty", nodes, err)
	}

	// partial entry: float toolCount, bad timestamp ignored, junk tool skipped
	f.settings = map[string]map[string]map[string]any{
		relayRegistryCategory: {relayRegistryKey: {"nodes": map[string]any{
			"mac-ada": map[string]any{
				"version":   "1.2.0",
				"toolCount": float64(3),
				"firstSeen": "not-a-timestamp",
				"lastSeen":  "2026-01-02T03:04:05Z",
				"tools": []any{
					map[string]any{"name": "notes_search", "description": "Search"},
					"junk",
					map[string]any{"name": ""},
				},
			},
			"garbage": "not-a-map",
		}}},
	}
	nodes, err = s.loadRelayRegistry(t.Context())
	if err != nil {
		t.Fatalf("partial registry: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("partial registry size = %d, want 1", len(nodes))
	}
	rec := nodes["mac-ada"]
	if rec.Version != "1.2.0" || rec.ToolCount != 3 {
		t.Errorf("partial record wrong: %+v", rec)
	}
	if !rec.FirstSeen.IsZero() {
		t.Errorf("bad firstSeen should be zero, got %v", rec.FirstSeen)
	}
	if rec.LastSeen.Format(time.RFC3339) != "2026-01-02T03:04:05Z" {
		t.Errorf("lastSeen = %v", rec.LastSeen)
	}
	if len(rec.Tools) != 1 || rec.Tools[0].Name != "notes_search" || rec.Tools[0].Description != "Search" {
		t.Errorf("tools = %+v", rec.Tools)
	}
}

// TestUpsertRelayNodeCreatesAndRefreshes asserts a first upsert seeds
// firstSeen=lastSeen, and a later observation refreshes version/toolCount/tools
// and lastSeen while preserving firstSeen.
func TestUpsertRelayNodeCreatesAndRefreshes(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{memory: f}
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Hour)

	if err := s.upsertRelayNode(t.Context(), relayNodeRecord{
		InstanceID: "mac-ada", Version: "1.0.0", ToolCount: 1,
		Tools: []RelayTool{{Name: "a"}}, LastSeen: t1,
	}); err != nil {
		t.Fatal(err)
	}
	nodes, _ := s.loadRelayRegistry(t.Context())
	rec := nodes["mac-ada"]
	if !rec.FirstSeen.Equal(t1) || !rec.LastSeen.Equal(t1) {
		t.Fatalf("first upsert timestamps: %+v", rec)
	}

	if err := s.upsertRelayNode(t.Context(), relayNodeRecord{
		InstanceID: "mac-ada", Version: "1.1.0", ToolCount: 2,
		Tools: []RelayTool{{Name: "a"}, {Name: "b"}}, LastSeen: t2,
	}); err != nil {
		t.Fatal(err)
	}
	nodes, _ = s.loadRelayRegistry(t.Context())
	rec = nodes["mac-ada"]
	if !rec.FirstSeen.Equal(t1) {
		t.Errorf("firstSeen must survive refresh: got %v want %v", rec.FirstSeen, t1)
	}
	if !rec.LastSeen.Equal(t2) || rec.Version != "1.1.0" || rec.ToolCount != 2 || len(rec.Tools) != 2 {
		t.Errorf("refresh did not update record: %+v", rec)
	}
}

// TestUpsertRelayNodeKeepsSnapshotOnNilTools asserts a nil tool snapshot (a
// failed per-node fetch) does not erase the cached tools.
func TestUpsertRelayNodeKeepsSnapshotOnNilTools(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{memory: f}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.upsertRelayNode(t.Context(), relayNodeRecord{
		InstanceID: "mac-ada", Version: "1.0.0", ToolCount: 1,
		Tools: []RelayTool{{Name: "a"}}, LastSeen: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.upsertRelayNode(t.Context(), relayNodeRecord{
		InstanceID: "mac-ada", Version: "1.1.0", ToolCount: 2,
		Tools: nil, LastSeen: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	nodes, _ := s.loadRelayRegistry(t.Context())
	rec := nodes["mac-ada"]
	if rec.Version != "1.1.0" || rec.ToolCount != 2 {
		t.Errorf("metadata should refresh on nil tools: %+v", rec)
	}
	if len(rec.Tools) != 1 || rec.Tools[0].Name != "a" {
		t.Errorf("cached tools must survive a nil snapshot: %+v", rec.Tools)
	}
}

// TestRemoveRelayNode asserts removal deletes the entry, tolerates an unknown
// id, and tolerates a missing registry.
func TestRemoveRelayNode(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{memory: f}
	now := time.Now().UTC()
	for _, id := range []string{"keep", "drop"} {
		if err := s.upsertRelayNode(t.Context(), relayNodeRecord{InstanceID: id, LastSeen: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.removeRelayNode(t.Context(), "drop"); err != nil {
		t.Fatal(err)
	}
	nodes, _ := s.loadRelayRegistry(t.Context())
	if _, ok := nodes["drop"]; ok {
		t.Error("drop should be removed")
	}
	if _, ok := nodes["keep"]; !ok {
		t.Error("unrelated entry should survive")
	}

	if err := s.removeRelayNode(t.Context(), "unknown"); err != nil {
		t.Errorf("unknown id should be tolerated: %v", err)
	}

	// no registry at all → no-op success
	f2 := &fakeMemory{}
	s2 := &Server{memory: f2}
	if err := s2.removeRelayNode(t.Context(), "anything"); err != nil {
		t.Errorf("missing registry should be tolerated: %v", err)
	}
}

// TestUIMCPNodesReconciliationOffline asserts a previously-recorded node absent
// from the live sessions renders offline, most recently seen first, and that
// its cached snapshot is served when selected.
func TestUIMCPNodesReconciliationOffline(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	now := time.Now().UTC().Truncate(time.Second)
	// newer offline node + older offline node; nothing live.
	if err := s.upsertRelayNode(t.Context(), relayNodeRecord{
		InstanceID: "newer-off", Version: "2.0.0", ToolCount: 1,
		Tools: []RelayTool{{Name: "snap_tool"}}, LastSeen: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.upsertRelayNode(t.Context(), relayNodeRecord{
		InstanceID: "older-off", Version: "1.0.0", ToolCount: 1, LastSeen: now.Add(-4 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	e := newTestEcho(s)
	rec := getRec(t, e, "/settings/mcp-nodes")
	body := rec.Body.String()
	for _, want := range []string{"newer-off", "older-off", "Disconnected", "last seen"} {
		if !strings.Contains(body, want) {
			t.Errorf("offline page missing %q", want)
		}
	}
	if strings.Index(body, "?node=newer-off") > strings.Index(body, "?node=older-off") {
		t.Error("offline nodes must render most recently seen first")
	}

	rec = getRec(t, e, "/settings/mcp-nodes?node=newer-off")
	if !strings.Contains(rec.Body.String(), "newer-off_snap_tool") {
		t.Error("offline node should serve its cached tool snapshot")
	}
}
