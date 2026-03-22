package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
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

	// bulk uninstall flags
	schemaAllExceptFlag  string
	schemaKeepLatestFlag bool

	// compiled-types flags
	schemaVerboseFlag bool

	// install migration flags
	schemaInstallForceFlag         bool
	schemaInstallAutoUninstallFlag bool
)

// ─────────────────────────────────────────────
// Helper: load schema file (JSON or YAML)
// ─────────────────────────────────────────────

// loadSchemaFile reads a schema definition file and returns its content as JSON bytes.
// It accepts .json, .yaml, and .yml files; YAML is converted to JSON transparently.
func loadSchemaFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return data, nil
	case ".yaml", ".yml":
		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("failed to parse YAML file %s: %w", path, err)
		}
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
		}
		return jsonBytes, nil
	default:
		return nil, fmt.Errorf("unsupported file format: must be .json, .yaml, or .yml")
	}
}

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
	Short: "Create a schema from a JSON or YAML file",
	Long:  "Create a new schema by loading its definition from a JSON or YAML file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaFileFlag == "" {
			return fmt.Errorf("--file is required")
		}

		data, err := loadSchemaFile(schemaFileFlag)
		if err != nil {
			return err
		}

		var req sdkschemas.CreatePackRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("failed to parse schema file: %w", err)
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
  install <schema-id>          Install an existing schema from the registry by ID.
  install --file schema.json   Create a new schema from a JSON or YAML file and install it in one step.`,
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
			data, err := loadSchemaFile(schemaFileFlag)
			if err != nil {
				return err
			}

			var req sdkschemas.CreatePackRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return fmt.Errorf("failed to parse schema file: %w", err)
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
			SchemaID:      schemaID,
			DryRun:        schemaDryRunFlag,
			Merge:         schemaMergeFlag,
			Force:         schemaInstallForceFlag,
			AutoUninstall: schemaInstallAutoUninstallFlag,
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

		// Report migration status if present
		if result.MigrationStatus != "" {
			fmt.Fprintf(out, "\nMigration status: %s\n", result.MigrationStatus)
		}
		if result.MigrationBlockReason != "" {
			fmt.Fprintf(out, "Block reason:     %s\n", result.MigrationBlockReason)
		}
		if result.MigrationJobID != nil {
			fmt.Fprintf(out, "Migration job ID: %s\n", *result.MigrationJobID)
			// Auto-poll if running in an interactive TTY
			if term.IsTerminal(int(os.Stdout.Fd())) {
				fmt.Fprintf(out, "\nWaiting for migration job to complete...\n")
				if err := pollMigrationJob(cmd.Context(), tp, *result.MigrationJobID, out, schemaOutputFlag); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: migration job polling failed: %v\n", err)
				}
			} else {
				fmt.Fprintf(out, "Run 'memory schemas migrate job --job-id %s --wait' to track progress.\n", *result.MigrationJobID)
			}
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// schemas uninstall <assignment-id>
// ─────────────────────────────────────────────

var schemasUninstallCmd = &cobra.Command{
	Use:   "uninstall [<assignment-id>]",
	Short: "Uninstall (remove) a schema assignment from the current project",
	Long: `Remove a schema assignment from the current project.

Single mode: provide the assignment ID to remove one schema.
Bulk mode: use --all-except or --keep-latest to remove multiple schemas.

Use 'memory schemas installed' to list assignment IDs.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate flag/arg combinations
		hasSingle := len(args) == 1
		hasAllExcept := cmd.Flags().Changed("all-except")
		hasKeepLatest := cmd.Flags().Changed("keep-latest")

		if !hasSingle && !hasAllExcept && !hasKeepLatest {
			return fmt.Errorf("provide an assignment ID, --all-except, or --keep-latest")
		}
		if hasSingle && (hasAllExcept || hasKeepLatest) {
			return fmt.Errorf("cannot combine a positional assignment ID with --all-except or --keep-latest")
		}
		if hasAllExcept && hasKeepLatest {
			return fmt.Errorf("--all-except and --keep-latest are mutually exclusive")
		}

		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		// Single-assignment mode
		if hasSingle {
			if schemaDryRunFlag {
				fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] Would remove schema assignment %s\n", args[0])
				return nil
			}
			if err := tp.DeleteAssignment(context.Background(), args[0]); err != nil {
				return fmt.Errorf("failed to uninstall schema: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Schema assignment %s removed.\n", args[0])
			return nil
		}

		// Bulk mode — fetch installed schemas
		installed, err := tp.GetInstalledPacks(context.Background())
		if err != nil {
			return fmt.Errorf("failed to list installed schemas: %w", err)
		}

		// Build the set of assignment IDs to remove
		var toRemove []sdkschemas.InstalledSchemaItem

		if hasAllExcept {
			// Build exclusion set from comma-separated IDs
			keepSet := map[string]bool{}
			for _, id := range strings.Split(schemaAllExceptFlag, ",") {
				id = strings.TrimSpace(id)
				if id != "" {
					keepSet[id] = true
				}
			}
			for _, p := range installed {
				if !keepSet[p.ID] && !keepSet[p.SchemaID] {
					toRemove = append(toRemove, p)
				}
			}
		} else if hasKeepLatest {
			// Keep the one most-recently installed per unique SchemaID; remove the rest
			// Sort by InstalledAt descending to find latest
			latest := map[string]string{} // schemaID → assignment ID of the latest
			sorted := make([]sdkschemas.InstalledSchemaItem, len(installed))
			copy(sorted, installed)
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].InstalledAt.After(sorted[j].InstalledAt)
			})
			for _, p := range sorted {
				if _, seen := latest[p.SchemaID]; !seen {
					latest[p.SchemaID] = p.ID
				}
			}
			keepSet := map[string]bool{}
			for _, id := range latest {
				keepSet[id] = true
			}
			for _, p := range installed {
				if !keepSet[p.ID] {
					toRemove = append(toRemove, p)
				}
			}
		}

		out := cmd.OutOrStdout()
		if len(toRemove) == 0 {
			fmt.Fprintln(out, "No schema assignments to remove.")
			return nil
		}

		for _, p := range toRemove {
			if schemaDryRunFlag {
				fmt.Fprintf(out, "[dry-run] Would remove %s (%s v%s)\n", p.ID, p.Name, p.Version)
				continue
			}
			if err := tp.DeleteAssignment(context.Background(), p.ID); err != nil {
				fmt.Fprintf(out, "  ERROR removing %s (%s): %v\n", p.ID, p.Name, err)
				continue
			}
			fmt.Fprintf(out, "Removed %s (%s v%s)\n", p.ID, p.Name, p.Version)
		}
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
--output json to receive the raw compiled types as JSON.

