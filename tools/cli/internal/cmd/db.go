package cmd

import (
	"github.com/spf13/cobra"
)

// dbCmd is the parent for all database-related subcommands.
// Subcommands: diagnose, bench
var dbCmd = &cobra.Command{
	Use:    "db",
	Short:  "Database utilities",
	Hidden: true,
	Long: `Database inspection and performance utilities for Memory.

Examples:
  memory db diagnose              Run full query performance analysis
  memory db diagnose --verbose    Include full EXPLAIN output for every query
  memory db diagnose --slow 50    Flag queries slower than 50ms
  memory db bench                 Benchmark write throughput with real IMDb data
  memory db bench --seed 500      Seed 500 titles and run EXPLAIN checks`,
}

func init() {
	dbCmd.AddCommand(dbDiagnoseCmd)
	dbCmd.AddCommand(dbBenchCmd)
	dbCmd.AddCommand(dbLovdataCmd)
	rootCmd.AddCommand(dbCmd)
}
