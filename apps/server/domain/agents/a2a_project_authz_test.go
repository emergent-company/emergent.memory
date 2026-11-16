package agents

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

const (
	a2aMemberUserID     = "11111111-1111-1111-1111-111111111111"
	a2aMemberProjectID  = "3f2c0e5a-1234-5678-9abc-def012345678"
	a2aForeignProjectID = "8b1d2f44-aaaa-bbbb-cccc-0123456789ab"
	a2aUnknownProjectID = "00000000-0000-0000-0000-000000000000"
	a2aMemberOrgID      = "aaaaaaaa-0000-0000-0000-000000000001"
	a2aForeignOrgID     = "bbbbbbbb-0000-0000-0000-000000000002"
)

// Issue #660. The A2A surface addresses a project through the X-Project-ID
// header, which RequireAuth copies into user.ProjectID without verifying it.
// RequireProjectMembership (pkg/auth) is the handler-layer guard for exactly
// that shape; these tests pin that the A2A chokepoint calls it with the project
// the request addressed and surfaces its verdict as an A2A error.

// newA2AHandlerWithMockDB builds a handler whose repository runs on a sqlmock
// connection, so the guard's two lookups (the addressed project's owning org,
// then the caller's membership) are scripted per test. The handler is built by
// hand, leaving authorizeProject nil: that is the default path a handler without
// an injected hook takes, and it must resolve the real guard.
func newA2AHandlerWithMockDB(t *testing.T) (*A2AHandler, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() {
		// Every expectation a test sets must be consumed: an unconsumed one means
		// the guard took a path the test did not intend.
		require.NoError(t, mock.ExpectationsWereMet())
		_ = sqlDB.Close()
	})

	repo := NewRepository(bun.NewDB(sqlDB, pgdialect.New()))
	return &A2AHandler{log: slog.Default(), repo: repo}, mock
}

// expectProjectOrg scripts the addressed project's owning-organization lookup.
// bun renders the project id as a quoted literal for this select, so the value
// is part of the statement the mock sees rather than a bound argument.
func expectProjectOrg(mock sqlmock.Sqlmock, projectID, orgID string) {
	mock.ExpectQuery(`SELECT .*organization_id.* FROM kb\.projects WHERE \(id = '` + projectID + `'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"organization_id"}).AddRow(orgID))
}

// expectMembership scripts the caller's membership probe for an org. Like the
// project lookup, bun renders the ids as quoted literals.
func expectMembership(mock sqlmock.Sqlmock, orgID, userID string, member bool) {
	mock.ExpectQuery(`SELECT EXISTS` +
		`.*organization_id = '` + orgID + `'.*user_id = '` + userID + `'`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(member))
}

// newA2AContextWithUser builds an Echo context carrying an authenticated user
// and an X-Project-ID header, the two inputs the guard reads. The user is
// stamped on both the Echo context (what handlers read) and the request context
// (what RequireProjectMembership reads), mirroring what RequireAuth does.
func newA2AContextWithUser(user *auth.AuthUser, headerProjectID string) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	if headerProjectID != "" {
		req.Header.Set("X-Project-ID", headerProjectID)
	}
	c := e.NewContext(req, httptest.NewRecorder())
	if user != nil {
		c.Set(string(auth.UserContextKey), user)
		c.SetRequest(req.WithContext(auth.InjectAuthContext(req.Context(), user)))
	}
	return c
}

func TestAddressedProject_NoSelectorIsProjectRequired(t *testing.T) {
	h, mock := newA2AHandlerWithMockDB(t)
	c := newA2AContextWithUser(&auth.AuthUser{ID: a2aMemberUserID}, "")

	_, err := h.addressedProject(c)
	require.Error(t, err)

	a2aErr := a2aErrorFrom(err)
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusBadRequest, a2aErr.Code.HTTPStatus())
	assert.Equal(t, A2AReasonProjectRequired, a2aErr.Reason)
	// No project to authorize, so no query may run.
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddressedProject_UnauthenticatedIsUnauthorized(t *testing.T) {
	h, mock := newA2AHandlerWithMockDB(t)
	c := newA2AContextWithUser(nil, a2aMemberProjectID)

	_, err := h.addressedProject(c)
	require.Error(t, err)
	assert.ErrorIs(t, err, apperror.ErrUnauthorized)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestAddressedProject_MemberOfAddressedProjectPasses is the positive control:
// a member of the project named in the header is allowed through and is handed
// that project.
func TestAddressedProject_MemberOfAddressedProjectPasses(t *testing.T) {
	h, mock := newA2AHandlerWithMockDB(t)
	expectProjectOrg(mock, a2aMemberProjectID, a2aMemberOrgID)
	expectMembership(mock, a2aMemberOrgID, a2aMemberUserID, true)

	c := newA2AContextWithUser(&auth.AuthUser{ID: a2aMemberUserID, ProjectID: a2aMemberProjectID}, a2aMemberProjectID)

	got, err := h.addressedProject(c)
	require.NoError(t, err)
	assert.Equal(t, a2aMemberProjectID, got)
}

// TestAddressedProject_NonMemberIsForbidden is the #660 regression guard: the
// caller is a real user, the project exists, and the caller is simply not a
// member of its owning org.
func TestAddressedProject_NonMemberIsForbidden(t *testing.T) {
	h, mock := newA2AHandlerWithMockDB(t)
	expectProjectOrg(mock, a2aForeignProjectID, a2aForeignOrgID)
	expectMembership(mock, a2aForeignOrgID, a2aMemberUserID, false)

	// The user is a member of project A and addresses project B by header: the
	// exact cross-tenant shape the issue reports.
	c := newA2AContextWithUser(&auth.AuthUser{ID: a2aMemberUserID, ProjectID: a2aForeignProjectID}, a2aForeignProjectID)

	_, err := h.addressedProject(c)
	require.Error(t, err)

	a2aErr := a2aErrorFrom(err)
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusForbidden, a2aErr.Code.HTTPStatus())
	assert.Equal(t, A2AReasonPermissionDenied, a2aErr.Reason)
}

// TestAddressedProject_UnknownProjectIsNotFound keeps the surface free of a
// project-existence oracle: a project that does not exist is a 404, not a 403
// and never a 500.
func TestAddressedProject_UnknownProjectIsNotFound(t *testing.T) {
	h, mock := newA2AHandlerWithMockDB(t)
	mock.ExpectQuery(`SELECT .*organization_id.* FROM kb\.projects WHERE \(id = '` + a2aUnknownProjectID + `'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"organization_id"})) // no rows

	c := newA2AContextWithUser(&auth.AuthUser{ID: a2aMemberUserID, ProjectID: a2aUnknownProjectID}, a2aUnknownProjectID)

	_, err := h.addressedProject(c)
	require.Error(t, err)

	a2aErr := a2aErrorFrom(err)
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusNotFound, a2aErr.Code.HTTPStatus())
	assert.Equal(t, A2AReasonNotFound, a2aErr.Reason)
}