With --verbose, additional columns show the schema version and whether a type
is shadowed (overridden) by a higher-priority schema. Shadowed types also emit
a warning to stderr.`,
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

		verbose := schemaVerboseFlag

		// Print shadowed warnings to stderr when verbose
		if verbose {
			for _, t := range types.ObjectTypes {
				if t.Shadowed {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: object type %q from schema %s is shadowed by a higher-priority schema\n", t.Name, t.SchemaName)
				}
			}
			for _, t := range types.RelationshipTypes {
				if t.Shadowed {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: relationship type %q from schema %s is shadowed by a higher-priority schema\n", t.Name, t.SchemaName)
				}
			}
		}

		fmt.Fprintf(out, "Object Types (%d):\n", len(types.ObjectTypes))
		if len(types.ObjectTypes) == 0 {
			fmt.Fprintln(out, "  (none)")
		} else {
			table := tablewriter.NewWriter(out)
			if verbose {
				table.Header("Name", "Label", "Schema", "Version", "Shadowed", "Description")
			} else {
				table.Header("Name", "Label", "Schema", "Description")
			}
			for _, t := range types.ObjectTypes {
				desc := t.Description
				if len(desc) > 50 {
					desc = desc[:49] + "…"
				}
				if verbose {
					shadowed := ""
					if t.Shadowed {
						shadowed = "yes"
					}
					_ = table.Append(t.Name, t.Label, t.SchemaName, t.SchemaVersion, shadowed, desc)
				} else {
					_ = table.Append(t.Name, t.Label, t.SchemaName, desc)
				}
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
			if verbose {
				table.Header("Name", "Label", "Source → Target", "Schema", "Version", "Shadowed")
			} else {
				table.Header("Name", "Label", "Source → Target", "Schema")
			}
			for _, t := range types.RelationshipTypes {
				srcDst := t.SourceType + " → " + t.TargetType
				if verbose {
					shadowed := ""
					if t.Shadowed {
						shadowed = "yes"
					}
					_ = table.Append(t.Name, t.Label, srcDst, t.SchemaName, t.SchemaVersion, shadowed)
				} else {
					_ = table.Append(t.Name, t.Label, srcDst, t.SchemaName)
				}
			}
			if err := table.Render(); err != nil {
				return err
			}
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// schemas diff <schema-id> --file <path>
// ─────────────────────────────────────────────

// schemaDiffResult is the JSON-serialisable diff output.
type schemaDiffResult struct {
	SchemaID       string             `json:"schemaId"`
	AddedObjects   []string           `json:"addedObjectTypes"`
	RemovedObjects []string           `json:"removedObjectTypes"`
	AddedRels      []string           `json:"addedRelationshipTypes"`
	RemovedRels    []string           `json:"removedRelationshipTypes"`
	PropertyDiffs  []typePropertyDiff `json:"propertyDiffs,omitempty"`
}

// typePropertyDiff captures property-level changes for one type.
type typePropertyDiff struct {
	TypeName    string           `json:"typeName"`
	Added       []string         `json:"added,omitempty"`
	Removed     []string         `json:"removed,omitempty"`
	TypeChanged []propTypeChange `json:"typeChanged,omitempty"`
}

// propTypeChange describes a property whose type changed between versions.
type propTypeChange struct {
	Property string `json:"property"`
	OldType  string `json:"oldType"`
	NewType  string `json:"newType"`
}

var schemasDiffCmd = &cobra.Command{
	Use:   "diff <schema-id> --file <path>",
	Short: "Diff a local schema file against the currently installed version",
	Long: `Compare a local schema definition file against the version already stored in the
