package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcpregistry"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var builtinToolsCmd = &cobra.Command{
	Use:   "builtin-tools",
	Short: "Manage built-in tools",
	Long: `Commands for managing built-in (Go-native) tools in the Memory platform.

Built-in tools are implemented directly in the server and are available to all
agents without requiring an external MCP server connection. Examples include
query_entities, brave_web_search, webfetch, and create_document.

Use 'memory agents mcp-servers' to manage externally-connected MCP servers.`,
}

var listBuiltinToolsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all built-in tools",
	Long: `List all built-in tools registered for the current project.

Prints each tool's enabled/disabled state, name, and description. Tools that
require runtime configuration (e.g. API keys) are shown with their config status.
The 'Source' column shows where the effective settings come from: project, org,
or global (server default).`,
	RunE: runListBuiltinTools,
}

var toggleBuiltinToolCmd = &cobra.Command{
	Use:   "toggle [tool-id] [on|off]",
	Short: "Enable or disable a built-in tool",
	Long: `Enable or disable a built-in tool for the current project.

The tool-id is the UUID shown in 'memory agents builtin-tools list'.

Examples:
  memory agents builtin-tools toggle <tool-id> off
  memory agents builtin-tools toggle <tool-id> on`,
	Args: cobra.ExactArgs(2),
	RunE: runToggleBuiltinTool,
}

var configureBuiltinToolCmd = &cobra.Command{
	Use:   "configure [tool-name] [key=value ...]",
	Short: "Set runtime config for a built-in tool",
	Long: `Set runtime configuration key/value pairs for a named built-in tool.

Looks up the tool by name and patches its config. Only the provided keys are
updated; existing keys not mentioned are left unchanged.

Examples:
  memory agents builtin-tools configure brave_web_search api_key=YOUR_KEY
  memory agents builtin-tools configure reddit_search client_id=ID client_secret=SECRET`,
	Args: cobra.MinimumNArgs(2),
	RunE: runConfigureBuiltinTool,
}

func runListBuiltinTools(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.MCPRegistry.ListBuiltinTools(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list built-in tools: %w", err)
	}

	tools := result.Data
	if len(tools) == 0 {
		fmt.Println("No built-in tools found.")
		return nil
	}

	// Detect terminal width for description wrapping; fall back to 80.
	termWidth := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termWidth = w
	}

	fmt.Printf("Found %d built-in tool(s):\n\n", len(tools))

	// Field label width (including trailing space) — all lines share the same indent.
	const indent = "  "
	const labelName = "Name:    "
	const labelEnabled = "Enabled: "
	const labelID = "ID:      "
	const labelSource = "Source:  "
	const labelDesc = "Desc:    "
	descIndent := indent + strings.Repeat(" ", len(labelDesc))

	for _, t := range tools {
		checkbox := "[✓]"
		if !t.Enabled {
			checkbox = "[ ]"
		}
		suffix := toolConfigSuffix(t.ConfigKeys, t.Config)
		source := t.InheritedFrom
		if source == "" {
			source = "global"
		}
		fmt.Printf("%s%s%s%s\n", indent, labelName, t.ToolName, suffix)
		fmt.Printf("%s%s%s\n", indent, labelEnabled, checkbox)
		fmt.Printf("%s%s%s\n", indent, labelID, t.ID)
		fmt.Printf("%s%s%s\n", indent, labelSource, source)
		if t.Description != nil && *t.Description != "" {
			avail := termWidth - len(indent) - len(labelDesc)
			if avail < 20 {
				avail = 20
			}
			lines := wrapText(*t.Description, avail)
			if len(lines) > 2 {
				if len(lines[1]) > avail-3 {
					lines[1] = lines[1][:avail-3] + "..."
				} else {
					lines[1] = lines[1] + "..."
				}
				lines = lines[:2]
			}
			for j, l := range lines {
				if j == 0 {
					fmt.Printf("%s%s%s\n", indent, labelDesc, l)
				} else {
					fmt.Printf("%s%s\n", descIndent, l)
				}
			}
		}
		fmt.Println()
	}

	return nil
}

func runToggleBuiltinTool(cmd *cobra.Command, args []string) error {
	toolID := args[0]
	state := strings.ToLower(args[1])

	if state != "on" && state != "off" {
		return fmt.Errorf("state must be 'on' or 'off', got %q", args[1])
	}

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	enabled := state == "on"
	_, err = c.SDK.MCPRegistry.UpdateBuiltinTool(context.Background(), toolID, &mcpregistry.UpdateBuiltinToolRequest{
		Enabled: &enabled,
	})
	if err != nil {
		return fmt.Errorf("failed to update built-in tool: %w", err)
	}

	fmt.Printf("Tool %s is now %s.\n", toolID, state)
	return nil
}

func runConfigureBuiltinTool(cmd *cobra.Command, args []string) error {
	toolName := args[0]
	kvPairs := args[1:]

	config := make(map[string]any, len(kvPairs))
	for _, kv := range kvPairs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid key=value pair %q (expected KEY=VALUE)", kv)
		}
		config[parts[0]] = parts[1]
	}

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	// Find the tool ID by name from the builtin-tools list.
	listResult, err := c.SDK.MCPRegistry.ListBuiltinTools(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list built-in tools: %w", err)
	}

	var foundToolID string
	for _, t := range listResult.Data {
		if t.ToolName == toolName {
			foundToolID = t.ID
			break
		}
	}
	if foundToolID == "" {
		return fmt.Errorf("built-in tool %q not found for this project", toolName)
	}

	_, err = c.SDK.MCPRegistry.UpdateBuiltinTool(context.Background(), foundToolID, &mcpregistry.UpdateBuiltinToolRequest{
		Config: config,
	})
	if err != nil {
		return fmt.Errorf("failed to configure built-in tool: %w", err)
	}

	fmt.Printf("Built-in tool %q configured successfully.\n", toolName)
	fmt.Printf("  Tool ID:  %s\n", foundToolID)
	fmt.Printf("  Keys set: %s\n", strings.Join(keysOf(config), ", "))
	return nil
}

func init() {
	builtinToolsCmd.AddCommand(listBuiltinToolsCmd)
	builtinToolsCmd.AddCommand(toggleBuiltinToolCmd)
	builtinToolsCmd.AddCommand(configureBuiltinToolCmd)

	// Register under agents command
	agentsCmd.AddCommand(builtinToolsCmd)
}

// wrapText splits text into lines of at most maxWidth runes, breaking on word
// boundaries. It returns at least one element even for empty input.
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) <= maxWidth {
			line += " " + w
		} else {
			lines = append(lines, line)
			line = w
		}
	}
	lines = append(lines, line)
	return lines
}
