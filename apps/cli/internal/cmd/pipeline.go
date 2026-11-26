package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/completion"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/spf13/cobra"
)

var pipelineCmd = &cobra.Command{
	Use:     "pipeline",
	Short:   "Manage the knowledge pipeline policy",
	GroupID: "account",
	Long: `Get, set and preview a project's knowledge-pipeline policy.

The pipeline governs how schema discovery, auto-apply, backfill and extraction
run without a human in the loop. It has three presets:

  off      discovery does not run automatically; nothing is applied
  suggest  discovery proposes a candidate schema; nothing is applied (default)
  auto     discovery runs, the discovered schema is applied, and documents are
           re-extracted against the new schema

The policy is per-project. Every knob is exposed, but the presets are the
intended starting point; see 'memory pipeline set --help'.`,
}

var (
	pipelineProjectFlag string
	pipelinePolicyFile  string
)

var pipelineGetCmd = &cobra.Command{
	Use:               "get [name-or-id]",
	Short:             "Show the resolved pipeline policy",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runPipelineGet,
}

var pipelineSetCmd = &cobra.Command{
	Use:   "set <off|suggest|auto> [name-or-id]",
	Short: "Set the pipeline mode",
	Long: `Set a project's pipeline mode to one of off, suggest or auto.

Setting a mode resets the full policy to that preset's defaults. To set an
individual knob instead, pass --policy-file with a complete resolved policy
JSON (the shape returned by 'memory pipeline get --json').`,
	Args:              cobra.MaximumNArgs(2),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runPipelineSet,
}

var pipelinePreviewCmd = &cobra.Command{
	Use:               "preview [name-or-id]",
	Short:             "Preview the resolved pipeline policy",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.ProjectNamesCompletionFunc(),
	RunE:              runPipelinePreview,
}

func init() {
	pipelineCmd.AddCommand(pipelineGetCmd, pipelineSetCmd, pipelinePreviewCmd)
	rootCmd.AddCommand(pipelineCmd)

	pipelineCmd.PersistentFlags().StringVar(&pipelineProjectFlag, "project", "", "Project name or ID (uses the configured project if omitted)")
	pipelineSetCmd.Flags().StringVar(&pipelinePolicyFile, "policy-file", "", "Path to a complete resolved policy JSON (use '-' for stdin; overrides the mode)")
}

// resolvePipelineProject returns the target project ID: an explicit positional
// argument wins, then --project, then the configured default.
func resolvePipelineProject(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return resolveProjectContext(cmd, args[0])
	}
	return resolveProjectContext(cmd, pipelineProjectFlag)
}

func runPipelineGet(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	projectID, err := resolvePipelineProject(cmd, args)
	if err != nil {
		return err
	}
	c.ResolveProjectContext(cmd.Context(), projectID)

	view, err := c.SDK.Projects.GetPipeline(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("get pipeline: %w", err)
	}
	printPipelineView(cmd, view)
	return nil
}

func runPipelinePreview(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	projectID, err := resolvePipelineProject(cmd, args)
	if err != nil {
		return err
	}
	c.ResolveProjectContext(cmd.Context(), projectID)

	view, err := c.SDK.Projects.PreviewPipeline(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("preview pipeline: %w", err)
	}
	printPipelineView(cmd, view)
	return nil
}

func runPipelineSet(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && pipelinePolicyFile == "" {
		return fmt.Errorf("a mode (off|suggest|auto) or --policy-file is required")
	}

	var req projects.PutPipelineRequest

	// The project positional differs by invocation: with --policy-file it is the
	// first arg, otherwise the mode is first and the project is second.
	var projectArgs []string

	if pipelinePolicyFile != "" {
		raw, err := readPolicyInput(pipelinePolicyFile)
		if err != nil {
			return fmt.Errorf("read policy: %w", err)
		}
		policy := json.RawMessage(raw)
		req.Policy = &policy
		projectArgs = args
	} else {
		mode := args[0]
		if !isPipelineMode(mode) {
			return fmt.Errorf("mode must be one of off, suggest or auto (got %q)", mode)
		}
		req.Mode = &mode
		projectArgs = args[1:]
	}

	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	projectID, err := resolvePipelineProject(cmd, projectArgs)
	if err != nil {
		return err
	}
	c.ResolveProjectContext(cmd.Context(), projectID)

	view, err := c.SDK.Projects.SetPipeline(context.Background(), projectID, &req)
	if err != nil {
		return fmt.Errorf("set pipeline: %w", err)
	}
	printPipelineView(cmd, view)
	return nil
}

// readPolicyInput reads a policy JSON from a file path, or stdin for "-".
func readPolicyInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// isPipelineMode reports whether m is a valid pipeline preset mode.
func isPipelineMode(m string) bool {
	switch m {
	case "off", "suggest", "auto":
		return true
	default:
		return false
	}
}

func printPipelineView(cmd *cobra.Command, view *projects.PipelineView) {
	if output == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		_ = enc.Encode(view)
		return
	}

	customized := ""
	if view.Customized {
		customized = " (customized)"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Mode: %s%s\n", view.Mode, customized)
	fmt.Fprintf(cmd.OutOrStdout(), "Schema generation: %d\n", view.SchemaGeneration)
	if len(view.Policy) > 0 {
		var buf bytes.Buffer
		_ = json.Indent(&buf, view.Policy, "  ", "  ")
		fmt.Fprintf(cmd.OutOrStdout(), "Policy:\n%s\n", buf.String())
	}
}