registry. Shows which object and relationship types would be added or removed.

Requires --file pointing to a JSON, YAML, or YML schema file.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaFileFlag == "" {
			return fmt.Errorf("--file is required")
		}

		schemaID := args[0]

		// Fetch the installed (current) schema
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		existing, err := c.SDK.Schemas.GetPack(context.Background(), schemaID)
		if err != nil {
			return fmt.Errorf("failed to fetch schema %s: %w", schemaID, err)
		}

		// Load the local (incoming) schema
		data, err := loadSchemaFile(schemaFileFlag)
		if err != nil {
			return err
		}

		// Parse both schemas into type name sets
		existingObjNames := extractObjectTypeNames(existing.ObjectTypeSchemas)
		existingRelNames := extractRelationshipTypeNames(existing.RelationshipTypeSchemas)

		var incomingRaw struct {
			ObjectTypeSchemas      json.RawMessage `json:"object_type_schemas"`
			ObjectTypeSchemasCamel json.RawMessage `json:"objectTypeSchemas"`
			RelTypeSchemas         json.RawMessage `json:"relationship_type_schemas"`
			RelTypeSchemasCamel    json.RawMessage `json:"relationshipTypeSchemas"`
		}
		if err := json.Unmarshal(data, &incomingRaw); err != nil {
			return fmt.Errorf("failed to parse schema file: %w", err)
		}
		objData := incomingRaw.ObjectTypeSchemas
		if len(objData) == 0 {
			objData = incomingRaw.ObjectTypeSchemasCamel
		}
		relData := incomingRaw.RelTypeSchemas
		if len(relData) == 0 {
			relData = incomingRaw.RelTypeSchemasCamel
		}

		incomingObjNames := extractObjectTypeNames(objData)
		incomingRelNames := extractRelationshipTypeNames(relData)

		result := schemaDiffResult{SchemaID: schemaID}

		// Compute added/removed object types
		for name := range incomingObjNames {
			if !existingObjNames[name] {
				result.AddedObjects = append(result.AddedObjects, name)
			}
		}
		for name := range existingObjNames {
			if !incomingObjNames[name] {
				result.RemovedObjects = append(result.RemovedObjects, name)
			}
		}
		// Compute added/removed relationship types
		for name := range incomingRelNames {
			if !existingRelNames[name] {
				result.AddedRels = append(result.AddedRels, name)
			}
		}
		for name := range existingRelNames {
			if !incomingRelNames[name] {
				result.RemovedRels = append(result.RemovedRels, name)
			}
		}

		sort.Strings(result.AddedObjects)
		sort.Strings(result.RemovedObjects)
		sort.Strings(result.AddedRels)
		sort.Strings(result.RemovedRels)

		// Compute property-level diffs for shared object types
		existingProps := extractObjectTypeProperties(existing.ObjectTypeSchemas)
		incomingProps := extractObjectTypeProperties(objData)

		var sharedTypes []string
		for name := range existingObjNames {
			if incomingObjNames[name] {
				sharedTypes = append(sharedTypes, name)
			}
		}
		sort.Strings(sharedTypes)

		for _, typeName := range sharedTypes {
			existP := existingProps[typeName]
			incomP := incomingProps[typeName]
			var diff typePropertyDiff
			diff.TypeName = typeName

			for prop, pType := range incomP {
				if oldType, exists := existP[prop]; !exists {
					diff.Added = append(diff.Added, prop+" ("+pType+")")
				} else if oldType != pType && pType != "" && oldType != "" {
					diff.TypeChanged = append(diff.TypeChanged, propTypeChange{
						Property: prop,
						OldType:  oldType,
						NewType:  pType,
					})
				}
			}
			for prop := range existP {
				if _, exists := incomP[prop]; !exists {
					diff.Removed = append(diff.Removed, prop)
				}
			}

			sort.Strings(diff.Added)
			sort.Strings(diff.Removed)
			sort.Slice(diff.TypeChanged, func(i, j int) bool {
				return diff.TypeChanged[i].Property < diff.TypeChanged[j].Property
			})

			if len(diff.Added) > 0 || len(diff.Removed) > 0 || len(diff.TypeChanged) > 0 {
				result.PropertyDiffs = append(result.PropertyDiffs, diff)
			}
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		// Human-readable output
		fmt.Fprintf(out, "Schema diff for %s\n\n", schemaID)

		fmt.Fprintf(out, "Object Types:\n")
		if len(result.AddedObjects) == 0 && len(result.RemovedObjects) == 0 {
			fmt.Fprintln(out, "  (no changes)")
		}
		for _, n := range result.AddedObjects {
			fmt.Fprintf(out, "  + %s\n", n)
		}
		for _, n := range result.RemovedObjects {
			fmt.Fprintf(out, "  - %s\n", n)
		}

		fmt.Fprintf(out, "\nRelationship Types:\n")
		if len(result.AddedRels) == 0 && len(result.RemovedRels) == 0 {
			fmt.Fprintln(out, "  (no changes)")
		}
		for _, n := range result.AddedRels {
			fmt.Fprintf(out, "  + %s\n", n)
		}
		for _, n := range result.RemovedRels {
			fmt.Fprintf(out, "  - %s\n", n)
		}

		// Property-level diffs for shared types
		if len(result.PropertyDiffs) > 0 {
			fmt.Fprintf(out, "\nProperty Changes:\n")
			for _, pd := range result.PropertyDiffs {
				var parts []string
				for _, a := range pd.Added {
					parts = append(parts, "+"+a)
				}
				for _, r := range pd.Removed {
					parts = append(parts, "-"+r)
				}
				for _, tc := range pd.TypeChanged {
					parts = append(parts, "~"+tc.Property+" ("+tc.OldType+"→"+tc.NewType+")")
				}
				fmt.Fprintf(out, "  [%s] %s\n", pd.TypeName, strings.Join(parts, ", "))
			}
		}

		// Suggested migrations YAML block
		hasMigrationHints := len(result.RemovedObjects) > 0 || len(result.PropertyDiffs) > 0
		if hasMigrationHints {
			fmt.Fprintf(out, "\nSuggested migrations block (paste into your schema file):\n")
			fmt.Fprintf(out, "  migrations:\n")
			fmt.Fprintf(out, "    from_version: \"<current-version>\"\n")

			// type_renames: user must fill in — we can't detect renames automatically
			if len(result.RemovedObjects) > 0 && len(result.AddedObjects) > 0 {
				fmt.Fprintf(out, "    # type_renames: (fill in if any types were renamed)\n")
				fmt.Fprintf(out, "    #   OldName: NewName\n")
			}

			// property_renames: user must fill in
			for _, pd := range result.PropertyDiffs {
				if len(pd.Removed) > 0 && len(pd.Added) > 0 {
					fmt.Fprintf(out, "    # property_renames for [%s]: (fill in if any properties were renamed)\n", pd.TypeName)
				}
			}

			// removed_properties: we can auto-populate from removed props
			var hasRemovedProps bool
			for _, pd := range result.PropertyDiffs {
				if len(pd.Removed) > 0 {
					hasRemovedProps = true
					break
				}
			}
			if hasRemovedProps {
				fmt.Fprintf(out, "    removed_properties:\n")
				for _, pd := range result.PropertyDiffs {
					for _, prop := range pd.Removed {
						fmt.Fprintf(out, "      - type_name: %s\n", pd.TypeName)
						fmt.Fprintf(out, "        name: %s\n", prop)
					}
				}
			}
		}

		return nil
	},
}

