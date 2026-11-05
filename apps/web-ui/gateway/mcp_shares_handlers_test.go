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
	"net/url"
	"reflect"
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
	templates []MCPShareTemplate
	listErr   error
	toolsErr  error
	createErr error
	getErr    error
	updateErr error
	revokeErr error
	rotateErr error
	inviteErr error

	// inviteEmailErr models a partial failure: the API accepts the invite but
	// the email queue reports a problem.
	inviteEmailErr string

	lastCreate       *MCPShareInput
	lastUpdateID     string
	lastUpdate       *MCPShareInput
	lastRevokeID     string
	lastRotateID     string
	lastRotateEmails []string
	lastInviteID     string
	lastInviteEmails []string
}

func newMCPShareTestBackend(shares []MCPShareInstance, catalog []MCPShareTool) *mcpShareTestBackend {
	return &mcpShareTestBackend{
		fakeMemory: &fakeMemory{},
		shares:     shares,
		catalog:    catalog,
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

// shareURL is the MCP endpoint URL the backend reports on create/rotate. It
// defaults to an external-looking host; tests override the embedded
// fakeMemory.mcpURL to exercise the internal-host rewrite.
func (b *mcpShareTestBackend) shareURL() string {
	if b.mcpURL != "" {
		return b.mcpURL
	}
	return "https://mem.example/api/mcp"
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
		Tools: in.Tools, Status: "active",
		CreatedAt: "2026-02-01T00:00:00Z", ToolCount: len(in.Tools),
	}
	b.shares = append(b.shares, inst)
	created := &MCPShareCreated{
		MCPShareInstance: inst,
		MCPShareSecret:   MCPShareSecret{Token: "emt_new_secret", MCPURL: b.shareURL()},
	}
	if len(in.Emails) > 0 {
		created.EmailsSent = append([]string(nil), in.Emails...)
	}
	return created, nil
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
	inst.ToolCount = len(in.Tools)
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

func (b *mcpShareTestBackend) RotateMCPShareInstance(ctx context.Context, id string, emails []string) (*MCPShareCreated, error) {
	if b.rotateErr != nil {
		return nil, b.rotateErr
	}
	b.lastRotateID = id
	b.lastRotateEmails = emails
	inst := b.find(id)
	if inst == nil {
		return nil, fmt.Errorf("memory 404 not found")
	}
	return &MCPShareCreated{
		MCPShareInstance: *inst,
		MCPShareSecret:   MCPShareSecret{Token: "emt_rot_secret", MCPURL: b.shareURL()},
		EmailsSent:       emails,
	}, nil
}

func (b *mcpShareTestBackend) InviteMCPShareInstance(ctx context.Context, id string, emails []string) (*MCPShareInvite, error) {
	if b.inviteErr != nil {
		return nil, b.inviteErr
	}
	b.lastInviteID = id
	b.lastInviteEmails = emails
	if b.find(id) == nil {
		return nil, fmt.Errorf("memory 404 not found")
	}
	if b.inviteEmailErr != "" {
		return &MCPShareInvite{TokenPrefix: "emt_existing", EmailError: b.inviteEmailErr}, nil
	}
	return &MCPShareInvite{TokenPrefix: "emt_existing", EmailsSent: emails}, nil
}

func (b *mcpShareTestBackend) ListMCPShareCatalog(ctx context.Context) (*MCPShareCatalog, error) {
	if b.toolsErr != nil {
		return nil, b.toolsErr
	}
	return &MCPShareCatalog{Tools: b.catalog, Templates: b.templates}, nil
}

// --- fixtures + wiring ---

func mcpShareFixture() []MCPShareInstance {
	return []MCPShareInstance{
		{
			ID: "sh-1", Name: "research", Description: "read-only research access",
			Tools:  []string{"search_memory", "get_object"},
			Status: "active", CreatedAt: "2026-01-02T00:00:00Z", LastUsedAt: strPtr("2026-01-05T00:00:00Z"),
			ToolCount: 2,
		},
		{
			ID: "sh-legacy", Name: "old share", IsLegacy: true, Status: "active",
			CreatedAt: "2025-12-01T00:00:00Z", ToolCount: 0,
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
	e.POST("/settings/mcp-servers/shares/:id/invite", s.uiMCPShareInvite)
	e.GET("/api/mcp-shares", s.listMCPShares)
	e.POST("/api/mcp-shares", s.createMCPShare)
	e.GET("/api/mcp-shares/tools", s.listMCPShareTools)
	e.GET("/api/mcp-shares/:id", s.getMCPShare)
	e.PATCH("/api/mcp-shares/:id", s.updateMCPShare)
	e.DELETE("/api/mcp-shares/:id", s.deleteMCPShare)
	e.POST("/api/mcp-shares/:id/rotate", s.rotateMCPShare)
	e.POST("/api/mcp-shares/:id/invite", s.inviteMCPShare)
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
		"2 tools",
		`href="/settings/mcp-servers/shares/sh-1/edit"`,
		`data-testid="mcp-share-actions-sh-1"`, `aria-label="Actions for research"`,
		`data-dialog-open="mcp-share-invite-sh-1"`,
		"legacy", "old share",
		`href="/settings/mcp-servers/shares/new"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("shares list missing %q", want)
		}
	}
	// no agent allowlist in the list representation
	for _, absent := range []string{`name="agents"`, "1 agent", "all agents"} {
		if strings.Contains(body, absent) {
			t.Errorf("shares list must not render agent allowlist data %q", absent)
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

func TestMCPShareNewPageRendersToolPicker(t *testing.T) {
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
		`name="emails"`, "Email the key (optional)",
		`data-mcp-tool-search`, `data-mcp-tool-select-all`, `data-mcp-tool-clear`,
		`data-mcp-tool-group`, `name="tools" value="search_memory"`, `name="tools" value="create_agent"`,
		"Memory", "Agents",
		"Create share", `href="/settings/mcp-servers/shares"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("new share page missing %q", want)
		}
	}
	// the retired agent picker must be gone
	for _, absent := range []string{`name="agents"`, "diane", "research-bot", "Leave all unchecked"} {
		if strings.Contains(body, absent) {
			t.Errorf("new share page must not render the agent picker %q", absent)
		}
	}
	if strings.Contains(body, "Save changes") {
		t.Error("new page must not render the edit submit label")
	}
}

// TestMCPShareNewPageIgnoresAgentPreselect proves the retired ?agent= deep link
// no longer preselects anything (the agent picker is gone).
func TestMCPShareNewPageIgnoresAgentPreselect(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/new?agent=a1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET new?agent = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `name="agents"`) || strings.Contains(body, `value="a1"`) {
		t.Error("create form must not render an agent picker or preselect an agent")
	}
}

func TestMCPShareCreateRendersOneTimeReveal(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{MemoryPublicURL: "https://mcp.example.com"}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/new",
		"name=research2&description=hello&tools=search_memory&tools=get_object")
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d, want 200 reveal render", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Share created", "emt_new_secret", "https://mcp.example.com/api/mcp",
		"will not be shown again", `data-mcp-share-reveal`, `clipboardCopyInput`,
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
	// the reveal is 50% wider than the old max-w-2xl (42rem -> 63rem)
	if !strings.Contains(body, `modal-box max-w-[63rem]`) {
		t.Error("reveal modal width not widened to 63rem")
	}
	// Client configs render as plain text in a mono code badge via
	// form.ClipboardCopy (SnippetCard); chroma syntax highlighting was removed
	// with the SnippetCard rewrite.
	if !strings.Contains(body, `clipboardCopyText`) {
		t.Error("snippet copy control (form.ClipboardCopy) missing")
	}
	if strings.Contains(body, `<pre id="mcp-share-snippet-`) {
		t.Error("snippet <pre> block still present — SnippetCard now renders a mono badge")
	}
	// the reveal is a native <dialog> the client auto-opens (data-dialog-autoopen,
	// scanned by app.js on load and after swaps): the modal-open class alone
	// leaves method="dialog" submits (Done / backdrop) inert.
	if !strings.Contains(body, `id="mcp-share-reveal-modal" class="modal"`) {
		t.Error("reveal dialog must use the native modal shell")
	}
	if strings.Contains(body, "modal modal-open") {
		t.Error("reveal dialog must not use the modal-open class")
	}
	if !strings.Contains(body, "data-dialog-autoopen") {
		t.Error("reveal dialog must be marked for client auto-open")
	}
	if len(f.shares) != 1 || f.shares[0].Name != "research2" {
		t.Fatalf("store after create = %+v", f.shares)
	}
	if len(f.lastCreate.Tools) != 2 {
		t.Errorf("create input = %+v", f.lastCreate)
	}
}

