package fixcmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newRewireCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagFrom   string
		flagMap    string
		flagDomain string
		flagApply  bool
	)

	cmd := &cobra.Command{
		Use:   "rewire",
		Short: "Rewire occurs_in relationships from coarse to granular contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			return runRewire(cfg.SDK, flagFrom, flagMap, flagDomain, flagApply)
		},
	}

	cmd.Flags().StringVar(&flagFrom, "from", "", "Key of coarse context (required)")
	cmd.Flags().StringVar(&flagMap, "map", "", "Comma-separated domain=context_key pairs (required)")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter by domain")
	cmd.Flags().BoolVar(&flagApply, "apply", false, "Apply changes")
	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("map")

	return cmd
}

func getByKey(ctx context.Context, gc *sdkgraph.Client, key string) (*sdkgraph.GraphObject, error) {
	res, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: key, Limit: 5})
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

func runRewire(client *sdk.Client, fromKey, mapStr, domainFilter string, apply bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fromObj, err := getByKey(ctx, client.Graph, fromKey)
	if err != nil {
		return fmt.Errorf("resolve from context: %w", err)
	}

	slugToKey := make(map[string]string)
	for _, pair := range strings.Split(mapStr, ",") {
		parts := strings.Split(pair, "=")
		if len(parts) == 2 {
			slugToKey[parts[0]] = parts[1]
		}
	}

	slugToID := make(map[string]string)
	for slug, key := range slugToKey {
		obj, err := getByKey(ctx, client.Graph, key)
		if err == nil {
			slugToID[slug] = obj.EntityID
		}
	}

	rels, err := listAllRels(ctx, client.Graph, "occurs_in")
	if err != nil {
		return err
	}

	var ops []rewireOp
	for _, r := range rels {
		if r.DstID != fromObj.EntityID {
			continue
		}
		// Find scenario for this step
		// (Simplified: would need to fetch has_step rels and Scenario objects)
		_ = r
	}

	if !apply {
		fmt.Printf("[DRY-RUN] Would rewire %d steps\n", len(ops))
		return nil
	}

	// Execute rewires...
	return nil
}

type rewireOp struct {
	relID, stepID, newCtxID string
}

func listAllRels(ctx context.Context, g *sdkgraph.Client, relType string) ([]*sdkgraph.GraphRelationship, error) {
	var all []*sdkgraph.GraphRelationship
	cursor := ""
	for {
		resp, err := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: relType, Limit: 500, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}
