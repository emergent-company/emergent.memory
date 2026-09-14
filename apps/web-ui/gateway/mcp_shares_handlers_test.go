package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- test backend ---

// mcpShareTestBackend is the share-management test backend: an embedded
// fakeMemory (so every unrelated interface method still exists) with real
// create/update/revoke/rotate/list semantics plus error injection.
type mcpShareTestBackend struct {
	*fakeMemory
	shares    []MCPShareInstance
	catalog   []MCPShareTool
	listErr   error
	toolsErr  error
	createErr error
	getErr    error
	updateErr error
	revokeErr error
	rotateErr error

	lastCreate   *MCPShareInput
	lastUpdateID string
	lastUpdate   *MCPShareInput
	lastRevokeID string
	lastRotateID string
}

func newMCPShareTestBackend(shares []MCPShareInstance, catalog []MCPShareTool) *mcpShareTestBackend {
	return &mcpShareTestBackend{
		fakeMemory: &fakeMemory{agents: []AgentDefinitionSummary{
			{ID: "a1", Name: "diane", FlowType: "agentic"},
			{ID: "a2", Name: "research-bot"},
		}},
		shares:  shares,
		catalog: catalog,
	}
}

func (b *mcpShareTestBackend) find(id string) *MCPShareInstance {
	for i := range b.shares {
		if b.shares[i].ID == id {
			return &b.shares[i]
		}
	}
	return nil
}

func (b *mcpShareTestBackend) nameExists(name, exceptID string) bool {
	for _, s := range b.shares {
		if s.ID != exceptID && strings.EqualFold(s.Name, name) {
			return true
		}
	}
	return false
}

func (b *mcpShareTestBackend) ListMCPShareInstances(ctx context.Context) ([]MCPShareInstance, error) {
	if b.listErr != nil {
		return nil, b.listErr
	}
	return append([]MCPShareInstance(nil), b.shares...), nil
}

func (b *mcpShareTestBackend) CreateMCPShareInstance(ctx context.Context, in *MCPShareInput) (*MCPShareCreated, error) {
	if b.createErr != nil {
		return nil, b.createErr
	}
	b.lastCreate = in
	if b.nameExists(in.Name, "") {
		return nil, fmt.Errorf("memory 409 conflict: share with name %q already exists", in.Name)
	}
	inst := MCPShareInstance{
		ID: "share-1", Name: in.Name, Description: in.Description,
		Tools: in.Tools, Agents: in.Agents, Status: "active",
		CreatedAt: "2026-02-01T00:00:00Z", ToolCount: len(in.Tools), AgentCount: len(in.Agents),
	}
	b.shares = append(b.shares, inst)
	return &MCPShareCreated{
		MCPShareInstance: inst,
		MCPShareSecret:   MCPShareSecret{Token: "emt_new_secret", MCPURL: "https://mem.example/api/mcp"},
	}, nil
}

func (b *mcpShareTestBackend) GetMCPShareInstance(ctx context.Context, id string) (*MCPShareInstance, error) {
	if b.getErr != nil {
		return nil, b.getErr
	}
	if inst := b.find(id); inst != nil {
		cp := *inst
		return &cp, nil
	}
	return nil, fmt.Errorf("memory 404 not found")
}

func (b *mcpShareTestBackend) UpdateMCPShareInstance(ctx context.Context, id string, in *MCPShareInput) (*MCPShareInstance, error) {
	if b.updateErr != nil {
		return nil, b.updateErr
	}
	b.lastUpdateID = id
	b.lastUpdate = in
	if b.nameExists(in.Name, id) {
		return nil, fmt.Errorf("memory 409 conflict: share with name %q already exists", in.Name)
	}
	inst := b.find(id)
	if inst == nil {
		return nil, fmt.Errorf("memory 404 not found")
	}
	inst.Name = in.Name
	inst.Description = in.Description
	inst.Tools = in.Tools
	inst.Agents = in.Agents
	inst.ToolCount = len(in.Tools)
	inst.AgentCount = len(in.Agents)
	cp := *inst
	return &cp, nil
}

