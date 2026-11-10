package mcpregistry_test

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// typeContractRig is a throwaway database plus a registry repository/service
// pointed at it. Every test below works against real Postgres because the bugs
// being pinned are in SQL predicates and service guards, not in pure helpers.
type typeContractRig struct {
	db        *bun.DB
	repo      *mcpregistry.Repository
	svc       *mcpregistry.Service
	projectID string
}

func newTypeContractRig(t *testing.T, suffix string) *typeContractRig {
	t.Helper()
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, suffix)
	t.Cleanup(tdb.Close)
	db := tdb.DB

	orgID, projectID := uuid.New(), uuid.New()
	if _, err := db.NewRaw(`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "type-contract-"+suffix).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id) VALUES (?, ?, ?)`, projectID, "type-contract-"+suffix, orgID).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	repo := mcpregistry.NewRepository(db)
	// The inspect/discover/delete/update guards under test do not reach
	// mcp.Service, so a nil in-process service is sufficient here.
	svc, err := mcpregistry.NewService(repo, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	return &typeContractRig{db: db, repo: repo, svc: svc, projectID: projectID.String()}
}

// seedServer inserts a server row of typ (plus one enabled tool) and returns it.
// The tool row lets the list/tool-pool queries be asserted as well.
func (r *typeContractRig) seedServer(t *testing.T, typ mcpregistry.MCPServerType, name string, enabled bool) *mcpregistry.MCPServer {
	t.Helper()
	ctx := context.Background()
	server := &mcpregistry.MCPServer{
		ProjectID: r.projectID,
		Name:      name,
		Enabled:   enabled,
		Type:      typ,
	}
	switch typ {
	case mcpregistry.ServerTypeStdio:
		cmd := "true"
		server.Command = &cmd
	case mcpregistry.ServerTypeSSE, mcpregistry.ServerTypeHTTP:
		url := "https://mcp.example.com/mcp"
		server.URL = &url
	}
	if err := r.repo.CreateServer(ctx, server); err != nil {
		t.Fatalf("create %s server %q: %v", typ, name, err)
	}
	if err := r.repo.BulkUpsertTools(ctx, []*mcpregistry.MCPServerTool{{
		ServerID: server.ID,
		ToolName: "tool-" + name,
		Enabled:  true,
	}}); err != nil {
		t.Fatalf("create tool for %s server %q: %v", typ, name, err)
	}
	return server
}

func sortedServerNames(servers []*mcpregistry.MCPServer) []string {
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}

func assertNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %v, want %v", got, want)
		}
	}
}

// TestRegistryListReadsExcludeInProcessRows pins repository.go:25,39,56: the
// list/UI/pool reads must not expose builtin or source rows as user-editable
// external servers.
func TestRegistryListReadsExcludeInProcessRows(t *testing.T) {
	r := newTypeContractRig(t, "type_contract_lists")
	ctx := context.Background()

	r.seedServer(t, mcpregistry.ServerTypeBuiltin, "builtin", true)
	r.seedServer(t, mcpregistry.ServerTypeSource, "source-srv", true)
	r.seedServer(t, mcpregistry.ServerTypeStdio, "stdio-srv", true)
	r.seedServer(t, mcpregistry.ServerTypeSSE, "sse-srv", false)
	r.seedServer(t, mcpregistry.ServerTypeHTTP, "http-srv", true)

	all, err := r.repo.FindAllServers(ctx, r.projectID)
	if err != nil {
		t.Fatalf("FindAllServers: %v", err)
	}
	assertNames(t, sortedServerNames(all), []string{"http-srv", "sse-srv", "stdio-srv"})

	withTools, err := r.repo.FindAllServersWithTools(ctx, r.projectID)
	if err != nil {
		t.Fatalf("FindAllServersWithTools: %v", err)
	}
	assertNames(t, sortedServerNames(withTools), []string{"http-srv", "sse-srv", "stdio-srv"})

	enabled, err := r.repo.FindEnabledServers(ctx, r.projectID)
	if err != nil {
		t.Fatalf("FindEnabledServers: %v", err)
	}
	assertNames(t, sortedServerNames(enabled), []string{"http-srv", "stdio-srv"})

	tools, err := r.repo.FindAllEnabledTools(ctx, r.projectID)
	if err != nil {
		t.Fatalf("FindAllEnabledTools: %v", err)
	}
	gotTools := make([]string, 0, len(tools))
	for _, tool := range tools {
		gotTools = append(gotTools, tool.ToolName)
	}
	sort.Strings(gotTools)
	assertNames(t, gotTools, []string{"tool-http-srv", "tool-stdio-srv"})
}

