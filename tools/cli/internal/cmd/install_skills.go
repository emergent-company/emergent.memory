package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/skillsfs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// SkillFrontmatter holds the parsed YAML frontmatter from a SKILL.md file.
type SkillFrontmatter struct {
	Name         string             `yaml:"name"`
	Description  string             `yaml:"description"`
	Version      string             `yaml:"version"` // flat form: version: "1.0"
	License      string             `yaml:"license"`
	AllowedTools []string           `yaml:"allowed-tools"`
	Metadata     SkillMetadataBlock `yaml:"metadata"`
}

// SkillMetadataBlock holds the nested metadata sub-block (metadata.version, metadata.author, …).
type SkillMetadataBlock struct {
	Version string `yaml:"version"`
	Author  string `yaml:"author"`
}

// EffectiveVersion returns the version string from whichever location it was set:
// the top-level version field takes precedence; falls back to metadata.version.
func (f *SkillFrontmatter) EffectiveVersion() string {
	if f.Version != "" {
		return f.Version
	}
	return f.Metadata.Version
}

// SkillMeta is the display/output representation of an installed skill.
type SkillMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	License     string `json:"license"`
	Path        string `json:"path"`
}

var installSkillsDir string

var installSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage Agent Skills in this project",
	Long:  "Install, list, remove, and validate Agent Skills (agentskills.io) for use with AI agents.",
}

var installSkillInstallCmd = &cobra.Command{
	Use:   "install [path]",
	Short: "Install a skill from a local directory",
	Long: `Install an Agent Skill from a local directory path into the project's skills directory.

If no path is given, installs from the current directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInstallSkill,
}

var installSkillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed skills",
	Long:  "List all Agent Skills installed in the project's skills directory.",
	Args:  cobra.NoArgs,
	RunE:  runListSkills,
}

var installSkillRemoveCmd = &cobra.Command{
	Use:   "remove <skill-name>",
	Short: "Remove an installed skill",
	Long:  "Remove an installed Agent Skill by name.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoveSkill,
}

var installSkillValidateCmd = &cobra.Command{
	Use:   "validate <path>",
	Short: "Validate a skill's SKILL.md frontmatter",
	Long:  "Validate the SKILL.md frontmatter of a skill directory against the agentskills.io specification.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runValidateSkill,
}

var (
	installSkillForce        bool
	installSkillSkipValidate bool
	installSkillOutput       string
	removeSkillForce         bool
)

// parseSkillFrontmatter reads a SKILL.md file and parses its YAML frontmatter.
func parseSkillFrontmatter(skillDir string) (*SkillFrontmatter, error) {
	skillPath := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no SKILL.md found in %s", skillDir)
		}
		return nil, fmt.Errorf("reading SKILL.md: %w", err)
	}

	content := string(data)
	// Must start with ---\n (a line containing only ---)
	trimmed := strings.TrimLeft(content, "\r\n")
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return nil, fmt.Errorf("SKILL.md in %s has no YAML frontmatter (expected to start with ---)", skillDir)
	}

	// Find the closing --- delimiter (must be on its own line after the opening)
	// Skip past the opening --- line first
	rest := trimmed[3:] // skip the opening "---"
	closingIdx := strings.Index(rest, "\n---")
	if closingIdx == -1 {
		return nil, fmt.Errorf("SKILL.md in %s has malformed frontmatter (missing closing ---)", skillDir)
	}
	yamlBlock := rest[:closingIdx]

	var fm SkillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return nil, fmt.Errorf("parsing SKILL.md frontmatter in %s: %w", skillDir, err)
	}

	return &fm, nil
}

var skillNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$|^[a-z0-9]$`)

// validateSkillFrontmatter checks frontmatter fields against the agentskills.io spec.
// dirName is the directory basename; pass empty string to skip name/dir match check.
// Returns a slice of error strings (empty = valid).
func validateSkillFrontmatter(fm *SkillFrontmatter, dirName string) []string {
	var errs []string

	if fm.Name == "" {
		errs = append(errs, "name is required")
	} else {
		if len(fm.Name) > 64 {
			errs = append(errs, "name must be 64 characters or fewer")
		}
		if strings.ToLower(fm.Name) != fm.Name {
			errs = append(errs, "name must contain only lowercase letters, numbers, and hyphens")
		}
		if strings.HasPrefix(fm.Name, "-") || strings.HasSuffix(fm.Name, "-") {
			errs = append(errs, "name must not start or end with a hyphen")
		}
		if strings.Contains(fm.Name, "--") {
			errs = append(errs, "name must not contain consecutive hyphens")
		}
		if !skillNameRe.MatchString(fm.Name) {
			errs = append(errs, "name must contain only lowercase letters, numbers, and hyphens")
		}
		if dirName != "" && fm.Name != dirName {
			errs = append(errs, fmt.Sprintf("name %q must match directory name %q", fm.Name, dirName))
		}
	}

	if fm.Description == "" {
		errs = append(errs, "description is required")
	} else if len(fm.Description) > 1024 {
		errs = append(errs, "description must be 1024 characters or fewer")
	}

	return errs
}

