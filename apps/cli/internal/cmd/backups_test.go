package cmd

import (
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/backups"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRetentionDays(t *testing.T) {
	for _, n := range []int{1, 30, 365} {
		assert.NoError(t, validateRetentionDays(n), "retention-days %d should be valid", n)
	}
	for _, n := range []int{0, -1, 366, 1000} {
		assert.Error(t, validateRetentionDays(n), "retention-days %d should be invalid", n)
	}
}

func TestIsTerminalBackupStatus(t *testing.T) {
	for _, s := range []string{backups.BackupStatusReady, backups.BackupStatusFailed, backups.BackupStatusDeleted} {
		assert.True(t, isTerminalBackupStatus(s), "status %q should be terminal", s)
	}
	assert.False(t, isTerminalBackupStatus(backups.BackupStatusCreating), "creating should not be terminal")
}

func TestIsTerminalRestoreStatus(t *testing.T) {
	for _, s := range []string{backups.RestoreStatusCompleted, backups.RestoreStatusFailed} {
		assert.True(t, isTerminalRestoreStatus(s), "status %q should be terminal", s)
	}
	assert.False(t, isTerminalRestoreStatus(backups.RestoreStatusPending), "pending should not be terminal")
	assert.False(t, isTerminalRestoreStatus(backups.RestoreStatusRunning), "running should not be terminal")
}

func TestBackupErrorMessage(t *testing.T) {
	assert.Equal(t, "", backupErrorMessage(&backups.Backup{}))

	msg := "boom"
	assert.Equal(t, "boom", backupErrorMessage(&backups.Backup{ErrorMessage: &msg}))
}

func TestRestoreErrorMessage(t *testing.T) {
	assert.Equal(t, "", restoreErrorMessage(&backups.Restore{}))

	msg := "boom"
	assert.Equal(t, "boom", restoreErrorMessage(&backups.Restore{ErrorMessage: &msg}))
}

func TestBackupsCommandStructure(t *testing.T) {
	subcommands := backupsCmd.Commands()
	names := make(map[string]bool, len(subcommands))
	for _, cmd := range subcommands {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"create", "list", "get", "download", "delete", "import"} {
		assert.True(t, names[name], "expected backups subcommand %q", name)
	}

	assert.Equal(t, "account", backupsCmd.GroupID)
}

func TestRestoresCommandStructure(t *testing.T) {
	subcommands := restoresCmd.Commands()
	names := make(map[string]bool, len(subcommands))
	for _, cmd := range subcommands {
		names[cmd.Name()] = true
	}

	for _, name := range []string{"create", "get"} {
		assert.True(t, names[name], "expected restores subcommand %q", name)
	}

	assert.Equal(t, "account", restoresCmd.GroupID)
}

func TestBackupsCreateFlags(t *testing.T) {
	for _, name := range []string{"include-deleted", "include-chat", "include-journal", "retention-days", "wait", "timeout"} {
		require.NotNil(t, backupsCreateCmd.Flags().Lookup(name), "create flag %q should be defined", name)
	}
}

func TestBackupsListFlags(t *testing.T) {
	for _, name := range []string{"org-id", "limit", "cursor"} {
		require.NotNil(t, backupsListCmd.Flags().Lookup(name), "list flag %q should be defined", name)
	}
}

func TestBackupsDownloadFlags(t *testing.T) {
	require.NotNil(t, backupsDownloadCmd.Flags().Lookup("out"), "download --out flag should be defined")
	require.NotNil(t, backupsDownloadCmd.Flags().Lookup("org-id"), "download --org-id flag should be defined")
}

func TestBackupsDeleteFlags(t *testing.T) {
	require.NotNil(t, backupsDeleteCmd.Flags().Lookup("force"), "delete --force flag should be defined")
}

func TestBackupsImportFlags(t *testing.T) {
	require.NotNil(t, backupsImportCmd.Flags().Lookup("retention-days"), "import --retention-days flag should be defined")
	require.NotNil(t, backupsImportCmd.Flags().Lookup("org-id"), "import --org-id flag should be defined")
}

func TestRestoresCreateFlags(t *testing.T) {
	for _, name := range []string{"org-id", "backup", "target-name", "wait", "timeout"} {
		require.NotNil(t, restoresCreateCmd.Flags().Lookup(name), "restores create flag %q should be defined", name)
	}
}

func TestBackupsGetRequiresArg(t *testing.T) {
	require.NotNil(t, backupsGetCmd.Args, "backups get should require an argument")
	require.NotNil(t, backupsDownloadCmd.Args, "backups download should require an argument")
	require.NotNil(t, backupsDeleteCmd.Args, "backups delete should require an argument")
	require.NotNil(t, backupsImportCmd.Args, "backups import should require an argument")
	require.NotNil(t, restoresGetCmd.Args, "restores get should require an argument")
}
