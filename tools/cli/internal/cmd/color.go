package cmd

import (
	"os"

	"github.com/fatih/color"
	"golang.org/x/term"
)

func init() {
	// Disable color when NO_COLOR is set (https://no-color.org) or stdout is not a TTY.
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor || !term.IsTerminal(int(os.Stdout.Fd())) {
		color.NoColor = true
	}
}

var (
	cBold   = color.New(color.Bold).SprintFunc()
	cDim    = color.New(color.Faint).SprintFunc()
	cYellow = color.New(color.FgYellow, color.Bold).SprintFunc()
	cCyan   = color.New(color.FgCyan).SprintFunc()
	cHeader = color.New(color.Bold).SprintFunc()
)

// colorRole returns a colored role string: yellow for admin roles, cyan for others.
func colorRole(role string) string {
	if role == "project_admin" || role == "org_admin" || role == "admin" {
		return cYellow(role)
	}
	return cCyan(role)
}
