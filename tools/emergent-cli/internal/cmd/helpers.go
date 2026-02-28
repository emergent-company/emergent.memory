package cmd

import (
	"fmt"
	"os"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// resolveProjectContext gets the project ID from the global flag, config, or environment.
func resolveProjectContext(cmd *cobra.Command, flagValue string) (string, error) {
	nameOrID := flagValue

	isFlagChanged := cmd.Flags().Changed("project-id") || flagValue != ""

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
		return "", fmt.Errorf("project is required. Use --project-id flag, set EMERGENT_PROJECT_ID in .env.local, or configure it in your config file")
	}

	// Print informative message if it was loaded from env (not via flag directly)
	if !isFlagChanged {
		// If we have a project name from the env, print it
		projectName := viper.GetString("project_name")
		if projectName != "" {
			fmt.Fprintf(os.Stderr, "Using project context: %s (from environment)\n", projectName)
		} else {
			fmt.Fprintf(os.Stderr, "Using project context ID: %s (from environment/config)\n", nameOrID)
		}
	}

	// If it's already a UUID, return directly
	if isUUID(nameOrID) {
		return nameOrID, nil
	}

	// Otherwise resolve the name to an ID
	c, err := getClient(cmd)
	if err != nil {
		return "", err
	}

	return resolveProjectNameOrID(c, nameOrID)
}
