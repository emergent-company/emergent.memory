package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/autoupdate"
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
	jsonFlag     bool
	debug        bool
	noColor      bool
	compact      bool
	projectID    string
	projectToken string
)

// updateCheckCh carries the background version-check result to PersistentPostRunE.
// It is buffered so the goroutine never blocks even if PostRunE is skipped.
var updateCheckCh chan *autoupdate.CheckResult

// skipUpdateCommands is the set of command names for which the update check
// is suppressed (they handle upgrade flow themselves, or are internal).
var skipUpdateCommands = map[string]bool{
	"upgrade":    true,
	"version":    true,
	"completion": true,
}

// isSkipCommand returns true when cmd or any ancestor is in the skip list.
func isSkipCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if skipUpdateCommands[c.Name()] {
			return true
		}
	}
	return false
}

// shouldSkipAutoUpdate returns true when auto-update logic should be bypassed
// entirely for this invocation.
func shouldSkipAutoUpdate(cmd *cobra.Command) bool {
	// Master kill-switch env var (checked before any config).
	if os.Getenv("MEMORY_NO_AUTO_UPDATE") != "" {
		return true
	}
	// Dev builds never auto-update.
	if Version == "dev" {
		return true
	}
	// Package-manager installs manage their own upgrades.
	if _, ok := isPackageManagerInstalled(); ok {
		return true
	}
	// Excluded commands.
	if isSkipCommand(cmd) {
		return true
	}
	// Config-level opt-out.
	if !viper.GetBool("auto_update.enabled") {
		return true
	}
	return false
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "memory",
	Short: "CLI tool for Memory platform",
	Long: `Command-line interface for the Memory knowledge base platform.

Manage projects, documents, graph objects, AI agents, and MCP integrations.

For self-hosted deployments, use 'memory server' to install and manage your server.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// --json is shorthand for --output json. Apply it to the global
		// output variable as well as any command-group-local output flags
		// (graph, documents, schemas) that shadow the global one.
		if jsonFlag {
			output = "json"
			graphOutputFlag = "json"
			docsOutputFlag = "json"
			schemaOutputFlag = "json"
		}

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

		// --- Background version check ---
		// Reset the channel for each invocation (important for tests).
		updateCheckCh = make(chan *autoupdate.CheckResult, 1)

		if shouldSkipAutoUpdate(cmd) {
			// Send a nil result so PostRunE doesn't hang on a 100ms timeout.
			updateCheckCh <- nil
			return nil
		}

		// Parse check interval from config (default 24h).
		intervalStr := viper.GetString("auto_update.check_interval")
		if intervalStr == "" {
			intervalStr = "24h"
		}
		checkInterval, err := time.ParseDuration(intervalStr)
		if err != nil {
			checkInterval = 24 * time.Hour
		}

		cachePath, _ := autoupdate.DefaultCachePath()

		go func() {
			result := autoupdate.CheckForUpdate(Version, cachePath, checkInterval, nil)
			updateCheckCh <- result
		}()

		return nil
	},

	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		if updateCheckCh == nil {
			return nil
		}

		// Wait up to 100ms for the background check.
		var result *autoupdate.CheckResult
		select {
		case result = <-updateCheckCh:
		case <-time.After(100 * time.Millisecond):
			return nil
		}

		if result == nil || !result.Available {
			return nil
		}

		// --- Notification ---
		mode := viper.GetString("auto_update.mode")

		currentNorm := autoupdate.NormalizeVersion(result.CurrentVersion)
		latestNorm := result.LatestVersion

		// Build a changelog teaser using SummarizeChanges from changelog.go.
		teaser := buildUpdateTeaser(currentNorm, latestNorm)

		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "  A new version of Memory CLI is available: %s → %s\n", currentNorm, latestNorm)
		if teaser != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", teaser)
		}

		if mode == "auto" {
			// Attempt silent auto-install.
			fmt.Fprintf(os.Stderr, "  Auto-updating...\n")
			release := &autoupdate.Release{
				TagName: result.LatestVersion,
				Body:    result.ReleaseBody,
				HTMLURL: result.ReleaseURL,
			}
			if _, err := autoupdate.DownloadAndInstall(release); err != nil {
				fmt.Fprintf(os.Stderr, "  Auto-update failed: %v\n", err)
				fmt.Fprintf(os.Stderr, "  Run 'memory upgrade' to update manually.\n")
			} else {
				fmt.Fprintf(os.Stderr, "  Updated to %s\n", latestNorm)
			}
		} else {
			fmt.Fprintf(os.Stderr, "  Run 'memory upgrade' to update.\n")
		}
		fmt.Fprintln(os.Stderr)

		return nil
	},
}

// buildUpdateTeaser builds a short human-readable summary of what changed
// between currentVersion and latestVersion.  It uses SummarizeChanges from
// changelog.go (already in this package).  On any error it returns "".
func buildUpdateTeaser(currentVersion, latestVersion string) string {
	releases, err := fetchReleasesBetween(currentVersion, latestVersion)
	if err != nil || len(releases) == 0 {
		return ""
	}
	return SummarizeChanges(releases)
}

// NewRootCommand creates and returns the root command
// This function is used for testing and allows dependency injection
func NewRootCommand() *cobra.Command {
	return rootCmd
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	// Silence cobra's own error/usage printing so main() can intercept auth
	// errors and print a friendly re-authentication prompt, and so the usage
	// block is not printed on every runtime error (only on flag/arg misuse).
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	// Set version so that --version / -v work as aliases for `memory version`.
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("Memory CLI version {{.Version}}\n")
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global persistent flags (available to all subcommands)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.memory/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "Memory server URL")
	rootCmd.PersistentFlags().StringVar(&output, "output", "table", "output format (table, json, yaml, csv)")
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "shorthand for --output json")
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
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv() // read in environment variables that match
	_ = viper.BindEnv("auto_update.enabled", "MEMORY_AUTO_UPDATE_ENABLED")
	_ = viper.BindEnv("auto_update.mode", "MEMORY_AUTO_UPDATE_MODE")
	_ = viper.BindEnv("auto_update.check_interval", "MEMORY_AUTO_UPDATE_CHECK_INTERVAL")

	// Set defaults for new config fields
	viper.SetDefault("cache.ttl", "5m")
	viper.SetDefault("cache.enabled", true)
	viper.SetDefault("ui.compact", false)
	viper.SetDefault("ui.color", "auto")
	viper.SetDefault("ui.pager", true)
	viper.SetDefault("query.default_limit", 50)
	viper.SetDefault("query.default_sort", "updated_at:desc")
	viper.SetDefault("completion.timeout", "2s")
	viper.SetDefault("auto_update.enabled", true)
	viper.SetDefault("auto_update.mode", "notify")
	viper.SetDefault("auto_update.check_interval", "24h")

	// If a config file is found, read it in
	if err := viper.ReadInConfig(); err == nil {
		if debug {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
	}
}