func (b *mcpShareTestBackend) RevokeMCPShareInstance(ctx context.Context, id string) error {
	if b.revokeErr != nil {
		return b.revokeErr
	}
	b.lastRevokeID = id
	out := b.shares[:0]
	found := false
	for _, s := range b.shares {
		if s.ID == id {
			found = true
			continue
		}
		out = append(out, s)
	}
	b.shares = out
	if !found {
		return fmt.Errorf("memory 404 not found")
	}
	return nil
}

func (b *mcpShareTestBackend) RotateMCPShareInstance(ctx context.Context, id string) (*MCPShareCreated, error) {
	if b.rotateErr != nil {
		return nil, b.rotateErr
	}
	b.lastRotateID = id
	inst := b.find(id)
	if inst == nil {
		return nil, fmt.Errorf("memory 404 not found")
	}
	return &MCPShareCreated{
		MCPShareInstance: *inst,
		MCPShareSecret:   MCPShareSecret{Token: "emt_rot_secret", MCPURL: "https://mem.example/api/mcp"},
	}, nil
}

func (b *mcpShareTestBackend) ListMCPShareTools(ctx context.Context) ([]MCPShareTool, error) {
	if b.toolsErr != nil {
		return nil, b.toolsErr
	}
	return b.catalog, nil
}

// --- fixtures + wiring ---

func mcpShareFixture() []MCPShareInstance {
	return []MCPShareInstance{
		{
			ID: "sh-1", Name: "research", Description: "read-only research access",
			Tools: []string{"search_memory", "get_object"}, Agents: []string{"a1"},
			Status: "active", CreatedAt: "2026-01-02T00:00:00Z", LastUsedAt: strPtr("2026-01-05T00:00:00Z"),
			ToolCount: 2, AgentCount: 1,
		},
		{
			ID: "sh-legacy", Name: "old share", IsLegacy: true, Status: "active",
			CreatedAt: "2025-12-01T00:00:00Z", ToolCount: 0, AgentCount: 0,
		},
	}
}

func mcpShareCatalogFixture() []MCPShareTool {
	return []MCPShareTool{
		{Name: "search_memory", Description: "Search memory", RequiredScope: "memory:read", Category: "Memory"},
		{Name: "get_object", Description: "Fetch an object", Category: "Memory"},
		{Name: "create_agent", Description: "Create an agent", Category: "Agents"},
	}
}

func mcpShareTestServer(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/settings/mcp-servers/shares", s.uiMCPShares)
	e.GET("/settings/mcp-servers/shares/new", s.uiMCPShareNewPage)
	e.POST("/settings/mcp-servers/shares/new", s.uiMCPShareCreate)
	e.GET("/settings/mcp-servers/shares/:id/edit", s.uiMCPShareEditPage)
	e.POST("/settings/mcp-servers/shares/:id/update", s.uiMCPShareUpdate)
	e.POST("/settings/mcp-servers/shares/:id/revoke", s.uiMCPShareRevoke)
	e.POST("/settings/mcp-servers/shares/:id/rotate", s.uiMCPShareRotate)
	e.GET("/api/mcp-shares", s.listMCPShares)
	e.POST("/api/mcp-shares", s.createMCPShare)
	e.GET("/api/mcp-shares/tools", s.listMCPShareTools)
	e.GET("/api/mcp-shares/:id", s.getMCPShare)
	e.PATCH("/api/mcp-shares/:id", s.updateMCPShare)
	e.DELETE("/api/mcp-shares/:id", s.deleteMCPShare)
	e.POST("/api/mcp-shares/:id/rotate", s.rotateMCPShare)
	return e
}

