package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/spf13/cobra"
)

var sessionsProjectFlag string

func getSessionsGraphClient(cmd *cobra.Command) (*sdkgraph.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, err
	}

	projectID, err := resolveProjectContext(cmd, sessionsProjectFlag)
	if err != nil {
		return nil, err
	}

	c.SetContext("", projectID)
	return c.SDK.Graph, nil
}

var sessionsCmd = &cobra.Command{
	Use:     "sessions",
	Short:   "Manage AI agent sessions and messages",
	GroupID: "knowledge",
}

// ─────────────────────────────────────────────
// sessions create
// ─────────────────────────────────────────────

var sessionsCreateTitle string
var sessionsCreateSummary string
var sessionsCreateAgentVersion string

var sessionsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new session",
	Long:  `Creates a new Session graph object to track an AI agent conversation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if sessionsCreateTitle == "" {
			return fmt.Errorf("--title is required")
		}

		ctx := context.Background()
		g, err := getSessionsGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.CreateSessionRequest{
			Title: sessionsCreateTitle,
		}
		if sessionsCreateSummary != "" {
			req.Summary = &sessionsCreateSummary
		}
		if sessionsCreateAgentVersion != "" {
			req.AgentVersion = &sessionsCreateAgentVersion
		}

		session, err := g.CreateSession(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}

		if jsonFlag || output == "json" {
			b, _ := json.MarshalIndent(session, "", "  ")
			fmt.Println(string(b))
			return nil
		}

		fmt.Printf("Session created\n")
		fmt.Printf("  ID:    %s\n", session.VersionID)
		fmt.Printf("  Title: %s\n", titleFromProps(session.Properties))
		return nil
	},
}

// ─────────────────────────────────────────────
// sessions list
// ─────────────────────────────────────────────

var sessionsListLimit int
var sessionsListCursor string

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sessions in the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		g, err := getSessionsGraphClient(cmd)
		if err != nil {
			return err
		}

		resp, err := g.ListSessions(ctx, sessionsListLimit, sessionsListCursor)
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}

		if jsonFlag || output == "json" {
			b, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(b))
			return nil
		}

		if len(resp.Items) == 0 {
			fmt.Println("No sessions found.")
			return nil
		}

		fmt.Printf("%-36s  %-40s  %s\n", "ID", "TITLE", "CREATED")
		fmt.Println(strings.Repeat("-", 90))
		for _, s := range resp.Items {
			title := titleFromProps(s.Properties)
			fmt.Printf("%-36s  %-40s  %s\n", s.VersionID, title, s.CreatedAt.Format("2006-01-02 15:04:05"))
		}

		if resp.NextCursor != nil {
			fmt.Printf("\nNext cursor: %s\n", *resp.NextCursor)
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// sessions get
// ─────────────────────────────────────────────

var sessionsGetCmd = &cobra.Command{
	Use:   "get [session-id]",
	Short: "Get a session by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		g, err := getSessionsGraphClient(cmd)
		if err != nil {
			return err
		}

		session, err := g.GetSession(ctx, args[0])
		if err != nil {
			return fmt.Errorf("failed to get session: %w", err)
		}

		if jsonFlag || output == "json" {
			b, _ := json.MarshalIndent(session, "", "  ")
			fmt.Println(string(b))
			return nil
		}

		fmt.Printf("ID:         %s\n", session.VersionID)
		fmt.Printf("Title:      %s\n", titleFromProps(session.Properties))
		fmt.Printf("Created:    %s\n", session.CreatedAt.Format("2006-01-02 15:04:05"))
		if v, ok := session.Properties["agent_version"]; ok {
			fmt.Printf("Agent:      %v\n", v)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// sessions spawn
// ─────────────────────────────────────────────

var sessionsSpawnTitle string
var sessionsSpawnForkContext bool
var sessionsSpawnMaxMessages int
var sessionsSpawnSummary string

var sessionsSpawnCmd = &cobra.Command{
	Use:   "spawn <parent-session-id>",
	Short: "Spawn a child session from a parent",
	Long: `Creates a new child session linked to the parent via a spawned_from relationship.

