package checkcmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Read-only graph quality analysis",
	}
	cmd.AddCommand(newAPICmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newLogicCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newCoverageCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newComplexityCmd(flagProjectID, flagBranch, flagFormat))
	return cmd
}