// TestCreateServerRejectsInProcessAndUnknownTypes pins service.go:255: only a
// registry-managed type may be created through the API. A source row accepted
// here would be an orphan with no source_id that looks provisioned.
func TestCreateServerRejectsInProcessAndUnknownTypes(t *testing.T) {
	r := newTypeContractRig(t, "type_contract_create")
	ctx := context.Background()

	for _, typ := range []mcpregistry.MCPServerType{
		mcpregistry.ServerTypeSource,
		mcpregistry.ServerTypeBuiltin,
		mcpregistry.MCPServerType("future-type"),
	} {
		_, err := r.svc.CreateServer(ctx, r.projectID, &mcpregistry.CreateMCPServerDTO{
			Name: "rejected-" + string(typ),
			Type: typ,
		})
		if err == nil {
			t.Fatalf("CreateServer(%q) succeeded, want rejection", typ)
		}
	}

	servers, err := r.repo.FindAllServers(ctx, r.projectID)
	if err != nil {
		t.Fatalf("FindAllServers: %v", err)
	}
	if len(servers) != 0 {
		t.Fatalf("rejected creates left %d server rows: %v", len(servers), sortedServerNames(servers))
	}

	// The same contract must not over-block: a registry-managed stdio server is
	// still creatable (command only, so no outbound URL probe is involved).
	cmd := "true"
	created, err := r.svc.CreateServer(ctx, r.projectID, &mcpregistry.CreateMCPServerDTO{
		Name:    "allowed-stdio",
		Type:    mcpregistry.ServerTypeStdio,
		Command: &cmd,
	})
	if err != nil {
		t.Fatalf("CreateServer(stdio): %v", err)
	}
	if created.Type != mcpregistry.ServerTypeStdio {
		t.Fatalf("created type = %q, want stdio", created.Type)
	}
}

