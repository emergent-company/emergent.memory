package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
	internalui "github.com/emergent-company/emergent.memory/apps/cli/internal/ui"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/spf13/cobra"
)

// batchObjectItem is the JSON shape accepted by graph objects create-batch (flat-array format).
type batchObjectItem struct {
	Type        string         `json:"type"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

// subgraphObjectInput is the JSON shape for objects in the subgraph format.
type subgraphObjectInput struct {
	Ref         string         `json:"_ref"`
	Type        string         `json:"type"`
	Key         *string        `json:"key,omitempty"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

// subgraphRelationshipInput is the JSON shape for relationships in the subgraph format.
// src_ref/dst_ref reference objects defined in the same file; src_id/dst_id reference
// pre-existing objects by UUID. src_ref takes precedence over src_id; dst_ref over dst_id.
type subgraphRelationshipInput struct {
	Type       string         `json:"type"`
	SrcRef     string         `json:"src_ref,omitempty"`
	DstRef     string         `json:"dst_ref,omitempty"`
	SrcID      string         `json:"src_id,omitempty"`
	DstID      string         `json:"dst_id,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

// subgraphInput is the top-level JSON shape for the subgraph format.
type subgraphInput struct {
	Objects       []subgraphObjectInput       `json:"objects"`
	Relationships []subgraphRelationshipInput `json:"relationships"`
}

// batchRelationshipItem is the JSON shape accepted by graph relationships create-batch.
type batchRelationshipItem struct {
	Type       string         `json:"type"`
	From       string         `json:"from"`
	To         string         `json:"to"`
	Properties map[string]any `json:"properties,omitempty"`
}

// batchUpdateObjectItem is the JSON shape accepted by graph objects update-batch.
type batchUpdateObjectItem struct {
	ID            string         `json:"id"`
	Key           *string        `json:"key,omitempty"`
	Properties    map[string]any `json:"properties,omitempty"`
	Labels        []string       `json:"labels,omitempty"`
	ReplaceLabels *bool          `json:"replaceLabels,omitempty"`
	Status        *string        `json:"status,omitempty"`
	BranchID      *string        `json:"branch_id,omitempty"`
}

// nameFromProps returns the "name" property from a properties map, or "" if absent.
func nameFromProps(props map[string]any) string {
	if props == nil {
		return ""
	}
	if v, ok := props["name"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ─────────────────────────────────────────────
// Top-level and sub-group commands
// ─────────────────────────────────────────────

var graphCmd = &cobra.Command{
	Use:     "graph",
	Short:   "Manage graph objects and relationships",
	Long:    "Commands for managing graph objects and relationships in the Memory knowledge graph",
	GroupID: "knowledge",
}

var graphObjectsCmd = &cobra.Command{
	Use:   "objects",
	Short: "Manage graph objects",
}

var graphRelationshipsCmd = &cobra.Command{
	Use:   "relationships",
	Short: "Manage graph relationships",
}

// ─────────────────────────────────────────────
// Flag variables
// ─────────────────────────────────────────────

var (
	graphProjectFlag  string
	graphOutputFlag   string
	graphLimitFlag    int
	graphCursorFlag   string
	graphTypeFlag     string
	graphNameFlag     string
	graphDescFlag     string
	graphPropsFlag    string
	graphKeyFlag      string
	graphStatusFlag   string
	graphBranchFlag   string
	graphUpsertFlag   bool
	graphFromFlag     string
	graphToFlag       string
	graphRelTypeFlag  string
	graphBatchFile    string
	graphFilterFlag   []string
	graphFilterOpFlag string
	graphIDsFlag      string
	graphForceFlag    bool

	// actor provenance filter flags (graph objects list)
	graphActorTypeFlag  string
	graphActorIDFlag    string
	graphProvenanceFlag string

	// write-stamp filter flags (#2468): graph objects list and graph
	// relationships list
	graphRunIDFlag          string
	graphCredentialIDFlag   string
	graphCredentialKindFlag string
	graphDataClassFlag      string

	// bulk action flags
	graphBulkActionFlag string
	graphBulkValueFlag  string
	graphBulkLabelsFlag []string
	graphBulkDryRunFlag bool
	graphBulkLimitFlag  int

	graphMoveTargetBranchFlag string
)

// ─────────────────────────────────────────────
// Helper: resolve project + set context on client
// ─────────────────────────────────────────────

// validFilterOps is the set of operators the server accepts for property filters.
var validFilterOps = map[string]bool{
	"eq": true, "neq": true, "gt": true, "gte": true,
	"lt": true, "lte": true, "contains": true, "in": true, "exists": true,
}

// parsePropertyFilters converts repeatable --filter key=value pairs and a
// --filter-op operator into a slice of sdkgraph.PropertyFilter.
//
//   - Splits on the first '=' only, so values like "a=b=c" work correctly.
//   - "exists" operator: value portion is ignored (omitted from filter).
//   - "in" operator: value is split on commas into a []string.
//   - All other operators: value is passed as a plain string.
func parsePropertyFilters(filters []string, op string) ([]sdkgraph.PropertyFilter, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	if !validFilterOps[op] {
		return nil, fmt.Errorf("unsupported --filter-op %q: valid operators are eq, neq, gt, gte, lt, lte, contains, in, exists", op)
	}
	out := make([]sdkgraph.PropertyFilter, 0, len(filters))
	for _, f := range filters {
		idx := strings.Index(f, "=")
		if op != "exists" && idx < 0 {
			return nil, fmt.Errorf("invalid --filter %q: expected key=value format", f)
		}
		var key, val string
		if idx >= 0 {
			key = f[:idx]
			val = f[idx+1:]
		} else {
			key = f // "exists" operator with no value
		}
		pf := sdkgraph.PropertyFilter{Path: key, Op: op}
		switch op {
		case "exists":
			// no value
		case "in":
			pf.Value = strings.Split(val, ",")
		default:
			pf.Value = val
		}
		out = append(out, pf)
	}
	return out, nil
}

func getGraphClient(cmd *cobra.Command) (*sdkgraph.Client, error) {
	return getProjectScopedGraphClient(cmd, graphProjectFlag)
}

// parseProvenanceFlag validates --provenance and returns the resolved
// version-scope mode. It is shared by the actor filter (objects) and the
// write-stamp filters (objects and relationships).
//
// An empty flag sends nothing: the server then treats the actor filter as "any"
// and the write-stamp filters as "updated" (the current version). The narrowing
// modes are rejected eagerly when there is nothing to narrow, so a typo fails
// locally instead of round-tripping to the server's 400. `requirement` names
// what the invocation would need, because the relationship list has no actor
// flags to point at.
func parseProvenanceFlag(hasPredicate bool, requirement string) (string, error) {
	provenance := graphProvenanceFlag
	switch provenance {
	case "", "any", "created", "updated":
		// valid
	default:
		return "", fmt.Errorf("invalid --provenance %q: must be one of created, updated, any", provenance)
	}
	if (provenance == "created" || provenance == "updated") && !hasPredicate {
		return "", fmt.Errorf("--provenance %s requires %s", provenance, requirement)
	}
	return provenance, nil
}

// parseProvenanceFilter validates the actor provenance flags (--actor-type,
// --actor-id, --provenance) and returns the resolved (actorType, actorID,
// provenance) triple. --actor-id without --actor-type is rejected (mirroring the
// server's pair rule); hasStamp tells parseProvenanceFlag whether a write-stamp
// filter is present, because --provenance may narrow that instead of an actor.
func parseProvenanceFilter(hasStamp bool) (actorType, actorID, provenance string, err error) {
	actorType = graphActorTypeFlag
	actorID = graphActorIDFlag

	if actorID != "" && actorType == "" {
		return "", "", "", fmt.Errorf("--actor-id requires --actor-type")
	}

	provenance, err = parseProvenanceFlag(actorType != "" || actorID != "" || hasStamp,
		"--actor-type or a write-stamp filter")
	if err != nil {
		return "", "", "", err
	}

	return actorType, actorID, provenance, nil
}

// parseWriteStampFlags validates the write-stamp filter flags (#2468) before any
// network call, so a typo in an enum fails fast instead of round-tripping to the
// server's 400. allowDataClass is false for relationships, which record no data
// class (the flag is not registered there either).
func parseWriteStampFlags(allowDataClass bool) (runID, credentialID, credentialKind, dataClass string, err error) {
	credentialKind = graphCredentialKindFlag
	switch credentialKind {
	case "", "api_key", "mcp_link", "session", "none":
		// valid ("" sends no filter)
	default:
		return "", "", "", "", fmt.Errorf("invalid --credential-kind %q: must be one of api_key, mcp_link, session, none", credentialKind)
	}

	if allowDataClass {
		dataClass = graphDataClassFlag
		switch dataClass {
		case "", "source", "synthesized", "adopted":
			// valid
		default:
			return "", "", "", "", fmt.Errorf("invalid --data-class %q: must be one of source, synthesized, adopted", dataClass)
		}
	}

	return graphRunIDFlag, graphCredentialIDFlag, credentialKind, dataClass, nil
}

// getProjectScopedGraphClient is the generic helper used by getGraphClient
// and any other domain that needs a project-scoped Graph client.
func getProjectScopedGraphClient(cmd *cobra.Command, projectFlag string) (*sdkgraph.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, err
	}

	projectID, err := resolveProjectContext(cmd, projectFlag)
	if err != nil {
		return nil, err
	}

	c.SetContext("", projectID)
	return c.SDK.Graph, nil
}

// resolveGraphObjectArgOrPick resolves a graph object entity ID from args[0],
// or, when args is empty and stdin is a terminal, lists objects and shows an
// interactive picker. Returns the resolved entity ID and a display label.
// The returned ID is the stable EntityID (a UUID); when args[0] is a non-UUID
// key, callers perform key→ID resolution themselves (see graph objects get).
func resolveGraphObjectArgOrPick(cmd *cobra.Command, g *sdkgraph.Client, args []string) (id, name string, err error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], args[0], nil
	}

	if isNonInteractive() {
		return "", "", fmt.Errorf("object ID is required — pass one or run interactively to pick from a list")
	}

	opts := &sdkgraph.ListObjectsOptions{}
	if graphBranchFlag != "" {
		branchID, err := resolveBranchNameOrID(cmd, graphBranchFlag)
		if err != nil {
			return "", "", err
		}
		opts.BranchID = branchID
	}
	if graphTypeFlag != "" {
		opts.Type = graphTypeFlag
	}
	if graphLimitFlag > 0 {
		opts.Limit = graphLimitFlag
	}

	resp, err := g.ListObjects(context.Background(), opts)
	if err != nil {
		return "", "", fmt.Errorf("failed to list objects: %w", err)
	}
	if len(resp.Items) == 0 {
		return "", "", fmt.Errorf("no objects found")
	}

	items := make([]PickerItem, 0, len(resp.Items))
	for _, obj := range resp.Items {
		label := obj.EntityID
		if obj.Type != "" {
			label = obj.Type + "  " + label
		}
		if n := nameFromProps(obj.Properties); n != "" {
			label = label + "  " + n
		}
		if obj.Key != nil && *obj.Key != "" {
			label = label + "  (" + *obj.Key + ")"
		}
		items = append(items, PickerItem{ID: obj.EntityID, Name: label})
	}

	pickedID, pickedName, err := promptResourcePicker("Select an object", items)
	if err != nil {
		return "", "", err
	}
	if pickedID == "" {
		return "", "", fmt.Errorf("object ID is required")
	}
	return pickedID, pickedName, nil
}

// resolveGraphRelationshipArgOrPick resolves a graph relationship entity ID
// from args[0], or, when args is empty and stdin is a terminal, lists
// relationships and shows an interactive picker. Returns the resolved entity
// ID and a display label.
func resolveGraphRelationshipArgOrPick(cmd *cobra.Command, g *sdkgraph.Client, args []string) (id, name string, err error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], args[0], nil
	}

	if isNonInteractive() {
		return "", "", fmt.Errorf("relationship ID is required — pass one or run interactively to pick from a list")
	}

	opts := &sdkgraph.ListRelationshipsOptions{}
	if graphRelTypeFlag != "" {
		opts.Type = graphRelTypeFlag
	}
	if graphBranchFlag != "" {
		opts.BranchID = graphBranchFlag
	}
	if graphLimitFlag > 0 {
		opts.Limit = graphLimitFlag
	}

	resp, err := g.ListRelationships(context.Background(), opts)
	if err != nil {
		return "", "", fmt.Errorf("failed to list relationships: %w", err)
	}
	if len(resp.Items) == 0 {
		return "", "", fmt.Errorf("no relationships found")
	}

	items := make([]PickerItem, 0, len(resp.Items))
	for _, r := range resp.Items {
		label := r.EntityID
		if r.Type != "" {
			label = r.Type + "  " + label
		}
		if r.SrcID != "" && r.DstID != "" {
			label = label + "  " + r.SrcID + " → " + r.DstID
		}
		items = append(items, PickerItem{ID: r.EntityID, Name: label})
	}

	pickedID, pickedName, err := promptResourcePicker("Select a relationship", items)
	if err != nil {
		return "", "", err
	}
	if pickedID == "" {
		return "", "", fmt.Errorf("relationship ID is required")
	}
	return pickedID, pickedName, nil
}

// ─────────────────────────────────────────────
// graph objects list
// ─────────────────────────────────────────────

var graphObjectsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List graph objects",
	Long: `List graph objects (entities) in the current project.

Output is a table with columns: Entity ID, Type, Version, Status, and Created
date. Use --type to filter by object type, --limit to control result count, and
--output json to receive the full list as JSON.

Use --key to filter by the object's stable key field directly. This is the most
efficient way to look up a single object by key without fetching all objects.

Use --filter key=value to filter by object properties (repeatable). All filters
are combined with AND. The --filter-op flag sets the comparison operator for
every --filter in the same invocation (default: eq).

Use --run-id, --credential-id, --credential-kind and --data-class to filter by
the write stamp every version row records: which agent run produced the row
(a root run matches the runs it delegated to), which credential wrote it, and
what class of data it is. This answers "what did this session write" and "what
did this key write" directly.

By default the filters match the object's current version. Use --provenance to
choose which version row must carry the stamp: updated (default) matches the
current version, created matches the version that created the object, and any
matches either — so a run that created an object which someone later edited is
still found with --provenance created (or any). --provenance also applies to the
--actor-type/--actor-id filter, which shares the selector.

  --filter-op operators: eq, neq, gt, gte, lt, lte, contains, in, exists

Examples:
  memory graph objects list --key sq-soft-delete-employee
  memory graph objects list --branch <branch-id> --key ep-employees-delete
  memory graph objects list --filter status=active
  memory graph objects list --type Feature --filter status=active --filter inertia_tier=1
  memory graph objects list --filter status=active,draft --filter-op in
  memory graph objects list --filter status --filter-op exists
  memory graph objects list --run-id <run-uuid>
  memory graph objects list --credential-id <token-uuid> --data-class source`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate the write-stamp and actor provenance flags before any network
		// call, so an invalid enum or an unsatisfiable --provenance fails fast
		// rather than relying on the server's 400. The stamp flags are parsed
		// first because --provenance may narrow them on their own.
		runID, credentialID, credentialKind, dataClass, err := parseWriteStampFlags(true)
		if err != nil {
			return err
		}

		hasStamp := runID != "" || credentialID != "" || credentialKind != "" || dataClass != ""
		actorType, actorID, provenance, err := parseProvenanceFilter(hasStamp)
		if err != nil {
			return err
		}

		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		opts := &sdkgraph.ListObjectsOptions{
			ActorType:  actorType,
			ActorID:    actorID,
			Provenance: provenance,

			RunID:          runID,
			CredentialID:   credentialID,
			CredentialKind: credentialKind,
			DataClass:      dataClass,
		}
		if graphTypeFlag != "" {
			opts.Type = graphTypeFlag
		}
		if graphLimitFlag > 0 {
			opts.Limit = graphLimitFlag
		}
		if graphCursorFlag != "" {
			opts.Cursor = graphCursorFlag
		}
		if graphBranchFlag != "" {
			branchID, err := resolveBranchNameOrID(cmd, graphBranchFlag)
			if err != nil {
				return err
			}
			opts.BranchID = branchID
		}
		if graphStatusFlag != "" {
			opts.Status = graphStatusFlag
		}
		if len(graphFilterFlag) > 0 {
			pf, err := parsePropertyFilters(graphFilterFlag, graphFilterOpFlag)
			if err != nil {
				return err
			}
			opts.PropertyFilters = pf
		}
		if graphIDsFlag != "" {
			opts.IDs = strings.Split(graphIDsFlag, ",")
		}
		if graphKeyFlag != "" {
			opts.Key = graphKeyFlag
		}

		resp, err := g.ListObjects(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(resp)
		}

		if len(resp.Items) == 0 {
			fmt.Fprintln(out, "No objects found.")
			return nil
		}

		table := internalui.NewTable(internalui.TableConfig{Compact: true})
		table.SetHeaders([]string{"Entity ID", "Type", "Version", "Status", "Created"})
		for _, obj := range resp.Items {
			status := ""
			if obj.Status != nil {
				status = *obj.Status
			}
			table.AddRow([]string{
				obj.EntityID,
				obj.Type,
				fmt.Sprintf("%d", obj.Version),
				status,
				fmtTime(obj.CreatedAt),
			})
		}
		fmt.Fprint(out, table.Render())
		if resp.Total > 0 {
			shown := len(resp.Items)
			if shown < resp.Total {
				fmt.Fprintf(out, "Showing %d of %d total", shown, resp.Total)
				if resp.NextCursor != nil && *resp.NextCursor != "" {
					fmt.Fprintf(out, " — use --cursor %s for next page", *resp.NextCursor)
				}
				fmt.Fprintln(out)
			} else {
				fmt.Fprintf(out, "%d total\n", resp.Total)
			}
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects get
// ─────────────────────────────────────────────

var graphObjectsGetCmd = &cobra.Command{
	Use:   "get <id|key>",
	Short: "Get a graph object by ID or key",
	Long: `Get details for a graph object (entity) by its ID or key.

When a UUID is provided it is fetched directly. When a non-UUID string is
provided it is treated as a key and resolved against the specified branch
(or the main branch when --branch is omitted).

Use --branch to scope key resolution to a specific branch — accepts either
a branch UUID or a human-readable branch name.

Prints Entity ID, Version ID, Type, Version number, Key (if set), Status (if
set), Labels (if any), Created timestamp, the write stamp (actor, credential,
run and data class, when recorded), and Properties as formatted JSON.
Use --output json to receive the full object as JSON instead.

Examples:
  memory graph objects get <uuid>
  memory graph objects get my-object-key
  memory graph objects get my-object-key --branch plan/next-gen
  memory graph objects get my-object-key --branch <branch-uuid>`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		idOrKey, _, err := resolveGraphObjectArgOrPick(cmd, g, args)
		if err != nil {
			return err
		}

		// Resolve branch name → UUID if --branch was provided.
		var resolvedBranchID string
		if graphBranchFlag != "" {
			resolvedBranchID, err = resolveBranchNameOrID(cmd, graphBranchFlag)
			if err != nil {
				return err
			}
		}

		// If the argument is not a UUID, treat it as a key and resolve via ListObjects.
		if !isUUID(idOrKey) {
			listOpts := &sdkgraph.ListObjectsOptions{
				Key:   idOrKey,
				Limit: 2,
			}
			if resolvedBranchID != "" {
				listOpts.BranchID = resolvedBranchID
			}
			resp, err := g.ListObjects(context.Background(), listOpts)
			if err != nil {
				return fmt.Errorf("failed to resolve key %q: %w", idOrKey, err)
			}
			switch len(resp.Items) {
			case 0:
				return fmt.Errorf("no object found with key %q", idOrKey)
			case 1:
				idOrKey = resp.Items[0].EntityID
			default:
				return fmt.Errorf("ambiguous key %q — multiple objects matched; use the object UUID instead", idOrKey)
			}
		}

		obj, err := g.GetObject(context.Background(), idOrKey)
		if err != nil {
			return fmt.Errorf("failed to get object: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(obj)
		}

		status := ""
		if obj.Status != nil {
			status = *obj.Status
		}
		key := ""
		if obj.Key != nil {
			key = *obj.Key
		}

		fmt.Fprintf(out, "Entity ID:   %s\n", obj.EntityID)
		fmt.Fprintf(out, "Version ID:  %s\n", obj.VersionID)
		fmt.Fprintf(out, "Type:        %s\n", obj.Type)
		if obj.Ref != "" {
			fmt.Fprintf(out, "Ref:         %s\n", obj.Ref)
		}
		if obj.Name != nil && *obj.Name != "" {
			fmt.Fprintf(out, "Name:        %s\n", *obj.Name)
		}
		fmt.Fprintf(out, "Version:     %d\n", obj.Version)
		if key != "" {
			fmt.Fprintf(out, "Key:         %s\n", key)
		}
		if status != "" {
			fmt.Fprintf(out, "Status:      %s\n", status)
		}
		if len(obj.Labels) > 0 {
			fmt.Fprintf(out, "Labels:      %s\n", strings.Join(obj.Labels, ", "))
		}
		fmt.Fprintf(out, "Created:     %s\n", fmtTime(obj.CreatedAt))
		if obj.SchemaVersion != nil && *obj.SchemaVersion != "" {
			fmt.Fprintf(out, "Schema Ver:  %s\n", *obj.SchemaVersion)
		}

		// Write stamp (#2376/#2468): who wrote this version and how. Printed only
		// for fields that are present, so an unattributed legacy row stays terse.
		if obj.ActorType != nil && *obj.ActorType != "" {
			actor := *obj.ActorType
			if obj.ActorID != nil && *obj.ActorID != "" {
				actor += " " + *obj.ActorID
			}
			_, _ = fmt.Fprintf(out, "Actor:       %s\n", actor)
		}
		if obj.CredentialKind != "" {
			credential := obj.CredentialKind
			if obj.CredentialID != nil && *obj.CredentialID != "" {
				credential += " " + *obj.CredentialID
			}
			_, _ = fmt.Fprintf(out, "Credential:  %s\n", credential)
		}
		if obj.RunID != nil && *obj.RunID != "" {
			run := *obj.RunID
			if obj.RootRunID != nil && *obj.RootRunID != "" && *obj.RootRunID != run {
				run += " (root " + *obj.RootRunID + ")"
			}
			_, _ = fmt.Fprintf(out, "Run:         %s\n", run)
		}
		if obj.DataClass != "" {
			class := obj.DataClass
			if obj.OwnerSourceID != nil && *obj.OwnerSourceID != "" {
				class += " (owner " + *obj.OwnerSourceID + ")"
			}
			_, _ = fmt.Fprintf(out, "Data class:  %s\n", class)
		}
		if obj.InputHash != nil && *obj.InputHash != "" {
			_, _ = fmt.Fprintf(out, "Input hash:  %s\n", *obj.InputHash)
		}

		if len(obj.Properties) > 0 {
			propsJSON, _ := json.MarshalIndent(obj.Properties, "             ", "  ")
			fmt.Fprintf(out, "Properties:  %s\n", propsJSON)
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects create
// ─────────────────────────────────────────────

var graphObjectsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a graph object",
	Long: `Create a new graph object with the given type and optional properties.

When --key is given, the object is keyed for idempotent operations:
  - By default (skip): if an object with that key already exists, the command
    exits successfully without modifying it.
  - With --upsert: if an object with that key already exists, it is updated
    (create-or-update semantics matching blueprint behavior).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphTypeFlag == "" {
			return fmt.Errorf("--type is required")
		}
		if strings.EqualFold(graphTypeFlag, "Relationship") {
			return fmt.Errorf("cannot create a Relationship via 'graph objects create'.\n\nRelationships are graph edges and require explicit source and destination entity IDs.\nUse instead:\n  memory graph relationships create --type <rel-type> --from <src-entity-id> --to <dst-entity-id>\n\nTo find entity IDs: memory graph objects list --type <type>")
		}
		if graphUpsertFlag && graphKeyFlag == "" {
			return fmt.Errorf("--upsert requires --key")
		}

		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.CreateObjectRequest{
			Type: graphTypeFlag,
		}

		if graphKeyFlag != "" {
			req.Key = &graphKeyFlag
		}
		if graphStatusFlag != "" {
			req.Status = &graphStatusFlag
		}
		if graphBranchFlag != "" {
			req.BranchID = &graphBranchFlag
		}

		if graphPropsFlag != "" {
			var props map[string]any
			if err := json.Unmarshal([]byte(graphPropsFlag), &props); err != nil {
				return fmt.Errorf("invalid --properties JSON: %w", err)
			}
			req.Properties = props
		}

		if graphNameFlag != "" {
			if req.Properties == nil {
				req.Properties = make(map[string]any)
			}
			req.Properties["name"] = graphNameFlag
		}
		if graphDescFlag != "" {
			if req.Properties == nil {
				req.Properties = make(map[string]any)
			}
			req.Properties["description"] = graphDescFlag
		}

		out := cmd.OutOrStdout()

		if graphUpsertFlag {
			// --upsert: create-or-update by (type, key)
			obj, err := g.UpsertObject(context.Background(), req)
			if err != nil {
				return fmt.Errorf("failed to upsert object: %w", err)
			}
			if graphOutputFlag == "json" {
				return json.NewEncoder(out).Encode(obj)
			}
			fmt.Fprintf(out, "%s\t%s\t%s\n", obj.EntityID, obj.Type, nameFromProps(obj.Properties))
			return nil
		}

		obj, err := g.CreateObject(context.Background(), req)
		if err != nil {
			// --key with no --upsert: treat a 409 conflict as "already exists, skip"
			if graphKeyFlag != "" && sdkerrors.IsConflict(err) {
				fmt.Fprintf(out, "Object with type %q and key %q already exists, skipping.\n", graphTypeFlag, graphKeyFlag)
				return nil
			}
			return fmt.Errorf("failed to create object: %w", err)
		}

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(obj)
		}

		fmt.Fprintf(out, "%s\t%s\t%s\n", obj.EntityID, obj.Type, nameFromProps(obj.Properties))
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects update
// ─────────────────────────────────────────────

var graphObjectsUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a graph object",
	Long:  "Update a graph object's properties or status (creates a new version)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.UpdateObjectRequest{}

		if graphKeyFlag != "" {
			req.Key = &graphKeyFlag
		}
		if graphStatusFlag != "" {
			req.Status = &graphStatusFlag
		}
		if graphBranchFlag != "" {
			req.BranchID = &graphBranchFlag
		}

		if graphPropsFlag != "" {
			var props map[string]any
			if err := json.Unmarshal([]byte(graphPropsFlag), &props); err != nil {
				return fmt.Errorf("invalid --properties JSON: %w", err)
			}
			req.Properties = props
		}

		if graphNameFlag != "" {
			if req.Properties == nil {
				req.Properties = make(map[string]any)
			}
			req.Properties["name"] = graphNameFlag
		}
		if graphDescFlag != "" {
			if req.Properties == nil {
				req.Properties = make(map[string]any)
			}
			req.Properties["description"] = graphDescFlag
		}

		objectID, _, err := resolveGraphObjectArgOrPick(cmd, g, args)
		if err != nil {
			return err
		}

		obj, err := g.UpdateObject(context.Background(), objectID, req)
		if err != nil {
			return fmt.Errorf("failed to update object: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(obj)
		}

		fmt.Fprintf(out, "Object updated.\n")
		fmt.Fprintf(out, "  Entity ID:  %s\n", obj.EntityID)
		fmt.Fprintf(out, "  Version ID: %s\n", obj.VersionID)
		fmt.Fprintf(out, "  Version:    %d\n", obj.Version)
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects update-batch
// ─────────────────────────────────────────────

var graphObjectsUpdateBatchCmd = &cobra.Command{
	Use:   "update-batch",
	Short: "Batch-update graph objects from a JSON file",
	Long: `Update multiple graph objects in one API call.

The input file must contain a JSON array of objects, each with:
  id         (string, required) — object entity ID to update
  key        (string, optional)
  properties (object, optional) — merged with existing properties
  labels     ([]string, optional) — appended (or replaced with replaceLabels)
  replaceLabels (bool, optional) — replace labels instead of appending
  status     (string, optional)
  branch_id  (string, optional)

Example updates.json:
  [
    {"id": "<entity-id-1>", "properties": {"priority": "high"}},
    {"id": "<entity-id-2>", "status": "archived", "labels": ["deprecated"]}
  ]

Output (one line per object): <entity-id>  <type>  <version>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphBatchFile == "" {
			return fmt.Errorf("--file is required")
		}

		data, err := os.ReadFile(graphBatchFile)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		var items []batchUpdateObjectItem
		if err := json.Unmarshal(data, &items); err != nil {
			return fmt.Errorf("parsing JSON: %w", err)
		}
		if len(items) == 0 {
			return fmt.Errorf("file contains no items")
		}

		reqs := make([]sdkgraph.BulkUpdateObjectItem, 0, len(items))
		for _, item := range items {
			if item.ID == "" {
				return fmt.Errorf("item missing 'id' field")
			}
			req := sdkgraph.BulkUpdateObjectItem{
				ID:            item.ID,
				Key:           item.Key,
				Properties:    item.Properties,
				Labels:        item.Labels,
				ReplaceLabels: item.ReplaceLabels,
				Status:        item.Status,
				BranchID:      item.BranchID,
			}
			reqs = append(reqs, req)
		}

		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		resp, err := g.BulkUpdateObjects(context.Background(), &sdkgraph.BulkUpdateObjectsRequest{
			Items: reqs,
		})
		if err != nil {
			return fmt.Errorf("bulk update failed: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(resp)
		}

		for _, r := range resp.Results {
			if r.Success && r.Object != nil {
				fmt.Fprintf(out, "%s\t%s\tv%d\n", r.Object.EntityID, r.Object.Type, r.Object.Version)
			} else {
				errMsg := "unknown error"
				if r.Error != nil {
					errMsg = *r.Error
				}
				fmt.Fprintf(out, "ERROR[%d]\t%s\t%s\n", r.Index, r.ID, errMsg)
			}
		}

		if resp.Failed > 0 {
			return fmt.Errorf("%d item(s) failed to update", resp.Failed)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects delete
// ─────────────────────────────────────────────

var graphObjectsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a graph object",
	Long:  "Soft-delete a graph object by ID",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		var branchID *string
		if graphBranchFlag != "" {
			resolved, err := resolveBranchNameOrID(cmd, graphBranchFlag)
			if err != nil {
				return err
			}
			branchID = &resolved
		}

		objectID, objectName, err := resolveGraphObjectArgOrPick(cmd, g, args)
		if err != nil {
			return err
		}

		if err := g.DeleteObject(context.Background(), objectID, branchID); err != nil {
			return fmt.Errorf("failed to delete object: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Object %s deleted.\n", objectName)
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects edges
// ─────────────────────────────────────────────

var graphObjectsEdgesCmd = &cobra.Command{
	Use:   "edges <id>",
	Short: "Show edges (relationships) for an object",
	Long: `Show all incoming and outgoing relationships for a graph object.

Prints two sections: Outgoing (format: [Type] → DstID (entity: EntityID)) and
Incoming (format: [Type] ← SrcID (entity: EntityID)) with counts for each.
Use --output json to receive the full edges response as JSON.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		objectID, _, err := resolveGraphObjectArgOrPick(cmd, g, args)
		if err != nil {
			return err
		}

		edges, err := g.GetObjectEdges(context.Background(), objectID, nil)
		if err != nil {
			return fmt.Errorf("failed to get edges: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(edges)
		}

		fmt.Fprintf(out, "Outgoing (%d):\n", len(edges.Outgoing))
		for _, r := range edges.Outgoing {
			fmt.Fprintf(out, "  [%s] → %s  (entity: %s)\n", r.Type, r.DstID, r.EntityID)
		}

		fmt.Fprintf(out, "\nIncoming (%d):\n", len(edges.Incoming))
		for _, r := range edges.Incoming {
			fmt.Fprintf(out, "  [%s] ← %s  (entity: %s)\n", r.Type, r.SrcID, r.EntityID)
		}

		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects similar
// ─────────────────────────────────────────────

var (
	graphSimilarLimitFlag    int
	graphSimilarTypeFlag     string
	graphSimilarMinScoreFlag float64
)

var graphObjectsSimilarCmd = &cobra.Command{
	Use:   "similar <id>",
	Short: "Find objects similar to a given object by embedding",
	Long: `Find graph objects similar to the given object using cosine similarity on stored embeddings.

Returns a ranked list with similarity scores. Use --limit to control result count,
--type to filter by object type, and --min-score to exclude low-confidence results.
Use --output json to receive the full response as JSON.

Examples:
  memory graph objects similar <entity-id>
  memory graph objects similar <entity-id> --limit 20 --type Feature
  memory graph objects similar <entity-id> --min-score 0.75 --output json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		opts := &sdkgraph.FindSimilarOptions{}
		if graphSimilarLimitFlag > 0 {
			opts.Limit = graphSimilarLimitFlag
		}
		if graphSimilarTypeFlag != "" {
			opts.Type = graphSimilarTypeFlag
		}
		if graphSimilarMinScoreFlag > 0 {
			v := float32(graphSimilarMinScoreFlag)
			opts.MinScore = &v
		}

		objectID, _, err := resolveGraphObjectArgOrPick(cmd, g, args)
		if err != nil {
			return err
		}

		results, err := g.FindSimilar(context.Background(), objectID, opts)
		if err != nil {
			return fmt.Errorf("failed to find similar objects: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(results)
		}

		if len(results) == 0 {
			fmt.Fprintln(out, "No similar objects found.")
			return nil
		}

		table := internalui.NewTable(internalui.TableConfig{Compact: true})
		table.SetHeaders([]string{"Score", "Type", "Entity ID", "Status", "Ref", "Name"})
		for _, r := range results {
			score := fmt.Sprintf("%.4f", 1-r.Distance)
			name := ""
			if r.Name != nil {
				name = *r.Name
			}
			canonicalID := r.ID
			if r.CanonicalID != nil {
				canonicalID = *r.CanonicalID
			}
			table.AddRow([]string{score, r.Type, canonicalID, r.Status, r.Ref, name})
		}
		fmt.Fprint(out, table.Render())
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects bulk-update
// ─────────────────────────────────────────────

var graphObjectsBulkUpdateCmd = &cobra.Command{
	Use:   "bulk-update",
	Short: "Bulk update graph objects matching a filter",
	Long: `Execute a bulk action on all graph objects matching the given filter.

The --action flag controls what operation is performed. Use --dry-run to preview
the number of matching objects without making any changes.

Supported actions:
  update_status        Set status field (requires --value)
  soft_delete          Set deleted_at timestamp
  hard_delete          Permanently remove objects
  merge_properties     Deep-merge --properties into existing JSONB properties
  replace_properties   Replace the entire properties object
  add_labels           Add labels (--labels)
  remove_labels        Remove labels (--labels)
  set_labels           Replace all labels (--labels)

Examples:
  memory graph objects bulk-update --type Message --action update_status --value archived
  memory graph objects bulk-update --type Document --filter "days_since_access>365" --action soft_delete --dry-run
  memory graph objects bulk-update --action merge_properties --properties '{"verified":true}' --limit 500`,
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.BulkActionRequest{
			Action: graphBulkActionFlag,
			Value:  graphBulkValueFlag,
			DryRun: graphBulkDryRunFlag,
			Limit:  graphBulkLimitFlag,
		}

		// Build filter
		if graphTypeFlag != "" {
			req.Filter.Types = strings.Split(graphTypeFlag, ",")
		}

		// Property filters from --filter key=value pairs
		pfs, err := parsePropertyFilters(graphFilterFlag, graphFilterOpFlag)
		if err != nil {
			return err
		}
		req.Filter.PropertyFilters = pfs

		// Label filters
		if len(graphBulkLabelsFlag) > 0 {
			req.Labels = graphBulkLabelsFlag
		}

		// Properties JSON for merge/replace actions
		if graphPropsFlag != "" {
			var props map[string]any
			if err := json.Unmarshal([]byte(graphPropsFlag), &props); err != nil {
				return fmt.Errorf("invalid --properties JSON: %w", err)
			}
			req.Properties = props
		}

		resp, err := g.BulkAction(context.Background(), req)
		if err != nil {
			return fmt.Errorf("bulk action failed: %w", err)
		}

		out := cmd.OutOrStdout()
		if graphBulkDryRunFlag {
			fmt.Fprintf(out, "Dry run — matched: %d (no changes made)\n", resp.Matched)
		} else {
			fmt.Fprintf(out, "matched: %d  affected: %d\n", resp.Matched, resp.Affected)
			if resp.Matched > resp.Affected {
				fmt.Fprintf(out, "Warning: limit was applied — %d objects matched but only %d were affected.\n", resp.Matched, resp.Affected)
			}
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects bulk-delete
// ─────────────────────────────────────────────

var graphObjectsBulkDeleteCmd = &cobra.Command{
	Use:   "bulk-delete",
	Short: "Bulk delete graph objects matching a filter",
	Long: `Permanently delete all graph objects matching the given filter.

This is a shorthand for bulk-update --action hard_delete. Use --dry-run to
preview the number of matching objects without deleting any.

Examples:
  memory graph objects bulk-delete --type Message --dry-run
  memory graph objects bulk-delete --type Log --filter "created_at=90d" --filter-op lt --limit 5000`,
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.BulkActionRequest{
			Action: "hard_delete",
			DryRun: graphBulkDryRunFlag,
			Limit:  graphBulkLimitFlag,
		}

		if graphTypeFlag != "" {
			req.Filter.Types = strings.Split(graphTypeFlag, ",")
		}

		if len(graphFilterFlag) > 0 {
			pfs, err := parsePropertyFilters(graphFilterFlag, graphFilterOpFlag)
			if err != nil {
				return err
			}
			req.Filter.PropertyFilters = pfs
		}

		resp, err := g.BulkAction(context.Background(), req)
		if err != nil {
			return fmt.Errorf("bulk delete failed: %w", err)
		}

		out := cmd.OutOrStdout()
		if graphBulkDryRunFlag {
			fmt.Fprintf(out, "Dry run — matched: %d (no objects deleted)\n", resp.Matched)
		} else {
			fmt.Fprintf(out, "matched: %d  deleted: %d\n", resp.Matched, resp.Affected)
			if resp.Matched > resp.Affected {
				fmt.Fprintf(out, "Warning: limit was applied — %d objects matched but only %d were deleted.\n", resp.Matched, resp.Affected)
			}
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects move
// ─────────────────────────────────────────────

var graphObjectsMoveCmd = &cobra.Command{
	Use:   "move <id>",
	Short: "Move a graph object to a different branch",
	Long: `Move a graph object from its current branch to a target branch.

The object's full version chain and any self-referencing relationships are moved.
If the object has relationships connecting to other objects that are not being
moved, the operation fails — move related objects first or delete the relationships.

Use --target-branch to specify the destination branch UUID. Use "main" or omit
the flag to move to the main branch.

Examples:
  memory graph objects move <entity-id> --target-branch <branch-uuid>
  memory graph objects move <entity-id> --target-branch main`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		objectID, _, err := resolveGraphObjectArgOrPick(cmd, g, args)
		if err != nil {
			return err
		}

		req := &sdkgraph.MoveObjectRequest{}
		if graphMoveTargetBranchFlag != "" && graphMoveTargetBranchFlag != "main" {
			req.TargetBranchID = &graphMoveTargetBranchFlag
		}
		// nil TargetBranchID means move to main branch

		result, err := g.MoveObject(context.Background(), objectID, req)
		if err != nil {
			if apiErr, ok := err.(*sdkerrors.Error); ok {
				return fmt.Errorf("move failed (%d): %s", apiErr.StatusCode, apiErr.Message)
			}
			return fmt.Errorf("failed to move object: %w", err)
		}
		out := cmd.OutOrStdout()

		if jsonFlag {
			graphOutputFlag = "json"
		}

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(result)
		}

		targetLabel := "main"
		if result.TargetBranchID != nil {
			targetLabel = *result.TargetBranchID
		}
		sourceLabel := "main"
		if result.SourceBranchID != nil {
			sourceLabel = *result.SourceBranchID
		}
		fmt.Fprintf(out, "Object moved: %s -> %s\n", sourceLabel, targetLabel)
		if result.Object != nil {
			fmt.Fprintf(out, "  Entity ID: %s\n", result.Object.EntityID)
			fmt.Fprintf(out, "  Type: %s\n", result.Object.Type)
			if result.Object.Key != nil {
				fmt.Fprintf(out, "  Key: %s\n", *result.Object.Key)
			}
		}
		if result.MovedRelationships > 0 {
			fmt.Fprintf(out, "  Moved relationships: %d\n", result.MovedRelationships)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph relationships list
// ─────────────────────────────────────────────

var graphRelationshipsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List relationships",
	Long: `List relationships in the current project.

Output is a table with columns: Entity ID, Type, From (source entity ID), To
(destination entity ID), and Created date. Use --type to filter by relationship
type, --from/--to to filter by endpoint, --limit to control result count,
--cursor to paginate through large result sets, and --output json to receive
the full list as JSON.

Use --run-id, --credential-id and --credential-kind to filter by the write stamp
every version row records (a root run matches the runs it delegated to).
Relationships record no data class, so there is no --data-class here.

By default those filters match the relationship's current version. Use
--provenance to choose which version row must carry the stamp: updated
(default) matches the current version, created matches the version that created
the relationship, and any matches either.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		runID, credentialID, credentialKind, _, err := parseWriteStampFlags(false)
		if err != nil {
			return err
		}

		hasStamp := runID != "" || credentialID != "" || credentialKind != ""
		provenance, err := parseProvenanceFlag(hasStamp, "a write-stamp filter")
		if err != nil {
			return err
		}

		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		opts := &sdkgraph.ListRelationshipsOptions{
			RunID:          runID,
			CredentialID:   credentialID,
			CredentialKind: credentialKind,
			Provenance:     provenance,
		}
		if graphRelTypeFlag != "" {
			opts.Type = graphRelTypeFlag
		}
		if graphFromFlag != "" {
			opts.SrcID = graphFromFlag
		}
		if graphToFlag != "" {
			opts.DstID = graphToFlag
		}
		if graphLimitFlag > 0 {
			opts.Limit = graphLimitFlag
		}
		if graphCursorFlag != "" {
			opts.Cursor = graphCursorFlag
		}
		if graphBranchFlag != "" {
			opts.BranchID = graphBranchFlag
		}

		resp, err := g.ListRelationships(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("failed to list relationships: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(resp)
		}

		if len(resp.Items) == 0 {
			fmt.Fprintln(out, "No relationships found.")
			return nil
		}

		table := internalui.NewTable(internalui.TableConfig{Compact: true})
		table.SetHeaders([]string{"Entity ID", "Type", "From", "To", "Created"})
		for _, r := range resp.Items {
			table.AddRow([]string{
				r.EntityID,
				r.Type,
				r.SrcID,
				r.DstID,
				fmtTime(r.CreatedAt),
			})
		}
		fmt.Fprint(out, table.Render())
		if resp.Total > 0 {
			shown := len(resp.Items)
			if shown < resp.Total {
				fmt.Fprintf(out, "Showing %d of %d total", shown, resp.Total)
				if resp.NextCursor != nil && *resp.NextCursor != "" {
					fmt.Fprintf(out, " — use --cursor %s for next page", *resp.NextCursor)
				}
				fmt.Fprintln(out)
			} else {
				fmt.Fprintf(out, "%d total\n", resp.Total)
			}
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph relationships get
// ─────────────────────────────────────────────

var graphRelationshipsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a relationship by ID",
	Long: `Get details for a graph relationship by its ID.

Prints Entity ID, Version ID, Type, From (source entity ID), To (destination
entity ID), Version number, Created timestamp, and Properties as formatted
JSON. Use --output json to receive the full relationship as JSON instead.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		relationshipID, _, err := resolveGraphRelationshipArgOrPick(cmd, g, args)
		if err != nil {
			return err
		}

		r, err := g.GetRelationship(context.Background(), relationshipID)
		if err != nil {
			return fmt.Errorf("failed to get relationship: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(r)
		}

		fmt.Fprintf(out, "Entity ID:  %s\n", r.EntityID)
		fmt.Fprintf(out, "Version ID: %s\n", r.VersionID)
		fmt.Fprintf(out, "Type:       %s\n", r.Type)
		fmt.Fprintf(out, "From:       %s\n", r.SrcID)
		fmt.Fprintf(out, "To:         %s\n", r.DstID)
		fmt.Fprintf(out, "Version:    %d\n", r.Version)
		fmt.Fprintf(out, "Created:    %s\n", fmtTime(r.CreatedAt))
		if len(r.Properties) > 0 {
			propsJSON, _ := json.MarshalIndent(r.Properties, "            ", "  ")
			fmt.Fprintf(out, "Properties: %s\n", propsJSON)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph relationships create
// ─────────────────────────────────────────────

var graphRelationshipsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a relationship",
	Long: `Create a directed relationship between two graph objects.

When --upsert is given, the operation is idempotent: if a relationship with the
same type, source, and destination already exists, it is returned as-is without
creating a duplicate. If the relationship was deleted, it is restored.
Without --upsert, creating a relationship that already exists returns an error.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphRelTypeFlag == "" {
			return fmt.Errorf("--type is required")
		}
		if graphFromFlag == "" {
			return fmt.Errorf("--from is required")
		}
		if graphToFlag == "" {
			return fmt.Errorf("--to is required")
		}

		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.CreateRelationshipRequest{
			Type:  graphRelTypeFlag,
			SrcID: graphFromFlag,
			DstID: graphToFlag,
		}

		if graphBranchFlag != "" {
			req.BranchID = &graphBranchFlag
		}

		if graphPropsFlag != "" {
			var props map[string]any
			if err := json.Unmarshal([]byte(graphPropsFlag), &props); err != nil {
				return fmt.Errorf("invalid --properties JSON: %w", err)
			}
			req.Properties = props
		}

		out := cmd.OutOrStdout()

		if graphUpsertFlag {
			r, err := g.UpsertRelationship(context.Background(), req)
			if err != nil {
				return fmt.Errorf("failed to upsert relationship: %w", err)
			}
			if graphOutputFlag == "json" {
				return json.NewEncoder(out).Encode(r)
			}
			fmt.Fprintf(out, "%s\t%s\t%s -> %s\n", r.EntityID, r.Type, r.SrcID, r.DstID)
			return nil
		}

		r, err := g.CreateRelationship(context.Background(), req)
		if err != nil {
			return fmt.Errorf("failed to create relationship: %w", err)
		}

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(r)
		}

		fmt.Fprintf(out, "%s\t%s\t%s -> %s\n", r.EntityID, r.Type, r.SrcID, r.DstID)
		return nil
	},
}

// ─────────────────────────────────────────────
// graph relationships delete
// ─────────────────────────────────────────────

var graphRelationshipsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a relationship",
	Long: `Soft-delete a graph relationship by ID.

Use --branch to scope the deletion to a specific branch (name or UUID).
Without --branch the relationship is deleted from the main graph.

Examples:
  memory graph relationships delete <id>
  memory graph relationships delete <id> --branch plan/next-gen
  memory graph relationships delete <id> --branch <branch-uuid>`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		var resolvedBranch string
		if graphBranchFlag != "" {
			resolvedBranch, err = resolveBranchNameOrID(cmd, graphBranchFlag)
			if err != nil {
				return err
			}
		}

		relationshipID, relationshipName, err := resolveGraphRelationshipArgOrPick(cmd, g, args)
		if err != nil {
			return err
		}

		if err := g.DeleteRelationship(context.Background(), relationshipID, resolvedBranch); err != nil {
			return fmt.Errorf("failed to delete relationship: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Relationship %s deleted.\n", relationshipName)
		return nil
	},
}

// ─────────────────────────────────────────────
// Subgraph helpers
// ─────────────────────────────────────────────

// toSubgraphSDKRequest converts a parsed subgraphInput into SDK request structs.
func toSubgraphSDKRequest(sg subgraphInput) sdkgraph.CreateSubgraphRequest {
	objReqs := make([]sdkgraph.SubgraphObjectRequest, 0, len(sg.Objects))
	for _, o := range sg.Objects {
		props := make(map[string]any)
		for k, v := range o.Properties {
			props[k] = v
		}
		if o.Name != "" {
			props["name"] = o.Name
		}
		if o.Description != "" {
			props["description"] = o.Description
		}
		req := sdkgraph.SubgraphObjectRequest{
			Ref:  o.Ref,
			Type: o.Type,
			Key:  o.Key,
		}
		if len(props) > 0 {
			req.Properties = props
		}
		objReqs = append(objReqs, req)
	}

	relReqs := make([]sdkgraph.SubgraphRelationshipRequest, 0, len(sg.Relationships))
	for _, r := range sg.Relationships {
		relReqs = append(relReqs, sdkgraph.SubgraphRelationshipRequest{
			Type:       r.Type,
			SrcRef:     r.SrcRef,
			DstRef:     r.DstRef,
			SrcID:      r.SrcID,
			DstID:      r.DstID,
			Properties: r.Properties,
		})
	}

	return sdkgraph.CreateSubgraphRequest{Objects: objReqs, Relationships: relReqs}
}

// callSubgraph converts and sends a subgraphInput to the server.
func callSubgraph(ctx context.Context, g *sdkgraph.Client, sg subgraphInput) (*sdkgraph.CreateSubgraphResponse, error) {
	req := toSubgraphSDKRequest(sg)
	resp, err := g.CreateSubgraph(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("subgraph create failed: %w", err)
	}
	return resp, nil
}

// printSubgraphResult writes the result of a subgraph call to out.
func printSubgraphResult(out io.Writer, resp *sdkgraph.CreateSubgraphResponse, outputFlag string) error {
	if outputFlag == "json" {
		return json.NewEncoder(out).Encode(resp)
	}
	for _, o := range resp.Objects {
		fmt.Fprintf(out, "%s\t%s\t%s\n", o.EntityID, o.Type, nameFromProps(o.Properties))
	}
	fmt.Fprintf(out, "Created %d objects, %d relationships\n", len(resp.Objects), len(resp.Relationships))
	return nil
}

// runSubgraphChunked splits a large subgraph into object-chunks of maxObj each,
// assigns relationships to the chunk that owns their src_ref (or sends them in a
// final pass for cross-chunk / src_id relationships). Warns on stderr for each chunk.
func runSubgraphChunked(cmd *cobra.Command, g *sdkgraph.Client, sg subgraphInput, out io.Writer, maxObj, maxRel int) error {
	ctx := context.Background()
	errOut := cmd.ErrOrStderr()

	// Build ref→chunkIndex map so we can assign relationships.
	refChunk := make(map[string]int, len(sg.Objects))
	chunks := make([]subgraphInput, 0)

	for i := 0; i < len(sg.Objects); i += maxObj {
		end := i + maxObj
		if end > len(sg.Objects) {
			end = len(sg.Objects)
		}
		chunk := subgraphInput{Objects: sg.Objects[i:end]}
		chunkIdx := len(chunks)
		for _, o := range chunk.Objects {
			if o.Ref != "" {
				refChunk[o.Ref] = chunkIdx
			}
		}
		chunks = append(chunks, chunk)
	}

	// Assign relationships: a relationship belongs to a chunk if both src and dst
	// resolve to the same chunk (via ref). Otherwise it goes to a cross-chunk pass.
	crossChunk := subgraphInput{}
	for _, rel := range sg.Relationships {
		srcChunk, srcOk := refChunk[rel.SrcRef]
		dstChunk, dstOk := refChunk[rel.DstRef]
		if srcOk && dstOk && srcChunk == dstChunk {
			chunks[srcChunk].Relationships = append(chunks[srcChunk].Relationships, rel)
		} else {
			// Cross-chunk or uses src_id/dst_id — defer to final pass.
			crossChunk.Relationships = append(crossChunk.Relationships, rel)
		}
	}

	// Execute each object chunk.
	totalObjs, totalRels := 0, 0
	mergedRefMap := make(map[string]string)

	for i, chunk := range chunks {
		fmt.Fprintf(errOut, "  chunk %d/%d: %d objects, %d relationships\n",
			i+1, len(chunks), len(chunk.Objects), len(chunk.Relationships))

		resp, err := callSubgraph(ctx, g, chunk)
		if err != nil {
			return fmt.Errorf("chunk %d failed: %w", i+1, err)
		}
		totalObjs += len(resp.Objects)
		totalRels += len(resp.Relationships)
		for _, o := range resp.Objects {
			fmt.Fprintf(out, "%s\t%s\t%s\n", o.EntityID, o.Type, nameFromProps(o.Properties))
		}
		for k, v := range resp.RefMap {
			mergedRefMap[k] = v
		}
	}

	// Cross-chunk pass: resolve src_ref/dst_ref that span chunks using the merged ref_map,
	// then send as relationships-only subgraph (objects array with a single placeholder).
	if len(crossChunk.Relationships) > 0 {
		fmt.Fprintf(errOut, "  cross-chunk pass: %d relationships\n", len(crossChunk.Relationships))

		// Resolve any remaining src_ref/dst_ref to src_id/dst_id using the merged ref_map.
		resolved := make([]subgraphRelationshipInput, 0, len(crossChunk.Relationships))
		for _, rel := range crossChunk.Relationships {
			r := rel
			if r.SrcRef != "" {
				if id, ok := mergedRefMap[r.SrcRef]; ok {
					r.SrcID = id
					r.SrcRef = ""
				} else {
					fmt.Fprintf(errOut, "  Warning: src_ref %q not resolved — skipping relationship %s→%s\n", r.SrcRef, r.SrcRef, r.DstRef)
					continue
				}
			}
			if r.DstRef != "" {
				if id, ok := mergedRefMap[r.DstRef]; ok {
					r.DstID = id
					r.DstRef = ""
				} else {
					fmt.Fprintf(errOut, "  Warning: dst_ref %q not resolved — skipping relationship %s→%s\n", r.DstRef, r.SrcRef, r.DstRef)
					continue
				}
			}
			resolved = append(resolved, r)
		}

		// Send cross-chunk relationships in batches of maxRel.
		for i := 0; i < len(resolved); i += maxRel {
			end := i + maxRel
			if end > len(resolved) {
				end = len(resolved)
			}
			batch := resolved[i:end]

			// Cross-chunk relationships all use src_id/dst_id (resolved above), so we
			// send them via BulkCreateRelationships rather than the subgraph endpoint.
			relItems := make([]sdkgraph.CreateRelationshipRequest, 0, len(batch))
			for _, r := range batch {
				relItems = append(relItems, sdkgraph.CreateRelationshipRequest{
					Type:  r.Type,
					SrcID: r.SrcID,
					DstID: r.DstID,
				})
			}
			relResp, err := g.BulkCreateRelationships(context.Background(), &sdkgraph.BulkCreateRelationshipsRequest{
				Items: relItems,
			})
			if err != nil {
				return fmt.Errorf("cross-chunk relationships failed: %w", err)
			}
			totalRels += relResp.Success
			if relResp.Failed > 0 {
				fmt.Fprintf(errOut, "  Warning: %d cross-chunk relationship(s) failed\n", relResp.Failed)
			}
		}
	}

	fmt.Fprintf(out, "Created %d objects, %d relationships (auto-chunked)\n", totalObjs, totalRels)
	return nil
}

// ─────────────────────────────────────────────
// graph objects create-batch
// ─────────────────────────────────────────────

// batchBranchPtr returns a branch ID pointer, or nil for the main graph.
func batchBranchPtr(branchID string) *string {
	if branchID == "" {
		return nil
	}
	id := branchID
	return &id
}

// bulkErrorMessage renders an optional per-item error from a bulk response.
func bulkErrorMessage(err *string) string {
	if err != nil && *err != "" {
		return *err
	}
	return "unknown error"
}

// runSeedImport reads a `memory blueprints dump` layout (dump root, seed
// directory, or .jsonl file) and applies it to a branch or the main graph.
// Records with a key are upserted, so the import is safe to re-run; unreadable
// rows are reported individually and the remaining rows are still applied.
func runSeedImport(cmd *cobra.Command, path string) error {
	imp, err := blueprints.LoadSeedPath(path)
	if err != nil {
		return err
	}

	g, err := getGraphClient(cmd)
	if err != nil {
		return err
	}

	branchID := ""
	if graphBranchFlag != "" {
		branchID, err = resolveBranchNameOrID(cmd, graphBranchFlag)
		if err != nil {
			return err
		}
	}

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	for _, issue := range imp.Issues {
		fmt.Fprintf(errOut, "ERROR\t%s\t%v\n", issue.Name, issue.Error)
	}

	// The seeder's progress and per-row error lines are diagnostics, so they
	// go to stderr and leave stdout clean for --output json.
	seeder := blueprints.NewSeeder(g, false, true /* upsert by key */, errOut)
	seeder.SetBranchID(branchID)
	result, runErr := seeder.Run(context.Background(), imp.Objects, imp.Relationships)
	if runErr != nil {
		return fmt.Errorf("seed import: %w", runErr)
	}

	failedRows := imp.FailedRows()
	if graphOutputFlag == "json" {
		payload := map[string]any{
			"branch_id":             branchID,
			"rows_failed":           failedRows,
			"objects_created":       result.ObjectsCreated,
			"objects_updated":       result.ObjectsUpdated,
			"objects_skipped":       result.ObjectsSkipped,
			"objects_failed":        result.ObjectsFailed,
			"relationships_created": result.RelsCreated,
			"relationships_failed":  result.RelsFailed,
		}
		if err := json.NewEncoder(out).Encode(payload); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "Imported %d objects created, %d updated, %d skipped, %d failed; %d relationships created, %d failed\n",
			result.ObjectsCreated, result.ObjectsUpdated, result.ObjectsSkipped, result.ObjectsFailed,
			result.RelsCreated, result.RelsFailed,
		)
	}

	if failedRows > 0 || result.ObjectsFailed > 0 || result.RelsFailed > 0 {
		return fmt.Errorf("import completed with errors: %d unreadable row(s), %d object(s) and %d relationship(s) failed",
			failedRows, result.ObjectsFailed, result.RelsFailed)
	}
	return nil
}

// subgraphObjectRequest converts one subgraph object into a bulk create
// request scoped to a branch.
func subgraphObjectRequest(o subgraphObjectInput, branchID string) sdkgraph.CreateObjectRequest {
	props := make(map[string]any)
	for k, v := range o.Properties {
		props[k] = v
	}
	if o.Name != "" {
		props["name"] = o.Name
	}
	if o.Description != "" {
		props["description"] = o.Description
	}

	req := sdkgraph.CreateObjectRequest{
		Type:     o.Type,
		BranchID: batchBranchPtr(branchID),
	}
	if o.Key != nil && *o.Key != "" {
		req.Key = o.Key
	}
	if len(props) > 0 {
		req.Properties = props
	}
	return req
}

// runSubgraphOnBranch imports subgraph objects and relationships into a branch.
// The atomic subgraph endpoint has no branch parameter, so this sends the same
// data through the branch-aware bulk endpoints: objects with a key are upserted
// by (type, key), keyless objects are created, and relationships are upserted by
// (type, src, dst) after their _ref placeholders are resolved to entity IDs.
// Per-row failures are reported and counted; the import continues.
func runSubgraphOnBranch(cmd *cobra.Command, g *sdkgraph.Client, sg subgraphInput, branchID string, out io.Writer) error {
	ctx := context.Background()
	errOut := cmd.ErrOrStderr()
	const maxItems = 100

	type indexedObject struct {
		input subgraphObjectInput
	}

	var keyed, keyless []indexedObject
	for _, o := range sg.Objects {
		if o.Key != nil && *o.Key != "" {
			keyed = append(keyed, indexedObject{input: o})
		} else {
			keyless = append(keyless, indexedObject{input: o})
		}
	}

	entityByRef := make(map[string]string, len(sg.Objects))
	objectFailed := 0

	recordEntity := func(slot indexedObject, obj *sdkgraph.GraphObject) {
		if slot.input.Ref != "" {
			entityByRef[slot.input.Ref] = obj.EntityID
		}
		if graphOutputFlag != "json" {
			fmt.Fprintf(out, "%s\t%s\t%s\n", obj.EntityID, obj.Type, nameFromProps(obj.Properties))
		}
	}

	chunkEnd := func(start, total int) int {
		end := start + maxItems
		if end > total {
			end = total
		}
		return end
	}

	// Keyed objects: create-or-update by (type, key) on the branch.
	for start := 0; start < len(keyed); start += maxItems {
		batch := keyed[start:chunkEnd(start, len(keyed))]
		items := make([]sdkgraph.CreateObjectRequest, len(batch))
		for i, slot := range batch {
			items[i] = subgraphObjectRequest(slot.input, branchID)
		}

		resp, err := g.BulkUpsertObjects(ctx, &sdkgraph.BulkUpsertObjectsRequest{Items: items})
		if err != nil {
			fmt.Fprintf(errOut, "  error: bulk upsert objects failed: %v\n", err)
			objectFailed += len(batch)
			continue
		}
		for i, r := range resp.Results {
			if i >= len(batch) {
				break
			}
			if r.Success && r.Object != nil {
				recordEntity(batch[i], r.Object)
			} else {
				objectFailed++
				fmt.Fprintf(errOut, "  error: object type=%s key=%q: %s\n",
					batch[i].input.Type, *batch[i].input.Key, bulkErrorMessage(r.Error))
			}
		}
	}

	// Keyless objects: plain create on the branch.
	for start := 0; start < len(keyless); start += maxItems {
		batch := keyless[start:chunkEnd(start, len(keyless))]
		items := make([]sdkgraph.CreateObjectRequest, len(batch))
		for i, slot := range batch {
			items[i] = subgraphObjectRequest(slot.input, branchID)
		}

		resp, err := g.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: items})
		if err != nil {
			fmt.Fprintf(errOut, "  error: bulk create objects failed: %v\n", err)
			objectFailed += len(batch)
			continue
		}
		for i, r := range resp.Results {
			if i >= len(batch) {
				break
			}
			if r.Success && r.Object != nil {
				recordEntity(batch[i], r.Object)
			} else {
				objectFailed++
				fmt.Fprintf(errOut, "  error: object type=%s: %s\n",
					batch[i].input.Type, bulkErrorMessage(r.Error))
			}
		}
	}

	// Relationships: resolve refs to the entity IDs recorded above, then upsert.
	type indexedRel struct {
		index int
		req   sdkgraph.CreateRelationshipRequest
	}
	var rels []indexedRel
	relFailed := 0

	for i, rel := range sg.Relationships {
		srcID, dstID := rel.SrcID, rel.DstID
		if rel.SrcRef != "" {
			id, ok := entityByRef[rel.SrcRef]
			if !ok {
				relFailed++
				fmt.Fprintf(errOut, "  error: relationship[%d] type=%s: src_ref %q was not created\n", i, rel.Type, rel.SrcRef)
				continue
			}
			srcID = id
		}
		if rel.DstRef != "" {
			id, ok := entityByRef[rel.DstRef]
			if !ok {
				relFailed++
				fmt.Fprintf(errOut, "  error: relationship[%d] type=%s: dst_ref %q was not created\n", i, rel.Type, rel.DstRef)
				continue
			}
			dstID = id
		}
		if srcID == "" || dstID == "" {
			relFailed++
			fmt.Fprintf(errOut, "  error: relationship[%d] type=%s: missing src/dst endpoint\n", i, rel.Type)
			continue
		}

		rels = append(rels, indexedRel{
			index: i,
			req: sdkgraph.CreateRelationshipRequest{
				Type:       rel.Type,
				SrcID:      srcID,
				DstID:      dstID,
				Properties: rel.Properties,
				BranchID:   batchBranchPtr(branchID),
			},
		})
	}

	relCreated := 0
	for start := 0; start < len(rels); start += maxItems {
		batch := rels[start:chunkEnd(start, len(rels))]
		items := make([]sdkgraph.CreateRelationshipRequest, len(batch))
		for i, entry := range batch {
			items[i] = entry.req
		}

		resp, err := g.BulkUpsertRelationships(ctx, &sdkgraph.BulkUpsertRelationshipsRequest{Items: items})
		if err != nil {
			fmt.Fprintf(errOut, "  error: bulk upsert relationships failed: %v\n", err)
			relFailed += len(batch)
			continue
		}
		for i, r := range resp.Results {
			if i >= len(batch) {
				break
			}
			if r.Success {
				relCreated++
			} else {
				relFailed++
				fmt.Fprintf(errOut, "  error: relationship[%d] type=%s: %s\n",
					batch[i].index, batch[i].req.Type, bulkErrorMessage(r.Error))
			}
		}
	}

	if graphOutputFlag == "json" {
		payload := map[string]any{
			"branch_id":             branchID,
			"objects_created":       len(sg.Objects) - objectFailed,
			"objects_failed":        objectFailed,
			"relationships_created": relCreated,
			"relationships_failed":  relFailed,
		}
		if err := json.NewEncoder(out).Encode(payload); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "Created %d objects, %d relationships on branch %s\n",
			len(sg.Objects)-objectFailed, relCreated, branchID)
	}

	if objectFailed > 0 || relFailed > 0 {
		return fmt.Errorf("%d object(s) and %d relationship(s) failed to import", objectFailed, relFailed)
	}
	return nil
}

