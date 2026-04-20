package branchcmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string) *cobra.Command {
	cmd := &cobra.Command{Use: "branch", Short: "Graph branch operations"}
	cmd.AddCommand(newVerifyCmd(flagProjectID))
	return cmd
}
