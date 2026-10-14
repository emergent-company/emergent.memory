package graph

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/stretchr/testify/require"
)

func TestIsBoardEnabledConfig(t *testing.T) {
	require.False(t, isBoardEnabledConfig(nil))
	require.False(t, isBoardEnabledConfig(&agents.ObjectTypeWorkConfig{}))
	require.True(t, isBoardEnabledConfig(&agents.ObjectTypeWorkConfig{BoardEnabled: true}))
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
			{Type: "Task", Status: &ready},
		})
		require.Error(t, err)
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
