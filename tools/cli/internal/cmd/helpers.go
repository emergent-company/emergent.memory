package cmd

import (
	"context"
	"fmt"
	"os"

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

	if nameOrID == "" {
		// Fall back to config file
		configPath, _ := cmd.Flags().GetString("config")
		if configPath == "" {
			configPath = config.DiscoverPath("")
		}

		cfg, err := config.LoadWithEnv(configPath)
		if err != nil {
			return "", fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.ProjectID != "" {
			nameOrID = cfg.ProjectID
		}
	}

	if nameOrID == "" {
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
