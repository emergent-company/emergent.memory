package fixcmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{Use: "fix", Short: "Repair graph state"}
	cmd.AddCommand(newStaleCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newRewireCmd(flagProjectID, flagBranch, flagFormat))
	return cmd
}
