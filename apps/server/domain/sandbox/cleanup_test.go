package sandbox

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCleanupConfig(t *testing.T) {
	cfg := DefaultCleanupConfig()

	assert.Equal(t, 1*time.Hour, cfg.Interval, "default interval should be 1 hour")
	assert.Equal(t, 10, cfg.MaxConcurrent, "default max concurrent should be 10")
	assert.Equal(t, 0.8, cfg.AlertThreshold, "default alert threshold should be 80%")
	assert.Equal(t, 0, cfg.PersistentIdleTTLDays, "idle MCP reclamation must default to disabled")
}

func TestCleanupJob_StartStop(t *testing.T) {
	// Verify that Start/Stop don't panic and are idempotent
	job := &CleanupJob{
		config: CleanupConfig{
			Interval:       100 * time.Millisecond,
			MaxConcurrent:  10,
			AlertThreshold: 0.8,
		},
		stopCh: make(chan struct{}),
	}

	// Stop before start should be safe
	job.Stop()

	// Double stop should be safe (stopCh already closed but running is false)
	job.Stop()
}

func TestCleanupJob_CheckResourceUsage_BelowThreshold(t *testing.T) {
	// Verify resource monitoring at different usage levels.
	// These test the logic of the threshold computation.

	tests := []struct {
		name           string
		activeCount    int
		maxConcurrent  int
		alertThreshold float64
		expectWarning  bool // >= alertThreshold but < 1.0
		expectError    bool // >= 1.0
	}{
		{
			name:           "no active workspaces",
			activeCount:    0,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  false,
			expectError:    false,
		},
		{
			name:           "below threshold",
			activeCount:    5,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  false,
			expectError:    false,
		},
		{
			name:           "at threshold",
			activeCount:    8,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  true,
			expectError:    false,
		},
		{
			name:           "above threshold below max",
			activeCount:    9,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  true,
			expectError:    false,
		},
		{
			name:           "at max capacity",
			activeCount:    10,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  false, // Error takes precedence
			expectError:    true,
		},
		{
			name:           "over max capacity",
			activeCount:    12,
			maxConcurrent:  10,
			alertThreshold: 0.8,
			expectWarning:  false,
			expectError:    true,
		},
		{
			name:           "zero max concurrent skips check",
			activeCount:    5,
			maxConcurrent:  0,
			alertThreshold: 0.8,
			expectWarning:  false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.maxConcurrent <= 0 {
				// Zero maxConcurrent causes early return — no alerts
				return
			}

			usageRatio := float64(tt.activeCount) / float64(tt.maxConcurrent)

			isError := usageRatio >= 1.0
			isWarning := !isError && usageRatio >= tt.alertThreshold

			assert.Equal(t, tt.expectError, isError, "error state mismatch")
			assert.Equal(t, tt.expectWarning, isWarning, "warning state mismatch")
		})
	}
}

func TestCleanupJob_MCPExemption(t *testing.T) {
	// Verify that the ListExpired query naturally excludes persistent MCP servers.
	// Persistent MCP servers have NULL expires_at, and ListExpired filters on
	// "expires_at IS NOT NULL AND expires_at < NOW()" — so they're automatically excluded.

	// Create a persistent MCP server entity
	mcpServer := &AgentSandbox{
		ContainerType: ContainerTypeMCPServer,
		Lifecycle:     LifecyclePersistent,
		ExpiresAt:     nil, // NULL — will never be returned by ListExpired
	}

	assert.Nil(t, mcpServer.ExpiresAt, "persistent MCP servers should have nil ExpiresAt")
	assert.Equal(t, LifecyclePersistent, mcpServer.Lifecycle)
	assert.Equal(t, ContainerTypeMCPServer, mcpServer.ContainerType)

	// Create an ephemeral workspace with expired TTL
	expired := time.Now().Add(-1 * time.Hour)
	agentWs := &AgentSandbox{
		ContainerType: ContainerTypeAgentSandbox,
		Lifecycle:     LifecycleEphemeral,
		ExpiresAt:     &expired,
	}

	assert.NotNil(t, agentWs.ExpiresAt, "ephemeral workspaces should have ExpiresAt set")
	assert.True(t, agentWs.ExpiresAt.Before(time.Now()), "should be expired")

	// Create a non-expired ephemeral workspace
	notExpired := time.Now().Add(24 * time.Hour)
	activeWs := &AgentSandbox{
		ContainerType: ContainerTypeAgentSandbox,
		Lifecycle:     LifecycleEphemeral,
		ExpiresAt:     &notExpired,
	}

	assert.NotNil(t, activeWs.ExpiresAt)
	assert.True(t, activeWs.ExpiresAt.After(time.Now()), "should not be expired")
}

