package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/blueprints"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	blueprintsProjectFlag string
	blueprintsUpgradeFlag bool
	blueprintsDryRunFlag  bool
	blueprintsTokenFlag   string
)

// ─────────────────────────────────────────────
// Command definition
// ─────────────────────────────────────────────

var blueprintsCmd = &cobra.Command{
	Use:   "blueprints <source>",
	Short: "Apply Blueprints (packs and agents) from a directory or GitHub URL",
	Long: `Apply Blueprints — template packs and agent definitions — to the current
project from a structured directory or a GitHub repository URL.

The source directory (or GitHub repo root) must contain:
  packs/    — one file per template pack  (.json, .yaml, .yml)
  agents/   — one file per agent definition (.json, .yaml, .yml)

By default the command is additive-only: existing resources are skipped.
Use --upgrade to update resources that already exist.

Examples:

  memory blueprints ./my-config
  memory blueprints https://github.com/acme/memory-blueprints
  memory blueprints https://github.com/acme/memory-blueprints#v1.2.0 --upgrade
  memory blueprints ./my-config --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]

		// ── Resolve project ─────────────────────────────────────────────
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		projectID, err := resolveProjectContext(cmd, blueprintsProjectFlag)
		if err != nil {
			return err
		}
		c.SetContext("", projectID)

		// ── Resolve source directory ────────────────────────────────────
		var dir string
		var cleanup func()

		if blueprints.IsGitHubURL(source) {
			token := blueprintsTokenFlag
			if token == "" {
				token = os.Getenv("MEMORY_GITHUB_TOKEN")
			}
			dir, cleanup, err = blueprints.FetchGitHubRepo(source, token)
			if err != nil {
				return fmt.Errorf("fetch GitHub repo: %w", err)
			}
			defer cleanup()
		} else {
			dir = source
		}

		// ── Load files ─────────────────────────────────────────────────
		packs, agents, loadResults, err := blueprints.LoadDir(dir)
		if err != nil {
			return fmt.Errorf("load directory: %w", err)
		}

		// Print any load-time errors immediately.
		out := cmd.OutOrStdout()
		for _, r := range loadResults {
			if r.Action == blueprints.BlueprintsActionError {
				fmt.Fprintf(out, "  warning  %s %q: %v\n", r.ResourceType, r.Name, r.Error)
			}
		}

		if len(packs) == 0 && len(agents) == 0 && len(loadResults) == 0 {
			fmt.Fprintln(out, "Nothing to apply — no blueprint files found.")
			return nil
		}

		// ── Run blueprinter ────────────────────────────────────────────
		applier := blueprints.NewBlueprintsApplier(
			c.SDK.TemplatePacks,
			c.SDK.AgentDefinitions,
			blueprintsDryRunFlag,
			blueprintsUpgradeFlag,
			out,
		)

		results, err := applier.Run(context.Background(), packs, agents)
		if err != nil {
			return err
		}

		// Combine load errors with blueprints results for exit-code decision.
		all := append(loadResults, results...)
		for _, r := range all {
			if r.Action == blueprints.BlueprintsActionError {
				return fmt.Errorf("blueprints completed with errors")
			}
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// init — wire flags and register
// ─────────────────────────────────────────────

func init() {
	blueprintsCmd.Flags().StringVar(&blueprintsProjectFlag, "project", "", "Project ID or name (overrides config/env)")
	blueprintsCmd.Flags().BoolVar(&blueprintsUpgradeFlag, "upgrade", false, "Update existing resources instead of skipping them")
	blueprintsCmd.Flags().BoolVar(&blueprintsDryRunFlag, "dry-run", false, "Preview actions without making any API calls")
	blueprintsCmd.Flags().StringVar(&blueprintsTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN")

	rootCmd.AddCommand(blueprintsCmd)
}
