package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
	"github.com/emergent-company/emergent.memory/apps/cli/internal/client"
	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	blueprintsProjectFlag    string
	blueprintsUpgradeFlag    bool
	blueprintsDryRunFlag     bool
	blueprintsTokenFlag      string
	blueprintsMergeTypesFlag bool

	// dump subcommand flags
	blueprintsDumpTypesFlag string

	// github-token subcommand flag
	blueprintsGithubTokenFlag string

	// update subcommand flag
	blueprintsUpdateWaitFlag bool

	// validate subcommand flag
	blueprintsStrictFlag bool

	// install model-mapping flags
	blueprintsMapModelFlag []string
	blueprintsNoMapFlag    bool
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

	// Print discovery summary so the user knows what was found.
	out := cmd.OutOrStdout()
	blueprints.PrintDiscoverySummary(out, projectFile, packs, agents, skills, seedObjects, seedRels)

	if projectFile == nil && len(packs) == 0 && len(agents) == 0 && len(skills) == 0 && len(seedObjects) == 0 && len(seedRels) == 0 && len(loadResults) == 0 {
		fmt.Fprintln(out, "Nothing to apply — no blueprint files found.")
		return nil
	}

	// ── Preflight: offline validation + model normalization ────────
	// Runs before anything is applied so a malformed blueprint — e.g. an agent
	// model not in "provider/model" form — aborts with zero mutations instead of
	// partially applying packs first (#1586). Only when a relationship endpoint is
	// not defined by any pack in the blueprint does it make a single read-only
	// call for the project's existing object types, so a relationship may target
	// a type owned by an already-installed pack or a built-in (#1853); the fetch
	// is best-effort — on failure the endpoint check falls back to the
	// blueprint's own packs. Preflight also normalizes model references in place
	// (folds a structured model.provider into the name) so the applier sends
	// exactly the shape the server accepts.
	var projectObjTypes []string
	if blueprints.HasUnknownRelationshipEndpoints(packs) {
		projectObjTypes = projectObjectTypeNames(context.Background(), c)
	}
	report := blueprints.PreflightWithProjectTypes(
		projectObjTypes,
		projectFile, packs, agents, skills, seedObjects, seedRels, loadResults,
	)
	if report.HasErrors() {
		for _, issue := range report.Errors() {
			_, _ = fmt.Fprintf(out, "  error    %s\n", issue)
		}
		return fmt.Errorf("blueprint validation failed with %d error(s); nothing was applied — run 'memory blueprints validate %s' for the full report", len(report.Errors()), source)
	}
	for _, issue := range report.Warnings() {
		_, _ = fmt.Fprintf(out, "  warning  %s\n", issue)
	}

	// ── Object-type merge preflight ────────────────────────────────
	// Read-only check against the project registry BEFORE any materialization.
	// A same-pack re-apply/upgrade is ignored server-side; any other type-name
	// collision needs an explicit merge confirmation. Dry-run is strictly
	// offline: it must not make any API calls, including this preview.
	mergeTypes := blueprintsMergeTypesFlag
	if !blueprintsDryRunFlag && len(packs) > 0 {
		conflicts, err := blueprints.PreviewPackConflicts(context.Background(), c.SDK.Schemas, packs)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			blueprints.PrintMergeConflicts(out, conflicts)
			if !mergeTypes {
				if err := confirmMergeTypeConflicts(cmd, conflicts); err != nil {
					return err
				}
				mergeTypes = true
			}
		}
	}

	// ── Model mapping ──────────────────────────────────────────────
	// After preflight passes (so a malformed model still aborts before any API
	// call), resolve install-time model choices: compare each agent's pinned
	// model against the project's configured providers, apply stored per-project
	// overrides and/or --map-model flags (or prompt when interactive), and
	// rewrite the agents' model names before the applier runs. This is advisory
	// — availability never blocks an install. The resolved choices are persisted
	// after a successful apply.
	mapping, err := runModelMapping(cmd, c, projectID, projectFile, agents, dir, source, out)
	if err != nil {
		return err
	}

	// ── Run blueprinter ────────────────────────────────────────────
	applier := blueprints.NewBlueprintsApplier(
		c.SDK.Projects,
		projectID,
		c.SDK.Schemas,
		c.SDK.AgentDefinitions,
		c.SDK.Agents,
		c.SDK.Skills,
		blueprintsDryRunFlag,
		blueprintsUpgradeFlag,
		out,
	)
	applier.SetMergeTypes(mergeTypes)

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

	// ── Persist model overrides ────────────────────────────────────
	// Only after a fully successful, non-dry-run apply: remember the resolved
	// agent→model choices per project so a re-install/upgrade pre-fills them.
	if mapping != nil && !blueprintsDryRunFlag {
		if err := persistModelOverrides(c, mapping); err != nil {
			_, _ = fmt.Fprintf(out, "  warning  could not persist model overrides: %v\n", err)
		}
	}

	return nil
}

