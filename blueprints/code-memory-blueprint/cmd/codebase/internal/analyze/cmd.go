package analyzecmd

import "github.com/spf13/cobra"

func NewCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze codebase structure",
	}
	cmd.AddCommand(newTreeCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newUMLCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newScenariosCmd(flagProjectID, flagBranch, flagFormat))
	cmd.AddCommand(newContextsCmd(flagProjectID, flagBranch, flagFormat))
	return cmd
}