func mcpShareGet(e *echo.Echo, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func mcpSharePost(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

func mcpShareJSONReq(e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	return rec
}

// --- list page ---

func TestMCPSharesListRendersRowsAndLegacyReadOnly(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET shares = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"MCP Sharing", "research", "read-only research access",
		"2 tools", "1 agent",
		`href="/settings/mcp-servers/shares/sh-1/edit"`,
		`aria-label="Rotate key for research"`, `aria-label="Revoke research"`,
		"legacy", "old share",
		`href="/settings/mcp-servers/shares/new"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("shares list missing %q", want)
		}
	}
	// legacy row is read-only: no edit/rotate/revoke for it
	for _, absent := range []string{
		`href="/settings/mcp-servers/shares/sh-legacy/edit"`,
		`aria-label="Rotate key for old share"`,
		`aria-label="Revoke old share"`,
	} {
		if strings.Contains(body, absent) {
			t.Errorf("legacy row must not offer %q", absent)
		}
	}
	if strings.Contains(body, "No MCP shares yet") {
		t.Error("list must not render the empty state when shares exist")
	}
	// a normal list load never carries the reveal modal or a key
	if strings.Contains(body, "data-mcp-share-reveal>") || strings.Contains(body, `id="mcp-share-key"`) {
		t.Error("list page must not render the one-time reveal UI")
	}
}

func TestMCPSharesListEmptyAndError(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No MCP shares yet") {
		t.Errorf("empty list = %d, want empty state", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `href="/settings/mcp-servers/shares/new"`) {
		t.Error("empty state must offer a create CTA")
	}

	f.listErr = errTest
	rec = mcpShareGet(e, "/settings/mcp-servers/shares")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "MCP shares unavailable") {
		t.Errorf("load-error list = %d, want the error state", rec.Code)
	}
}

// --- create ---

func TestMCPShareNewPageRendersPickers(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET new = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"New MCP share", `action="/settings/mcp-servers/shares/new"`,
		`name="name"`, `name="description"`,
		`data-mcp-tool-search`, `data-mcp-tool-select-all`, `data-mcp-tool-clear`,
		`data-mcp-tool-group`, `name="tools" value="search_memory"`, `name="tools" value="create_agent"`,
		"Memory", "Agents",
		`name="agents" value="a1"`, "diane", "research-bot",
		"Create share", `href="/settings/mcp-servers/shares"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("new share page missing %q", want)
		}
	}
	if strings.Contains(body, "Save changes") {
		t.Error("new page must not render the edit submit label")
	}
}

func TestMCPShareNewPagePreselectsAgent(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/new?agent=a1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET new?agent = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `value="a1" class="checkbox checkbox-sm mt-0.5" checked`) {
		t.Error("create form must preselect the agent from the query string")
	}
}

func TestMCPShareCreateRendersOneTimeReveal(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/new",
		"name=research2&description=hello&tools=search_memory&tools=get_object&agents=a1")
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d, want 200 reveal render", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Share created", "emt_new_secret", "https://mem.example/api/mcp",
		"will not be shown again", `data-mcp-share-reveal`, `data-copy-target="#mcp-share-key"`,
		"Claude Desktop", "Claude Code", "Cursor", "Cloud Code",
		"Done — I've saved the key",
		// the list re-render behind the modal shows the new share
		"research2",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reveal modal missing %q", want)
		}
	}
	// the snippet must embed both the endpoint and the key
	for _, s := range []string{"mcp-remote", "Bearer emt_new_secret"} {
		if !strings.Contains(body, s) {
			t.Errorf("snippet missing %q", s)
		}
	}
	if len(f.shares) != 1 || f.shares[0].Name != "research2" {
		t.Fatalf("store after create = %+v", f.shares)
	}
	if len(f.lastCreate.Tools) != 2 || len(f.lastCreate.Agents) != 1 {
		t.Errorf("create input = %+v", f.lastCreate)
	}
}

func TestMCPShareCreateValidationInline(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	// empty tools
	rec := mcpSharePost(e, "/settings/mcp-servers/shares/new", "name=no-tools")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Select at least one tool to expose.") {
		t.Errorf("empty-tools create = %d, want inline tools error", rec.Code)
	}
	// empty name
	rec = mcpSharePost(e, "/settings/mcp-servers/shares/new", "tools=search_memory")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Share name is required.") {
		t.Errorf("empty-name create = %d, want inline name error", rec.Code)
	}
	// duplicate name
	rec = mcpSharePost(e, "/settings/mcp-servers/shares/new", "name=research&tools=search_memory")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "already exists") {
		t.Errorf("duplicate-name create = %d, want inline conflict", rec.Code)
	}
	if len(f.shares) != 2 {
		t.Errorf("invalid submissions must not create shares, store = %+v", f.shares)
	}
}