// projectObjectTypeNames returns the object type names already present in the
// project — defined by installed packs or built-ins — used to widen the install
// preflight's relationship-endpoint check beyond the blueprint's own packs
// (#1853). It is best-effort: any error yields nil so preflight still runs with
// the blueprint-only union instead of failing an install on a read failure.
func projectObjectTypeNames(ctx context.Context, c *client.Client) []string {
	compiled, err := c.SDK.Schemas.GetCompiledTypes(ctx)
	if err != nil || compiled == nil {
		return nil
	}
	names := make([]string, 0, len(compiled.ObjectTypes))
	for _, ot := range compiled.ObjectTypes {
		if ot.Name != "" {
			names = append(names, ot.Name)
		}
	}
	return names
}

// confirmMergeTypeConflicts enforces explicit confirmation when object type
// names already exist. Interactive terminals get a y/N prompt; non-interactive
// callers must pass --merge-types, and the error names the conflicting types.
func confirmMergeTypeConflicts(cmd *cobra.Command, conflicts []sdkschemas.MergePreviewConflict) error {
	if isNonInteractive() {
		return fmt.Errorf("object type name conflicts require --merge-types: %s", blueprints.FormatMergeConflictNames(conflicts))
	}
	if err := promptMergeConfirmation(cmd); err != nil {
		return err
	}
	return nil
}

// promptMergeConfirmation asks the y/N question on the command's input/output.
// EOF or anything other than y/yes aborts.
func promptMergeConfirmation(cmd *cobra.Command) error {
	_, _ = fmt.Fprint(cmd.OutOrStdout(), "Existing object type names will be merged. Continue? [y/N]: ")
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		return fmt.Errorf("merge cancelled — no changes applied")
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("merge cancelled — no changes applied")
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
  memory blueprints inspect <source>    List contents of a blueprint without installing
  memory blueprints validate <source>   Validate a blueprint offline (no API calls)
  memory blueprints dump <output-dir>   Export project graph data as JSONL seed files

Examples:

  memory blueprints install ./my-config
  memory blueprints install https://github.com/acme/memory-blueprints
  memory blueprints install https://github.com/acme/memory-blueprints#v1.2.0 --upgrade
  memory blueprints inspect https://github.com/acme/memory-blueprints
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

Before applying packs, the command previews object type names already registered
in the project. If any incoming type name collides with an existing type from a
different pack, the property-level conflicts are printed and an interactive
confirmation (or --merge-types for non-interactive use) is required. With
confirmation, incoming types are additively merged: new properties are added and
existing property definitions win. Re-applying the same pack name is not a
conflict.

The blueprint is validated offline before anything is applied, and agent model
references are normalized to "provider/model" form. A validation error aborts
the install with nothing applied.

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
  Packs      — required fields, object/relationship type definitions,
               cross-references (sourceType/targetType must name an objectType
               defined by some pack in the blueprint — an endpoint may be owned
               by a different pack than the one declaring the relationship),
               duplicate names within and across files.
  Type metadata — labels, required property lists, property definitions and
               types, enum values, array items, minimum/maximum, embedding
               target, embedding/extraction blocks, inline ui and pack uiConfigs
               (icon/color), and relationship properties.
  Agents     — required fields, enum values (visibility, dispatchMode),
               model.name required when model block is present, duplicate names.
  Skills     — required fields, non-empty content body, duplicate names.
  Seed data  — required fields, key presence (warning when missing), type
               cross-referenced against loaded pack definitions, srcKey/dstKey
               cross-referenced against seed object keys.

This command is offline and checks relationship endpoints against the blueprint's
own packs only; 'memory blueprints install' additionally accepts object types
already present in the project.

Exits 0 when only warnings are found, exits 1 when any error is found. With
--strict, warnings also make the command exit 1, so a blueprint must be fully
clean to pass.

Examples:

  memory blueprints validate ./my-config
  memory blueprints validate ./my-config --strict
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

		return validationFailure(report, blueprintsStrictFlag)
	},
}

