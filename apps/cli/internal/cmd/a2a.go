package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	internalui "github.com/emergent-company/emergent.memory/apps/cli/internal/ui"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
	"github.com/spf13/cobra"
)

// ── A2A client construction ──────────────────────────────────────────────────

// getA2AClient creates an A2A SDK client from the current CLI config.
func getA2AClient(cmd *cobra.Command) (*a2a.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, err
	}
	baseURL := c.BaseURL()
	token := c.AuthorizationHeader()
	if token == "" {
		return nil, fmt.Errorf("no authentication configured — run 'memory login' or set MEMORY_PROJECT_API_KEY")
	}
	// AuthorizationHeader returns "Bearer <token>"; we need just the token.
	token = strings.TrimPrefix(token, "Bearer ")
	return a2a.NewClient(baseURL, token), nil
}

// ── Root command: memory a2a ─────────────────────────────────────────────────

var a2aCmd = &cobra.Command{
	Use:     "a2a",
	Short:   "A2A Protocol (A2A v1.0) agent operations",
	Long:    "Commands for discovering and invoking agents via the A2A Protocol v1.0 HTTP+JSON API.",
	GroupID: "ai",
}

// ── memory a2a discover ──────────────────────────────────────────────────────

var a2aDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover externally-visible agents (extended AgentCard)",
	Long: `Fetch and display the authenticated extended AgentCard.

The card lists every agent with visibility='external' in the current project
as an A2A skill. Use --json for the raw JSON response.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getA2AClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()
		card, err := client.ExtendedAgentCard(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch extended AgentCard: %w", err)
		}

		if output == "json" {
			return a2aPrintJSON(cmd.OutOrStdout(), card)
		}

		fmt.Printf("Name:        %s\n", card.Name)
		fmt.Printf("Description: %s\n", card.Description)
		fmt.Printf("Version:     %s\n", card.Version)

		if len(card.Skills) == 0 {
			fmt.Println("\nNo externally-visible agents found")
			return nil
		}

		fmt.Printf("\nSkills (%d):\n", len(card.Skills))
		t := internalui.NewTable(internalui.TableConfig{Compact: true})
		t.SetHeaders([]string{"ID", "NAME", "DESCRIPTION"})
		for _, s := range card.Skills {
			t.AddRow([]string{s.ID, s.Name, truncate(s.Description, 60)})
		}
		fmt.Print(t.Render())
		return nil
	},
}

// ── memory a2a send ──────────────────────────────────────────────────────────

var (
	a2aSendMessage string
	a2aSendTaskID  string
	a2aSendMode    string
)

var a2aSendCmd = &cobra.Command{
	Use:   "send <agent-skill-id>",
	Short: "Send a message to an A2A agent",
	Long: `Send a message to the agent identified by its A2A skill id (the RFC 1123
slug shown by 'memory a2a discover').

The --mode flag controls execution:
  sync   (default) — blocks until the task reaches a terminal state, prints output
  async  — returns immediately with the task ID and state
  stream — streams the agent's output to stdout in real time

If a sync send pauses for human input (TASK_STATE_INPUT_REQUIRED), the CLI
prompts you interactively and resumes the task automatically.

Use --task <id> to resume an existing input-required task by ID; in that case
the skill id positional is accepted but not re-sent (the server infers the
agent from the task).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		skillID := args[0]
		if a2aSendMessage == "" {
			return fmt.Errorf("--message flag is required")
		}

		client, err := getA2AClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		req := a2a.SendMessageRequest{
			Message: a2a.Message{
				MessageID: newA2AMessageID(),
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{a2a.TextPart(a2aSendMessage)},
			},
		}
		if a2aSendTaskID != "" {
			req.Message.TaskID = a2aSendTaskID
		} else {
			req.Message.Metadata = map[string]any{a2a.SkillIDMetadataKey: skillID}
		}

		switch a2aSendMode {
		case "stream":
			return a2aStreamSend(ctx, client, req)
		case "async":
			req.Configuration = &a2a.SendMessageConfiguration{ReturnImmediately: true}
		case "sync":
			// default
		default:
			return fmt.Errorf("invalid --mode %q: must be sync, async, or stream", a2aSendMode)
		}

		resp, err := client.SendMessage(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}

		// A2A may respond with a message instead of a task (turn-scoped reply).
		if resp.Task == nil {
			if output == "json" {
				return a2aPrintJSON(cmd.OutOrStdout(), resp)
			}
			if resp.Message != nil {
				fmt.Println(a2aMessageText(resp.Message))
			}
			return nil
		}

		if output == "json" {
			return a2aPrintJSON(cmd.OutOrStdout(), resp.Task)
		}

		// Async: print task id + state and exit.
		if a2aSendMode == "async" {
			fmt.Printf("Task ID: %s\n", resp.Task.ID)
			fmt.Printf("Status:  %s\n", resp.Task.Status.State)
			return nil
		}

		// Sync: loop on human-in-the-loop pauses.
		task := resp.Task
		for task.Status.State == a2a.TaskStateInputRequired {
			task, err = handleA2AInputRequired(ctx, client, task)
			if err != nil {
				return err
			}
		}
		printA2ATask(task)
		return nil
	},
}