When --fork-context is set, the parent's message history is copied into the child
as a snapshot at spawn time. The child then operates independently — no live sync.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if sessionsSpawnTitle == "" {
			return fmt.Errorf("--title is required")
		}
		parentID := args[0]

		ctx := context.Background()
		g, err := getSessionsGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.SpawnSessionRequest{
			Title:       sessionsSpawnTitle,
			ForkContext: sessionsSpawnForkContext,
			MaxMessages: sessionsSpawnMaxMessages,
		}
		if sessionsSpawnSummary != "" {
			req.Summary = &sessionsSpawnSummary
		}

		result, err := g.SpawnSession(ctx, parentID, req)
		if err != nil {
			return err
		}

		if jsonFlag || output == "json" {
			b, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(b))
			return nil
		}

		sess := result.Session
		fmt.Printf("Session spawned\n")
		fmt.Printf("  ID:              %s\n", sess.EntityID)
		fmt.Printf("  Title:           %s\n", titleFromProps(sess.Properties))
		fmt.Printf("  Parent:          %s\n", parentID)
		fmt.Printf("  Forked messages: %d\n", result.ForkedMessages)
		return nil
	},
}

// ─────────────────────────────────────────────
// sessions messages
// ─────────────────────────────────────────────

var sessionsMessagesCmd = &cobra.Command{
	Use:   "messages",
	Short: "Manage messages in a session",
}

// sessions messages add

var messagesAddRole string
var messagesAddContent string
var messagesAddTokenCount int

var messagesAddCmd = &cobra.Command{
	Use:   "add [session-id]",
	Short: "Append a message to a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if messagesAddRole == "" {
			return fmt.Errorf("--role is required (user|assistant|system)")
		}
		if messagesAddContent == "" {
			return fmt.Errorf("--content is required")
		}

		ctx := context.Background()
		g, err := getSessionsGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.AppendMessageRequest{
			Role:    messagesAddRole,
			Content: messagesAddContent,
		}
		if messagesAddTokenCount > 0 {
			tc := messagesAddTokenCount
			req.TokenCount = &tc
		}

		msg, err := g.AppendMessage(ctx, args[0], req)
		if err != nil {
			return fmt.Errorf("failed to append message: %w", err)
		}

		if jsonFlag || output == "json" {
			b, _ := json.MarshalIndent(msg, "", "  ")
			fmt.Println(string(b))
			return nil
		}

		fmt.Printf("Message appended\n")
		fmt.Printf("  ID:   %s\n", msg.VersionID)
		fmt.Printf("  Role: %s\n", messagesAddRole)
		if seq, ok := msg.Properties["sequence_number"]; ok {
			fmt.Printf("  Seq:  %v\n", seq)
		}
		return nil
	},
}

// sessions messages list

var messagesListLimit int
var messagesListCursor string

var messagesListCmd = &cobra.Command{
	Use:   "list [session-id]",
	Short: "List messages in a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		g, err := getSessionsGraphClient(cmd)
		if err != nil {
			return err
		}

		resp, err := g.ListMessages(ctx, args[0], messagesListLimit, messagesListCursor)
		if err != nil {
			return fmt.Errorf("failed to list messages: %w", err)
		}

		if jsonFlag || output == "json" {
			b, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(b))
			return nil
		}

		if len(resp.Items) == 0 {
			fmt.Println("No messages found.")
			return nil
		}

		for _, m := range resp.Items {
			role := "?"
			if r, ok := m.Properties["role"].(string); ok {
				role = r
			}
			seq := ""
			if s, ok := m.Properties["sequence_number"]; ok {
				seq = fmt.Sprintf("[%v] ", s)
			}
			content := ""
			if c, ok := m.Properties["content"].(string); ok {
				if len(c) > 80 {
					c = c[:77] + "..."
				}
				content = c
			}
			fmt.Printf("%s%s: %s\n", seq, role, content)
		}

		if resp.NextCursor != nil {
			fmt.Printf("\nNext cursor: %s\n", *resp.NextCursor)
		}

		return nil
	},
}

