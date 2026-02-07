package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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

			table.Append("Server URL", cfg.ServerURL)

			if cfg.APIKey != "" {
				maskedKey := cfg.APIKey[:8] + "..." + cfg.APIKey[len(cfg.APIKey)-4:]
				table.Append("API Key", maskedKey+" (configured)")
			} else {
				table.Append("API Key", "(not set)")
			}

			table.Append("Email", cfg.Email)
			table.Append("Organization ID", cfg.OrgID)
			table.Append("Project ID", cfg.ProjectID)
			table.Append("Debug", fmt.Sprintf("%v", cfg.Debug))
			table.Append("Config File", configPath)

			return table.Render()
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	return cmd
}

func newConfigLogoutCmd() *cobra.Command {
	var credsPath string

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if credsPath == "" {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to get home directory: %w", err)
				}
				credsPath = filepath.Join(homeDir, ".emergent", "credentials.json")
			}

			if _, err := os.Stat(credsPath); os.IsNotExist(err) {
				fmt.Println("No credentials found")
				return nil
			}

			if err := os.Remove(credsPath); err != nil {
				return fmt.Errorf("failed to remove credentials: %w", err)
			}

			fmt.Println("Logged out successfully")
			fmt.Printf("Credentials removed from: %s\n", credsPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&credsPath, "credentials-path", "", "path to credentials file")
	return cmd
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(newConfigSetServerCmd())
	configCmd.AddCommand(newConfigSetCredentialsCmd())
	configCmd.AddCommand(newConfigShowCmd())
	configCmd.AddCommand(newConfigLogoutCmd())
}