// validationFailure maps a validation report to the command's result. Errors
// always fail; warnings fail only in strict mode.
func validationFailure(report *blueprints.ValidationReport, strict bool) error {
	if errCount := len(report.Errors()); errCount > 0 {
		return fmt.Errorf("blueprint has %d validation error(s)", errCount)
	}
	if strict {
		if warnCount := len(report.Warnings()); warnCount > 0 {
			return fmt.Errorf("blueprint has %d validation warning(s) and --strict is set", warnCount)
		}
	}
	return nil
}

// ─────────────────────────────────────────────
// blueprintsCheckUpdatesCmd — trigger/print update status
// ─────────────────────────────────────────────

var blueprintsCheckUpdatesCmd = &cobra.Command{
	Use:   "check-updates [name]",
	Short: "Check GitHub-imported blueprints for newer upstream versions",
	Long: `Check the current project's GitHub-imported blueprints for newer upstream
versions. With a blueprint name, it triggers a check (POST .../updates/check) and
prints the latest result; without a name, it prints the current status of every
applied blueprint.

Examples:

  memory blueprints check-updates
  memory blueprints check-updates crm
  memory blueprints check-updates crm --project my-project`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBlueprintsCheckUpdates,
}

func runBlueprintsCheckUpdates(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	projectID, err := resolveProjectContext(cmd, blueprintsProjectFlag)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	out := cmd.OutOrStdout()

	if len(args) == 0 {
		return printAppliedUpdateStatus(cmd, c, out)
	}
	name := args[0]
	if err := triggerBlueprintCheck(cmd, c, name); err != nil {
		return err
	}
	return printSingleUpdateStatus(cmd, c, name, out)
}

type appliedBlueprintStatus struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	SourceURL       string `json:"sourceUrl"`
	UpdateAvailable bool   `json:"updateAvailable"`
	LatestVersion   string `json:"latestVersion"`
	LastCheckedAt   string `json:"lastCheckedAt"`
}