// extractObjectTypeNames returns a set of type names from objectTypeSchemas JSON.
func extractObjectTypeNames(data json.RawMessage) map[string]bool {
	set := map[string]bool{}
	if len(data) == 0 {
		return set
	}
	// Array format
	var arr []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &arr); err == nil {
		for _, item := range arr {
			if item.Name != "" {
				set[item.Name] = true
			}
		}
		return set
	}
	// Map format
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err == nil {
		for name := range m {
			set[name] = true
		}
	}
	return set
}

// extractRelationshipTypeNames returns a set of type names from relationshipTypeSchemas JSON.
func extractRelationshipTypeNames(data json.RawMessage) map[string]bool {
	set := map[string]bool{}
	if len(data) == 0 {
		return set
	}
	// Array format
	var arr []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		for _, item := range arr {
			if item.Name != "" {
				set[item.Name] = true
			}
		}
		return set
	}
	// Map format
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err == nil {
		for name := range m {
			set[name] = true
		}
	}
	return set
}

// objectTypePropertyMap maps type name → (property name → property type string).
// Supports both array format [{name, properties: {key: {type:...}}}] and map format.
func extractObjectTypeProperties(data json.RawMessage) map[string]map[string]string {
	result := map[string]map[string]string{}
	if len(data) == 0 {
		return result
	}

	type propDef struct {
		Type string `json:"type"`
	}
	type typeDef struct {
		Name       string             `json:"name"`
		Properties map[string]propDef `json:"properties"`
	}

	// Try array format
	var arr []typeDef
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		for _, t := range arr {
			if t.Name == "" {
				continue
			}
			props := map[string]string{}
			for propName, propDef := range t.Properties {
				props[propName] = propDef.Type
			}
			result[t.Name] = props
		}
		return result
	}

	// Try map format: {TypeName: {properties: {key: {type:...}}}}
	var m map[string]struct {
		Properties map[string]propDef `json:"properties"`
	}
	if err := json.Unmarshal(data, &m); err == nil {
		for typeName, typeDef := range m {
			props := map[string]string{}
			for propName, propDef := range typeDef.Properties {
				props[propName] = propDef.Type
			}
			result[typeName] = props
		}
	}
	return result
}

// ─────────────────────────────────────────────
// schemas history
// ─────────────────────────────────────────────

var schemasHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show installation history for schemas on the current project",
	Long: `Show the full installation history for schemas assigned to the current project,
including schemas that have since been removed (soft-deleted).

Output is a table with columns: Assignment ID, Schema ID, Name, Version,
Installed, Removed. Use --output json to receive the full list as JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		history, err := tp.GetPackHistory(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get schema history: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(history)
		}

		if len(history) == 0 {
			fmt.Fprintln(out, "No schema history found.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("Assignment ID", "Schema ID", "Name", "Version", "Installed", "Removed")
		for _, h := range history {
			removed := ""
			if h.RemovedAt != nil {
				removed = h.RemovedAt.Format("2006-01-02")
			}
			_ = table.Append(
				h.ID,
				h.SchemaID,
				h.Name,
				h.Version,
				h.InstalledAt.Format("2006-01-02"),
				removed,
			)
		}
		return table.Render()
	},
}

// ─────────────────────────────────────────────
// schemas migrate
// ─────────────────────────────────────────────

var (
	schemaMigrateRenameTypeFlag     []string
	schemaMigrateRenamePropertyFlag []string
)

var schemasMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate live graph data by renaming types or properties",
	Long: `Rename object types or properties across live graph objects and edges.

Use --rename-type OldName:NewName to rename an object or edge type.
Use --rename-property OldType.old_key:OldType.new_key to rename a property within a type.
Both flags are repeatable. At least one rename must be provided.