// TestSourceRowsAreReadOnlyThroughRegistryAPI pins service.go:554,952,1160,1213
// and 387: inspect/discover/sync/delete/config-update refuse an in-process
// source row instead of treating it as a user-managed external server. Builtin
// inspect keeps its synthetic-OK behaviour, and enable/disable stays allowed.
func TestSourceRowsAreReadOnlyThroughRegistryAPI(t *testing.T) {
	r := newTypeContractRig(t, "type_contract_source")
	ctx := context.Background()

	source := r.seedServer(t, mcpregistry.ServerTypeSource, "source-srv", true)
	builtin := r.seedServer(t, mcpregistry.ServerTypeBuiltin, "builtin", true)

	// Inspect: source is refused as not inspectable; builtin still answers a
	// synthetic OK without dialing anything.
	if _, err := r.svc.InspectServer(ctx, source.ID, r.projectID); !errors.Is(err, mcpregistry.ErrServerNotInspectable) {
		t.Fatalf("InspectServer(source) error = %v, want ErrServerNotInspectable", err)
	}
	inspected, err := r.svc.InspectServer(ctx, builtin.ID, r.projectID)
	if err != nil {
		t.Fatalf("InspectServer(builtin): %v", err)
	}
	if inspected == nil || inspected.Status != "ok" || inspected.ServerType != mcpregistry.ServerTypeBuiltin {
		t.Fatalf("InspectServer(builtin) = %+v, want synthetic ok result", inspected)
	}

	// Discover and manual sync both refuse: source tools are provisioned by
	// domain/sources.
	if _, err := r.svc.DiscoverAndSyncTools(ctx, source.ID, r.projectID); err == nil {
		t.Fatal("DiscoverAndSyncTools(source) succeeded, want refusal")
	}
	if err := r.svc.SyncServerTools(ctx, r.projectID, source.ID, []mcpregistry.DiscoveredTool{{Name: "injected"}}); err == nil {
		t.Fatal("SyncServerTools(source) succeeded, want refusal")
	}

	// Delete refuses, so provisioning cannot flap the row away.
	if err := r.svc.DeleteServer(ctx, source.ID, r.projectID); err == nil {
		t.Fatal("DeleteServer(source) succeeded, want refusal")
	}
	if still, err := r.repo.FindServerByID(ctx, source.ID, &r.projectID); err != nil || still == nil {
		t.Fatalf("source row disappeared after refused delete: server=%v err=%v", still, err)
	}

	// Config updates refuse; an enable/disable toggle is still allowed.
	renamed := "renamed"
	if _, err := r.svc.UpdateServer(ctx, source.ID, r.projectID, &mcpregistry.UpdateMCPServerDTO{Name: &renamed}); err == nil {
		t.Fatal("UpdateServer(source, name) succeeded, want refusal")
	}
	disabled := false
	updated, err := r.svc.UpdateServer(ctx, source.ID, r.projectID, &mcpregistry.UpdateMCPServerDTO{Enabled: &disabled})
	if err != nil {
		t.Fatalf("UpdateServer(source, enabled=false): %v", err)
	}
	if updated == nil || updated.Enabled {
		t.Fatalf("source enable/disable did not persist: %+v", updated)
	}
}

// fakeSourceToolHandler stands in for domain/sources so the in-process call
// routing can be observed without wiring the whole sources domain.
const sourceCallToolName = "gmail-alice-message-search"

type fakeSourceToolHandler struct {
	executed string
}

func (f *fakeSourceToolHandler) GetSourceToolDefinitionsForProject(context.Context, string) []mcp.ToolDefinition {
	return nil
}

func (f *fakeSourceToolHandler) HandlesSourceTool(_ context.Context, _ string, toolName string) bool {
	return toolName == sourceCallToolName
}

func (f *fakeSourceToolHandler) ExecuteSourceTool(_ context.Context, _ string, toolName string, _ map[string]any) (*mcp.ToolResult, error) {
	f.executed = toolName
	return &mcp.ToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: "source-ok"}}}, nil
}

// TestSourceCallToolDelegatesInProcess is the regression test for
// service.go:1160: a source row's tool call must reach domain/mcp (the source
// handler), never the outbound external proxy.
func (s *MCPRegistryToolInvokeSuite) TestSourceCallToolDelegatesInProcess() {
	serverID := s.seedServer("source", true)
	s.seedTool(serverID, "gmail-alice-message-search", true)

	mcpSvc := mcp.NewService(mcp.ServiceParams{DB: s.DB(), Cfg: s.TestDB.Config, Log: slog.Default()})
	_ = mcpSvc.GetToolDefinitions()
	fake := &fakeSourceToolHandler{}
	mcpSvc.RegisterSourceToolHandler(fake)

	svc, err := mcpregistry.NewService(mcpregistry.NewRepository(s.DB()), mcpSvc, nil, slog.Default())
	s.Require().NoError(err)

	result, err := svc.CallToolOnServer(s.Ctx, s.ProjectID, serverID, "gmail-alice-message-search", nil, nil, "")
	s.Require().NoError(err, "source call must delegate in-process: %v", err)
	s.Require().NotNil(result)
	s.Require().Equal("gmail-alice-message-search", fake.executed, "source handler must execute the call")
}
