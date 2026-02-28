package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/mcpregistry"
	"github.com/spf13/cobra"
)

var mcpServersCmd = &cobra.Command{
	Use:   "mcp-servers",
	Short: "Manage MCP servers",
	Long:  "Commands for managing Model Context Protocol (MCP) servers in the Emergent platform",
}

var listMCPServersCmd = &cobra.Command{
	Use:   "list",
	Short: "List all MCP servers",
	Long:  "List all MCP servers configured for the current project",
	RunE:  runListMCPServers,
}

var getMCPServerCmd = &cobra.Command{
	Use:   "get [server-id]",
	Short: "Get MCP server details",
	Long:  "Get detailed information about a specific MCP server, including its tools",
	Args:  cobra.ExactArgs(1),
	RunE:  runGetMCPServer,
}

var createMCPServerCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new MCP server",
	Long: `Register a new MCP server with the specified configuration.

Examples:
  emergent-cli mcp-servers create --name "my-server" --type sse --url "http://localhost:8080/sse"
  emergent-cli mcp-servers create --name "stdio-server" --type stdio --command "npx" --args "-y,@modelcontextprotocol/server-github"
  emergent-cli mcp-servers create --name "my-server" --type http --url "http://localhost:8080/mcp" --env "API_KEY=abc123"`,
	RunE: runCreateMCPServer,
}

var deleteMCPServerCmd = &cobra.Command{
	Use:   "delete [server-id]",
	Short: "Delete an MCP server",
	Long:  "Remove an MCP server and all its tools from your project configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeleteMCPServer,
}

var syncMCPServerCmd = &cobra.Command{
	Use:   "sync [server-id]",
	Short: "Sync tools from an MCP server",
	Long:  "Connect to the MCP server and refresh the list of available tools",
	Args:  cobra.ExactArgs(1),
	RunE:  runSyncMCPServer,
}

var inspectMCPServerCmd = &cobra.Command{
	Use:   "inspect [server-id]",
	Short: "Inspect an MCP server",
	Long:  "Test connection to an MCP server and display its capabilities, tools, prompts, and resources",
	Args:  cobra.ExactArgs(1),
	RunE:  runInspectMCPServer,
}

var toolsMCPServerCmd = &cobra.Command{
	Use:   "tools [server-id]",
	Short: "List tools for an MCP server",
	Long:  "List all tools registered for a specific MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runListMCPServerTools,
}

var (
	mcpServerName        string
	mcpServerType        string
	mcpServerURL         string
	mcpServerCommand     string
	mcpServerArgs        string
	mcpServerDescription string
	mcpServerEnvVars     []string
	mcpServerEnabled     string
)

func parseEnvVars(envVarStrs []string) (map[string]any, error) {
	if len(envVarStrs) == 0 {
		return nil, nil
	}
	envVars := make(map[string]any)
	for _, ev := range envVarStrs {
		parts := strings.SplitN(ev, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid env var format %q (expected KEY=VALUE)", ev)
		}
		envVars[parts[0]] = parts[1]
	}
	return envVars, nil
}

func runListMCPServers(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.MCPRegistry.List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list MCP servers: %w", err)
	}

	servers := result.Data
	if len(servers) == 0 {
		fmt.Println("No MCP servers found.")
		return nil
	}

	fmt.Printf("Found %d MCP server(s):\n\n", len(servers))
	for i, s := range servers {
		enabledStr := "enabled"
		if !s.Enabled {
			enabledStr = "disabled"
		}
		fmt.Printf("%d. %s (%s)\n", i+1, s.Name, enabledStr)
		fmt.Printf("   ID:        %s\n", s.ID)
		fmt.Printf("   Type:      %s\n", s.Type)
		if s.URL != nil && *s.URL != "" {
			fmt.Printf("   URL:       %s\n", *s.URL)
		}
		if s.Command != nil && *s.Command != "" {
			fmt.Printf("   Command:   %s\n", *s.Command)
		}
		fmt.Printf("   Tools:     %d\n", s.ToolCount)
		fmt.Printf("   Created:   %s\n", s.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Println()
	}

	return nil
}

