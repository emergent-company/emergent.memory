package mcpregistry_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestEnsureSourceServerKeepsWriteToolsGatedByOptIn covers must-fix 4: a source
// server's write tools must be disabled unless the source opted in, and a
// re-provision (every connect/update path calls one) must not re-enable a tool
// an administrator explicitly disabled.
func TestEnsureSourceServerKeepsWriteToolsGatedByOptIn(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "src_mcp_write_gate")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.NewRaw(query, args...).Exec(ctx); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	orgID, projectID, sourceID := uuid.New(), uuid.New(), uuid.New()
	mustExec(`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "write-gate-org")
	mustExec(`INSERT INTO kb.projects (id, name, organization_id) VALUES (?, ?, ?)`, projectID, "write-gate-project", orgID)
	mustExec(`INSERT INTO kb.sources (id, project_id, provider, display_name) VALUES (?, ?, 'gmail', 'alice')`, sourceID, projectID)

	svc, err := mcpregistry.NewService(mcpregistry.NewRepository(db), nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	readDef := mcp.ToolDefinition{
		Name:          "gmail-alice-message-search",
		RequiredScope: "email:read",
		InputSchema:   mcp.InputSchema{Type: "object"},
	}
	writeDef := mcp.ToolDefinition{
		Name:          "gmail-alice-message-send",
		RequiredScope: "email:write",
		InputSchema:   mcp.InputSchema{Type: "object"},
	}
	defs := func(write bool) []mcp.ToolDefinition {
		if write {
			return []mcp.ToolDefinition{readDef, writeDef}
		}
		return []mcp.ToolDefinition{readDef}
	}

	// toolRow returns (enabled, enabledOverride, present). enabled_override is
	// the gate the agent tool pool applies, so it is what these assertions watch.
	toolRow := func(name string) (bool, *bool, bool) {
		t.Helper()
		var row struct {
			Enabled         bool  `bun:"enabled"`
			EnabledOverride *bool `bun:"enabled_override"`
		}
		err := db.NewRaw(`
			SELECT mst.enabled, mst.enabled_override
			FROM kb.mcp_server_tools AS mst
			JOIN kb.mcp_servers AS ms ON ms.id = mst.server_id
			WHERE ms.source_id = ? AND mst.tool_name = ?`, sourceID, name).Scan(ctx, &row)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil, false
			}
			t.Fatalf("read tool row %s: %v", name, err)
		}
		return row.Enabled, row.EnabledOverride, true
	}
	// poolNames is the agent tool pool's own query (FindAllEnabledBuiltinTools),
	// so it proves the row flag really gates execution rather than just the row.
	repo := mcpregistry.NewRepository(db)
	poolNames := func() map[string]bool {
		t.Helper()
		tools, err := repo.FindAllEnabledBuiltinTools(ctx, projectID.String())
		if err != nil {
			t.Fatalf("FindAllEnabledBuiltinTools: %v", err)
		}
		out := make(map[string]bool, len(tools))
		for _, tool := range tools {
			out[tool.ToolName] = true
		}
		return out
	}

	// 1. Not opted in: read tools are provisioned and pooled, write tools are not
	// even defined.
	if err := svc.EnsureSourceServer(ctx, projectID.String(), sourceID.String(), defs(false), false); err != nil {
		t.Fatalf("EnsureSourceServer(no opt-in): %v", err)
	}
	if enabled, override, ok := toolRow(readDef.Name); !ok || !enabled || override != nil {
		t.Fatalf("read tool = (enabled %v, override %v, present %v), want enabled with no override", enabled, override, ok)
	}
	if _, _, ok := toolRow(writeDef.Name); ok {
		t.Fatal("message-send exists without the write-tool opt-in")
	}
	pool := poolNames()
	if !pool[readDef.Name] {
		t.Fatal("read tool is missing from the agent tool pool")
	}
	if pool[writeDef.Name] {
		t.Fatal("write tool reached the agent tool pool without the opt-in")
	}

	// 2. Opted in: the write tool is registered and pooled.
	if err := svc.EnsureSourceServer(ctx, projectID.String(), sourceID.String(), defs(true), true); err != nil {
		t.Fatalf("EnsureSourceServer(opt-in): %v", err)
	}
	enabled, override, ok := toolRow(writeDef.Name)
	if !ok || !enabled || override == nil || !*override {
		t.Fatalf("write tool = (enabled %v, override %v, present %v), want enabled with override=true", enabled, override, ok)
	}
	if !poolNames()[writeDef.Name] {
		t.Fatal("opted-in write tool is missing from the agent tool pool")
	}

	// 3. An administrator disables the tool. A re-provision must not undo it.
	mustExec(`UPDATE kb.mcp_server_tools SET enabled = false, enabled_override = false WHERE tool_name = ?`, writeDef.Name)
	if err := svc.EnsureSourceServer(ctx, projectID.String(), sourceID.String(), defs(true), true); err != nil {
		t.Fatalf("EnsureSourceServer(re-provision): %v", err)
	}
	enabled, override, ok = toolRow(writeDef.Name)
	if !ok {
		t.Fatal("re-provision deleted the write tool")
	}
	if enabled || override == nil || *override {
		t.Fatalf("re-provision re-enabled a disabled write tool: enabled=%v override=%v", enabled, override)
	}
	if poolNames()[writeDef.Name] {
		t.Fatal("a disabled write tool is back in the agent tool pool")
	}

	// 4. Opting back out prunes the write tool entirely.
	if err := svc.EnsureSourceServer(ctx, projectID.String(), sourceID.String(), defs(false), false); err != nil {
		t.Fatalf("EnsureSourceServer(opt-out): %v", err)
	}
	if _, _, ok := toolRow(writeDef.Name); ok {
		t.Fatal("opting out left the write tool row behind")
	}
	if !poolNames()[readDef.Name] {
		t.Fatal("opting out removed the read tools too")
	}
}
