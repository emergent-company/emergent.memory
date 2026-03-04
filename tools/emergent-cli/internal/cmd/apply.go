package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/apply"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	applyProjectFlag string
	applyUpgradeFlag bool
	applyDryRunFlag  bool
	applyTokenFlag   string
)

// ─────────────────────────────────────────────
// Command definition
// ─────────────────────────────────────────────

var applyCmd = &cobra.Command{
	Use:   "apply <source>",
	Short: "Apply template packs and agent definitions from a directory or GitHub URL",
	Long: `Apply template packs and agent definitions to the current project from a
structured directory or a GitHub repository URL.

The source directory (or GitHub repo root) must contain:
  packs/    — one file per template pack  (.json, .yaml, .yml)
  agents/   — one file per agent definition (.json, .yaml, .yml)

By default the command is additive-only: existing resources are skipped.
Use --upgrade to update resources that already exist.

Examples:

  emergent apply ./my-config
  emergent apply https://github.com/acme/emergent-packs
  emergent apply https://github.com/acme/emergent-packs#v1.2.0 --upgrade
  emergent apply ./my-config --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]

		// ── Resolve project ─────────────────────────────────────────────
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		projectID, err := resolveProjectContext(cmd, applyProjectFlag)
		if err != nil {
			return err
		}
		c.SetContext("", projectID)

		// ── Resolve source directory ────────────────────────────────────
		var dir string
		var cleanup func()

		if apply.IsGitHubURL(source) {
			token := applyTokenFlag
			if token == "" {
				token = os.Getenv("EMERGENT_GITHUB_TOKEN")
			}
			dir, cleanup, err = apply.FetchGitHubRepo(source, token)
			if err != nil {
				return fmt.Errorf("fetch GitHub repo: %w", err)
			}
			defer cleanup()
		} else {
			dir = source
		}

		// ── Load files ─────────────────────────────────────────────────
		packs, agents, loadResults, err := apply.LoadDir(dir)
		if err != nil {
			return fmt.Errorf("load directory: %w", err)
		}

		// Print any load-time errors immediately.
		out := cmd.OutOrStdout()
		for _, r := range loadResults {
			if r.Action == apply.ActionError {
				fmt.Fprintf(out, "  warning  %s %q: %v\n", r.ResourceType, r.Name, r.Error)
			}
		}

		if len(packs) == 0 && len(agents) == 0 && len(loadResults) == 0 {
			fmt.Fprintln(out, "Nothing to apply — no pack or agent files found.")
			return nil
		}

		// ── Run applier ────────────────────────────────────────────────
		applier := apply.NewApplier(
			c.SDK.TemplatePacks,
			c.SDK.AgentDefinitions,
			applyDryRunFlag,
			applyUpgradeFlag,
			out,
		)

		results, err := applier.Run(context.Background(), packs, agents)
		if err != nil {
			return err
		}

		// Combine load errors with apply results for exit-code decision.
		all := append(loadResults, results...)
		for _, r := range all {
			if r.Action == apply.ActionError {
				return fmt.Errorf("apply completed with errors")
			}
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// init — wire flags and register
// ─────────────────────────────────────────────

func init() {
	applyCmd.Flags().StringVar(&applyProjectFlag, "project", "", "Project ID or name (overrides config/env)")
	applyCmd.Flags().BoolVar(&applyUpgradeFlag, "upgrade", false, "Update existing resources instead of skipping them")
	applyCmd.Flags().BoolVar(&applyDryRunFlag, "dry-run", false, "Preview actions without making any API calls")
	applyCmd.Flags().StringVar(&applyTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from EMERGENT_GITHUB_TOKEN")

	rootCmd.AddCommand(applyCmd)
}
