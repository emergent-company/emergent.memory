package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"strings"

	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

// detectProjectSource returns a short human-readable label describing how the
// active project scope was supplied.
func detectProjectSource(cmd *cobra.Command) string {
	if cmd.Flags().Changed("project-token") {
		return "project API key via --project-token"
	}
	if os.Getenv("MEMORY_PROJECT_TOKEN") != "" {
		return "project API key"
	}
	if os.Getenv("MEMORY_PROJECT") != "" {
		return "MEMORY_PROJECT"
	}
	return "config file"
}

// printProjectIndicator writes a single-line project context breadcrumb to
// stderr when a project scope is active (either via a project token or a
// resolved project name). It is a no-op when:
//   - no project scope is active
//   - stderr is not a terminal (piped / CI usage)
func printProjectIndicator(cmd *cobra.Command, cfg *config.Config) {
	// Active if a project token is set OR a project ID has been resolved.
	hasScope := cfg.ProjectToken != "" || cfg.ProjectID != ""
	if !hasScope {
		return
	}

	// Only show in interactive terminals — keep piped/CI output clean.
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return
	}

	// Resolve display name:
	// 1. Explicit MEMORY_PROJECT env var (name/slug)
	// 2. cfg.ProjectName (set from MEMORY_PROJECT slug in config.go)
	// 3. API lookup when only a token is available
	// 4. Masked token as last resort
	name := cfg.ProjectName
	if name == "" && os.Getenv("MEMORY_PROJECT") != "" {
		name = os.Getenv("MEMORY_PROJECT")
	}
	if name == "" && cfg.ProjectToken != "" {
		name = resolveProjectNameFromToken(cfg)
	}
	if name == "" && cfg.ProjectToken != "" {
		name = maskAPIKey(cfg.ProjectToken)
	}
	if name == "" {
		name = cfg.ProjectID // UUID fallback — at least show something
	}

	source := detectProjectSource(cmd)

	useColor := config.ShouldUseColor(noColor)
	if useColor {
		fmt.Fprintf(os.Stderr, "\033[2;36mProject: %s  (%s)\033[0m\n", name, source)
	} else {
		fmt.Fprintf(os.Stderr, "Project: %s  (%s)\n", name, source)
	}
}

// getAccountClient creates a client that uses account-level credentials,
// skipping any project-scoped token. This is for commands that operate at the
// account or org level (e.g. listing all projects, managing account tokens).
func getAccountClient(cmd *cobra.Command) (*client.Client, error) {
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}

	cfg, err := config.LoadWithEnv(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Apply persistent flag values from global viper (same as getClient).
	if v := viper.GetString("server"); v != "" {
		cfg.ServerURL = v
	}
	// Note: we intentionally do NOT apply viper's "project_token" here —
	// the whole point is to skip project-scoped tokens.

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("no server URL configured. Set MEMORY_SERVER_URL or run: memory config set-server <url>")
	}

	return client.NewAccountClient(cfg)
}

// resolveProjectNameFromToken attempts a quick API call to get the project name
// for the given project token. Returns empty string on any error.
func resolveProjectNameFromToken(cfg *config.Config) string {
	if cfg.ServerURL == "" || cfg.ProjectToken == "" {
		return ""
	}
	projects, err := fetchProjects(cfg.ServerURL, cfg.ProjectToken)
	if err != nil || len(projects) == 0 {
		return ""
	}
	return projects[0].Name
}

// resolveProjectContext gets the project ID from the global flag, config, or environment.
func resolveProjectContext(cmd *cobra.Command, flagValue string) (string, error) {
	nameOrID := flagValue

	// If not provided via command-specific flag, check global flag/viper
	if nameOrID == "" {
		nameOrID = viper.GetString("project_id")
	}

	// Load config once; used for the project ID fallback and the picker.
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}
	cfg, cfgErr := config.LoadWithEnv(configPath)

	if nameOrID == "" {
		if cfgErr != nil {
			return "", fmt.Errorf("failed to load config: %w", cfgErr)
		}
		if cfg.ProjectID != "" {
			nameOrID = cfg.ProjectID
		}
	}

	if nameOrID == "" {
		pickedID, pickErr := promptProjectPicker(cmd, cfg)
		if pickErr != nil {
			// User cancelled or timed out — surface the picker error directly.
			return "", pickErr
		}
		if pickedID != "" {
			return pickedID, nil
		}

		// Non-interactive or no projects — fall through to the original error.
		return "", fmt.Errorf("project is required. Use --project flag, set MEMORY_PROJECT in .env.local, or configure it in your config file")
	}

	// If it's already a UUID, validate it exists on the server.
	if isUUID(nameOrID) {
		c, err := getClient(cmd)
		if err != nil {
			// Can't reach server — return the ID optimistically.
			return nameOrID, nil
		}
		if _, err := c.SDK.Projects.Get(context.Background(), nameOrID, nil); err != nil {
			return "", fmt.Errorf("project %s not found — it may have been deleted or belong to a different server.\nUpdate your config with: memory config set project_id <id>", nameOrID)
		}
		return nameOrID, nil
	}

	// Otherwise resolve the name to an ID
	c, err := getClient(cmd)
	if err != nil {
		return "", err
	}

	return resolveProjectNameOrID(c, nameOrID)
}

