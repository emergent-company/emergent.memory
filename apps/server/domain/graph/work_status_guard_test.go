package graph

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/stretchr/testify/require"
)

func TestIsBoardEnabledConfig(t *testing.T) {
	require.False(t, isBoardEnabledConfig(nil))
	require.False(t, isBoardEnabledConfig(&agents.ObjectTypeWorkConfig{}))
	require.True(t, isBoardEnabledConfig(&agents.ObjectTypeWorkConfig{BoardEnabled: true}))
}

func TestRejectAgentWorkStatusWriteCtx(t *testing.T) {
	// The exemption is scoped and explicit: without it an agent still cannot
	// write board status.
	require.Error(t, rejectAgentWorkStatusWriteCtx(context.Background(), ActorAgent, true, true))

	ctx := auth.WithAgentWorkStatusAllowed(context.Background())
	require.NoError(t, rejectAgentWorkStatusWriteCtx(ctx, ActorAgent, true, true))

	// The exemption does not relax the other guard conditions.
	require.NoError(t, rejectAgentWorkStatusWriteCtx(ctx, ActorAgent, false, true))
	require.NoError(t, rejectAgentWorkStatusWriteCtx(ctx, ActorAgent, true, false))
	require.NoError(t, rejectAgentWorkStatusWriteCtx(context.Background(), ActorUser, true, true))

	// The status vocabulary is enforced separately, so the exemption cannot
	// smuggle in an out-of-set status.
	cfg := &agents.ObjectTypeWorkConfig{
		BoardEnabled:    true,
		AllowedStatuses: []string{"new", "triaged"},
	}
	ready := "ready"
	require.Error(t, validateTypeStatus(cfg, &ready, nil))
	triaged := "triaged"
	require.NoError(t, validateTypeStatus(cfg, &triaged, nil))
}

// TestResolvedInitialStatus pins the #422 entry-lane contract: an explicit
// initialStatus wins, otherwise the first allowed status is the fallback, and a
// type that is not board-enabled or declares no statuses is unaffected.
func TestResolvedInitialStatus(t *testing.T) {
	require.Empty(t, resolvedInitialStatus(nil))
	require.Empty(t, resolvedInitialStatus(&agents.ObjectTypeWorkConfig{}))
	require.Equal(t, "pending", resolvedInitialStatus(&agents.ObjectTypeWorkConfig{
		BoardEnabled:    true,
		AllowedStatuses: []string{"pending", "done"},
	}))
	require.Equal(t, "open", resolvedInitialStatus(&agents.ObjectTypeWorkConfig{
		BoardEnabled:    true,
		InitialStatus:   "open",
		AllowedStatuses: []string{"ready", "open", "closed"},
	}))
	// A stray initialStatus outside the set degrades to the first allowed status.
	require.Equal(t, "ready", resolvedInitialStatus(&agents.ObjectTypeWorkConfig{
		BoardEnabled:    true,
		InitialStatus:   "open",
		AllowedStatuses: []string{"ready", "closed"},
	}))
	// Empty allowedStatuses means no entry lane, even with an initialStatus.
	require.Empty(t, resolvedInitialStatus(&agents.ObjectTypeWorkConfig{
		BoardEnabled:  true,
		InitialStatus: "open",
	}))
	// Non-board types are never defaulted.
	require.Empty(t, resolvedInitialStatus(&agents.ObjectTypeWorkConfig{
		InitialStatus:   "open",
		AllowedStatuses: []string{"open"},
	}))
}

func TestApplyInitialStatus(t *testing.T) {
	cfg := &agents.ObjectTypeWorkConfig{
		BoardEnabled:    true,
		InitialStatus:   "open",
		AllowedStatuses: []string{"ready", "open", "closed"},
	}
	// No status: the entry lane is applied.
	applied := applyInitialStatus(cfg, nil, nil)
	require.NotNil(t, applied)
	require.Equal(t, "open", *applied)

	// An explicit status is never overridden.
	done := "closed"
	require.Equal(t, &done, applyInitialStatus(cfg, &done, nil))

	// A properties["status"] mirror wins and is left to statusForWrite.
	require.Nil(t, applyInitialStatus(cfg, nil, map[string]any{"status": "ready"}))

	// An explicit empty string is not a status; it is defaulted.
	empty := ""
	applied = applyInitialStatus(cfg, &empty, nil)
	require.NotNil(t, applied)
	require.Equal(t, "open", *applied)

	// Non-board/empty-vocabulary types are unchanged.
	require.Nil(t, applyInitialStatus(nil, nil, nil))
	require.Nil(t, applyInitialStatus(&agents.ObjectTypeWorkConfig{BoardEnabled: true}, nil, nil))
}