// A submitted form must reach validation exactly as the user left it. The
// create-page default is applied only on GET; injecting it into the POST made
// the "at least one tool" rule unenforceable once presets existed.
func TestMCPShareCreateDoesNotInjectDefaultPreset(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	f.templates = mcpShareTemplateFixture()
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	before := len(f.shares)
	rec := mcpSharePost(e, "/settings/mcp-servers/shares/new", "name=no-tools")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Select at least one tool to expose.") {
		t.Errorf("empty-tools create with presets = %d, want inline tools error", rec.Code)
	}
	if len(f.shares) != before || f.lastCreate != nil {
		t.Errorf("an empty submission must not create a share even when a default preset exists")
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

// --- invite email ---

func TestMCPShareParseEmails(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"alice@example.com", []string{"alice@example.com"}},
		{" alice@example.com , bob@example.com\n", []string{"alice@example.com", "bob@example.com"}},
		{"alice@example.com;ALICE@example.com  carol@example.com", []string{"alice@example.com", "carol@example.com"}},
	}
	for _, tc := range cases {
		got := mcpShareParseEmails(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("mcpShareParseEmails(%q) = %v, want %v", tc.raw, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("mcpShareParseEmails(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		}
	}
}

func TestMCPShareCreateInvitesRecipients(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/new",
		"name=research3&tools=search_memory&emails="+url.QueryEscape("alice@example.com, bob@example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d, want 200 reveal render", rec.Code)
	}
	if len(f.lastCreate.Emails) != 2 || f.lastCreate.Emails[0] != "alice@example.com" || f.lastCreate.Emails[1] != "bob@example.com" {
		t.Fatalf("create input emails = %v", f.lastCreate.Emails)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Key emailed to 2 recipients: alice@example.com, bob@example.com.") {
		t.Error("reveal does not confirm the invite recipients")
	}
}

func TestMCPShareCreateRejectsInvalidEmailInline(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/new",
		"name=research4&tools=search_memory&emails=not-an-email")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Not a valid email address: not-an-email") {
		t.Fatalf("invalid-email create = %d, want inline email error", rec.Code)
	}
	if len(f.shares) != 0 {
		t.Fatalf("invalid email must not create a share, store = %+v", f.shares)
	}
}