type blueprintUpdateCheckStatus struct {
	BlueprintName   string `json:"blueprintName"`
	SourceURL       string `json:"sourceUrl"`
	SourceRef       string `json:"sourceRef"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	LastCheckedAt   string `json:"lastCheckedAt"`
	LastCheckError  string `json:"lastCheckError"`
}

// blueprintRequest issues an authenticated, project-scoped request and returns
// the response body, treating any non-2xx status as an error.
func blueprintRequest(c *client.Client, method, path string, body []byte) ([]byte, error) {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(context.Background(), method, path, bytes.NewReader(body))
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, path, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.SDK.Do(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach server: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func printAppliedUpdateStatus(cmd *cobra.Command, c *client.Client, out io.Writer) error {
	data, err := blueprintRequest(c, http.MethodGet, c.BaseURL()+"/api/blueprints/applied", nil)
	if err != nil {
		return err
	}
	var applied []appliedBlueprintStatus
	if err := json.Unmarshal(data, &applied); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(applied) == 0 {
		fmt.Fprintln(out, "No applied blueprints.")
		return nil
	}
	fmt.Fprintln(out, "Applied blueprints:")
	for _, a := range applied {
		status := "up to date"
		if a.UpdateAvailable {
			status = "update available"
			if a.LatestVersion != "" {
				status += " (latest " + a.LatestVersion + ")"
			}
		}
		line := fmt.Sprintf("  %-24s %-12s %s", a.Name, a.Version, status)
		if a.SourceURL != "" {
			line += "  [" + a.SourceURL + "]"
		}
		fmt.Fprintln(out, line)
	}
	return nil
}

func triggerBlueprintCheck(cmd *cobra.Command, c *client.Client, name string) error {
	_, err := blueprintRequest(c, http.MethodPost,
		c.BaseURL()+"/api/blueprints/"+url.PathEscape(name)+"/updates/check", nil)
	return err
}

func printSingleUpdateStatus(cmd *cobra.Command, c *client.Client, name string, out io.Writer) error {
	data, err := blueprintRequest(c, http.MethodGet,
		c.BaseURL()+"/api/blueprints/"+url.PathEscape(name)+"/updates", nil)
	if err != nil {
		return err
	}
	var check blueprintUpdateCheckStatus
	if err := json.Unmarshal(data, &check); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	status := "up to date"
	if check.UpdateAvailable {
		status = "update available"
	}
	if check.LastCheckError != "" {
		status = "last check failed: " + check.LastCheckError
	}
	fmt.Fprintf(out, "%s: %s\n", check.BlueprintName, status)
	if check.LatestVersion != "" {
		fmt.Fprintf(out, "  latest version: %s\n", check.LatestVersion)
	}
	if check.SourceURL != "" {
		fmt.Fprintf(out, "  source: %s\n", check.SourceURL)
	}
	return nil
}

// ─────────────────────────────────────────────
// blueprintsUpdateCmd — apply an available update
// ─────────────────────────────────────────────

var blueprintsUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Apply an available update for a GitHub-imported blueprint",
	Long: `Enqueue an asynchronous update (apply) for the current project's
GitHub-imported blueprint. The server fetches the upstream source and, when a
newer version is available, creates, publishes, and applies it. With --wait, the
command blocks until the update job reaches a terminal state and prints the
outcome.

Before enqueuing, the command previews object type names already registered in
the project. If the upstream pack collides with types from a different pack, the
property-level conflicts are printed and an interactive confirmation (or
--merge-types for non-interactive use) is required.

Examples:

  memory blueprints update crm
  memory blueprints update crm --wait
  memory blueprints update crm --merge-types
  memory blueprints update crm --project my-project`,
	Args: cobra.ExactArgs(1),
	RunE: runBlueprintsUpdate,
}

type blueprintUpdateJobStatus struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId"`
	BlueprintName string          `json:"blueprintName"`
	Action        string          `json:"action"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	LastError     string          `json:"lastError"`
	Result        json.RawMessage `json:"result"`
	CreatedAt     string          `json:"createdAt"`
	StartedAt     string          `json:"startedAt"`
	FinishedAt    string          `json:"finishedAt"`
}

type blueprintUpdateStatus struct {
	BlueprintName   string                    `json:"blueprintName"`
	UpdateAvailable bool                      `json:"updateAvailable"`
	LatestVersion   string                    `json:"latestVersion"`
	LastCheckError  string                    `json:"lastCheckError"`
	LatestJob       *blueprintUpdateJobStatus `json:"latestJob"`
}

// blueprintUpdatePreview is the response of POST .../updates/preview.
type blueprintUpdatePreview struct {
	BlueprintName  string                            `json:"blueprintName"`
	CurrentVersion string                            `json:"currentVersion"`
	LatestVersion  string                            `json:"latestVersion"`
	RequiresMerge  bool                              `json:"requiresMerge"`
	Conflicts      []sdkschemas.MergePreviewConflict `json:"conflicts"`
}

func runBlueprintsUpdate(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	projectID, err := resolveProjectContext(cmd, blueprintsProjectFlag)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	name := args[0]
	out := cmd.OutOrStdout()

	// Read-only preflight before enqueuing: report object-type collisions and
	// require explicit merge confirmation.
	previewPath := c.BaseURL() + "/api/blueprints/" + url.PathEscape(name) + "/updates/preview"
	data, err := blueprintRequest(c, http.MethodPost, previewPath, nil)
	if err != nil {
		return err
	}
	var preview blueprintUpdatePreview
	if err := json.Unmarshal(data, &preview); err != nil {
		return fmt.Errorf("decode update preview: %w", err)
	}

	mergeTypes := blueprintsMergeTypesFlag
	if len(preview.Conflicts) > 0 {
		blueprints.PrintMergeConflicts(out, preview.Conflicts)
		if !mergeTypes {
			if err := confirmMergeTypeConflicts(cmd, preview.Conflicts); err != nil {
				return err
			}
			mergeTypes = true
		}
	}

	var applyBody []byte
	if mergeTypes {
		applyBody, err = json.Marshal(map[string]bool{"mergeTypeConflicts": true})
		if err != nil {
			return fmt.Errorf("marshal update request: %w", err)
		}
	}
	applyPath := c.BaseURL() + "/api/blueprints/" + url.PathEscape(name) + "/updates/apply"
	if _, err := blueprintRequest(c, http.MethodPost, applyPath, applyBody); err != nil {
		return err
	}

	if !blueprintsUpdateWaitFlag {
		fmt.Fprintf(out, "Update queued for %s.\n", name)
		return nil
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	statusPath := c.BaseURL() + "/api/blueprints/" + url.PathEscape(name) + "/updates"
	for {
		data, err := blueprintRequest(c, http.MethodGet, statusPath, nil)
		if err != nil {
			return err
		}
		var status blueprintUpdateStatus
		if err := json.Unmarshal(data, &status); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if status.LatestJob == nil {
			fmt.Fprintln(out, "No update job recorded yet.")
			return nil
		}
		switch status.LatestJob.Status {
		case "succeeded":
			printUpdateJobOutcome(out, name, status.LatestJob)
			return nil
		case "failed":
			printUpdateJobConflicts(out, status.LatestJob)
			fmt.Fprintf(out, "%s: update failed: %s\n", name, status.LatestJob.LastError)
			return fmt.Errorf("blueprint update failed: %s", status.LatestJob.LastError)
		default:
			<-ticker.C
		}
	}
}

// printUpdateJobConflicts surfaces object-type conflict details persisted on a
// failed update job result.
func printUpdateJobConflicts(out io.Writer, job *blueprintUpdateJobStatus) {
	if job == nil || len(job.Result) == 0 {
		return
	}
	var result struct {
		Conflicts []sdkschemas.MergePreviewConflict `json:"conflicts"`
	}
	if err := json.Unmarshal(job.Result, &result); err != nil {
		return
	}
	if len(result.Conflicts) > 0 {
		blueprints.PrintMergeConflicts(out, result.Conflicts)
		_, _ = fmt.Fprintln(out, "Re-run with --merge-types to confirm the merge.")
	}
}

func printUpdateJobOutcome(out io.Writer, name string, job *blueprintUpdateJobStatus) {
	var result struct {
		Noop            bool   `json:"noop"`
		Version         string `json:"version"`
		PreviousVersion string `json:"previousVersion"`
		Reason          string `json:"reason"`
	}
	if len(job.Result) > 0 {
		_ = json.Unmarshal(job.Result, &result)
	}
	if result.Noop {
		msg := "up to date"
		if result.Reason != "" {
			msg = result.Reason
		}
		fmt.Fprintf(out, "%s: %s\n", name, msg)
		return
	}
	fmt.Fprintf(out, "%s: updated to version %s\n", name, result.Version)
}

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
// blueprintsInspectCmd — view blueprint contents
// ─────────────────────────────────────────────

var blueprintsInspectCmd = &cobra.Command{
	Use:   "inspect <source>",
	Short: "List the contents of a Blueprint directory or GitHub URL",
	Long: `Inspect a Blueprint directory (or GitHub repository) and display a detailed
listing of its contents without making any API calls or modifying the project.

Shows all discovered packs (with object and relationship type listings), agents,
skills, and seed data counts. Useful for reviewing what a blueprint contains
before installing it.

Examples:

  memory blueprints inspect ./my-config
  memory blueprints inspect https://github.com/acme/memory-blueprints
  memory blueprints inspect https://github.com/acme/memory-blueprints#v1.2.0`,
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

		// Print load errors first.
		for _, r := range loadResults {
			if r.Action == blueprints.BlueprintsActionError {
				fmt.Fprintf(out, "  warning  %s %q: %v\n", r.ResourceType, r.Name, r.Error)
			}
		}

		if projectFile == nil && len(packs) == 0 && len(agents) == 0 && len(skills) == 0 && len(seedObjects) == 0 && len(seedRels) == 0 {
			fmt.Fprintln(out, "Nothing found — no blueprint files detected.")
			return nil
		}

		fmt.Fprintln(out)
		blueprints.PrintInspect(out, projectFile, packs, agents, skills, seedObjects, seedRels)
		return nil
	},
}