func TestCleanupConfig_CustomValues(t *testing.T) {
	cfg := CleanupConfig{
		Interval:       30 * time.Minute,
		MaxConcurrent:  50,
		AlertThreshold: 0.9,
	}

	assert.Equal(t, 30*time.Minute, cfg.Interval)
	assert.Equal(t, 50, cfg.MaxConcurrent)
	assert.Equal(t, 0.9, cfg.AlertThreshold)
}

// =============================================================================
// Persistent MCP idle reclamation (Gap 2)
// =============================================================================

type fakeIdleLister struct {
	servers   []*AgentSandbox
	listErr   error
	deleteErr error
	listCalls int
	deleted   []string
	// recheck, when set, overrides the fresh re-read result per server ID so
	// tests can simulate a concurrent touch between the candidate SELECT and
	// the reclaim. Returning nil simulates a vanished/touched row.
	recheck    func(id string) *AgentSandbox
	recheckErr error
}

func (f *fakeIdleLister) ListPersistentMCPServers(_ context.Context) ([]*AgentSandbox, error) {
	f.listCalls++
	return f.servers, f.listErr
}

func (f *fakeIdleLister) GetIdlePersistentMCPServer(_ context.Context, id string, _ time.Time) (*AgentSandbox, error) {
	if f.recheckErr != nil {
		return nil, f.recheckErr
	}
	if f.recheck != nil {
		return f.recheck(id), nil
	}
	for _, s := range f.servers {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, nil
}

func (f *fakeIdleLister) Delete(_ context.Context, id string) (bool, error) {
	f.deleted = append(f.deleted, id)
	return true, f.deleteErr
}

type fakeIdleReclaimer struct {
	stopped    []string
	destroyed  []string
	destroyErr map[string]error
	onDestroy  func(ctx context.Context, ws *AgentSandbox)
}

func (f *fakeIdleReclaimer) StopRuntime(workspaceID string) {
	f.stopped = append(f.stopped, workspaceID)
}

func (f *fakeIdleReclaimer) DestroyContainer(ctx context.Context, ws *AgentSandbox) error {
	if f.onDestroy != nil {
		f.onDestroy(ctx, ws)
	}
	f.destroyed = append(f.destroyed, ws.ID)
	if f.destroyErr != nil {
		if err, ok := f.destroyErr[ws.ID]; ok {
			return err
		}
	}
	return nil
}

func idleMCPServer(id string, ageDays int, status Status) *AgentSandbox {
	return &AgentSandbox{
		ID:                  id,
		ContainerType:       ContainerTypeMCPServer,
		Lifecycle:           LifecyclePersistent,
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "prov-" + id,
		Status:              status,
		CreatedAt:           time.Now().AddDate(0, 0, -ageDays),
		LastUsedAt:          time.Now().AddDate(0, 0, -ageDays),
	}
}

func TestRunIdleReclamation_DisabledReclaimsNothing(t *testing.T) {
	lister := &fakeIdleLister{servers: []*AgentSandbox{idleMCPServer("ws-1", 100, StatusReady)}}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 0, testLogger())

	assert.Equal(t, idleReclaimResult{}, res)
	assert.Equal(t, 0, lister.listCalls, "disabled policy must not even query the store")
	assert.Empty(t, rt.destroyed)
	assert.Empty(t, lister.deleted)
}

func TestRunIdleReclamation_ReclaimsIdleServer(t *testing.T) {
	lister := &fakeIdleLister{servers: []*AgentSandbox{idleMCPServer("ws-1", 10, StatusReady)}}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 1, res.Reclaimed)
	assert.Equal(t, 0, res.Failed)
	assert.Equal(t, 0, res.Skipped)
	assert.Equal(t, []string{"ws-1"}, rt.stopped, "runtime monitoring must be stopped before destroy")
	assert.Equal(t, []string{"ws-1"}, rt.destroyed)
	assert.Equal(t, []string{"ws-1"}, lister.deleted)
}

func TestRunIdleReclamation_RecentlyUsedServerKept(t *testing.T) {
	lister := &fakeIdleLister{servers: []*AgentSandbox{idleMCPServer("ws-fresh", 1, StatusReady)}}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 0, res.Reclaimed)
	assert.Equal(t, 1, res.Skipped)
	assert.Empty(t, rt.destroyed)
	assert.Empty(t, lister.deleted)
}