func TestMCPShareRotateInvitesRecipients(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/rotate",
		"emails="+url.QueryEscape("carol@example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate = %d, want 200 reveal render", rec.Code)
	}
	if len(f.lastRotateEmails) != 1 || f.lastRotateEmails[0] != "carol@example.com" {
		t.Fatalf("rotate emails = %v", f.lastRotateEmails)
	}
	if !strings.Contains(rec.Body.String(), "Key emailed to carol@example.com.") {
		t.Error("rotate reveal does not confirm the invite recipient")
	}
}

func TestMCPShareRotateRejectsInvalidEmail(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/rotate", "emails=nope")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rotate with invalid email = %d, want redirect back to the list", rec.Code)
	}
	if f.lastRotateID != "" {
		t.Error("rotate must not run with an invalid invite list")
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
		`name="tools" value="search_memory"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
	if strings.Contains(body, `name="agents"`) {
		t.Error("edit page must not render the agent picker")
	}
	// The invite field only exists where a key is minted.
	if strings.Contains(body, `name="emails"`) {
		t.Error("edit page must not render the invite-recipient field")
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
		"name=research&description=updated&tools=get_object")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers/shares?updated=1" {
		t.Fatalf("update = %d %q, want 303 ?updated=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastUpdateID != "sh-1" || len(f.lastUpdate.Tools) != 1 || f.lastUpdate.Tools[0] != "get_object" {
		t.Errorf("update input = %s %+v", f.lastUpdateID, f.lastUpdate)
	}
	// submitting an agents field must not reach the backend allowlist
	mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/update",
		"name=research&description=updated&tools=get_object&agents=a1&agents=a2")
	if inst := f.find("sh-1"); inst == nil || len(inst.Tools) != 1 || inst.Description != "updated" {
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
	s := &Server{cfg: Config{MemoryPublicURL: "https://mcp.example.com"}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares",
		`{"name":"api share","tools":["search_memory"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/mcp-shares = %d, want 201", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"token":"emt_new_secret"`, `"mcpUrl":"https://mcp.example.com/api/mcp"`, `"snippets"`, `"instance"`} {
		if !strings.Contains(body, want) {
			t.Errorf("create response missing %q: %s", want, body)
		}
	}
	// the snippet map embeds the key + endpoint
	if !strings.Contains(body, "Bearer emt_new_secret") {
		t.Error("create response snippets must include the key")
	}
	// no invite list ⇒ no delivery fields, so a client cannot mistake "email
	// unavailable" for "asked and failed"
	for _, absent := range []string{`"emailsSent"`, `"emailError"`} {
		if strings.Contains(body, absent) {
			t.Errorf("create without recipients must omit %s: %s", absent, body)
		}
	}
}

func TestMCPShareJSONCreateReportsInviteDelivery(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares",
		`{"name":"api share","tools":["search_memory"],"emails":["alice@example.com"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/mcp-shares = %d, want 201", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"emailsSent":["alice@example.com"]`) {
		t.Errorf("create response must report the invite recipients: %s", body)
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
	f.templates = []MCPShareTemplate{{ID: "read-only", Label: "Read-only", Tools: []string{"search_memory"}, ToolCount: 1, Default: true}}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodGet, "/api/mcp-shares/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET tools = %d, want 200", rec.Code)
	}
	var out struct {
		Tools     []MCPShareTool     `json:"tools"`
		Templates []MCPShareTemplate `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Tools) != 3 {
		t.Fatalf("tools body = %s", rec.Body.String())
	}
	if len(out.Templates) != 1 || out.Templates[0].ID != "read-only" || !out.Templates[0].Default {
		t.Fatalf("templates body = %s", rec.Body.String())
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

	s := &Server{cfg: Config{}, memory: NewMemoryClient(ts.URL, "proj-1")}
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
		`href="/agents/a1/settings/mcp"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("agent dashboard missing %q", want)
		}
	}
}

