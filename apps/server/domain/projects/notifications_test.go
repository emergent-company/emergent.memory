package projects

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/notifications"
)

type fakeNotifier struct {
	calls []notifications.CreateInput
}

func (f *fakeNotifier) Create(_ context.Context, in notifications.CreateInput) (*notifications.Notification, error) {
	f.calls = append(f.calls, in)
	return &notifications.Notification{ID: "n-1"}, nil
}

func TestEmitAccountNotification_MemberRemoved(t *testing.T) {
	n := &fakeNotifier{}
	svc := &Service{notificationsSvc: n, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	svc.emitAccountNotification(context.Background(), "user-1", "proj-1", "project.member.removed", "membership", "Removed from project", "You were removed.")

	require.Len(t, n.calls, 1)
	in := n.calls[0]
	require.Equal(t, "project.member.removed", in.EventKey)
	require.Equal(t, notifications.ScopeAccount, in.Scope)
	require.Equal(t, "user-1", in.UserID)
	require.NotNil(t, in.ProjectID)
	require.Equal(t, "proj-1", *in.ProjectID)
	require.NotNil(t, in.Category)
	require.Equal(t, "membership", *in.Category)
}

func TestEmitAccountNotification_NilServiceNoOp(t *testing.T) {
	svc := &Service{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// must not panic
	svc.emitAccountNotification(context.Background(), "u", "p", "user.role.changed", "permissions", "Role changed", "msg")
}
