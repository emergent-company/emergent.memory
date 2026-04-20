package analyzecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newTreeCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagDomain        string
		flagShowScenarios bool
		flagShowEndpoints bool
		flagMinScenarios  int
	)

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show functionality map as a tree",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			return runTree(cfg.SDK, flagDomain, flagShowScenarios, flagShowEndpoints, flagMinScenarios, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to one domain (key slug, e.g. 'agents')")
	cmd.Flags().BoolVar(&flagShowScenarios, "show-scenarios", false, "List individual scenarios")
	cmd.Flags().BoolVar(&flagShowEndpoints, "show-endpoints", false, "List individual endpoints")
	cmd.Flags().IntVar(&flagMinScenarios, "min-scenarios", 0, "Only show domains with >= N scenarios")

	return cmd
}

func runTree(client *sdk.Client, domainFilter string, showScenarios, showEndpoints bool, minScenarios int, format string) error {
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
				mu.Lock()
				fetchErr = e
				mu.Unlock()
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

	domainByID := map[string]*sdkgraph.GraphObject{}
	domainSlugs := []string{}
	for _, d := range domains {
		domainByID[d.EntityID] = d
		domainSlugs = append(domainSlugs, domainKeySlug(derefStr(d.Key)))
	}
	sort.Slice(domainSlugs, func(i, j int) bool { return len(domainSlugs[i]) > len(domainSlugs[j]) })

	svcByID := map[string]*sdkgraph.GraphObject{}
	for _, s := range services {
		svcByID[s.EntityID] = s
	}

	domainToSvcs := map[string][]string{}
	svcToDomains := map[string][]string{}
	for _, r := range btRels {
		if _, isDomain := domainByID[r.DstID]; isDomain {
			if _, isSvc := svcByID[r.SrcID]; isSvc {
				domainToSvcs[r.DstID] = append(domainToSvcs[r.DstID], r.SrcID)
				svcToDomains[r.SrcID] = append(svcToDomains[r.SrcID], r.DstID)
			}
		}
	}

	epByID := map[string]*sdkgraph.GraphObject{}
	for _, ep := range endpoints {
		epByID[ep.EntityID] = ep
	}

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
		statusStr := derefStr(sc.Status)
		if statusStr == "" {
			statusStr = sp(sc, "status")
		}
		switch statusStr {
		case "planned":
			statusStr = "planned"
		case "":
			statusStr = "implemented"
		}
		scenBySlug[slug] = append(scenBySlug[slug], ScenarioInfo{
			Name:   sp(sc, "name"),
			Status: statusStr,
			Key:    scKey,
		})
	}

	var nodes []DomainNode
	for _, d := range domains {
		slug := domainKeySlug(derefStr(d.Key))
		if domainFilter != "" && slug != domainFilter {
			continue
		}

		var svcNames []string
		for _, svcID := range domainToSvcs[d.EntityID] {
			if svc, ok := svcByID[svcID]; ok {
				svcNames = append(svcNames, sp(svc, "name"))
			}
		}
		sort.Strings(svcNames)

		eps := epBySlug[slug]
		sort.Slice(eps, func(i, j int) bool {
			if eps[i].Method != eps[j].Method {
				return eps[i].Method < eps[j].Method
			}
			return eps[i].Path < eps[j].Path
		})

		scens := scenBySlug[slug]
		sort.Slice(scens, func(i, j int) bool {
			return scens[i].Name < scens[j].Name
		})

		if minScenarios > 0 && len(scens) < minScenarios {
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

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	if domainFilter == "" {
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

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "markdown":
		printMarkdown(report, showScenarios)
	default:
		printTree(report, showScenarios, showEndpoints)
	}
	return nil
}

type ScenarioInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Key    string `json:"key"`
}

type DomainNode struct {
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	Services  []string       `json:"services"`
	Endpoints []EndpointInfo `json:"endpoints"`
	Scenarios []ScenarioInfo `json:"scenarios"`
}

type EndpointInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type Report struct {
	Generated string       `json:"generated"`
	Domains   []DomainNode `json:"domains"`
}

func domainKeySlug(key string) string {
	return strings.TrimPrefix(key, "domain-")
}

var scenarioKeyAliases = map[string]string{
	"org-user":            "org-user",
	"org":                 "org-user",
	"graph-objects":       "graph-objects",
	"graph-relationships": "graph-relationships",
	"graph":               "graph-objects",
}

func scenarioDomainSlug(key string, domainSlugs []string) string {
	if !strings.HasPrefix(key, "s-") {
		return ""
	}
	rest := key[2:]
	for _, slug := range domainSlugs {
		if rest == slug || strings.HasPrefix(rest, slug+"-") {
			return slug
		}
	}
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

func printTree(r Report, showScenarios, showEndpoints bool) {
	totalScen := 0
	totalEP := 0
	implemented := 0
	planned := 0
	for _, d := range r.Domains {
		totalEP += len(d.Endpoints)
		for _, s := range d.Scenarios {
			totalScen++
			if s.Status == "planned" {
				planned++
			} else {
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

		scenCount := len(d.Scenarios)
		coverage := ""
		if scenCount > 0 {
			pct := 100 * implCount / scenCount
			bar := strings.Repeat("█", pct/10) + strings.Repeat("░", 10-pct/10)
			coverage = fmt.Sprintf(" [%s %d%%]", bar, pct)
		}
		fmt.Printf("┌─ %s%s\n", d.Name, coverage)

		if len(d.Services) > 0 {
			fmt.Printf("│  ⚙  %s\n", strings.Join(d.Services, ", "))
		}

		if len(d.Endpoints) > 0 {
			fmt.Printf("│  ⇄  %d endpoints\n", len(d.Endpoints))
			if showEndpoints {
				for _, ep := range d.Endpoints {
					fmt.Printf("│     %-7s %s\n", ep.Method, ep.Path)
				}
			}
		}

		if scenCount > 0 {
			fmt.Printf("│  ◈  %d scenarios", scenCount)
			if planCount > 0 {
				fmt.Printf(" (%d planned)", planCount)
			}
			fmt.Println()
			if showScenarios {
				for _, s := range d.Scenarios {
					fmt.Printf("│     %s %s\n", statusIcon(s.Status), s.Name)
				}
			}
		}
		fmt.Println("│")
	}
}

func printMarkdown(r Report, showScenarios bool) {
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
		fmt.Printf("| **%s** | %s | %d | %d | %d |\n", d.Name, strings.Join(d.Services, ", "), len(d.Endpoints), len(d.Scenarios), planned)
	}
	if showScenarios {
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
