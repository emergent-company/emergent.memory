package mcp

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// apperrorStatus extracts the HTTP status from an apperror.Error (0 if not one).
func apperrorStatus(err error) int {
	var e *apperror.Error
	if errors.As(err, &e) {
		return e.HTTPStatus
	}
	return 0
}

// newAuthzMockDB builds a *bun.DB over go-sqlmock (PostgreSQL dialect) with a
// regexp query matcher so the exact bun SQL is not pinned — expectations anchor
// on the table name each authorization query touches.
func newAuthzMockDB(t *testing.T) (*bun.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return bun.NewDB(sqldb, pgdialect.New()), mock
}

// expectProjectOrg sets the expectation for the kb.projects org lookup and
// returns the org the caller is (not) a member of via the membership query.
func expectProjectOrg(t *testing.T, mock sqlmock.Sqlmock, orgID string) {
	t.Helper()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT") + `[\s\S]*kb\.projects[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"organization_id"}).AddRow(orgID))
}

// expectMembership sets the expectation for the kb.organization_memberships
// existence check, returning isMember.
func expectMembership(t *testing.T, mock sqlmock.Sqlmock, isMember bool) {
	t.Helper()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT") + `[\s\S]*kb\.organization_memberships[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(isMember))
}

func TestAuthorizeProjectClaim(t *testing.T) {
	cases := []struct {
		name       string
		user       *auth.AuthUser
		projectID  string
		setup      func(t *testing.T, mock sqlmock.Sqlmock)
		wantErr    bool
		wantStatus int // 0 = no error
	}{
		{
			name:      "empty claim passes",
			user:      &auth.AuthUser{ID: "u1", Scopes: []string{"agents:read"}},
			projectID: "",
			wantErr:   false,
		},
		{
			name: "ownerless token passes",
			user: &auth.AuthUser{
				APITokenID: "tok1", ID: "", APITokenProjectID: "", Scopes: []string{"agents:write"},
			},
			projectID: "proj-foreign",
			wantErr:   false,
		},
		{
			name: "project-bound token claiming foreign project is forbidden",
			user: &auth.AuthUser{
				APITokenID: "tok1", ID: "u1", APITokenProjectID: "proj-own", Scopes: []string{"agents:write"},
			},
			projectID:  "proj-foreign",
			wantErr:    true,
			wantStatus: 403,
		},
		{
			name: "project-bound token claiming its own project passes",
			user: &auth.AuthUser{
				APITokenID: "tok1", ID: "u1", APITokenProjectID: "proj-own", Scopes: []string{"agents:write"},
			},
			projectID: "proj-own",
			wantErr:   false,
		},
		{
			name: "account token with owner, non-member, is forbidden",
			user: &auth.AuthUser{
				APITokenID: "tok1", ID: "u1", APITokenProjectID: "", Scopes: []string{"agents:write"},
			},
			projectID: "proj-victim",
			setup: func(t *testing.T, mock sqlmock.Sqlmock) {
				expectProjectOrg(t, mock, "org-victim")
				expectMembership(t, mock, false)
			},
			wantErr:    true,
			wantStatus: 403,
		},
		{
			name: "account token with owner, member, passes",
			user: &auth.AuthUser{
				APITokenID: "tok1", ID: "u1", APITokenProjectID: "", Scopes: []string{"agents:write"},
			},
			projectID: "proj-own",
			setup: func(t *testing.T, mock sqlmock.Sqlmock) {
				expectProjectOrg(t, mock, "org-own")
				expectMembership(t, mock, true)
			},
			wantErr: false,
		},
		{
			name: "session caller, non-member, is forbidden",
			user: &auth.AuthUser{
				ID: "u1", Scopes: []string{"agents:write"},
			},
			projectID: "proj-victim",
			setup: func(t *testing.T, mock sqlmock.Sqlmock) {
				expectProjectOrg(t, mock, "org-victim")
				expectMembership(t, mock, false)
			},
			wantErr:    true,
			wantStatus: 403,
		},
		{
			name: "session caller with no identity is unauthorized",
			user: &auth.AuthUser{
				ID: "", Scopes: []string{"agents:write"},
			},
			projectID:  "proj-victim",
			wantErr:    true,
			wantStatus: 401,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newAuthzMockDB(t)
			if tc.setup != nil {
				tc.setup(t, mock)
			}
			svc := &Service{db: db}

			err := svc.authorizeProjectClaim(context.Background(), tc.user, tc.projectID)

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantStatus != 0 {
					require.Equal(t, tc.wantStatus, apperrorStatus(err), "unexpected error status: %v", err)
				}
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
