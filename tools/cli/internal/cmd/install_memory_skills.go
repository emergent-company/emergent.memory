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
overwrite existing skill directories.

After installing, any "memory-" prefixed skill directories in the target that
are no longer present in the catalog are considered stale. Use --prune to
remove them automatically, or run interactively to be prompted for each one.`,
	RunE: runInstallMemorySkills,
}

var (
	installMemorySkillsForce bool
	installMemorySkillsDir   string
	installMemorySkillsPrune bool
)

func init() {
	installMemorySkillsCmd.Flags().BoolVar(&installMemorySkillsForce, "force", false, "overwrite existing skill directories")
	installMemorySkillsCmd.Flags().StringVar(&installMemorySkillsDir, "dir", "", "target directory (default: .agents/skills relative to cwd)")
	installMemorySkillsCmd.Flags().BoolVar(&installMemorySkillsPrune, "prune", false, "remove stale memory-* skill directories not present in the catalog")
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

	// Build a set of catalog skill names for stale-detection.
	catalogNames := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "memory-") {
			catalogNames[entry.Name()] = struct{}{}
		}
	}

	// Detect stale memory-* directories in targetDir not present in the catalog.
	pruned := 0
	existing, err := os.ReadDir(targetDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading target directory %s: %w", targetDir, err)
	}
	for _, e := range existing {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "memory-") {
			continue
		}
		if _, inCatalog := catalogNames[name]; inCatalog {
			continue
		}
		// Stale skill found.
		remove := false
		if installMemorySkillsPrune {
			remove = true
		} else if isInteractiveTerminal() {
			ok, err := promptYesNo(fmt.Sprintf("  remove stale skill %s? [y/N] ", name))
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
			remove = ok
		}
		if remove {
			if err := os.RemoveAll(filepath.Join(targetDir, name)); err != nil {
				return fmt.Errorf("removing stale skill %s: %w", name, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  pruned %s\n", name)
			pruned++
		}
	}

	if !installMemorySkillsPrune && !isInteractiveTerminal() {
		// Count stale skills for the hint message.
		stale := 0
		for _, e := range existing {
			if !e.IsDir() {
				continue
			}
			if !strings.HasPrefix(e.Name(), "memory-") {
				continue
			}
			if _, inCatalog := catalogNames[e.Name()]; !inCatalog {
				stale++
			}
		}
		if stale > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "\n%d stale memory-* skill(s) detected; run with --prune to remove them\n", stale)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n%d skill(s) installed", installed)
	if skipped > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), ", %d skipped", skipped)
	}
	if pruned > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), ", %d pruned", pruned)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
