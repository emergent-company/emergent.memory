// func-tree: builds a functionality map from the Memory knowledge graph.
//
// For each Domain, shows:
//   - Services linked via belongs_to
//   - APIEndpoints (via Service→exposes→APIEndpoint traversal)
//   - Scenarios grouped by status (implemented / planned / unknown)
//
// Scenario→Domain mapping uses key prefix (s-<domain>-<slug>).
// Endpoint→Domain mapping: Domain←belongs_to←Service→exposes→APIEndpoint.
//
// Usage:
//
//	MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... go run ./...
//	  --format tree|markdown|json   (default: tree)
//	  --domain <name>               filter to one domain
//	  --show-scenarios              include scenario list (default: counts only)
//	  --show-endpoints              include endpoint list (default: counts only)
//	  --min-scenarios N             only show domains with >= N scenarios
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

var (
	flagFormat       = flag.String("format", "tree", "Output format: tree, markdown, json")
	flagDomain       = flag.String("domain", "", "Filter to one domain (key slug, e.g. 'agents')")
	flagShowScenarios = flag.Bool("show-scenarios", false, "List individual scenarios")
	flagShowEndpoints = flag.Bool("show-endpoints", false, "List individual endpoints")
	flagMinScenarios = flag.Int("min-scenarios", 0, "Only show domains with >= N scenarios")
)

// domainKeySlug extracts the code slug from a domain key (domain-<slug>)
func domainKeySlug(key string) string {
	return strings.TrimPrefix(key, "domain-")
}

// scenarioDomainSlug extracts domain from scenario key (s-<domain>-<rest>)
// Matches against known domain slugs (longest first to avoid prefix collisions).
// Also handles aliased prefixes (e.g. s-org-* → org-user).
var scenarioKeyAliases = map[string]string{
	"org-user":     "org-user",
	"org":          "org-user",
	"graph-objects": "graph-objects",
	"graph-relationships": "graph-relationships",
	"graph":        "graph-objects", // graph code domain → KG Objects conceptual domain
}

func scenarioDomainSlug(key string, domainSlugs []string) string {
	if !strings.HasPrefix(key, "s-") {
		return ""
	}
	rest := key[2:]
	// Try direct slug match first (longest-first order)
	for _, slug := range domainSlugs {
		if rest == slug || strings.HasPrefix(rest, slug+"-") {
			return slug
		}
	}
	// Try alias prefixes (longest-first)
	type aliasEntry struct{ prefix, target string }
	aliases := []aliasEntry{}
	for prefix, target := range scenarioKeyAliases {
		aliases = append(aliases, aliasEntry{prefix, target})
	}
	sort.Slice(aliases, func(i, j int) bool { return len(aliases[i].prefix) > len(aliases[j].prefix) })
	for _, a := range aliases {
		if rest == a.prefix || strings.HasPrefix(rest, a.prefix+"-") {
			return a.target
		}
	}
	return ""
}

type ScenarioInfo struct {
	Name   string
	Status string // "implemented", "planned", ""
	Key    string
}

type DomainNode struct {
	Key      string
	Name     string
	Services []string
	Endpoints []EndpointInfo
	Scenarios []ScenarioInfo
}

type EndpointInfo struct {
	Method string
	Path   string
}

