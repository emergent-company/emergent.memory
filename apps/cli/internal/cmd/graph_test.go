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

// TestGraphObjectsListCmd_ProvenanceFlagsRegistered verifies the actor
// provenance flags are registered on `memory graph objects list`.
func TestGraphObjectsListCmd_ProvenanceFlagsRegistered(t *testing.T) {
	cmd := graphObjectsListCmd
	for _, f := range []string{"actor-type", "actor-id", "provenance"} {
		assert.NotNil(t, cmd.Flags().Lookup(f), "flag --%s should be registered", f)
	}
}

// TestParseProvenanceFilter_Defaults verifies absent flags resolve to an empty
// actor filter with provenance defaulting to "any".
func TestParseProvenanceFilter_Defaults(t *testing.T) {
	graphActorTypeFlag = ""
	graphActorIDFlag = ""
	graphProvenanceFlag = ""
	t.Cleanup(func() {
		graphActorTypeFlag = ""
		graphActorIDFlag = ""
		graphProvenanceFlag = ""
	})

	actorType, actorID, provenance, err := parseProvenanceFilter()
	require.NoError(t, err)
	assert.Equal(t, "", actorType)
	assert.Equal(t, "", actorID)
	assert.Equal(t, "any", provenance)
}

// TestParseProvenanceFilter_AgentScoped verifies the agent-scoped case
// (--actor-type agent --actor-id <uuid>) resolves to the matching pair with a
// default "any" provenance.
func TestParseProvenanceFilter_AgentScoped(t *testing.T) {
	const agentID = "11111111-1111-1111-1111-111111111111"
	graphActorTypeFlag = "agent"
	graphActorIDFlag = agentID
	graphProvenanceFlag = ""
	t.Cleanup(func() {
		graphActorTypeFlag = ""
		graphActorIDFlag = ""
		graphProvenanceFlag = ""
	})

	actorType, actorID, provenance, err := parseProvenanceFilter()
	require.NoError(t, err)
	assert.Equal(t, "agent", actorType)
	assert.Equal(t, agentID, actorID)
	assert.Equal(t, "any", provenance)
}

// TestParseProvenanceFilter_ExplicitProvenance verifies an explicit valid value
// is passed through unchanged.
func TestParseProvenanceFilter_ExplicitProvenance(t *testing.T) {
	graphActorTypeFlag = "agent"
	graphActorIDFlag = "11111111-1111-1111-1111-111111111111"
	graphProvenanceFlag = "created"
	t.Cleanup(func() {
		graphActorTypeFlag = ""
		graphActorIDFlag = ""
		graphProvenanceFlag = ""
	})

	_, _, provenance, err := parseProvenanceFilter()
	require.NoError(t, err)
	assert.Equal(t, "created", provenance)
}

// TestParseProvenanceFilter_InvalidProvenance verifies an unknown --provenance
// value is rejected client-side.
func TestParseProvenanceFilter_InvalidProvenance(t *testing.T) {
	graphActorTypeFlag = "agent"
	graphActorIDFlag = ""
	graphProvenanceFlag = "bogus"
	t.Cleanup(func() {
		graphActorTypeFlag = ""
		graphActorIDFlag = ""
		graphProvenanceFlag = ""
	})

	_, _, _, err := parseProvenanceFilter()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--provenance")
	assert.Contains(t, err.Error(), "bogus")
}

// TestParseProvenanceFilter_ActorIDWithoutActorType verifies --actor-id without
// --actor-type is rejected (mirroring the server's pair rule).
func TestParseProvenanceFilter_ActorIDWithoutActorType(t *testing.T) {
	graphActorTypeFlag = ""
	graphActorIDFlag = "11111111-1111-1111-1111-111111111111"
	graphProvenanceFlag = ""
	t.Cleanup(func() {
		graphActorTypeFlag = ""
		graphActorIDFlag = ""
		graphProvenanceFlag = ""
	})

	_, _, _, err := parseProvenanceFilter()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--actor-id requires --actor-type")
}
