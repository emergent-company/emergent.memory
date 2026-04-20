// codebase — knowledge graph CLI for codebase analysis and sync.
//
// Reads project config from .codebase.yml (walked up from cwd) and
// authenticates via the Memory SDK (MEMORY_API_KEY / ~/.memory/config.yaml).
//
// Install:
//
//	task codebase:install   → /usr/local/bin/codebase
//
// Usage:
//
//	codebase sync routes
//	codebase check api
//	codebase graph list --type APIEndpoint
//	codebase --help
package main

import (
	"fmt"
	"os"

	analyzecmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/analyze"
	branchcmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/branch"
	checkcmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/check"
	constitutioncmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/constitution"
	createcmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/create"
	fixcmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/fix"
	graphcmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/graph"
	onboardcmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/onboard"
	seedcmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/seed"
	skillscmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/skills"
	synccmd "github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/sync"
	"github.com/spf13/cobra"
)

// Build-time version info — injected via ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var (
	flagProjectID string
	flagBranch    string
	flagFormat    string
)

var rootCmd = &cobra.Command{
	Use:     "codebase",
	Short:   "Knowledge graph CLI for codebase analysis and sync",
	Version: Version,
	Long: `codebase — populate, audit, and explore the Memory knowledge graph for your codebase.

Auth: reads from ~/.memory/config.yaml, .env.local, or MEMORY_API_KEY env var.
Project: reads from .codebase.yml (project_id or project name), or MEMORY_PROJECT_ID.

Examples:
  codebase sync routes          # populate APIEndpoint objects from route files
  codebase sync middleware      # wire Middleware→APIEndpoint relationships
  codebase check api            # audit APIEndpoint quality
  codebase check coverage       # test coverage gaps by domain
  codebase check complexity     # domain complexity scores
  codebase analyze tree         # Domain→Service→Endpoint map
  codebase graph list --type APIEndpoint --all
  codebase graph get ep-agents-listagents
  codebase fix stale            # remove stale graph objects
`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagProjectID, "project-id", "", "Memory project ID (overrides .codebase.yml and MEMORY_PROJECT_ID)")
	rootCmd.PersistentFlags().StringVar(&flagBranch, "branch", "", "Graph branch ID (default: main branch)")
	rootCmd.PersistentFlags().StringVar(&flagFormat, "format", "table", "Output format: table, json, markdown")

	// Register command groups
	rootCmd.AddCommand(onboardcmd.NewCmd(&flagProjectID, &flagBranch))
	rootCmd.AddCommand(synccmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(checkcmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(analyzecmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(graphcmd.NewCmd(&flagProjectID, &flagBranch))
	rootCmd.AddCommand(seedcmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(fixcmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	rootCmd.AddCommand(branchcmd.NewCmd(&flagProjectID))
	rootCmd.AddCommand(skillscmd.NewCmd())
	rootCmd.AddCommand(constitutioncmd.NewCmd(&flagProjectID, &flagBranch, &flagFormat))
	createcmd.Register(rootCmd, &flagProjectID, &flagBranch, &flagFormat)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
