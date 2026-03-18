package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// batchObjectItem is the JSON shape accepted by graph objects create-batch.
type batchObjectItem struct {
	Type        string         `json:"type"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

// batchRelationshipItem is the JSON shape accepted by graph relationships create-batch.
type batchRelationshipItem struct {
	Type       string         `json:"type"`
	From       string         `json:"from"`
	To         string         `json:"to"`
	Properties map[string]any `json:"properties,omitempty"`
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
	graphProjectFlag string
	graphOutputFlag  string
	graphLimitFlag   int
	graphTypeFlag    string
	graphNameFlag    string
	graphDescFlag    string
	graphPropsFlag   string
	graphKeyFlag     string
	graphUpsertFlag  bool
	graphFromFlag    string
	graphToFlag      string
	graphRelTypeFlag string
	graphBatchFile   string
)

// ─────────────────────────────────────────────
// Helper: resolve project + set context on client
// ─────────────────────────────────────────────

func getGraphClient(cmd *cobra.Command) (*sdkgraph.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, err
	}

	projectID, err := resolveProjectContext(cmd, graphProjectFlag)
	if err != nil {
		return nil, err
	}

	c.SetContext("", projectID)
	return c.SDK.Graph, nil
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
--output json to receive the full list as JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		opts := &sdkgraph.ListObjectsOptions{}
		if graphTypeFlag != "" {
			opts.Type = graphTypeFlag
		}
		if graphLimitFlag > 0 {
			opts.Limit = graphLimitFlag
		}

		resp, err := g.ListObjects(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(resp.Items)
		}

		if len(resp.Items) == 0 {
			fmt.Fprintln(out, "No objects found.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("Entity ID", "Type", "Version", "Status", "Created")
		for _, obj := range resp.Items {
			status := ""
			if obj.Status != nil {
				status = *obj.Status
			}
			_ = table.Append(
				obj.EntityID,
				obj.Type,
				fmt.Sprintf("%d", obj.Version),
				status,
				obj.CreatedAt.Format("2006-01-02"),
			)
		}
		return table.Render()
	},
}

// ─────────────────────────────────────────────
// graph objects get
// ─────────────────────────────────────────────

var graphObjectsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a graph object by ID",
	Long: `Get details for a graph object (entity) by its ID.

Prints Entity ID, Version ID, Type, Version number, Key (if set), Status (if
set), Labels (if any), Created timestamp, and Properties as formatted JSON.
Use --output json to receive the full object as JSON instead.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		obj, err := g.GetObject(context.Background(), args[0])
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
		fmt.Fprintf(out, "Created:     %s\n", obj.CreatedAt.Format("2006-01-02 15:04:05"))
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
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		req := &sdkgraph.UpdateObjectRequest{}

		if graphPropsFlag != "" {
			var props map[string]any
			if err := json.Unmarshal([]byte(graphPropsFlag), &props); err != nil {
				return fmt.Errorf("invalid --properties JSON: %w", err)
			}
			req.Properties = props
		}

		obj, err := g.UpdateObject(context.Background(), args[0], req)
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
// graph objects delete
// ─────────────────────────────────────────────

var graphObjectsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a graph object",
	Long:  "Soft-delete a graph object by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		if err := g.DeleteObject(context.Background(), args[0]); err != nil {
			return fmt.Errorf("failed to delete object: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Object %s deleted.\n", args[0])
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
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		edges, err := g.GetObjectEdges(context.Background(), args[0], nil)
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
// graph relationships list
// ─────────────────────────────────────────────

var graphRelationshipsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List relationships",
	Long: `List relationships in the current project.

Output is a table with columns: Entity ID, Type, From (source entity ID), To
(destination entity ID), and Created date. Use --type to filter by relationship
type, --from/--to to filter by endpoint, --limit to control result count, and
--output json to receive the full list as JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		opts := &sdkgraph.ListRelationshipsOptions{}
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

		resp, err := g.ListRelationships(context.Background(), opts)
		if err != nil {
			return fmt.Errorf("failed to list relationships: %w", err)
		}

		out := cmd.OutOrStdout()

		if graphOutputFlag == "json" {
			return json.NewEncoder(out).Encode(resp.Items)
		}

		if len(resp.Items) == 0 {
			fmt.Fprintln(out, "No relationships found.")
			return nil
		}

		table := tablewriter.NewWriter(out)
		table.Header("Entity ID", "Type", "From", "To", "Created")
		for _, r := range resp.Items {
			_ = table.Append(
				r.EntityID,
				r.Type,
				r.SrcID,
				r.DstID,
				r.CreatedAt.Format("2006-01-02"),
			)
		}
		return table.Render()
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
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		r, err := g.GetRelationship(context.Background(), args[0])
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
		fmt.Fprintf(out, "Created:    %s\n", r.CreatedAt.Format("2006-01-02 15:04:05"))
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
	Long:  "Create a directed relationship between two graph objects",
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

		if graphPropsFlag != "" {
			var props map[string]any
			if err := json.Unmarshal([]byte(graphPropsFlag), &props); err != nil {
				return fmt.Errorf("invalid --properties JSON: %w", err)
			}
			req.Properties = props
		}

		r, err := g.CreateRelationship(context.Background(), req)
		if err != nil {
			return fmt.Errorf("failed to create relationship: %w", err)
		}

		out := cmd.OutOrStdout()

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
	Long:  "Soft-delete a graph relationship by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		if err := g.DeleteRelationship(context.Background(), args[0]); err != nil {
			return fmt.Errorf("failed to delete relationship: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Relationship %s deleted.\n", args[0])
		return nil
	},
}

// ─────────────────────────────────────────────
// graph objects create-batch
// ─────────────────────────────────────────────

var graphObjectsCreateBatchCmd = &cobra.Command{
	Use:   "create-batch",
	Short: "Batch-create graph objects from a JSON file",
	Long: `Create multiple graph objects in one API call.

The input file must contain a JSON array of objects, each with:
  type        (string, required)
  name        (string, optional) — placed in properties.name
  description (string, optional) — placed in properties.description
  properties  (object, optional) — arbitrary additional properties

Example objects.json:
  [
    {"type": "Person", "name": "Alice"},
    {"type": "Person", "name": "Bob", "description": "A developer"},
    {"type": "Project", "name": "Acme", "properties": {"status": "active"}}
  ]

Output (one line per object): <entity-id>  <type>  <name>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if graphBatchFile == "" {
			return fmt.Errorf("--file is required")
		}

		data, err := os.ReadFile(graphBatchFile)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		var items []batchObjectItem
		if err := json.Unmarshal(data, &items); err != nil {
			return fmt.Errorf("parsing JSON: %w", err)
		}
		if len(items) == 0 {
			return fmt.Errorf("file contains no items")
		}

		reqs := make([]sdkgraph.CreateObjectRequest, 0, len(items))
		for _, item := range items {
			req := sdkgraph.CreateObjectRequest{
				Type: item.Type,
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

		g, err := getGraphClient(cmd)
		if err != nil {
			return err
		}

		resp, err := g.BulkCreateObjects(context.Background(), &sdkgraph.BulkCreateObjectsRequest{
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
				Type:  item.Type,
				SrcID: item.From,
				DstID: item.To,
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
// init — wire up the command tree
// ─────────────────────────────────────────────

func init() {
	// Persistent project flag
	graphCmd.PersistentFlags().StringVar(&graphProjectFlag, "project", "", "Project ID (overrides config/env)")
	graphCmd.PersistentFlags().StringVar(&graphOutputFlag, "output", "table", "Output format: table or json")

	// Object subcommand flags
	graphObjectsListCmd.Flags().StringVar(&graphTypeFlag, "type", "", "Filter by object type")
	graphObjectsListCmd.Flags().IntVar(&graphLimitFlag, "limit", 50, "Maximum number of results")

	graphObjectsGetCmd.Flags().StringVar(&graphOutputFlag, "output", "table", "Output format: table or json")

	graphObjectsCreateCmd.Flags().StringVar(&graphTypeFlag, "type", "", "Object type (required)")
	graphObjectsCreateCmd.Flags().StringVar(&graphNameFlag, "name", "", "Set properties.name")
	graphObjectsCreateCmd.Flags().StringVar(&graphDescFlag, "description", "", "Set properties.description")
	graphObjectsCreateCmd.Flags().StringVar(&graphPropsFlag, "properties", "", "JSON properties object")
	graphObjectsCreateCmd.Flags().StringVar(&graphKeyFlag, "key", "", "Stable key for idempotent operations")
	graphObjectsCreateCmd.Flags().BoolVar(&graphUpsertFlag, "upsert", false, "Update existing object if key already exists (requires --key)")

	graphObjectsCreateBatchCmd.Flags().StringVar(&graphBatchFile, "file", "", "Path to JSON file containing array of objects (required)")

	graphObjectsUpdateCmd.Flags().StringVar(&graphPropsFlag, "properties", "", "JSON properties object to merge")

	// Relationship subcommand flags
	graphRelationshipsListCmd.Flags().StringVar(&graphRelTypeFlag, "type", "", "Filter by relationship type")
	graphRelationshipsListCmd.Flags().StringVar(&graphFromFlag, "from", "", "Filter by source object ID")
	graphRelationshipsListCmd.Flags().StringVar(&graphToFlag, "to", "", "Filter by destination object ID")
	graphRelationshipsListCmd.Flags().IntVar(&graphLimitFlag, "limit", 50, "Maximum number of results")

	graphRelationshipsCreateCmd.Flags().StringVar(&graphRelTypeFlag, "type", "", "Relationship type (required)")
	graphRelationshipsCreateCmd.Flags().StringVar(&graphFromFlag, "from", "", "Source object ID (required)")
	graphRelationshipsCreateCmd.Flags().StringVar(&graphToFlag, "to", "", "Destination object ID (required)")
	graphRelationshipsCreateCmd.Flags().StringVar(&graphPropsFlag, "properties", "", "JSON properties object")

	graphRelationshipsCreateBatchCmd.Flags().StringVar(&graphBatchFile, "file", "", "Path to JSON file containing array of relationships (required)")

	// Assemble objects subcommands
	graphObjectsCmd.AddCommand(graphObjectsListCmd)
	graphObjectsCmd.AddCommand(graphObjectsGetCmd)
	graphObjectsCmd.AddCommand(graphObjectsCreateCmd)
	graphObjectsCmd.AddCommand(graphObjectsCreateBatchCmd)
	graphObjectsCmd.AddCommand(graphObjectsUpdateCmd)
	graphObjectsCmd.AddCommand(graphObjectsDeleteCmd)
	graphObjectsCmd.AddCommand(graphObjectsEdgesCmd)

	// Assemble relationships subcommands
	graphRelationshipsCmd.AddCommand(graphRelationshipsListCmd)
	graphRelationshipsCmd.AddCommand(graphRelationshipsGetCmd)
	graphRelationshipsCmd.AddCommand(graphRelationshipsCreateCmd)
	graphRelationshipsCmd.AddCommand(graphRelationshipsCreateBatchCmd)
	graphRelationshipsCmd.AddCommand(graphRelationshipsDeleteCmd)

	// Assemble top-level graph command
	graphCmd.AddCommand(graphObjectsCmd)
	graphCmd.AddCommand(graphRelationshipsCmd)

	rootCmd.AddCommand(graphCmd)

	// Suppress unused import warning for os
	_ = os.Stderr
}
