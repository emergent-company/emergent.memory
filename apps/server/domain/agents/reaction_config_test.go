package agents

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/domain/events"
)

func boolPtr(b bool) *bool { return &b }

// ---------- shouldTriggerAgentForActor (pure) ----------

func TestShouldTriggerAgentForActor(t *testing.T) {
	agentWithDef := func(rc *ReactionConfig) *Agent {
		a := makeTestAgent("a1", "reaction", "p1", rc)
		a.AgentDefinitionID = testStrPtr("def-self")
		return a
	}
	otherAgent := &events.ActorContext{ActorType: events.ActorAgent, ActorID: "def-other"}
	selfAgent := &events.ActorContext{ActorType: events.ActorAgent, ActorID: "def-self"}
	userActor := &events.ActorContext{ActorType: events.ActorUser, ActorID: "user-1"}
	systemActor := &events.ActorContext{ActorType: events.ActorSystem}

	base := &ReactionConfig{ObjectTypes: []string{"Person"}, Events: []ReactionEventType{EventTypeCreated}}

	tests := []struct {
		name  string
		agent *Agent
		actor *events.ActorContext
		want  bool
	}{
		{"nil actor always triggers", agentWithDef(base), nil, true},
		{"user actor always triggers", agentWithDef(base), userActor, true},
		{"system actor always triggers", agentWithDef(base), systemActor, true},
		{"nil reaction config ignores agent actor", makeTestAgent("a1", "r", "p1", nil), otherAgent, false},
		{"unset ignoreAgentTriggered ignores agent actor", agentWithDef(base), otherAgent, false},
		{"ignoreAgentTriggered true ignores agent actor", agentWithDef(&ReactionConfig{ObjectTypes: base.ObjectTypes, Events: base.Events, IgnoreAgentTriggered: boolPtr(true)}), otherAgent, false},
		{"ignoreAgentTriggered false allows other agent", agentWithDef(&ReactionConfig{ObjectTypes: base.ObjectTypes, Events: base.Events, IgnoreAgentTriggered: boolPtr(false)}), otherAgent, true},
		{"self trigger default self flag nil is skipped", agentWithDef(&ReactionConfig{ObjectTypes: base.ObjectTypes, Events: base.Events, IgnoreAgentTriggered: boolPtr(false)}), selfAgent, false},
		{"self trigger ignoreSelfTriggered true is skipped", agentWithDef(&ReactionConfig{ObjectTypes: base.ObjectTypes, Events: base.Events, IgnoreAgentTriggered: boolPtr(false), IgnoreSelfTriggered: boolPtr(true)}), selfAgent, false},
		{"self trigger both explicit false is allowed", agentWithDef(&ReactionConfig{ObjectTypes: base.ObjectTypes, Events: base.Events, IgnoreAgentTriggered: boolPtr(false), IgnoreSelfTriggered: boolPtr(false)}), selfAgent, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldTriggerAgentForActor(tt.agent, tt.actor))
		})
	}
}

func testStrPtr(s string) *string { return &s }

// ---------- HandleEvent actor gate (integration of matching + dispatch) ----------

type dispatchProbe struct {
	ch chan [3]string // agentID, objectType, objectID
}

func newDispatchProbe(ts *TriggerService) *dispatchProbe {
	p := &dispatchProbe{ch: make(chan [3]string, 16)}
	ts.dispatch = func(_ context.Context, agent *Agent, _ string, objectType, objectID string) error {
		p.ch <- [3]string{agent.ID, objectType, objectID}
		return nil
	}
	return p
}

func (p *dispatchProbe) expectDispatch(t *testing.T) [3]string {
	t.Helper()
	select {
	case d := <-p.ch:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("expected a dispatch, got none")
		return [3]string{}
	}
}

func (p *dispatchProbe) expectNoDispatch(t *testing.T) {
	t.Helper()
	select {
	case d := <-p.ch:
		t.Fatalf("expected no dispatch, got %v", d)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestHandleEvent_AgentOriginated_DefaultIgnored(t *testing.T) {
	ts := newTestTriggerService()
	probe := newDispatchProbe(ts)
	ts.registerEventTrigger(makeTestAgent("a1", "r", "p1", &ReactionConfig{
		ObjectTypes: []string{"Person"}, Events: []ReactionEventType{EventTypeCreated},
	}))

	ts.handleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"},
		&events.ActorContext{ActorType: events.ActorAgent, ActorID: "def-other"})

	probe.expectNoDispatch(t)
}

func TestHandleEvent_AgentOriginated_OptInTriggers(t *testing.T) {
	ts := newTestTriggerService()
	probe := newDispatchProbe(ts)
	agent := makeTestAgent("a1", "r", "p1", &ReactionConfig{
		ObjectTypes: []string{"Person"}, Events: []ReactionEventType{EventTypeCreated},
		IgnoreAgentTriggered: boolPtr(false),
	})
	agent.AgentDefinitionID = testStrPtr("def-self")
	ts.registerEventTrigger(agent)

	ts.handleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"},
		&events.ActorContext{ActorType: events.ActorAgent, ActorID: "def-other"})

	d := probe.expectDispatch(t)
	assert.Equal(t, [3]string{"a1", "Person", "obj-1"}, d)
}