// TestMCPShareExternalOriginAndBundle asserts that when the backend reports the
// share MCP URL on an internal host, the create reveal and the JSON response
// use the configured memory-server public origin (MEMORY_PUBLIC_URL) — NOT the
// gateway's PUBLIC_BASE_URL, since MCP routes are served by the memory server —
// and the reveal carries a one-click Claude Desktop .mcpb bundle download link
// on that same server origin.
func TestMCPShareExternalOriginAndBundle(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	f.mcpURL = "http://memory-server:3002/api/mcp"
	s := &Server{cfg: Config{
		PublicBaseURL:   "https://gateway.example.com",
		MemoryPublicURL: "https://mcp.example.com",
	}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/new", "name=research2&tools=search_memory")
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d, want 200 reveal render", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "memory-server:3002") {
		t.Errorf("reveal leaked the internal host: %s", body)
	}
	for _, want := range []string{
		"https://mcp.example.com/api/mcp",
		`data-testid="mcp-share-bundle-link"`,
		`href="https://mcp.example.com/api/mcp/bundle?token=emt_new_secret"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reveal missing %q in %s", want, body)
		}
	}
	// The reveal must target the memory server's origin, never the gateway's.
	if strings.Contains(body, "https://gateway.example.com/api/mcp") {
		t.Errorf("reveal used the gateway PUBLIC_BASE_URL instead of the memory-server origin: %s", body)
	}

	jsonRec := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares", `{"name":"api share","tools":["search_memory"]}`)
	if jsonRec.Code != http.StatusCreated {
		t.Fatalf("json create = %d, want 201", jsonRec.Code)
	}
	if strings.Contains(jsonRec.Body.String(), "memory-server:3002") {
		t.Errorf("json create leaked the internal host: %s", jsonRec.Body.String())
	}
	if !strings.Contains(jsonRec.Body.String(), `"mcpUrl":"https://mcp.example.com/api/mcp"`) {
		t.Errorf("json create missing server-public host URL: %s", jsonRec.Body.String())
	}
	if strings.Contains(jsonRec.Body.String(), `"mcpUrl":"https://gateway.example.com/api/mcp"`) {
		t.Errorf("json create used the gateway PUBLIC_BASE_URL instead of the memory-server origin: %s", jsonRec.Body.String())
	}
}

// --- ad-hoc invite (emails the existing key, never rotates) ---

// TestMCPShareInviteEmailsExistingKey proves the invite action reaches the
// backend with the row's id and recipients, redirects to the list, and leaves
// the key alone (the rotate path is never touched).
func TestMCPShareInviteEmailsExistingKey(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/invite",
		"emails="+url.QueryEscape("carol@example.com, dave@example.com"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("invite = %d, want 303 redirect", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "?invited=2") {
		t.Fatalf("invite redirect = %q, want the invited count", loc)
	}
	if f.lastInviteID != "sh-1" {
		t.Fatalf("invite id = %q, want sh-1", f.lastInviteID)
	}
	if len(f.lastInviteEmails) != 2 || f.lastInviteEmails[1] != "dave@example.com" {
		t.Fatalf("invite emails = %v", f.lastInviteEmails)
	}
	if f.lastRotateID != "" {
		t.Error("invite must not rotate the share key")
	}
}

// TestMCPShareInviteNotRecoverablePointsAtRotate proves the one dead end is
// explained rather than surfaced as a raw backend 422.
func TestMCPShareInviteNotRecoverablePointsAtRotate(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	f.inviteErr = &memoryHTTPError{Status: 422, Code: "not_recoverable", Message: "share key cannot be recovered"}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/invite", "emails=carol%40example.com")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("invite = %d, want redirect", rec.Code)
	}
	loc, err := url.QueryUnescape(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("unescape location: %v", err)
	}
	if !strings.Contains(loc, "Issue a new key instead") {
		t.Fatalf("flash %q does not point at the rotate fallback", loc)
	}
}

// TestMCPShareInviteEmailFailureKeepsKey proves a queue failure is reported
// without implying the key changed.
func TestMCPShareInviteEmailFailureKeepsKey(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	f.inviteEmailErr = "Invite email is unavailable on this server."
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/invite", "emails=carol%40example.com")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("invite = %d, want redirect", rec.Code)
	}
	loc, err := url.QueryUnescape(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("unescape location: %v", err)
	}
	if !strings.Contains(loc, "key was not changed") {
		t.Fatalf("flash %q does not say the key survived", loc)
	}
}

// TestMCPShareInviteRejectsBadInput proves validation runs before the backend
// is called.
func TestMCPShareInviteRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"invalid address": "emails=nope",
		"no recipients":   "emails=",
	} {
		t.Run(name, func(t *testing.T) {
			f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
			s := &Server{cfg: Config{}, memory: f}
			e := mcpShareTestServer(s)

			rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-1/invite", body)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("invite = %d, want redirect back to the list", rec.Code)
			}
			if !strings.Contains(rec.Header().Get("Location"), "err=") {
				t.Error("rejected invite must carry an error flash")
			}
			if f.lastInviteID != "" {
				t.Error("the backend must not be called with a rejected invite list")
			}
			if f.lastRotateID != "" {
				t.Error("a rejected invite must not rotate the key")
			}
		})
	}
}

// TestMCPShareInviteLegacyRejected matches rotate/revoke: legacy token rows are
// read-only.
func TestMCPShareInviteLegacyRejected(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpSharePost(e, "/settings/mcp-servers/shares/sh-legacy/invite", "emails=carol%40example.com")
	if f.lastInviteID != "" {
		t.Error("legacy shares must not be invited")
	}
	if !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Error("legacy invite must flash an explanation")
	}
}

// TestMCPShareInviteFlashCount proves the list confirms how many recipients the
// key went to.
func TestMCPShareInviteFlashCount(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares?invited=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Invite emailed to 2 recipients.") {
		t.Error("list does not confirm the invite recipients")
	}
}

// TestMCPShareActionsMenuItems proves the row exposes a labelled actions menu
// (edit, invite, issue a new key, revoke) and that legacy rows stay read-only.
func TestMCPShareActionsMenuItems(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares")
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="mcp-share-actions-sh-1"`,
		`data-dialog-open="mcp-share-invite-sh-1"`,
		`data-testid="mcp-share-invite-open-sh-1"`,
		`Invite by email`,
		`id="mcp-share-invite-sh-1"`,
		`mcp-share-invite-send-sh-1`,
		`mcp-share-rotate-open-sh-1`,
		`mcp-share-revoke-open-sh-1`,
		`mcp-share-edit-link-sh-1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("share row missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="mcp-share-actions-sh-legacy"`) {
		t.Error("legacy rows must not offer the actions menu")
	}
	// The invite dialog can fall back to the rotate flow with the same field.
	if !strings.Contains(body, `formaction="/settings/mcp-servers/shares/sh-1/rotate"`) {
		t.Error("invite dialog does not offer the rotate fallback")
	}
}

