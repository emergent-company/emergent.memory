package cmd

import (
	"github.com/spf13/cobra"
)

// dbCmd is the parent for all database-related subcommands.
// Subcommands: diagnose
var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database utilities",
	Long: `Database inspection and performance utilities for Emergent.

Examples:
  emergent db diagnose              Run full query performance analysis
  emergent db diagnose --verbose    Include full EXPLAIN output for every query
  emergent db diagnose --slow 50    Flag queries slower than 50ms`,
}

func init() {
	dbCmd.AddCommand(dbDiagnoseCmd)
	rootCmd.AddCommand(dbCmd)
}
