package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcpimport"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpconfig"
	"github.com/spf13/cobra"
)

// newMCPCmd builds the `mcp` command group.
func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "manage hosted MCP servers",
		Long:  "Manage the MCP servers this connector hosts. Use 'memory-connector mcp import --help' for the Claude-config import.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "memory-connector mcp: unknown command %q\n", args[0])
				return exitError{code: 2}
			}
			return cmd.Help()
		},
	}
	imp := &cobra.Command{
		Use:   "import",
		Short: "import servers from a Claude MCP config",
		Long: `Import MCP servers from a Claude-format config document into this connector's local config.

Accepts the mcpServers object used by Claude Desktop (claude_desktop_config.json),
Claude Code (.mcp.json / ~/.claude.json), and 'claude mcp add-json'. Existing
servers are skipped unless --overwrite is set; an overwrite keeps the existing
enabled state and disabled_tools.

The import refuses while the connector is running (its config lock is held):
use the connector app or its loopback API to import into a live connector.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResult(runMCPImport(stdlibArgs(cmd, args), cmd.OutOrStdout(), cmd.ErrOrStderr()))
		},
	}
	imp.Flags().String("config", config.DefaultConfigPath(), "config file path")
	imp.Flags().String("from-claude", "", "Claude config file path, or - for stdin (required)")
	imp.Flags().Bool("overwrite", false, "update servers whose name already exists instead of skipping them")
	imp.Flags().Bool("dry-run", false, "validate and plan the import without writing")
	imp.Flags().Bool("json", false, "emit the import result as JSON")
	cmd.AddCommand(imp)
	return cmd
}

// runMCPImport imports a Claude-format config into the connector's local YAML
// config. It refuses while another connector process holds the config lock:
// only that process can apply a live reload, and two writers would race the
// file. The connector app (or the loopback API) imports into a running
// connector.
func runMCPImport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.String("config", config.DefaultConfigPath(), "config file path")
	fs.String("from-claude", "", "Claude config file path, or - for stdin")
	fs.Bool("overwrite", false, "overwrite existing servers")
	fs.Bool("dry-run", false, "validate without writing")
	fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	source := fs.Lookup("from-claude").Value.String()
	if strings.TrimSpace(source) == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector mcp import: --from-claude is required")
		fs.Usage()
		return 2
	}
	configPath := fs.Lookup("config").Value.String()
	overwrite := fs.Lookup("overwrite").Value.String() == "true"
	dryRun := fs.Lookup("dry-run").Value.String() == "true"
	asJSON := fs.Lookup("json").Value.String() == "true"

	release, err := acquireLock(configPath + ".lock")
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: the connector is running; import from the connector app or its loopback API instead (lock held: %s)\n", configPath+".lock")
		} else {
			_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: %v\n", err)
		}
		return 1
	}
	defer release()

	cfg, err := config.Load(configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: %v\n", err)
		return 1
	}

	data, err := readImportSource(source)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: %v\n", err)
		return 1
	}
	parsed, err := mcpconfig.ParseClaudeConfig(data)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: %v\n", err)
		return 1
	}

	next, result := mcpimport.Apply(cfg.MCPServers, parsed, overwrite, nil)
	result.DryRun = dryRun

	if !dryRun && result.Changed() {
		if err := mcphost.ValidateServers(next); err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: %v\n", err)
			return 1
		}
		cfg.MCPServers = next
		if err := cfg.Save(configPath); err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: %v\n", err)
			return 1
		}
	}

	if asJSON {
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector mcp import: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, string(b))
	} else {
		printMCPImportResult(stdout, result)
	}

	if !result.Changed() && len(result.Failed) > 0 {
		return 1
	}
	return 0
}

// readImportSource reads a Claude config from a file, or stdin for "-".
func readImportSource(path string) ([]byte, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading Claude config %q: %w", path, err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, fmt.Errorf("Claude config %q is empty", path)
	}
	return b, nil
}

func printMCPImportResult(w io.Writer, result mcpimport.Result) {
	prefix := ""
	if result.DryRun {
		prefix = "[dry run] "
	}
	_, _ = fmt.Fprintf(w, "%sImported MCP servers: %d created, %d updated, %d skipped, %d failed\n",
		prefix, len(result.Created), len(result.Updated), len(result.Skipped), len(result.Failed))
	for _, name := range result.Created {
		_, _ = fmt.Fprintf(w, "  + %s\n", name)
	}
	for _, name := range result.Updated {
		_, _ = fmt.Fprintf(w, "  ~ %s\n", name)
	}
	for _, skip := range result.Skipped {
		_, _ = fmt.Fprintf(w, "  - %s (%s)\n", skip.Name, skip.Reason)
	}
	for _, failure := range result.Failed {
		_, _ = fmt.Fprintf(w, "  ! %s: %s\n", failure.Name, failure.Error)
	}
}
