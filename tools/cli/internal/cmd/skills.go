package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/skillsfs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────
// Top-level command
// ─────────────────────────────────────────────

var skillsCmd = &cobra.Command{
	Use:     "skills",
	Short:   "Manage skills",
	Long:    "Commands for managing skills — reusable Markdown workflow instructions for agents",
	GroupID: "ai",
}

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	skillProjectFlag     string
	skillOrgFlag         string
	skillGlobalFlag      bool
	skillJSONFlag        bool
	skillNameFlag        string
	skillDescFlag        string
	skillContentFlag     string
	skillContentFileFlag string
	skillConfirmFlag     bool
	// import flags
	skillImportFromDirFlag      string
	skillImportDiscoverFlag     bool
	skillImportAllFlag          bool
	skillImportExperimentalFlag bool
)

// slug regex must match server-side validation
var skillSlugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ─────────────────────────────────────────────
// Helper: resolve skill scope
// ─────────────────────────────────────────────

// resolveSkillScope determines where a skill should be created/listed.
// Resolution priority:
//  1. --global → (projectID="", orgID="")   — global scope, requires superadmin
//  2. --org <id> → (projectID="", orgID=id) — org-scoped
//  3. --project <id> / config project → (projectID=id, orgID="") — project-scoped (default)
//
// If none of the above yield a scope, returns an error asking the user to specify.
func resolveSkillScope(cmd *cobra.Command) (projectID, orgID string, err error) {
	if skillGlobalFlag {
		return "", "", nil
	}
	if skillOrgFlag != "" {
		return "", skillOrgFlag, nil
	}
	if skillProjectFlag != "" {
		return skillProjectFlag, "", nil
	}
	// Try config/env for project context
	pid, pidErr := resolveProjectContext(cmd, "")
	if pidErr == nil && pid != "" {
		return pid, "", nil
	}
	return "", "", fmt.Errorf("scope required: use --project <id>, --org <id>, or --global")
}

// ─────────────────────────────────────────────
// skills list
// ─────────────────────────────────────────────

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skills installed on the server",
	Long: `List skills stored on the server and available to agents.

Output is a table with columns: NAME, DESCRIPTION (truncated to 55 characters),
SCOPE (global/org/project), and ID. Use --project to include project-scoped
skills, or --global for global-only skills. Use --json to receive the full
skill list as JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		projectID, orgID, err := resolveSkillScope(cmd)
		if err != nil {
			return err
		}

		var skills []*sdkskills.Skill
		if orgID != "" {
			skills, err = c.SDK.Skills.ListOrgSkills(context.Background(), orgID)
		} else {
			skills, err = c.SDK.Skills.List(context.Background(), projectID)
		}
		if err != nil {
			return fmt.Errorf("failed to list skills: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag || output == "json" {
			return json.NewEncoder(out).Encode(skills)
		}

		if len(skills) == 0 {
			fmt.Fprintln(out, "No skills found.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("NAME", "DESCRIPTION", "SCOPE", "ID")
		for _, s := range skills {
			scope := "global"
			if s.ProjectID != nil && *s.ProjectID != "" {
				scope = "project"
			} else if s.OrgID != nil && *s.OrgID != "" {
				scope = "org"
			}
			desc := s.Description
			if len(desc) > 55 {
				desc = desc[:54] + "…"
			}
			_ = table.Append(s.Name, desc, scope, s.ID)
		}
		return table.Render()
	},
}

// resolveSkillArgOrPick resolves a skill ID from args[0], or, when args is
// empty and stdin is a terminal, lists skills in the current scope and shows
// an interactive picker. Returns the resolved skill ID.
func resolveSkillArgOrPick(cmd *cobra.Command, c *client.Client, args []string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}

	if isNonInteractive() {
		return "", fmt.Errorf("skill ID is required — pass an ID or run interactively to pick from a list")
	}

	// Resolve scope so we know which skills to list.
	// --global passes projectID="" to List which returns global skills.
	projectID, orgID, _ := resolveSkillScope(cmd)

	var skills []*sdkskills.Skill
	var err error
	if orgID != "" {
		skills, err = c.SDK.Skills.ListOrgSkills(context.Background(), orgID)
	} else {
		// Covers both --global (projectID="") and project-scoped.
		skills, err = c.SDK.Skills.List(context.Background(), projectID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to list skills: %w", err)
	}
	if len(skills) == 0 {
		return "", fmt.Errorf("no skills found in the current scope")
	}

	items := make([]PickerItem, len(skills))
	for i, s := range skills {
		desc := s.Description
		if len(desc) > 60 {
			desc = desc[:57] + "…"
		}
		items[i] = PickerItem{ID: s.ID, Name: s.Name + "  " + desc}
	}

	id, _, err := promptResourcePicker("Select a skill", items)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("skill ID is required")
	}
	return id, nil
}

// ─────────────────────────────────────────────
// skills get <id>
// ─────────────────────────────────────────────

var skillGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get a skill by ID",
	Long: `Get full details for a skill by its ID.

