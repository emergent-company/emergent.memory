package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
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

// TestParsePropertyFilters covers all operator / format combinations.
func TestParsePropertyFilters(t *testing.T) {
	t.Run("single eq", func(t *testing.T) {
		got, err := parsePropertyFilters([]string{"status=active"}, "eq")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, sdkgraph.PropertyFilter{Path: "status", Op: "eq", Value: "active"}, got[0])
	})

	t.Run("multiple AND", func(t *testing.T) {
		got, err := parsePropertyFilters([]string{"status=active", "priority=high"}, "eq")
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "status", got[0].Path)
		assert.Equal(t, "priority", got[1].Path)
	})

	t.Run("missing = returns error", func(t *testing.T) {
		_, err := parsePropertyFilters([]string{"statusactive"}, "eq")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key=value")
	})

	t.Run("gte operator", func(t *testing.T) {
		got, err := parsePropertyFilters([]string{"score=90"}, "gte")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, sdkgraph.PropertyFilter{Path: "score", Op: "gte", Value: "90"}, got[0])
	})

	t.Run("contains operator", func(t *testing.T) {
		got, err := parsePropertyFilters([]string{"title=postgres"}, "contains")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, sdkgraph.PropertyFilter{Path: "title", Op: "contains", Value: "postgres"}, got[0])
	})

	t.Run("exists operator ignores value", func(t *testing.T) {
		// With key only (no =)
		got, err := parsePropertyFilters([]string{"embedding"}, "exists")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, sdkgraph.PropertyFilter{Path: "embedding", Op: "exists", Value: nil}, got[0])
	})

	t.Run("in comma-split", func(t *testing.T) {
		got, err := parsePropertyFilters([]string{"status=active,draft,archived"}, "in")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "in", got[0].Op)
		assert.Equal(t, []string{"active", "draft", "archived"}, got[0].Value)
	})

	t.Run("unsupported operator error", func(t *testing.T) {
		_, err := parsePropertyFilters([]string{"status=active"}, "like")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "like")
	})

	t.Run("empty filters returns nil", func(t *testing.T) {
		got, err := parsePropertyFilters(nil, "eq")
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}