// titleFromProps extracts the "title" property from a graph object's properties map.
func titleFromProps(props map[string]any) string {
	if props == nil {
		return ""
	}
	if v, ok := props["title"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ─────────────────────────────────────────────
// sessions recount — backfill message_count + total_tokens
// ─────────────────────────────────────────────

var sessionsRecountCmd = &cobra.Command{
	Use:   "recount <session-id>",
	Short: "Recount message_count and total_tokens for a session",
	Long:  "Scans all messages for a session and updates message_count and total_tokens on the Session object. Useful for backfilling existing sessions.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]
		g, err := getSessionsGraphClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		// Paginate all messages.
		var totalTokens int
		var msgCount int
		cursor := ""
		for {
			resp, err := g.ListMessages(ctx, sessionID, 200, cursor)
			if err != nil {
				return fmt.Errorf("list messages: %w", err)
			}
			for _, msg := range resp.Items {
				msgCount++
				if tc, ok := msg.Properties["token_count"]; ok {
					switch v := tc.(type) {
					case float64:
						totalTokens += int(v)
					case int:
						totalTokens += v
					}
				}
			}
			if resp.NextCursor == nil || *resp.NextCursor == "" {
				break
			}
			cursor = *resp.NextCursor
		}

		// Update Session via BulkAction merge_properties targeting by canonical ID.
		_, err = g.BulkAction(ctx, &sdkgraph.BulkActionRequest{
			Filter: sdkgraph.BulkActionFilter{
				Types: []string{"Session"},
				PropertyFilters: []sdkgraph.PropertyFilter{
					{Path: "id", Op: "eq", Value: sessionID},
				},
			},
			Action: "merge_properties",
			Properties: map[string]any{
				"message_count": msgCount,
				"total_tokens":  totalTokens,
			},
		})
		if err != nil {
			// Fallback: patch via UpdateObject.
			props := map[string]any{
				"message_count": msgCount,
				"total_tokens":  totalTokens,
			}
			if _, patchErr := g.UpdateObject(ctx, sessionID, &sdkgraph.UpdateObjectRequest{
				Properties: props,
			}); patchErr != nil {
				return fmt.Errorf("update session: %w", patchErr)
			}
		}

		fmt.Fprintf(cmd.OutOrStdout(), "session %s: message_count=%d total_tokens=%d\n", sessionID, msgCount, totalTokens)
		return nil
	},
}

func init() {
	sessionsCmd.PersistentFlags().StringVar(&sessionsProjectFlag, "project", "", "Project name or ID")

	// sessions create
	sessionsCreateCmd.Flags().StringVar(&sessionsCreateTitle, "title", "", "Session title (required)")
	sessionsCreateCmd.Flags().StringVar(&sessionsCreateSummary, "summary", "", "Optional session summary")
	sessionsCreateCmd.Flags().StringVar(&sessionsCreateAgentVersion, "agent-version", "", "Optional agent version")
	sessionsCmd.AddCommand(sessionsCreateCmd)

	// sessions list
	sessionsListCmd.Flags().IntVar(&sessionsListLimit, "limit", 20, "Max sessions to return")
	sessionsListCmd.Flags().StringVar(&sessionsListCursor, "cursor", "", "Pagination cursor")
	sessionsCmd.AddCommand(sessionsListCmd)

	// sessions get
	sessionsCmd.AddCommand(sessionsGetCmd)

	// sessions spawn
	sessionsSpawnCmd.Flags().StringVar(&sessionsSpawnTitle, "title", "", "Child session title (required)")
	sessionsSpawnCmd.Flags().BoolVar(&sessionsSpawnForkContext, "fork-context", false, "Copy parent message history into child session")
	sessionsSpawnCmd.Flags().IntVar(&sessionsSpawnMaxMessages, "max-messages", 50, "Max parent messages to copy when --fork-context is set")
	sessionsSpawnCmd.Flags().StringVar(&sessionsSpawnSummary, "summary", "", "Optional session summary")
	sessionsCmd.AddCommand(sessionsSpawnCmd)

	// sessions messages add
	messagesAddCmd.Flags().StringVar(&messagesAddRole, "role", "", "Message role: user|assistant|system (required)")
	messagesAddCmd.Flags().StringVar(&messagesAddContent, "content", "", "Message content (required)")
	messagesAddCmd.Flags().IntVar(&messagesAddTokenCount, "tokens", 0, "Token count (optional)")
	sessionsMessagesCmd.AddCommand(messagesAddCmd)

	// sessions messages list
	messagesListCmd.Flags().IntVar(&messagesListLimit, "limit", 50, "Max messages to return")
	messagesListCmd.Flags().StringVar(&messagesListCursor, "cursor", "", "Pagination cursor")
	sessionsMessagesCmd.AddCommand(messagesListCmd)

	sessionsCmd.AddCommand(sessionsMessagesCmd)
	sessionsCmd.AddCommand(sessionsRecountCmd)
	rootCmd.AddCommand(sessionsCmd)
}