Prints ID, Name, Description, Scope (global / org / project), Created and
Updated timestamps, and the full skill Content. Use --json to receive the raw
JSON response instead.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		skillID, err := resolveSkillArgOrPick(cmd, c, args)
		if err != nil {
			return err
		}

		skill, err := c.SDK.Skills.Get(context.Background(), skillID)
		if err != nil {
			return fmt.Errorf("failed to get skill: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag || output == "json" {
			return json.NewEncoder(out).Encode(skill)
		}

		scope := "global"
		if skill.ProjectID != nil && *skill.ProjectID != "" {
			scope = "project (" + *skill.ProjectID + ")"
		} else if skill.OrgID != nil && *skill.OrgID != "" {
			scope = "org (" + *skill.OrgID + ")"
		}

		fmt.Fprintf(out, "ID:          %s\n", skill.ID)
		fmt.Fprintf(out, "Name:        %s\n", skill.Name)
		fmt.Fprintf(out, "Description: %s\n", skill.Description)
		fmt.Fprintf(out, "Scope:       %s\n", scope)
		fmt.Fprintf(out, "Created:     %s\n", skill.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "Updated:     %s\n", skill.UpdatedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "\n--- Content ---\n%s\n", skill.Content)
		return nil
	},
}

// ─────────────────────────────────────────────
// skills create
// ─────────────────────────────────────────────

var skillCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a skill",
	Long:  "Create a new skill. Use --project to create a project-scoped skill, or omit for global.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if skillNameFlag == "" {
			return fmt.Errorf("--name is required")
		}
		if !skillSlugRe.MatchString(skillNameFlag) || len(skillNameFlag) > 64 {
			return fmt.Errorf("--name must be a lowercase slug (e.g. 'my-skill'), max 64 chars")
		}
		if skillDescFlag == "" {
			return fmt.Errorf("--description is required")
		}

		content, err := resolveSkillContent()
		if err != nil {
			return err
		}
		if content == "" {
			return fmt.Errorf("provide skill content via --content or --content-file")
		}

		projectID, orgID, err := resolveSkillScope(cmd)
		if err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkskills.CreateSkillRequest{
			Name:        skillNameFlag,
			Description: skillDescFlag,
			Content:     content,
		}

		var skill *sdkskills.Skill
		if orgID != "" {
			skill, err = c.SDK.Skills.CreateOrgSkill(context.Background(), orgID, req)
		} else {
			skill, err = c.SDK.Skills.Create(context.Background(), projectID, req)
		}
		if err != nil {
			return fmt.Errorf("failed to create skill: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag || output == "json" {
			return json.NewEncoder(out).Encode(skill)
		}

		fmt.Fprintf(out, "Skill created!\n")
		fmt.Fprintf(out, "  ID:   %s\n", skill.ID)
		fmt.Fprintf(out, "  Name: %s\n", skill.Name)
		return nil
	},
}

// ─────────────────────────────────────────────
// skills update <id>
// ─────────────────────────────────────────────

var skillUpdateCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update a skill",
	Long: `Update the description or content of an existing skill.

Prints "Skill updated." followed by the skill's ID and Name on success. At
least one of --description, --content, or --content-file must be provided.
Use --json to receive the full updated skill as JSON instead.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := &sdkskills.UpdateSkillRequest{}
		hasUpdate := false

		if skillDescFlag != "" {
			req.Description = &skillDescFlag
			hasUpdate = true
		}

		if skillContentFlag != "" || skillContentFileFlag != "" {
			content, err := resolveSkillContent()
			if err != nil {
				return err
			}
			if content != "" {
				req.Content = &content
				hasUpdate = true
			}
		}

		if !hasUpdate {
			return fmt.Errorf("provide at least one of --description, --content, or --content-file")
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		skillID, err := resolveSkillArgOrPick(cmd, c, args)
		if err != nil {
			return err
		}

		skill, err := c.SDK.Skills.Update(context.Background(), skillID, req)
		if err != nil {
			return fmt.Errorf("failed to update skill: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag || output == "json" {
			return json.NewEncoder(out).Encode(skill)
		}

		fmt.Fprintf(out, "Skill updated.\n")
		fmt.Fprintf(out, "  ID:   %s\n", skill.ID)
		fmt.Fprintf(out, "  Name: %s\n", skill.Name)
		return nil
	},
}

// ─────────────────────────────────────────────
// skills delete <id>
// ─────────────────────────────────────────────

var skillDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a skill by ID",
	Long: `Permanently delete a skill by its ID.

