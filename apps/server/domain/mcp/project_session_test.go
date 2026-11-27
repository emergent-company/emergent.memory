package mcp

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func TestProjectSessionAction(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		args   map[string]any
		want   string
		wantOK bool
	}{
		{name: "family list", tool: "project", args: map[string]any{"action": "list"}, want: "list", wantOK: true},
		{name: "family use", tool: "project", args: map[string]any{"action": "use"}, want: "use", wantOK: true},
		{name: "legacy list alias", tool: "project-list", want: "list", wantOK: true},
		{name: "legacy use alias", tool: "project-use", want: "use", wantOK: true},
		{name: "family get is not session control", tool: "project", args: map[string]any{"action": "get"}},
		{name: "legacy get alias is not session control", tool: "project-get"},
		{name: "family without action", tool: "project"},
		{name: "other family action", tool: "entity", args: map[string]any{"action": "use"}},
		{name: "other tool", tool: "entity-query", args: map[string]any{"action": "list"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := projectSessionAction(tc.tool, tc.args)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestProjectUseProjectID(t *testing.T) {
	const id = "11111111-1111-1111-1111-111111111111"

	got, err := projectUseProjectID(map[string]any{"project_id": id})
	require.NoError(t, err)
	require.Equal(t, id, got)

	got, err = projectUseProjectID(map[string]any{"project_id": " " + id + " "})
	require.NoError(t, err)
	require.Equal(t, id, got, "surrounding whitespace must be trimmed")

	_, err = projectUseProjectID(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "project_id")

	_, err = projectUseProjectID(map[string]any{"project_id": "not-a-uuid"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "UUID")
}

func TestIsWriteScope(t *testing.T) {
	for _, scope := range []string{"graph:write", "documents:write", "schema:migrate", "admin", "project:admin"} {
		require.True(t, isWriteScope(scope), "%s must count as a write scope", scope)
	}
	for _, scope := range []string{"", "graph:read", "search", "documents:read", "projects:read", "chat:use"} {
		require.False(t, isWriteScope(scope), "%s must not count as a write scope", scope)
	}
}

func TestProjectSwitchRequired(t *testing.T) {
	svc := &Service{requireProjectSwitch: true}
	// The catalog is built lazily by GetToolByName.
	require.True(t, svc.ProjectSwitchRequired(context.Background(), "", false, "entity-write", map[string]any{"action": "create"}),
		"a write tool must require an explicit switch")
	require.False(t, svc.ProjectSwitchRequired(context.Background(), "", false, "no-such-tool", nil),
		"unknown tools are not blocked (dispatch reports them)")
	require.False(t, svc.ProjectSwitchRequired(context.Background(), "", false, "entity", map[string]any{"action": "query"}),
		"read tools stay available")
	require.False(t, svc.ProjectSwitchRequired(context.Background(), "", false, "project", map[string]any{"action": "use"}),
		"the switch action itself must never be blocked")
	require.False(t, svc.ProjectSwitchRequired(context.Background(), "", false, "project", map[string]any{"action": "list"}),
		"the list action must never be blocked")
	require.False(t, svc.ProjectSwitchRequired(context.Background(), "", false, "project-write", map[string]any{"action": "create"}),
		"creating a new project does not write into the session's project")
	require.False(t, svc.ProjectSwitchRequired(context.Background(), "", true, "entity-write", map[string]any{"action": "create"}),
		"an explicitly switched session may write")

	disabled := &Service{}
	require.False(t, disabled.ProjectSwitchRequired(context.Background(), "", false, "entity-write", map[string]any{"action": "create"}),
		"the gate is opt-in")
}

// expectMemberProjectRows sets the expectation for the membership-joined
// project query and returns the given (id, name, orgID, orgName, role) rows.
func expectMemberProjectRows(t *testing.T, mock sqlmock.Sqlmock, rows ...[]string) {
	t.Helper()
	result := sqlmock.NewRows([]string{"id", "name", "organization_id", "organization_name", "role"})
	for _, row := range rows {
		result.AddRow(row[0], row[1], row[2], row[3], row[4])
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT") + `[\s\S]*kb\.projects[\s\S]*kb\.organization_memberships[\s\S]*`).
		WillReturnRows(result)
}

func TestListMemberProjectsSessionUser(t *testing.T) {
	db, mock := newAuthzMockDB(t)
	svc := &Service{db: db}
	expectMemberProjectRows(t, mock,
		[]string{"proj-a", "Alpha", "org-1", "Org One", "org_admin"},
		[]string{"proj-b", "Beta", "org-1", "Org One", "org_member"},
	)

	user := &auth.AuthUser{ID: "u1"}
	projects, err := svc.ListMemberProjects(context.Background(), user, "proj-b")
	require.NoError(t, err)
	require.Len(t, projects, 2)
	require.Equal(t, "proj-a", projects[0].ID)
	require.False(t, projects[0].Active)
	require.Equal(t, "proj-b", projects[1].ID)
	require.True(t, projects[1].Active)
	require.Equal(t, "Org One", projects[1].OrganizationName)
	require.Equal(t, "org_member", projects[1].Role)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListMemberProjectsProjectBoundTokenIsLimitedToItsProject(t *testing.T) {
	db, mock := newAuthzMockDB(t)
	svc := &Service{db: db}
	expectOAuthGrant(t, mock, false)
	expectMemberProjectRows(t, mock,
		[]string{"proj-a", "Alpha", "org-1", "Org One", "org_admin"},
		[]string{"proj-b", "Beta", "org-1", "Org One", "org_member"},
	)

	user := &auth.AuthUser{ID: "u1", APITokenID: "tok1", APITokenProjectID: "proj-a"}
	projects, err := svc.ListMemberProjects(context.Background(), user, "proj-a")
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "proj-a", projects[0].ID)
	require.True(t, projects[0].Active)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListMemberProjectsOAuthTokenSeesEveryMembership(t *testing.T) {
	db, mock := newAuthzMockDB(t)
	svc := &Service{db: db}
	expectOAuthGrant(t, mock, true)
	expectMemberProjectRows(t, mock,
		[]string{"proj-a", "Alpha", "org-1", "Org One", "org_admin"},
		[]string{"proj-b", "Beta", "org-2", "Org Two", "org_member"},
	)

	user := &auth.AuthUser{ID: "u1", APITokenID: "tok1", APITokenProjectID: "proj-a"}
	projects, err := svc.ListMemberProjects(context.Background(), user, "proj-a")
	require.NoError(t, err)
	require.Len(t, projects, 2)
	require.True(t, projects[0].Active)
	require.Equal(t, "org-2", projects[1].OrganizationID)
	require.False(t, projects[1].Active)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListMemberProjectsWithoutIdentityIsUnauthorized(t *testing.T) {
	svc := &Service{}
	_, err := svc.ListMemberProjects(context.Background(), nil, "")
	require.Error(t, err)

	_, err = svc.ListMemberProjects(context.Background(), &auth.AuthUser{}, "")
	require.Error(t, err)
}

func TestExecuteListMemberProjectsEnvelope(t *testing.T) {
	db, mock := newAuthzMockDB(t)
	svc := &Service{db: db}
	expectMemberProjectRows(t, mock, []string{"proj-a", "Alpha", "org-1", "Org One", "org_admin"})

	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: "u1"})
	result, err := svc.executeListMemberProjects(ctx, "proj-a")
	require.NoError(t, err)
	require.NotNil(t, result)
	data, ok := result.StructuredContent["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "proj-a", data["activeProjectId"])
	require.Equal(t, 1, data["count"])
	require.NoError(t, mock.ExpectationsWereMet())
}