// copyDirTree recursively copies src directory to dst, skipping .git/.
func copyDirTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directories
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode())
		}

		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// isInteractiveTerminal returns true if stdin is a real terminal.
func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptYesNo prints a prompt and reads a y/N answer from stdin.
// Returns true if the user typed 'y' or 'Y'.
func promptYesNo(prompt string) (bool, error) {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.TrimSpace(scanner.Text())
	return answer == "y" || answer == "Y", nil
}

// ensureSkillsDir ensures targetDir exists, prompting the user to create it if
// it doesn't. Returns an error if the user declines or creation fails.
func ensureSkillsDir(targetDir string) error {
	if _, err := os.Stat(targetDir); err == nil {
		return nil // already exists
	}
	if isInteractiveTerminal() {
		ok, err := promptYesNo(fmt.Sprintf("Skills directory %q does not exist. Create it? [y/N]: ", targetDir))
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		if !ok {
			return fmt.Errorf("aborted: skills directory %q does not exist", targetDir)
		}
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating skills directory: %w", err)
	}
	fmt.Printf("Created skills directory %q\n", targetDir)
	return nil
}

// installSingleSkill installs one skill from src into targetDir.
func installSingleSkill(src, targetDir string, force, skipValidate bool) error {
	var (
		fm  *SkillFrontmatter
		err error
	)
	if skipValidate {
		fmt.Println("Warning: Skipping SKILL.md validation")
		fm, _ = parseSkillFrontmatter(src)
		if fm == nil || fm.Name == "" {
			return fmt.Errorf("--skip-validate requires a parseable SKILL.md with a name field")
		}
	} else {
		fm, err = parseSkillFrontmatter(src)
		if err != nil {
			return err
		}
		errs := validateSkillFrontmatter(fm, "")
		if len(errs) > 0 {
			return fmt.Errorf("SKILL.md validation failed:\n  - %s", strings.Join(errs, "\n  - "))
		}
	}

	dest := filepath.Join(targetDir, fm.Name)

	if _, err := os.Stat(dest); err == nil {
		if force {
			if err := os.RemoveAll(dest); err != nil {
				return fmt.Errorf("removing existing skill: %w", err)
			}
			if err := copyDirTree(src, dest); err != nil {
				return fmt.Errorf("copying skill: %w", err)
			}
			fmt.Printf("Reinstalled skill '%s'\n", fm.Name)
			return nil
		}

		if isInteractiveTerminal() {
			ok, err := promptYesNo(fmt.Sprintf("Skill '%s' is already installed. Update? [y/N]: ", fm.Name))
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}
			if err := os.RemoveAll(dest); err != nil {
				return fmt.Errorf("removing existing skill: %w", err)
			}
			if err := copyDirTree(src, dest); err != nil {
				return fmt.Errorf("copying skill: %w", err)
			}
			fmt.Printf("Reinstalled skill '%s'\n", fm.Name)
			return nil
		}

		return fmt.Errorf("skill '%s' is already installed; use --force to overwrite", fm.Name)
	}

	if err := copyDirTree(src, dest); err != nil {
		return fmt.Errorf("copying skill: %w", err)
	}
	fmt.Printf("Installed skill '%s'\n", fm.Name)
	return nil
}

