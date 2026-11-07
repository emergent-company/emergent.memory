package cmd

import (
	"bytes"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/backups"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupsScheduleCommandStructure(t *testing.T) {
	var parentFound bool
	for _, cmd := range backupsCmd.Commands() {
		if cmd.Name() == "schedule" {
			parentFound = true
		}
	}
	require.True(t, parentFound, "expected a backups schedule subcommand")

	names := make(map[string]bool)
	for _, cmd := range backupsScheduleCmd.Commands() {
		names[cmd.Name()] = true
	}
	for _, name := range []string{"get", "set", "delete"} {
		assert.True(t, names[name], "expected backups schedule subcommand %q", name)
	}
}

func TestBackupsScheduleSetFlags(t *testing.T) {
	for _, name := range []string{
		"cron", "disabled", "retention-days",
		"include-chat", "include-journal", "include-deleted",
	} {
		require.NotNil(t, backupsScheduleSetCmd.Flags().Lookup(name), "schedule set flag %q should be defined", name)
	}
}

func TestPrintBackupSchedule(t *testing.T) {
	lastRun := "2026-11-07T02:00:00Z"
	lastBackup := "11111111-2222-3333-4444-555555555555"
	s := &backups.Schedule{
		Enabled:        true,
		CronSchedule:   "0 2 * * *",
		Timezone:       "UTC",
		RetentionDays:  30,
		IncludeChat:    true,
		IncludeJournal: false,
		IncludeDeleted: false,
		NextRunAt:      "2026-11-08T02:00:00Z",
		LastRunAt:      &lastRun,
		LastBackupID:   &lastBackup,
	}

	var buf bytes.Buffer
	printBackupSchedule(&buf, s)
	out := buf.String()

	assert.Contains(t, out, "0 2 * * *")
	assert.Contains(t, out, "UTC")
	assert.Contains(t, out, "enabled")
	assert.Contains(t, out, "30 days")
	assert.Contains(t, out, "chat=true journal=false deleted=false")
	assert.Contains(t, out, "Last run")
	assert.Contains(t, out, "Last backup")

	disabled := &backups.Schedule{Enabled: false, CronSchedule: "0 4 * * *", Timezone: "UTC"}
	buf.Reset()
	printBackupSchedule(&buf, disabled)
	out = buf.String()
	assert.Contains(t, out, "disabled")
	assert.NotContains(t, out, "Last run")
}
