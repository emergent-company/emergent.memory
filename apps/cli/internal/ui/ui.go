// Package ui provides terminal UI utilities for rich output formatting.
package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Colors returns true if colored output should be enabled.
// Respects NO_COLOR env var and --no-color flag.
func Colors(noColorFlag bool) bool {
	if noColorFlag {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return true
}

// Style returns a lipgloss style with color support based on configuration.
func Style(noColor bool) lipgloss.Style {
	if noColor {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("default"))
}
