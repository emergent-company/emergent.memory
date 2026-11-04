package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/client"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	mcpsdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/mcp"
	"github.com/spf13/cobra"
)

// ============================================================================
// Commands
//
// This group manages the agent-owned MCP endpoint: the credential an external
// MCP client uses to call one agent. It is NOT the MCP registry
// ('agents mcp-servers'), which manages the external servers an agent calls.
// ============================================================================

var agentMCPEndpointCmd = &cobra.Command{
	Use:   "mcp-endpoint",
	Short: "Manage an agent's MCP endpoint and its keys",
	Long: `Manage the MCP endpoint that an external MCP client uses to call an agent.

The endpoint is agent-owned: one active endpoint per agent, with many labeled
keys. Each key is an independently revocable, rotatable credential. Its secret is
returned exactly once, on create or rotate.

This is the opposite direction of 'memory agents mcp-servers', which manages the
external MCP servers an agent can call.

Examples:
  memory agents mcp-endpoint show my-agent
  memory agents mcp-endpoint create my-agent
  memory agents mcp-endpoint keys create my-agent --label "Claude Desktop"
  memory agents mcp-endpoint keys list my-agent --json
  memory agents mcp-endpoint keys rotate <key-id> --yes
  memory agents mcp-endpoint sessions my-agent --status active
  memory agents mcp-endpoint revoke my-agent --yes`,
}

var agentMCPEndpointShowCmd = &cobra.Command{
	Use:   "show [agent]",
	Short: "Show an agent's MCP endpoint",
	Long: `Show the agent's active MCP endpoint: id, status, agent, creation time, and
the MCP URL to paste into a client. Exits non-zero when the agent has no endpoint.

The agent may be an id, a runtime agent name, or an agent-definition name or
slug; when omitted on a terminal an agent picker is shown.

Use --json for machine-readable output.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentMCPEndpointShow,
}

var agentMCPEndpointCreateCmd = &cobra.Command{
	Use:   "create [agent]",
	Short: "Create an agent's MCP endpoint",
	Long: `Create the single active MCP endpoint owned by an agent.

Fails when the agent already has an active endpoint; revoke the existing one
first. Prints the endpoint id and the MCP URL to paste into a client. Add keys
with 'memory agents mcp-endpoint keys create'.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentMCPEndpointCreate,
}

var agentMCPEndpointRevokeCmd = &cobra.Command{
	Use:   "revoke [agent]",
	Short: "Revoke an agent's MCP endpoint",
	Long: `Revoke the agent's active MCP endpoint and all of its remaining keys,
immediately invalidating their secrets. Prompts for confirmation unless --yes is
passed. Idempotent: revoking an already-revoked endpoint succeeds.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentMCPEndpointRevoke,
}

var agentMCPKeysCmd = &cobra.Command{
	Use:   "keys",
	Short: "Manage an MCP endpoint's labeled keys",
	Long: `Create, list, revoke, and rotate the labeled credentials on an agent's MCP
endpoint. Create and rotate print the raw secret exactly once.`,
}

var agentMCPKeysCreateCmd = &cobra.Command{
	Use:   "create [agent]",
	Short: "Create a labeled key on an endpoint",
	Long: `Mint a labeled credential bound to the agent's MCP endpoint.

The raw secret is printed exactly once and cannot be retrieved later; list shows
only the label, status, and timestamps. Labels are unique per endpoint, ignoring
case.

Examples:
  memory agents mcp-endpoint keys create my-agent --label "Claude Desktop"
  memory agents mcp-endpoint keys create my-agent --label "CI" --expires-at 2026-12-31T00:00:00Z`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentMCPKeysCreate,
}

var agentMCPKeysListCmd = &cobra.Command{
	Use:   "list [agent]",
	Short: "List an endpoint's keys",
	Long: `List the agent's MCP endpoint keys with label, status, creation time, and
last-used time. Secrets are never shown — rotate or recreate a key if a secret
is lost.

Use --json for machine-readable output.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentMCPKeysList,
}

