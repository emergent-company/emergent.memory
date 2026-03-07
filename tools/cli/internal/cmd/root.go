package cmd

import (
	"fmt"
	"os"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/completion"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile      string
	serverURL    string
	output       string
	debug        bool
	noColor      bool
	compact      bool
	projectID    string
	projectToken string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "memory",
	Short: "CLI tool for Memory platform",
	Long: `Command-line interface for the Memory knowledge base platform.

Manage projects, documents, graph objects, AI agents, and MCP integrations.

For self-hosted deployments, use 'memory server' to install and manage your server.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		if configPath == "" {
			configPath = config.DiscoverPath("")
		}
		cfg, err := config.LoadWithEnv(configPath)
		if err != nil {
			return nil // non-fatal — commands handle missing config themselves
		}

		// If MEMORY_PROJECT is set (name/slug), always resolve it to a project
		// ID and override any existing MEMORY_PROJECT_ID. MEMORY_PROJECT is
		// the intentional per-workspace override (set in .env.local) and must
		// win over any placeholder value that may be present in .env.
		if projectName := os.Getenv("MEMORY_PROJECT"); projectName != "" {
			c, clientErr := getClient(cmd)
			if clientErr == nil {
				if id, resolveErr := resolveProjectNameOrID(c, projectName); resolveErr == nil {
					cfg.ProjectID = id
					// Propagate the resolved ID so downstream code (TUI, commands)
					// that re-loads config or calls getClient picks it up.
					_ = os.Setenv("MEMORY_PROJECT_ID", id)
				}
				// Resolution failure is silent — commands that need a project
				// will surface the error themselves.
			}
		}

		printProjectIndicator(cmd, cfg)
		return nil
	},
}

// NewRootCommand creates and returns the root command
// This function is used for testing and allows dependency injection
func NewRootCommand() *cobra.Command {
	return rootCmd
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global persistent flags (available to all subcommands)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.memory/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "Memory server URL")
	rootCmd.PersistentFlags().StringVar(&output, "output", "table", "output format (table, json, yaml, csv)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVar(&compact, "compact", false, "use compact output layout")
	rootCmd.PersistentFlags().StringVar(&projectID, "project", "", "project ID (overrides config and environment)")
	rootCmd.PersistentFlags().StringVar(&projectToken, "project-token", "", "project token (overrides config and environment)")

	// Bind flags to viper for config file support
	_ = viper.BindPFlag("server", rootCmd.PersistentFlags().Lookup("server"))
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("no-color", rootCmd.PersistentFlags().Lookup("no-color"))
	_ = viper.BindPFlag("ui.compact", rootCmd.PersistentFlags().Lookup("compact"))
	_ = viper.BindPFlag("project_id", rootCmd.PersistentFlags().Lookup("project"))
	_ = viper.BindPFlag("project_token", rootCmd.PersistentFlags().Lookup("project-token"))

	// Command groups for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: "knowledge", Title: "Knowledge Base"},
		&cobra.Group{ID: "ai", Title: "Agents & AI"},
		&cobra.Group{ID: "account", Title: "Account & Access"},
	)

	// Register completion functions for flags
	_ = rootCmd.RegisterFlagCompletionFunc("output", completion.OutputFormatCompletionFunc())
}

// initConfig reads in config file and ENV variables if set
func initConfig() {
	// Automatically load .env files if present (ignore errors as they are optional)
	_ = godotenv.Load(".env.local")
	_ = godotenv.Load(".env")

	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Search config in home directory with name ".memory" (without extension)
		viper.AddConfigPath(home + "/.memory")
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")
	}

	// Environment variables
	viper.SetEnvPrefix("MEMORY")
	viper.AutomaticEnv() // read in environment variables that match

	// Set defaults for new config fields
	viper.SetDefault("cache.ttl", "5m")
	viper.SetDefault("cache.enabled", true)
	viper.SetDefault("ui.compact", false)
	viper.SetDefault("ui.color", "auto")
	viper.SetDefault("ui.pager", true)
	viper.SetDefault("query.default_limit", 50)
	viper.SetDefault("query.default_sort", "updated_at:desc")
	viper.SetDefault("completion.timeout", "2s")

	// If a config file is found, read it in
	if err := viper.ReadInConfig(); err == nil {
		if debug {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
	}
}