func TestMCPShareCreateTestIdMarkers(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)
	rec := mcpSharePost(e, "/settings/mcp-servers/shares/new", "name=x&tools=search_memory")
	for _, want := range []string{`data-mcp-share-reveal`, `id="mcp-share-key"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("reveal missing test id %q", want)
		}
	}
}

// --- edit / update ---

func TestMCPShareEditPagePrefills(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/sh-1/edit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET edit = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Edit share", `action="/settings/mcp-servers/shares/sh-1/update"`,
		`value="research"`, "read-only research access", "Save changes",
		`name="tools" value="search_memory"`, `name="agents" value="a1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
}

func TestMCPShareEditLegacyRedirects(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/sh-legacy/edit")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "legacy") {
		t.Errorf("legacy edit = %d %q, want redirect with a legacy reason", rec.Code, rec.Header().Get("Location"))
	}
}

func TestMCPShareUpdatePersists(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/update",
		"name=research&description=updated&tools=get_object&agents=a1&agents=a2")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers/shares?updated=1" {
		t.Fatalf("update = %d %q, want 303 ?updated=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastUpdateID != "sh-1" || len(f.lastUpdate.Tools) != 1 || f.lastUpdate.Tools[0] != "get_object" {
		t.Errorf("update input = %s %+v", f.lastUpdateID, f.lastUpdate)
	}
	if inst := f.find("sh-1"); inst == nil || len(inst.Agents) != 2 || inst.Description != "updated" {
		t.Errorf("persisted share = %+v", inst)
	}
}

func TestMCPShareUpdateLegacyRejected(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-legacy/update", "name=x&tools=search_memory")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "legacy") {
		t.Errorf("legacy update = %d %q, want redirect with a legacy reason", rec.Code, rec.Header().Get("Location"))
	}
}

// --- revoke / rotate ---

func TestMCPShareRevoke(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/revoke", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers/shares?revoked=1" {
		t.Fatalf("revoke = %d %q, want 303 ?revoked=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastRevokeID != "sh-1" {
		t.Errorf("revoke id = %q, want sh-1", f.lastRevokeID)
	}
	if f.find("sh-1") != nil {
		t.Error("revoke must remove the share")
	}
}

func TestMCPShareRevokeLegacyRejected(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-legacy/revoke", "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "legacy") {
		t.Errorf("legacy revoke = %d %q, want redirect with a legacy reason", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastRevokeID != "" {
		t.Error("legacy revoke must not reach the backend")
	}
}

func TestMCPShareRevokeErrorFlashes(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	f.revokeErr = fmt.Errorf("memory 500 internal: boom")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/revoke", "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("revoke error = %d %q, want redirect carrying the error", rec.Code, rec.Header().Get("Location"))
	}
	if !strings.Contains(rec.Header().Get("Location"), "boom") {
		t.Errorf("revoke error location = %q, want the memory error surfaced", rec.Header().Get("Location"))
	}
}

func TestMCPShareRotateRevealsNewKey(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/rotate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate = %d, want 200 reveal render", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Share key rotated") || !strings.Contains(body, "emt_rot_secret") {
		t.Errorf("rotate reveal missing new key: %s", body)
	}
	if f.lastRotateID != "sh-1" {
		t.Errorf("rotate id = %q, want sh-1", f.lastRotateID)
	}
}

// --- JSON surface ---

func TestMCPShareJSONListNeverExposesToken(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodGet, "/api/mcp-shares", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/mcp-shares = %d, want 200", rec.Code)
	}
	var out []MCPShareInstance
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 2 {
		t.Fatalf("list body = %s (%v)", rec.Body.String(), err)
	}
	if strings.Contains(rec.Body.String(), `"token"`) {
		t.Error("list response must never contain a token field")
	}
}

