package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sdktpacks "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/templatepacks"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────
// Top-level command
// ─────────────────────────────────────────────

var templatePacksCmd = &cobra.Command{
	Use:     "template-packs",
	Short:   "Manage template packs",
	Long:    "Commands for managing template packs in the Memory platform",
	GroupID: "knowledge",
}

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	tpackProjectFlag string
	tpackOutputFlag  string
	tpackFileFlag    string
	tpackDryRunFlag  bool
	tpackMergeFlag   bool
)

// ─────────────────────────────────────────────
// Helper: resolve project + return client
// ─────────────────────────────────────────────

func getTemplatePacksClient(cmd *cobra.Command) (*sdktpacks.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, err
	}

	projectID, err := resolveProjectContext(cmd, tpackProjectFlag)
	if err != nil {
		return nil, err
	}

	c.SetContext("", projectID)
	return c.SDK.TemplatePacks, nil
}

// ─────────────────────────────────────────────
// template-packs list  (available packs)
// ─────────────────────────────────────────────

var templatePacksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available template packs",
	Long:  "List template packs available for the current project to install",
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getTemplatePacksClient(cmd)
		if err != nil {
			return err
		}

		packs, err := tp.GetAvailablePacks(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list available packs: %w", err)
		}

		out := cmd.OutOrStdout()

		if tpackOutputFlag == "json" {
			return json.NewEncoder(out).Encode(packs)
		}

		if len(packs) == 0 {
			fmt.Fprintln(out, "No template packs available.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("ID", "Name", "Version", "Description")
		for _, p := range packs {
			desc := ""
			if p.Description != nil {
				desc = *p.Description
			}
			if len(desc) > 60 {
				desc = desc[:59] + "…"
			}
			_ = table.Append(p.ID, p.Name, p.Version, desc)
		}
		return table.Render()
	},
}

// ─────────────────────────────────────────────
// template-packs installed
// ─────────────────────────────────────────────

var templatePacksInstalledCmd = &cobra.Command{
	Use:   "installed",
	Short: "List installed template packs",
	Long:  "List template packs currently installed on the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getTemplatePacksClient(cmd)
		if err != nil {
			return err
		}

		packs, err := tp.GetInstalledPacks(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list installed packs: %w", err)
		}

		out := cmd.OutOrStdout()

		if tpackOutputFlag == "json" {
			return json.NewEncoder(out).Encode(packs)
		}

		if len(packs) == 0 {
			fmt.Fprintln(out, "No template packs installed.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("Assignment ID", "Pack ID", "Name", "Version", "Active", "Installed")
		for _, p := range packs {
			active := "yes"
			if !p.Active {
				active = "no"
			}
			_ = table.Append(
				p.ID,
				p.TemplatePackID,
				p.Name,
				p.Version,
				active,
				p.InstalledAt.Format("2006-01-02"),
			)
		}
		return table.Render()
	},
}

// ─────────────────────────────────────────────
// template-packs get <pack-id>
// ─────────────────────────────────────────────

var templatePacksGetCmd = &cobra.Command{
	Use:   "get <pack-id>",
	Short: "Get a template pack by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// get-pack is a global operation — no project context needed
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		pack, err := c.SDK.TemplatePacks.GetPack(context.Background(), args[0])
		if err != nil {
			return fmt.Errorf("failed to get template pack: %w", err)
		}

		out := cmd.OutOrStdout()

		if tpackOutputFlag == "json" {
			return json.NewEncoder(out).Encode(pack)
		}

		desc := ""
		if pack.Description != nil {
			desc = *pack.Description
		}
		author := ""
		if pack.Author != nil {
			author = *pack.Author
		}

		fmt.Fprintf(out, "ID:          %s\n", pack.ID)
		fmt.Fprintf(out, "Name:        %s\n", pack.Name)
		fmt.Fprintf(out, "Version:     %s\n", pack.Version)
		if desc != "" {
			fmt.Fprintf(out, "Description: %s\n", desc)
		}
		if author != "" {
			fmt.Fprintf(out, "Author:      %s\n", author)
		}
		fmt.Fprintf(out, "Draft:       %v\n", pack.Draft)
		fmt.Fprintf(out, "Created:     %s\n", pack.CreatedAt.Format("2006-01-02 15:04:05"))

		return nil
	},
}

// ─────────────────────────────────────────────
// template-packs create --file pack.json
// ─────────────────────────────────────────────

var templatePacksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a template pack from a JSON file",
	Long:  "Create a new template pack by loading its definition from a JSON file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if tpackFileFlag == "" {
			return fmt.Errorf("--file is required")
		}

		data, err := os.ReadFile(tpackFileFlag)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", tpackFileFlag, err)
		}

		var req sdktpacks.CreatePackRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}

		// create-pack is a global operation — no project context needed
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		pack, err := c.SDK.TemplatePacks.CreatePack(context.Background(), &req)
		if err != nil {
			return fmt.Errorf("failed to create template pack: %w", err)
		}

		out := cmd.OutOrStdout()

		if tpackOutputFlag == "json" {
			return json.NewEncoder(out).Encode(pack)
		}

		fmt.Fprintf(out, "Template pack created!\n")
		fmt.Fprintf(out, "  ID:      %s\n", pack.ID)
		fmt.Fprintf(out, "  Name:    %s\n", pack.Name)
		fmt.Fprintf(out, "  Version: %s\n", pack.Version)
		return nil
	},
}