Prints "Skill <id> deleted." on success. You will be prompted for confirmation
unless the --confirm flag is provided.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		skillID, err := resolveSkillArgOrPick(cmd, c, args)
		if err != nil {
			return err
		}

		if !skillConfirmFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Delete skill %s? [y/N]: ", skillID)
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			answer := strings.TrimSpace(scanner.Text())
			if strings.ToLower(answer) != "y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}

		if err := c.SDK.Skills.Delete(context.Background(), skillID); err != nil {
			return fmt.Errorf("failed to delete skill: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Skill %s deleted.\n", skillID)
		return nil
	},
}

// ─────────────────────────────────────────────
// skills import
// ─────────────────────────────────────────────

// skillFrontmatter is the YAML frontmatter parsed from SKILL.md files.
type skillFrontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Metadata    map[string]any `yaml:"metadata,omitempty"`
}

var skillImportCmd = &cobra.Command{
	Use:   "import [path]",
	Short: "Import skills from a SKILL.md file or directory",
	Long: `Import one or more skills and register them on the server so agents can use them.

Import a single SKILL.md file:
  memory skills import path/to/SKILL.md

Import all skills found in a directory (scans one level deep for SKILL.md files):
  memory skills import --from-dir .agents/skills/

Auto-discover skills from well-known locations (.agents/skills/, ~/.claude/skills/, etc.):
  memory skills import --discover

Import all discovered skills without prompting:
  memory skills import --discover --all

Import built-in Memory skills from the embedded catalog:
  memory skills import --builtin

Import built-in skills including experimental ones:
  memory skills import --builtin --experimental`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, orgID, err := resolveSkillScope(cmd)
		if err != nil {
			return err
		}
		_ = orgID // used below when routing to org-scoped import

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		// --- built-in embedded catalog ---
		builtinFlag, _ := cmd.Flags().GetBool("builtin")
		if builtinFlag {
			return importFromEmbeddedCatalog(cmd, c, projectID, orgID)
		}

		// --- directory scan ---
		if skillImportFromDirFlag != "" {
			skills, err := discoverSkillsInDir(skillImportFromDirFlag)
			if err != nil {
				return fmt.Errorf("scanning directory: %w", err)
			}
			if len(skills) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No SKILL.md files found in %s\n", skillImportFromDirFlag)
				return nil
			}
			return importFoundSkills(cmd, c, projectID, orgID, skills, skillImportAllFlag)
		}

		// --- auto-discover from known locations ---
		if skillImportDiscoverFlag {
			var all []FoundSkill
			for _, dir := range knownSkillDirs() {
				found, err := discoverSkillsInDir(dir)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s: %v\n", dir, err)
					continue
				}
				all = append(all, found...)
			}
			if len(all) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No skills found in well-known locations.")
				fmt.Fprintln(cmd.OutOrStdout(), "Searched:", strings.Join(knownSkillDirs(), ", "))
				return nil
			}
			return importFoundSkills(cmd, c, projectID, orgID, all, skillImportAllFlag)
		}

		// --- single file import ---
		if len(args) == 0 {
			return fmt.Errorf("provide a SKILL.md path, --from-dir, --discover, or --builtin")
		}

		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", args[0], err)
		}

		fm, content, err := parseSkillFile(data)
		if err != nil {
			return fmt.Errorf("failed to parse skill file: %w", err)
		}

		if fm.Name == "" {
			return fmt.Errorf("frontmatter missing required field: name")
		}
		if fm.Description == "" {
			return fmt.Errorf("frontmatter missing required field: description")
		}
		if !skillSlugRe.MatchString(fm.Name) || len(fm.Name) > 64 {
			return fmt.Errorf("name %q is not a valid slug (lowercase, hyphens, max 64 chars)", fm.Name)
		}
		if strings.TrimSpace(content) == "" {
			return fmt.Errorf("skill content (after frontmatter) is empty")
		}

		req := &sdkskills.CreateSkillRequest{
			Name:        fm.Name,
			Description: fm.Description,
			Content:     content,
			Metadata:    fm.Metadata,
		}

		var skill *sdkskills.Skill
		if orgID != "" {
			skill, err = c.SDK.Skills.CreateOrgSkill(context.Background(), orgID, req)
		} else {
			skill, err = c.SDK.Skills.Create(context.Background(), projectID, req)
		}
		if err != nil {
			return fmt.Errorf("failed to import skill: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag || output == "json" {
			return json.NewEncoder(out).Encode(skill)
		}

		fmt.Fprintf(out, "Skill imported!\n")
		fmt.Fprintf(out, "  ID:   %s\n", skill.ID)
		fmt.Fprintf(out, "  Name: %s\n", skill.Name)
		return nil
	},
}

