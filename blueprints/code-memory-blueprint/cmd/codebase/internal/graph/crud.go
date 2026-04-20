package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/output"
	"github.com/spf13/cobra"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var fallbackTypes = []string{
	"Scenario", "Context", "Action", "APIEndpoint", "ServiceMethod",
	"SQLQuery", "SourceFile", "ScenarioStep", "UIComponent", "Actor",
	"Domain", "TestSuite", "Pattern", "Rule", "Middleware",
}

func getByKey(ctx context.Context, gc *sdkgraph.Client, key string) (*sdkgraph.GraphObject, error) {
	res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: key, Limit: 1})
	if err != nil {
		return nil, err
	}
	for _, o := range res.Items {
		if o.Key != nil && *o.Key == key {
			return o, nil
		}
	}
	return nil, fmt.Errorf("object with key %q not found", key)
}

func getByKeyAndType(ctx context.Context, gc *sdkgraph.Client, key, objType string) (*sdkgraph.GraphObject, error) {
	res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: key, Type: objType, Limit: 1})
	if err != nil {
		return nil, err
	}
	for _, o := range res.Items {
		if o.Key != nil && *o.Key == key {
			return o, nil
		}
	}
	return nil, fmt.Errorf("object %q of type %s not found", key, objType)
}

func runList(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	var allItems []*sdkgraph.GraphObject
	cursor := flagCursor
	limit := flagLimit
	if flagAll {
		limit = 500
	}

	for {
		opts := &sdkgraph.ListObjectsOptions{
			Type:   flagType,
			Limit:  limit,
			Cursor: cursor,
			Key:    flagKey, // Use SDK key filter if provided
		}
		res, err := c.Graph.ListObjects(ctx, opts)
		if err != nil {
			return err
		}
		allItems = append(allItems, res.Items...)
		if !flagAll || res.NextCursor == nil || *res.NextCursor == "" {
			break
		}
		cursor = *res.NextCursor
	}

	// Client-side filters for prefix/case-insensitive if flagKey was used
	if flagKey != "" {
		filtered := allItems[:0]
		for _, item := range allItems {
			if item.Key != nil && (strings.EqualFold(*item.Key, flagKey) || strings.HasPrefix(*item.Key, flagKey)) {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	for _, f := range flagFilter {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 {
			continue
		}
		filterKey, filterVal := parts[0], parts[1]
		filtered := allItems[:0]
		for _, item := range allItems {
			if filterKey == "key" {
				if item.Key != nil && (strings.EqualFold(*item.Key, filterVal) || strings.Contains(*item.Key, filterVal)) {
					filtered = append(filtered, item)
				}
				continue
			}
			if item.Properties != nil {
				if v, ok := item.Properties[filterKey]; ok {
					if fmt.Sprintf("%v", v) == filterVal {
						filtered = append(filtered, item)
					}
				}
			}
		}
		allItems = filtered
	}

	if flagStatus != "" {
		filtered := allItems[:0]
		for _, item := range allItems {
			if item.Status != nil && *item.Status == flagStatus {
				filtered = append(filtered, item)
			}
		}
		allItems = filtered
	}

	if flagCount {
		fmt.Fprintln(cmd.OutOrStdout(), len(allItems))
		return nil
	}

	if flagListJSON {
		data, err := json.MarshalIndent(map[string]interface{}{"items": allItems, "count": len(allItems)}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	if flagVerbose {
		for _, item := range allItems {
			key := ""
			if item.Key != nil {
				key = *item.Key
			}
			status := ""
			if item.Status != nil {
				status = *item.Status
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-15s  %s", item.Type, key)
			if status != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s]", status)
			}
			fmt.Fprintln(cmd.OutOrStdout())

			keys := make([]string, 0, len(item.Properties))
			for k := range item.Properties {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %v\n", k+":", item.Properties[k])
			}
			fmt.Fprintln(cmd.OutOrStdout())
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%d objects\n", len(allItems))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%-15s %-40s %-12s %-15s\n", "TYPE", "KEY", "STATUS", "ID")
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("─", 84))
	for _, item := range allItems {
		key := ""
		if item.Key != nil {
			key = *item.Key
		}
		status := ""
		if item.Status != nil {
			status = *item.Status
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-15s %-40s %-12s %-15s\n", item.Type, key, status, output.ShortID(item.EntityID))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n%d objects\n", len(allItems))
	return nil
}

func runGet(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	var obj *sdkgraph.GraphObject
	input := flagKey
	if input == "" && len(args) > 0 {
		input = args[0]
	}

	if input == "" {
		return fmt.Errorf("ID or --key is required")
	}

	if uuidRE.MatchString(input) {
		obj, err = c.Graph.GetObject(ctx, input)
	} else {
		obj, err = getByKey(ctx, c.Graph, input)
		if err != nil {
			for _, t := range fallbackTypes {
				if o, e2 := getByKeyAndType(ctx, c.Graph, input, t); e2 == nil {
					obj = o
					err = nil
					break
				}
			}
		}
	}

	if err != nil {
		return fmt.Errorf("object %q not found", input)
	}

	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func runCreate(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	var props map[string]interface{}
	if err := json.Unmarshal([]byte(flagProperties), &props); err != nil {
		return fmt.Errorf("invalid properties JSON: %w", err)
	}

	req := &sdkgraph.CreateObjectRequest{
		Type:       flagType,
		Key:        &flagKey,
		Properties: props,
	}
	if flagStatus != "" {
		req.Status = &flagStatus
	}

	var obj *sdkgraph.GraphObject
	if flagUpsert {
		obj, err = c.Graph.UpsertObject(ctx, req)
	} else {
		obj, err = c.Graph.CreateObject(ctx, req)
	}
	if err != nil {
		return err
	}

	if flagCreateJSON {
		data, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), obj.EntityID)
	return nil
}

func runUpdate(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	var props map[string]interface{}
	if flagProperties != "{}" && flagProperties != "" {
		if err := json.Unmarshal([]byte(flagProperties), &props); err != nil {
			return fmt.Errorf("invalid properties JSON: %w", err)
		}
	}

	req := &sdkgraph.UpdateObjectRequest{Properties: props}
	if flagStatus != "" {
		req.Status = &flagStatus
	}

	var obj *sdkgraph.GraphObject
	if flagID != "" {
		obj, err = c.Graph.UpdateObject(ctx, flagID, req)
	} else if flagKey != "" {
		target, err := getByKey(ctx, c.Graph, flagKey)
		if err != nil {
			return err
		}
		obj, err = c.Graph.UpdateObject(ctx, target.EntityID, req)
	} else {
		return fmt.Errorf("either --id or --key is required")
	}

	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), obj.EntityID)
	return nil
}

func runRelate(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	req := &sdkgraph.CreateRelationshipRequest{
		Type:  flagType,
		SrcID: flagFrom,
		DstID: flagTo,
	}

	var rel *sdkgraph.GraphRelationship
	if flagUpsert {
		rel, err = c.Graph.UpsertRelationship(ctx, req)
	} else {
		rel, err = c.Graph.CreateRelationship(ctx, req)
	}

	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), rel.EntityID)
	return nil
}

func runUnrelate(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	var relID string
	if len(args) == 1 {
		relID = args[0]
	} else if flagFrom != "" && flagTo != "" {
		if flagRelType == "" {
			return fmt.Errorf("--type is required for lookup")
		}
		res, err := c.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{
			SrcID: flagFrom,
			DstID: flagTo,
			Type:  flagRelType,
		})
		if err != nil || len(res.Items) == 0 {
			return fmt.Errorf("relationship not found")
		}
		relID = res.Items[0].EntityID
	} else {
		return fmt.Errorf("provide rel-id or --from/--to/--type")
	}

	return c.Graph.DeleteRelationship(ctx, relID)
}