Use --dry-run to preview how many objects/edges would be affected without making changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(schemaMigrateRenameTypeFlag) == 0 && len(schemaMigrateRenamePropertyFlag) == 0 {
			return fmt.Errorf("at least one --rename-type or --rename-property is required")
		}

		// Parse --rename-type flags
		type renameTypePair struct{ From, To string }
		var typeRenames []renameTypePair
		for _, r := range schemaMigrateRenameTypeFlag {
			parts := strings.SplitN(r, ":", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("invalid --rename-type value %q: expected OldName:NewName", r)
			}
			typeRenames = append(typeRenames, renameTypePair{parts[0], parts[1]})
		}

		// Parse --rename-property flags
		type renamePropPair struct{ TypeName, FromProp, ToProp string }
		var propRenames []renamePropPair
		for _, r := range schemaMigrateRenamePropertyFlag {
			// Format: OldType.old_key:OldType.new_key
			halves := strings.SplitN(r, ":", 2)
			if len(halves) != 2 {
				return fmt.Errorf("invalid --rename-property value %q: expected OldType.old_key:OldType.new_key", r)
			}
			fromParts := strings.SplitN(halves[0], ".", 2)
			toParts := strings.SplitN(halves[1], ".", 2)
			if len(fromParts) != 2 || len(toParts) != 2 {
				return fmt.Errorf("invalid --rename-property value %q: expected OldType.old_key:OldType.new_key", r)
			}
			if fromParts[0] != toParts[0] {
				return fmt.Errorf("--rename-property type name mismatch in %q: both sides must reference the same type", r)
			}
			propRenames = append(propRenames, renamePropPair{fromParts[0], fromParts[1], toParts[1]})
		}

		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		// Build SDK request
		req := &sdkschemas.MigrateRequest{DryRun: schemaDryRunFlag}
		for _, tr := range typeRenames {
			req.TypeRenames = append(req.TypeRenames, sdkschemas.TypeRename{From: tr.From, To: tr.To})
		}
		for _, pr := range propRenames {
			req.PropertyRenames = append(req.PropertyRenames, sdkschemas.PropertyRename{
				TypeName: pr.TypeName,
				From:     pr.FromProp,
				To:       pr.ToProp,
			})
		}

		result, err := tp.MigratePack(context.Background(), req)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		prefix := ""
		if schemaDryRunFlag {
			prefix = "[dry-run] "
		}

		fmt.Fprintf(out, "%sMigration results:\n", prefix)
		for _, r := range result.TypeRenameResults {
			warn := ""
			if r.ObjectsAffected == 0 && r.EdgesAffected == 0 {
				warn = " (WARNING: 0 objects/edges affected — check the type name)"
			}
			fmt.Fprintf(out, "  %s → %s: %d object(s), %d edge(s) updated%s\n",
				r.From, r.To, r.ObjectsAffected, r.EdgesAffected, warn)
		}
		for _, r := range result.PropertyRenameResults {
			warn := ""
			if r.ObjectsAffected == 0 {
				warn = " (WARNING: 0 objects affected — check the type/property name)"
			}
			fmt.Fprintf(out, "  %s.%s → %s.%s: %d object(s) updated%s\n",
				r.TypeName, r.From, r.TypeName, r.To, r.ObjectsAffected, warn)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// schemas migrate preview
// ─────────────────────────────────────────────

var (
	schemaMigratePreviewFromFlag string
	schemaMigratePreviewToFlag   string
)

var schemasMigratePreviewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Preview a schema migration (risk assessment, no data changes)",
	Long: `Preview the risk of migrating live graph objects from one schema version to another.

Returns a risk breakdown per object type (safe/cautious/risky/dangerous) and a
total object count, but makes no data changes.

Use --from and --to to specify the source and target schema IDs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaMigratePreviewFromFlag == "" {
			return fmt.Errorf("--from is required")
		}
		if schemaMigratePreviewToFlag == "" {
			return fmt.Errorf("--to is required")
		}

		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		result, err := tp.PreviewMigration(context.Background(), &sdkschemas.SchemaMigrationPreviewRequest{
			FromSchemaID: schemaMigratePreviewFromFlag,
			ToSchemaID:   schemaMigratePreviewToFlag,
		})
		if err != nil {
			return fmt.Errorf("preview failed: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		fmt.Fprintf(out, "Migration preview: %s → %s\n", result.FromSchemaID, result.ToSchemaID)
		fmt.Fprintf(out, "Total objects:     %d\n", result.TotalObjects)
		fmt.Fprintf(out, "Overall risk:      %s\n\n", result.OverallRiskLevel)

		if len(result.TypeBreakdown) > 0 {
			table := tablewriter.NewWriter(out)
			table.Header("Type", "Risk", "Objects", "Dropped Fields", "Block Reason")
			for _, t := range result.TypeBreakdown {
				_ = table.Append(t.TypeName, t.RiskLevel, fmt.Sprintf("%d", t.ObjectCount),
					fmt.Sprintf("%d", t.DroppedFields), t.BlockReason)
			}
			if err := table.Render(); err != nil {
				return err
			}
		}

		// Suggest a migrations YAML block based on the from/to schema IDs
		fmt.Fprintf(out, "\nSuggested migrations block:\n")
		fmt.Fprintf(out, "  migrations:\n")
		fmt.Fprintf(out, "    from_version: \"<from-schema-version>\"\n")
		fmt.Fprintf(out, "    # Add type_renames, property_renames, removed_properties as needed\n")
		fmt.Fprintf(out, "    # Run 'memory schemas diff <id> --file schema.yaml' for property-level diff\n")

		return nil
	},
}

// ─────────────────────────────────────────────
// schemas migrate execute
// ─────────────────────────────────────────────

var (
	schemaMigrateExecuteFromFlag       string
	schemaMigrateExecuteToFlag         string
	schemaMigrateExecuteForceFlag      bool
	schemaMigrateExecuteMaxObjectsFlag int
)

var schemasMigrateExecuteCmd = &cobra.Command{
	Use:   "execute",
	Short: "Execute a schema migration (System A: full field-level migration with archive)",
	Long: `Migrate live graph objects from one schema version to another using System A
(SchemaMigrator). Applies type renames, property renames, and archives dropped
properties for rollback.

Use --force to bypass the dangerous-risk block. Use --max-objects to limit how
many objects are migrated (useful for staged rollouts).

A confirmation prompt is shown for risky or dangerous migrations unless --force
is set.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaMigrateExecuteFromFlag == "" {
			return fmt.Errorf("--from is required")
		}
		if schemaMigrateExecuteToFlag == "" {
			return fmt.Errorf("--to is required")
		}

		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()

		// Run preview first to check risk, unless force is set
		if !schemaMigrateExecuteForceFlag {
			preview, err := tp.PreviewMigration(context.Background(), &sdkschemas.SchemaMigrationPreviewRequest{
				FromSchemaID: schemaMigrateExecuteFromFlag,
				ToSchemaID:   schemaMigrateExecuteToFlag,
			})
			if err != nil {
				return fmt.Errorf("preview failed: %w", err)
			}

			risk := preview.OverallRiskLevel
			if risk == "risky" || risk == "dangerous" {
				fmt.Fprintf(out, "Overall risk: %s (%d total objects)\n", risk, preview.TotalObjects)
				if term.IsTerminal(int(os.Stdout.Fd())) {
					fmt.Fprintf(out, "Type 'yes' to confirm: ")
					var confirm string
					if _, err := fmt.Fscan(cmd.InOrStdin(), &confirm); err != nil || confirm != "yes" {
						return fmt.Errorf("migration cancelled")
					}
				} else {
					return fmt.Errorf("risk level is %q — rerun with --force to proceed non-interactively", risk)
				}
			}
		}

		result, err := tp.ExecuteMigration(context.Background(), &sdkschemas.SchemaMigrationExecuteRequest{
			FromSchemaID: schemaMigrateExecuteFromFlag,
			ToSchemaID:   schemaMigrateExecuteToFlag,
			Force:        schemaMigrateExecuteForceFlag,
			MaxObjects:   schemaMigrateExecuteMaxObjectsFlag,
		})
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		fmt.Fprintf(out, "Migration complete.\n")
		fmt.Fprintf(out, "  Objects migrated: %d\n", result.ObjectsMigrated)
		if result.ObjectsFailed > 0 {
			fmt.Fprintf(out, "  Objects failed:   %d\n", result.ObjectsFailed)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// schemas migrate rollback
// ─────────────────────────────────────────────

var (
	schemaMigrateRollbackToVersionFlag       string
	schemaMigrateRollbackRestoreRegistryFlag bool
)

var schemasMigrateRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back a schema migration by restoring archived property data",
	Long: `Restore property data archived during a previous migration.

Use --to-version to specify which schema version to roll back to.
Use --restore-registry to also restore the type registry to the pre-migration state
(re-installs the old schema types and removes new additions). This is a transactional
operation — if any step fails, the entire rollback is reverted.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaMigrateRollbackToVersionFlag == "" {
			return fmt.Errorf("--to-version is required")
		}

		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		result, err := tp.RollbackMigration(context.Background(), &sdkschemas.SchemaMigrationRollbackRequest{
			ToVersion:           schemaMigrateRollbackToVersionFlag,
			RestoreTypeRegistry: schemaMigrateRollbackRestoreRegistryFlag,
		})
		if err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		fmt.Fprintf(out, "Rollback complete.\n")
		fmt.Fprintf(out, "  Rolled back to:   %s\n", result.ToVersion)
		fmt.Fprintf(out, "  Objects restored: %d\n", result.ObjectsRestored)
		return nil
	},
}

// ─────────────────────────────────────────────
// schemas migrate commit
// ─────────────────────────────────────────────

var schemaMigrateCommitThroughVersionFlag string

var schemasMigrateCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit (prune) migration archive data through a given schema version",
	Long: `Remove migration_archive entries from all project objects whose to_version is
<= the given through_version. Once committed, rollback to those versions is no
longer possible.

This is an explicit user action — run after a migration has been stable for
some time. The assignment itself is unaffected.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaMigrateCommitThroughVersionFlag == "" {
			return fmt.Errorf("--through-version is required")
		}

		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		result, err := tp.CommitMigrationArchive(context.Background(), &sdkschemas.CommitMigrationArchiveRequest{
			ThroughVersion: schemaMigrateCommitThroughVersionFlag,
		})
		if err != nil {
			return fmt.Errorf("commit failed: %w", err)
		}

		out := cmd.OutOrStdout()

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		fmt.Fprintf(out, "Migration archive committed.\n")
		fmt.Fprintf(out, "  Through version: %s\n", result.ThroughVersion)
		fmt.Fprintf(out, "  Objects updated: %d\n", result.ObjectsUpdated)
		return nil
	},
}