// TestMCPShareJSONInvite proves the JSON surface proxies the invite and reports
// which key went out.
func TestMCPShareJSONInvite(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares/sh-1/invite", `{"emails":["carol@example.com"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("invite = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got MCPShareInvite
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TokenPrefix != "emt_existing" {
		t.Fatalf("tokenPrefix = %q, want the existing key's prefix", got.TokenPrefix)
	}
	if len(got.EmailsSent) != 1 || got.EmailsSent[0] != "carol@example.com" {
		t.Fatalf("emailsSent = %v", got.EmailsSent)
	}
	if f.lastRotateID != "" {
		t.Error("JSON invite must not rotate the key")
	}

	bad := mcpShareJSONReq(e, http.MethodPost, "/api/mcp-shares/sh-1/invite", `{"emails":[]}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("empty invite = %d, want 400", bad.Code)
// --- share templates ---

// mcpShareTemplateFixture is a two-preset catalog: a default read-only preset
// and a non-default writer preset.
func mcpShareTemplateFixture() []MCPShareTemplate {
	return []MCPShareTemplate{
		{ID: "read-only", Label: "Read-only", Description: "Read only tools.", Default: true,
			Tools: []string{"search_memory"}, ToolCount: 1},
		{ID: "knowledge-writer", Label: "Knowledge writer", Description: "Write tools.",
			Tools: []string{"search_memory", "create_agent"}, ToolCount: 2},
	}
}

func TestMCPShareNewPageRendersPresetRow(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	f.templates = mcpShareTemplateFixture()
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET new = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Start from a preset",
		`data-share-template="read-only"`,
		`data-share-template="knowledge-writer"`,
		`data-share-template-tools="[&#34;search_memory&#34;]"`,
		`data-share-template-tools="[&#34;search_memory&#34;,&#34;create_agent&#34;]"`,
		"Read-only", "Knowledge writer", "1 tool", "2 tools",
		`data-mcp-tool-count`, "1 selected",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("new share page missing %q", want)
		}
	}
	// The default preset is what the form starts on, so its chip must render
	// pressed rather than looking like no preset was chosen.
	if got := pressedShareTemplateIDs(body); len(got) != 1 || got[0] != "read-only" {
		t.Errorf("pressed preset chips = %v, want [read-only]", got)
	}
	if !strings.Contains(shareTemplateTag(body, "read-only"), "btn-active") {
		t.Error("the default preset chip must carry btn-active")
	}
	if strings.Contains(shareTemplateTag(body, "knowledge-writer"), "btn-active") {
		t.Error("a non-default preset must not render pressed")
	}
}

// shareTemplateTag returns the opening <button ...> tag for a preset id.
// Attribute order is not stable (ui.Button spreads a map), so this locates the
// id and walks out to the enclosing tag instead of matching a fixed string.
func shareTemplateTag(body, id string) string {
	idx := strings.Index(body, `data-share-template="`+id+`"`)
	if idx < 0 {
		return ""
	}
	start := strings.LastIndex(body[:idx], "<button")
	if start < 0 {
		return ""
	}
	end := strings.Index(body[idx:], ">")
	if end < 0 {
		return ""
	}
	return body[start : idx+end]
}

// pressedShareTemplateIDs lists the presets rendered with aria-pressed="true".
func pressedShareTemplateIDs(body string) []string {
	var ids []string
	for _, chunk := range strings.Split(body, "<button")[1:] {
		tag := chunk
		if end := strings.Index(tag, ">"); end >= 0 {
			tag = tag[:end]
		}
		if !strings.Contains(tag, `aria-pressed="true"`) {
			continue
		}
		const marker = `data-share-template="`
		i := strings.Index(tag, marker)
		if i < 0 {
			continue
		}
		rest := tag[i+len(marker):]
		if j := strings.Index(rest, `"`); j >= 0 {
			ids = append(ids, rest[:j])
		}
	}
	return ids
}

