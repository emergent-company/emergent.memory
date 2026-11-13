package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestStreamableHandlerProvidersDoNotConflict pins the fx dependency-graph
// contract: the project transport and the operator transport are provided by
// separate constructors, so they must have distinct result types or fx panics
// at startup with "already provided". This test builds a minimal graph with both
// providers and asserts it constructs cleanly (regression: the admin endpoint
// was added with the same `*StreamableHTTPHandler` result type as the project
// one, which crashed the server before serving a single request).
func TestStreamableHandlerProvidersDoNotConflict(t *testing.T) {
	app := fx.New(
		fx.NopLogger,
		fx.Provide(func() *Service { return &Service{} }),
		fx.Provide(func() *slog.Logger { return slog.Default() }),
		fx.Provide(NewStreamableHTTPHandler),
		fx.Provide(NewAdminStreamableHTTPHandler),
	)
	require.NoError(t, app.Err(), "project and operator transports must provide distinct types")
}

// ============================================================================
// Catalog shape / naming
// ============================================================================

// TestSuperadminToolDefinitionsCatalog pins the NEW operator catalog: every tool
// is SuperadminOnly, has an empty RequiredScope, and its name is kebab-case.
func TestSuperadminToolDefinitionsCatalog(t *testing.T) {
	tools := superadminToolDefinitions()
	require.NotEmpty(t, tools, "operator catalog must not be empty")

	names := map[string]bool{}
	kebab := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

	for _, tool := range tools {
		names[tool.Name] = true
		if !tool.SuperadminOnly {
			t.Errorf("operator tool %q must be SuperadminOnly", tool.Name)
		}
		if tool.RequiredScope != "" {
			t.Errorf("operator tool %q must have empty RequiredScope, got %q", tool.Name, tool.RequiredScope)
		}
		if !kebab.MatchString(tool.Name) {
			t.Errorf("operator tool %q is not kebab-case", tool.Name)
		}
		if strings.Contains(tool.Name, "_") {
			t.Errorf("operator tool %q contains an underscore", tool.Name)
		}
	}

	want := []string{
		"superadmin-get", "user-list", "user-get", "org-list", "org-get",
		"project-list", "project-record-get", "project-member-list",
		"email-job-list", "email-job-get",
		"embedding-job-list", "embedding-job-stats",
		"extraction-job-list", "extraction-job-stats",
		"document-parsing-job-list", "document-parsing-job-stats",
		"user-delete", "org-delete", "project-delete", "superadmin-grant",
		"project-member-create", "project-member-delete", "service-token-create",
		"embedding-job-delete", "embedding-job-cleanup", "embedding-job-reset",
		"extraction-job-delete", "extraction-job-cancel",
		"document-parsing-job-delete", "document-parsing-job-retry",
	}
	for _, name := range want {
		if !names[name] {
			t.Errorf("operator catalog missing tool %q", name)
		}
	}
	if len(tools) != len(want) {
		t.Errorf("operator catalog has %d tools, want %d", len(tools), len(want))
	}
}

// TestSuperadminOnlyToolNamesCoversOperatorCatalog proves the derived gate set
// covers every NEW operator tool, so IsSuperadminOnlyTool and the in-process
// gate cannot drift from the catalog (no manual map).
func TestSuperadminOnlyToolNamesCoversOperatorCatalog(t *testing.T) {
	for _, def := range superadminToolDefinitions() {
		if !superadminOnlyToolNames[def.Name] {
			t.Errorf("superadminOnlyToolNames missing operator tool %q", def.Name)
		}
		if !superadminCatalogToolNames[def.Name] {
			t.Errorf("admin catalog missing operator tool %q", def.Name)
		}
	}
}

// TestProjectRecordGetNotShadowingProjectGet proves the operator "project-record-get"
// is superadmin-only while the project-scoped "project-get" tool is NOT.
func TestProjectRecordGetNotShadowingProjectGet(t *testing.T) {
	svc := &Service{}
	if !svc.IsSuperadminOnlyTool("project-record-get") {
		t.Fatal("operator project-record-get must be superadmin-only")
	}
	if !svc.IsAdminOperatorTool("project-record-get") {
		t.Fatal("operator project-record-get must be in the admin catalog")
	}
	if svc.IsSuperadminOnlyTool("project-get") {
		t.Fatal("project-scoped project-get must not be superadmin-only")
	}
}