// ─────────────────────────────────────────────
// schemas migrate job
// ─────────────────────────────────────────────

var (
	schemaMigrateJobIDFlag   string
	schemaMigrateJobWaitFlag bool
)

var schemasMigrateJobCmd = &cobra.Command{
	Use:   "job",
	Short: "Check the status of an async schema migration job",
	Long: `Poll the status of a background schema migration job.

Use --job-id to specify the job ID (returned by 'memory schemas install' when
auto-migration is triggered).

Use --wait to block until the job completes, streaming progress updates every
2 seconds.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if schemaMigrateJobIDFlag == "" {
			return fmt.Errorf("--job-id is required")
		}

		tp, err := getSchemasClient(cmd)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()

		if schemaMigrateJobWaitFlag {
			return pollMigrationJob(cmd.Context(), tp, schemaMigrateJobIDFlag, out, schemaOutputFlag)
		}

		job, err := tp.GetMigrationJobStatus(cmd.Context(), schemaMigrateJobIDFlag)
		if err != nil {
			return fmt.Errorf("failed to get job status: %w", err)
		}

		if schemaOutputFlag == "json" {
			return json.NewEncoder(out).Encode(job)
		}

		printMigrationJob(out, job)
		return nil
	},
}

// pollMigrationJob polls the migration job until it reaches a terminal state.
func pollMigrationJob(ctx context.Context, tp *sdkschemas.Client, jobID string, out io.Writer, outputFormat string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	for {
		job, err := tp.GetMigrationJobStatus(ctx, jobID)
		if err != nil {
			return fmt.Errorf("failed to get job status: %w", err)
		}

		if outputFormat == "json" {
			_ = json.NewEncoder(out).Encode(job)
		} else if job.Status != lastStatus {
			lastStatus = job.Status
			fmt.Fprintf(out, "  [job %s] status=%s migrated=%d failed=%d\n",
				job.ID[:8], job.Status, job.ObjectsMigrated, job.ObjectsFailed)
		}

		if job.Status == "completed" || job.Status == "failed" {
			if outputFormat != "json" {
				printMigrationJob(out, job)
			}
			if job.Status == "failed" {
				errMsg := ""
				if job.Error != nil {
					errMsg = *job.Error
				}
				return fmt.Errorf("migration job failed: %s", errMsg)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// printMigrationJob prints a human-readable summary of a migration job.
func printMigrationJob(out io.Writer, job *sdkschemas.SchemaMigrationJob) {
	fmt.Fprintf(out, "Job ID:          %s\n", job.ID)
	fmt.Fprintf(out, "Status:          %s\n", job.Status)
	fmt.Fprintf(out, "From schema:     %s\n", job.FromSchemaID)
	fmt.Fprintf(out, "To schema:       %s\n", job.ToSchemaID)
	fmt.Fprintf(out, "Objects migrated:%d\n", job.ObjectsMigrated)
	if job.ObjectsFailed > 0 {
		fmt.Fprintf(out, "Objects failed:  %d\n", job.ObjectsFailed)
	}
	if job.RiskLevel != "" {
		fmt.Fprintf(out, "Risk level:      %s\n", job.RiskLevel)
	}
	if job.Error != nil {
		fmt.Fprintf(out, "Error:           %s\n", *job.Error)
	}
	fmt.Fprintf(out, "Created:         %s\n", job.CreatedAt)
	if job.StartedAt != nil {
		fmt.Fprintf(out, "Started:         %s\n", *job.StartedAt)
	}
	if job.CompletedAt != nil {
		fmt.Fprintf(out, "Completed:       %s\n", *job.CompletedAt)
	}
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
	schemasInstallCmd.Flags().BoolVar(&schemaInstallForceFlag, "force", false, "Force migration even if risk level is dangerous")
	schemasInstallCmd.Flags().BoolVar(&schemaInstallAutoUninstallFlag, "auto-uninstall", false, "Uninstall the from-version schema after successful migration chain")

	// Uninstall flags (bulk + dry-run)
	schemasUninstallCmd.Flags().StringVar(&schemaAllExceptFlag, "all-except", "", "Remove all assignments except the comma-separated IDs (assignment IDs or schema IDs)")
	schemasUninstallCmd.Flags().BoolVar(&schemaKeepLatestFlag, "keep-latest", false, "Remove all but the most-recently installed assignment per unique schema")
	schemasUninstallCmd.Flags().BoolVar(&schemaDryRunFlag, "dry-run", false, "Preview what would be removed without making changes")

	// Diff flags
	schemasDiffCmd.Flags().StringVar(&schemaFileFlag, "file", "", "Path to incoming schema file (JSON, YAML, or YML)")

	// Compiled-types flags
	schemasCompiledTypesCmd.Flags().BoolVar(&schemaVerboseFlag, "verbose", false, "Include schema version and shadowed status in output")

	// Migrate (System B) flags
	schemasMigrateCmd.Flags().StringArrayVar(&schemaMigrateRenameTypeFlag, "rename-type", nil, "Rename a type: OldName:NewName (repeatable)")
	schemasMigrateCmd.Flags().StringArrayVar(&schemaMigrateRenamePropertyFlag, "rename-property", nil, "Rename a property: OldType.old_key:OldType.new_key (repeatable)")
	schemasMigrateCmd.Flags().BoolVar(&schemaDryRunFlag, "dry-run", false, "Preview migration without making changes")

	// Migrate preview flags (System A)
	schemasMigratePreviewCmd.Flags().StringVar(&schemaMigratePreviewFromFlag, "from", "", "Source schema ID (required)")
	schemasMigratePreviewCmd.Flags().StringVar(&schemaMigratePreviewToFlag, "to", "", "Target schema ID (required)")

	// Migrate execute flags (System A)
	schemasMigrateExecuteCmd.Flags().StringVar(&schemaMigrateExecuteFromFlag, "from", "", "Source schema ID (required)")
	schemasMigrateExecuteCmd.Flags().StringVar(&schemaMigrateExecuteToFlag, "to", "", "Target schema ID (required)")
	schemasMigrateExecuteCmd.Flags().BoolVar(&schemaMigrateExecuteForceFlag, "force", false, "Force migration even if risk level is dangerous")
	schemasMigrateExecuteCmd.Flags().IntVar(&schemaMigrateExecuteMaxObjectsFlag, "max-objects", 0, "Limit number of objects to migrate (0 = no limit)")

	// Migrate rollback flags
	schemasMigrateRollbackCmd.Flags().StringVar(&schemaMigrateRollbackToVersionFlag, "to-version", "", "Schema version to roll back to (required)")
	schemasMigrateRollbackCmd.Flags().BoolVar(&schemaMigrateRollbackRestoreRegistryFlag, "restore-registry", false, "Also restore type registry to the pre-migration state")

	// Migrate commit flags
	schemasMigrateCommitCmd.Flags().StringVar(&schemaMigrateCommitThroughVersionFlag, "through-version", "", "Prune archive entries at or below this version (required)")

	// Migrate job flags
	schemasMigrateJobCmd.Flags().StringVar(&schemaMigrateJobIDFlag, "job-id", "", "Migration job ID to poll (required)")
	schemasMigrateJobCmd.Flags().BoolVar(&schemaMigrateJobWaitFlag, "wait", false, "Block until job completes, streaming progress updates")

	// Wire System A subcommands under schemasMigrateCmd
	schemasMigrateCmd.AddCommand(schemasMigratePreviewCmd)
	schemasMigrateCmd.AddCommand(schemasMigrateExecuteCmd)
	schemasMigrateCmd.AddCommand(schemasMigrateRollbackCmd)
	schemasMigrateCmd.AddCommand(schemasMigrateCommitCmd)
	schemasMigrateCmd.AddCommand(schemasMigrateJobCmd)

	// Assemble
	schemasCmd.AddCommand(schemasListCmd)
	schemasCmd.AddCommand(schemasInstalledCmd)
	schemasCmd.AddCommand(schemasGetCmd)
	schemasCmd.AddCommand(schemasCreateCmd)
	schemasCmd.AddCommand(schemasInstallCmd)
	schemasCmd.AddCommand(schemasUninstallCmd)
	schemasCmd.AddCommand(schemasDeleteCmd)
	schemasCmd.AddCommand(schemasCompiledTypesCmd)
	schemasCmd.AddCommand(schemasDiffCmd)
	schemasCmd.AddCommand(schemasHistoryCmd)
	schemasCmd.AddCommand(schemasMigrateCmd)

	rootCmd.AddCommand(schemasCmd)
}
