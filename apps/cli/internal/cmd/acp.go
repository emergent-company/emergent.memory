package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/acp"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
	"github.com/spf13/cobra"
)

var acpAgent string

// acpCmd runs the CLI as an Agent Client Protocol (ACP) v1 agent over stdio.
// ACP clients (Paseo, Claude Code, Gemini CLI, …) spawn `memory acp` and speak
// newline-delimited JSON-RPC 2.0 on stdin/stdout; every prompt is forwarded to
// the configured Memory agent via A2A.
var acpCmd = &cobra.Command{
	Use:   "acp",
	Short: "Run as an ACP (Agent Client Protocol) agent over stdio",
	Long: `Run as an Agent Client Protocol v1 agent over stdio, bridging ACP clients
(Paseo, Claude Code, Gemini CLI, and others) to a Memory agent.

Select the Memory agent to front with --agent <skill-id> (the RFC 1123 slug
shown by 'memory a2a discover') or the MEMORY_AGENT environment variable.
Connection, auth, and project are read from the standard CLI config
(MEMORY_SERVER_URL / MEMORY_PROJECT_TOKEN) — set them as env vars so a
spawning ACP client can inject them.

Only ACP JSON-RPC messages are written to stdout; diagnostics go to stderr.`,
	GroupID: "ai",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		skill := acpAgent
		if skill == "" {
			skill = os.Getenv("MEMORY_AGENT")
		}
		if skill == "" {
			return fmt.Errorf("no Memory agent selected: pass --agent <skill-id> or set MEMORY_AGENT")
		}

		client, err := getA2AClient(cmd)
		if err != nil {
			return err
		}

		modes := discoverModes(cmd.Context(), client)
		agent := acp.NewAgent(client, skill, Version, modes)
		return acp.Run(cmd.Context(), os.Stdin, os.Stdout, os.Stderr, agent)
	},
}

// discoverModes fetches the extended AgentCard and derives the selectable agent
// modes. A failed or empty card yields nil, which the agent degrades to a single
// default mode — the ACP bridge still works, just without an agent selector.
func discoverModes(ctx context.Context, client *a2a.Client) []acp.Mode {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	card, err := client.ExtendedAgentCard(ctx)
	if err != nil || card == nil {
		return nil
	}
	modes := make([]acp.Mode, 0, len(card.Skills))
	for _, s := range card.Skills {
		if s.ID == "" {
			continue
		}
		modes = append(modes, acp.Mode{ID: s.ID, Name: s.Name, Description: s.Description})
	}
	return modes
}

func init() {
	acpCmd.Flags().StringVar(&acpAgent, "agent", "", "Memory agent skill id to front (default: $MEMORY_AGENT)")
	rootCmd.AddCommand(acpCmd)
}
