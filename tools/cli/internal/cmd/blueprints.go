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
// Shared install logic
// ─────────────────────────────────────────────

func runBlueprintsInstall(cmd *cobra.Command, args []string) error {
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
}

// ─────────────────────────────────────────────
// blueprintsCmd — namespace + deprecated fallback
// ─────────────────────────────────────────────

var blueprintsCmd = &cobra.Command{
	Use:     "blueprints",
	Short:   "Install or export Blueprints (schemas, agents, skills, seed data)",
	GroupID: "knowledge",
	Long: `Blueprints are structured directories (or GitHub repos) containing schemas,
agent definitions, skills, and seed data to apply to a project.

Subcommands:

  memory blueprints install <source>    Apply blueprints from a directory or GitHub URL
  memory blueprints dump <output-dir>   Export project graph data as JSONL seed files

Examples:

  memory blueprints install ./my-config
  memory blueprints install https://github.com/acme/memory-blueprints
  memory blueprints install https://github.com/acme/memory-blueprints#v1.2.0 --upgrade
  memory blueprints install ./my-config --dry-run
  memory blueprints dump ./exported`,
	// No Args constraint — subcommands handle their own args.
	// Bare invocation with a positional arg is the deprecated form.
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		// Backwards-compat: memory blueprints <source> → memory blueprints install <source>
		fmt.Fprintln(os.Stderr, "Deprecated: 'memory blueprints <source>' is deprecated. Use 'memory blueprints install <source>' instead.")
		return runBlueprintsInstall(cmd, args)
	},
}

// ─────────────────────────────────────────────
// blueprintsInstallCmd — apply blueprints
// ─────────────────────────────────────────────