func runGetMCPServer(cmd *cobra.Command, args []string) error {
	serverID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.MCPRegistry.Get(context.Background(), serverID)
	if err != nil {
		return fmt.Errorf("failed to get MCP server: %w", err)
	}

	s := result.Data
	enabledStr := "enabled"
	if !s.Enabled {
		enabledStr = "disabled"
	}

	fmt.Printf("MCP Server: %s (%s)\n", s.Name, enabledStr)
	fmt.Printf("  ID:         %s\n", s.ID)
	fmt.Printf("  Project ID: %s\n", s.ProjectID)
	fmt.Printf("  Type:       %s\n", s.Type)
	if s.URL != nil && *s.URL != "" {
		fmt.Printf("  URL:        %s\n", *s.URL)
	}
	if s.Command != nil && *s.Command != "" {
		fmt.Printf("  Command:    %s\n", *s.Command)
		if len(s.Args) > 0 {
			fmt.Printf("  Args:       %s\n", strings.Join(s.Args, " "))
		}
	}
	if len(s.Env) > 0 {
		fmt.Printf("  Env Vars:   %d configured\n", len(s.Env))
	}
	if len(s.Headers) > 0 {
		fmt.Printf("  Headers:    %d configured\n", len(s.Headers))
	}
	fmt.Printf("  Created:    %s\n", s.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Updated:    %s\n", s.UpdatedAt.Format("2006-01-02 15:04:05"))

	if len(s.Tools) > 0 {
		fmt.Printf("\n  Tools (%d):\n", len(s.Tools))
		for i, t := range s.Tools {
			enabledLabel := "on"
			if !t.Enabled {
				enabledLabel = "off"
			}
			desc := ""
			if t.Description != nil {
				desc = *t.Description
				if len(desc) > 60 {
					desc = desc[:57] + "..."
				}
				desc = " - " + desc
			}
			fmt.Printf("    %d. [%s] %s%s\n", i+1, enabledLabel, t.ToolName, desc)
		}
	} else {
		fmt.Println("\n  No tools synced. Run 'mcp-servers sync' to discover tools.")
	}

	return nil
}

func runCreateMCPServer(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	serverType := mcpregistry.MCPServerType(mcpServerType)

	createReq := &mcpregistry.CreateMCPServerRequest{
		Name: mcpServerName,
		Type: serverType,
	}

	// URL for SSE/HTTP types
	if mcpServerURL != "" {
		createReq.URL = &mcpServerURL
	}

	// Command/args for stdio type
	if mcpServerCommand != "" {
		createReq.Command = &mcpServerCommand
	}
	if mcpServerArgs != "" {
		createReq.Args = strings.Split(mcpServerArgs, ",")
	}

	// Enabled flag
	if mcpServerEnabled != "" {
		val := mcpServerEnabled == "true"
		createReq.Enabled = &val
	}

	// Environment variables
	envVars, err := parseEnvVars(mcpServerEnvVars)
	if err != nil {
		return err
	}
	if envVars != nil {
		createReq.Env = envVars
	}

	result, err := c.SDK.MCPRegistry.Create(context.Background(), createReq)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}

	s := result.Data
	fmt.Println("MCP server created successfully!")
	fmt.Printf("  ID:   %s\n", s.ID)
	fmt.Printf("  Name: %s\n", s.Name)
	fmt.Printf("  Type: %s\n", s.Type)

	return nil
}

func runDeleteMCPServer(cmd *cobra.Command, args []string) error {
	serverID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	err = c.SDK.MCPRegistry.Delete(context.Background(), serverID)
	if err != nil {
		return fmt.Errorf("failed to delete MCP server: %w", err)
	}

	fmt.Printf("MCP server %s deleted successfully.\n", serverID)
	return nil
}

func runSyncMCPServer(cmd *cobra.Command, args []string) error {
	serverID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.MCPRegistry.SyncTools(context.Background(), serverID)
	if err != nil {
		return fmt.Errorf("failed to sync MCP server tools: %w", err)
	}

	if result.Message != nil {
		fmt.Println(*result.Message)
	} else {
		fmt.Println("Tools synced successfully.")
	}

	return nil
}

