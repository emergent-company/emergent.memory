package skillscmd

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

//go:embed bundled/*
var bundledSkills embed.FS

// skillDirs returns candidate install directories in priority order.
// Uses the first one that exists (or can be created).
func skillDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		// project-local (preferred when inside a repo)
		".opencode/skills",
		// user-global opencode
		filepath.Join(home, ".config", "opencode", "skills"),
		// user-global agents
		filepath.Join(home, ".agents", "skills"),
	}
}

func newInstallCmd() *cobra.Command {
	var destDir string
	var list bool
	var force bool

	cmd := &cobra.Command{
		Use:   "install [skill-name...]",
		Short: "Install bundled agent skills into the project",
		Long: `Install one or more bundled skills into the .opencode/skills directory.

Skills are SKILL.md files that teach the AI agent how to use codebase commands.
They are picked up automatically by opencode when present in .opencode/skills/.

Examples:
  codebase skills install              # install all bundled skills
  codebase skills install codebase     # install only the codebase skill
  codebase skills install --list       # list available bundled skills
  codebase skills install --dir /path  # install to a custom directory
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// List mode
			if list {
				entries, err := fs.ReadDir(bundledSkills, "bundled")
				if err != nil {
					return fmt.Errorf("reading bundled skills: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Available bundled skills:")
				for _, e := range entries {
					if e.IsDir() {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", e.Name())
					}
				}
				return nil
			}

			// Resolve destination
			dest := destDir
			if dest == "" {
				dest = resolveInstallDir()
			}

			// Collect skills to install
			entries, err := fs.ReadDir(bundledSkills, "bundled")
			if err != nil {
				return fmt.Errorf("reading bundled skills: %w", err)
			}

			toInstall := map[string]bool{}
			for _, name := range args {
				toInstall[name] = true
			}

			installed := 0
			skipped := 0
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				if len(toInstall) > 0 && !toInstall[name] {
					continue
				}

				skillSrc := filepath.Join("bundled", name, "SKILL.md")
				data, err := bundledSkills.ReadFile(skillSrc)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "  warn: %s has no SKILL.md, skipping\n", name)
					continue
				}

				skillDir := filepath.Join(dest, name)
				if err := os.MkdirAll(skillDir, 0o755); err != nil {
					return fmt.Errorf("creating %s: %w", skillDir, err)
				}

				outPath := filepath.Join(skillDir, "SKILL.md")
				if _, err := os.Stat(outPath); err == nil && !force {
					fmt.Fprintf(cmd.OutOrStdout(), "  skip  %s (already exists, use --force to overwrite)\n", name)
					skipped++
					continue
				}

				if err := os.WriteFile(outPath, data, 0o644); err != nil {
					return fmt.Errorf("writing %s: %w", outPath, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  ✓     %s → %s\n", name, outPath)
				installed++
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\n%d installed, %d skipped\n", installed, skipped)
			return nil
		},
	}

	cmd.Flags().StringVar(&destDir, "dir", "", "Destination skills directory (default: auto-detect .opencode/skills)")
	cmd.Flags().BoolVar(&list, "list", false, "List available bundled skills without installing")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing skill files")
	return cmd
}

// resolveInstallDir picks the best install directory.
// Prefers .opencode/skills in cwd (project-local), falls back to user-global.
func resolveInstallDir() string {
	// Walk up to find .opencode/skills
	dir, _ := os.Getwd()
	for {
		candidate := filepath.Join(dir, ".opencode", "skills")
		if _, err := os.Stat(filepath.Join(dir, ".opencode")); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fall back to user home
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "opencode", "skills")
}
