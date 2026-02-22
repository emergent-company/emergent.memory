package cmd

import (
	"github.com/spf13/cobra"
)

// dbCmd is the parent for all database-related subcommands.
// Subcommands: diagnose, bench
var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database utilities",
	Long: `Database inspection and performance utilities for Emergent.

Examples:
  emergent db diagnose              Run full query performance analysis
  emergent db diagnose --verbose    Include full EXPLAIN output for every query
  emergent db diagnose --slow 50    Flag queries slower than 50ms
  emergent db bench                 Benchmark write throughput with real IMDb data
  emergent db bench --seed 500      Seed 500 titles and run EXPLAIN checks`,
}

func init() {
	dbCmd.AddCommand(dbDiagnoseCmd)
	dbCmd.AddCommand(dbBenchCmd)
	rootCmd.AddCommand(dbCmd)
}
