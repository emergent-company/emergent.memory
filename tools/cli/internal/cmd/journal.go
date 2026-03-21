package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"

	"github.com/spf13/cobra"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type journalEntry struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	EventType  string         `json:"event_type"`
	EntityType *string        `json:"entity_type,omitempty"`
	EntityID   *string        `json:"entity_id,omitempty"`
	ObjectType *string        `json:"object_type,omitempty"`
	ActorType  string         `json:"actor_type"`
	ActorID    *string        `json:"actor_id,omitempty"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  string         `json:"created_at"`
	Notes      []journalNote  `json:"notes,omitempty"`
}

type journalNote struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"project_id"`
	JournalID *string `json:"journal_id,omitempty"`
	Body      string  `json:"body"`
	ActorType string  `json:"actor_type"`
	ActorID   *string `json:"actor_id,omitempty"`
	CreatedAt string  `json:"created_at"`
}

type journalResponse struct {
	Entries []journalEntry `json:"entries"`
	Notes   []journalNote  `json:"notes"`
	Total   int            `json:"total"`
}

// ── Flags ─────────────────────────────────────────────────────────────────────

var (
	journalListSince           string
	journalListLimit           int
	journalListBranch          string
	journalListIncludeBranches bool
	journalNoteEntry           string
)

// ── Commands ──────────────────────────────────────────────────────────────────

var journalCmd = &cobra.Command{
	Use:     "journal",
	Short:   "View and annotate the project journal",
	GroupID: "knowledge",
}

var journalListCmd = &cobra.Command{
	Use:   "list",
	Short: "List project journal entries",
	Long: `List recent graph mutations and notes for the current project.

Output is a log-style feed showing each event with timestamp, actor, event type,
and relevant details. Notes are printed inline with entries or at the end (for
standalone notes).

Use --since to filter by age (e.g. 7d, 24h, 1h). Defaults to last 7 days.
Use --limit to control the maximum number of entries returned.
Use --output json for machine-readable output.`,
	RunE: runJournalList,
}

