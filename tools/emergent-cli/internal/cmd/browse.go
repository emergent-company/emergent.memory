package cmd

import (
	"fmt"
	"net/url"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/tui"
	"github.com/spf13/cobra"
)

var browseFlags struct {
	tempoURL string
}

var browseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Interactive TUI for browsing projects and documents",
	Long: `Launch an interactive terminal UI (TUI) for browsing projects, documents, and extractions.

The TUI provides:
- Tab-based navigation (Projects, Documents, Worker Stats, Template Packs, Query, Extractions, Traces)
- Natural language query (Ctrl+Q) to ask questions about your project
- Vim-style keybindings (j/k for up/down, Enter to select)
- Search functionality (press / to search)
- Help panel (press ? to toggle)

Minimum terminal size: 80x24

The Traces tab connects to the Grafana Tempo instance that runs alongside the configured
server. The Tempo URL is derived automatically from the server URL (same host, port 3200).
Override with --tempo-url or EMERGENT_TEMPO_URL if Tempo runs elsewhere.`,
	RunE: runBrowse,
}

// deriveTempoURL builds a Tempo base URL from the configured server URL.
// It keeps the same hostname but always uses port 3200 over HTTP,
// since Tempo is an internal service co-located with the server.
func deriveTempoURL(serverURL string) string {
	if v := os.Getenv("EMERGENT_TEMPO_URL"); v != "" {
		return v
	}
	u, err := url.Parse(serverURL)
	if err != nil || u.Hostname() == "" {
		return "http://localhost:3200"
	}
	return "http://" + u.Hostname() + ":3200"
}

func runBrowse(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	tempoURL := browseFlags.tempoURL
	if tempoURL == "" {
		tempoURL = deriveTempoURL(c.BaseURL())
	}

	// Create TUI model
	model := tui.New(c, tempoURL)

	// Run TUI
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(browseCmd)
	browseCmd.Flags().StringVar(&browseFlags.tempoURL, "tempo-url", "", "Override Tempo URL (auto-derived from server URL by default)")
}
