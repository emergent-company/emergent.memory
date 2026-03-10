package cmd

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	return parseSkillFrontmatterFromBytes(data, skillDir)
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

// skillContentFromBytes extracts the body content (after frontmatter) from SKILL.md bytes.
func skillContentFromBytes(data []byte) string {
	content := string(data)
	trimmed := strings.TrimLeft(content, "\r\n")
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return content
	}
	rest := trimmed[3:]
	closingIdx := strings.Index(rest, "\n---")
	if closingIdx == -1 {
		return content
	}
	after := rest[closingIdx+4:] // skip "\n---"
	return strings.TrimPrefix(strings.TrimPrefix(after, "\r"), "\n")
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

// FoundSkill represents a SKILL.md file found during discovery.
type FoundSkill struct {
	Path        string
	Name        string
	Description string
	Version     string
	Content     string
}

// discoverSkillsInDir scans a directory for SKILL.md files (either directly or
// in one level of subdirectories) and returns the parsed skills.
func discoverSkillsInDir(dir string) ([]FoundSkill, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("accessing %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", dir)
	}

	var found []FoundSkill

	// Check if the directory itself is a single skill.
	skillMDPath := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(skillMDPath); err == nil {
		if s, err := loadFoundSkill(dir, skillMDPath); err == nil {
			found = append(found, s)
		}
		return found, nil
	}

	// Otherwise scan subdirectories.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %q: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subPath := filepath.Join(dir, e.Name(), "SKILL.md")
		if _, err := os.Stat(subPath); err == nil {
			if s, err := loadFoundSkill(filepath.Join(dir, e.Name()), subPath); err == nil {
				found = append(found, s)
			}
		}
	}

	return found, nil
}

func loadFoundSkill(skillDir, skillMDPath string) (FoundSkill, error) {
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return FoundSkill{}, err
	}
	fm, err := parseSkillFrontmatterFromBytes(data, skillDir)
	if err != nil {
		return FoundSkill{}, err
	}
	content := skillContentFromBytes(data)
	return FoundSkill{
		Path:        skillMDPath,
		Name:        fm.Name,
		Description: fm.Description,
		Version:     fm.EffectiveVersion(),
		Content:     content,
	}, nil
}

// knownSkillDirs returns a list of well-known directories to search for skills.
// Expands ~ to the user's home directory.
func knownSkillDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	expand := func(p string) string {
		if home != "" && strings.HasPrefix(p, "~/") {
			return filepath.Join(home, p[2:])
		}
		return p
	}

	return []string{
		".agents/skills",
		expand("~/.claude/skills"),
		expand("~/.config/claude/skills"),
		expand("~/.local/share/claude/skills"),
		expand("~/.gemini/skills"),
		expand("~/.config/gemini/skills"),
		expand("~/.opencode/skills"),
		expand("~/.config/opencode/skills"),
	}
}