var graphObjectsCreateBatchCmd = &cobra.Command{
	Use:   "create-batch",
	Short: "Batch-create graph objects (and optionally relationships) from a JSON file or blueprint dump",
	Long: `Create multiple graph objects in one API call. Accepts three input formats:

FLAT ARRAY FORMAT (objects only):
  A JSON array of objects, each with:
    type        (string, required)
    name        (string, optional) — placed in properties.name
    description (string, optional) — placed in properties.description
    properties  (object, optional) — arbitrary additional properties

  Example:
    [
      {"type": "Person", "name": "Alice"},
      {"type": "Project", "name": "Acme", "properties": {"status": "active"}}
    ]

  Output: one line per object: <entity-id>  <type>  <name>

SUBGRAPH FORMAT (objects + relationships, preferred when wiring is needed):
  A JSON object with "objects" and "relationships" arrays. Objects carry an
  optional "_ref" placeholder; relationships reference objects via "src_ref"/"dst_ref"
  for new objects in the same call, or "src_id"/"dst_id" for pre-existing UUIDs.
  Both can be mixed freely. Max 500 objects, 500 relationships per call — larger
  inputs are auto-chunked with a warning.

  Example (new objects only):
    {
      "objects": [
        {"_ref": "alice", "type": "Person", "key": "person-alice", "name": "Alice"},
        {"_ref": "acme",  "type": "Project", "key": "proj-acme",  "name": "Acme"}
      ],
      "relationships": [
        {"type": "member_of", "src_ref": "alice", "dst_ref": "acme"}
      ]
    }

  Example (mixed: new objects wired to existing UUIDs):
    {
      "objects": [
        {"_ref": "svc", "type": "Service", "name": "auth-service"}
      ],
      "relationships": [
        {"type": "calls_service", "src_id": "<existing-module-uuid>", "dst_ref": "svc"}
      ]
    }

  Output (text): one line per object, then "Created N objects, M relationships"
  Output (--output json): full response including ref_map (placeholder → UUID)

BLUEPRINT SEED FORMAT (round-trip for 'memory blueprints dump'):
  --file may point at a dump root (containing seed/objects/ and
  seed/relationships/), a directory of *.jsonl seed files, or a single
  .jsonl file. Records are upserted by key, "srcKey"/"dstKey" relationship
  endpoints are resolved after the objects are written, and each unreadable
  row is reported without aborting the import. Files named _untyped.jsonl are
  read back as type "Untyped".

BRANCHES:
  Pass --branch to write into a graph branch instead of the main graph. The
  value may be a branch name or UUID. For the subgraph format on a branch,
  objects are upserted by key and relationships by (type, src, dst) through
  the branch-aware bulk endpoints, because the subgraph endpoint has no
  branch parameter.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphBatchFile == "" {
			return fmt.Errorf("--file is required")
		}

		info, err := os.Stat(graphBatchFile)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}
		if info.IsDir() || strings.EqualFold(filepath.Ext(graphBatchFile), ".jsonl") {
			return runSeedImport(cmd, graphBatchFile)
		}

		data, err := os.ReadFile(graphBatchFile)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		// Detect format by peeking at the first non-whitespace byte.
		firstByte := byte(0)
		for _, b := range data {
			if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
				firstByte = b
				break
			}
		}

		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()

		branchID := ""
		if graphBranchFlag != "" {
			branchID, err = resolveBranchNameOrID(cmd, graphBranchFlag)
			if err != nil {
				return err
			}
		}

		if firstByte == '{' {
			// ── Subgraph format ──────────────────────────────────────────────
			var sg subgraphInput
			if err := json.Unmarshal(data, &sg); err != nil {
				return fmt.Errorf("parsing subgraph JSON: %w", err)
			}
			if len(sg.Objects) == 0 {
				return fmt.Errorf("subgraph contains no objects")
			}

			if branchID != "" {
				return runSubgraphOnBranch(cmd, g, sg, branchID, out)
			}

			const maxObjects = 500
			const maxRelationships = 500

			// Auto-chunk if over limits, with a warning.
			if len(sg.Objects) > maxObjects || len(sg.Relationships) > maxRelationships {
				fmt.Fprintf(out, "Warning: subgraph has %d objects and %d relationships — exceeds per-call limits (%d/%d). Auto-chunking by objects.\n",
					len(sg.Objects), len(sg.Relationships), maxObjects, maxRelationships)
				return runSubgraphChunked(cmd, g, sg, out, maxObjects, maxRelationships)
			}

			resp, err := callSubgraph(context.Background(), g, sg)
			if err != nil {
				return err
			}
			if err := printSubgraphResult(out, resp, graphOutputFlag); err != nil {
				return err
			}
			return nil
		}

		// ── Flat array format ────────────────────────────────────────────────
		if firstByte != '[' {
			return fmt.Errorf("unexpected JSON: expected array ([) or subgraph object ({), got %q", string(firstByte))
		}

		var items []batchObjectItem
		if err := json.Unmarshal(data, &items); err != nil {
			return fmt.Errorf("parsing JSON: %w", err)
		}
		if len(items) == 0 {
			return fmt.Errorf("file contains no items")
		}

		// Heuristic: if items look like relationships (have "from"/"to" but no "type"),
		// guide the user to the right command.
		if len(items) > 0 {
			var rawItems []map[string]json.RawMessage
			if e := json.Unmarshal(data, &rawItems); e == nil && len(rawItems) > 0 {
				first := rawItems[0]
				_, hasFrom := first["from"]
				_, hasTo := first["to"]
				_, hasType := first["type"]
				if hasFrom && hasTo && !hasType {
					return fmt.Errorf("input looks like relationships — use 'memory graph relationships create-batch' instead")
				}
			}
		}

		reqs := make([]sdkgraph.CreateObjectRequest, 0, len(items))
		for _, item := range items {
			req := sdkgraph.CreateObjectRequest{
				Type:     item.Type,
				BranchID: batchBranchPtr(branchID),
			}
			props := make(map[string]any)
			for k, v := range item.Properties {
				props[k] = v
			}
			if item.Name != "" {
				props["name"] = item.Name
			}
			if item.Description != "" {
				props["description"] = item.Description
			}
			if len(props) > 0 {
				req.Properties = props
			}
			reqs = append(reqs, req)
		}

		resp, err := g.BulkCreateObjects(context.Background(), &sdkgraph.BulkCreateObjectsRequest{
			Items: reqs,
		})
		if err != nil {
			return fmt.Errorf("bulk create failed: %w", err)
		}

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(resp)
		}

		for _, r := range resp.Results {
			if r.Success && r.Object != nil {
				fmt.Fprintf(out, "%s\t%s\t%s\n", r.Object.EntityID, r.Object.Type, nameFromProps(r.Object.Properties))
			} else {
				errMsg := "unknown error"
				if r.Error != nil {
					errMsg = *r.Error
				}
				fmt.Fprintf(out, "ERROR[%d]\t%s\n", r.Index, errMsg)
			}
		}

		if resp.Failed > 0 {
			return fmt.Errorf("%d item(s) failed to create", resp.Failed)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph relationships create-batch
// ─────────────────────────────────────────────

var graphRelationshipsCreateBatchCmd = &cobra.Command{
	Use:   "create-batch",
	Short: "Batch-create graph relationships from a JSON file",
	Long: `Create multiple graph relationships in one API call.

The input file must contain a JSON array of objects, each with:
  type  (string, required) — relationship type
  from  (string, required) — source entity ID
  to    (string, required) — destination entity ID
  properties (object, optional)

Use --upsert to make the operation idempotent: existing relationships (same
type, from, to) are returned as-is instead of causing an error. Safe to retry.

Pass --branch (a branch name or UUID) to create the relationships on a graph
branch instead of the main graph. Both endpoint objects must already exist on
that branch, because the server rejects an edge whose endpoints are not on the
same branch as the edge.

To import the JSONL files written by 'memory blueprints dump' (which reference
objects by srcKey/dstKey and include the objects themselves), use
'memory graph objects create-batch --file <dump-dir> --branch <name>'.

Example relationships.json:
  [
    {"type": "knows", "from": "<entity-id-1>", "to": "<entity-id-2>"},
    {"type": "manages", "from": "<entity-id-3>", "to": "<entity-id-4>"}
  ]

Output (one line per relationship): <entity-id>  <type>  <from> -> <to>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphBatchFile == "" {
			return fmt.Errorf("--file is required")
		}

		data, err := os.ReadFile(graphBatchFile)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		var items []batchRelationshipItem
		if err := json.Unmarshal(data, &items); err != nil {
			return fmt.Errorf("parsing JSON: %w", err)
		}
		if len(items) == 0 {
			return fmt.Errorf("file contains no items")
		}

		branchID := ""
		if graphBranchFlag != "" {
			branchID, err = resolveBranchNameOrID(cmd, graphBranchFlag)
			if err != nil {
				return err
			}
		}

		reqs := make([]sdkgraph.CreateRelationshipRequest, 0, len(items))
		for _, item := range items {
			if item.Type == "" {
				return fmt.Errorf("item missing 'type' field")
			}
			if item.From == "" {
				return fmt.Errorf("item missing 'from' field")
			}
			if item.To == "" {
				return fmt.Errorf("item missing 'to' field")
			}
			req := sdkgraph.CreateRelationshipRequest{
				Type:     item.Type,
				SrcID:    item.From,
				DstID:    item.To,
				Upsert:   graphUpsertFlag,
				BranchID: batchBranchPtr(branchID),
			}
			if len(item.Properties) > 0 {
				req.Properties = item.Properties
			}
			reqs = append(reqs, req)
		}

		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		resp, err := g.BulkCreateRelationships(context.Background(), &sdkgraph.BulkCreateRelationshipsRequest{
			Items: reqs,
		})
		if err != nil {
			return fmt.Errorf("bulk create failed: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(resp)
		}

		for _, r := range resp.Results {
			if r.Success && r.Relationship != nil {
				fmt.Fprintf(out, "%s\t%s\t%s -> %s\n", r.Relationship.EntityID, r.Relationship.Type, r.Relationship.SrcID, r.Relationship.DstID)
			} else {
				errMsg := "unknown error"
				if r.Error != nil {
					errMsg = *r.Error
				}
				fmt.Fprintf(out, "ERROR[%d]\t%s\n", r.Index, errMsg)
			}
		}

		if resp.Failed > 0 {
			return fmt.Errorf("%d item(s) failed to create", resp.Failed)
		}
		return nil
	},
}

