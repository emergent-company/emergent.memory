package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJournalListFlags(t *testing.T) {
	for _, name := range []string{"since", "limit", "branch", "include-branches", "cursor"} {
		require.NotNil(t, journalListCmd.Flags().Lookup(name), "list flag %q should be defined", name)
	}
}

func TestJournalCommandStructure(t *testing.T) {
	subcommands := journalCmd.Commands()
	names := make(map[string]bool, len(subcommands))
	for _, cmd := range subcommands {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"list", "note"} {
		assert.True(t, names[name], "expected journal subcommand %q", name)
	}

	assert.Equal(t, "knowledge", journalCmd.GroupID)
}

func TestJournalNoteFlags(t *testing.T) {
	require.NotNil(t, journalNoteCmd.Flags().Lookup("entry"), "note --entry flag should be defined")
}
