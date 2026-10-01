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
