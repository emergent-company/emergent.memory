package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  "Configure server URL, credentials, and other settings for the Emergent CLI",
}

func newConfigSetServerCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "set-server [url]",
		Short: "Set the Emergent server URL",
		Args:  cobra.ExactArgs(1),
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
		Args:  cobra.ExactArgs(1),
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

			fmt.Println("Current Configuration:")
			table := tablewriter.NewWriter(os.Stdout)
			table.Header("Setting", "Value")

			_ = table.Append("Server URL", cfg.ServerURL)

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
	"server_url": true,
	"api_key":    true,
	"email":      true,
	"org_id":     true,
	"project_id": true,
}

func newConfigSetCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value by key.

Supported keys:
  server_url      Server URL (e.g., http://localhost:3002)
  api_key         API key for authentication
  email           Email for authentication
  org_id          Organization ID
  project_id      Project ID
  google_api_key  Google API key (standalone installations only)

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
				installDir := filepath.Join(homeDir, ".emergent")
				envPath := filepath.Join(installDir, "config", ".env.local")

				if _, err := os.Stat(envPath); os.IsNotExist(err) {
					return fmt.Errorf(".env.local not found at %s\n  This setting is only available for standalone installations.\n  Run 'emergent install' first to set up a standalone server", envPath)
				}

				if err := updateEnvFileKey(envPath, envKey, value); err != nil {
					return fmt.Errorf("failed to update .env.local: %w", err)
				}

				fmt.Printf("%s updated in .env.local\n", key)
				fmt.Println("Restart services to apply: emergent ctl restart")
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