var blueprintsInstallCmd = &cobra.Command{
	Use:   "install <source>",
	Short: "Apply Blueprints from a directory or GitHub URL",
	Long: `Apply Blueprints — schemas, agent definitions, skills, and seed data — to the
current project from a structured directory or a GitHub repository URL.

The source directory (or GitHub repo root) may contain:
  schemas/            — one file per memory schema  (.json, .yaml, .yml)
  agents/             — one file per agent definition (.json, .yaml, .yml)
  skills/             — one subdirectory per skill, each containing a SKILL.md file
  seed/objects/       — per-type JSONL files with graph objects to seed
  seed/relationships/ — per-type JSONL files with graph relationships to seed

Skills follow the agentskills.io open standard: each skill is a directory with a
SKILL.md file containing YAML frontmatter (name, description) and Markdown content.

By default the command is additive-only: existing resources are skipped.
Use --upgrade to update resources that already exist.

Examples:

  memory blueprints install ./my-config
  memory blueprints install https://github.com/acme/memory-blueprints
  memory blueprints install https://github.com/acme/memory-blueprints#v1.2.0 --upgrade
  memory blueprints install ./my-config --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runBlueprintsInstall,
}

// ─────────────────────────────────────────────
// blueprintsValidateCmd — offline schema validation
// ─────────────────────────────────────────────

var blueprintsValidateCmd = &cobra.Command{
	Use:   "validate <source>",
	Short: "Validate a Blueprint directory or GitHub URL without applying it",
	Long: `Validate a Blueprint directory (or GitHub repository) without making any API
calls or modifying the project.

Checks performed:
  Packs      — required fields, object/relationship type definitions, internal
               cross-references (sourceType/targetType must name an objectType
               in the same pack), duplicate names within and across files.
  Agents     — required fields, enum values (flowType, visibility, dispatchMode),
               model.name required when model block is present, duplicate names.
  Skills     — required fields, non-empty content body, duplicate names.
  Seed data  — required fields, key presence (warning when missing), type
               cross-referenced against loaded pack definitions, srcKey/dstKey
               cross-referenced against seed object keys.

Exits 0 when only warnings are found, exits 1 when any error is found.

Examples:

  memory blueprints validate ./my-config
  memory blueprints validate https://github.com/acme/memory-blueprints
  memory blueprints validate https://github.com/acme/memory-blueprints#v1.2.0`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := args[0]
		out := cmd.OutOrStdout()

		// ── Resolve source directory ──────────────────────────────────
		var dir string
		var err error

		if blueprints.IsGitHubURL(source) {
			token := blueprintsTokenFlag
			if token == "" {
				token = os.Getenv("MEMORY_GITHUB_TOKEN")
			}
			var cleanup func()
			dir, cleanup, err = blueprints.FetchGitHubRepo(source, token)
			if err != nil {
				return fmt.Errorf("fetch GitHub repo: %w", err)
			}
			defer cleanup()
		} else {
			dir = source
		}

		// ── Load files ────────────────────────────────────────────────
		envVars := blueprints.LoadEnvFiles(dir)
		projectFile, packs, agents, skills, seedObjects, seedRels, loadResults, err := blueprints.LoadDir(dir, envVars)
		if err != nil {
			return fmt.Errorf("load directory: %w", err)
		}

		// ── Validate ──────────────────────────────────────────────────
		report := blueprints.Validate(projectFile, packs, agents, skills, seedObjects, seedRels, loadResults)

		// ── Print results ─────────────────────────────────────────────
		useColor := !noColor
		errPrefix := "  error    "
		warnPrefix := "  warning  "
		if useColor {
			errPrefix = "  \033[0;31merror\033[0m    "
			warnPrefix = "  \033[0;33mwarning\033[0m  "
		}

		for _, issue := range report.Issues {
			prefix := warnPrefix
			if issue.Severity == blueprints.ValidationError {
				prefix = errPrefix
			}
			loc := issue.SourceFile
			if issue.Field != "" {
				loc += " → " + issue.Field
			}
			name := ""
			if issue.Name != "" {
				name = fmt.Sprintf(" %q", issue.Name)
			}
			fmt.Fprintf(out, "%s%s%s: %s\n    (%s)\n", prefix, issue.ResourceType, name, issue.Message, loc)
		}

		// Summary line.
		errCount := len(report.Errors())
		warnCount := len(report.Warnings())

		fmt.Fprintln(out, "")
		if errCount == 0 && warnCount == 0 {
			if useColor {
				fmt.Fprintf(out, "\033[0;32mValidation passed\033[0m — no issues found.\n")
			} else {
				fmt.Fprintf(out, "Validation passed — no issues found.\n")
			}
			return nil
		}

		resourceCounts := fmt.Sprintf("(%d pack(s), %d agent(s), %d skill(s), %d seed object(s), %d seed relationship(s))",
			len(packs), len(agents), len(skills), len(seedObjects), len(seedRels))
		fmt.Fprintf(out, "Validation complete %s: %d error(s), %d warning(s)\n", resourceCounts, errCount, warnCount)

		if errCount > 0 {
			return fmt.Errorf("blueprint has %d validation error(s)", errCount)
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
seed files that can be re-applied with "memory blueprints install <dir>".

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
	// --project is persistent so it's inherited by all subcommands
	blueprintsCmd.PersistentFlags().StringVar(&blueprintsProjectFlag, "project", "", "Project ID or name (overrides config/env)")

	// install subcommand flags
	blueprintsInstallCmd.Flags().BoolVar(&blueprintsUpgradeFlag, "upgrade", false, "Update existing resources instead of skipping them")
	blueprintsInstallCmd.Flags().BoolVar(&blueprintsDryRunFlag, "dry-run", false, "Preview actions without making any API calls")
	blueprintsInstallCmd.Flags().StringVar(&blueprintsTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN")

	// deprecated bare form also needs these flags
	blueprintsCmd.Flags().BoolVar(&blueprintsUpgradeFlag, "upgrade", false, "Update existing resources instead of skipping them")
	blueprintsCmd.Flags().BoolVar(&blueprintsDryRunFlag, "dry-run", false, "Preview actions without making any API calls")
	blueprintsCmd.Flags().StringVar(&blueprintsTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN")

	// dump subcommand flags
	blueprintsDumpCmd.Flags().StringVar(&blueprintsDumpTypesFlag, "types", "", "Comma-separated list of object/relationship types to export (default: all types)")

	// validate subcommand flags
	blueprintsValidateCmd.Flags().StringVar(&blueprintsTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN")

	blueprintsCmd.AddCommand(blueprintsInstallCmd)
	blueprintsCmd.AddCommand(blueprintsDumpCmd)
	blueprintsCmd.AddCommand(blueprintsValidateCmd)
	rootCmd.AddCommand(blueprintsCmd)
}
