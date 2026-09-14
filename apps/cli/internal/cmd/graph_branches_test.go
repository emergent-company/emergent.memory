package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphBranchesCommandStructure(t *testing.T) {
	subcommands := graphBranchesCmd.Commands()
	names := make(map[string]bool, len(subcommands))
	for _, cmd := range subcommands {
		names[cmd.Name()] = true
	}

	expected := []string{"list", "get", "create", "update", "delete", "merge"}
	for _, name := range expected {
		assert.True(t, names[name], "expected subcommand %q to be registered", name)
	}
}

func TestGraphBranchesGetRequiresArg(t *testing.T) {
	assert.NotNil(t, graphBranchesGetCmd.Args, "get command should require args")
}

func TestGraphBranchesUpdateRequiresArg(t *testing.T) {
	assert.NotNil(t, graphBranchesUpdateCmd.Args, "update command should require args")
}

func TestGraphBranchesDeleteRequiresArg(t *testing.T) {
	assert.NotNil(t, graphBranchesDeleteCmd.Args, "delete command should require args")
}

func TestGraphBranchesMergeRequiresArg(t *testing.T) {
	assert.NotNil(t, graphBranchesMergeCmd.Args, "merge command should require args")
}

func TestGraphBranchesCreateFlags(t *testing.T) {
	nameFlag := graphBranchesCreateCmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag, "name flag should be defined on create")

	parentFlag := graphBranchesCreateCmd.Flags().Lookup("parent")
	require.NotNil(t, parentFlag, "parent flag should be defined on create")
}

func TestGraphBranchesUpdateFlags(t *testing.T) {
	nameFlag := graphBranchesUpdateCmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag, "name flag should be defined on update")
}

func TestGraphBranchesMergeFlags(t *testing.T) {
	sourceFlag := graphBranchesMergeCmd.Flags().Lookup("source")
	require.NotNil(t, sourceFlag, "source flag should be defined on merge")

	executeFlag := graphBranchesMergeCmd.Flags().Lookup("execute")
	require.NotNil(t, executeFlag, "execute flag should be defined on merge")
}

func TestGraphBranchesInheritsPersistentFlags(t *testing.T) {
	// --project and --output are persistent on graphCmd and should be
	// accessible from branch subcommands via the parent chain.
	projectFlag := graphCmd.PersistentFlags().Lookup("project")
	require.NotNil(t, projectFlag, "project persistent flag should be on graphCmd")

	outputFlag := graphCmd.PersistentFlags().Lookup("output")
	require.NotNil(t, outputFlag, "output persistent flag should be on graphCmd")
}