func runDelete(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	if len(args) == 0 {
		return fmt.Errorf("ID or key is required")
	}
	input := args[0]

	var obj *sdkgraph.GraphObject
	if uuidRE.MatchString(input) {
		obj, err = c.Graph.GetObject(ctx, input)
	} else if flagType != "" {
		obj, err = getByKeyAndType(ctx, c.Graph, input, flagType)
	} else {
		obj, err = getByKey(ctx, c.Graph, input)
		if err != nil {
			for _, t := range fallbackTypes {
				if o, e2 := getByKeyAndType(ctx, c.Graph, input, t); e2 == nil {
					obj = o
					err = nil
					break
				}
			}
		}
	}

	if err != nil {
		return fmt.Errorf("object %q not found", input)
	}

	return c.Graph.DeleteObject(ctx, obj.EntityID, nil)
}

func runRename(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	oldKey, newKey := args[0], args[1]
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	obj, err := getByKey(ctx, c.Graph, oldKey)
	if err != nil {
		for _, t := range fallbackTypes {
			if o, e2 := getByKeyAndType(ctx, c.Graph, oldKey, t); e2 == nil {
				obj = o
				err = nil
				break
			}
		}
	}
	if err != nil {
		return fmt.Errorf("object %q not found", oldKey)
	}

	outRels, _ := c.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{SrcID: obj.EntityID, Limit: 1000})
	inRels, _ := c.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{DstID: obj.EntityID, Limit: 1000})

	if flagDryRun {
		fmt.Printf("[DRY RUN] Rename %s -> %s (%s)\n", oldKey, newKey, obj.Type)
		return nil
	}

	newObj, err := c.Graph.CreateObject(ctx, &sdkgraph.CreateObjectRequest{
		Type:       obj.Type,
		Key:        &newKey,
		Properties: obj.Properties,
		Status:     obj.Status,
	})
	if err != nil {
		return err
	}

	for _, r := range outRels.Items {
		c.Graph.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{Type: r.Type, SrcID: newObj.EntityID, DstID: r.DstID})
	}
	for _, r := range inRels.Items {
		c.Graph.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{Type: r.Type, SrcID: r.SrcID, DstID: newObj.EntityID})
	}

	return c.Graph.DeleteObject(ctx, obj.EntityID, nil)
}

