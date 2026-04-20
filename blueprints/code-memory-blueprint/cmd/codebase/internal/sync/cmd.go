package synccmd

import (
	"github.com/spf13/cobra"
)

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Populate the graph from codebase source files",
	}
	cmd.AddCommand(newRoutesCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newMiddlewareCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newFilesCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newViewsCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newComponentsCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newActionsCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newScenariosCmd(flagProjectID, flagBranch, flagFormat))
	return cmd
}