// TestGetSuperadminToolDefinitionsIncludesExistingOperatorTools proves the admin
// catalog also carries the pre-existing deployment-wide tools (embedding-*,
// provider-*, trace-*).
func TestGetSuperadminToolDefinitionsIncludesExistingOperatorTools(t *testing.T) {
	svc := &Service{}
	catalog := svc.GetSuperadminToolDefinitions()
	names := map[string]bool{}
	for _, def := range catalog {
		names[def.Name] = true
	}
	for _, want := range []string{
		"embedding-status", "embedding-pause", "embedding-resume", "embedding-config-update",
		"provider-test", "provider-usage-get",
		"trace-list", "trace-get",
		"user-list", "project-delete", // new tools also present
	} {
		if !names[want] {
			t.Errorf("admin catalog missing tool %q", want)
		}
	}
	// No project-scoped tool names leak into the admin catalog.
	for _, banned := range []string{"entity-query", "schema-list", "project-briefing"} {
		if names[banned] {
			t.Errorf("admin catalog must not contain project-scoped tool %q", banned)
		}
	}
}

// ============================================================================
// Fake handler
// ============================================================================

type fakeSuperadminHandler struct {
	calls []string
	user  any
	org   any
	proj  any
	list  any
	count int
	err   error
}

func (f *fakeSuperadminHandler) rec(m string) { f.calls = append(f.calls, m) }

func (f *fakeSuperadminHandler) SuperadminGet(context.Context) (any, error) {
	f.rec("SuperadminGet")
	return map[string]any{"user_id": "u1", "role": "superadmin_full"}, f.err
}
func (f *fakeSuperadminHandler) ListUsers(context.Context, int, int, string, string) (any, error) {
	f.rec("ListUsers")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) GetUser(context.Context, string) (any, error) {
	f.rec("GetUser")
	return f.user, f.err
}
func (f *fakeSuperadminHandler) ListOrganizations(context.Context, int, int) (any, error) {
	f.rec("ListOrganizations")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) GetOrg(context.Context, string) (any, error) {
	f.rec("GetOrg")
	return f.org, f.err
}
func (f *fakeSuperadminHandler) ListProjects(context.Context, int, int, string) (any, error) {
	f.rec("ListProjects")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) GetProject(context.Context, string) (any, error) {
	f.rec("GetProject")
	return f.proj, f.err
}
func (f *fakeSuperadminHandler) ListProjectMembers(context.Context, string) (any, error) {
	f.rec("ListProjectMembers")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) ListEmailJobs(context.Context, int, int, string, string, string, string) (any, error) {
	f.rec("ListEmailJobs")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) GetEmailJob(context.Context, string) (any, error) {
	f.rec("GetEmailJob")
	return f.user, f.err
}
func (f *fakeSuperadminHandler) ListEmbeddingJobs(context.Context, int, int, string, *bool, string, string) (any, error) {
	f.rec("ListEmbeddingJobs")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) GetEmbeddingJobStats(context.Context) (any, error) {
	f.rec("GetEmbeddingJobStats")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) ListExtractionJobs(context.Context, int, int, string, string, string, *bool) (any, error) {
	f.rec("ListExtractionJobs")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) GetExtractionJobStats(context.Context) (any, error) {
	f.rec("GetExtractionJobStats")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) ListDocumentParsingJobs(context.Context, int, int, string, string, *bool) (any, error) {
	f.rec("ListDocumentParsingJobs")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) GetDocumentParsingJobStats(context.Context) (any, error) {
	f.rec("GetDocumentParsingJobStats")
	return f.list, f.err
}
func (f *fakeSuperadminHandler) SoftDeleteUser(context.Context, string, string) error {
	f.rec("SoftDeleteUser")
	return f.err
}
func (f *fakeSuperadminHandler) SoftDeleteOrg(context.Context, string, string) error {
	f.rec("SoftDeleteOrg")
	return f.err
}
func (f *fakeSuperadminHandler) SoftDeleteProject(context.Context, string, string) error {
	f.rec("SoftDeleteProject")
	return f.err
}
func (f *fakeSuperadminHandler) GrantSuperadmin(context.Context, string, string, *string) error {
	f.rec("GrantSuperadmin")
	return f.err
}
func (f *fakeSuperadminHandler) AddProjectMember(context.Context, string, string, string) error {
	f.rec("AddProjectMember")
	return f.err
}
func (f *fakeSuperadminHandler) RemoveProjectMember(context.Context, string, string) error {
	f.rec("RemoveProjectMember")
	return f.err
}
func (f *fakeSuperadminHandler) CreateServiceToken(context.Context, string, *string, string) (any, error) {
	f.rec("CreateServiceToken")
	return map[string]any{"token": "tok-secret-value"}, f.err
}
func (f *fakeSuperadminHandler) DeleteEmbeddingJobs(context.Context, []string, string) (int, error) {
	f.rec("DeleteEmbeddingJobs")
	return f.count, f.err
}
func (f *fakeSuperadminHandler) CleanupEmbeddingJobs(context.Context) (int, error) {
	f.rec("CleanupEmbeddingJobs")
	return f.count, f.err
}
func (f *fakeSuperadminHandler) ResetEmbeddingJobs(context.Context) (int, error) {
	f.rec("ResetEmbeddingJobs")
	return f.count, f.err
}
func (f *fakeSuperadminHandler) DeleteExtractionJobs(context.Context, []string) (int, error) {
	f.rec("DeleteExtractionJobs")
	return f.count, f.err
}
func (f *fakeSuperadminHandler) CancelExtractionJobs(context.Context, []string) (int, error) {
	f.rec("CancelExtractionJobs")
	return f.count, f.err
}
func (f *fakeSuperadminHandler) DeleteDocumentParsingJobs(context.Context, []string) (int, error) {
	f.rec("DeleteDocumentParsingJobs")
	return f.count, f.err
}
func (f *fakeSuperadminHandler) RetryDocumentParsingJobs(context.Context, []string) (int, error) {
	f.rec("RetryDocumentParsingJobs")
	return f.count, f.err
}