// a2aStreamSend consumes the SSE stream and prints text to stdout.
func a2aStreamSend(ctx context.Context, client *a2a.Client, req a2a.SendMessageRequest) error {
	stream, err := client.StreamMessage(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to stream message: %w", err)
	}
	defer func() { _ = stream.Close() }()

	for {
		event, err := stream.Next()
		if err == io.EOF {
			fmt.Println() // final newline
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		switch {
		case event.ArtifactUpdate != nil:
			for _, p := range event.ArtifactUpdate.Artifact.Parts {
				if p.Text != nil {
					fmt.Print(*p.Text)
				}
			}
		case event.StatusUpdate != nil:
			if event.StatusUpdate.Status.State == a2a.TaskStateFailed {
				return a2aStreamFailure(event.StatusUpdate.Status.Message)
			}
		case event.Task != nil:
			if event.Task.Status.State == a2a.TaskStateFailed {
				return a2aStreamFailure(event.Task.Status.Message)
			}
		}
	}
}

// a2aStreamFailure formats a terminal FAILED event as an error.
func a2aStreamFailure(msg *a2a.Message) error {
	if msg != nil && a2aMessageText(msg) != "" {
		return fmt.Errorf("agent task failed: %s", a2aMessageText(msg))
	}
	return fmt.Errorf("agent task failed")
}

// handleA2AInputRequired prompts the user interactively for the answer to an
// input-required task and resumes it by sending a new message with taskId.
func handleA2AInputRequired(ctx context.Context, client *a2a.Client, task *a2a.Task) (*a2a.Task, error) {
	question := ""
	if task.Status.Message != nil {
		question = a2aMessageText(task.Status.Message)
	}
	if question != "" {
		fmt.Printf("\nAgent is asking: %s\n", question)
	}

	fmt.Print("\nYour response: ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil, fmt.Errorf("no input provided")
	}
	response := strings.TrimSpace(scanner.Text())
	if response == "" {
		return nil, fmt.Errorf("empty response — task remains in input-required state")
	}

	resp, err := client.SendMessage(ctx, a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: newA2AMessageID(),
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{a2a.TextPart(response)},
			TaskID:    task.ID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resume task: %w", err)
	}
	if resp.Task == nil {
		return nil, fmt.Errorf("server returned no task on resume")
	}
	return resp.Task, nil
}

// printA2ATask prints a task summary followed by its text artifacts.
func printA2ATask(task *a2a.Task) {
	fmt.Printf("Task ID: %s\n", task.ID)
	fmt.Printf("Status:  %s\n", task.Status.State)
	if task.ContextID != "" {
		fmt.Printf("Context: %s\n", task.ContextID)
	}
	if task.Status.Message != nil {
		if text := a2aMessageText(task.Status.Message); text != "" {
			fmt.Printf("Message: %s\n", text)
		}
	}
	for _, a := range task.Artifacts {
		for _, p := range a.Parts {
			if p.Text != nil && *p.Text != "" {
				fmt.Println()
				fmt.Println(*p.Text)
			}
		}
	}
}

// a2aMessageText joins the text parts of a message into a single string.
func a2aMessageText(m *a2a.Message) string {
	var sb strings.Builder
	for _, p := range m.Parts {
		if p.Text != nil {
			sb.WriteString(*p.Text)
		}
	}
	return sb.String()
}

// ── memory a2a tasks ─────────────────────────────────────────────────────────

var (
	a2aTasksContextID     string
	a2aTasksStatus        string
	a2aTasksHistoryLength int
)

var a2aTasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "Inspect and manage A2A tasks",
	Long:  "Get, list, and cancel A2A tasks.",
}

