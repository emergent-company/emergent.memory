package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

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

	// dump subcommand flags
	blueprintsDumpTypesFlag string
)

// ─────────────────────────────────────────────
// Command definition
// ─────────────────────────────────────────────

var blueprintsCmd = &cobra.Command{
	Use:     "blueprints <source>",
	Short:   "Apply Blueprints (packs, agents, seed data) from a directory or GitHub URL",
	GroupID: "knowledge",
	Long: `Apply Blueprints — schemas, agent definitions, skills, and seed data — to the
current project from a structured directory or a GitHub repository URL.

The source directory (or GitHub repo root) may contain:
  packs/             — one file per memory schema  (.json, .yaml, .yml)
  agents/            — one file per agent definition (.json, .yaml, .yml)
  skills/            — one subdirectory per skill, each containing a SKILL.md file
  seed/objects/      — per-type JSONL files with graph objects to seed
  seed/relationships/ — per-type JSONL files with graph relationships to seed

Skills follow the agentskills.io open standard: each skill is a directory with a
SKILL.md file containing YAML frontmatter (name, description) and Markdown content.

By default the command is additive-only: existing resources are skipped.
Use --upgrade to update resources that already exist.

Use the dump subcommand to export an existing project's data as seed files:

  memory blueprints dump <output-dir>

Examples:

  memory blueprints ./my-config
  memory blueprints https://github.com/acme/memory-blueprints
  memory blueprints https://github.com/acme/memory-blueprints#v1.2.0 --upgrade
  memory blueprints ./my-config --dry-run
  memory blueprints dump ./exported`,
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
		// Load .env and .env.local from blueprint dir (secrets, API keys).
		envVars := blueprints.LoadEnvFiles(dir)
		projectFile, packs, agents, skills, seedObjects, seedRels, loadResults, err := blueprints.LoadDir(dir, envVars)
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

		if projectFile == nil && len(packs) == 0 && len(agents) == 0 && len(skills) == 0 && len(seedObjects) == 0 && len(seedRels) == 0 && len(loadResults) == 0 {
			fmt.Fprintln(out, "Nothing to apply — no blueprint files found.")
			return nil
		}

		// ── Run blueprinter ────────────────────────────────────────────
		applier := blueprints.NewBlueprintsApplier(
			c.SDK.Projects,
			projectID,
			c.SDK.Schemas,
			c.SDK.AgentDefinitions,
			c.SDK.Skills,
			blueprintsDryRunFlag,
			blueprintsUpgradeFlag,
			out,
		)

		results, err := applier.Run(context.Background(), projectFile, packs, agents, skills)
		if err != nil {
			return err
		}

		// ── Run seeder (seed objects and relationships) ────────────────
		if len(seedObjects) > 0 || len(seedRels) > 0 {
			seeder := blueprints.NewSeeder(
				c.SDK.Graph,
				blueprintsDryRunFlag,
				blueprintsUpgradeFlag,
				out,
			)
			seedResult, err := seeder.Run(context.Background(), seedObjects, seedRels)
			if err != nil {
				return fmt.Errorf("seed: %w", err)
			}
			if !blueprintsDryRunFlag {
				fmt.Fprintf(out, "  seed: %d objects created, %d updated, %d skipped, %d failed; %d relationships created, %d failed\n",
					seedResult.ObjectsCreated, seedResult.ObjectsUpdated, seedResult.ObjectsSkipped, seedResult.ObjectsFailed,
					seedResult.RelsCreated, seedResult.RelsFailed,
				)
			}
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
// blueprintsDumpCmd — export project data as seed files
// ─────────────────────────────────────────────

var blueprintsDumpCmd = &cobra.Command{
	Use:   "dump <output-dir>",
	Short: "Export project graph objects and relationships as JSONL seed files",
	Long: `Export the current project's graph objects and relationships as per-type JSONL
seed files that can be re-applied with "memory blueprints <dir>".

Output layout:
  <output-dir>/seed/objects/<Type>.jsonl
  <output-dir>/seed/relationships/<Type>.jsonl

Files exceeding 50 MB are automatically split:
  <Type>.001.jsonl, <Type>.002.jsonl, …

Examples:

  memory blueprints dump ./exported
  memory blueprints dump ./exported --types Document,Person
  memory blueprints dump ./exported --project my-project`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outputDir := args[0]

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		projectID, err := resolveProjectContext(cmd, blueprintsProjectFlag)
		if err != nil {
			return err
		}
		c.SetContext("", projectID)

		var typeFilter []string
		if blueprintsDumpTypesFlag != "" {
			for _, t := range strings.Split(blueprintsDumpTypesFlag, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					typeFilter = append(typeFilter, t)
				}
			}
		}

		out := cmd.OutOrStdout()
		dumper := blueprints.NewDumper(c.SDK.Graph, typeFilter, out)

		result, err := dumper.Run(context.Background(), outputDir)
		if err != nil {
			return fmt.Errorf("dump: %w", err)
		}

		_ = result // summary already printed by dumper.Run
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

	blueprintsDumpCmd.Flags().StringVar(&blueprintsProjectFlag, "project", "", "Project ID or name (overrides config/env)")
	blueprintsDumpCmd.Flags().StringVar(&blueprintsDumpTypesFlag, "types", "", "Comma-separated list of object/relationship types to export (default: all types)")

	blueprintsCmd.AddCommand(blueprintsDumpCmd)
	rootCmd.AddCommand(blueprintsCmd)
}