func runInspectMCPServer(cmd *cobra.Command, args []string) error {
	serverID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.MCPRegistry.Inspect(context.Background(), serverID)
	if err != nil {
		return fmt.Errorf("failed to inspect MCP server: %w", err)
	}

	inspect := result.Data

	fmt.Printf("MCP Server Inspection: %s\n", inspect.ServerName)
	fmt.Printf("  Server ID:  %s\n", inspect.ServerID)
	fmt.Printf("  Type:       %s\n", inspect.ServerType)
	fmt.Printf("  Status:     %s\n", inspect.Status)
	fmt.Printf("  Latency:    %dms\n", inspect.LatencyMs)

	if inspect.Error != nil {
		fmt.Printf("  Error:      %s\n", *inspect.Error)
	}

	if inspect.ServerInfo != nil {
		fmt.Printf("\n  Server Info:\n")
		fmt.Printf("    Name:             %s\n", inspect.ServerInfo.Name)
		fmt.Printf("    Version:          %s\n", inspect.ServerInfo.Version)
		fmt.Printf("    Protocol Version: %s\n", inspect.ServerInfo.ProtocolVersion)
		if inspect.ServerInfo.Instructions != "" {
			fmt.Printf("    Instructions:     %s\n", inspect.ServerInfo.Instructions)
		}
	}

	if inspect.Capabilities != nil {
		fmt.Printf("\n  Capabilities:\n")
		fmt.Printf("    Tools:       %v\n", inspect.Capabilities.Tools)
		fmt.Printf("    Prompts:     %v\n", inspect.Capabilities.Prompts)
		fmt.Printf("    Resources:   %v\n", inspect.Capabilities.Resources)
		fmt.Printf("    Logging:     %v\n", inspect.Capabilities.Logging)
		fmt.Printf("    Completions: %v\n", inspect.Capabilities.Completions)
	}

	if len(inspect.Tools) > 0 {
		fmt.Printf("\n  Tools (%d):\n", len(inspect.Tools))
		for i, t := range inspect.Tools {
			desc := ""
			if t.Description != "" {
				desc = t.Description
				if len(desc) > 60 {
					desc = desc[:57] + "..."
				}
				desc = " - " + desc
			}
			fmt.Printf("    %d. %s%s\n", i+1, t.Name, desc)
		}
	}

	if len(inspect.Prompts) > 0 {
		fmt.Printf("\n  Prompts (%d):\n", len(inspect.Prompts))
		for i, p := range inspect.Prompts {
			fmt.Printf("    %d. %s\n", i+1, p.Name)
			if p.Description != "" {
				fmt.Printf("       %s\n", p.Description)
			}
		}
	}

	if len(inspect.Resources) > 0 {
		fmt.Printf("\n  Resources (%d):\n", len(inspect.Resources))
		for i, r := range inspect.Resources {
			fmt.Printf("    %d. %s (%s)\n", i+1, r.Name, r.URI)
		}
	}

	if len(inspect.ResourceTemplates) > 0 {
		fmt.Printf("\n  Resource Templates (%d):\n", len(inspect.ResourceTemplates))
		for i, rt := range inspect.ResourceTemplates {
			fmt.Printf("    %d. %s (%s)\n", i+1, rt.Name, rt.URITemplate)
		}
	}

	return nil
}

func runListMCPServerTools(cmd *cobra.Command, args []string) error {
	serverID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.MCPRegistry.ListTools(context.Background(), serverID)
	if err != nil {
		return fmt.Errorf("failed to list MCP server tools: %w", err)
	}

	tools := result.Data
	if len(tools) == 0 {
		fmt.Println("No tools found. Run 'mcp-servers sync' to discover tools.")
		return nil
	}

	fmt.Printf("Found %d tool(s):\n\n", len(tools))
	for i, t := range tools {
		enabledLabel := "on"
		if !t.Enabled {
			enabledLabel = "off"
		}
		fmt.Printf("%d. [%s] %s\n", i+1, enabledLabel, t.ToolName)
		fmt.Printf("   ID:       %s\n", t.ID)
		if t.Description != nil && *t.Description != "" {
			fmt.Printf("   Desc:     %s\n", *t.Description)
		}
		if len(t.InputSchema) > 0 {
			schemaJSON, _ := json.MarshalIndent(t.InputSchema, "            ", "  ")
			fmt.Printf("   Schema:   %s\n", string(schemaJSON))
		}
		fmt.Println()
	}

	return nil
}

func init() {
	// Create command flags
	createMCPServerCmd.Flags().StringVar(&mcpServerName, "name", "", "Server name (required)")
	createMCPServerCmd.Flags().StringVar(&mcpServerType, "type", "", "Server type: 'sse', 'stdio', or 'http' (required)")
	createMCPServerCmd.Flags().StringVar(&mcpServerURL, "url", "", "Server URL (for sse/http types)")
	createMCPServerCmd.Flags().StringVar(&mcpServerCommand, "command", "", "Command to run (for stdio type)")
	createMCPServerCmd.Flags().StringVar(&mcpServerArgs, "args", "", "Comma-separated arguments (for stdio type)")
	createMCPServerCmd.Flags().StringVar(&mcpServerDescription, "description", "", "Server description")
	createMCPServerCmd.Flags().StringSliceVar(&mcpServerEnvVars, "env", []string{}, "Environment variables (KEY=VALUE format, can be specified multiple times)")
	createMCPServerCmd.Flags().StringVar(&mcpServerEnabled, "enabled", "", "Enable server (true/false, default: true)")

	_ = createMCPServerCmd.MarkFlagRequired("name")
	_ = createMCPServerCmd.MarkFlagRequired("type")

	// Add subcommands
	mcpServersCmd.AddCommand(listMCPServersCmd)
	mcpServersCmd.AddCommand(getMCPServerCmd)
	mcpServersCmd.AddCommand(createMCPServerCmd)
	mcpServersCmd.AddCommand(deleteMCPServerCmd)
	mcpServersCmd.AddCommand(syncMCPServerCmd)
	mcpServersCmd.AddCommand(inspectMCPServerCmd)
	mcpServersCmd.AddCommand(toolsMCPServerCmd)

	// Register with root command
	rootCmd.AddCommand(mcpServersCmd)
}
