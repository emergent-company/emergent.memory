package cmd

import (
	"fmt"
	"os"

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

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(newConfigSetServerCmd())
	configCmd.AddCommand(newConfigSetCredentialsCmd())
	configCmd.AddCommand(newConfigShowCmd())
}