// ============================================================================
// Handler delegation + confirm guard
// ============================================================================

// TestSuperadminReadHandlersDelegate proves each read handler parses its
// arguments and delegates to the corresponding repository/interface method.
func TestSuperadminReadHandlersDelegate(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name     string
		args     map[string]any
		call     string
		wantCall string
	}{
		{"superadmin-get", map[string]any{}, "superadmin-get", "SuperadminGet"},
		{"user-list", map[string]any{"page": 2.0}, "user-list", "ListUsers"},
		{"user-get", map[string]any{"user_id": "u1"}, "user-get", "GetUser"},
		{"org-list", map[string]any{}, "org-list", "ListOrganizations"},
		{"org-get", map[string]any{"org_id": "o1"}, "org-get", "GetOrg"},
		{"project-list", map[string]any{}, "project-list", "ListProjects"},
		{"project-record-get", map[string]any{"project_id": "p1"}, "project-record-get", "GetProject"},
		{"project-member-list", map[string]any{"project_id": "p1"}, "project-member-list", "ListProjectMembers"},
		{"email-job-list", map[string]any{}, "email-job-list", "ListEmailJobs"},
		{"email-job-get", map[string]any{"job_id": "j1"}, "email-job-get", "GetEmailJob"},
		{"embedding-job-list", map[string]any{}, "embedding-job-list", "ListEmbeddingJobs"},
		{"embedding-job-stats", map[string]any{}, "embedding-job-stats", "GetEmbeddingJobStats"},
		{"extraction-job-list", map[string]any{}, "extraction-job-list", "ListExtractionJobs"},
		{"extraction-job-stats", map[string]any{}, "extraction-job-stats", "GetExtractionJobStats"},
		{"document-parsing-job-list", map[string]any{}, "document-parsing-job-list", "ListDocumentParsingJobs"},
		{"document-parsing-job-stats", map[string]any{}, "document-parsing-job-stats", "GetDocumentParsingJobStats"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSuperadminHandler{list: map[string]any{"ok": true}, user: map[string]any{"id": "u1"}, org: map[string]any{"id": "o1"}, proj: map[string]any{"id": "p1"}}
			svc := &Service{superadminToolHandler: f}
			_, err := svc.dispatchSuperadminTool(ctx, tc.name, tc.args)
			require.NoError(t, err)
			require.Contains(t, f.calls, tc.wantCall, "expected %s to delegate to %s, got %v", tc.name, tc.wantCall, f.calls)
		})
	}
}