// TestRejectAgentCreateWorkStatus pins the create-specific agent rule (#422
// option 2): explicitly naming the resolved entry status is allowed, every other
// status stays platform-owned, and a create with no status is a default rather
// than a write.
func TestRejectAgentCreateWorkStatus(t *testing.T) {
	cfg := &agents.ObjectTypeWorkConfig{
		BoardEnabled:    true,
		InitialStatus:   "open",
		AllowedStatuses: []string{"ready", "open", "closed"},
	}
	ctx := context.Background()

	require.NoError(t, rejectAgentCreateWorkStatus(ctx, ActorAgent, cfg, nil, nil))
	open := "open"
	require.NoError(t, rejectAgentCreateWorkStatus(ctx, ActorAgent, cfg, &open, nil))
	require.NoError(t, rejectAgentCreateWorkStatus(ctx, ActorAgent, cfg, nil, map[string]any{"status": "open"}))
	ready := "ready"
	err := rejectAgentCreateWorkStatus(ctx, ActorAgent, cfg, &ready, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "work_status_owned_by_platform")
	require.Error(t, rejectAgentCreateWorkStatus(ctx, ActorAgent, cfg, nil, map[string]any{"status": "ready"}))
	// Humans are never guarded.
	require.NoError(t, rejectAgentCreateWorkStatus(ctx, ActorUser, cfg, &ready, nil))
	// Non-board types are unconstrained.
	require.NoError(t, rejectAgentCreateWorkStatus(ctx, ActorAgent, &agents.ObjectTypeWorkConfig{}, &ready, nil))
}

func TestValidateTypeStatus(t *testing.T) {
	cfg := &agents.ObjectTypeWorkConfig{
		BoardEnabled:    true,
		AllowedStatuses: []string{"ready", "in_progress", "review", "done"},
	}

	// nil config / not board-enabled / no allowed set → unconstrained.
	require.NoError(t, validateTypeStatus(nil, strPtr("shipped"), nil))
	require.NoError(t, validateTypeStatus(&agents.ObjectTypeWorkConfig{}, strPtr("shipped"), nil))
	require.NoError(t, validateTypeStatus(&agents.ObjectTypeWorkConfig{BoardEnabled: true}, strPtr("shipped"), nil))

	// In-set status accepted.
	require.NoError(t, validateTypeStatus(cfg, strPtr("ready"), nil))
	require.NoError(t, validateTypeStatus(cfg, strPtr("done"), nil))

	// Out-of-set status rejected (both the status field and properties["status"]).
	require.Error(t, validateTypeStatus(cfg, strPtr("shipped"), nil))
	require.Error(t, validateTypeStatus(cfg, nil, map[string]any{"status": "shipped"}))

	// properties["status"] wins over the explicit field (mirrors the write path).
	require.Error(t, validateTypeStatus(cfg, strPtr("ready"), map[string]any{"status": "shipped"}))
	require.NoError(t, validateTypeStatus(cfg, strPtr("shipped"), map[string]any{"status": "done"}))

	// No status being set → no validation.
	require.NoError(t, validateTypeStatus(cfg, nil, nil))
	require.NoError(t, validateTypeStatus(cfg, nil, map[string]any{"topic": "x"}))
}

// subgraphStatusSchemas has one constrained board type ("Task") and one
// unconstrained non-board type ("Note").
func subgraphStatusSchemas() *ExtractionSchemas {
	return &ExtractionSchemas{
		ObjectSchemas: map[string]agents.ObjectSchema{
			"Task": {Name: "Task", ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
				BoardEnabled:    true,
				InitialStatus:   "ready",
				AllowedStatuses: []string{"ready", "in_progress", "review", "done"},
			}},
			"Note": {Name: "Note", ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{BoardEnabled: false}},
		},
	}
}