var journalNoteCmd = &cobra.Command{
	Use:   "note [text]",
	Short: "Add a note to the project journal",
	Long: `Add a markdown note to the project journal.

The note body can be passed as an argument, piped via stdin, or entered
interactively in your $EDITOR when no argument or stdin is provided.

Use --entry <journal-entry-id> to attach the note to a specific journal entry.

Examples:
  memory journal note "Skipped worker services — need schema clarification first."
  echo "Some context" | memory journal note
  memory journal note --entry <entry-id> "Removed legacy auth service"`,
	RunE: runJournalNote,
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// journalGet makes a GET request to /api/graph/journal with query params.
func journalGet(cmd *cobra.Command, projectID string, params url.Values) ([]byte, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, fmt.Errorf("cannot initialise client: %w", err)
	}
	u := strings.TrimRight(c.BaseURL(), "/") + "/api/graph/journal"
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// Inject project scope header if needed.
	if projectID != "" {
		req.Header.Set("X-Project-Id", projectID)
	}
	resp, err := c.SDK.Do(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach server at %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// journalPost makes a POST request to /api/graph/journal/notes.
func journalPost(cmd *cobra.Command, projectID string, payload any) ([]byte, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, fmt.Errorf("cannot initialise client: %w", err)
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	u := strings.TrimRight(c.BaseURL(), "/") + "/api/graph/journal/notes"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if projectID != "" {
		req.Header.Set("X-Project-Id", projectID)
	}
	resp, err := c.SDK.Do(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach server at %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// actorLabel returns "[agent]", "[user]", or "[system]".
func actorLabel(actorType string) string {
	switch actorType {
	case "agent":
		return "[agent]"
	case "user":
		return "[user]"
	default:
		return "[system]"
	}
}

// formatTimestamp parses an RFC3339 string and formats it as "2006-01-02 15:04:05".
func formatTimestamp(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return s
		}
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// metaStr extracts a string value from metadata by key.
func metaStr(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	v, _ := metadata[key].(string)
	return v
}

// metaInt extracts an int-like value from metadata as an int.
func metaInt(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch v := metadata[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// printJournalEntry prints a single journal entry in the log format.
func printJournalEntry(e journalEntry) {
	ts := formatTimestamp(e.CreatedAt)
	actor := actorLabel(e.ActorType)
	et := strings.ToUpper(e.EventType)

	switch e.EventType {
	case "created", "updated", "deleted", "restored":
		key := metaStr(e.Metadata, "key")
		name := metaStr(e.Metadata, "name")
		objType := metaStr(e.Metadata, "object_type")
		nameStr := ""
		if name != "" {
			nameStr = fmt.Sprintf("  %q", name)
		}
		fmt.Printf("%-19s  %-8s  %-9s  %-16s  %-30s%s\n",
			ts, actor, et, objType, key, nameStr)

	case "related":
		srcKey := metaStr(e.Metadata, "src_key")
		relType := metaStr(e.Metadata, "rel_type")
		dstKey := metaStr(e.Metadata, "dst_key")
		fmt.Printf("%-19s  %-8s  %-9s  %s -> %s -> %s\n",
			ts, actor, et, srcKey, relType, dstKey)

	case "batch":
		n := metaInt(e.Metadata, "created")
		if byType, ok := e.Metadata["by_type"].(map[string]any); ok && len(byType) > 0 {
			// Objects batch
			parts := make([]string, 0, len(byType))
			for typeName, cnt := range byType {
				parts = append(parts, fmt.Sprintf("%s x%v", typeName, cnt))
			}
			sort.Strings(parts)
			fmt.Printf("%-19s  %-8s  %-9s  %d objects created (%s)\n",
				ts, actor, et, n, strings.Join(parts, ", "))
		} else {
			// Relationships batch
			fmt.Printf("%-19s  %-8s  %-9s  %d relationships created\n",
				ts, actor, et, n)
		}

	case "merge":
		objects := metaInt(e.Metadata, "objects_merged")
		rels := metaInt(e.Metadata, "relationships_merged")
		fmt.Printf("%-19s  %-8s  %-9s  %d objects, %d relationships\n",
			ts, actor, et, objects, rels)

	case "note":
		// Standalone notes printed separately; skip here.
		return

	default:
		fmt.Printf("%-19s  %-8s  %-9s\n", ts, actor, et)
	}

	// Print attached notes indented under the entry.
	for _, n := range e.Notes {
		printAttachedNote(n)
	}
}

// renderMarkdown renders markdown text for terminal display using glamour.
// Falls back to plain text if rendering fails or stdout is not a terminal.
func renderMarkdown(body string) string {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return body
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		return body
	}
	rendered, err := r.Render(body)
	if err != nil {
		return body
	}
	return rendered
}

// printAttachedNote prints a note attached to a journal entry.
func printAttachedNote(n journalNote) {
	ts := formatTimestamp(n.CreatedAt)
	actor := actorLabel(n.ActorType)
	body := strings.TrimSpace(n.Body)
	fmt.Printf("  %-19s  %-8s  NOTE\n", ts, actor)
	rendered := renderMarkdown(body)
	// Indent each line of the rendered output.
	for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()
}

// printStandaloneNote prints a standalone journal note.
func printStandaloneNote(n journalNote) {
	ts := formatTimestamp(n.CreatedAt)
	actor := actorLabel(n.ActorType)
	body := strings.TrimSpace(n.Body)
	fmt.Printf("%-19s  %-8s  NOTE\n", ts, actor)
	rendered := renderMarkdown(body)
	// Indent each line of the rendered output.
	for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()
}

// openEditor opens $EDITOR (or vi) with initialContent and returns the saved text.
func openEditor(initialContent string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	f, err := os.CreateTemp("", "memory-note-*.md")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	if _, err := f.WriteString(initialContent); err != nil {
		f.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("read temp file: %w", err)
	}
	return strings.TrimSpace(string(content)), nil
}

// ── Command implementations ───────────────────────────────────────────────────

func runJournalList(cmd *cobra.Command, _ []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	params := url.Values{
		"since": {journalListSince},
		"limit": {fmt.Sprintf("%d", journalListLimit)},
	}
	if journalListBranch != "" {
		params.Set("branch", journalListBranch)
	}
	if journalListIncludeBranches {
		params.Set("include_branches", "true")
	}

	body, err := journalGet(cmd, projectID, params)
	if err != nil {
		return err
	}

	var resp journalResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}

	// JSON output
	if output == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if len(resp.Entries) == 0 && len(resp.Notes) == 0 {
		fmt.Printf("No journal entries found (last %s).\n", journalListSince)
		return nil
	}

	// Print header
	fmt.Printf("Project journal — last %s  (%d entries)\n\n", journalListSince, resp.Total)

	// Print entries in order
	for _, e := range resp.Entries {
		printJournalEntry(e)
	}

	// Print standalone notes
	if len(resp.Notes) > 0 {
		if len(resp.Entries) > 0 {
			fmt.Println()
		}
		for _, n := range resp.Notes {
			printStandaloneNote(n)
		}
	}

	return nil
}

func runJournalNote(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	var noteBody string

	// Priority: arg > stdin pipe > editor
	if len(args) > 0 {
		noteBody = strings.TrimSpace(strings.Join(args, " "))
	} else if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Piped input
		scanner := bufio.NewScanner(os.Stdin)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		noteBody = strings.TrimSpace(strings.Join(lines, "\n"))
	} else {
		// Interactive: open editor
		noteBody, err = openEditor("")
		if err != nil {
			return err
		}
	}

	if noteBody == "" {
		return fmt.Errorf("note body is required")
	}

	type addNotePayload struct {
		Body      string  `json:"body"`
		JournalID *string `json:"journal_id,omitempty"`
	}

	payload := addNotePayload{Body: noteBody}
	if journalNoteEntry != "" {
		payload.JournalID = &journalNoteEntry
	}

	respBody, err := journalPost(cmd, projectID, payload)
	if err != nil {
		return err
	}

	var note journalNote
	if err := json.Unmarshal(respBody, &note); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if output == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(note)
	}

	fmt.Printf("Note added (id: %s)\n", note.ID)
	return nil
}

// ── Init ──────────────────────────────────────────────────────────────────────

func init() {
	journalListCmd.Flags().StringVar(&journalListSince, "since", "7d", "Show entries from the last duration (e.g. 7d, 24h, 1h)")
	journalListCmd.Flags().IntVar(&journalListLimit, "limit", 100, "Maximum number of entries to return")
	journalListCmd.Flags().StringVar(&journalListBranch, "branch", "", "Branch name or UUID (omit for main branch)")
	journalListCmd.Flags().BoolVar(&journalListIncludeBranches, "include-branches", false, "Include merged branches in the feed alongside the main branch")

	journalNoteCmd.Flags().StringVar(&journalNoteEntry, "entry", "", "Journal entry ID to attach the note to")

	journalCmd.AddCommand(journalListCmd)
	journalCmd.AddCommand(journalNoteCmd)
	rootCmd.AddCommand(journalCmd)
}
