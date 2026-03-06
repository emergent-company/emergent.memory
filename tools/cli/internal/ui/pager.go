package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Pager manages paging of long output.
type Pager struct {
	enabled bool
	command string
	args    []string
}

// NewPager creates a new pager based on configuration.
func NewPager(enabled bool) *Pager {
	if !enabled || IsPiped() {
		return &Pager{enabled: false}
	}

	// Try to get pager from environment
	pagerCmd := os.Getenv("PAGER")
	if pagerCmd == "" {
		// Try common pagers
		for _, cmd := range []string{"less", "more", "cat"} {
			if _, err := exec.LookPath(cmd); err == nil {
				pagerCmd = cmd
				break
			}
		}
	}

	if pagerCmd == "" {
		return &Pager{enabled: false}
	}

	// Parse command and args
	parts := strings.Fields(pagerCmd)
	command := parts[0]
	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}

	// Add default args for less
	if command == "less" && len(args) == 0 {
		args = []string{"-R", "-F", "-X"} // -R: raw control chars, -F: quit if one screen, -X: no clear
	}

	return &Pager{
		enabled: true,
		command: command,
		args:    args,
	}
}

// Page displays content through the pager if enabled, otherwise prints directly.
func (p *Pager) Page(content string) error {
	if !p.enabled {
		fmt.Print(content)
		return nil
	}

	// Check if content needs paging (count lines)
	lines := strings.Count(content, "\n")
	termHeight := getTerminalHeight()

	// If content fits on screen, don't use pager
	if lines < termHeight-2 {
		fmt.Print(content)
		return nil
	}

	// Use pager
	cmd := exec.Command(p.command, p.args...)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// getTerminalHeight returns the terminal height or a default.
func getTerminalHeight() int {
	_, height, err := getTermSize()
	if err != nil || height <= 0 {
		return 24 // default
	}
	return height
}

// getTermSize returns terminal dimensions.
func getTermSize() (width, height int, err error) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}

	_, err = fmt.Sscanf(string(out), "%d %d", &height, &width)
	return width, height, err
}
