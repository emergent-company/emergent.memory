package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/client"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/backups"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var backupsCmd = &cobra.Command{
	Use:     "backups",
	Short:   "Manage project backups",
	Long:    "Commands for creating, listing, downloading, importing, and deleting project backups",
	GroupID: "account",
}

// ── create ────────────────────────────────────────────────────────────────────

var (
	backupsCreateIncludeDeleted bool
	backupsCreateIncludeChat    bool
	backupsCreateIncludeJournal bool
	backupsCreateRetentionDays  int
	backupsCreateWait           bool
	backupsCreateTimeout        time.Duration
)

var backupsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a project backup",
	Long: `Create a full backup of a project (documents, chunks, graph, files, and
optionally deleted items, chat, and journal). The server starts the backup
asynchronously and returns a backup with status "creating".

Use --wait to block until the backup reaches "ready" or "failed".`,
	RunE: runBackupsCreate,
}

func runBackupsCreate(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectFlag, _ := cmd.Flags().GetString("project")
	projectID, err := resolveProjectContext(cmd, projectFlag)
	if err != nil {
		return err
	}

	if err := validateRetentionDays(backupsCreateRetentionDays); err != nil {
		return err
	}

	b, err := c.SDK.Backups.CreateBackup(context.Background(), projectID, &backups.CreateBackupRequest{
		IncludeDeleted: backupsCreateIncludeDeleted,
		IncludeChat:    backupsCreateIncludeChat,
		IncludeJournal: backupsCreateIncludeJournal,
		RetentionDays:  backupsCreateRetentionDays,
	})
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	if backupsCreateWait {
		ctx, cancel := context.WithTimeout(context.Background(), backupsCreateTimeout)
		defer cancel()
		b, err = waitForBackup(ctx, c, b.OrganizationID, b.ID)
		if err != nil {
			return err
		}
		if b.Status == backups.BackupStatusFailed {
			return fmt.Errorf("backup failed: %s", backupErrorMessage(b))
		}
	}

	if handled, err := emitStructured(cmd, b); handled {
		return err
	}
	printBackup(cmd.OutOrStdout(), b)
	return nil
}

// ── list ──────────────────────────────────────────────────────────────────────

var (
	backupsListOrgID  string
	backupsListLimit  int
	backupsListCursor string
)

var backupsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List backups",
	Long: `List backups for an organization, optionally filtered by project and
paginated with --limit and --cursor.`,
	RunE: runBackupsList,
}

func runBackupsList(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, backupsListOrgID)
	if err != nil {
		return err
	}

	// The global --project persistent flag filters the list; read it directly.
	projectFilter, _ := cmd.Flags().GetString("project")

	result, err := c.SDK.Backups.ListBackups(context.Background(), orgID, &backups.ListBackupsOptions{
		ProjectID: projectFilter,
		Limit:     backupsListLimit,
		Cursor:    backupsListCursor,
	})
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	if handled, err := emitStructured(cmd, result); handled {
		return err
	}

	if len(result.Backups) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No backups found.")
		return nil
	}

	printBackupList(cmd.OutOrStdout(), result)
	return nil
}

// ── get ───────────────────────────────────────────────────────────────────────

var backupsGetOrgID string

var backupsGetCmd = &cobra.Command{
	Use:   "get <backupId>",
	Short: "Get backup details",
	Long:  "Get details for a specific backup by ID.",
	Args:  cobra.ExactArgs(1),
	RunE:  runBackupsGet,
}

func runBackupsGet(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, backupsGetOrgID)
	if err != nil {
		return err
	}

	b, err := c.SDK.Backups.GetBackup(context.Background(), orgID, args[0])
	if err != nil {
		return fmt.Errorf("failed to get backup: %w", err)
	}

	if handled, err := emitStructured(cmd, b); handled {
		return err
	}
	printBackup(cmd.OutOrStdout(), b)
	return nil
}

// ── download ──────────────────────────────────────────────────────────────────

var (
	backupsDownloadOrgID string
	backupsDownloadOut   string
)

var backupsDownloadCmd = &cobra.Command{
	Use:   "download <backupId>",
	Short: "Download a backup archive",
	Long:  "Download a ready backup archive to --out, streaming it to disk.",
	Args:  cobra.ExactArgs(1),
	RunE:  runBackupsDownload,
}

