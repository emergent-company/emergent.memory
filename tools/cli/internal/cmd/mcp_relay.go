package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

// --------------------------------------------------------------------------
// Top-level command
// --------------------------------------------------------------------------

var mcpRelayCmd = &cobra.Command{
	Use:   "mcp-relay",
	Short: "Inspect and interact with connected MCP relay instances",
}

// --------------------------------------------------------------------------
// sessions
// --------------------------------------------------------------------------

var mcpRelaySessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List connected MCP relay instances for the current project",
	RunE:  runMCPRelaySessions,
}

func runMCPRelaySessions(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	url := fmt.Sprintf("%s/api/mcp-relay/sessions?projectId=%s", c.BaseURL(), projectID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", c.AuthorizationHeader())
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Sessions []struct {
			InstanceID  string    `json:"instance_id"`
			Version     string    `json:"version"`
			ToolCount   int       `json:"tool_count"`
			ConnectedAt time.Time `json:"connected_at"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Sessions) == 0 {
		fmt.Println("No relay instances connected.")
		return nil
	}

	fmt.Printf("Connected relay instances (%d):\n\n", len(result.Sessions))
	for i, s := range result.Sessions {
		fmt.Printf("%d. %s\n", i+1, s.InstanceID)
		if s.Version != "" {
			fmt.Printf("   Version:     %s\n", s.Version)
		}
		fmt.Printf("   Tools:       %d\n", s.ToolCount)
		fmt.Printf("   Connected:   %s\n", s.ConnectedAt.Format("2006-01-02 15:04:05"))
		fmt.Println()
	}
	return nil
}

// --------------------------------------------------------------------------
// tools
// --------------------------------------------------------------------------

var mcpRelayToolsCmd = &cobra.Command{
	Use:   "tools <instance-id>",
	Short: "List tools exposed by a connected relay instance",
	Args:  cobra.ExactArgs(1),
	RunE:  runMCPRelayTools,
}

func runMCPRelayTools(cmd *cobra.Command, args []string) error {
	instanceID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	url := fmt.Sprintf("%s/api/mcp-relay/sessions/%s/tools?projectId=%s", c.BaseURL(), instanceID, projectID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", c.AuthorizationHeader())
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("relay instance %q not found or disconnected", instanceID)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	// Pretty-print the raw tools/list JSON returned by the relay instance.
	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		fmt.Println(string(body))
		return nil
	}
	pretty, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(pretty))
	return nil
}

// --------------------------------------------------------------------------
// call
// --------------------------------------------------------------------------

var mcpRelayCallArgsJSON string

var mcpRelayCallCmd = &cobra.Command{
	Use:   "call <instance-id> <tool-name>",
	Short: "Invoke a tool on a connected relay instance",
	Args:  cobra.ExactArgs(2),
	RunE:  runMCPRelayCall,
}

func runMCPRelayCall(cmd *cobra.Command, args []string) error {
	instanceID := args[0]
	toolName := args[1]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	// Parse optional arguments JSON.
	var arguments map[string]any
	if mcpRelayCallArgsJSON != "" {
		if err := json.Unmarshal([]byte(mcpRelayCallArgsJSON), &arguments); err != nil {
			return fmt.Errorf("invalid --args JSON: %w", err)
		}
	}

	payload := map[string]any{
		"name":      toolName,
		"arguments": arguments,
	}
	bodyBytes, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/api/mcp-relay/sessions/%s/call?projectId=%s", c.BaseURL(), instanceID, projectID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", c.AuthorizationHeader())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("relay instance %q not found or disconnected", instanceID)
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &e)
		return fmt.Errorf("tool call failed: %s", e.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		fmt.Println(string(body))
		return nil
	}
	pretty, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(pretty))
	return nil
}

// --------------------------------------------------------------------------
// init
// --------------------------------------------------------------------------

func init() {
	mcpRelayCallCmd.Flags().StringVar(&mcpRelayCallArgsJSON, "args", "", `Tool arguments as a JSON object, e.g. '{"key":"value"}'`)

	mcpRelayCmd.AddCommand(mcpRelaySessionsCmd)
	mcpRelayCmd.AddCommand(mcpRelayToolsCmd)
	mcpRelayCmd.AddCommand(mcpRelayCallCmd)

	rootCmd.AddCommand(mcpRelayCmd)
}
