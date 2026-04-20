// Package skillscmd provides the `codebase skills` command group.
package skillscmd

import "github.com/spf13/cobra"

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage codebase agent skills",
	}
	cmd.AddCommand(newInstallCmd())
	return cmd
}
