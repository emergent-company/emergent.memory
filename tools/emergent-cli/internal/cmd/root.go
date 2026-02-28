package cmd

import (
	"fmt"
	"os"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/completion"
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
	Use:   "emergent-cli",
	Short: "CLI tool for Emergent platform",
	Long: `Command-line interface for interacting with the Emergent knowledge base API.

The Emergent CLI provides commands to manage projects, documents, and other
resources in your Emergent knowledge base. It supports both interactive and
non-interactive workflows with flexible output formats.`,
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
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.emergent/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "Emergent server URL")
	rootCmd.PersistentFlags().StringVar(&output, "output", "table", "output format (table, json, yaml, csv)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVar(&compact, "compact", false, "use compact output layout")
	rootCmd.PersistentFlags().StringVar(&projectID, "project-id", "", "project ID (overrides config and environment)")
	rootCmd.PersistentFlags().StringVar(&projectToken, "project-token", "", "project token (overrides config and environment)")

	// Bind flags to viper for config file support
	_ = viper.BindPFlag("server", rootCmd.PersistentFlags().Lookup("server"))
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("no-color", rootCmd.PersistentFlags().Lookup("no-color"))
	_ = viper.BindPFlag("ui.compact", rootCmd.PersistentFlags().Lookup("compact"))
	_ = viper.BindPFlag("project_id", rootCmd.PersistentFlags().Lookup("project-id"))
	_ = viper.BindPFlag("project_token", rootCmd.PersistentFlags().Lookup("project-token"))

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

		// Search config in home directory with name ".emergent" (without extension)
		viper.AddConfigPath(home + "/.emergent")
		viper.SetConfigType("yaml")
		viper.SetConfigName("config")
	}

	// Environment variables
	viper.SetEnvPrefix("EMERGENT")
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
