package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/client"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/backups"
	"github.com/spf13/cobra"
)

var restoresCmd = &cobra.Command{
	Use:     "restores",
	Short:   "Manage project restores",
	Long:    "Commands for creating clone restores from backups and tracking restore status",
	GroupID: "account",
}

// ── create ────────────────────────────────────────────────────────────────────

var (
	restoresCreateOrgID      string
	restoresCreateBackupID   string
	restoresCreateTargetName string
	restoresCreateWait       bool
	restoresCreateTimeout    time.Duration
)

var restoresCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a clone restore from a backup",
	Long: `Clone a backup into a new project in the target organization. The server
starts the restore asynchronously and returns a restore with status "pending".

Use --wait to block until the restore reaches "completed" or "failed".`,
	RunE: runRestoresCreate,
}

func runRestoresCreate(cmd *cobra.Command, args []string) error {
	if restoresCreateBackupID == "" {
		return fmt.Errorf("--backup is required")
	}

	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, restoresCreateOrgID)
	if err != nil {
		return err
	}

	r, err := c.SDK.Backups.CreateCloneRestore(context.Background(), orgID, &backups.RestoreRequest{
		BackupID:          restoresCreateBackupID,
		TargetProjectName: restoresCreateTargetName,
	})
	if err != nil {
		return fmt.Errorf("failed to create restore: %w", err)
	}

	if restoresCreateWait {
		ctx, cancel := context.WithTimeout(context.Background(), restoresCreateTimeout)
		defer cancel()
		r, err = waitForRestore(ctx, c, r.ID)
		if err != nil {
			return err
		}
		if r.Status == backups.RestoreStatusFailed {
			return fmt.Errorf("restore failed: %s", restoreErrorMessage(r))
		}
	}

	if handled, err := emitStructured(cmd, r); handled {
		return err
	}
	printRestore(cmd.OutOrStdout(), r)
	return nil
}

// ── get ───────────────────────────────────────────────────────────────────────

var restoresGetCmd = &cobra.Command{
	Use:   "get <restoreId>",
	Short: "Get restore job status",
	Long:  "Get the status, progress, and target project of a restore job by ID.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRestoresGet,
}

func runRestoresGet(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	r, err := c.SDK.Backups.GetRestore(context.Background(), args[0])
	if err != nil {
		return fmt.Errorf("failed to get restore: %w", err)
	}

	if handled, err := emitStructured(cmd, r); handled {
		return err
	}
	printRestore(cmd.OutOrStdout(), r)
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// isTerminalRestoreStatus reports whether a restore status is terminal.
func isTerminalRestoreStatus(status string) bool {
	return status == backups.RestoreStatusCompleted || status == backups.RestoreStatusFailed
}

// restoreErrorMessage returns a restore's error message, or "" when unset.
func restoreErrorMessage(r *backups.Restore) string {
	if r.ErrorMessage == nil {
		return ""
	}
	return *r.ErrorMessage
}

// waitForRestore polls a restore until it reaches a terminal state or ctx is done.
func waitForRestore(ctx context.Context, c *client.Client, restoreID string) (*backups.Restore, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		r, err := c.SDK.Backups.GetRestore(ctx, restoreID)
		if err != nil {
			return nil, fmt.Errorf("failed to get restore status: %w", err)
		}
		if isTerminalRestoreStatus(r.Status) {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for restore to finish: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// printRestore prints a human-readable summary of a restore job.
func printRestore(out io.Writer, r *backups.Restore) {
	fmt.Fprintf(out, "Restore: %s\n", cBold(shortID(r.ID)))
	fmt.Fprintf(out, "  Status:      %s\n", r.Status)
	fmt.Fprintf(out, "  Progress:    %d%%\n", r.Progress)
	fmt.Fprintf(out, "  Backup:      %s\n", r.BackupID)
	if r.TargetProjectID != nil {
		fmt.Fprintf(out, "  Target:      %s\n", *r.TargetProjectID)
	}
	fmt.Fprintf(out, "  Created:     %s\n", fmtTimeStr(r.CreatedAt))
	if r.CompletedAt != nil {
		fmt.Fprintf(out, "  Completed:   %s\n", fmtTimeStr(*r.CompletedAt))
	}
	if r.ErrorMessage != nil && *r.ErrorMessage != "" {
		fmt.Fprintf(out, "  Error:       %s\n", *r.ErrorMessage)
	}
}

func init() {
	restoresCreateCmd.Flags().StringVar(&restoresCreateOrgID, "org-id", "", "Organization ID (clone destination, auto-detected if not specified)")
	restoresCreateCmd.Flags().StringVar(&restoresCreateBackupID, "backup", "", "Backup ID to restore from (required)")
	restoresCreateCmd.Flags().StringVar(&restoresCreateTargetName, "target-name", "", "Name for the cloned project")
	restoresCreateCmd.Flags().BoolVar(&restoresCreateWait, "wait", false, "Block until the restore reaches a terminal status")
	restoresCreateCmd.Flags().DurationVar(&restoresCreateTimeout, "timeout", 15*time.Minute, "Wait timeout (e.g. 15m, 30s)")

	restoresCmd.AddCommand(restoresCreateCmd)
	restoresCmd.AddCommand(restoresGetCmd)

	rootCmd.AddCommand(restoresCmd)
}
