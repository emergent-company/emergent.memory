package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/auth"
	"github.com/emergent-company/emergent.memory/apps/cli/internal/config"
	"github.com/emergent-company/emergent.memory/apps/cli/internal/envfile"
	internalui "github.com/emergent-company/emergent.memory/apps/cli/internal/ui"
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

Prints a Setting/Value table with: Server URL, API Key (masked), Email,
Organization ID, Project ID, Debug mode, and the Config File path. Values are
merged from the config file and any overriding environment variables.

Secrets are masked to the first and last 4 characters when they are long
enough for that to leave the rest unknown; shorter values print as a fixed
placeholder instead.`,
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
			table := internalui.NewTable(internalui.TableConfig{Compact: true})
			table.SetHeaders([]string{"Setting", "Value"})

			serverURL := cfg.ServerURL
			if creds != nil && creds.IssuerURL != "" && creds.IssuerURL != cfg.ServerURL {
				serverURL = fmt.Sprintf("%s (config) / %s (authenticated)", cfg.ServerURL, creds.IssuerURL)
			}
			table.AddRow([]string{"Server URL", serverURL})

			if cfg.APIKey != "" {
				table.AddRow([]string{"API Key", maskSecret(cfg.APIKey) + " (configured)"})
			} else {
				table.AddRow([]string{"API Key", "(not set)"})
			}

			table.AddRow([]string{"Email", cfg.Email})
			table.AddRow([]string{"Organization ID", cfg.OrgID})
			table.AddRow([]string{"Project ID", cfg.ProjectID})
			table.AddRow([]string{"Debug", fmt.Sprintf("%v", cfg.Debug)})
			table.AddRow([]string{"Config File", configPath})
			table.AddRow([]string{"Auto-Update Enabled", fmt.Sprintf("%v", cfg.AutoUpdate.Enabled)})
			table.AddRow([]string{"Auto-Update Mode", cfg.AutoUpdate.Mode})
			table.AddRow([]string{"Auto-Update Interval", cfg.AutoUpdate.CheckInterval})

			// Show environment variables from standalone .env.local if it exists
			homeDir, _ = os.UserHomeDir()
			envPath := filepath.Join(homeDir, ".memory", "config", ".env.local")
			if _, err := os.Stat(envPath); err == nil {
				content, err := os.ReadFile(envPath)
				if err == nil {
					lines := strings.Split(string(content), "\n")
					for _, line := range lines {
						if strings.HasPrefix(line, "GOOGLE_API_KEY=") {
							val := strings.TrimPrefix(line, "GOOGLE_API_KEY=")
							if val != "" {
								table.AddRow([]string{"GOOGLE_API_KEY", maskSecret(val) + " (.env.local)"})
							}
						}
						if strings.HasPrefix(line, "OPENAI_BASE_URL=") {
							val := strings.TrimPrefix(line, "OPENAI_BASE_URL=")
							table.AddRow([]string{"OPENAI_BASE_URL", val + " (.env.local)"})
						}
						if strings.HasPrefix(line, "OPENAI_API_KEY=") {
							val := strings.TrimPrefix(line, "OPENAI_API_KEY=")
							if val != "" {
								table.AddRow([]string{"OPENAI_API_KEY", maskSecret(val) + " (.env.local)"})
							}
						}
						if strings.HasPrefix(line, "LLM_MODEL=") {
							val := strings.TrimPrefix(line, "LLM_MODEL=")
							table.AddRow([]string{"LLM_MODEL", val + " (.env.local)"})
						}
					}
				}
			}

			fmt.Print(table.Render())
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	return cmd
}

// Settable keys and their mapping for standalone .env.local
var standaloneEnvKeys = map[string]string{
	"google_api_key":  "GOOGLE_API_KEY",
	"openai_base_url": "OPENAI_BASE_URL",
	"openai_api_key":  "OPENAI_API_KEY",
	"llm_model":       "LLM_MODEL",
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

// secretConfigKeys lists the config.yaml keys whose value is a credential, so
// `config set` must not echo it back. stdout is copied into terminal
// scrollback, screen shares, and tee'd CI logs; printing the value there
// defeats the 0600 config file (#2068, #2075, #2081, #2110, #2118) on the
// normal path. This is a classification, not blanket suppression: URLs, email
// and IDs are identifiers, so their echo is a useful confirmation and is kept.
// A key added to configYAMLKeys without being considered here is echoed, which
// is why the switch below and this set have to be updated together.
var secretConfigKeys = map[string]bool{
	"api_key": true,
}

// confirmConfigSet prints the outcome of a config.yaml write. Secret values are
// replaced by an explicit marker: the confirmation still proves the key was
// written, without putting the credential on stdout. The .env.local branch
// already prints the key name only; this brings the other branch in line.
func confirmConfigSet(key, value string, configPath string) {
	if secretConfigKeys[key] {
		fmt.Printf("%s updated (value hidden)\n", key)
	} else {
		fmt.Printf("%s updated to: %s\n", key, value)
	}
	fmt.Printf("Configuration saved to: %s\n", configPath)
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
  google_api_key              Google AI API key (for LLM features)
  openai_base_url             OpenAI-compatible base URL (e.g., http://localhost:11434/v1)
  openai_api_key              OpenAI-compatible API key (optional for local servers)
  llm_model                   LLM model name (e.g., llama3, mistral)

For standalone installations, google_api_key is saved to .env.local.
All other keys are saved to config.yaml.

Credential values (api_key, google_api_key, openai_api_key) are never echoed:
the confirmation names the key and reports the value as hidden, so a secret
does not land in terminal scrollback or tee'd CI logs. Non-secret values
(server_url, email, org_id, project_id) are still printed back.`,
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

			// Ensure parent directory exists. Left at 0755 on purpose: --config
			// may point at a system-wide location shared with other users, and
			// this is the parent of config.yaml, not of the credentials file.
			// The credential-bearing ~/.memory/config directory is created
			// 0700 by the installer (see installer.CreateDirectories).
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

			confirmConfigSet(key, value, configPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	return cmd
}

// updateEnvFileKey writes one key into the CLI's .env.local at envLocalMode
// through the shared writer. Every env-file edit in this CLI goes through
// apps/cli/internal/envfile, so this wrapper only pins the mode.
func updateEnvFileKey(envPath, key, value string) error {
	return envfile.Update(envPath, key, value, envLocalMode)
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(newConfigSetCmd())
	configCmd.AddCommand(newConfigSetServerCmd())
	configCmd.AddCommand(newConfigSetCredentialsCmd())
	configCmd.AddCommand(newConfigShowCmd())
}
