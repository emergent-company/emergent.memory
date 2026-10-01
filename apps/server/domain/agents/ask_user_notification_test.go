package agents

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/notifications"
)

// fakeNotifier records Create calls for assertions without touching a DB.
type fakeNotifier struct {
	calls []notifications.CreateInput
}

func (f *fakeNotifier) Create(_ context.Context, in notifications.CreateInput) (*notifications.Notification, error) {
	f.calls = append(f.calls, in)
	return &notifications.Notification{ID: "n-1"}, nil
}

func TestCreateQuestionNotification_UsesCentralProducer(t *testing.T) {
	n := &fakeNotifier{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := AskUserToolDeps{
		Logger:           log,
		ProjectID:        "proj-1",
		RunID:            "run-1",
		UserID:           "user-1",
		NotificationsSvc: n,
	}

	q := &AgentQuestion{
		ID:       "q-1",
		Question: "Approve this?",
		Options:  []AgentQuestionOption{{Label: "Yes", Value: "yes"}},
	}

	id := createQuestionNotificationDirect(context.Background(), deps, q)
	require.Equal(t, "n-1", id)
	require.Len(t, n.calls, 1)

	in := n.calls[0]
	require.Equal(t, "agent.question", in.EventKey)
	require.Equal(t, notifications.ScopeProject, in.Scope)
	require.True(t, in.RequiresAction)
	require.Equal(t, "user-1", in.UserID)
	require.NotNil(t, in.ProjectID)
	require.Equal(t, "proj-1", *in.ProjectID)
	require.NotNil(t, in.Type)
	require.Equal(t, "agent_question", *in.Type)
	require.Equal(t, "important", in.Importance)
	require.NotNil(t, in.RelatedResourceID)
	require.Equal(t, "q-1", *in.RelatedResourceID)
}

func TestCreateQuestionNotification_NilServiceSkips(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := AskUserToolDeps{Logger: log, ProjectID: "p", UserID: "u"}
	q := &AgentQuestion{ID: "q-1", Question: "hi"}

	require.Equal(t, "", createQuestionNotificationDirect(context.Background(), deps, q))
}