type Report struct {
	Generated string       `json:"generated"`
	Domains   []DomainNode `json:"domains"`
}

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	client, err := sdk.New(sdk.Config{
		ServerURL: os.Getenv("MEMORY_SERVER_URL"),
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: os.Getenv("MEMORY_API_KEY")},
		ProjectID: os.Getenv("MEMORY_PROJECT_ID"),
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fmt.Fprintln(os.Stderr, "→ Fetching graph data...")

	var (
		domains     []*sdkgraph.GraphObject
		endpoints   []*sdkgraph.GraphObject
		services    []*sdkgraph.GraphObject
		scenarios   []*sdkgraph.GraphObject
		btRels      []*sdkgraph.GraphRelationship
		exposesRels []*sdkgraph.GraphRelationship
		mu          sync.Mutex
		wg          sync.WaitGroup
		fetchErr    error
	)

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := fn(); e != nil {
				mu.Lock(); fetchErr = e; mu.Unlock()
			}
		}()
	}

	fetch(func() error { r, e := listAll(ctx, client.Graph, "Domain"); domains = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "APIEndpoint"); endpoints = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "Service"); services = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "Scenario"); scenarios = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "belongs_to"); btRels = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "exposes"); exposesRels = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	fmt.Fprintf(os.Stderr, "  Domains:%d Endpoints:%d Services:%d Scenarios:%d belongs_to:%d exposes:%d\n",
		len(domains), len(endpoints), len(services), len(scenarios), len(btRels), len(exposesRels))

	// ── Build indexes ─────────────────────────────────────────────────────────

	// Domain by entity_id and by slug
	domainByID := map[string]*sdkgraph.GraphObject{}
	domainBySlug := map[string]*sdkgraph.GraphObject{}
	domainSlugs := []string{}
	for _, d := range domains {
		domainByID[d.EntityID] = d
		slug := domainKeySlug(derefStr(d.Key))
		domainBySlug[slug] = d
		domainSlugs = append(domainSlugs, slug)
	}
	// Sort slugs longest-first for prefix matching
	sort.Slice(domainSlugs, func(i, j int) bool {
		return len(domainSlugs[i]) > len(domainSlugs[j])
	})

	// Service by entity_id
	svcByID := map[string]*sdkgraph.GraphObject{}
	for _, s := range services {
		svcByID[s.EntityID] = s
	}

	// belongs_to: domainID → []svcID
	domainToSvcs := map[string][]string{}
	// svcID → []domainID (service may belong to multiple domains)
	svcToDomains := map[string][]string{}
	for _, r := range btRels {
		if _, isDomain := domainByID[r.DstID]; isDomain {
			if _, isSvc := svcByID[r.SrcID]; isSvc {
				domainToSvcs[r.DstID] = append(domainToSvcs[r.DstID], r.SrcID)
				svcToDomains[r.SrcID] = append(svcToDomains[r.SrcID], r.DstID)
			}
		}
	}

	// Endpoint by entity_id
	epByID := map[string]*sdkgraph.GraphObject{}
	for _, ep := range endpoints {
		epByID[ep.EntityID] = ep
	}

	// Endpoints by domain: traverse Service→exposes→APIEndpoint
	// Domain membership: svcToDomains[svcID] → []domainID → domainSlug
	// If service belongs to multiple domains, endpoint appears in all of them.
	epBySlug := map[string][]EndpointInfo{}
	for _, r := range exposesRels {
		domainIDs, ok := svcToDomains[r.SrcID]
		if !ok {
			continue
		}
		ep, ok := epByID[r.DstID]
		if !ok {
			continue
		}
		info := EndpointInfo{
			Method: sp(ep, "method"),
			Path:   sp(ep, "path"),
		}
		for _, domainID := range domainIDs {
			domain, ok := domainByID[domainID]
			if !ok {
				continue
			}
			slug := domainKeySlug(derefStr(domain.Key))
			epBySlug[slug] = append(epBySlug[slug], info)
		}
	}

	// Scenarios by domain slug (via key prefix)
	scenBySlug := map[string][]ScenarioInfo{}
	for _, sc := range scenarios {
		scKey := derefStr(sc.Key)
		slug := scenarioDomainSlug(scKey, domainSlugs)
		if slug == "" {
			slug = strings.ToLower(sp(sc, "domain"))
		}
		if slug == "" {
			slug = "(unassigned)"
		}
		// Status: prefer object-level Status field, fall back to property
		statusStr := derefStr(sc.Status)
		if statusStr == "" {
			statusStr = sp(sc, "status")
		}
		// Normalise
		switch statusStr {
		case "planned":
			statusStr = "planned"
		case "":
			statusStr = "implemented" // no status = assumed implemented/existing
		}
		scenBySlug[slug] = append(scenBySlug[slug], ScenarioInfo{
			Name:   sp(sc, "name"),
			Status: statusStr,
			Key:    scKey,
		})
	}

	// ── Build domain nodes ────────────────────────────────────────────────────
	var nodes []DomainNode
	for _, d := range domains {
		slug := domainKeySlug(derefStr(d.Key))
		if *flagDomain != "" && slug != *flagDomain {
			continue
		}

		// Services
		var svcNames []string
		for _, svcID := range domainToSvcs[d.EntityID] {
			if svc, ok := svcByID[svcID]; ok {
				svcNames = append(svcNames, sp(svc, "name"))
			}
		}
		sort.Strings(svcNames)

		// Endpoints
		eps := epBySlug[slug]
		sort.Slice(eps, func(i, j int) bool {
			if eps[i].Method != eps[j].Method {
				return eps[i].Method < eps[j].Method
			}
			return eps[i].Path < eps[j].Path
		})

		// Scenarios
		scens := scenBySlug[slug]
		sort.Slice(scens, func(i, j int) bool {
			return scens[i].Name < scens[j].Name
		})

		total := len(scens)
		if *flagMinScenarios > 0 && total < *flagMinScenarios {
			continue
		}

		nodes = append(nodes, DomainNode{
			Key:       slug,
			Name:      sp(d, "name"),
			Services:  svcNames,
			Endpoints: eps,
			Scenarios: scens,
		})
	}

	// Sort domains by name
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})

	// Add unassigned bucket if no domain filter
	if *flagDomain == "" {
		unassigned := scenBySlug["(unassigned)"]
		if len(unassigned) > 0 {
			sort.Slice(unassigned, func(i, j int) bool { return unassigned[i].Name < unassigned[j].Name })
			nodes = append(nodes, DomainNode{
				Key:       "(unassigned)",
				Name:      "(Unassigned)",
				Scenarios: unassigned,
			})
		}
	}

	report := Report{
		Generated: time.Now().Format("2006-01-02"),
		Domains:   nodes,
	}

	switch *flagFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "markdown":
		printMarkdown(report)
	default:
		printTree(report)
	}
	return nil
}

