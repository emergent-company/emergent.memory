package graph_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// setupWorkPickupEventTest wires a real events.Service (with a capturing
// subscriber) into the graph service so tests can assert post-commit emission
// of the work-pickup signals (status_changed / assigned).
func setupWorkPickupEventTest(t *testing.T) (context.Context, *graph.Service, uuid.UUID, chan events.EntityEvent) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_work_pickup_events")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Work Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Work Project",
	}, testutil.AdminUser.ID))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)

	eventsSvc := events.NewService(log)
	eventsCh := make(chan events.EntityEvent, 32)
	eventsSvc.Subscribe("*", func(ev events.EntityEvent) {
		select {
		case eventsCh <- ev:
		default:
		}
	})

	svc := graph.NewService(repo, log, &fakeSchemaProvider{objectSchemas: map[string]agents.ObjectSchema{
		"ResearchRequest": {ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{BoardEnabled: true}},
	}}, nil, nil, nil, nil, graph.NoopEventSink{}, nil, eventsSvc)

	return ctx, svc, uuid.MustParse(projectID), eventsCh
}

// waitForEvent consumes events until one of the requested type arrives,
// discarding any other types (e.g. the create event emitted during setup).
func waitForEvent(t *testing.T, ch chan events.EntityEvent, typ events.EntityEventType) events.EntityEvent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %s", typ)
		}
	}
}

// drainEvents consumes everything currently in the channel (with a short grace
// period for in-flight goroutine deliveries).
func drainEvents(ch chan events.EntityEvent) {
	for {
		select {
		case <-ch:
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

// assertNoEvent fails if an event of the requested type arrives within a short
// window. Non-matching events are ignored.
func assertNoEvent(t *testing.T, ch chan events.EntityEvent, typ events.EntityEventType) {
	t.Helper()
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case ev := <-ch:
			if ev.Type == typ {
				t.Fatalf("unexpected event %s: %+v", typ, ev)
			}
		case <-deadline:
			return
		}
	}
}

// TestTransitionWorkObject_EmitsStatusChangedAfterCommit verifies a status move
// emits a status_changed event carrying the new status, previous status, and
// assignee — after the transaction commits, not inside it.
func TestTransitionWorkObject_EmitsStatusChangedAfterCommit(t *testing.T) {
	ctx, svc, projectID, ch := setupWorkPickupEventTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "emit-sc", "todo")

	ok, err := svc.TransitionWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), graph.WorkObjectTransition{
		FromStatus: "todo",
		ToStatus:   "in_progress",
	})
	require.NoError(t, err)
	require.True(t, ok)

	ev := waitForEvent(t, ch, events.EventTypeStatusChanged)
	require.Equal(t, "in_progress", ev.Data["status"])
	require.Equal(t, "todo", ev.Data["previousStatus"])
	require.Equal(t, "researcher", ev.Data["assignee"])
	require.NotNil(t, ev.Version)
}

// TestReassignWorkObject_EmitsAssignedAfterCommit verifies a status-preserving
// reassign emits an assigned event (not status_changed) carrying the assignee
// and the preserved status.
func TestReassignWorkObject_EmitsAssignedAfterCommit(t *testing.T) {
	ctx, svc, projectID, ch := setupWorkPickupEventTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "emit-assign", "in_progress")

	newAssignee := "reviewer"
	ok, err := svc.ReassignWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), &newAssignee)
	require.NoError(t, err)
	require.True(t, ok)

	ev := waitForEvent(t, ch, events.EventTypeAssigned)
	require.Equal(t, "in_progress", ev.Data["status"])
	require.Equal(t, "reviewer", ev.Data["assignee"])
}

// TestTransitionWorkObject_LostCasEmitsNoEvent verifies that when the from-status
// CAS loses (the HEAD already moved), the transition returns false and emits no
// status_changed event.
func TestTransitionWorkObject_LostCasEmitsNoEvent(t *testing.T) {
	ctx, svc, projectID, ch := setupWorkPickupEventTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "lost-cas", "todo")

	ok, err := svc.TransitionWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), graph.WorkObjectTransition{
		FromStatus: "todo",
		ToStatus:   "in_progress",
	})
	require.NoError(t, err)
	require.True(t, ok)
	waitForEvent(t, ch, events.EventTypeStatusChanged)
	drainEvents(ch)

	// HEAD is now "in_progress": a second "todo"-based claim loses the CAS.
	ok, err = svc.TransitionWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), graph.WorkObjectTransition{
		FromStatus: "todo",
		ToStatus:   "done",
	})
	require.NoError(t, err)
	require.False(t, ok)

	assertNoEvent(t, ch, events.EventTypeStatusChanged)
}

// TestTransitionWorkObject_SameStatusNoEvent verifies a transition that does not
// actually move the status emits no status_changed event (the emit is gated on
// status change, so a status-preserving transition stays silent).
func TestTransitionWorkObject_SameStatusNoEvent(t *testing.T) {
	ctx, svc, projectID, ch := setupWorkPickupEventTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "same-status", "in_progress")

	waitForEvent(t, ch, events.EventTypeCreated)
	drainEvents(ch)

	ok, err := svc.TransitionWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), graph.WorkObjectTransition{
		FromStatus: "in_progress",
		ToStatus:   "in_progress",
	})
	require.NoError(t, err)
	require.True(t, ok)

	assertNoEvent(t, ch, events.EventTypeStatusChanged)
}
