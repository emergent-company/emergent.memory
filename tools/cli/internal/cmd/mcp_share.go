package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcp"
	"github.com/spf13/cobra"
)

var (
	mcpShareName   string
	mcpShareEmails []string
	mcpShareJSON   bool
)

var mcpShareCmd = &cobra.Command{
	Use:   "share-mcp",
	Short: "Share read-only MCP access with a teammate or AI agent",
	Long: `Generate a read-only API token for your project and print ready-to-use
MCP config snippets for Claude Desktop, Cursor, and OpenCode.

The token is scoped to data:read, schema:read, agents:read, and projects:read.
Write operations will be rejected. The token can be revoked at any time with:

  memory tokens revoke <token-id> --project <project>

Examples:
  memory projects share-mcp --project my-project
  memory projects share-mcp --project my-project --name "Alice read-only"
  memory projects share-mcp --project my-project --email alice@example.com --email bob@example.com`,
	RunE: runMCPShare,
}

func runMCPShare(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectContext(cmd, mcpShareProjectID)
	if err != nil {
		return err
	}

	req := mcp.ShareRequest{
		Name:   mcpShareName,
		Emails: mcpShareEmails,
	}

	result, err := c.SDK.MCP.Share(context.Background(), projectID, req)
	if err != nil {
		return fmt.Errorf("failed to share MCP access: %w", err)
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	if mcpShareJSON {
		out, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	// Email mode — compact summary only, the guide is in the email
	if len(mcpShareEmails) > 0 {
		fmt.Println()
		fmt.Println("MCP invite sent!")
		fmt.Println()
		fmt.Printf("  To:        %s\n", strings.Join(mcpShareEmails, ", "))
		fmt.Printf("  Project:   %s\n", result.ProjectID)
		fmt.Printf("  MCP URL:   %s\n", result.MCPURL)
		fmt.Printf("  Scopes:    data:read, schema:read, agents:read, projects:read\n")
		fmt.Printf("  Token:     %s\n", result.Token)
		fmt.Printf("  Generated: %s\n", now)
		fmt.Printf("  Email sent: %s\n", now)
		fmt.Println()
		fmt.Println("  The email contains the API key and setup instructions for")
		fmt.Println("  Claude Desktop and Cursor.")
		fmt.Println()
		fmt.Printf("  Revoke:  memory tokens revoke <token-id> --project %s\n", projectID)
		fmt.Println()
		return nil
	}

	// No email — print full guide to terminal
	fmt.Println()
	fmt.Println("MCP access token created!")
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
	fmt.Printf("  │  Token:  %-52s│\n", result.Token)
	fmt.Println("  │  Save this — it will not be shown again.                    │")
	fmt.Println("  └─────────────────────────────────────────────────────────────┘")
	fmt.Println()
	fmt.Printf("  MCP URL:  %s\n", result.MCPURL)
	fmt.Println()

	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Println("Claude Desktop  (~/.config/Claude/claude_desktop_config.json)")
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Println()
	printMCPClientConfig(result.MCPURL, result.Token)

	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Println("Cursor  (Settings → MCP)")
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Println()
	printMCPClientConfig(result.MCPURL, result.Token)

	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Println("OpenCode  (opencode.json in project root)")
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Println()
	openCodeConfig := map[string]interface{}{
		"mcp": map[string]interface{}{
			"memory": map[string]interface{}{
				"type":    "remote",
				"url":     result.MCPURL,
				"headers": map[string]string{"X-API-Key": result.Token},
				"enabled": true,
			},
		},
	}
	out, _ := json.MarshalIndent(openCodeConfig, "", "  ")
	fmt.Println(string(out))
	fmt.Println()

	fmt.Printf("  Revoke:  memory tokens revoke <token-id> --project %s\n", projectID)
	fmt.Println()

	return nil
}

func printMCPClientConfig(mcpURL, token string) {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"memory": map[string]interface{}{
				"url":     mcpURL,
				"headers": map[string]string{"X-API-Key": token},
			},
		},
	}
	out, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Println(string(out))
}

var mcpShareProjectID string

func init() {
	mcpShareCmd.Flags().StringVar(&mcpShareProjectID, "project", "", "Project name or ID (required if not set in config)")
	mcpShareCmd.Flags().StringVar(&mcpShareName, "name", "", "Display name for the token (default: auto-generated)")
	mcpShareCmd.Flags().StringArrayVar(&mcpShareEmails, "email", []string{}, "Email address to invite (can be repeated)")
	mcpShareCmd.Flags().BoolVar(&mcpShareJSON, "json", false, "Output raw JSON response")

	projectsCmd.AddCommand(mcpShareCmd)
}