func TestMCPShareJSONCreateReturnsTokenOnce(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares",
		`{"name":"api share","tools":["search_memory"],"agents":[]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/mcp-shares = %d, want 201", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"token":"emt_new_secret"`, `"mcpUrl":"https://mem.example/api/mcp"`, `"snippets"`, `"instance"`} {
		if !strings.Contains(body, want) {
			t.Errorf("create response missing %q: %s", want, body)
		}
	}
	// the snippet map embeds the key + endpoint
	if !strings.Contains(body, "Bearer emt_new_secret") {
		t.Error("create response snippets must include the key")
	}
}

func TestMCPShareJSONGetNeverExposesToken(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodGet, "/api/mcp-shares/sh-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/mcp-shares/sh-1 = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"token"`) {
		t.Error("get response must never contain a token field")
	}
}

func TestMCPShareJSONRotateReturnsToken(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares/sh-1/rotate", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"token":"emt_rot_secret"`) {
		t.Errorf("rotate response = %d %s, want the new token", rec.Code, rec.Body.String())
	}
}

func TestMCPShareJSONToolsCatalog(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodGet, "/api/mcp-shares/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET tools = %d, want 200", rec.Code)
	}
	var out []MCPShareTool
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 3 {
		t.Fatalf("tools body = %s", rec.Body.String())
	}
}

func TestMCPShareJSONBackendStatusPassThrough(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	// 409 duplicate name passes through
	f.createErr = fmt.Errorf("memory 409 conflict: share with name %q already exists", "research")
	rec := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares", `{"name":"research","tools":["search_memory"]}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already exists") {
		t.Errorf("409 create = %d %s, want 409 + message", rec.Code, rec.Body.String())
	}

	// 422 validation passes through
	f.updateErr = fmt.Errorf("memory 422 unprocessable: invalid tools")
	rec = mcpShareJSONReq(e, http.MethodPatch, "/api/mcp-shares/sh-1", `{"name":"research","tools":[]}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "invalid tools") {
		t.Errorf("422 update = %d %s, want 422 + message", rec.Code, rec.Body.String())
	}
}

func TestMCPShareJSONUnauthenticated(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := echo.New()
	e.Use(s.authDispatch)
	e.GET("/api/mcp-shares", s.listMCPShares)

	rec := mcpShareJSONReq(e, http.MethodGet, "/api/mcp-shares", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated share route = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "research") {
		t.Error("unauthenticated response must not expose share data")
	}
}

// TestMCPShareCreateNeverLogsToken drives the JSON create handler through a real
// MemoryClient against a stub backend and asserts the one-time token does not
// reach the gateway log.
func TestMCPShareCreateNeverLogsToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":true,"data":{"id":"s1","name":"api share","token":"emt_logged_secret","mcpUrl":"https://mem.example/mcp"}}`)
	}))
	defer ts.Close()

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	s := &Server{cfg: Config{}, memory: NewMemoryClient(ts.URL, "tok", "proj-1")}
	e := echo.New()
	e.POST("/api/mcp-shares", s.createMCPShare)
	rec := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares", `{"name":"api share","tools":["search_memory"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "emt_logged_secret") {
		t.Fatalf("create response should carry the token, got %s", rec.Body.String())
	}
	if strings.Contains(buf.String(), "emt_logged_secret") {
		t.Error("the raw token must never be written to the log")
	}
}

// --- entry points ---
func TestMCPServersPageLinksToSharing(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: mcpTestRegistryFixture()}))
	for _, want := range []string{`href="/settings/mcp-servers/shares"`, "Sharing"} {
		if !strings.Contains(html, want) {
			t.Errorf("MCP Servers page missing sharing entry %q", want)
		}
	}
}

func TestAgentDashboardShareViaMCP(t *testing.T) {
	data := agentDashboardData{Agent: &AgentDefinition{ID: "a1", Name: "diane"}}
	html := renderHTML(t, AgentDashboardPage(data))
	for _, want := range []string{
		"Share via MCP",
		`href="/settings/mcp-servers/shares/new?agent=a1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("agent dashboard missing %q", want)
		}
	}
}