func TestMCPShareNewPagePreselectsDefaultTemplate(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	f.templates = mcpShareTemplateFixture()
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET new = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="search_memory" class="checkbox checkbox-sm mt-0.5" checked`) {
		t.Errorf("default preset tool must be preselected on the create form: %s", body)
	}
	// A non-default preset must not leak its extra tools into the initial state.
	if strings.Contains(body, `value="create_agent" class="checkbox checkbox-sm mt-0.5" checked`) {
		t.Error("the writer preset's tool must not be preselected")
	}
}

func TestMCPShareEditPageDoesNotApplyDefaultTemplate(t *testing.T) {
	f := newMCPShareTestBackend(mcpShareFixture(), mcpShareCatalogFixture())
	f.templates = mcpShareTemplateFixture()
	// Keep the share's own selection away from the default preset's tool so the
	// assertion cannot pass by accident.
	f.shares[0].Tools = []string{"get_object"}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/sh-1/edit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET edit = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `value="search_memory" class="checkbox checkbox-sm mt-0.5" checked`) {
		t.Error("editing a share must never apply a preset to its existing grant")
	}
	if !strings.Contains(body, `data-share-template="read-only"`) {
		t.Error("edit page must still offer the presets")
	}
}

func TestMCPShareNewPageWithoutTemplatesHidesPresetRow(t *testing.T) {
	f := newMCPShareTestBackend(nil, mcpShareCatalogFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpShareTestServer(s)

	rec := mcpShareGet(e, "/settings/mcp-servers/shares/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET new = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "Start from a preset") || strings.Contains(body, `data-share-template="`) {
		t.Error("a backend without presets must render the picker with no preset row")
	}
	if strings.Contains(body, `class="checkbox checkbox-sm mt-0.5" checked`) {
		t.Error("no preset means no preselected tool")
	}
}