// TestSuperadminReadMissingIdentifierRejected proves a read tool missing its
// required identifier returns an error naming the field and performs no call.
func TestSuperadminReadMissingIdentifierRejected(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		field string
	}{
		{"user-get", "user_id"},
		{"org-get", "org_id"},
		{"project-record-get", "project_id"},
		{"project-member-list", "project_id"},
		{"email-job-get", "job_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSuperadminHandler{}
			svc := &Service{superadminToolHandler: f}
			_, err := svc.dispatchSuperadminTool(ctx, tc.name, map[string]any{})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.field, "error must name the missing field %q", tc.field)
			require.Empty(t, f.calls, "no repository call must be made")
		})
	}
}

// TestSuperadminMutationConfirmDryRun proves an unconfirmed mutation returns a
// structured dry-run and performs zero repository mutation.
func TestSuperadminMutationConfirmDryRun(t *testing.T) {
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "actor-1"})
	cases := []struct {
		name   string
		args   map[string]any
		action string
	}{
		{"user-delete", map[string]any{"user_id": "u1"}, "delete user"},
		{"org-delete", map[string]any{"org_id": "o1"}, "delete organization"},
		{"project-delete", map[string]any{"project_id": "p1"}, "delete project"},
		{"superadmin-grant", map[string]any{"user_id": "u1"}, "grant superadmin_readonly"},
		{"project-member-create", map[string]any{"project_id": "p1", "user_id": "u1", "role": "editor"}, "add project member"},
		{"project-member-delete", map[string]any{"project_id": "p1", "user_id": "u1"}, "remove project member"},
		{"service-token-create", map[string]any{"name": "svc"}, "create service token"},
		{"embedding-job-delete", map[string]any{"ids": []any{"j1"}}, "delete embedding jobs"},
		{"embedding-job-cleanup", map[string]any{}, "cleanup orphan embedding jobs"},
		{"embedding-job-reset", map[string]any{}, "reset dead-letter embedding jobs"},
		{"extraction-job-delete", map[string]any{"ids": []any{"j1"}}, "delete extraction jobs"},
		{"extraction-job-cancel", map[string]any{"ids": []any{"j1"}}, "cancel extraction jobs"},
		{"document-parsing-job-delete", map[string]any{"ids": []any{"j1"}}, "delete document parsing jobs"},
		{"document-parsing-job-retry", map[string]any{"ids": []any{"j1"}}, "retry document parsing jobs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSuperadminHandler{}
			svc := &Service{superadminToolHandler: f}
			result, err := svc.dispatchSuperadminTool(ctx, tc.name, tc.args)
			require.NoError(t, err)
			require.Empty(t, f.calls, "unconfirmed mutation must not call the repository")
			sc := result.StructuredContent
			require.Equal(t, true, sc["dry_run"], "dry_run must be true")
			require.Equal(t, tc.action, sc["action"], "dry-run must name the action")
		})
	}
}

// TestSuperadminMutationConfirmExecutesOnce proves a confirmed mutation invokes
// the repository exactly once and returns its outcome.
func TestSuperadminMutationConfirmExecutesOnce(t *testing.T) {
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "actor-1"})
	f := &fakeSuperadminHandler{count: 3}
	svc := &Service{superadminToolHandler: f}

	result, err := svc.dispatchSuperadminTool(ctx, "user-delete", map[string]any{"user_id": "u1", "confirm": true})
	require.NoError(t, err)
	require.Equal(t, []string{"SoftDeleteUser"}, f.calls, "exactly one mutation call expected")
	require.Equal(t, true, result.StructuredContent["success"])

	f2 := &fakeSuperadminHandler{count: 5}
	svc.superadminToolHandler = f2
	result, err = svc.dispatchSuperadminTool(ctx, "embedding-job-delete", map[string]any{"ids": []any{"j1"}, "confirm": true})
	require.NoError(t, err)
	require.Equal(t, []string{"DeleteEmbeddingJobs"}, f2.calls)
	require.Equal(t, float64(5), result.StructuredContent["deleted_count"])
}

