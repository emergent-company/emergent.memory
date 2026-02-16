package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

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
	Long:  "Get detailed information about a specific MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runGetMCPServer,
}

var createMCPServerCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new MCP server",
	Long:  "Register a new MCP server with the specified configuration",
	RunE:  runCreateMCPServer,
}

var deleteMCPServerCmd = &cobra.Command{
	Use:   "delete [server-id]",
	Short: "Delete an MCP server",
	Long:  "Remove an MCP server from your project configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeleteMCPServer,
}

var syncMCPServerCmd = &cobra.Command{
	Use:   "sync [server-id]",
	Short: "Sync tools from an MCP server",
	Long:  "Refresh the list of available tools by connecting to the MCP server",
	Args:  cobra.ExactArgs(1),
	RunE:  runSyncMCPServer,
}

var (
	mcpServerName        string
	mcpServerType        string
	mcpServerURL         string
	mcpServerDescription string
	mcpServerEnvVars     []string
)

// MCPServer represents an MCP server configuration
type MCPServer struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"project_id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	URL         string            `json:"url"`
	Description string            `json:"description,omitempty"`
	EnvVars     map[string]string `json:"env_vars,omitempty"`
	IsEnabled   bool              `json:"is_enabled"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// MCPServerTool represents a tool provided by an MCP server
type MCPServerTool struct {
	ID          string `json:"id"`
	ServerID    string `json:"server_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsEnabled   bool   `json:"is_enabled"`
}

func parseEnvVars(envVarStrs []string) (map[string]string, error) {
	envVars := make(map[string]string)
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
	fmt.Println("MCP Servers command (list) - TO BE IMPLEMENTED")
	fmt.Println("The MCP registry API is ready at /api/admin/mcp-servers")
	fmt.Println("SDK integration needed - for now, use the API directly")
	return nil
}

func runGetMCPServer(cmd *cobra.Command, args []string) error {
	serverID := args[0]
	fmt.Printf("MCP Server (get %s) - TO BE IMPLEMENTED\n", serverID)
	fmt.Println("The MCP registry API is ready at /api/admin/mcp-servers/:id")
	fmt.Println("SDK integration needed - for now, use the API directly")
	return nil
}

func runCreateMCPServer(cmd *cobra.Command, args []string) error {
	if mcpServerName == "" {
		return fmt.Errorf("server name is required. Use --name flag")
	}
	if mcpServerType == "" {
		return fmt.Errorf("server type is required. Use --type flag (e.g., 'sse', 'stdio')")
	}
	if mcpServerURL == "" {
		return fmt.Errorf("server URL is required. Use --url flag")
	}

	fmt.Printf("Create MCP Server: %s (%s) - TO BE IMPLEMENTED\n", mcpServerName, mcpServerType)
	fmt.Printf("URL: %s\n", mcpServerURL)
	fmt.Println("\nThe MCP registry API is ready at /api/admin/mcp-servers")
	fmt.Println("SDK integration needed - for now, use the API directly")

	// Show what the payload would be
	envVars, err := parseEnvVars(mcpServerEnvVars)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"name":        mcpServerName,
		"type":        mcpServerType,
		"url":         mcpServerURL,
		"description": mcpServerDescription,
		"env_vars":    envVars,
	}

	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Printf("\nPayload that would be sent:\n%s\n", string(payloadJSON))

	return nil
}

func runDeleteMCPServer(cmd *cobra.Command, args []string) error {
	serverID := args[0]
	fmt.Printf("Delete MCP Server %s - TO BE IMPLEMENTED\n", serverID)
	fmt.Println("The MCP registry API is ready at /api/admin/mcp-servers/:id")
	fmt.Println("SDK integration needed - for now, use the API directly")
	return nil
}

func runSyncMCPServer(cmd *cobra.Command, args []string) error {
	serverID := args[0]
	fmt.Printf("Sync MCP Server %s - TO BE IMPLEMENTED\n", serverID)
	fmt.Println("The MCP registry API is ready at /api/admin/mcp-servers/:id/sync")
	fmt.Println("SDK integration needed - for now, use the API directly")
	return nil
}

func init() {
	// Create command flags
	createMCPServerCmd.Flags().StringVar(&mcpServerName, "name", "", "Server name (required)")
	createMCPServerCmd.Flags().StringVar(&mcpServerType, "type", "", "Server type: 'sse' or 'stdio' (required)")
	createMCPServerCmd.Flags().StringVar(&mcpServerURL, "url", "", "Server URL (required)")
	createMCPServerCmd.Flags().StringVar(&mcpServerDescription, "description", "", "Server description")
	createMCPServerCmd.Flags().StringSliceVar(&mcpServerEnvVars, "env", []string{}, "Environment variables (KEY=VALUE format, can be specified multiple times)")

	_ = createMCPServerCmd.MarkFlagRequired("name")
	_ = createMCPServerCmd.MarkFlagRequired("type")
	_ = createMCPServerCmd.MarkFlagRequired("url")

	// Add subcommands
	mcpServersCmd.AddCommand(listMCPServersCmd)
	mcpServersCmd.AddCommand(getMCPServerCmd)
	mcpServersCmd.AddCommand(createMCPServerCmd)
	mcpServersCmd.AddCommand(deleteMCPServerCmd)
	mcpServersCmd.AddCommand(syncMCPServerCmd)

	// Register with root command
	rootCmd.AddCommand(mcpServersCmd)
}