// ─────────────────────────────────────────────
// template-packs install [<pack-id>] [--file pack.json]
// ─────────────────────────────────────────────

var templatePacksInstallCmd = &cobra.Command{
	Use:   "install [<pack-id>]",
	Short: "Install a template pack into the current project",
	Long: `Install a template pack into the current project.

Two modes:
  install <pack-id>         Install an existing pack from the registry by ID.
  install --file pack.json  Create a new pack from a JSON file and install it in one step.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && tpackFileFlag == "" {
			return fmt.Errorf("provide either a pack-id argument or --file <path>")
		}
		if len(args) > 0 && tpackFileFlag != "" {
			return fmt.Errorf("cannot use both a pack-id argument and --file at the same time")
		}

		var packID string

		if tpackFileFlag != "" {
			// Create the pack first, then fall through to install.
			data, err := os.ReadFile(tpackFileFlag)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", tpackFileFlag, err)
			}

			var req sdktpacks.CreatePackRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return fmt.Errorf("failed to parse JSON: %w", err)
			}

			// create-pack is a global operation — no project context needed
			c, err := getClient(cmd)
			if err != nil {
				return err
			}

			pack, err := c.SDK.TemplatePacks.CreatePack(context.Background(), &req)
			if err != nil {
				return fmt.Errorf("failed to create template pack: %w", err)
			}

			packID = pack.ID
		} else {
			packID = args[0]
		}

		tp, err := getTemplatePacksClient(cmd)
		if err != nil {
			return err
		}

		result, err := tp.AssignPack(context.Background(), &sdktpacks.AssignPackRequest{
			TemplatePackID: packID,
			DryRun:         tpackDryRunFlag,
			Merge:          tpackMergeFlag,
		})
		if err != nil {
			return fmt.Errorf("failed to install template pack: %w", err)
		}

		out := cmd.OutOrStdout()

		if tpackOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		if tpackDryRunFlag {
			// Dry-run output
			fmt.Fprintf(out, "[dry-run] Pack %q — %d type(s) would install, %d conflict(s)\n",
				result.PackName, len(result.InstalledTypes), len(result.Conflicts))
			if len(result.InstalledTypes) > 0 {
				fmt.Fprintf(out, "\nTypes to install (%d):\n", len(result.InstalledTypes))
				for _, t := range result.InstalledTypes {
					fmt.Fprintf(out, "  + %s\n", t)
				}
			}
			if len(result.SkippedTypes) > 0 {
				fmt.Fprintf(out, "\nTypes to skip — already registered (%d):\n", len(result.SkippedTypes))
				for _, t := range result.SkippedTypes {
					fmt.Fprintf(out, "  ~ %s\n", t)
				}
			}
			if len(result.MergedTypes) > 0 {
				fmt.Fprintf(out, "\nTypes to merge (%d):\n", len(result.MergedTypes))
				for _, t := range result.MergedTypes {
					fmt.Fprintf(out, "  ↑ %s\n", t)
				}
			}
			if len(result.Conflicts) > 0 {
				fmt.Fprintf(out, "\nConflicts (%d):\n", len(result.Conflicts))
				for _, c := range result.Conflicts {
					fmt.Fprintf(out, "  [%s]\n", c.TypeName)
					if len(c.AddedProperties) > 0 {
						fmt.Fprintf(out, "    added properties:    %s\n", strings.Join(c.AddedProperties, ", "))
					}
					if len(c.ConflictingProperties) > 0 {
						propNames := make([]string, len(c.ConflictingProperties))
						for i, p := range c.ConflictingProperties {
							propNames[i] = p.Property + " (existing_wins)"
						}
						fmt.Fprintf(out, "    property conflicts:  %s\n", strings.Join(propNames, ", "))
					}
				}
			}
			return nil
		}

		// Normal (non dry-run) output
		fmt.Fprintf(out, "Template pack installed.\n")
		fmt.Fprintf(out, "  Assignment ID:  %s\n", result.AssignmentID)
		fmt.Fprintf(out, "  Pack ID:        %s\n", result.PackID)
		fmt.Fprintf(out, "  Pack Name:      %s\n", result.PackName)
		if len(result.InstalledTypes) > 0 {
			fmt.Fprintf(out, "  Installed types (%d): %s\n", len(result.InstalledTypes), strings.Join(result.InstalledTypes, ", "))
		}
		if len(result.SkippedTypes) > 0 {
			fmt.Fprintf(out, "  Skipped types   (%d): %s\n", len(result.SkippedTypes), strings.Join(result.SkippedTypes, ", "))
		}
		if len(result.MergedTypes) > 0 {
			fmt.Fprintf(out, "  Merged types    (%d): %s\n", len(result.MergedTypes), strings.Join(result.MergedTypes, ", "))
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// template-packs uninstall <assignment-id>
// ─────────────────────────────────────────────

var templatePacksUninstallCmd = &cobra.Command{
	Use:   "uninstall <assignment-id>",
	Short: "Uninstall (remove) a template pack assignment from the current project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getTemplatePacksClient(cmd)
		if err != nil {
			return err
		}

		if err := tp.DeleteAssignment(context.Background(), args[0]); err != nil {
			return fmt.Errorf("failed to uninstall template pack: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Template pack assignment %s removed.\n", args[0])
		return nil
	},
}

// ─────────────────────────────────────────────
// template-packs delete <pack-id>
// ─────────────────────────────────────────────

var templatePacksDeleteCmd = &cobra.Command{
	Use:   "delete <pack-id>",
	Short: "Delete a template pack from the registry",
	Long:  "Permanently delete a template pack definition from the global registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// delete-pack is a global operation — no project context needed
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		if err := c.SDK.TemplatePacks.DeletePack(context.Background(), args[0]); err != nil {
			return fmt.Errorf("failed to delete template pack: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Template pack %s deleted.\n", args[0])
		return nil
	},
}

// ─────────────────────────────────────────────
// template-packs compiled-types
// ─────────────────────────────────────────────

var templatePacksCompiledTypesCmd = &cobra.Command{
	Use:   "compiled-types",
	Short: "Show compiled object and relationship types for the current project",
	Long:  "Returns the merged set of object and relationship type definitions from all installed template packs",
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getTemplatePacksClient(cmd)
		if err != nil {
			return err
		}

		types, err := tp.GetCompiledTypes(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get compiled types: %w", err)
		}

		out := cmd.OutOrStdout()

		if tpackOutputFlag == "json" {
			return json.NewEncoder(out).Encode(types)
		}

		fmt.Fprintf(out, "Object Types (%d):\n", len(types.ObjectTypes))
		if len(types.ObjectTypes) == 0 {
			fmt.Fprintln(out, "  (none)")
		} else {
			table := tablewriter.NewWriter(out)
			table.Header("Name", "Label", "Pack", "Description")
			for _, t := range types.ObjectTypes {
				desc := t.Description
				if len(desc) > 50 {
					desc = desc[:49] + "…"
				}
				_ = table.Append(t.Name, t.Label, t.PackName, desc)
			}
			if err := table.Render(); err != nil {
				return err
			}
		}

		fmt.Fprintf(out, "\nRelationship Types (%d):\n", len(types.RelationshipTypes))
		if len(types.RelationshipTypes) == 0 {
			fmt.Fprintln(out, "  (none)")
		} else {
			table := tablewriter.NewWriter(out)
			table.Header("Name", "Label", "Source → Target", "Pack")
			for _, t := range types.RelationshipTypes {
				srcDst := t.SourceType + " → " + t.TargetType
				_ = table.Append(t.Name, t.Label, srcDst, t.PackName)
			}
			if err := table.Render(); err != nil {
				return err
			}
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// init — wire up the command tree
// ─────────────────────────────────────────────

func init() {
	// Persistent flags on the parent command
	templatePacksCmd.PersistentFlags().StringVar(&tpackProjectFlag, "project", "", "Project ID (overrides config/env)")
	templatePacksCmd.PersistentFlags().StringVar(&tpackOutputFlag, "output", "table", "Output format: table or json")

	// Per-subcommand flags
	templatePacksCreateCmd.Flags().StringVar(&tpackFileFlag, "file", "", "Path to template pack JSON file (required)")
	templatePacksInstallCmd.Flags().StringVar(&tpackFileFlag, "file", "", "Create pack from JSON file and install in one step")
	templatePacksInstallCmd.Flags().BoolVar(&tpackDryRunFlag, "dry-run", false, "Preview what would be installed without making changes")
	templatePacksInstallCmd.Flags().BoolVar(&tpackMergeFlag, "merge", false, "Additively merge incoming type schemas into existing registered types")

	// Assemble
	templatePacksCmd.AddCommand(templatePacksListCmd)
	templatePacksCmd.AddCommand(templatePacksInstalledCmd)
	templatePacksCmd.AddCommand(templatePacksGetCmd)
	templatePacksCmd.AddCommand(templatePacksCreateCmd)
	templatePacksCmd.AddCommand(templatePacksInstallCmd)
	templatePacksCmd.AddCommand(templatePacksUninstallCmd)
	templatePacksCmd.AddCommand(templatePacksDeleteCmd)
	templatePacksCmd.AddCommand(templatePacksCompiledTypesCmd)

	rootCmd.AddCommand(templatePacksCmd)
}