func TestHandleEvent_SelfTrigger_SkippedByDefault(t *testing.T) {
	ts := newTestTriggerService()
	probe := newDispatchProbe(ts)
	agent := makeTestAgent("a1", "r", "p1", &ReactionConfig{
		ObjectTypes: []string{"Person"}, Events: []ReactionEventType{EventTypeCreated},
		IgnoreAgentTriggered: boolPtr(false),
	})
	agent.AgentDefinitionID = testStrPtr("def-self")
	ts.registerEventTrigger(agent)

	ts.handleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"},
		&events.ActorContext{ActorType: events.ActorAgent, ActorID: "def-self"})

	probe.expectNoDispatch(t)
}

func TestHandleEvent_SelfTrigger_AllowedWhenBothOptIn(t *testing.T) {
	ts := newTestTriggerService()
	probe := newDispatchProbe(ts)
	agent := makeTestAgent("a1", "r", "p1", &ReactionConfig{
		ObjectTypes: []string{"Person"}, Events: []ReactionEventType{EventTypeCreated},
		IgnoreAgentTriggered: boolPtr(false),
		IgnoreSelfTriggered:  boolPtr(false),
	})
	agent.AgentDefinitionID = testStrPtr("def-self")
	ts.registerEventTrigger(agent)

	ts.handleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"},
		&events.ActorContext{ActorType: events.ActorAgent, ActorID: "def-self"})

	d := probe.expectDispatch(t)
	assert.Equal(t, [3]string{"a1", "Person", "obj-1"}, d)
}

func TestHandleEvent_UserEvent_StillTriggers(t *testing.T) {
	ts := newTestTriggerService()
	probe := newDispatchProbe(ts)
	ts.registerEventTrigger(makeTestAgent("a1", "r", "p1", &ReactionConfig{
		ObjectTypes: []string{"Person"}, Events: []ReactionEventType{EventTypeCreated},
	}))

	ts.handleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"},
		&events.ActorContext{ActorType: events.ActorUser, ActorID: "user-1"})

	probe.expectDispatch(t)
}

func TestHandleEvent_NilActor_StillTriggers(t *testing.T) {
	ts := newTestTriggerService()
	probe := newDispatchProbe(ts)
	ts.registerEventTrigger(makeTestAgent("a1", "r", "p1", &ReactionConfig{
		ObjectTypes: []string{"Person"}, Events: []ReactionEventType{EventTypeCreated},
	}))

	ts.HandleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"})

	probe.expectDispatch(t)
}

// ---------- ConcurrencyStrategy ----------

type fakeActiveRuns struct {
	active     bool
	err        error
	calls      int
	lastAgent  string
	lastObject string
	lastType   string
}

func (f *fakeActiveRuns) HasActiveRunForAgentObject(_ context.Context, agentID, objectID, objectType string) (bool, error) {
	f.calls++
	f.lastAgent = agentID
	f.lastObject = objectID
	f.lastType = objectType
	return f.active, f.err
}

func newConcurrencyService(strategy ConcurrencyStrategy, active bool) (*TriggerService, *dispatchProbe, *fakeActiveRuns) {
	ts := newTestTriggerService()
	probe := newDispatchProbe(ts)
	fake := &fakeActiveRuns{active: active}
	ts.activeRuns = fake
	ts.registerEventTrigger(makeTestAgent("a1", "r", "p1", &ReactionConfig{
		ObjectTypes: []string{"Person"}, Events: []ReactionEventType{EventTypeCreated},
		ConcurrencyStrategy: strategy,
	}))
	return ts, probe, fake
}

func TestHandleEvent_ConcurrencySkip_ActiveRunSkips(t *testing.T) {
	ts, probe, fake := newConcurrencyService(ConcurrencySkip, true)

	ts.HandleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"})

	probe.expectNoDispatch(t)
	require.Equal(t, 1, fake.calls)
	assert.Equal(t, "a1", fake.lastAgent)
	assert.Equal(t, "obj-1", fake.lastObject)
	assert.Equal(t, "Person", fake.lastType)
}

func TestHandleEvent_ConcurrencySkip_NoActiveRunDispatches(t *testing.T) {
	ts, probe, fake := newConcurrencyService(ConcurrencySkip, false)

	ts.HandleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"})

	probe.expectDispatch(t)
	require.Equal(t, 1, fake.calls)
}

func TestHandleEvent_ConcurrencyEmpty_BehavesAsParallel(t *testing.T) {
	ts, probe, fake := newConcurrencyService("", true)

	ts.HandleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"})

	probe.expectDispatch(t)
	assert.Zero(t, fake.calls, "empty strategy must not consult the active-run checker")
}

func TestHandleEvent_ConcurrencyParallel_DoesNotSkip(t *testing.T) {
	ts, probe, fake := newConcurrencyService(ConcurrencyParallel, true)

	ts.HandleEvent(context.Background(), "Person", EventTypeCreated, "p1", map[string]any{"id": "obj-1"})

	probe.expectDispatch(t)
	assert.Zero(t, fake.calls, "parallel strategy must not consult the active-run checker")
}

// ---------- Repository query ----------

func TestHasActiveRunForAgentObject_QueryShape(t *testing.T) {
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	repo := NewRepository(bun.NewDB(sqldb, pgdialect.New()))

	mock.ExpectQuery(`(?s)SELECT EXISTS.*kb\.agent_runs.*subjectObjectId.*subjectObjectType`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	active, err := repo.HasActiveRunForAgentObject(context.Background(), "a1", "obj-1", "Person")
	require.NoError(t, err)
	assert.True(t, active)
	require.NoError(t, mock.ExpectationsWereMet())
}