// TestSuperadminMutationMalformedRejectedBeforeConfirm proves malformed/missing
// identifiers are rejected before the confirmation check and before any call.
func TestSuperadminMutationMalformedRejectedBeforeConfirm(t *testing.T) {
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "actor-1"})
	f := &fakeSuperadminHandler{}
	svc := &Service{superadminToolHandler: f}

	_, err := svc.dispatchSuperadminTool(ctx, "user-delete", map[string]any{"confirm": true}) // no user_id
	require.Error(t, err)
	require.Contains(t, err.Error(), "user_id")
	require.Empty(t, f.calls)

	_, err = svc.dispatchSuperadminTool(ctx, "embedding-job-delete", map[string]any{"confirm": true}) // no ids
	require.Error(t, err)
	require.Contains(t, err.Error(), "ids")
	require.Empty(t, f.calls)

	_, err = svc.dispatchSuperadminTool(ctx, "service-token-create", map[string]any{"confirm": true}) // no name
	require.Error(t, err)
	require.Contains(t, err.Error(), "name")
	require.Empty(t, f.calls)
}

// ============================================================================
// Nil handler fail-closed
// ============================================================================

// TestSuperadminNilHandlerFailsClosed proves that with the feature off (nil
// handler), every operator tool fails closed with a clear error and performs no
// operation.
func TestSuperadminNilHandlerFailsClosed(t *testing.T) {
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "u1"})
	svc := &Service{} // superadminToolHandler nil
	for _, name := range []string{
		"superadmin-get", "user-list", "user-get", "user-delete", "project-delete",
		"service-token-create", "embedding-job-reset",
	} {
		_, err := svc.dispatchSuperadminTool(ctx, name, map[string]any{"user_id": "u1", "confirm": true})
		require.Error(t, err, "%s must fail closed with a nil handler", name)
		require.Contains(t, err.Error(), "not available", "%s must name the missing handler", name)
	}
}

// ============================================================================
// Hard split on the project endpoint (non-DB)
// ============================================================================

// TestProjectEndpointHardSplit proves the project MCP transports never list
// SuperadminOnly tools and refuse calling them, even for a superadmin_full
// principal (which would previously have been admitted).
func TestProjectEndpointHardSplit(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions() // populate tool index
	h := NewHandler(svc, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)

	superUser := &auth.AuthUser{ID: uuid.New().String(), Scopes: []string{"admin"}, ProjectID: uuid.New().String()}
	token := "tok-super"
	h.sessionsMu.Lock()
	h.sessions[token] = &Session{Initialized: true, ProjectID: superUser.ProjectID}
	h.sessionsMu.Unlock()

	// tools/list: no SuperadminOnly tool appears.
	c, _ := gateContext(superUser, token)
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/list"}
	resp := h.handleToolsList(c, req, superUser)
	require.Nil(t, resp.Error)
	list, ok := resp.Result.(ToolsListResult)
	require.True(t, ok, "tools/list must return ToolsListResult")
	for _, def := range list.Tools {
		if def.SuperadminOnly {
			t.Errorf("project endpoint must not list SuperadminOnly tool %q", def.Name)
		}
	}

	// tools/call: operator tool name is method-not-found and no operation runs.
	params, _ := json.Marshal(ToolsCallParams{Name: "embedding-pause", Arguments: map[string]any{}})
	req = &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}
	resp = h.handleToolsCall(c, req, superUser)
	require.NotNil(t, resp.Error)
	require.Equal(t, ErrCodeMethodNotFound, resp.Error.Code, "operator tool on project endpoint must be method-not-found")
}

// ============================================================================
// Audit redaction + best-effort
// ============================================================================

