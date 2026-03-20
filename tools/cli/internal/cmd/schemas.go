package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────
// Top-level command
// ─────────────────────────────────────────────

var schemasCmd = &cobra.Command{
	Use:     "schemas",
	Short:   "Manage schemas",
	Long:    "Commands for managing schemas in the Memory platform",
	GroupID: "knowledge",
}

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	schemaProjectFlag string
	schemaOutputFlag  string
	schemaFileFlag    string
	schemaDryRunFlag  bool
	schemaMergeFlag   bool
)

// ─────────────────────────────────────────────
// Helper: resolve project + return client
// ─────────────────────────────────────────────

func getSchemasClient(cmd *cobra.Command) (*sdkschemas.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, err
	}

	projectID, err := resolveProjectContext(cmd, schemaProjectFlag)
	if err != nil {
		return nil, err
	}

	// Resolve orgID: prefer config, then auto-detect from server.
	orgID := ""
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}
	if cfg, err := config.LoadWithEnv(configPath); err == nil && cfg.OrgID != "" {
		orgID = cfg.OrgID
	}
	if orgID == "" {
		// Auto-detect from server (same pattern as resolveProviderOrgID).
		orgs, err := c.SDK.Orgs.List(context.Background())
		if err == nil && len(orgs) > 0 {
			orgID = orgs[0].ID
		}
	}

	c.SetContext(orgID, projectID)
	return c.SDK.Schemas, nil
}

// ─────────────────────────────────────────────
// schemas list  (installed schemas by default; --available for registry)
// ─────────────────────────────────────────────

var schemasListAvailableFlag bool