// TestAddressedProject_MalformedProjectIsNotFound: the header is client input,
// so a non-uuid value must not reach the uuid column (which would surface as a
// 500) and must not be treated as any project.
func TestAddressedProject_MalformedProjectIsNotFound(t *testing.T) {
	h, mock := newA2AHandlerWithMockDB(t)
	c := newA2AContextWithUser(&auth.AuthUser{ID: a2aMemberUserID, ProjectID: "not-a-uuid"}, "not-a-uuid")

	_, err := h.addressedProject(c)
	require.Error(t, err)

	a2aErr := a2aErrorFrom(err)
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusNotFound, a2aErr.Code.HTTPStatus())
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestAddressedProject_ProjectBoundTokenNeedsNoMembershipLookup pins the bounded
// class the issue calls safe: an emt_* token bound to a project at mint time
// addresses that project whatever the header says, and the guard accepts it
// without a membership query. The nil repository is deliberate — a query attempt
// would fail, so a pass proves none ran.
func TestAddressedProject_ProjectBoundTokenNeedsNoMembershipLookup(t *testing.T) {
	h := &A2AHandler{log: slog.Default()} // no repository at all

	c := newA2AContextWithUser(&auth.AuthUser{
		ID:                a2aMemberUserID,
		APITokenID:        "tok-1",
		APITokenProjectID: a2aMemberProjectID,
		ProjectID:         a2aMemberProjectID, // RequireAuth normalizes the header
	}, a2aForeignProjectID) // the header names a *different* project

	got, err := h.addressedProject(c)
	require.NoError(t, err)
	assert.Equal(t, a2aMemberProjectID, got, "the token's project must win over the header")
}

// TestRequireProjectMembershipTokenBinding is a direct assertion of the branch
// the A2A guard relies on: a project-bound token addressing another project is
// a 403 with no database access.
func TestRequireProjectMembershipTokenBinding(t *testing.T) {
	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{
		ID:                a2aMemberUserID,
		APITokenID:        "tok-1",
		APITokenProjectID: a2aMemberProjectID,
	})

	err := auth.RequireProjectMembership(ctx, nil, a2aForeignProjectID)
	require.Error(t, err)

	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

	// Its own project is accepted without touching the nil database handle.
	require.NoError(t, auth.RequireProjectMembership(ctx, nil, a2aMemberProjectID))
}

// TestNewA2AHandlerWiresTheGuard proves the production constructor installs the
// real membership check rather than leaving the hook nil.
func TestNewA2AHandlerWiresTheGuard(t *testing.T) {
	h := NewA2AHandler(NewRepository(nil), nil, nil, slog.Default(), nil)
	require.NotNil(t, h.authorizeProject, "NewA2AHandler must wire the project-membership guard")

	ctx := auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: a2aMemberUserID})
	// A nil DB is a missing-database error, i.e. the real guard ran and could not
	// be satisfied — never a silent pass.
	require.Error(t, h.authorizeProject(ctx, a2aMemberProjectID))
}

// TestAddressedProject_NilDBFailsClosed: without a database the guard cannot
// prove membership, so it denies rather than allowing the request.
func TestAddressedProject_NilDBFailsClosed(t *testing.T) {
	h := &A2AHandler{log: slog.Default()}
	c := newA2AContextWithUser(&auth.AuthUser{ID: a2aMemberUserID, ProjectID: a2aMemberProjectID}, a2aMemberProjectID)

	_, err := h.addressedProject(c)
	require.Error(t, err)

	a2aErr := a2aErrorFrom(err)
	require.NotNil(t, a2aErr)
	assert.Equal(t, http.StatusInternalServerError, a2aErr.Code.HTTPStatus())
}