func TestRedactSecrets(t *testing.T) {
	in := map[string]any{
		"org_id":        "o1",
		"api_key":       "sk-abc123",
		"provider_name": "google",
		"nested": map[string]any{
			"service_account_json": `{"client_email":"x"}`,
			"gcp_project":          "proj-1",
		},
		"tokens":        []any{"t1", "t2"},
		"authorization": "Bearer secret",
	}
	out := RedactSecrets(in)
	require.Equal(t, "[REDACTED]", out["api_key"])
	require.Equal(t, "[REDACTED]", out["authorization"])
	require.Equal(t, "o1", out["org_id"])
	nested := out["nested"].(map[string]any)
	require.Equal(t, "[REDACTED]", nested["service_account_json"])
	require.Equal(t, "proj-1", nested["gcp_project"])
}

type failingAuditWriter struct{ err error }

func (f *failingAuditWriter) Write(context.Context, SuperadminAuditEntry) error { return f.err }

// TestAuditBestEffort proves an audit-writer failure never changes the tool
// result — the emit path returns nothing and logs a warning.
func TestAuditBestEffort(t *testing.T) {
	svc := &Service{
		log:                   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		superadminAuditWriter: &failingAuditWriter{err: errors.New("audit store down")},
	}
	// Must not panic and must not return anything.
	svc.auditSuperadmin(context.Background(), SuperadminAuditEntry{
		EventType: "mcp.superadmin.tool_call",
		Outcome:   "success",
		Action:    "user-list",
		Details:   map[string]any{"api_key": "secret"},
	})
}

// ============================================================================
// Admin endpoint route gating (non-DB: 401)
// ============================================================================

func registerTestRoutesWithAdmin(t *testing.T) *echo.Echo {
	t.Helper()
	e := echo.New()
	svc := &Service{}
	h := NewHandler(svc, slog.Default(), nil)
	sse := NewSSEHandler(svc, h, slog.Default())
	stream := NewStreamableHTTPHandler(svc, slog.Default())
	adminStream := NewAdminStreamableHTTPHandler(svc, slog.Default())
	ep := NewAgentEndpointHandler(svc, slog.Default())
	mw := auth.NewMiddleware(auth.MiddlewareParams{Cfg: &config.Config{}, Log: slog.Default()})
	RegisterRoutes(e, h, sse, stream, adminStream, ep, mw)
	return e
}

// TestAdminEndpointUnauthenticated401 proves the dedicated operator endpoint
// sits behind RequireAuth (401 for an unauthenticated caller).
func TestAdminEndpointUnauthenticated401(t *testing.T) {
	e := registerTestRoutesWithAdmin(t)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/mcp/admin", nil)
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "%s /api/mcp/admin must be 401 when unauthenticated", method)
	}
}

// TestAdminEndpointRouteRegistered proves the admin route is registered with the
// expected methods (the 401 above already proves auth is in front; this pins the
// route table).
func TestAdminEndpointRouteRegistered(t *testing.T) {
	e := registerTestRoutesWithAdmin(t)
	routes := routeSet(e)
	for _, m := range []string{"GET", "POST", "DELETE"} {
		require.True(t, routes[m+" /api/mcp/admin"], "%s /api/mcp/admin must be registered", m)
	}
}

// ============================================================================
// In-process authority parity for a NEW operator tool (non-DB fail-closed)
// ============================================================================

// TestInProcessOperatorToolDeniedWithoutGrant proves an in-process dispatch of a
// NEW operator tool (project-delete) without a resolvable superadmin_full grant
// is denied before any handler runs.
func TestInProcessOperatorToolDeniedWithoutGrant(t *testing.T) {
	svc := &Service{} // db nil → grant unresolvable → fail-closed
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: uuid.New().String(), Scopes: []string{"admin:all"}})

	_, err := svc.ExecuteTool(ctx, "", "project-delete", map[string]any{"project_id": "p1", "confirm": true})
	require.Error(t, err, "in-process operator tool without a grant must be denied")
}