var a2aTasksGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get an A2A task by ID",
	Long: `Get the current state of an A2A task.

Use --history-length to include history messages. Use --json for raw output.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getA2AClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		task, err := client.GetTask(ctx, args[0], a2aTasksHistoryLength)
		if err != nil {
			if a2a.IsNotFound(err) {
				return fmt.Errorf("task '%s' not found", args[0])
			}
			return fmt.Errorf("failed to get task: %w", err)
		}

		if output == "json" {
			return a2aPrintJSON(cmd.OutOrStdout(), task)
		}
		printA2ATask(task)
		return nil
	},
}

var a2aTasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List A2A tasks",
	Long: `List project-scoped A2A tasks.

Optionally filter by --context <id> and/or --status <state>. Use --json for
the raw JSON response.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getA2AClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		resp, err := client.ListTasks(ctx, a2a.ListTasksRequest{
			ContextID: a2aTasksContextID,
			Status:    a2aTasksStatus,
		})
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		if output == "json" {
			return a2aPrintJSON(cmd.OutOrStdout(), resp)
		}

		if len(resp.Tasks) == 0 {
			fmt.Println("No tasks found")
			return nil
		}

		t := internalui.NewTable(internalui.TableConfig{Compact: true})
		t.SetHeaders([]string{"TASK ID", "CONTEXT", "STATUS", "ARTIFACTS"})
		for _, task := range resp.Tasks {
			t.AddRow([]string{
				task.ID,
				task.ContextID,
				string(task.Status.State),
				strconv.Itoa(len(task.Artifacts)),
			})
		}
		fmt.Print(t.Render())
		return nil
	},
}

var a2aTasksCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel an A2A task",
	Long: `Request cancellation of a running or queued A2A task.

The task transitions to TASK_STATE_CANCELED. Cancelling a terminal task is
rejected.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getA2AClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		task, err := client.CancelTask(ctx, args[0])
		if err != nil {
			switch {
			case a2a.IsNotFound(err):
				return fmt.Errorf("task '%s' not found", args[0])
			case a2a.Reason(err) == "TASK_NOT_CANCELABLE":
				return fmt.Errorf("cannot cancel task %s: task is already in a terminal state", args[0])
			}
			return fmt.Errorf("failed to cancel task: %w", err)
		}

		if output == "json" {
			return a2aPrintJSON(cmd.OutOrStdout(), task)
		}
		fmt.Printf("Task %s cancellation requested (status: %s)\n", task.ID, task.Status.State)
		return nil
	},
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// a2aPrintJSON encodes v as indented JSON to w.
func a2aPrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// newA2AMessageID generates a random message id for A2A messages.
func newA2AMessageID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ── Registration ─────────────────────────────────────────────────────────────

func init() {
	a2aSendCmd.Flags().StringVar(&a2aSendMessage, "message", "", "Input message for the agent (required)")
	a2aSendCmd.Flags().StringVar(&a2aSendTaskID, "task", "", "Resume an input-required task by ID")
	a2aSendCmd.Flags().StringVar(&a2aSendMode, "mode", "sync", "Execution mode: sync, async, stream")

	a2aTasksGetCmd.Flags().IntVar(&a2aTasksHistoryLength, "history-length", 0, "Number of history messages to include")
	a2aTasksListCmd.Flags().StringVar(&a2aTasksContextID, "context", "", "Filter tasks by context ID")
	a2aTasksListCmd.Flags().StringVar(&a2aTasksStatus, "status", "", "Filter tasks by state (e.g. TASK_STATE_COMPLETED)")

	a2aTasksCmd.AddCommand(a2aTasksGetCmd)
	a2aTasksCmd.AddCommand(a2aTasksListCmd)
	a2aTasksCmd.AddCommand(a2aTasksCancelCmd)

	a2aCmd.AddCommand(a2aDiscoverCmd)
	a2aCmd.AddCommand(a2aSendCmd)
	a2aCmd.AddCommand(a2aTasksCmd)

	rootCmd.AddCommand(a2aCmd)
}
