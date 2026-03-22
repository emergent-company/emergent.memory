package cmd

import (
	"bytes"
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
	Short: "Install Memory skills to .agents/skills/ and known agent config dirs",
	Long: `Install the built-in Memory skills from the embedded catalog into
.agents/skills/ in the current directory (or the directory specified by --dir),
and into any other known agent skill directories that already exist on disk
(e.g. ~/.opencode/skills, ~/.claude/skills, ~/.gemini/skills).

Only skills with the "memory-" prefix are installed. This is the set of skills
that teach AI agents how to use the Memory CLI and platform.

By default the command skips skills that are already up to date. Skills whose
content has changed since they were last installed are reported as outdated —
use --force to overwrite them with the latest version.

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

	// Resolve primary target directory (.agents/skills or --dir).
	primaryDir := installMemorySkillsDir
	if primaryDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}
		primaryDir = filepath.Join(cwd, ".agents", "skills")
	}

	// Resolve to absolute path for display.
	if abs, err := filepath.Abs(primaryDir); err == nil {
		primaryDir = abs
	}

	// Build list of target dirs: primary always included (created if needed),
	// plus any other knownSkillDirs() that already exist on disk.
	targetDirs := []string{primaryDir}
	for _, d := range knownSkillDirs() {
		if abs, err := filepath.Abs(d); err == nil {
			d = abs
		}
		if d == primaryDir {
			continue
		}
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			targetDirs = append(targetDirs, d)
		}
	}

	// Enumerate catalog entries once.
	entries, err := fs.ReadDir(catalog, ".")
	if err != nil {
		return fmt.Errorf("reading embedded skills catalog: %w", err)
	}

	// Build catalog name set for stale-detection.
	catalogNames := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "memory-") {
			catalogNames[entry.Name()] = struct{}{}
		}
	}

	// Process each target directory.
	for _, targetDir := range targetDirs {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("creating target directory %s: %w", targetDir, err)
		}

		installed, skipped, outdated, pruned, err := installSkillsToDir(cmd, catalog, entries, catalogNames, targetDir)
		if err != nil {
			return err
		}

		// Summary line with absolute path.
		fmt.Fprintf(cmd.OutOrStdout(), "\n%d skill(s) installed", installed)
		if skipped > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), ", %d up to date", skipped)
		}
		if outdated > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), ", %d outdated (run with --force to update)", outdated)
		}
		if pruned > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), ", %d pruned", pruned)
		}
		fmt.Fprintf(cmd.OutOrStdout(), " → %s\n", targetDir)
	}

	return nil
}

// installSkillsToDir installs memory-* skills from catalog into targetDir and
// returns counts of (installed, skipped/up-to-date, outdated, pruned).
func installSkillsToDir(cmd *cobra.Command, catalog fs.FS, entries []fs.DirEntry, catalogNames map[string]struct{}, targetDir string) (installed, skipped, outdated, pruned int, err error) {
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "memory-") {
			continue
		}

		destDir := filepath.Join(targetDir, name)

		sub, subErr := fs.Sub(catalog, name)
		if subErr != nil {
			err = fmt.Errorf("accessing skill %s: %w", name, subErr)
			return
		}

		// Check if already exists.
		if _, statErr := os.Stat(destDir); statErr == nil {
			if !installMemorySkillsForce {
				changed, diffErr := skillDirChanged(sub, destDir)
				if diffErr != nil {
					changed = true
				}
				if changed {
					fmt.Fprintf(cmd.OutOrStdout(), "  outdated %s (use --force to update)\n", name)
					outdated++
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "  up to date %s\n", name)
					skipped++
				}
				continue
			}
			// Remove existing dir before copying fresh.
			if rmErr := os.RemoveAll(destDir); rmErr != nil {
				err = fmt.Errorf("removing existing %s: %w", destDir, rmErr)
				return
			}
		}

		if cpErr := copyFSTree(sub, destDir); cpErr != nil {
			err = fmt.Errorf("installing skill %s: %w", name, cpErr)
			return
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  installed %s\n", name)
		installed++
	}

	// Detect and handle stale memory-* directories.
	existing, rdErr := os.ReadDir(targetDir)
	if rdErr != nil && !os.IsNotExist(rdErr) {
		err = fmt.Errorf("reading target directory %s: %w", targetDir, rdErr)
		return
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
			ok, promptErr := promptYesNo(fmt.Sprintf("  remove stale skill %s? [y/N] ", name))
			if promptErr != nil {
				err = fmt.Errorf("reading input: %w", promptErr)
				return
			}
			remove = ok
		}
		if remove {
			if rmErr := os.RemoveAll(filepath.Join(targetDir, name)); rmErr != nil {
				err = fmt.Errorf("removing stale skill %s: %w", name, rmErr)
				return
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  pruned %s\n", name)
			pruned++
		}
	}

	// Print stale hint for non-interactive, non-prune runs.
	if !installMemorySkillsPrune && !isInteractiveTerminal() {
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

	return
}

// skillDirChanged reports whether any file in the catalog FS differs from the
// corresponding file on disk under destDir. Returns true if any file is missing
// or has different content, false if everything matches exactly.
func skillDirChanged(catalogSub fs.FS, destDir string) (bool, error) {
	changed := false
	err := fs.WalkDir(catalogSub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		catalogData, err := fs.ReadFile(catalogSub, path)
		if err != nil {
			return err
		}
		diskPath := filepath.Join(destDir, filepath.FromSlash(path))
		diskData, err := os.ReadFile(diskPath)
		if err != nil {
			// File missing on disk — definitely changed.
			changed = true
			return fs.SkipAll
		}
		if !bytes.Equal(catalogData, diskData) {
			changed = true
			return fs.SkipAll
		}
		return nil
	})
	return changed, err
}