var schemasListCmd = &cobra.Command{
	Use:   "list",
	Short: "List schemas installed on this project",
	Long: `List schemas installed on the current project (default), or schemas available
in the registry to install (--available).

Schemas installed via 'memory blueprints install' appear here under 'installed'.
Use 'memory schemas compiled-types' to see the full merged set of active types.

Output is a table. Use --output json to receive the full list as JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()

		if schemasListAvailableFlag {
			// Show registry schemas not yet installed
			packs, err := tp.GetAvailablePacks(context.Background())
			if err != nil {
				return fmt.Errorf("failed to list available schemas: %w", err)
			}
			if schemaOutputFlag == "json" {
				return json.NewEncoder(out).Encode(packs)
			}
			if len(packs) == 0 {
				fmt.Fprintln(out, "No schemas available in the registry.")
				fmt.Fprintln(out, "Schemas are typically installed via 'memory blueprints install', not the registry.")
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
		}

		// Default: show installed schemas (same as 'schemas installed')
		packs, err := tp.GetInstalledPacks(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list installed schemas: %w", err)
		}
		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(packs)
		}
		if len(packs) == 0 {
			fmt.Fprintln(out, "No schemas installed on this project.")
			fmt.Fprintln(out, "Install schemas via: memory blueprints install <source>")
			return nil
		}
		table := tablewriter.NewWriter(out)
		table.Header("Schema ID", "Name", "Version", "Active", "Installed")
		for _, p := range packs {
			active := "yes"
			if !p.Active {
				active = "no"
			}
			_ = table.Append(p.SchemaID, p.Name, p.Version, active, p.InstalledAt.Format("2006-01-02"))
		}
		return table.Render()
	},
}

// ─────────────────────────────────────────────
// schemas installed
// ─────────────────────────────────────────────

var schemasInstalledCmd = &cobra.Command{
	Use:   "installed",
	Short: "List installed schemas",
	Long: `List schemas currently installed (assigned) on the current project.

Output is a table with columns: Assignment ID, Schema ID, Name, Version,
Active (yes/no), and Installed date. The Assignment ID is used with
'memory schemas uninstall' to remove a schema from the project. Use
--output json to receive the full list as JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		packs, err := tp.GetInstalledPacks(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list installed schemas: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(packs)
		}

		if len(packs) == 0 {
			fmt.Fprintln(out, "No schemas installed.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("Assignment ID", "Schema ID", "Name", "Version", "Active", "Installed")
		for _, p := range packs {
			active := "yes"
			if !p.Active {
				active = "no"
			}
			_ = table.Append(
				p.ID,
				p.SchemaID,
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
// schemas get <schema-id>
// ─────────────────────────────────────────────

var schemasGetCmd = &cobra.Command{
	Use:   "get <schema-id>",
	Short: "Get a schema by ID",
	Long: `Get details for a schema pack by its ID.

Prints ID, Name, Version, Description (if set), Author (if set), Draft status,
and Created timestamp. Use --output json to receive the full schema record as
JSON instead.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// get-schema is a global operation — no project context needed
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		pack, err := c.SDK.Schemas.GetPack(context.Background(), args[0])
		if err != nil {
			return fmt.Errorf("failed to get schema: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
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
// schemas create --file schema.json
// ─────────────────────────────────────────────

var schemasCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a schema from a JSON file",
	Long:  "Create a new schema by loading its definition from a JSON file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaFileFlag == "" {
			return fmt.Errorf("--file is required")
		}

		data, err := os.ReadFile(schemaFileFlag)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", schemaFileFlag, err)
		}

		var req sdkschemas.CreatePackRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}

		// create-schema is a global operation — no project context needed
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		pack, err := c.SDK.Schemas.CreatePack(context.Background(), &req)
		if err != nil {
			return fmt.Errorf("failed to create schema: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(pack)
		}

		fmt.Fprintf(out, "Schema created!\n")
		fmt.Fprintf(out, "  ID:      %s\n", pack.ID)
		fmt.Fprintf(out, "  Name:    %s\n", pack.Name)
		fmt.Fprintf(out, "  Version: %s\n", pack.Version)
		return nil
	},
}

// ─────────────────────────────────────────────
// schemas install [<schema-id>] [--file schema.json]
// ─────────────────────────────────────────────

var schemasInstallCmd = &cobra.Command{
	Use:   "install [<schema-id>]",
	Short: "Install a schema into the current project",
	Long: `Install a schema into the current project.

Two modes:
  install <schema-id>         Install an existing schema from the registry by ID.
  install --file schema.json  Create a new schema from a JSON file and install it in one step.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && schemaFileFlag == "" {
			return fmt.Errorf("provide either a schema-id argument or --file <path>")
		}
		if len(args) > 0 && schemaFileFlag != "" {
			return fmt.Errorf("cannot use both a schema-id argument and --file at the same time")
		}

		var schemaID string

		if schemaFileFlag != "" {
			// Create the schema first, then fall through to install.
			data, err := os.ReadFile(schemaFileFlag)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", schemaFileFlag, err)
			}

			var req sdkschemas.CreatePackRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return fmt.Errorf("failed to parse JSON: %w", err)
			}

			// create-schema is a global operation — no project context needed
			c, err := getClient(cmd)
			if err != nil {
				return err
			}

			pack, err := c.SDK.Schemas.CreatePack(context.Background(), &req)
			if err != nil {
				return fmt.Errorf("failed to create schema: %w", err)
			}

			schemaID = pack.ID
		} else {
			schemaID = args[0]
		}

		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		result, err := tp.AssignPack(context.Background(), &sdkschemas.AssignPackRequest{
			SchemaID: schemaID,
			DryRun:   schemaDryRunFlag,
			Merge:    schemaMergeFlag,
		})
		if err != nil {
			return fmt.Errorf("failed to install schema: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		if schemaDryRunFlag {
			// Dry-run output
			fmt.Fprintf(out, "[dry-run] Schema %q — %d type(s) would install, %d conflict(s)\n",
				result.SchemaName, len(result.InstalledTypes), len(result.Conflicts))
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
		fmt.Fprintf(out, "Schema installed.\n")
		fmt.Fprintf(out, "  Assignment ID:  %s\n", result.AssignmentID)
		fmt.Fprintf(out, "  Schema ID:      %s\n", result.SchemaID)
		fmt.Fprintf(out, "  Schema Name:    %s\n", result.SchemaName)
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
// schemas uninstall <assignment-id>
// ─────────────────────────────────────────────

var schemasUninstallCmd = &cobra.Command{
	Use:   "uninstall <assignment-id>",
	Short: "Uninstall (remove) a schema assignment from the current project",
	Long: `Remove a schema assignment from the current project by its assignment ID.

Use 'memory schemas installed' to list assignment IDs. Prints
"Schema assignment <id> removed." on success.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		if err := tp.DeleteAssignment(context.Background(), args[0]); err != nil {
			return fmt.Errorf("failed to uninstall schema: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Schema assignment %s removed.\n", args[0])
		return nil
	},
}

// ─────────────────────────────────────────────
// schemas delete <schema-id>
// ─────────────────────────────────────────────

var schemasDeleteCmd = &cobra.Command{
	Use:   "delete <schema-id>",
	Short: "Delete a schema from the registry",
	Long:  "Permanently delete a schema definition from the global registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// delete-schema is a global operation — no project context needed
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		if err := c.SDK.Schemas.DeletePack(context.Background(), args[0]); err != nil {
			return fmt.Errorf("failed to delete schema: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Schema %s deleted.\n", args[0])
		return nil
	},
}

// ─────────────────────────────────────────────
// schemas compiled-types
// ─────────────────────────────────────────────

var schemasCompiledTypesCmd = &cobra.Command{
	Use:   "compiled-types",
	Short: "Show compiled object and relationship types for the current project",
	Long: `Show the merged set of type definitions compiled from all installed schemas.

Prints two tables: Object Types (columns: Name, Label, Schema, Description)
and Relationship Types (columns: Name, Label, Source → Target, Schema). Use
--output json to receive the raw compiled types as JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		types, err := tp.GetCompiledTypes(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get compiled types: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(types)
		}

		fmt.Fprintf(out, "Object Types (%d):\n", len(types.ObjectTypes))
		if len(types.ObjectTypes) == 0 {
			fmt.Fprintln(out, "  (none)")
		} else {
			table := tablewriter.NewWriter(out)
			table.Header("Name", "Label", "Schema", "Description")
			for _, t := range types.ObjectTypes {
				desc := t.Description
				if len(desc) > 50 {
					desc = desc[:49] + "…"
				}
				_ = table.Append(t.Name, t.Label, t.SchemaName, desc)
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
			table.Header("Name", "Label", "Source → Target", "Schema")
			for _, t := range types.RelationshipTypes {
				srcDst := t.SourceType + " → " + t.TargetType
				_ = table.Append(t.Name, t.Label, srcDst, t.SchemaName)
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
	schemasCmd.PersistentFlags().StringVar(&schemaProjectFlag, "project", "", "Project ID (overrides config/env)")
	schemasCmd.PersistentFlags().StringVar(&schemaOutputFlag, "output", "table", "Output format: table or json")

	// Per-subcommand flags
	schemasListCmd.Flags().BoolVar(&schemasListAvailableFlag, "available", false, "Show schemas available in the registry (not yet installed) instead of installed schemas")
	schemasCreateCmd.Flags().StringVar(&schemaFileFlag, "file", "", "Path to schema JSON file (required)")
	schemasInstallCmd.Flags().StringVar(&schemaFileFlag, "file", "", "Create schema from JSON file and install in one step")
	schemasInstallCmd.Flags().BoolVar(&schemaDryRunFlag, "dry-run", false, "Preview what would be installed without making changes")
	schemasInstallCmd.Flags().BoolVar(&schemaMergeFlag, "merge", false, "Additively merge incoming type schemas into existing registered types")

	// Assemble
	schemasCmd.AddCommand(schemasListCmd)
	schemasCmd.AddCommand(schemasInstalledCmd)
	schemasCmd.AddCommand(schemasGetCmd)
	schemasCmd.AddCommand(schemasCreateCmd)
	schemasCmd.AddCommand(schemasInstallCmd)
	schemasCmd.AddCommand(schemasUninstallCmd)
	schemasCmd.AddCommand(schemasDeleteCmd)
	schemasCmd.AddCommand(schemasCompiledTypesCmd)

	rootCmd.AddCommand(schemasCmd)
}
