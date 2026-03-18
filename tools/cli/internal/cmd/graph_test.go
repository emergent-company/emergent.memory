package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGraphObjectsCreateCmd_Flags verifies that the create command exposes the
// expected flags, including the newly added --key and --upsert flags (issue #95).
func TestGraphObjectsCreateCmd_Flags(t *testing.T) {
	cmd := graphObjectsCreateCmd

	flags := []string{"type", "name", "description", "properties", "key", "upsert"}
	for _, f := range flags {
		assert.NotNil(t, cmd.Flags().Lookup(f), "flag --%s should be registered", f)
	}
}

// TestGraphObjectsCreateCmd_UpsertRequiresKey verifies that --upsert without
// --key is rejected before any API call is made.
func TestGraphObjectsCreateCmd_UpsertRequiresKey(t *testing.T) {
	// Reset flags to known state before executing
	graphTypeFlag = "Belief"
	graphKeyFlag = ""
	graphUpsertFlag = true
	t.Cleanup(func() {
		graphTypeFlag = ""
		graphKeyFlag = ""
		graphUpsertFlag = false
	})

	err := graphObjectsCreateCmd.RunE(graphObjectsCreateCmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--upsert requires --key")
}

// TestGraphObjectsCreateCmd_TypeRequired verifies that --type is enforced.
func TestGraphObjectsCreateCmd_TypeRequired(t *testing.T) {
	graphTypeFlag = ""
	graphKeyFlag = ""
	graphUpsertFlag = false
	t.Cleanup(func() {
		graphTypeFlag = ""
	})

	err := graphObjectsCreateCmd.RunE(graphObjectsCreateCmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--type is required")
}

// TestGraphObjectsCreateCmd_KeyFlagRegistered verifies --key flag default value.
func TestGraphObjectsCreateCmd_KeyFlagRegistered(t *testing.T) {
	f := graphObjectsCreateCmd.Flags().Lookup("key")
	require.NotNil(t, f)
	assert.Equal(t, "", f.DefValue, "--key should default to empty string")
}

// TestGraphObjectsCreateCmd_UpsertFlagRegistered verifies --upsert flag default value.
func TestGraphObjectsCreateCmd_UpsertFlagRegistered(t *testing.T) {
	f := graphObjectsCreateCmd.Flags().Lookup("upsert")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue, "--upsert should default to false")
}