// ── output ────────────────────────────────────────────────────────────────────

func statusIcon(s string) string {
	switch s {
	case "planned":
		return "◌"
	case "implemented":
		return "✓"
	default:
		return "·"
	}
}

func printTree(r Report) {
	// Summary header
	totalScen := 0
	totalEP := 0
	implemented := 0
	planned := 0
	for _, d := range r.Domains {
		totalEP += len(d.Endpoints)
		for _, s := range d.Scenarios {
			totalScen++
			switch s.Status {
			case "planned":
				planned++
			case "implemented":
				implemented++
			}
		}
	}

	fmt.Printf("\n╔══ FUNCTIONALITY MAP  %s\n", r.Generated)
	fmt.Printf("║   %d domains · %d scenarios (%d implemented, %d planned) · %d endpoints\n\n",
		len(r.Domains), totalScen, implemented, planned, totalEP)

	for _, d := range r.Domains {
		implCount := 0
		planCount := 0
		for _, s := range d.Scenarios {
			if s.Status == "planned" {
				planCount++
			} else {
				implCount++
			}
		}

		// Domain header
		epCount := len(d.Endpoints)
		scenCount := len(d.Scenarios)
		coverage := ""
		if scenCount > 0 {
			pct := 100 * implCount / scenCount
			bar := strings.Repeat("█", pct/10) + strings.Repeat("░", 10-pct/10)
			coverage = fmt.Sprintf(" [%s %d%%]", bar, pct)
		}
		fmt.Printf("┌─ %s%s\n", d.Name, coverage)

		// Services
		if len(d.Services) > 0 {
			fmt.Printf("│  ⚙  %s\n", strings.Join(d.Services, ", "))
		}

		// Endpoint count
		if epCount > 0 {
			fmt.Printf("│  ⇄  %d endpoints\n", epCount)
			if *flagShowEndpoints {
				for _, ep := range d.Endpoints {
					fmt.Printf("│     %-7s %s\n", ep.Method, ep.Path)
				}
			}
		}

		// Scenarios
		if scenCount > 0 {
			fmt.Printf("│  ◈  %d scenarios", scenCount)
			if planCount > 0 {
				fmt.Printf(" (%d planned)", planCount)
			}
			fmt.Println()
			if *flagShowScenarios {
				for _, s := range d.Scenarios {
					fmt.Printf("│     %s %s\n", statusIcon(s.Status), s.Name)
				}
			}
		}
		fmt.Println("│")
	}
}

func printMarkdown(r Report) {
	fmt.Printf("# Functionality Map\n\nGenerated: %s\n\n", r.Generated)

	totalScen := 0
	totalEP := 0
	for _, d := range r.Domains {
		totalEP += len(d.Endpoints)
		totalScen += len(d.Scenarios)
	}
	fmt.Printf("**%d domains · %d scenarios · %d endpoints**\n\n", len(r.Domains), totalScen, totalEP)

	fmt.Println("| Domain | Services | Endpoints | Scenarios | Planned |")
	fmt.Println("|--------|----------|-----------|-----------|---------|")
	for _, d := range r.Domains {
		planned := 0
		for _, s := range d.Scenarios {
			if s.Status == "planned" {
				planned++
			}
		}
		fmt.Printf("| **%s** | %s | %d | %d | %d |\n",
			d.Name,
			strings.Join(d.Services, ", "),
			len(d.Endpoints),
			len(d.Scenarios),
			planned,
		)
	}

	if *flagShowScenarios {
		fmt.Println()
		for _, d := range r.Domains {
			if len(d.Scenarios) == 0 {
				continue
			}
			fmt.Printf("\n## %s\n\n", d.Name)
			for _, s := range d.Scenarios {
				fmt.Printf("- %s %s\n", statusIcon(s.Status), s.Name)
			}
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func listAll(ctx context.Context, g *sdkgraph.Client, objType string) ([]*sdkgraph.GraphObject, error) {
	var all []*sdkgraph.GraphObject
	cursor := ""
	for {
		resp, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: 500, Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", objType, err)
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

func listAllRels(ctx context.Context, g *sdkgraph.Client, relType string) ([]*sdkgraph.GraphRelationship, error) {
	var all []*sdkgraph.GraphRelationship
	cursor := ""
	for {
		resp, err := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: relType, Limit: 500, Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("listing %s rels: %w", relType, err)
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

func sp(o *sdkgraph.GraphObject, key string) string {
	if v, ok := o.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