// importFoundSkills imports a list of FoundSkills to the server.
// If all is true, imports without prompting. Otherwise prompts for each.
func importFoundSkills(cmd *cobra.Command, c *client.Client, projectID, orgID string, skills []FoundSkill, all bool) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Found %d skill(s):\n", len(skills))
	for _, s := range skills {
		ver := ""
		if s.Version != "" {
			ver = " (v" + s.Version + ")"
		}
		label := s.Name
		if s.Experimental {
			label = s.Name + " [experimental]"
		}
		fmt.Fprintf(out, "  • %s%s — %s\n", label, ver, truncate(s.Description, 60))
	}
	fmt.Fprintln(out)

	imported := 0
	skipped := 0
	errors := 0
	var err error

	for _, s := range skills {
		if !all && isInteractiveTerminal() {
			promptName := s.Name
			if s.Experimental {
				promptName = s.Name + " [experimental]"
			}
			ok, err := promptYesNo(fmt.Sprintf("Import '%s'? [y/N]: ", promptName))
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
			if !ok {
				fmt.Fprintf(out, "Skipped '%s'\n", s.Name)
				skipped++
				continue
			}
		}

		req := &sdkskills.CreateSkillRequest{
			Name:        s.Name,
			Description: s.Description,
			Content:     s.Content,
		}

		if orgID != "" {
			_, err = c.SDK.Skills.CreateOrgSkill(context.Background(), orgID, req)
		} else {
			_, err = c.SDK.Skills.Create(context.Background(), projectID, req)
		}
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error importing '%s': %v\n", s.Name, err)
			errors++
			continue
		}

		fmt.Fprintf(out, "Imported '%s'\n", s.Name)
		imported++
	}

	fmt.Fprintf(out, "\nDone: %d imported, %d skipped", imported, skipped)
	if errors > 0 {
		fmt.Fprintf(out, ", %d error(s)", errors)
	}
	fmt.Fprintln(out)

	if errors > 0 {
		return fmt.Errorf("%d skill(s) failed to import", errors)
	}
	return nil
}

// importFromEmbeddedCatalog imports skills from the built-in embedded catalog.
func importFromEmbeddedCatalog(cmd *cobra.Command, c *client.Client, projectID, orgID string) error {
	out := cmd.OutOrStdout()
	catalog := skillsfs.Catalog()

	experimentalFlag, _ := cmd.Flags().GetBool("experimental")

	entries, err := fs.ReadDir(catalog, ".")
	if err != nil {
		return fmt.Errorf("reading embedded catalog: %w", err)
	}

	var skills []FoundSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillMDPath := e.Name() + "/SKILL.md"
		data, err := fs.ReadFile(catalog, skillMDPath)
		if err != nil {
			continue
		}
		fm, err := parseSkillFrontmatterFromBytes(data, e.Name())
		if err != nil {
			continue
		}
		// Skip experimental skills unless --experimental flag is set.
		if fm.Experimental && !experimentalFlag {
			continue
		}
		content := skillContentFromBytes(data)
		skills = append(skills, FoundSkill{
			Path:         skillMDPath,
			Name:         fm.Name,
			Description:  fm.Description,
			Version:      fm.EffectiveVersion(),
			Content:      content,
			Experimental: fm.Experimental,
		})
	}

	if len(skills) == 0 {
		if experimentalFlag {
			fmt.Fprintln(out, "No skills found in the embedded catalog.")
		} else {
			fmt.Fprintln(out, "No skills found in the embedded catalog. Use --experimental to include experimental skills.")
		}
		return nil
	}

	return importFoundSkills(cmd, c, projectID, orgID, skills, skillImportAllFlag)
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

// resolveSkillContent returns the skill content from --content or --content-file flags.
func resolveSkillContent() (string, error) {
	if skillContentFileFlag != "" {
		data, err := os.ReadFile(skillContentFileFlag)
		if err != nil {
			return "", fmt.Errorf("failed to read content file %s: %w", skillContentFileFlag, err)
		}
		return string(data), nil
	}
	return skillContentFlag, nil
}