func runBackupsDownload(cmd *cobra.Command, args []string) error {
	if backupsDownloadOut == "" {
		return fmt.Errorf("--out is required")
	}

	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, backupsDownloadOrgID)
	if err != nil {
		return err
	}

	body, filename, err := c.SDK.Backups.DownloadBackup(context.Background(), orgID, args[0])
	if err != nil {
		return fmt.Errorf("failed to download backup: %w", err)
	}
	defer body.Close()

	out := backupsDownloadOut
	if fi, statErr := os.Stat(out); statErr == nil && fi.IsDir() {
		out = filepath.Join(out, filename)
	}

	f, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, body)
	if err != nil {
		return fmt.Errorf("failed to write archive: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Downloaded %d bytes to %s\n", n, out)
	return nil
}

// ── delete ────────────────────────────────────────────────────────────────────

var (
	backupsDeleteOrgID string
	backupsDeleteForce bool
)

var backupsDeleteCmd = &cobra.Command{
	Use:   "delete <backupId>",
	Short: "Delete a backup",
	Long:  "Permanently delete a backup and its archive. Use --force to skip confirmation.",
	Args:  cobra.ExactArgs(1),
	RunE:  runBackupsDelete,
}

func runBackupsDelete(cmd *cobra.Command, args []string) error {
	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, backupsDeleteOrgID)
	if err != nil {
		return err
	}

	backupID := args[0]

	if !backupsDeleteForce {
		if isNonInteractive() {
			return fmt.Errorf("deleting a backup is destructive — re-run with --force to confirm")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Delete backup %s? [y/N] ", backupID)
		scanner := bufio.NewScanner(cmd.InOrStdin())
		if !scanner.Scan() {
			return fmt.Errorf("delete aborted")
		}
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			return fmt.Errorf("delete aborted")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.SDK.Backups.DeleteBackup(ctx, orgID, backupID); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Backup %s deleted.\n", backupID)
	return nil
}

// ── import ────────────────────────────────────────────────────────────────────

var (
	backupsImportOrgID         string
	backupsImportRetentionDays int
)

var backupsImportCmd = &cobra.Command{
	Use:   "import <archive.zip>",
	Short: "Import a backup archive",
	Long: `Import a backup archive produced by another deployment, registering it as a
ready backup for clone restore. The archive is streamed (max 1 GiB).`,
	Args: cobra.ExactArgs(1),
	RunE: runBackupsImport,
}

func runBackupsImport(cmd *cobra.Command, args []string) error {
	if err := validateRetentionDays(backupsImportRetentionDays); err != nil {
		return err
	}

	c, err := getAccountClient(cmd)
	if err != nil {
		return err
	}

	orgID, err := resolveProviderOrgID(c, backupsImportOrgID)
	if err != nil {
		return err
	}

	f, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer f.Close()

	b, err := c.SDK.Backups.ImportBackup(context.Background(), orgID, &backups.ImportBackupInput{
		Filename:      filepath.Base(args[0]),
		Reader:        f,
		RetentionDays: backupsImportRetentionDays,
	})
	if err != nil {
		return fmt.Errorf("failed to import backup: %w", err)
	}

	if handled, err := emitStructured(cmd, b); handled {
		return err
	}
	printBackup(cmd.OutOrStdout(), b)
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// validateRetentionDays enforces the server's inclusive 1..365 range.
func validateRetentionDays(n int) error {
	if n < 1 || n > 365 {
		return fmt.Errorf("retention-days must be between 1 and 365 (got %d)", n)
	}
	return nil
}

// isTerminalBackupStatus reports whether a backup status is terminal.
func isTerminalBackupStatus(status string) bool {
	return status == backups.BackupStatusReady ||
		status == backups.BackupStatusFailed ||
		status == backups.BackupStatusDeleted
}

// backupErrorMessage returns a backup's error message, or "" when unset.
func backupErrorMessage(b *backups.Backup) string {
	if b.ErrorMessage == nil {
		return ""
	}
	return *b.ErrorMessage
}

// waitForBackup polls a backup until it reaches a terminal state or ctx is done.
func waitForBackup(ctx context.Context, c *client.Client, orgID, backupID string) (*backups.Backup, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		b, err := c.SDK.Backups.GetBackup(ctx, orgID, backupID)
		if err != nil {
			return nil, fmt.Errorf("failed to get backup status: %w", err)
		}
		if isTerminalBackupStatus(b.Status) {
			return b, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for backup to finish: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// emitStructured writes v as JSON or YAML when --output requests it, returning
// handled=true. Returns handled=false for table/csv (caller renders the table).
func emitStructured(cmd *cobra.Command, v interface{}) (bool, error) {
	switch output {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return true, enc.Encode(v)
	case "yaml":
		data, err := yaml.Marshal(v)
		if err != nil {
			return true, fmt.Errorf("failed to encode YAML: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return true, err
	default:
		return false, nil
	}
}

// printBackup prints a human-readable summary of a single backup.
func printBackup(out io.Writer, b *backups.Backup) {
	fmt.Fprintf(out, "Backup: %s (%s)\n", cBold(b.ProjectName), cDim(shortID(b.ID)))
	fmt.Fprintf(out, "  ID:         %s\n", b.ID)
	fmt.Fprintf(out, "  Status:     %s\n", b.Status)
	fmt.Fprintf(out, "  Progress:   %d%%\n", b.Progress)
	fmt.Fprintf(out, "  Size:       %d bytes\n", b.SizeBytes)
	fmt.Fprintf(out, "  Created:    %s\n", fmtTimeStr(b.CreatedAt))
	if b.CompletedAt != nil {
		fmt.Fprintf(out, "  Completed:  %s\n", fmtTimeStr(*b.CompletedAt))
	}
	if b.Imported {
		fmt.Fprintf(out, "  Imported:   true\n")
	}
	if b.ErrorMessage != nil && *b.ErrorMessage != "" {
		fmt.Fprintf(out, "  Error:      %s\n", *b.ErrorMessage)
	}
}

// printBackupList prints a table of backups.
func printBackupList(out io.Writer, result *backups.ListBackupsResult) {
	fmt.Fprintf(out, "%s:\n\n", cHeader(fmt.Sprintf("Found %d backup(s)", result.Total)))
	for _, b := range result.Backups {
		fmt.Fprintf(out, "  %s  %s\n", cBold(shortID(b.ID)), cDim(b.ProjectName))
		fmt.Fprintf(out, "      status=%s progress=%d%% size=%d created=%s\n",
			b.Status, b.Progress, b.SizeBytes, fmtTimeStr(b.CreatedAt))
	}
	if result.NextCursor != nil {
		fmt.Fprintf(out, "\nNext cursor: %s\n", result.NextCursor.ID)
	}
}

func init() {
	backupsCreateCmd.Flags().BoolVar(&backupsCreateIncludeDeleted, "include-deleted", false, "Include soft-deleted items")
	backupsCreateCmd.Flags().BoolVar(&backupsCreateIncludeChat, "include-chat", false, "Include chat conversations and messages")
	backupsCreateCmd.Flags().BoolVar(&backupsCreateIncludeJournal, "include-journal", false, "Include journal entries")
	backupsCreateCmd.Flags().IntVar(&backupsCreateRetentionDays, "retention-days", 30, "Retention period in days (1-365)")
	backupsCreateCmd.Flags().BoolVar(&backupsCreateWait, "wait", false, "Block until the backup reaches a terminal status")
	backupsCreateCmd.Flags().DurationVar(&backupsCreateTimeout, "timeout", 10*time.Minute, "Wait timeout (e.g. 10m, 30s)")

	backupsListCmd.Flags().StringVar(&backupsListOrgID, "org-id", "", "Organization ID (auto-detected if not specified)")
	backupsListCmd.Flags().IntVar(&backupsListLimit, "limit", 0, "Maximum number of results (1-100)")
	backupsListCmd.Flags().StringVar(&backupsListCursor, "cursor", "", "Pagination cursor from a previous response")

	backupsGetCmd.Flags().StringVar(&backupsGetOrgID, "org-id", "", "Organization ID (auto-detected if not specified)")

	backupsDownloadCmd.Flags().StringVar(&backupsDownloadOrgID, "org-id", "", "Organization ID (auto-detected if not specified)")
	backupsDownloadCmd.Flags().StringVar(&backupsDownloadOut, "out", "", "Output file path (required)")

	backupsDeleteCmd.Flags().StringVar(&backupsDeleteOrgID, "org-id", "", "Organization ID (auto-detected if not specified)")
	backupsDeleteCmd.Flags().BoolVar(&backupsDeleteForce, "force", false, "Skip the confirmation prompt")

	backupsImportCmd.Flags().StringVar(&backupsImportOrgID, "org-id", "", "Organization ID (auto-detected if not specified)")
	backupsImportCmd.Flags().IntVar(&backupsImportRetentionDays, "retention-days", 30, "Retention period in days (1-365)")

	backupsCmd.AddCommand(backupsCreateCmd)
	backupsCmd.AddCommand(backupsListCmd)
	backupsCmd.AddCommand(backupsGetCmd)
	backupsCmd.AddCommand(backupsDownloadCmd)
	backupsCmd.AddCommand(backupsDeleteCmd)
	backupsCmd.AddCommand(backupsImportCmd)

	rootCmd.AddCommand(backupsCmd)
}