// TestValidateSubgraphWorkStatus pins the per-type work-status vocabulary and
// agent-write rule on the subgraph/bulk create path (issue #1851): the generic
// batch create door must reject a status the type's declared allowed set does
// not offer, exactly as the single-object create/patch doors do.
func TestValidateSubgraphWorkStatus(t *testing.T) {
	schemas := subgraphStatusSchemas()
	shipped := "shipped"
	ready := "ready"
	done := "done"

	t.Run("out-of-set status rejected", func(t *testing.T) {
		err := validateSubgraphWorkStatus(ActorUser, schemas, []SubgraphObjectRequest{
			{Type: "Task", Status: &shipped},
		})
		require.Error(t, err)
	})

	t.Run("in-set status accepted", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorUser, schemas, []SubgraphObjectRequest{
			{Type: "Task", Status: &ready},
		}))
	})

	t.Run("status smuggled through properties is rejected", func(t *testing.T) {
		err := validateSubgraphWorkStatus(ActorUser, schemas, []SubgraphObjectRequest{
			{Type: "Task", Properties: map[string]any{"status": "shipped"}},
		})
		require.Error(t, err)
	})

	t.Run("one off-vocabulary object rejects the whole batch", func(t *testing.T) {
		err := validateSubgraphWorkStatus(ActorUser, schemas, []SubgraphObjectRequest{
			{Type: "Task", Status: &ready},
			{Type: "Task", Status: &shipped},
		})
		require.Error(t, err)
	})

	t.Run("non-board type unconstrained", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorUser, schemas, []SubgraphObjectRequest{
			{Type: "Note", Status: &shipped},
		}))
	})

	t.Run("unknown type unconstrained", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorUser, schemas, []SubgraphObjectRequest{
			{Type: "FreeForm", Status: &shipped},
		}))
	})

	t.Run("agent status write on board type rejected", func(t *testing.T) {
		err := validateSubgraphWorkStatus(ActorAgent, schemas, []SubgraphObjectRequest{
			{Type: "Task", Status: &done},
		})
		require.Error(t, err)
	})

	t.Run("agent create naming the initial status accepted", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorAgent, schemas, []SubgraphObjectRequest{
			{Type: "Task", Status: &ready},
		}))
		require.NoError(t, validateSubgraphWorkStatus(ActorAgent, schemas, []SubgraphObjectRequest{
			{Type: "Task", Properties: map[string]any{"status": "ready"}},
		}))
	})

	t.Run("agent create with no status accepted", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorAgent, schemas, []SubgraphObjectRequest{
			{Type: "Task", Properties: map[string]any{"topic": "x"}},
		}))
	})

	t.Run("agent status write on non-board type accepted", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorAgent, schemas, []SubgraphObjectRequest{
			{Type: "Note", Status: &ready},
		}))
	})

	t.Run("in-set status with other properties accepted", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorUser, schemas, []SubgraphObjectRequest{
			{Type: "Task", Status: &done, Properties: map[string]any{"topic": "x"}},
		}))
	})

	t.Run("no status being set accepted", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorUser, schemas, []SubgraphObjectRequest{
			{Type: "Task", Properties: map[string]any{"topic": "x"}},
		}))
	})

	t.Run("nil schemas inert", func(t *testing.T) {
		require.NoError(t, validateSubgraphWorkStatus(ActorUser, nil, []SubgraphObjectRequest{
			{Type: "Task", Status: &shipped},
		}))
	})
}

// TestValidateWorkObjectStatus pins the handler-side allowed-set check the two
// device work-item writes inherit (issue #454). A status outside the type's
// declared allowedStatuses is refused before any graph write; an unconstrained
// or non-board type accepts it, exactly as the web status control does.
func TestValidateWorkObjectStatus(t *testing.T) {
	board := &agents.ObjectTypeWorkConfig{
		BoardEnabled:    true,
		AllowedStatuses: []string{"ready", "in_progress", "review", "done"},
	}
	require.NoError(t, ValidateWorkObjectStatus(board, "done"))
	require.NoError(t, ValidateWorkObjectStatus(board, "review"))

	err := ValidateWorkObjectStatus(board, "shipped")
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	require.Equal(t, "work_status_not_allowed", appErr.Code)

	// No declared set on a board-enabled type: any status is accepted.
	require.NoError(t, ValidateWorkObjectStatus(&agents.ObjectTypeWorkConfig{BoardEnabled: true}, "shipped"))
	// Not a board type: any status is accepted.
	require.NoError(t, ValidateWorkObjectStatus(&agents.ObjectTypeWorkConfig{AllowedStatuses: []string{"ready"}}, "shipped"))
	require.NoError(t, ValidateWorkObjectStatus(nil, "shipped"))
}
