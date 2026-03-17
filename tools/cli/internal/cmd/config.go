package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/auth"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:     "config",
	Short:   "Manage CLI configuration",
	Long:    "Configure server URL, credentials, and other settings for the Memory CLI",
	GroupID: "account",
}

func newConfigSetServerCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "set-server [url]",
		Short: "Set the Memory server URL",
		Long: `Set the Memory server URL in the CLI configuration file.

Prints the new server URL and the path to the configuration file where the
setting was saved. Use this to point the CLI at a different server environment
(e.g. local dev vs production).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverURL := args[0]
			if configPath == "" {
				configPath = config.DiscoverPath("")
			}

			cfg, err := config.Load(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if cfg == nil {
				cfg = &config.Config{}
			}

			cfg.ServerURL = serverURL

			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("Server URL updated to: %s\n", serverURL)
			fmt.Printf("Configuration saved to: %s\n", configPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	return cmd
}

func newConfigSetCredentialsCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "set-credentials [email]",
		Short: "Set the email for authentication",
		Long: `Set the email address used for authentication in the CLI configuration file.

Prints the email that was set and the path to the configuration file where
the setting was saved.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]
			if configPath == "" {
				configPath = config.DiscoverPath("")
			}

			cfg, err := config.Load(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if cfg == nil {
				cfg = &config.Config{}
			}

			cfg.Email = email

			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("Email set to: %s\n", email)
			fmt.Printf("Configuration saved to: %s\n", configPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Display current configuration",
		Long: `Display the current CLI configuration as a table.

Prints a Setting/Value table with: Server URL, API Key (masked, showing only
the first 8 and last 4 characters), Email, Organization ID, Project ID, Debug
mode, and the Config File path. Values are merged from the config file and any
overriding environment variables.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = config.DiscoverPath("")
			}

			cfg, err := config.LoadWithEnv(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if cfg == nil {
				cfg = &config.Config{}
			}

			// Load credentials to check the authenticated server URL
			homeDir, _ := os.UserHomeDir()
			credsPath := filepath.Join(homeDir, ".memory", "credentials.json")
			creds, _ := auth.Load(credsPath)

			fmt.Println("Current Configuration:")
			table := tablewriter.NewWriter(os.Stdout)
			table.Header("Setting", "Value")

			serverURL := cfg.ServerURL
			if creds != nil && creds.IssuerURL != "" && creds.IssuerURL != cfg.ServerURL {
				serverURL = fmt.Sprintf("%s (config) / %s (authenticated)", cfg.ServerURL, creds.IssuerURL)
			}
			_ = table.Append("Server URL", serverURL)

			if cfg.APIKey != "" {
				maskedKey := cfg.APIKey[:8] + "..." + cfg.APIKey[len(cfg.APIKey)-4:]
				_ = table.Append("API Key", maskedKey+" (configured)")
			} else {
				_ = table.Append("API Key", "(not set)")
			}

			_ = table.Append("Email", cfg.Email)
			_ = table.Append("Organization ID", cfg.OrgID)
			_ = table.Append("Project ID", cfg.ProjectID)
			_ = table.Append("Debug", fmt.Sprintf("%v", cfg.Debug))
			_ = table.Append("Config File", configPath)
			_ = table.Append("Auto-Update Enabled", fmt.Sprintf("%v", cfg.AutoUpdate.Enabled))
			_ = table.Append("Auto-Update Mode", cfg.AutoUpdate.Mode)
			_ = table.Append("Auto-Update Interval", cfg.AutoUpdate.CheckInterval)

			return table.Render()
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	return cmd
}

// Settable keys and their mapping for standalone .env.local
var standaloneEnvKeys = map[string]string{
	"google_api_key": "GOOGLE_API_KEY",
}

// Settable keys for config.yaml
var configYAMLKeys = map[string]bool{
	"server_url":                 true,
	"api_key":                    true,
	"email":                      true,
	"org_id":                     true,
	"project_id":                 true,
	"auto_update_enabled":        true,
	"auto_update_mode":           true,
	"auto_update_check_interval": true,
}

func newConfigSetCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value by key.

Supported keys:
  server_url                  Server URL (e.g., http://localhost:3002)
  api_key                     API key for authentication
  email                       Email for authentication
  org_id                      Organization ID
  project_id                  Project ID
  auto_update_enabled         Enable/disable automatic update checks (true/false)
  auto_update_mode            Update mode: notify (default) or auto
  auto_update_check_interval  How often to check for updates (e.g., 24h, 12h)
  google_api_key              Google API key (standalone installations only)

For standalone installations, google_api_key is saved to .env.local.
All other keys are saved to config.yaml.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.ToLower(args[0])
			value := args[1]

			if configPath == "" {
				configPath = config.DiscoverPath("")
			}

			// Check if this is a standalone .env.local key
			if envKey, ok := standaloneEnvKeys[key]; ok {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("cannot determine home directory: %w", err)
				}
				installDir := filepath.Join(homeDir, ".memory")
				envPath := filepath.Join(installDir, "config", ".env.local")

				if _, err := os.Stat(envPath); os.IsNotExist(err) {
					return fmt.Errorf(".env.local not found at %s\n  This setting is only available for standalone installations.\n  Run 'memory install' first to set up a standalone server", envPath)
				}

				if err := updateEnvFileKey(envPath, envKey, value); err != nil {
					return fmt.Errorf("failed to update .env.local: %w", err)
				}

				fmt.Printf("%s updated in .env.local\n", key)
				fmt.Println("Restart services to apply: memory ctl restart")
				return nil
			}

			// Check if this is a config.yaml key
			if !configYAMLKeys[key] {
				allKeys := []string{}
				for k := range configYAMLKeys {
					allKeys = append(allKeys, k)
				}
				for k := range standaloneEnvKeys {
					allKeys = append(allKeys, k)
				}
				return fmt.Errorf("unknown config key '%s'\n  Supported keys: %s", key, strings.Join(allKeys, ", "))
			}

			// Load, update, save config.yaml
			cfg, err := config.Load(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if cfg == nil {
				cfg = &config.Config{}
			}

			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}

			switch key {
			case "server_url":
				cfg.ServerURL = value
			case "api_key":
				cfg.APIKey = value
			case "email":
				cfg.Email = value
			case "org_id":
				cfg.OrgID = value
			case "project_id":
				cfg.ProjectID = value
			case "auto_update_enabled":
				cfg.AutoUpdate.Enabled = value == "true" || value == "1" || value == "yes"
			case "auto_update_mode":
				cfg.AutoUpdate.Mode = value
			case "auto_update_check_interval":
				cfg.AutoUpdate.CheckInterval = value
			}

			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("%s updated to: %s\n", key, value)
			fmt.Printf("Configuration saved to: %s\n", configPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	return cmd
}

// updateEnvFileKey updates a key=value pair in an env file, or appends it if not found.
func updateEnvFileKey(envPath, key, value string) error {
	content, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}

	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0600)
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(newConfigSetCmd())
	configCmd.AddCommand(newConfigSetServerCmd())
	configCmd.AddCommand(newConfigSetCredentialsCmd())
	configCmd.AddCommand(newConfigShowCmd())
}