// ─────────────────────────────────────────────
// init — wire flags and register
// ─────────────────────────────────────────────

// blueprintsGithubTokenSetCmd stores a project-scoped GitHub PAT (encrypted at
// rest by the server; never echoed back).
var blueprintsGithubTokenSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Store the project's GitHub personal access token",
	Long: `Store the project's GitHub personal access token, used by the server to check
private repositories for updates. The token is encrypted at rest by the server
and never echoed back.

The token is read from --token, or from stdin when --token is omitted (e.g.
` + "`echo ghp_... | memory blueprints github-token set`" + `).

Examples:

  memory blueprints github-token set --token ghp_...
  memory blueprints github-token set`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := strings.TrimSpace(blueprintsGithubTokenFlag)
		if token == "" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read token from stdin: %w", err)
			}
			token = strings.TrimSpace(string(data))
		}
		if token == "" {
			return fmt.Errorf("token is required (use --token or pipe it via stdin)")
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		projectID, err := resolveProjectContext(cmd, blueprintsProjectFlag)
		if err != nil {
			return err
		}
		c.SetContext("", projectID)

		body, err := json.Marshal(map[string]string{"token": token})
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		if _, err := blueprintRequest(c, http.MethodPut,
			c.BaseURL()+"/api/blueprints/github-credentials", body); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "GitHub token stored.")
		return nil
	},
}

