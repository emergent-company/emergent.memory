package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/skillsfs"
	"github.com/spf13/cobra"
)

var installMemorySkillsCmd = &cobra.Command{
	Use:   "install-memory-skills",
	Short: "Install Memory skills to .agents/skills/",
	Long: `Install the built-in Memory skills from the embedded catalog into
.agents/skills/ in the current directory (or the directory specified by --dir).

Only skills with the "memory-" prefix are installed. This is the set of skills
that teach AI agents how to use the Memory CLI and platform.

By default the command skips skills that already exist. Use --force to
overwrite existing skill directories.`,
	RunE: runInstallMemorySkills,
}

var (
	installMemorySkillsForce bool
	installMemorySkillsDir   string
)

func init() {
	installMemorySkillsCmd.Flags().BoolVar(&installMemorySkillsForce, "force", false, "overwrite existing skill directories")
	installMemorySkillsCmd.Flags().StringVar(&installMemorySkillsDir, "dir", "", "target directory (default: .agents/skills relative to cwd)")
	rootCmd.AddCommand(installMemorySkillsCmd)
}

func runInstallMemorySkills(cmd *cobra.Command, args []string) error {
	catalog := skillsfs.Catalog()

	// Resolve target directory.
	targetDir := installMemorySkillsDir
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}
		targetDir = filepath.Join(cwd, ".agents", "skills")
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating target directory %s: %w", targetDir, err)
	}

	// Enumerate top-level entries in the catalog; install only memory-* ones.
	entries, err := fs.ReadDir(catalog, ".")
	if err != nil {
		return fmt.Errorf("reading embedded skills catalog: %w", err)
	}

	installed := 0
	skipped := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "memory-") {
			continue
		}

		destDir := filepath.Join(targetDir, name)

		// Check if already exists.
		if _, err := os.Stat(destDir); err == nil {
			if !installMemorySkillsForce {
				fmt.Fprintf(cmd.OutOrStdout(), "  skipping %s (already exists; use --force to overwrite)\n", name)
				skipped++
				continue
			}
			// Remove existing dir before copying fresh.
			if err := os.RemoveAll(destDir); err != nil {
				return fmt.Errorf("removing existing %s: %w", destDir, err)
			}
		}

		sub, err := fs.Sub(catalog, name)
		if err != nil {
			return fmt.Errorf("accessing skill %s: %w", name, err)
		}

		if err := copyFSTree(sub, destDir); err != nil {
			return fmt.Errorf("installing skill %s: %w", name, err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  installed %s\n", name)
		installed++
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n%d skill(s) installed", installed)
	if skipped > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), ", %d skipped", skipped)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
