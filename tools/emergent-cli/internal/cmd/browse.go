package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/tui"
	"github.com/spf13/cobra"
)

var browseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Interactive TUI for browsing projects and documents",
	Long: `Launch an interactive terminal UI (TUI) for browsing projects, documents, and extractions.

The TUI provides:
- Tab-based navigation (Projects, Documents, Extractions)
- Vim-style keybindings (j/k for up/down, Enter to select)
- Search functionality (press / to search)
- Help panel (press ? to toggle)

Minimum terminal size: 80x24`,
	RunE: runBrowse,
}

func runBrowse(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Create TUI model
	model := tui.New(c)

	// Run TUI
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(browseCmd)
}