// parseSkillFile splits a SKILL.md file into YAML frontmatter and body content.
// The file must start with "---\n", contain YAML, and close with "---\n" or "---".
func parseSkillFile(data []byte) (*skillFrontmatter, string, error) {
	const delim = "---"

	// Trim leading whitespace/BOM
	trimmed := bytes.TrimSpace(data)

	// Must start with ---
	if !bytes.HasPrefix(trimmed, []byte(delim)) {
		return nil, "", fmt.Errorf("file does not begin with YAML frontmatter (expected '---')")
	}

	// Find end of first line (the opening ---)
	rest := trimmed[len(delim):]
	if len(rest) > 0 && rest[0] == '\r' {
		rest = rest[1:]
	}
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}

	// Find closing ---
	closeIdx := bytes.Index(rest, []byte("\n"+delim))
	if closeIdx < 0 {
		return nil, "", fmt.Errorf("frontmatter closing '---' not found")
	}

	yamlPart := rest[:closeIdx]
	after := rest[closeIdx+1+len(delim):]

	// Strip optional \r\n after closing ---
	after = bytes.TrimPrefix(after, []byte("\r"))
	after = bytes.TrimPrefix(after, []byte("\n"))

	var fm skillFrontmatter
	if err := yaml.Unmarshal(yamlPart, &fm); err != nil {
		return nil, "", fmt.Errorf("failed to parse YAML frontmatter: %w", err)
	}

	return &fm, string(after), nil
}

// truncate shortens s to max chars, appending "…" if truncated.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// ─────────────────────────────────────────────
// init — wire up the command tree
// ─────────────────────────────────────────────

func init() {
	// Persistent flags on the parent command
	skillsCmd.PersistentFlags().StringVar(&skillProjectFlag, "project", "", "Project ID (creates/lists project-scoped skill)")
	skillsCmd.PersistentFlags().StringVar(&skillOrgFlag, "org", "", "Organization ID (creates/lists org-scoped skill)")
	skillsCmd.PersistentFlags().BoolVar(&skillGlobalFlag, "global", false, "Use global scope (built-in skills only, superadmin)")
	skillsCmd.PersistentFlags().BoolVar(&skillJSONFlag, "json", false, "Output as JSON")

	// Per-subcommand flags
	skillCreateCmd.Flags().StringVar(&skillNameFlag, "name", "", "Skill name (slug, e.g. 'my-skill') (required)")
	skillCreateCmd.Flags().StringVar(&skillDescFlag, "description", "", "Skill description (required)")
	skillCreateCmd.Flags().StringVar(&skillContentFlag, "content", "", "Skill content (Markdown)")
	skillCreateCmd.Flags().StringVar(&skillContentFileFlag, "content-file", "", "Path to a file containing the skill content")

	skillUpdateCmd.Flags().StringVar(&skillDescFlag, "description", "", "New description")
	skillUpdateCmd.Flags().StringVar(&skillContentFlag, "content", "", "New content (Markdown)")
	skillUpdateCmd.Flags().StringVar(&skillContentFileFlag, "content-file", "", "Path to file with new content")

	skillDeleteCmd.Flags().BoolVar(&skillConfirmFlag, "confirm", false, "Skip confirmation prompt")

	// import flags
	skillImportCmd.Flags().StringVar(&skillImportFromDirFlag, "from-dir", "", "Scan a directory for SKILL.md files and import all found skills")
	skillImportCmd.Flags().BoolVar(&skillImportDiscoverFlag, "discover", false, "Auto-discover skills from well-known locations (.agents/skills/, ~/.claude/skills/, etc.)")
	skillImportCmd.Flags().BoolVar(&skillImportAllFlag, "all", false, "Import all found skills without prompting")
	skillImportCmd.Flags().Bool("builtin", false, "Import from the built-in embedded Memory skill catalog")
	skillImportCmd.Flags().BoolVar(&skillImportExperimentalFlag, "experimental", false, "Include experimental skills when importing from the built-in catalog (--builtin)")

	// Assemble
	skillsCmd.AddCommand(skillListCmd)
	skillsCmd.AddCommand(skillGetCmd)
	skillsCmd.AddCommand(skillCreateCmd)
	skillsCmd.AddCommand(skillUpdateCmd)
	skillsCmd.AddCommand(skillDeleteCmd)
	skillsCmd.AddCommand(skillImportCmd)

	rootCmd.AddCommand(skillsCmd)
}
