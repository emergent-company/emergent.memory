package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/installer"
	"github.com/spf13/cobra"
)

var uninstallFlags struct {
	dir      string
	keepData bool
	force    bool
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove Emergent installation",
	Long: `Remove Emergent standalone server installation.

This command will:
  - Stop and remove Docker containers
  - Remove Docker volumes (unless --keep-data is specified)
  - Remove installation directory

Example:
  emergent uninstall
  emergent uninstall --keep-data
  emergent uninstall --force`,
	RunE: runUninstall,
}

func init() {
	homeDir, _ := os.UserHomeDir()
	defaultDir := filepath.Join(homeDir, ".emergent")

	uninstallCmd.Flags().StringVar(&uninstallFlags.dir, "dir", defaultDir, "Installation directory")
	uninstallCmd.Flags().BoolVar(&uninstallFlags.keepData, "keep-data", false, "Keep Docker volumes (preserve data)")
	uninstallCmd.Flags().BoolVar(&uninstallFlags.force, "force", false, "Skip confirmation prompt")

	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	cfg := installer.Config{
		InstallDir: uninstallFlags.dir,
	}

	inst := installer.New(cfg)

	if !inst.IsInstalled() {
		fmt.Printf("No installation found at %s\n", uninstallFlags.dir)
		return nil
	}

	if !uninstallFlags.force {
		fmt.Printf("This will remove Emergent from %s\n", uninstallFlags.dir)
		if !uninstallFlags.keepData {
			fmt.Println("WARNING: All data (database, files) will be permanently deleted!")
		}
		fmt.Print("Are you sure? [y/N]: ")

		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if confirm != "y" && confirm != "Y" {
			fmt.Println("Uninstall canceled.")
			return nil
		}
	}

	return inst.Uninstall(uninstallFlags.keepData)
}
