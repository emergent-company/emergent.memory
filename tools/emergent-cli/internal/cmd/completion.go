package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for Emergent CLI.

The completion script provides:
- Command and subcommand completion
- Flag name completion
- Flag value completion for enum flags (e.g., --output)
- Dynamic resource completion (project names, document IDs)

To load completions:

Bash:
  $ source <(emergent-cli completion bash)
  
  # To load completions for each session, execute once:
  # Linux:
  $ emergent-cli completion bash > /etc/bash_completion.d/emergent-cli
  # macOS:
  $ emergent-cli completion bash > $(brew --prefix)/etc/bash_completion.d/emergent-cli

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ emergent-cli completion zsh > "${fpath[1]}/_emergent-cli"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ emergent-cli completion fish | source

  # To load completions for each session, execute once:
  $ emergent-cli completion fish > ~/.config/fish/completions/emergent-cli.fish

PowerShell:
  PS> emergent-cli completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> emergent-cli completion powershell > emergent-cli.ps1
  # and source this file from your PowerShell profile.

Notes:
- Dynamic completions (project names, document IDs) are cached locally for 5 minutes
- Cache location: ~/.emergent/cache/
- Completion timeout: 2 seconds (configurable via ~/.emergent/config.yaml)
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