func runPrune(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	res, err := c.Graph.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Limit: 1000})
	if err != nil {
		return err
	}

	for _, obj := range res.Items {
		if obj.Type == "Scenario" {
			continue
		}
		out, _ := c.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{SrcID: obj.EntityID, Limit: 1})
		in, _ := c.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{DstID: obj.EntityID, Limit: 1})

		if len(out.Items) == 0 && len(in.Items) == 0 {
			if flagDryRun {
				key := obj.Type + ":" + output.ShortID(obj.EntityID)
				if obj.Key != nil && *obj.Key != "" {
					key = *obj.Key
				}
				fmt.Printf("[DRY RUN] Prune %s (%s)\n", key, obj.Type)
			} else {
				c.Graph.DeleteObject(ctx, obj.EntityID, nil)
			}
		}
	}
	return nil
}

func runTree(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()

	if flagType != "" {
		res, err := c.Graph.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: flagType, Limit: 500})
		if err != nil {
			return err
		}
		for _, obj := range res.Items {
			key := obj.Type + ":" + output.ShortID(obj.EntityID)
			if obj.Key != nil && *obj.Key != "" {
				key = *obj.Key
			}
			fmt.Printf("%s (%s)\n", key, obj.Type)
			rels, _ := c.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{SrcID: obj.EntityID, Limit: 20})
			for _, r := range rels.Items {
				dst, _ := c.Graph.GetObject(ctx, r.DstID)
				dstKey := r.DstID
				if dst != nil && dst.Key != nil {
					dstKey = *dst.Key
				}
				fmt.Printf("  └── %s -> %s\n", r.Type, dstKey)
			}
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("key or ID required")
	}
	input := args[0]
	var root *sdkgraph.GraphObject
	if uuidRE.MatchString(input) {
		root, _ = c.Graph.GetObject(ctx, input)
	} else {
		root, _ = getByKey(ctx, c.Graph, input)
	}
	if root == nil {
		return fmt.Errorf("object not found")
	}

	type node struct {
		obj   *sdkgraph.GraphObject
		depth int
	}
	queue := []node{{root, 0}}
	seen := map[string]bool{root.EntityID: true}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		indent := strings.Repeat("  ", curr.depth)
		key := curr.obj.Type + ":" + output.ShortID(curr.obj.EntityID)
		if curr.obj.Key != nil && *curr.obj.Key != "" {
			key = *curr.obj.Key
		}
		fmt.Printf("%s%s (%s)\n", indent, key, curr.obj.Type)

		if curr.depth >= flagDepth {
			continue
		}

		rels, _ := c.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{SrcID: curr.obj.EntityID, Limit: 50})
		for _, r := range rels.Items {
			if seen[r.DstID] {
				continue
			}
			seen[r.DstID] = true
			dst, _ := c.Graph.GetObject(ctx, r.DstID)
			if dst != nil {
				queue = append(queue, node{dst, curr.depth + 1})
			}
		}
	}

	return nil
}

type batchOp struct {
	Op     string                 `json:"op"`
	Type   string                 `json:"type"`
	Key    string                 `json:"key"`
	Status string                 `json:"status"`
	Props  map[string]interface{} `json:"props"`
	From   string                 `json:"from"`
	To     string                 `json:"to"`
}

func runCreateBatch(cmd *cobra.Command, args []string, flagProjectID *string, flagBranch *string) error {
	var data []byte
	var err error
	if batchFile != "" {
		data, err = os.ReadFile(batchFile)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		return err
	}

	var ops []batchOp
	if err := json.Unmarshal(data, &ops); err != nil {
		return err
	}

	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}
	ctx := context.Background()
	cache := map[string]string{}

	for i, op := range ops {
		if op.Op == "create" {
			req := &sdkgraph.CreateObjectRequest{Type: op.Type, Key: &op.Key, Properties: op.Props}
			if op.Status != "" {
				req.Status = &op.Status
			}
			obj, err := c.Graph.UpsertObject(ctx, req)
			if err != nil {
				if batchFailFast {
					return err
				}
				fmt.Fprintf(cmd.OutOrStderr(), "Error [%d]: %v\n", i, err)
				continue
			}
			cache[op.Key] = obj.EntityID
			fmt.Printf("created %s %s\n", op.Key, obj.EntityID)
		} else if op.Op == "relate" {
			srcID := op.From
			if id, ok := cache[op.From]; ok {
				srcID = id
			} else if !uuidRE.MatchString(op.From) {
				if o, _ := getByKey(ctx, c.Graph, op.From); o != nil {
					srcID = o.EntityID
				}
			}
			dstID := op.To
			if id, ok := cache[op.To]; ok {
				dstID = id
			} else if !uuidRE.MatchString(op.To) {
				if o, _ := getByKey(ctx, c.Graph, op.To); o != nil {
					dstID = o.EntityID
				}
			}
			rel, err := c.Graph.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{Type: op.Type, SrcID: srcID, DstID: dstID})
			if err != nil {
				if batchFailFast {
					return err
				}
				fmt.Fprintf(cmd.OutOrStderr(), "Error [%d]: %v\n", i, err)
				continue
			}
			fmt.Printf("related %s -> %s (%s) %s\n", op.From, op.To, op.Type, rel.EntityID)
		}
	}
	return nil
}