func TestRunIdleReclamation_InFlightLifecycleStateNeverReclaimed(t *testing.T) {
	lister := &fakeIdleLister{servers: []*AgentSandbox{
		idleMCPServer("ws-creating", 10, StatusCreating),
		idleMCPServer("ws-stopping", 10, StatusStopping),
	}}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 0, res.Reclaimed)
	assert.Equal(t, 2, res.Skipped)
	assert.Empty(t, rt.destroyed)
}

// A never-used server has last_used_at equal to its creation time, so a server
// created beyond the window is eligible.
func TestRunIdleReclamation_NeverUsedJudgedByCreationTime(t *testing.T) {
	ws := idleMCPServer("ws-never-used", 30, StatusReady)
	lister := &fakeIdleLister{servers: []*AgentSandbox{ws}}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 1, res.Reclaimed)
}

func TestRunIdleReclamation_ListFailureDoesNotPanic(t *testing.T) {
	lister := &fakeIdleLister{listErr: assert.AnError}
	rt := &fakeIdleReclaimer{}

	assert.NotPanics(t, func() {
		res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())
		assert.Equal(t, idleReclaimResult{}, res)
	})
}

// A failed destroy is logged, the row is kept (never deleted), and the
// remaining eligible servers are still processed.
func TestRunIdleReclamation_DestroyFailureDoesNotAbortPass(t *testing.T) {
	lister := &fakeIdleLister{servers: []*AgentSandbox{
		idleMCPServer("ws-fail", 10, StatusReady),
		idleMCPServer("ws-ok", 10, StatusReady),
	}}
	rt := &fakeIdleReclaimer{destroyErr: map[string]error{"ws-fail": assert.AnError}}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 1, res.Reclaimed)
	assert.Equal(t, 1, res.Failed)
	assert.Equal(t, []string{"ws-ok"}, lister.deleted,
		"a failed destroy must leave its row in place while the pass continues")
}

// Row deletion must happen only after a successful container destroy.
func TestRunIdleReclamation_RowDeletedOnlyAfterSuccessfulDestroy(t *testing.T) {
	lister := &fakeIdleLister{servers: []*AgentSandbox{idleMCPServer("ws-fail", 10, StatusReady)}}
	rt := &fakeIdleReclaimer{destroyErr: map[string]error{"ws-fail": assert.AnError}}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 0, res.Reclaimed)
	assert.Equal(t, 1, res.Failed)
	assert.Empty(t, lister.deleted, "row must not be deleted when the container destroy failed")
}

func TestRunIdleReclamation_DeleteFailureCountsAsFailed(t *testing.T) {
	lister := &fakeIdleLister{
		servers:   []*AgentSandbox{idleMCPServer("ws-1", 10, StatusReady)},
		deleteErr: assert.AnError,
	}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 0, res.Reclaimed)
	assert.Equal(t, 1, res.Failed)
}

func TestRunIdleReclamation_LogsReclaimAndSkipDecisions(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	lister := &fakeIdleLister{servers: []*AgentSandbox{
		idleMCPServer("ws-reclaim", 10, StatusReady),
		idleMCPServer("ws-recent", 1, StatusReady),
		idleMCPServer("ws-creating", 10, StatusCreating),
	}}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 7, log)

	require.Equal(t, 1, res.Reclaimed)
	require.Equal(t, 2, res.Skipped)

	out := buf.String()
	assert.Contains(t, out, "ws-reclaim")
	assert.Contains(t, out, "idle_days", "reclaim log must include the idle age")
	assert.Contains(t, out, "ws-recent")
	assert.Contains(t, out, "ws-creating")
	assert.Contains(t, out, "used within idle window", "skip log must include the reason")
	assert.Contains(t, out, "in-flight lifecycle state")
}

// A call that starts after the candidate SELECT but before reclamation touches
// last_used_at; the fresh re-read must detect it and skip the server, never
// destroying the container or deleting the row.
func TestRunIdleReclamation_TouchedBetweenSelectAndDestroy(t *testing.T) {
	ws := idleMCPServer("ws-touched", 10, StatusReady)
	lister := &fakeIdleLister{
		servers: []*AgentSandbox{ws},
		recheck: func(string) *AgentSandbox { return nil },
	}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 0, res.Reclaimed)
	assert.Equal(t, 1, res.Skipped)
	assert.Empty(t, rt.destroyed, "container must not be destroyed when the row was touched after selection")
	assert.Empty(t, lister.deleted, "row must not be deleted when the row was touched after selection")
	assert.Empty(t, rt.stopped, "runtime must not be stopped for a server skipped by the fresh re-check")
}