// TestAdminEndpointRefusesProjectTool proves a project-scoped tool name is
// method-not-found on the admin transport (only the operator catalog is served).
func TestAdminEndpointRefusesProjectTool(t *testing.T) {
	svc := &Service{}
	_ = svc.GetToolDefinitions()
	h := NewAdminStreamableHTTPHandler(svc, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	superUser := &auth.AuthUser{ID: uuid.New().String(), Scopes: []string{"admin"}}
	sess := &MCPSession{ID: "s1", Initialized: true}
	c, _ := gateContext(superUser, "tok")

	params, _ := json.Marshal(ToolsCallParams{Name: "entity-query", Arguments: map[string]any{}})
	req := &Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}
	resp := h.handleAdminToolsCall(c, req, sess, superUser, ToolsCallParams{Name: "entity-query"})
	require.NotNil(t, resp.Error)
	require.Equal(t, ErrCodeMethodNotFound, resp.Error.Code, "project tool on admin endpoint must be method-not-found")
}

// ============================================================================
// S1 / N2 / N3 / N4
// ============================================================================

// TestSuperadminMutationToolNamesCoverage pins the mutating operator tool set:
// it covers the pre-existing SuperadminOnly mutators
// (embedding-pause/resume/config-update) AND the new mutation tools.
func TestSuperadminMutationToolNamesCoverage(t *testing.T) {
	for _, name := range []string{
		"embedding-pause", "embedding-resume", "embedding-config-update",
	} {
		if !superadminMutationToolNames[name] {
			t.Errorf("pre-existing mutator %q missing from superadminMutationToolNames", name)
		}
	}
	for _, name := range []string{"user-delete", "service-token-create", "embedding-job-reset", "project-member-create"} {
		if !superadminMutationToolNames[name] {
			t.Errorf("new mutation %q missing from superadminMutationToolNames", name)
		}
	}
	// Reads must NOT be in the mutation set.
	for _, name := range []string{"user-list", "superadmin-get", "embedding-job-stats", "trace-list", "embedding-status"} {
		if superadminMutationToolNames[name] {
			t.Errorf("read tool %q must not be in superadminMutationToolNames", name)
		}
	}
}

// TestRedactString proves the free-form string redaction boundary (N2): secret
// key=value pairs are redacted, ordinary errors are left unchanged.
func TestRedactString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"configure_project_provider: api_key=sk-abc123 failed", "configure_project_provider: api_key=[REDACTED] failed"},
		{"token=secret123", "token=[REDACTED]"},
		{"service_account_json={\"client_email\":\"x\"}", "service_account_json=[REDACTED]"},
		{"ordinary error with no secrets", "ordinary error with no secrets"},
		{"user_delete: 'user_id' is required", "user_delete: 'user_id' is required"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := RedactString(tc.in); got != tc.want {
			t.Errorf("RedactString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestInProcessProjectRecordGetDeniedWithoutGrant mirrors the project-delete
// denial: ExecuteTool refuses project-record-get in-process without a resolved
// superadmin_full platform grant.
func TestInProcessProjectRecordGetDeniedWithoutGrant(t *testing.T) {
	svc := &Service{} // db nil → grant unresolvable → fail-closed
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: uuid.New().String(), Scopes: []string{"admin:all"}})

	_, err := svc.ExecuteTool(ctx, "", "project-record-get", map[string]any{"project_id": "p1"})
	require.Error(t, err, "in-process project-record-get without a grant must be denied")
	require.Contains(t, err.Error(), "superadmin", "denial must come from the superadmin gate")
}

// TestSuperadminPaginationClamped proves page/limit are clamped to the
// superadmin handler convention (page >= 1, limit in [1..100], default 20).
func TestSuperadminPaginationClamped(t *testing.T) {
	require.Equal(t, 1, superadminArgPage(map[string]any{"page": -5}))
	require.Equal(t, 1, superadminArgPage(map[string]any{}))
	require.Equal(t, 3, superadminArgPage(map[string]any{"page": 3}))

	require.Equal(t, 1, superadminArgLimit(map[string]any{"limit": -1}))
	require.Equal(t, 20, superadminArgLimit(map[string]any{}))
	require.Equal(t, 100, superadminArgLimit(map[string]any{"limit": 500}))
	require.Equal(t, 50, superadminArgLimit(map[string]any{"limit": 50}))
}
