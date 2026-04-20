package seedcmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{Use: "seed", Short: "Write seed objects to the graph"}
	cmd.AddCommand(newEntitiesCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newExposesCmd(flagProjectID, flagBranch, flagFormat))
	return cmd
}
