package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/backups"
	"github.com/spf13/cobra"
)

// backupsScheduleCmd groups the recurring-backup configuration commands. A
// project has at most one schedule: `set` creates or replaces it, `get` reads
// it, `delete` removes it.
var backupsScheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage the project's recurring backup schedule",
	Long: `Configure automatic project backups.

A project has at most one schedule. The cadence is a standard 5-field cron
expression (minute hour day-of-month month day-of-week) evaluated in UTC, and it
must not fire more often than every 15 minutes. The server runs due schedules
once a minute and starts a normal project backup for each one.`,
}

// ── schedule get ──────────────────────────────────────────────────────────────

var backupsScheduleGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show the project's backup schedule",
	Long: `Show the project's recurring backup configuration.

Exits 0 with "No backup schedule configured." when the project has none.`,
	RunE: runBackupsScheduleGet,
}

func runBackupsScheduleGet(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectFlag, _ := cmd.Flags().GetString("project")
	projectID, err := resolveProjectContext(cmd, projectFlag)
	if err != nil {
		return err
	}

	schedule, err := c.SDK.Backups.GetSchedule(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("failed to get backup schedule: %w", err)
	}
	if schedule == nil {
		// An absent schedule is a valid answer, not an error: --output json|yaml
		// gets an explicit null instead of prose, so a script can tell the two
		// apart without parsing English.
		if handled, err := emitStructured(cmd, nil); handled {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No backup schedule configured.")
		return nil
	}

	if handled, err := emitStructured(cmd, schedule); handled {
		return err
	}
	printBackupSchedule(cmd.OutOrStdout(), schedule)
	return nil
}

// ── schedule set ──────────────────────────────────────────────────────────────

var (
	backupsScheduleSetCron           string
	backupsScheduleSetDisabled       bool
	backupsScheduleSetRetentionDays  int
	backupsScheduleSetIncludeChat    bool
	backupsScheduleSetIncludeJournal bool
	backupsScheduleSetIncludeDeleted bool
)

var backupsScheduleSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Create or replace the project's backup schedule",
	Long: `Create or replace the project's recurring backup configuration.

--cron is a standard 5-field cron expression evaluated in UTC (for example
"0 2 * * *" for 02:00 UTC daily). It must not fire more often than every 15
minutes. The schedule is enabled unless --disabled is passed; retention applies
to every backup the schedule creates.

Editing the schedule recomputes the next run immediately, so a cadence change
takes effect without waiting out the previous expression.`,
	Example: `  memory backups schedule set --cron "0 2 * * *"
  memory backups schedule set --cron "0 */6 * * *" --retention-days 14 --include-chat
  memory backups schedule set --cron "0 2 * * *" --disabled`,
	RunE: runBackupsScheduleSet,
}

func runBackupsScheduleSet(cmd *cobra.Command, args []string) error {
	if err := validateRetentionDays(backupsScheduleSetRetentionDays); err != nil {
		return err
	}
	if backupsScheduleSetCron == "" {
		return fmt.Errorf("--cron is required (5-field cron expression, e.g. \"0 2 * * *\")")
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectFlag, _ := cmd.Flags().GetString("project")
	projectID, err := resolveProjectContext(cmd, projectFlag)
	if err != nil {
		return err
	}

	schedule, err := c.SDK.Backups.SaveSchedule(context.Background(), projectID, &backups.ScheduleRequest{
		Enabled:        !backupsScheduleSetDisabled,
		CronSchedule:   backupsScheduleSetCron,
		RetentionDays:  backupsScheduleSetRetentionDays,
		IncludeChat:    backupsScheduleSetIncludeChat,
		IncludeJournal: backupsScheduleSetIncludeJournal,
		IncludeDeleted: backupsScheduleSetIncludeDeleted,
	})
	if err != nil {
		return fmt.Errorf("failed to save backup schedule: %w", err)
	}

	if handled, err := emitStructured(cmd, schedule); handled {
		return err
	}
	printBackupSchedule(cmd.OutOrStdout(), schedule)
	return nil
}

// ── schedule delete ───────────────────────────────────────────────────────────

var backupsScheduleDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the project's backup schedule",
	Long: `Delete the project's recurring backup configuration.

Backups the schedule already created are kept (subject to their own retention).
Future runs stop immediately.`,
	RunE: runBackupsScheduleDelete,
}

func runBackupsScheduleDelete(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectFlag, _ := cmd.Flags().GetString("project")
	projectID, err := resolveProjectContext(cmd, projectFlag)
	if err != nil {
		return err
	}

	if err := c.SDK.Backups.DeleteSchedule(context.Background(), projectID); err != nil {
		return fmt.Errorf("failed to delete backup schedule: %w", err)
	}

	if handled, err := emitStructured(cmd, scheduleDeletion{ProjectID: projectID, Deleted: true}); handled {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Backup schedule deleted.")
	return nil
}

// scheduleDeletion is the machine-readable result of `schedule delete`, so
// --output json|yaml reports which project lost its schedule.
type scheduleDeletion struct {
	ProjectID string `json:"projectId" yaml:"projectId"`
	Deleted   bool   `json:"deleted" yaml:"deleted"`
}

// printBackupSchedule renders a schedule for humans.
func printBackupSchedule(out io.Writer, s *backups.Schedule) {
	state := "enabled"
	if !s.Enabled {
		state = "disabled"
	}
	fmt.Fprintf(out, "%s\n\n", cHeader("Backup schedule"))
	fmt.Fprintf(out, "  Cron:       %s (%s)\n", s.CronSchedule, s.Timezone)
	fmt.Fprintf(out, "  State:      %s\n", state)
	fmt.Fprintf(out, "  Retention:  %d days\n", s.RetentionDays)
	fmt.Fprintf(out, "  Includes:   chat=%t journal=%t deleted=%t\n",
		s.IncludeChat, s.IncludeJournal, s.IncludeDeleted)
	fmt.Fprintf(out, "  Next run:   %s\n", fmtTimeStr(s.NextRunAt))
	if s.LastRunAt != nil {
		fmt.Fprintf(out, "  Last run:   %s\n", fmtTimeStr(*s.LastRunAt))
	}
	if s.LastBackupID != nil {
		fmt.Fprintf(out, "  Last backup: %s\n", shortID(*s.LastBackupID))
	}
}

func init() {
	backupsScheduleSetCmd.Flags().StringVar(&backupsScheduleSetCron, "cron", "", "5-field cron expression in UTC (required)")
	backupsScheduleSetCmd.Flags().BoolVar(&backupsScheduleSetDisabled, "disabled", false, "Save the schedule but do not run it")
	backupsScheduleSetCmd.Flags().IntVar(&backupsScheduleSetRetentionDays, "retention-days", 30, "Retention for the backups it creates (1-365)")
	backupsScheduleSetCmd.Flags().BoolVar(&backupsScheduleSetIncludeChat, "include-chat", false, "Include chat conversations and messages")
	backupsScheduleSetCmd.Flags().BoolVar(&backupsScheduleSetIncludeJournal, "include-journal", false, "Include journal entries")
	backupsScheduleSetCmd.Flags().BoolVar(&backupsScheduleSetIncludeDeleted, "include-deleted", false, "Include soft-deleted items")

	backupsScheduleCmd.AddCommand(backupsScheduleGetCmd)
	backupsScheduleCmd.AddCommand(backupsScheduleSetCmd)
	backupsScheduleCmd.AddCommand(backupsScheduleDeleteCmd)

	backupsCmd.AddCommand(backupsScheduleCmd)
}
