// service-exposes-seed: wires Service→exposes→APIEndpoint relationships.
//
// Steps:
//  1. Fetch all Service objects (name → id map)
//  2. Fetch all APIEndpoint objects (properties.domain → code domain slug)
//  3. Derive service name from code domain: capitalize first letter + "Service"
//  4. Bulk-upsert "exposes" rels: Service → APIEndpoint
//  5. (--cleanup) Delete all existing APIEndpoint belongs_to Domain rels
//
// Usage:
//
//	MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... go run ./...
//	  --dry-run      print what would be created without writing
//	  --cleanup      also delete APIEndpoint belongs_to Domain rels after seeding
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

var (
	flagDryRun  = flag.Bool("dry-run", false, "Print actions without writing to graph")
	flagCleanup = flag.Bool("cleanup", false, "Delete APIEndpoint belongs_to Domain rels after seeding")
)

func main() {
	flag.Parse()

	apiKey := os.Getenv("MEMORY_API_KEY")
	projectID := os.Getenv("MEMORY_PROJECT_ID")
	serverURL := os.Getenv("MEMORY_SERVER_URL")
	if apiKey == "" || projectID == "" || serverURL == "" {
		fmt.Fprintln(os.Stderr, "MEMORY_API_KEY, MEMORY_PROJECT_ID, MEMORY_SERVER_URL required")
		os.Exit(1)
	}

	client, err := sdk.New(sdk.Config{
		ServerURL: serverURL,
		Auth: sdk.AuthConfig{
			Mode:   "apikey",
			APIKey: apiKey,
		},
		ProjectID: projectID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sdk init: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Fetch all Services
	fmt.Println("Fetching Services...")
	services, err := fetchAllObjects(ctx, client, "Service")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch services: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Found %d services\n", len(services))

	// Build map: codeDomainSlug → serviceID
	// Service name pattern: "<Domain>Service" where Domain = capitalize(codeDomain)
	// e.g. "agents" → "AgentsService"
	serviceByDomain := map[string]string{} // codeDomain → entityID
	for _, svc := range services {
		name := svc.Properties["name"]
		if name == nil {
			continue
		}
		nameStr, ok := name.(string)
		if !ok {
			continue
		}
		// Strip "Service" suffix, lowercase → code domain slug
		slug := strings.TrimSuffix(nameStr, "Service")
		slug = strings.ToLower(slug)
		serviceByDomain[slug] = svc.EntityID
	}
	fmt.Printf("  Mapped %d service→domain entries\n", len(serviceByDomain))

	// 2. Fetch all APIEndpoints
	fmt.Println("Fetching APIEndpoints...")
	endpoints, err := fetchAllObjects(ctx, client, "APIEndpoint")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch endpoints: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Found %d endpoints\n", len(endpoints))

	// 3. Build exposes rels
	type rel struct {
		svcID string
		epID  string
		epKey string
		svcName string
	}
	var toCreate []rel
	var noService []string

	for _, ep := range endpoints {
		domainVal := ep.Properties["domain"]
		if domainVal == nil {
			noService = append(noService, ep.EntityID)
			continue
		}
		domain, ok := domainVal.(string)
		if !ok || domain == "" {
			noService = append(noService, ep.EntityID)
			continue
		}
		svcID, found := serviceByDomain[domain]
		if !found {
			noService = append(noService, fmt.Sprintf("%s (domain=%s)", ep.EntityID, domain))
			continue
		}
		keyStr := ""
		if ep.Key != nil {
			keyStr = *ep.Key
		}
		// derive service name for display
		svcName := strings.ToUpper(domain[:1]) + domain[1:] + "Service"
		toCreate = append(toCreate, rel{svcID: svcID, epID: ep.EntityID, epKey: keyStr, svcName: svcName})
	}

	fmt.Printf("\nExposes rels to create: %d\n", len(toCreate))
	if len(noService) > 0 {
		fmt.Printf("Endpoints with no matching service: %d\n", len(noService))
		for _, s := range noService {
			fmt.Printf("  - %s\n", s)
		}
	}

	if *flagDryRun {
		fmt.Println("\n[dry-run] Would create:")
		for _, r := range toCreate {
			fmt.Printf("  %s --exposes--> %s\n", r.svcName, r.epKey)
		}
		return
	}

	// 4. Bulk upsert in batches of 100
	fmt.Println("\nCreating exposes rels...")
	batchSize := 100
	totalSuccess := 0
	totalFailed := 0
	for i := 0; i < len(toCreate); i += batchSize {
		end := i + batchSize
		if end > len(toCreate) {
			end = len(toCreate)
		}
		batch := toCreate[i:end]
		items := make([]sdkgraph.CreateRelationshipRequest, len(batch))
		for j, r := range batch {
			items[j] = sdkgraph.CreateRelationshipRequest{
				Type:  "exposes",
				SrcID: r.svcID,
				DstID: r.epID,
				Upsert: true,
			}
		}
		resp, err := client.Graph.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: items})
		if err != nil {
			fmt.Fprintf(os.Stderr, "bulk create batch %d: %v\n", i/batchSize, err)
			continue
		}
		totalSuccess += resp.Success
		totalFailed += resp.Failed
		fmt.Printf("  Batch %d/%d: %d ok, %d failed\n", i/batchSize+1, (len(toCreate)+batchSize-1)/batchSize, resp.Success, resp.Failed)
	}
	fmt.Printf("\nTotal: %d created, %d failed\n", totalSuccess, totalFailed)

	// 5. Cleanup: delete APIEndpoint belongs_to Domain rels
	if *flagCleanup {
		fmt.Println("\nCleaning up APIEndpoint belongs_to Domain rels...")
		deleted, err := deleteEndpointBelongsToDomain(ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cleanup: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Deleted %d belongs_to rels\n", deleted)
	}
}

// fetchAllObjects paginates through all objects of a given type.
func fetchAllObjects(ctx context.Context, client *sdk.Client, objType string) ([]*sdkgraph.GraphObject, error) {
	var all []*sdkgraph.GraphObject
	cursor := ""
	for {
		opts := &sdkgraph.ListObjectsOptions{
			Type:   objType,
			Limit:  500,
			Cursor: cursor,
		}
		resp, err := client.Graph.ListObjects(ctx, opts)
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

// deleteEndpointBelongsToDomain finds and deletes all belongs_to rels where src is an APIEndpoint.
func deleteEndpointBelongsToDomain(ctx context.Context, client *sdk.Client) (int, error) {
	// Fetch all belongs_to rels
	var allRels []*sdkgraph.GraphRelationship
	cursor := ""
	for {
		opts := &sdkgraph.ListRelationshipsOptions{
			Type:   "belongs_to",
			Limit:  500,
			Cursor: cursor,
		}
		resp, err := client.Graph.ListRelationships(ctx, opts)
		if err != nil {
			return 0, fmt.Errorf("list belongs_to: %w", err)
		}
		allRels = append(allRels, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	fmt.Printf("  Found %d total belongs_to rels\n", len(allRels))

	// Fetch all APIEndpoint IDs for fast lookup
	endpoints, err := fetchAllObjects(ctx, client, "APIEndpoint")
	if err != nil {
		return 0, fmt.Errorf("fetch endpoints for cleanup: %w", err)
	}
	epIDs := map[string]bool{}
	for _, ep := range endpoints {
		epIDs[ep.EntityID] = true
	}

	// Delete rels where src is an APIEndpoint
	deleted := 0
	for _, rel := range allRels {
		if epIDs[rel.SrcID] {
			if err := client.Graph.DeleteRelationship(ctx, rel.EntityID); err != nil {
				fmt.Fprintf(os.Stderr, "  delete rel %s: %v\n", rel.EntityID, err)
				continue
			}
			deleted++
		}
	}
	return deleted, nil
}
