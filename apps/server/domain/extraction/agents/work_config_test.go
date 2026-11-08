package agents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObjectTypeWorkConfigApplyConfigInitialStatus(t *testing.T) {
	var cfg ObjectTypeWorkConfig
	cfg.ApplyConfig(map[string]any{
		"boardEnabled":    true,
		"allowedStatuses": []any{"ready", "open"},
		"initialStatus":   "open",
	})
	require.True(t, cfg.BoardEnabled)
	require.Equal(t, []string{"ready", "open"}, cfg.AllowedStatuses)
	require.Equal(t, "open", cfg.InitialStatus)
}

// TestObjectTypeWorkConfigValidate pins the authoring rule for the entry lane
// (#422): a declared initialStatus must be one of the type's allowed statuses.
func TestObjectTypeWorkConfigValidate(t *testing.T) {
	require.NoError(t, (*ObjectTypeWorkConfig)(nil).Validate())
	require.NoError(t, (&ObjectTypeWorkConfig{}).Validate())
	require.NoError(t, (&ObjectTypeWorkConfig{
		BoardEnabled:    true,
		AllowedStatuses: []string{"pending", "done"},
	}).Validate())
	require.NoError(t, (&ObjectTypeWorkConfig{
		BoardEnabled:    true,
		InitialStatus:   "pending",
		AllowedStatuses: []string{"pending", "done"},
	}).Validate())
	require.NoError(t, (&ObjectTypeWorkConfig{
		BoardEnabled:  true,
		InitialStatus: "pending",
	}).Validate())

	require.Error(t, (&ObjectTypeWorkConfig{
		BoardEnabled:    true,
		InitialStatus:   "open",
		AllowedStatuses: []string{"pending", "done"},
	}).Validate())
}