// ─────────────────────────────────────────────
// graph events — stream SSE graph object events
// ─────────────────────────────────────────────

var graphEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Stream real-time graph object events via SSE",
	Long:  "Opens a Server-Sent Events stream and prints graph object create/update/delete events as they occur.",
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		stream, err := g.StreamEvents(ctx)
		if err != nil {
			return err
		}
		defer stream.Close()

		reader := bufio.NewReader(stream)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return fmt.Errorf("stream error: %w", err)
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				if data == "" || data == "heartbeat" {
					continue
				}
				fmt.Fprintln(cmd.OutOrStdout(), data)
			}
		}
	},
}

// ─────────────────────────────────────────────
// init — wire up the command tree
// ─────────────────────────────────────────────

func init() {
	// Persistent project flag
	graphCmd.PersistentFlags().StringVar(&graphProjectFlag, "project", "", "Project ID (overrides config/env)")
	graphCmd.PersistentFlags().StringVar(&graphOutputFlag, "output", "table", "Output format: table or json")

	// Object subcommand flags
	graphObjectsListCmd.Flags().StringVar(&graphTypeFlag, "type", "", "Filter by object type")
	graphObjectsListCmd.Flags().IntVar(&graphLimitFlag, "limit", 1000, "Maximum number of results (server default: 1000)")
	graphObjectsListCmd.Flags().StringVar(&graphCursorFlag, "cursor", "", "Pagination cursor from a previous response (next_cursor field)")
	graphObjectsListCmd.Flags().StringArrayVar(&graphFilterFlag, "filter", nil, "Property filter as key=value (repeatable); see --filter-op")
	graphObjectsListCmd.Flags().StringVar(&graphFilterOpFlag, "filter-op", "eq", "Operator for --filter: eq, neq, gt, gte, lt, lte, contains, in, exists")
	graphObjectsListCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch ID to scope results to (omit for main branch)")
	graphObjectsListCmd.Flags().StringVar(&graphStatusFlag, "status", "", "Filter by object status")
	graphObjectsListCmd.Flags().StringVar(&graphIDsFlag, "ids", "", "Fetch specific objects by ID (comma-separated: --ids id1,id2,id3)")
	graphObjectsListCmd.Flags().StringVar(&graphKeyFlag, "key", "", "Filter by object key (direct key-based lookup)")
	graphObjectsListCmd.Flags().StringVar(&graphActorTypeFlag, "actor-type", "", "Filter by actor type: user, agent, or system")
	graphObjectsListCmd.Flags().StringVar(&graphActorIDFlag, "actor-id", "", "Filter by actor ID (UUID; requires --actor-type)")
	graphObjectsListCmd.Flags().StringVar(&graphProvenanceFlag, "provenance", "", "Which version row must carry the actor/write-stamp filter: updated (current version, default for write stamps), created, or any")
	graphObjectsListCmd.Flags().StringVar(&graphRunIDFlag, "run-id", "", "Filter by write-stamp run ID (matches the run or any run it delegated to)")
	graphObjectsListCmd.Flags().StringVar(&graphCredentialIDFlag, "credential-id", "", "Filter by the credential (API token) that wrote the rows")
	graphObjectsListCmd.Flags().StringVar(&graphCredentialKindFlag, "credential-kind", "", "Filter by credential kind: api_key, mcp_link, session, none")
	graphObjectsListCmd.Flags().StringVar(&graphDataClassFlag, "data-class", "", "Filter by data class: source, synthesized, adopted")

	graphObjectsGetCmd.Flags().StringVar(&graphOutputFlag, "output", "table", "Output format: table or json")
	graphObjectsGetCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch ID or name to resolve the key against (omit for main branch)")

	graphObjectsCreateCmd.Flags().StringVar(&graphTypeFlag, "type", "", "Object type (required)")
	graphObjectsCreateCmd.Flags().StringVar(&graphNameFlag, "name", "", "Shorthand for --properties '{\"name\": \"...\"}' (subject to schema validation)")
	graphObjectsCreateCmd.Flags().StringVar(&graphDescFlag, "description", "", "Shorthand for --properties '{\"description\": \"...\"}' (subject to schema validation)")
	graphObjectsCreateCmd.Flags().StringVar(&graphPropsFlag, "properties", "", "JSON properties object")
	graphObjectsCreateCmd.Flags().StringVar(&graphKeyFlag, "key", "", "Stable key for idempotent operations")
	graphObjectsCreateCmd.Flags().StringVar(&graphStatusFlag, "status", "", "Object status (e.g. active, planned, deprecated)")
	graphObjectsCreateCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch ID to create the object on (omit for main branch)")
	graphObjectsCreateCmd.Flags().BoolVar(&graphUpsertFlag, "upsert", false, "Update existing object if key already exists (requires --key)")

	graphObjectsCreateBatchCmd.Flags().StringVar(&graphBatchFile, "file", "", "JSON file (array or subgraph), blueprint dump directory, or seed .jsonl file (required)")
	graphObjectsCreateBatchCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch name or UUID to import into (omit for main branch)")

	graphObjectsUpdateCmd.Flags().StringVar(&graphPropsFlag, "properties", "", "JSON properties object to merge")
	graphObjectsUpdateCmd.Flags().StringVar(&graphNameFlag, "name", "", "Shorthand for --properties '{\"name\": \"...\"}' (subject to schema validation)")
	graphObjectsUpdateCmd.Flags().StringVar(&graphDescFlag, "description", "", "Shorthand for --properties '{\"description\": \"...\"}' (subject to schema validation)")
	graphObjectsUpdateCmd.Flags().StringVar(&graphKeyFlag, "key", "", "Set a stable key on the object (enables cross-session src_key/dst_key references)")
	graphObjectsUpdateCmd.Flags().StringVar(&graphStatusFlag, "status", "", "Set object status (e.g. active, planned, deprecated)")
	graphObjectsUpdateCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch ID to update the object on (omit for main branch)")

	// Relationship subcommand flags
	graphRelationshipsListCmd.Flags().StringVar(&graphRelTypeFlag, "type", "", "Filter by relationship type")
	graphRelationshipsListCmd.Flags().StringVar(&graphFromFlag, "from", "", "Filter by source object ID")
	graphRelationshipsListCmd.Flags().StringVar(&graphToFlag, "to", "", "Filter by destination object ID")
	graphRelationshipsListCmd.Flags().IntVar(&graphLimitFlag, "limit", 1000, "Maximum number of results (server default: 1000)")
	graphRelationshipsListCmd.Flags().StringVar(&graphCursorFlag, "cursor", "", "Pagination cursor from a previous response (next_cursor field)")
	graphRelationshipsListCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch ID to scope results to (omit for main branch)")
	graphRelationshipsListCmd.Flags().StringVar(&graphRunIDFlag, "run-id", "", "Filter by write-stamp run ID (matches the run or any run it delegated to)")
	graphRelationshipsListCmd.Flags().StringVar(&graphCredentialIDFlag, "credential-id", "", "Filter by the credential (API token) that wrote the rows")
	graphRelationshipsListCmd.Flags().StringVar(&graphCredentialKindFlag, "credential-kind", "", "Filter by credential kind: api_key, mcp_link, session, none")
	graphRelationshipsListCmd.Flags().StringVar(&graphProvenanceFlag, "provenance", "", "Which version row must carry the write-stamp filter: updated (current version, default), created, or any")

	graphRelationshipsDeleteCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch name or ID to scope deletion to (omit for main branch)")

	graphObjectsDeleteCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch name or ID to scope deletion to (omit for main branch)")
	graphObjectsDeleteCmd.Flags().BoolVarP(&graphForceFlag, "force", "f", false, "Skip confirmation prompt (accepted for scripting compatibility)")

	graphRelationshipsCreateCmd.Flags().StringVar(&graphRelTypeFlag, "type", "", "Relationship type (required)")
	graphRelationshipsCreateCmd.Flags().StringVar(&graphFromFlag, "from", "", "Source object ID (required)")
	graphRelationshipsCreateCmd.Flags().StringVar(&graphToFlag, "to", "", "Destination object ID (required)")
	graphRelationshipsCreateCmd.Flags().StringVar(&graphPropsFlag, "properties", "", "JSON properties object")
	graphRelationshipsCreateCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch ID to create the relationship on (omit for main branch)")
	graphRelationshipsCreateCmd.Flags().BoolVar(&graphUpsertFlag, "upsert", false, "Return existing relationship if (type, from, to) already exists instead of creating a duplicate")

	graphRelationshipsCreateBatchCmd.Flags().StringVar(&graphBatchFile, "file", "", "Path to JSON file containing array of relationships (required)")
	graphRelationshipsCreateBatchCmd.Flags().BoolVar(&graphUpsertFlag, "upsert", false, "Apply upsert semantics to all items: skip existing relationships instead of failing")
	graphRelationshipsCreateBatchCmd.Flags().StringVar(&graphBranchFlag, "branch", "", "Branch name or UUID to create relationships on (omit for main branch)")

	// Assemble objects subcommands
	graphObjectsSimilarCmd.Flags().IntVar(&graphSimilarLimitFlag, "limit", 10, "Maximum number of similar objects to return")
	graphObjectsSimilarCmd.Flags().StringVar(&graphSimilarTypeFlag, "type", "", "Filter results by object type")
	graphObjectsSimilarCmd.Flags().Float64Var(&graphSimilarMinScoreFlag, "min-score", 0, "Minimum similarity score (0–1); 0 means no threshold")
	graphObjectsSimilarCmd.Flags().StringVar(&graphOutputFlag, "output", "table", "Output format: table or json")

	graphObjectsMoveCmd.Flags().StringVar(&graphMoveTargetBranchFlag, "target-branch", "", "Target branch UUID (use 'main' or omit for main branch)")
	graphObjectsMoveCmd.Flags().StringVar(&graphOutputFlag, "output", "table", "Output format: table or json")

	graphObjectsUpdateBatchCmd.Flags().StringVar(&graphBatchFile, "file", "", "Path to JSON file containing array of object updates (required)")
	graphObjectsUpdateBatchCmd.Flags().StringVar(&graphOutputFlag, "output", "table", "Output format: table or json")

	// bulk-update flags
	graphObjectsBulkUpdateCmd.Flags().StringVar(&graphBulkActionFlag, "action", "", "Action to perform: update_status, soft_delete, hard_delete, merge_properties, replace_properties, add_labels, remove_labels, set_labels (required)")
	graphObjectsBulkUpdateCmd.Flags().StringVar(&graphBulkValueFlag, "value", "", "Value for update_status action")
	graphObjectsBulkUpdateCmd.Flags().StringArrayVar(&graphBulkLabelsFlag, "labels", nil, "Labels for add_labels/remove_labels/set_labels actions")
	graphObjectsBulkUpdateCmd.Flags().BoolVar(&graphBulkDryRunFlag, "dry-run", false, "Preview matched count without making changes")
	graphObjectsBulkUpdateCmd.Flags().IntVar(&graphBulkLimitFlag, "limit", 0, "Maximum number of objects to affect (default 1000, max 100000; 0 = server default)")
	graphObjectsBulkUpdateCmd.Flags().StringArrayVar(&graphFilterFlag, "filter", nil, "Property filter key=value (repeatable)")
	graphObjectsBulkUpdateCmd.Flags().StringVar(&graphFilterOpFlag, "filter-op", "eq", "Operator for --filter: eq, neq, gt, gte, lt, lte, contains, in, exists")
	graphObjectsBulkUpdateCmd.Flags().StringVar(&graphTypeFlag, "type", "", "Filter by object type (comma-separated)")
	graphObjectsBulkUpdateCmd.Flags().StringVar(&graphPropsFlag, "properties", "", "JSON properties for merge_properties/replace_properties actions")
	_ = graphObjectsBulkUpdateCmd.MarkFlagRequired("action")

	// bulk-delete flags
	graphObjectsBulkDeleteCmd.Flags().BoolVar(&graphBulkDryRunFlag, "dry-run", false, "Preview matched count without deleting")
	graphObjectsBulkDeleteCmd.Flags().IntVar(&graphBulkLimitFlag, "limit", 0, "Maximum number of objects to delete (default 1000, max 100000; 0 = server default)")
	graphObjectsBulkDeleteCmd.Flags().StringArrayVar(&graphFilterFlag, "filter", nil, "Property filter key=value (repeatable)")
	graphObjectsBulkDeleteCmd.Flags().StringVar(&graphFilterOpFlag, "filter-op", "eq", "Operator for --filter: eq, neq, gt, gte, lt, lte, contains, in, exists")
	graphObjectsBulkDeleteCmd.Flags().StringVar(&graphTypeFlag, "type", "", "Filter by object type (comma-separated)")

	graphObjectsCmd.AddCommand(graphObjectsListCmd)
	graphObjectsCmd.AddCommand(graphObjectsGetCmd)
	graphObjectsCmd.AddCommand(graphObjectsCreateCmd)
	graphObjectsCmd.AddCommand(graphObjectsCreateBatchCmd)
	graphObjectsCmd.AddCommand(graphObjectsUpdateCmd)
	graphObjectsCmd.AddCommand(graphObjectsUpdateBatchCmd)
	graphObjectsCmd.AddCommand(graphObjectsDeleteCmd)
	graphObjectsCmd.AddCommand(graphObjectsEdgesCmd)
	graphObjectsCmd.AddCommand(graphObjectsSimilarCmd)
	graphObjectsCmd.AddCommand(graphObjectsMoveCmd)
	graphObjectsCmd.AddCommand(graphObjectsBulkUpdateCmd)
	graphObjectsCmd.AddCommand(graphObjectsBulkDeleteCmd)

	// Assemble relationships subcommands
	graphRelationshipsCmd.AddCommand(graphRelationshipsListCmd)
	graphRelationshipsCmd.AddCommand(graphRelationshipsGetCmd)
	graphRelationshipsCmd.AddCommand(graphRelationshipsCreateCmd)
	graphRelationshipsCmd.AddCommand(graphRelationshipsCreateBatchCmd)
	graphRelationshipsCmd.AddCommand(graphRelationshipsDeleteCmd)

	// Assemble top-level graph command
	graphCmd.AddCommand(graphObjectsCmd)
	graphCmd.AddCommand(graphRelationshipsCmd)
	graphCmd.AddCommand(graphBranchesCmd)
	graphCmd.AddCommand(graphEventsCmd)

	rootCmd.AddCommand(graphCmd)

}