func TestMCPShareApplyDefaultTemplate(t *testing.T) {
	catalog := &MCPShareCatalog{Templates: mcpShareTemplateFixture()}
	if got := mcpShareApplyDefaultTemplate(catalog, nil); !reflect.DeepEqual(got, []string{"search_memory"}) {
		t.Errorf("empty draft = %v, want the default preset's tools", got)
	}
	if got := mcpShareApplyDefaultTemplate(catalog, []string{"get_object"}); !reflect.DeepEqual(got, []string{"get_object"}) {
		t.Errorf("existing selection = %v, must be preserved", got)
	}
	if got := mcpShareApplyDefaultTemplate(&MCPShareCatalog{}, nil); got != nil {
		t.Errorf("no presets = %v, want nil", got)
	}
	if got := mcpShareApplyDefaultTemplate(nil, nil); got != nil {
		t.Errorf("nil catalog = %v, want nil", got)
	}
}

func TestMCPShareTemplateToolsAttr(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, "[]"},
		{"one", []string{"a"}, `["a"]`},
		{"many", []string{"a", "b"}, `["a","b"]`},
	}
	for _, tc := range cases {
		if got := mcpShareTemplateToolsAttr(MCPShareTemplate{Tools: tc.in}); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMCPShareSameToolSet(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"same order", []string{"a", "b"}, []string{"a", "b"}, true},
		{"reordered", []string{"a", "b"}, []string{"b", "a"}, true},
		{"duplicates collapse", []string{"a", "a", "b"}, []string{"b", "a"}, true},
		{"subset is not equal", []string{"a"}, []string{"a", "b"}, false},
		{"disjoint", []string{"a"}, []string{"b"}, false},
		{"both empty", nil, nil, false},
		{"one empty", nil, []string{"a"}, false},
	}
	for _, tc := range cases {
		if got := mcpShareSameToolSet(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMCPShareSelectedLabel(t *testing.T) {
	catalog := []MCPShareTool{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"none", nil, "0 selected"},
		{"some", []string{"b"}, "1 selected"},
		{"all", []string{"a", "b", "c"}, "3 selected"},
		// A tool the catalog no longer offers has no checkbox to tick, so it
		// must not inflate the count the picker shows.
		{"unknown ignored", []string{"a", "gone"}, "1 selected"},
	}
	for _, tc := range cases {
		if got := mcpShareSelectedLabel(catalog, tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMCPShareTemplateActivePresentation(t *testing.T) {
	if got := mcpShareTemplateActiveClass(true); got != "gap-1.5 btn-active" {
		t.Errorf("active class = %q", got)
	}
	if got := mcpShareTemplateActiveClass(false); got != "gap-1.5" {
		t.Errorf("inactive class = %q", got)
	}
	if got := mcpShareAriaPressed(true); got != "true" {
		t.Errorf("aria-pressed true = %q", got)
	}
	if got := mcpShareAriaPressed(false); got != "false" {
		t.Errorf("aria-pressed false = %q", got)
	}
}
