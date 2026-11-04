package invites

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/domain/notifications"
)

type fakeNotifier struct {
	calls []notifications.CreateInput
}

func (f *fakeNotifier) Create(_ context.Context, in notifications.CreateInput) (*notifications.Notification, error) {
	f.calls = append(f.calls, in)
	return &notifications.Notification{ID: "n-1"}, nil
}

func testInviteLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEmitMembershipGranted_ProjectInvite(t *testing.T) {
	n := &fakeNotifier{}
	pid := "proj-1"
	svc := &Service{notificationsSvc: n, log: testInviteLog()}

	svc.emitMembershipGranted(context.Background(), "user-1", &Invite{ProjectID: &pid, OrganizationID: "org-1"})

	require.Len(t, n.calls, 1)
	in := n.calls[0]
	require.Equal(t, "project.member.added", in.EventKey)
	require.Equal(t, notifications.ScopeAccount, in.Scope)
	require.Equal(t, "user-1", in.UserID)
	require.NotNil(t, in.ProjectID)
	require.Equal(t, "proj-1", *in.ProjectID)
}

func TestEmitMembershipGranted_OrgInvite(t *testing.T) {
	n := &fakeNotifier{}
	svc := &Service{notificationsSvc: n, log: testInviteLog()}

	svc.emitMembershipGranted(context.Background(), "user-1", &Invite{OrganizationID: "org-1"})

	require.Len(t, n.calls, 1)
	in := n.calls[0]
	require.Equal(t, "user.access.granted", in.EventKey)
	require.Equal(t, notifications.ScopeAccount, in.Scope)
	require.Nil(t, in.ProjectID)
}

func TestEmitInviteReceived(t *testing.T) {
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, pgdialect.New())

	mock.ExpectQuery(regexp.QuoteMeta("SELECT") + `[\s\S]*core\.user_emails[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("target-1"))

	n := &fakeNotifier{}
	svc := &Service{db: db, baseURL: "https://app.example", notificationsSvc: n, log: testInviteLog()}

	invite := &Invite{
		ID:        "inv-1",
		Email:     "target@example.com",
		Token:     "tok123",
		ProjectID: nil,
	}
	svc.emitInviteReceived(context.Background(), invite, "ACME")

	require.Len(t, n.calls, 1)
	in := n.calls[0]
	require.Equal(t, "invite.received", in.EventKey)
	require.Equal(t, notifications.ScopeAccount, in.Scope)
	require.True(t, in.RequiresAction)
	require.Equal(t, "target-1", in.UserID)
	require.NotNil(t, in.ActionURL)
	require.Contains(t, *in.ActionURL, "/invites/accept?token=tok123")
	require.NotEmpty(t, in.Actions)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEmitInviteReceived_UnknownEmailSkips(t *testing.T) {
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, pgdialect.New())

	// No matching email → empty result set → sql.ErrNoRows → resolve returns "".
	mock.ExpectQuery(regexp.QuoteMeta("SELECT") + `[\s\S]*core\.user_emails[\s\S]*`).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}))

	n := &fakeNotifier{}
	svc := &Service{db: db, baseURL: "https://app.example", notificationsSvc: n, log: testInviteLog()}

	svc.emitInviteReceived(context.Background(), &Invite{ID: "inv-1", Email: "ghost@example.com", Token: "tok"}, "ACME")

	require.Len(t, n.calls, 0)
	require.NoError(t, mock.ExpectationsWereMet())
}