// isNonInteractive returns true when the CLI is running in a context where
// interactive prompts must not be shown: a CI environment (CI env var set),
// an explicit opt-out (NO_PROMPT env var), or when stdin is not a real
// terminal (piped / redirected input).
func isNonInteractive() bool {
	if os.Getenv("CI") != "" || os.Getenv("NO_PROMPT") != "" {
		return true
	}
	return !term.IsTerminal(int(os.Stdin.Fd()))
}

// promptProjectPicker attempts an interactive project selection when no project
// has been configured. It is a no-op in non-interactive contexts and returns
// ("", nil) there so the caller falls through to its normal error path.
//
// When exactly one project exists, it is auto-selected without showing the
// picker. When multiple projects exist, a Bubbletea arrow-key list is rendered
// to stderr (keeping stdout clean) with a 30-second timeout.
//
// On success the selected project ID is returned and cfg.ProjectID is updated
// in-memory so downstream code picks it up without re-fetching.
func promptProjectPicker(cmd *cobra.Command, cfg *config.Config) (string, error) {
	if isNonInteractive() {
		return "", nil
	}

	c, err := getClient(cmd)
	if err != nil {
		return "", nil // can't reach server — fall through to original error
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	projectList, err := c.SDK.Projects.List(ctx, &projects.ListOptions{})
	if err != nil || len(projectList) == 0 {
		return "", nil // nothing to pick — fall through
	}

	// Single project: auto-select silently.
	if len(projectList) == 1 {
		id := projectList[0].ID
		cfg.ProjectID = id
		cfg.ProjectName = projectList[0].Name
		fmt.Fprintf(os.Stderr, "\033[2;36mAuto-selected project: %s\033[0m\n", projectList[0].Name)
		return id, nil
	}

	// Multiple projects: show the interactive picker.
	items := make([]PickerItem, len(projectList))
	for i, p := range projectList {
		items[i] = PickerItem{ID: p.ID, Name: p.Name}
	}

	id, name, err := PickProject(items, 30*time.Second, os.Stderr)
	if err != nil {
		return "", err
	}

	cfg.ProjectID = id
	cfg.ProjectName = name
	return id, nil
}

// promptResourcePicker shows an interactive picker for any list of PickerItems.
// It is a no-op in non-interactive contexts (returns "", "", nil).
// When exactly one item is present it is auto-selected silently.
// When multiple items are present a Bubbletea arrow-key list is shown on stderr.
func promptResourcePicker(title string, items []PickerItem) (id, name string, err error) {
	if isNonInteractive() {
		return "", "", nil
	}
	if len(items) == 0 {
		return "", "", nil
	}
	if len(items) == 1 {
		fmt.Fprintf(os.Stderr, "\033[2;36mAuto-selected: %s\033[0m\n", items[0].Name)
		return items[0].ID, items[0].Name, nil
	}

	// Temporarily override the title rendered by the Bubbletea list.
	// PickProject already uses a fixed title; we replace it by passing a
	// copy of items with a custom-titled model. Since PickProject accepts
	// a slice and constructs its own model, we call the lower-level helper
	// directly so we can set the title.
	return pickResourceWithTitle(title, items, 30*time.Second, os.Stderr)
}

// isAuthError returns true when err indicates the request was rejected due to
// a missing, invalid, or expired authentication token (HTTP 401). It handles
// both *sdkerrors.Error values returned by the SDK and the raw error strings
// produced by commands that make HTTP requests directly (ask, query).
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	// SDK errors carry a structured status code.
	if sdkerrors.IsUnauthorized(err) {
		return true
	}
	// Raw errors from ask/query include the HTTP status in the message.
	s := err.Error()
	return strings.Contains(s, "status 401") ||
		strings.Contains(s, "missing_token") ||
		strings.Contains(s, "invalid_token") ||
		strings.Contains(s, "token_expired") ||
		strings.Contains(s, "Missing authorization token")
}

// PrintAuthError writes a friendly re-authentication prompt to stderr and
// returns the exit-ready error to be returned from main (so the original
// raw error is suppressed).
func PrintAuthError() {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "\033[0;33mYour session has expired or you are not authenticated.\033[0m")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Run the following command to log in:")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  \033[1mmemory login\033[0m")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Then retry your command.")
}