var agentMCPKeysRevokeCmd = &cobra.Command{
	Use:   "revoke <key-id>",
	Short: "Revoke one key",
	Long: `Revoke one key, immediately invalidating its secret without affecting the
endpoint or its other keys. Prompts for confirmation unless --yes is passed.
Idempotent.`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentMCPKeysRevoke,
}

var agentMCPKeysRotateCmd = &cobra.Command{
	Use:   "rotate <key-id>",
	Short: "Rotate one key's secret",
	Long: `Issue a replacement secret for one key. The previous secret is invalidated
immediately and the new secret is printed exactly once. The key identity is
preserved, so existing sessions remain continuable. Prompts for confirmation
unless --yes is passed.`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentMCPKeysRotate,
}

var agentMCPSessionsCmd = &cobra.Command{
	Use:   "sessions [agent]",
	Short: "List an endpoint's MCP sessions",
	Long: `List the sessions created through the agent's MCP endpoint, most recently
active first. Each row shows the session id, the owning key's label, status, turn
count, total steps, and timestamps.

Filter with --status (active, running, interrupted, expired). Use --json for
machine-readable output.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentMCPSessions,
}

// Flags for the mcp-endpoint group.
var (
	agentMCPEndpointJSON  bool
	agentMCPKeyLabel      string
	agentMCPKeyExpiresAt  string
	agentMCPKeysJSON      bool
	agentMCPSessionsJSON  bool
	agentMCPSessionStatus string
	agentMCPYes           bool
)

// ============================================================================
// Runners
// ============================================================================

func runAgentMCPEndpointShow(cmd *cobra.Command, args []string) error {
	projectID, c, err := agentMCPSetup(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentForEndpoint(cmd, c, args)
	if err != nil {
		return err
	}

	endpoint, err := c.SDK.MCP.GetAgentEndpoint(context.Background(), projectID, agentID)
	if err != nil {
		if sdkerrors.IsNotFound(err) {
			return fmt.Errorf("agent %s has no MCP endpoint — create one with 'memory agents mcp-endpoint create %s'", agentID, agentID)
		}
		return mapAgentMCPError(err)
	}

	if agentMCPEndpointJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(endpoint)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Agent MCP endpoint\n")
	fmt.Fprintf(out, "  ID:        %s\n", endpoint.ID)
	fmt.Fprintf(out, "  Agent:     %s\n", endpoint.AgentID)
	fmt.Fprintf(out, "  Status:    %s\n", endpoint.Status)
	fmt.Fprintf(out, "  MCP URL:   %s\n", endpoint.MCPURL)
	fmt.Fprintf(out, "  Created:   %s\n", fmtTime(endpoint.CreatedAt))
	return nil
}

func runAgentMCPEndpointCreate(cmd *cobra.Command, args []string) error {
	projectID, c, err := agentMCPSetup(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentForEndpoint(cmd, c, args)
	if err != nil {
		return err
	}

	endpoint, err := c.SDK.MCP.CreateAgentEndpoint(context.Background(), projectID, agentID)
	if err != nil {
		return mapAgentMCPError(err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Agent MCP endpoint created!")
	fmt.Fprintf(out, "  ID:      %s\n", endpoint.ID)
	fmt.Fprintf(out, "  Agent:   %s\n", endpoint.AgentID)
	fmt.Fprintf(out, "  Status:  %s\n", endpoint.Status)
	fmt.Fprintf(out, "  MCP URL: %s\n", endpoint.MCPURL)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Add a credential with: memory agents mcp-endpoint keys create %s --label <label>\n", agentID)
	return nil
}

func runAgentMCPEndpointRevoke(cmd *cobra.Command, args []string) error {
	projectID, c, err := agentMCPSetup(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentForEndpoint(cmd, c, args)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf("Revoke the MCP endpoint for agent %s (this also revokes its keys)", agentID)
	if err := confirmDestructive(cmd, prompt, agentMCPYes); err != nil {
		return err
	}

	endpoint, err := c.SDK.MCP.GetAgentEndpoint(context.Background(), projectID, agentID)
	if err != nil {
		if sdkerrors.IsNotFound(err) {
			fmt.Fprintf(cmd.OutOrStdout(), "Agent %s has no active MCP endpoint; nothing to revoke.\n", agentID)
			return nil
		}
		return mapAgentMCPError(err)
	}

	if err := c.SDK.MCP.RevokeAgentEndpoint(context.Background(), projectID, endpoint.ID); err != nil {
		return mapAgentMCPError(err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Revoked MCP endpoint %s for agent %s.\n", endpoint.ID, agentID)
	return nil
}

func runAgentMCPKeysCreate(cmd *cobra.Command, args []string) error {
	if strings.TrimSpace(agentMCPKeyLabel) == "" {
		return fmt.Errorf("key label is required. Use --label")
	}

	var expiresAt *time.Time
	if agentMCPKeyExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, agentMCPKeyExpiresAt)
		if err != nil {
			return fmt.Errorf("invalid --expires-at %q: expected RFC3339 (e.g. 2026-12-31T00:00:00Z)", agentMCPKeyExpiresAt)
		}
		expiresAt = &parsed
	}

	projectID, c, err := agentMCPSetup(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentForEndpoint(cmd, c, args)
	if err != nil {
		return err
	}

	endpoint, err := c.SDK.MCP.GetAgentEndpoint(context.Background(), projectID, agentID)
	if err != nil {
		if sdkerrors.IsNotFound(err) {
			return fmt.Errorf("agent %s has no MCP endpoint — create one with 'memory agents mcp-endpoint create %s'", agentID, agentID)
		}
		return mapAgentMCPError(err)
	}

	secret, err := c.SDK.MCP.CreateAgentKey(context.Background(), projectID, endpoint.ID, mcpsdk.CreateAgentMCPKeyRequest{
		Label:     strings.TrimSpace(agentMCPKeyLabel),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return mapAgentMCPError(err)
	}

	return renderAgentMCPKeySecret(cmd.OutOrStdout(), secret)
}

func runAgentMCPKeysList(cmd *cobra.Command, args []string) error {
	projectID, c, err := agentMCPSetup(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentForEndpoint(cmd, c, args)
	if err != nil {
		return err
	}

	endpoint, err := c.SDK.MCP.GetAgentEndpoint(context.Background(), projectID, agentID)
	if err != nil {
		if sdkerrors.IsNotFound(err) {
			return fmt.Errorf("agent %s has no MCP endpoint — create one with 'memory agents mcp-endpoint create %s'", agentID, agentID)
		}
		return mapAgentMCPError(err)
	}

	list, err := c.SDK.MCP.ListAgentKeys(context.Background(), projectID, endpoint.ID)
	if err != nil {
		return mapAgentMCPError(err)
	}

	if agentMCPKeysJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(list)
	}

	out := cmd.OutOrStdout()
	if len(list.Keys) == 0 {
		fmt.Fprintln(out, "No keys found for this endpoint.")
		return nil
	}

	fmt.Fprintf(out, "Found %d key(s):\n\n", len(list.Keys))
	for i, k := range list.Keys {
		fmt.Fprintf(out, "%d. %s\n", i+1, k.Label)
		fmt.Fprintf(out, "   ID:        %s\n", k.ID)
		fmt.Fprintf(out, "   Status:    %s\n", k.Status)
		fmt.Fprintf(out, "   Created:   %s\n", fmtTime(k.CreatedAt))
		if k.LastUsedAt != nil {
			fmt.Fprintf(out, "   Last used: %s\n", fmtTimePTime(k.LastUsedAt))
		} else {
			fmt.Fprintf(out, "   Last used: never\n")
		}
		if k.ExpiresAt != nil {
			fmt.Fprintf(out, "   Expires:   %s\n", fmtTimePTime(k.ExpiresAt))
		}
		fmt.Fprintln(out)
	}
	return nil
}

func runAgentMCPKeysRevoke(cmd *cobra.Command, args []string) error {
	keyID := args[0]

	if err := confirmDestructive(cmd, fmt.Sprintf("Revoke key %s", keyID), agentMCPYes); err != nil {
		return err
	}

	projectID, c, err := agentMCPSetup(cmd)
	if err != nil {
		return err
	}

	if err := c.SDK.MCP.RevokeAgentKey(context.Background(), projectID, keyID); err != nil {
		return mapAgentMCPError(err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Revoked key %s.\n", keyID)
	return nil
}

func runAgentMCPKeysRotate(cmd *cobra.Command, args []string) error {
	keyID := args[0]

	if err := confirmDestructive(cmd, fmt.Sprintf("Rotate key %s (the previous secret stops working)", keyID), agentMCPYes); err != nil {
		return err
	}

	projectID, c, err := agentMCPSetup(cmd)
	if err != nil {
		return err
	}

	secret, err := c.SDK.MCP.RotateAgentKey(context.Background(), projectID, keyID)
	if err != nil {
		return mapAgentMCPError(err)
	}

	return renderAgentMCPKeySecret(cmd.OutOrStdout(), secret)
}

func runAgentMCPSessions(cmd *cobra.Command, args []string) error {
	status := strings.TrimSpace(agentMCPSessionStatus)
	if status != "" && !validAgentMCPSessionStatus(status) {
		return fmt.Errorf("invalid --status %q: expected active, running, interrupted, or expired", status)
	}

	projectID, c, err := agentMCPSetup(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentForEndpoint(cmd, c, args)
	if err != nil {
		return err
	}

	endpoint, err := c.SDK.MCP.GetAgentEndpoint(context.Background(), projectID, agentID)
	if err != nil {
		if sdkerrors.IsNotFound(err) {
			return fmt.Errorf("agent %s has no MCP endpoint — create one with 'memory agents mcp-endpoint create %s'", agentID, agentID)
		}
		return mapAgentMCPError(err)
	}

	list, err := c.SDK.MCP.ListAgentSessions(context.Background(), projectID, endpoint.ID, status)
	if err != nil {
		return mapAgentMCPError(err)
	}

	if agentMCPSessionsJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(list)
	}

	out := cmd.OutOrStdout()
	if len(list.Sessions) == 0 {
		fmt.Fprintln(out, "No sessions found for this endpoint.")
		return nil
	}

	fmt.Fprintf(out, "Found %d session(s):\n\n", len(list.Sessions))
	for i, s := range list.Sessions {
		fmt.Fprintf(out, "%d. %s\n", i+1, s.SessionID)
		fmt.Fprintf(out, "   Key:        %s (%s)\n", s.KeyLabel, s.KeyID)
		fmt.Fprintf(out, "   Status:     %s\n", s.Status)
		fmt.Fprintf(out, "   Turns:      %d\n", s.TurnCount)
		fmt.Fprintf(out, "   Steps:      %d\n", s.TotalSteps)
		fmt.Fprintf(out, "   Created:    %s\n", fmtTime(s.CreatedAt))
		fmt.Fprintf(out, "   Last active: %s\n", fmtTime(s.LastActiveAt))
		if s.ExpiresAt != nil {
			fmt.Fprintf(out, "   Expires:    %s\n", fmtTimePTime(s.ExpiresAt))
		}
		fmt.Fprintln(out)
	}
	return nil
}

// ============================================================================
// Setup helpers
// ============================================================================

// agentMCPSetup resolves the project context and returns a client scoped to it.
func agentMCPSetup(cmd *cobra.Command) (string, *client.Client, error) {
	projectID, err := resolveProjectContext(cmd, agentProjectID)
	if err != nil {
		return "", nil, err
	}

	c, err := getClient(cmd)
	if err != nil {
		return "", nil, err
	}
	c.SetContext("", projectID)
	return projectID, c, nil
}

// resolveAgentForEndpoint resolves the agent positional argument. A non-empty
// argument is resolved by id, runtime agent name, or agent-definition name/slug;
// an omitted argument falls back to the interactive picker.
func resolveAgentForEndpoint(cmd *cobra.Command, c *client.Client, args []string) (string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return resolveAgentsMCPReference(c.SDK, args[0])
	}
	return resolveAgentArgOrPick(cmd, c, args)
}

// ============================================================================
// Agent reference resolution
// ============================================================================

// resolveAgentsMCPReference resolves an agent reference accepted by the agent
// MCP endpoint API: a runtime agent id, a runtime agent name, or an
// agent-definition name or slug.
func resolveAgentsMCPReference(sdkClient *sdk.Client, ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", fmt.Errorf("agent is required")
	}
	if isUUID(trimmed) {
		return trimmed, nil
	}

	ctx := context.Background()

	runtime, err := sdkClient.Agents.List(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list agents: %w", err)
	}
	runtimeMatches := make([]string, 0, 1)
	for _, a := range runtime.Data {
		if strings.EqualFold(strings.TrimSpace(a.Name), trimmed) {
			runtimeMatches = append(runtimeMatches, a.ID)
		}
	}
	if len(runtimeMatches) > 1 {
		return "", fmt.Errorf("agent %q is ambiguous: %d runtime agents share that name", trimmed, len(runtimeMatches))
	}
	if len(runtimeMatches) == 1 {
		return runtimeMatches[0], nil
	}

	defs, err := sdkClient.AgentDefinitions.List(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list agent definitions: %w", err)
	}
	wantSlug := agentSlug(trimmed)
	defMatches := make([]string, 0, 1)
	for _, d := range defs.Data {
		if strings.EqualFold(strings.TrimSpace(d.Name), trimmed) || (wantSlug != "" && agentSlug(d.Name) == wantSlug) {
			defMatches = append(defMatches, d.ID)
		}
	}
	if len(defMatches) > 1 {
		return "", fmt.Errorf("agent %q is ambiguous: %d agent definitions match", trimmed, len(defMatches))
	}
	if len(defMatches) == 1 {
		return defMatches[0], nil
	}

	return "", fmt.Errorf("no agent matches %q in this project — use an agent id, a runtime agent name, or an agent-definition name or slug", trimmed)
}

var agentSlugNonAlnum = regexp.MustCompile(`[^a-z0-9-]`)
var agentSlugMultiHyphen = regexp.MustCompile(`-{2,}`)

// agentSlug normalizes a free-form agent name to an RFC 1123 DNS label, matching
// the server's ACP slug normalization (pkg/acpslug.FromName) so definition
// references resolve identically.
func agentSlug(name string) string {
	s := strings.ToLower(name)
	s = agentSlugNonAlnum.ReplaceAllString(s, "-")
	s = agentSlugMultiHyphen.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "-")
	}
	return s
}

// ============================================================================
// Output helpers
// ============================================================================

// renderAgentMCPKeySecret is the only place a raw secret is written. It prints
// the secret exactly once with an unmissable warning; the caller must not echo
// the token again.
func renderAgentMCPKeySecret(w io.Writer, secret *mcpsdk.AgentMCPKeySecret) error {
	if secret == nil {
		return fmt.Errorf("no key secret returned")
	}
	fmt.Fprintln(w, "Agent MCP key created!")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Label:    %s\n", secret.Label)
	fmt.Fprintf(w, "  Key ID:   %s\n", secret.ID)
	fmt.Fprintf(w, "  Status:   %s\n", secret.Status)
	fmt.Fprintf(w, "  MCP URL:  %s\n", secret.MCPURL)
	if secret.ExpiresAt != nil {
		fmt.Fprintf(w, "  Expires:  %s\n", fmtTimePTime(secret.ExpiresAt))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  ┌───────────────────────────────────────────────────────────┐")
	fmt.Fprintf(w, "  │  Secret: %-49s│\n", secret.Token)
	fmt.Fprintln(w, "  │  Save this now — it will not be shown again.              │")
	fmt.Fprintln(w, "  └───────────────────────────────────────────────────────────┘")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Configure your MCP client with the MCP URL above and send the")
	fmt.Fprintln(w, "  secret in the X-API-Key header.")
	return nil
}

// validAgentMCPSessionStatus reports whether status names a real session
// lifecycle state, matching the server's filter.
func validAgentMCPSessionStatus(status string) bool {
	switch status {
	case "active", "running", "interrupted", "expired":
		return true
	default:
		return false
	}
}

// mapAgentMCPError translates the agent MCP API's error responses into
// actionable messages. Unrecognized errors are returned wrapped, unchanged.
func mapAgentMCPError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *sdkerrors.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.StatusCode == http.StatusConflict && apiErr.Code == "agent_mcp_key_label_exists":
		return fmt.Errorf("a key with that label already exists on this endpoint (labels are compared ignoring case): %w", err)
	case apiErr.StatusCode == http.StatusConflict && apiErr.Code == "agent_mcp_endpoint_exists":
		return fmt.Errorf("an active MCP endpoint already exists for this agent — revoke it first with 'memory agents mcp-endpoint revoke': %w", err)
	case apiErr.StatusCode == http.StatusConflict:
		return fmt.Errorf("the request conflicts with the endpoint's current state: %w", err)
	case apiErr.StatusCode == http.StatusNotFound:
		return fmt.Errorf("not found — the endpoint or key does not exist in this project: %w", err)
	case apiErr.StatusCode == http.StatusForbidden:
		return fmt.Errorf("project admin permission is required to manage an agent's MCP endpoint: %w", err)
	default:
		return err
	}
}

// confirmDestructive enforces explicit confirmation for destructive commands:
// --yes skips the prompt; non-interactive callers must pass it; an interactive
// terminal is prompted for y/yes.
func confirmDestructive(cmd *cobra.Command, action string, yes bool) error {
	if yes {
		return nil
	}
	if isNonInteractive() {
		return fmt.Errorf("%s is destructive — re-run with --yes to confirm", action)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s? [y/N] ", action)
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		return fmt.Errorf("%s aborted", action)
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("%s aborted", action)
	}
	return nil
}

// ============================================================================
// Registration
// ============================================================================

func init() {
	agentMCPEndpointShowCmd.Flags().BoolVar(&agentMCPEndpointJSON, "json", false, "Output as JSON")

	agentMCPKeysCreateCmd.Flags().StringVar(&agentMCPKeyLabel, "label", "", "Key label, unique per endpoint (required)")
	agentMCPKeysCreateCmd.Flags().StringVar(&agentMCPKeyExpiresAt, "expires-at", "", "Optional expiry as RFC3339 (e.g. 2026-12-31T00:00:00Z)")
	_ = agentMCPKeysCreateCmd.MarkFlagRequired("label")

	agentMCPKeysListCmd.Flags().BoolVar(&agentMCPKeysJSON, "json", false, "Output as JSON")

	agentMCPSessionsCmd.Flags().StringVar(&agentMCPSessionStatus, "status", "", "Filter by session status (active, running, interrupted, expired)")
	agentMCPSessionsCmd.Flags().BoolVar(&agentMCPSessionsJSON, "json", false, "Output as JSON")

	for _, c := range []*cobra.Command{agentMCPEndpointRevokeCmd, agentMCPKeysRevokeCmd, agentMCPKeysRotateCmd} {
		c.Flags().BoolVar(&agentMCPYes, "yes", false, "Skip the confirmation prompt")
	}

	agentMCPKeysCmd.AddCommand(agentMCPKeysCreateCmd, agentMCPKeysListCmd, agentMCPKeysRevokeCmd, agentMCPKeysRotateCmd)

	agentMCPEndpointCmd.AddCommand(
		agentMCPEndpointShowCmd,
		agentMCPEndpointCreateCmd,
		agentMCPEndpointRevokeCmd,
		agentMCPKeysCmd,
		agentMCPSessionsCmd,
	)

	agentsCmd.AddCommand(agentMCPEndpointCmd)
}