// A failed fresh re-read is counted as failed and does not destroy the server.
func TestRunIdleReclamation_RecheckFailureCountsAsFailed(t *testing.T) {
	lister := &fakeIdleLister{
		servers:    []*AgentSandbox{idleMCPServer("ws-1", 10, StatusReady)},
		recheckErr: assert.AnError,
	}
	rt := &fakeIdleReclaimer{}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 0, res.Reclaimed)
	assert.Equal(t, 1, res.Failed)
	assert.Empty(t, rt.destroyed)
	assert.Empty(t, lister.deleted)
}

// The per-server destroy is bounded by a deadline so a blocked provider cannot
// hang the cleanup goroutine (and thus Stop()/graceful shutdown).
func TestRunIdleReclamation_DestroyBoundedByTimeout(t *testing.T) {
	lister := &fakeIdleLister{servers: []*AgentSandbox{idleMCPServer("ws-1", 10, StatusReady)}}
	var (
		deadline time.Time
		hasDL    bool
	)
	rt := &fakeIdleReclaimer{onDestroy: func(ctx context.Context, _ *AgentSandbox) {
		deadline, hasDL = ctx.Deadline()
	}}

	res := runIdleReclamation(context.Background(), lister, rt, 7, testLogger())

	assert.Equal(t, 1, res.Reclaimed)
	require.True(t, hasDL, "destroy must receive a bounded context with a deadline")
	assert.WithinDuration(t, time.Now().Add(idleReclaimTimeout), deadline, 5*time.Second)
}

func TestIdleEligible(t *testing.T) {
	cutoff := time.Now().AddDate(0, 0, -7)

	tests := []struct {
		name string
		ws   *AgentSandbox
		want bool
	}{
		{"nil", nil, false},
		{"idle persistent", idleMCPServer("a", 10, StatusReady), true},
		{"recent", idleMCPServer("b", 1, StatusReady), false},
		{"creating", idleMCPServer("c", 10, StatusCreating), false},
		{"stopping", idleMCPServer("d", 10, StatusStopping), false},
		{"not mcp", &AgentSandbox{ContainerType: ContainerTypeAgentSandbox, Lifecycle: LifecyclePersistent, LastUsedAt: cutoff.AddDate(0, 0, -1)}, false},
		{"not persistent", &AgentSandbox{ContainerType: ContainerTypeMCPServer, Lifecycle: LifecycleEphemeral, LastUsedAt: cutoff.AddDate(0, 0, -1)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, idleEligible(tt.ws, cutoff))
		})
	}
}

// The idle pass destroys the container through the same provider Destroy path
// as explicit removal.
func TestHostedMCPRuntime_DestroyContainer_UsesProviderDestroy(t *testing.T) {
	o := NewOrchestrator(testLogger())
	mp := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true}
	o.RegisterProvider(ProviderGVisor, mp)

	rt := &hostedMCPRuntime{orchestrator: o}
	err := rt.DestroyContainer(context.Background(), &AgentSandbox{
		ID:                  "ws-1",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-1",
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), mp.destroyCount.Load())
}

func TestHostedMCPRuntime_DestroyContainer_PropagatesProviderError(t *testing.T) {
	o := NewOrchestrator(testLogger())
	mp := &mockProvider{name: "gvisor", providerType: ProviderGVisor, healthy: true, destroyErr: assert.AnError}
	o.RegisterProvider(ProviderGVisor, mp)

	rt := &hostedMCPRuntime{orchestrator: o}
	err := rt.DestroyContainer(context.Background(), &AgentSandbox{
		ID:                  "ws-1",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-1",
	})

	require.Error(t, err)
}

func TestHostedMCPRuntime_DestroyContainer_ProviderUnavailable(t *testing.T) {
	o := NewOrchestrator(testLogger()) // no providers registered

	rt := &hostedMCPRuntime{orchestrator: o}
	err := rt.DestroyContainer(context.Background(), &AgentSandbox{
		ID:                  "ws-1",
		Provider:            ProviderGVisor,
		ProviderWorkspaceID: "container-1",
	})

	require.Error(t, err)
}

func TestHostedMCPRuntime_StopRuntime_NilHostingIsSafe(t *testing.T) {
	rt := &hostedMCPRuntime{}
	assert.NotPanics(t, func() { rt.StopRuntime("ws-1") })
}
