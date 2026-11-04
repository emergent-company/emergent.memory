package cmd

import (
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage a self-hosted Memory server",
	Long:  "Commands for installing, running, and maintaining a self-hosted Memory server.",
}

func init() {
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(upgradeCmd)

	serverCmd.AddCommand(installCmd)
	serverCmd.AddCommand(serverUpgradeCmd)
	serverCmd.AddCommand(uninstallCmd)
	serverCmd.AddCommand(ctlCmd)
	serverCmd.AddCommand(doctorCmd)
}