var blueprintsGithubTokenCmd = &cobra.Command{
	Use:   "github-token",
	Short: "Manage the project's GitHub personal access token",
	Long:  `Manage the project-scoped GitHub personal access token used for update checks.`,
}

func init() {
	// --project is persistent so it's inherited by all subcommands
	blueprintsCmd.PersistentFlags().StringVar(&blueprintsProjectFlag, "project", "", "Project ID or name (overrides config/env)")

	// install subcommand flags
	blueprintsInstallCmd.Flags().BoolVar(&blueprintsUpgradeFlag, "upgrade", false, "Update existing resources instead of skipping them")
	blueprintsInstallCmd.Flags().BoolVar(&blueprintsDryRunFlag, "dry-run", false, "Preview actions without making any API calls")
	blueprintsInstallCmd.Flags().BoolVar(&blueprintsMergeTypesFlag, "merge-types", false, "Merge incoming object types into existing registered types (required non-interactively on a type-name collision)")
	blueprintsInstallCmd.Flags().StringVar(&blueprintsTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN")
	blueprintsInstallCmd.Flags().StringArrayVar(&blueprintsMapModelFlag, "map-model", nil, "Map a blueprint agent to a project model (agent=provider/model, repeatable; empty model = project default)")
	blueprintsInstallCmd.Flags().BoolVar(&blueprintsNoMapFlag, "no-map", false, "Skip install-time model mapping and use the blueprint's pinned models as-is")

	// deprecated bare form also needs these flags
	blueprintsCmd.Flags().BoolVar(&blueprintsUpgradeFlag, "upgrade", false, "Update existing resources instead of skipping them")
	blueprintsCmd.Flags().BoolVar(&blueprintsDryRunFlag, "dry-run", false, "Preview actions without making any API calls")
	blueprintsCmd.Flags().BoolVar(&blueprintsMergeTypesFlag, "merge-types", false, "Merge incoming object types into existing registered types (required non-interactively on a type-name collision)")
	blueprintsCmd.Flags().StringVar(&blueprintsTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN")
	blueprintsCmd.Flags().StringArrayVar(&blueprintsMapModelFlag, "map-model", nil, "Map a blueprint agent to a project model (agent=provider/model, repeatable; empty model = project default)")
	blueprintsCmd.Flags().BoolVar(&blueprintsNoMapFlag, "no-map", false, "Skip install-time model mapping and use the blueprint's pinned models as-is")

	// dump subcommand flags
	blueprintsDumpCmd.Flags().StringVar(&blueprintsDumpTypesFlag, "types", "", "Comma-separated list of object/relationship types to export (default: all types)")

	// validate subcommand flags
	blueprintsValidateCmd.Flags().StringVar(&blueprintsTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN")
	blueprintsValidateCmd.Flags().BoolVar(&blueprintsStrictFlag, "strict", false, "Treat warnings as failures (exit 1)")

	// inspect subcommand flags
	blueprintsInspectCmd.Flags().StringVar(&blueprintsTokenFlag, "token", "", "GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN")

	blueprintsCmd.AddCommand(blueprintsInstallCmd)
	blueprintsCmd.AddCommand(blueprintsDumpCmd)
	blueprintsCmd.AddCommand(blueprintsValidateCmd)
	blueprintsCmd.AddCommand(blueprintsInspectCmd)
	blueprintsCmd.AddCommand(blueprintsCheckUpdatesCmd)
	blueprintsCmd.AddCommand(blueprintsUpdateCmd)

	blueprintsGithubTokenCmd.AddCommand(blueprintsGithubTokenSetCmd)
	blueprintsGithubTokenSetCmd.Flags().StringVar(&blueprintsGithubTokenFlag, "token", "", "GitHub personal access token (read from stdin when omitted)")
	blueprintsCmd.AddCommand(blueprintsGithubTokenCmd)

	// update subcommand flags
	blueprintsUpdateCmd.Flags().BoolVar(&blueprintsUpdateWaitFlag, "wait", false, "Block until the update job reaches a terminal state")
	blueprintsUpdateCmd.Flags().BoolVar(&blueprintsMergeTypesFlag, "merge-types", false, "Merge incoming object types into existing registered types (required non-interactively on a type-name collision)")

	rootCmd.AddCommand(blueprintsCmd)
}
