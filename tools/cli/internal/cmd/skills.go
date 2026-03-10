package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	sdkskills "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/skills"
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
	skillGlobalFlag      bool
	skillJSONFlag        bool
	skillNameFlag        string
	skillDescFlag        string
	skillContentFlag     string
	skillContentFileFlag string
	skillConfirmFlag     bool
)

// slug regex must match server-side validation
var skillSlugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ─────────────────────────────────────────────
// Helper: resolve project ID (empty = global)
// ─────────────────────────────────────────────

func resolveSkillProjectID(cmd *cobra.Command) (string, error) {
	if skillGlobalFlag {
		return "", nil
	}
	if skillProjectFlag != "" {
		return skillProjectFlag, nil
	}
	// Try to read from config/env, but don't error if absent (skills can be global)
	pid, err := resolveProjectContext(cmd, "")
	if err != nil {
		// No project context — treat as global
		return "", nil
	}
	return pid, nil
}

// ─────────────────────────────────────────────
// skills list
// ─────────────────────────────────────────────

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skills",
	Long:  "List skills. Use --project to list merged global + project-scoped skills, or --global for global only.",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		projectID, err := resolveSkillProjectID(cmd)
		if err != nil {
			return err
		}

		skills, err := c.SDK.Skills.List(context.Background(), projectID)
		if err != nil {
			return fmt.Errorf("failed to list skills: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag {
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

// ─────────────────────────────────────────────
// skills get <id>
// ─────────────────────────────────────────────

var skillGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a skill by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		skill, err := c.SDK.Skills.Get(context.Background(), args[0])
		if err != nil {
			return fmt.Errorf("failed to get skill: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag {
			return json.NewEncoder(out).Encode(skill)
		}

		scope := "global"
		if skill.ProjectID != nil && *skill.ProjectID != "" {
			scope = "project (" + *skill.ProjectID + ")"
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

		projectID, err := resolveSkillProjectID(cmd)
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

		skill, err := c.SDK.Skills.Create(context.Background(), projectID, req)
		if err != nil {
			return fmt.Errorf("failed to create skill: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag {
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
	Use:   "update <id>",
	Short: "Update a skill",
	Args:  cobra.ExactArgs(1),
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

		skill, err := c.SDK.Skills.Update(context.Background(), args[0], req)
		if err != nil {
			return fmt.Errorf("failed to update skill: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag {
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
	Use:   "delete <id>",
	Short: "Delete a skill by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !skillConfirmFlag {
			fmt.Fprintf(cmd.OutOrStdout(), "Delete skill %s? [y/N]: ", args[0])
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			answer := strings.TrimSpace(scanner.Text())
			if strings.ToLower(answer) != "y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		if err := c.SDK.Skills.Delete(context.Background(), args[0]); err != nil {
			return fmt.Errorf("failed to delete skill: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Skill %s deleted.\n", args[0])
		return nil
	},
}

// ─────────────────────────────────────────────
// skills import <path>
// ─────────────────────────────────────────────

// skillFrontmatter is the YAML frontmatter parsed from SKILL.md files.
type skillFrontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Metadata    map[string]any `yaml:"metadata,omitempty"`
}

var skillImportCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "Import a skill from a SKILL.md file",
	Long: `Import a skill from a SKILL.md file with YAML frontmatter.

The file must begin with a YAML frontmatter block delimited by "---":

  ---
  name: my-skill
  description: Does something useful
  ---
  # Skill content goes here`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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

		projectID, err := resolveSkillProjectID(cmd)
		if err != nil {
			return err
		}

		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkskills.CreateSkillRequest{
			Name:        fm.Name,
			Description: fm.Description,
			Content:     content,
			Metadata:    fm.Metadata,
		}

		skill, err := c.SDK.Skills.Create(context.Background(), projectID, req)
		if err != nil {
			return fmt.Errorf("failed to import skill: %w", err)
		}

		out := cmd.OutOrStdout()

		if skillJSONFlag {
			return json.NewEncoder(out).Encode(skill)
		}

		fmt.Fprintf(out, "Skill imported!\n")
		fmt.Fprintf(out, "  ID:   %s\n", skill.ID)
		fmt.Fprintf(out, "  Name: %s\n", skill.Name)
		return nil
	},
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

// ─────────────────────────────────────────────
// init — wire up the command tree
// ─────────────────────────────────────────────

func init() {
	// Persistent flags on the parent command
	skillsCmd.PersistentFlags().StringVar(&skillProjectFlag, "project", "", "Project ID (creates/lists project-scoped skill)")
	skillsCmd.PersistentFlags().BoolVar(&skillGlobalFlag, "global", false, "Use global scope (overrides --project)")
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

	skillImportCmd.Flags().StringVar(&skillProjectFlag, "project", "", "Project ID (imports as project-scoped skill)")

	// Assemble
	skillsCmd.AddCommand(skillListCmd)
	skillsCmd.AddCommand(skillGetCmd)
	skillsCmd.AddCommand(skillCreateCmd)
	skillsCmd.AddCommand(skillUpdateCmd)
	skillsCmd.AddCommand(skillDeleteCmd)
	skillsCmd.AddCommand(skillImportCmd)

	rootCmd.AddCommand(skillsCmd)
}
