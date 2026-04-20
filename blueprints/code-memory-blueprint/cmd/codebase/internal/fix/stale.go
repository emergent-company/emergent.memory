package fixcmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newStaleCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagDryRun bool
		flagDomain string
	)

	cmd := &cobra.Command{
		Use:   "stale",
		Short: "Delete stale APIEndpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			return runStale(cfg.SDK, flagDryRun, flagDomain)
		},
	}

	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Dry run")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter by domain")

	return cmd
}

func runStale(client *sdk.Client, dryRun bool, domainFilter string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	eps, err := listAll(ctx, client.Graph, "APIEndpoint")
	if err != nil {
		return err
	}

	deleted := 0
	for _, ep := range eps {
		domain, _ := ep.Properties["domain"].(string)
		if domainFilter != "" && domain != domainFilter {
			continue
		}

		key := ""
		if ep.Key != nil {
			key = *ep.Key
		}
		handler, _ := ep.Properties["handler"].(string)

		if key == "" && domain == "" && handler == "" {
			if dryRun {
				fmt.Printf("[DRY-RUN] Delete stale endpoint: %s\n", ep.EntityID)
			} else {
				if err := client.Graph.DeleteObject(ctx, ep.EntityID, nil); err != nil {
					fmt.Fprintf(os.Stderr, "Error deleting %s: %v\n", ep.EntityID, err)
				} else {
					deleted++
				}
			}
		}
	}
	fmt.Printf("Deleted %d stale endpoints\n", deleted)
	return nil
}

func listAll(ctx context.Context, g *sdkgraph.Client, objType string) ([]*sdkgraph.GraphObject, error) {
	var all []*sdkgraph.GraphObject
	cursor := ""
	for {
		resp, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: 500, Cursor: cursor})
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