// installFromLocalPath installs from src into targetDir.
// If src contains a SKILL.md it is treated as a single skill.
// If src has no SKILL.md but contains subdirectories with SKILL.md files,
// it is treated as a catalog and every valid skill subdir is installed.
func installFromLocalPath(src, targetDir string, force, skipValidate bool) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("path %q does not exist", src)
		}
		return fmt.Errorf("accessing %q: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", src)
	}

	// Determine mode: single skill or catalog.
	skillMDPath := filepath.Join(src, "SKILL.md")
	if _, err := os.Stat(skillMDPath); err == nil {
		// Single skill mode.
		if err := ensureSkillsDir(targetDir); err != nil {
			return err
		}
		return installSingleSkill(src, targetDir, force, skipValidate)
	}

	// Catalog mode: look for subdirectories that each contain a SKILL.md.
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading catalog directory: %w", err)
	}
	var skillDirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(src, e.Name(), "SKILL.md")); err == nil {
			skillDirs = append(skillDirs, filepath.Join(src, e.Name()))
		}
	}
	if len(skillDirs) == 0 {
		return fmt.Errorf("no SKILL.md found in %q and no skill subdirectories detected", src)
	}

	if err := ensureSkillsDir(targetDir); err != nil {
		return err
	}

	var errs []string
	for _, sd := range skillDirs {
		if err := installSingleSkill(sd, targetDir, force, skipValidate); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(sd), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("some skills failed to install:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func runInstallSkill(cmd *cobra.Command, args []string) error {
	targetDir, err := resolveSkillsDir(cmd)
	if err != nil {
		return err
	}

	// No path argument — install the built-in embedded catalog.
	if len(args) == 0 {
		return installFromEmbedded(targetDir, installSkillForce, installSkillSkipValidate)
	}

	src := args[0]

	// Expand ~ if present
	if strings.HasPrefix(src, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		src = filepath.Join(home, src[2:])
	}

	return installFromLocalPath(src, targetDir, installSkillForce, installSkillSkipValidate)
}

// installFromEmbedded installs all valid skills from the built-in catalog into targetDir.
func installFromEmbedded(targetDir string, force, skipValidate bool) error {
	catalog := skillsfs.Catalog()

	entries, err := fs.ReadDir(catalog, ".")
	if err != nil {
		return fmt.Errorf("reading embedded catalog: %w", err)
	}

	var skillDirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Only install skills prefixed with "emergent-" by default.
		if !strings.HasPrefix(e.Name(), "emergent-") {
			continue
		}
		// Check for SKILL.md
		if _, err := fs.Stat(catalog, e.Name()+"/SKILL.md"); err == nil {
			skillDirs = append(skillDirs, e.Name())
		}
	}

	if len(skillDirs) == 0 {
		return fmt.Errorf("embedded catalog contains no skills")
	}

	if err := ensureSkillsDir(targetDir); err != nil {
		return err
	}

	var errs []string
	for _, name := range skillDirs {
		sub, err := fs.Sub(catalog, name)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if err := installSingleSkillFromFS(name, sub, targetDir, force, skipValidate); err != nil {
			// Warn but don't fail — catalog may contain skills in other formats.
			fmt.Printf("Skipped '%s': %v\n", name, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("some skills failed to install:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// installSingleSkillFromFS installs a skill from an fs.FS into targetDir.
// skillName is the directory name; skillFS is rooted at that skill's directory.
func installSingleSkillFromFS(skillName string, skillFS fs.FS, targetDir string, force, skipValidate bool) error {
	// Parse frontmatter from the embedded SKILL.md
	data, err := fs.ReadFile(skillFS, "SKILL.md")
	if err != nil {
		return fmt.Errorf("reading SKILL.md: %w", err)
	}

	fm, err := parseSkillFrontmatterFromBytes(data, skillName)
	if err != nil {
		return err
	}

	if !skipValidate {
		errs := validateSkillFrontmatter(fm, "")
		if len(errs) > 0 {
			return fmt.Errorf("SKILL.md validation failed:\n  - %s", strings.Join(errs, "\n  - "))
		}
	}

	dest := filepath.Join(targetDir, fm.Name)

	if _, err := os.Stat(dest); err == nil {
		if force {
			if err := os.RemoveAll(dest); err != nil {
				return fmt.Errorf("removing existing skill: %w", err)
			}
			if err := copyFSTree(skillFS, dest); err != nil {
				return fmt.Errorf("copying skill: %w", err)
			}
			fmt.Printf("Reinstalled skill '%s'\n", fm.Name)
			return nil
		}
		if isInteractiveTerminal() {
			ok, err := promptYesNo(fmt.Sprintf("Skill '%s' is already installed. Update? [y/N]: ", fm.Name))
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}
			if err := os.RemoveAll(dest); err != nil {
				return fmt.Errorf("removing existing skill: %w", err)
			}
			if err := copyFSTree(skillFS, dest); err != nil {
				return fmt.Errorf("copying skill: %w", err)
			}
			fmt.Printf("Reinstalled skill '%s'\n", fm.Name)
			return nil
		}
		return fmt.Errorf("skill '%s' is already installed; use --force to overwrite", fm.Name)
	}

	if err := copyFSTree(skillFS, dest); err != nil {
		return fmt.Errorf("copying skill: %w", err)
	}
	fmt.Printf("Installed skill '%s'\n", fm.Name)
	return nil
}

// parseSkillFrontmatterFromBytes parses YAML frontmatter from raw SKILL.md bytes.
func parseSkillFrontmatterFromBytes(data []byte, skillDir string) (*SkillFrontmatter, error) {
	content := string(data)
	trimmed := strings.TrimLeft(content, "\r\n")
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return nil, fmt.Errorf("SKILL.md in %s has no YAML frontmatter (expected to start with ---)", skillDir)
	}
	rest := trimmed[3:]
	closingIdx := strings.Index(rest, "\n---")
	if closingIdx == -1 {
		return nil, fmt.Errorf("SKILL.md in %s has malformed frontmatter (missing closing ---)", skillDir)
	}
	yamlBlock := rest[:closingIdx]
	var fm SkillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return nil, fmt.Errorf("parsing SKILL.md frontmatter in %s: %w", skillDir, err)
	}
	return &fm, nil
}

// copyFSTree copies all files from srcFS into dstDir on disk.
func copyFSTree(srcFS fs.FS, dstDir string) error {
	return fs.WalkDir(srcFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(srcFS, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func runListSkills(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	targetDir, err := resolveSkillsDir(cmd)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(out, "No skills installed in %s\n", targetDir)
			return nil
		}
		return fmt.Errorf("reading skills directory: %w", err)
	}

	var skills []SkillMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(targetDir, e.Name())
		fm, err := parseSkillFrontmatter(skillDir)
		if err != nil {
			continue // skip directories without valid SKILL.md
		}
		skills = append(skills, SkillMeta{
			Name:        fm.Name,
			Description: fm.Description,
			Version:     fm.EffectiveVersion(),
			License:     fm.License,
			Path:        skillDir,
		})
	}

	if len(skills) == 0 {
		fmt.Fprintf(out, "No skills installed in %s\n", targetDir)
		return nil
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		data, err := json.MarshalIndent(skills, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	// Table output
	table := tablewriter.NewWriter(out)
	table.Header("Name", "Description", "Version")
	for _, s := range skills {
		desc := s.Description
		if len(desc) > 60 {
			desc = desc[:59] + "…"
		}
		_ = table.Append(s.Name, desc, s.Version)
	}
	return table.Render()
}

func runRemoveSkill(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	targetDir, err := resolveSkillsDir(cmd)
	if err != nil {
		return err
	}

	dest := filepath.Join(targetDir, skillName)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return fmt.Errorf("skill '%s' is not installed", skillName)
	}

	force, _ := cmd.Flags().GetBool("force")
	if !force && isInteractiveTerminal() {
		ok, err := promptYesNo(fmt.Sprintf("Remove skill '%s'? [y/N]: ", skillName))
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		if !ok {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("removing skill: %w", err)
	}
	fmt.Printf("Removed skill '%s'\n", skillName)
	return nil
}

func runValidateSkill(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	fm, err := parseSkillFrontmatter(path)
	if err != nil {
		return err
	}

	// The standalone validate command does not enforce name == dirname;
	// that check is only relevant during install.
	errs := validateSkillFrontmatter(fm, "")
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return fmt.Errorf("validation failed")
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Valid SKILL.md for skill '%s'\n", fm.Name)
	return nil
}

// resolveSkillsDir returns the skills directory from the --dir flag, relative to CWD.
func resolveSkillsDir(cmd *cobra.Command) (string, error) {
	// Walk up to find the flag on the parent (installSkillsCmd)
	dir := installSkillsDir
	if !filepath.IsAbs(dir) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting working directory: %w", err)
		}
		dir = filepath.Join(cwd, dir)
	}
	return dir, nil
}

func init() {
	// Persistent --dir flag on the root group command
	installSkillsCmd.PersistentFlags().StringVar(&installSkillsDir, "dir", ".agents/skills", "Directory where skills are installed")

	// install subcommand flags
	installSkillInstallCmd.Flags().BoolVar(&installSkillForce, "force", false, "Overwrite an existing skill without prompting")
	installSkillInstallCmd.Flags().BoolVar(&installSkillSkipValidate, "skip-validate", false, "Skip SKILL.md frontmatter validation")

	// list subcommand flags
	installSkillListCmd.Flags().StringVar(&installSkillOutput, "output", "table", "Output format: table or json")

	// remove subcommand flags
	installSkillRemoveCmd.Flags().BoolVar(&removeSkillForce, "force", false, "Remove without confirmation prompt")

	// Register subcommands
	installSkillsCmd.AddCommand(installSkillInstallCmd)
	installSkillsCmd.AddCommand(installSkillListCmd)
	installSkillsCmd.AddCommand(installSkillRemoveCmd)
	installSkillsCmd.AddCommand(installSkillValidateCmd)

	// Register with root
	rootCmd.AddCommand(installSkillsCmd)
}
