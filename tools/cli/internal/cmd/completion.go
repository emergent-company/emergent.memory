package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for Memory CLI.

The completion script provides:
- Command and subcommand completion
- Flag name completion
- Flag value completion for enum flags (e.g., --output)
- Dynamic resource completion (project names, document IDs)

To load completions:

Bash:
  $ source <(memory completion bash)
  
  # To load completions for each session, execute once:
  # Linux:
  $ memory completion bash > /etc/bash_completion.d/memory
  # macOS:
  $ memory completion bash > $(brew --prefix)/etc/bash_completion.d/memory

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ memory completion zsh > "${fpath[1]}/_memory"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ memory completion fish | source

  # To load completions for each session, execute once:
  $ memory completion fish > ~/.config/fish/completions/memory.fish

PowerShell:
  PS> memory completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> memory completion powershell > memory.ps1
  # and source this file from your PowerShell profile.

Notes:
- Dynamic completions (project names, document IDs) are cached locally for 5 minutes
- Cache location: ~/.memory/cache/
- Completion timeout: 2 seconds (configurable via ~/.memory/config.yaml)
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
