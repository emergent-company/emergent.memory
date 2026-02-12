package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/installer"
	"github.com/spf13/cobra"
)

var installFlags struct {
	dir          string
	port         int
	googleAPIKey string
	skipStart    bool
	force        bool
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Emergent standalone server",
	Long: `Install Emergent standalone server with all required components.

This command will:
  - Check Docker and Docker Compose are installed
  - Create installation directory (~/.emergent by default)
  - Generate secure configuration (API keys, passwords)
  - Write Docker Compose configuration
  - Pull and start Docker containers
  - Configure the CLI to connect to the local server

Example:
  emergent install
  emergent install --port 8080 --google-api-key YOUR_KEY
  emergent install --dir /opt/emergent --skip-start`,
	RunE: runInstall,
}

func init() {
	homeDir, _ := os.UserHomeDir()
	defaultDir := filepath.Join(homeDir, ".emergent")

	installCmd.Flags().StringVar(&installFlags.dir, "dir", defaultDir, "Installation directory")
	installCmd.Flags().IntVar(&installFlags.port, "port", 3002, "Server port")
	installCmd.Flags().StringVar(&installFlags.googleAPIKey, "google-api-key", "", "Google API key for embeddings")
	installCmd.Flags().BoolVar(&installFlags.skipStart, "skip-start", false, "Generate config but don't start services")
	installCmd.Flags().BoolVar(&installFlags.force, "force", false, "Overwrite existing installation")

	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	cfg := installer.Config{
		InstallDir:   installFlags.dir,
		ServerPort:   installFlags.port,
		GoogleAPIKey: installFlags.googleAPIKey,
		SkipStart:    installFlags.skipStart,
		Force:        installFlags.force,
	}

	inst := installer.New(cfg)

	if inst.IsInstalled() && !installFlags.force {
		fmt.Println("Existing installation detected. Use --force to overwrite or run 'emergent upgrade'.")

		var confirm string
		fmt.Print("Run upgrade instead? [y/N]: ")
		_, _ = fmt.Scanln(&confirm)
		if confirm == "y" || confirm == "Y" {
			cfg.ServerPort = inst.GetServerPort()
			// Fetch the latest release version for the upgrade
			release, err := getLatestRelease()
			if err != nil {
				fmt.Printf("Warning: could not determine latest version: %v\n", err)
				return inst.Upgrade("")
			}
			return inst.Upgrade(release.TagName)
		}
		return nil
	}

	if err := inst.Install(); err != nil {
		return err
	}

	// Record the installed version so future upgrades know the baseline.
	// Version is the build-time variable set via ldflags.
	if Version != "dev" && Version != "" {
		inst.SaveInstalledVersion(Version)
	}

	return nil
}
