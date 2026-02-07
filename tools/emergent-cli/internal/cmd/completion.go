package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for Emergent CLI.

To load completions:

Bash:
  $ source <(emergent completion bash)
  # To load completions for each session, add to ~/.bashrc:
  echo 'source <(emergent completion bash)' >> ~/.bashrc

Zsh:
  $ source <(emergent completion zsh)
  # To load completions for each session, add to ~/.zshrc:
  echo 'source <(emergent completion zsh)' >> ~/.zshrc

  # If shell completion is not already enabled in your zsh config:
  echo 'autoload -Uz compinit && compinit' >> ~/.zshrc

Fish:
  $ emergent completion fish | source
  # To load completions for each session:
  emergent completion fish > ~/.config/fish/completions/emergent.fish

PowerShell:
  PS> emergent completion powershell | Out-String | Invoke-Expression
  # To load completions for each session, add to your profile:
  Add-Content $PROFILE 'emergent completion powershell | Out-String | Invoke-Expression'
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			return cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
