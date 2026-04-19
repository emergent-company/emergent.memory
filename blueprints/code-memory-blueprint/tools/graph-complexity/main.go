// graph-complexity: analyze structural complexity of a Memory knowledge graph.
//
// Scores each domain by:
//   - endpoint count (API surface)
//   - method count (business logic depth)
//   - sql query count (data access breadth)
//   - job count (async complexity)
//   - dependency fan-out (how many modules it belongs_to or defines)
//
// Composite complexity score = endpoints*3 + methods*2 + sqlqueries + jobs*2
//
// Usage:
//
//	MEMORY_API_KEY=<token> MEMORY_PROJECT_ID=<id> MEMORY_SERVER_URL=https://... \
//	  go run . [--format table|markdown|json] [--top N] [--sort score|endpoints|methods]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// DomainComplexity holds complexity metrics for one domain.
type DomainComplexity struct {
	Domain        string   `json:"domain"`
	Services      []string `json:"services"`
	EndpointCount int      `json:"endpoint_count"`
	MethodCount   int      `json:"method_count"`
	SQLQueryCount int      `json:"sql_query_count"`
	JobCount      int      `json:"job_count"`
	MiddlewareCount int    `json:"middleware_count"`
	Score         int      `json:"score"`
	Tier          string   `json:"tier"` // critical / high / medium / low
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	serverURL := flag.String("server", envOr("MEMORY_SERVER_URL", "http://localhost:3012"), "Memory server URL")
	apiKey := flag.String("api-key", envOr("MEMORY_API_KEY", envOr("MEMORY_PROJECT_TOKEN", "")), "API key or project token")
	orgID := flag.String("org-id", envOr("MEMORY_ORG_ID", ""), "Organization ID")
	projectID := flag.String("project-id", envOr("MEMORY_PROJECT_ID", ""), "Project ID")
	format := flag.String("format", "table", "Output format: table, markdown, json")
	topN := flag.Int("top", 0, "Show only top N domains by score (0 = all)")
	sortBy := flag.String("sort", "score", "Sort by: score, endpoints, methods, sql, domain")
	domainFilter := flag.String("domain", "", "Filter to a specific domain")
	flag.Parse()

	if *apiKey == "" {
		return fmt.Errorf("--api-key or MEMORY_API_KEY is required")
	}
	if *projectID == "" {
		return fmt.Errorf("--project-id or MEMORY_PROJECT_ID is required")
	}

	client, err := sdk.New(sdk.Config{
		ServerURL: *serverURL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: *apiKey},
		OrgID:     *orgID,
		ProjectID: *projectID,
	})
	if err != nil {
		return fmt.Errorf("creating SDK client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Fprintln(os.Stderr, "→ Fetching graph data...")

	var wg sync.WaitGroup
	var services, endpoints, methods, sqlqueries, jobs, middlewares []*sdkgraph.GraphObject
	var definedInRels, handlesRels []*sdkgraph.GraphRelationship
	var errs []error
	var mu sync.Mutex

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}

	fetch(func() error {
		var err error
		services, err = listAllObjects(ctx, client.Graph, "Service")
		return err
	})
	fetch(func() error {
		var err error
		endpoints, err = listAllObjects(ctx, client.Graph, "APIEndpoint")
		return err
	})
	fetch(func() error {
		var err error
		methods, err = listAllObjects(ctx, client.Graph, "Method")
		return err
	})
	fetch(func() error {
		var err error
		sqlqueries, err = listAllObjects(ctx, client.Graph, "SQLQuery")
		return err
	})
	fetch(func() error {
		var err error
		jobs, err = listAllObjects(ctx, client.Graph, "Job")
		return err
	})
	fetch(func() error {
		var err error
		middlewares, err = listAllObjects(ctx, client.Graph, "Middleware")
		return err
	})
	fetch(func() error {
		var err error
		definedInRels, err = listAllRelationships(ctx, client.Graph, "defined_in")
		return err
	})
	fetch(func() error {
		var err error
		handlesRels, err = listAllRelationships(ctx, client.Graph, "handles")
		return err
	})

	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("fetch errors: %v", errs)
	}

	// Build SourceFile ID -> domain map via defined_in rels
	// We need: object -> SourceFile -> domain path
	// Simpler: extract domain from object properties directly
	// Each object has a "file" or "path" property, or we derive from name

	// Build domain -> complexity
	reports := make(map[string]*DomainComplexity)
	getReport := func(domain string) *DomainComplexity {
		if r, ok := reports[domain]; ok {
			return r
		}
		r := &DomainComplexity{Domain: domain}
		reports[domain] = r
		return r
	}

	// Index: object ID -> domain (from defined_in -> SourceFile path)
	// Build SourceFile ID -> domain
	sfIDToDomain := make(map[string]string)
	for _, r := range definedInRels {
		// We'll resolve this after we have SourceFile objects
		_ = r
	}

	// Fetch SourceFiles to build path->domain index
	sourceFiles, err := listAllObjects(ctx, client.Graph, "SourceFile")
	if err != nil {
		return fmt.Errorf("fetching SourceFiles: %w", err)
	}
	for _, sf := range sourceFiles {
		path := strProp(sf, "path")
		domain := domainFromPath(path)
		if domain != "" {
			sfIDToDomain[sf.ID] = domain
			sfIDToDomain[sf.EntityID] = domain
		}
	}

	// Object ID -> domain via defined_in
	objIDToDomain := make(map[string]string)
	for _, r := range definedInRels {
		if d, ok := sfIDToDomain[r.DstID]; ok {
			objIDToDomain[r.SrcID] = d
		}
	}

	// handles rels: Service ID -> []APIEndpoint ID
	// We use this to count endpoints per service domain
	endpointIDToServiceID := make(map[string]string)
	for _, r := range handlesRels {
		// handles: Service -> APIEndpoint (src=service, dst=endpoint)
		endpointIDToServiceID[r.DstID] = r.SrcID
	}

	// Service ID -> domain
	serviceIDToDomain := make(map[string]string)
	for _, s := range services {
		domain := getDomain(s)
		if domain == "" {
			domain = objIDToDomain[s.ID]
		}
		if domain == "" {
			continue
		}
		serviceIDToDomain[s.ID] = domain
		serviceIDToDomain[s.EntityID] = domain
		r := getReport(domain)
		r.Services = append(r.Services, strProp(s, "name"))
	}

	// Count endpoints per domain
	for _, ep := range endpoints {
		domain := getDomain(ep)
		if domain == "" {
			// try via handles rel
			if svcID, ok := endpointIDToServiceID[ep.ID]; ok {
				domain = serviceIDToDomain[svcID]
			}
		}
		if domain == "" {
			domain = objIDToDomain[ep.ID]
		}
		if domain == "" {
			continue
		}
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}
		getReport(domain).EndpointCount++
	}

	// Count methods per domain
	for _, m := range methods {
		domain := getDomain(m)
		if domain == "" {
			domain = objIDToDomain[m.ID]
		}
		if domain == "" {
			continue
		}
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}
		getReport(domain).MethodCount++
	}

	// Count SQL queries per domain
	for _, sq := range sqlqueries {
		domain := getDomain(sq)
		if domain == "" {
			domain = objIDToDomain[sq.ID]
		}
		if domain == "" {
			// try file property
			file := strProp(sq, "file")
			domain = domainFromPath(file)
		}
		if domain == "" {
			continue
		}
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}
		getReport(domain).SQLQueryCount++
	}

	// Count jobs per domain
	for _, j := range jobs {
		domain := getDomain(j)
		if domain == "" {
			domain = objIDToDomain[j.ID]
		}
		if domain == "" {
			continue
		}
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}
		getReport(domain).JobCount++
	}

	// Count middleware per domain
	for _, mw := range middlewares {
		domain := getDomain(mw)
		if domain == "" {
			domain = objIDToDomain[mw.ID]
		}
		if domain == "" {
			continue
		}
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}
		getReport(domain).MiddlewareCount++
	}

	// Compute scores and tiers
	for _, r := range reports {
		r.Score = r.EndpointCount*3 + r.MethodCount*2 + r.SQLQueryCount + r.JobCount*2 + r.MiddlewareCount
		switch {
		case r.Score >= 100:
			r.Tier = "critical"
		case r.Score >= 50:
			r.Tier = "high"
		case r.Score >= 20:
			r.Tier = "medium"
		default:
			r.Tier = "low"
		}
	}

	// Sort
	sorted := make([]*DomainComplexity, 0, len(reports))
	for _, r := range reports {
		sorted = append(sorted, r)
	}
	sort.Slice(sorted, func(i, j int) bool {
		switch *sortBy {
		case "endpoints":
			if sorted[i].EndpointCount != sorted[j].EndpointCount {
				return sorted[i].EndpointCount > sorted[j].EndpointCount
			}
		case "methods":
			if sorted[i].MethodCount != sorted[j].MethodCount {
				return sorted[i].MethodCount > sorted[j].MethodCount
			}
		case "sql":
			if sorted[i].SQLQueryCount != sorted[j].SQLQueryCount {
				return sorted[i].SQLQueryCount > sorted[j].SQLQueryCount
			}
		case "domain":
			return sorted[i].Domain < sorted[j].Domain
		default: // score
			if sorted[i].Score != sorted[j].Score {
				return sorted[i].Score > sorted[j].Score
			}
		}
		return sorted[i].Domain < sorted[j].Domain
	})

	if *topN > 0 && *topN < len(sorted) {
		sorted = sorted[:*topN]
	}

	// Summary stats
	totalScore := 0
	maxScore := 0
	criticalDomains := 0
	highDomains := 0
	for _, r := range reports {
		totalScore += r.Score
		if r.Score > maxScore {
			maxScore = r.Score
		}
		if r.Tier == "critical" {
			criticalDomains++
		} else if r.Tier == "high" {
			highDomains++
		}
	}

	switch *format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(sorted)
	case "markdown":
		return printMarkdown(sorted, reports, totalScore, maxScore, criticalDomains, highDomains)
	default:
		return printTable(sorted, reports, totalScore, maxScore, criticalDomains, highDomains)
	}
}

func printTable(sorted []*DomainComplexity, all map[string]*DomainComplexity, totalScore, maxScore, critical, high int) error {
	now := time.Now().Format("2006-01-02")
	fmt.Printf("┌─ GRAPH COMPLEXITY REPORT\n")
	fmt.Printf("  Generated: %s\n\n", now)

	fmt.Printf("┌─ SUMMARY\n")
	fmt.Printf("  Total domains   : %d\n", len(all))
	fmt.Printf("  Critical domains: %d\n", critical)
	fmt.Printf("  High domains    : %d\n", high)
	fmt.Printf("  Total score     : %d\n", totalScore)
	fmt.Printf("  Max domain score: %d\n\n", maxScore)

	fmt.Printf("┌─ DOMAIN COMPLEXITY\n")
	table := tablewriter.NewWriter(os.Stdout)
	table.Header("TIER", "DOMAIN", "ENDPOINTS", "METHODS", "SQL", "JOBS", "MW", "SCORE")

	table.Options(
		tablewriter.WithAutoHide(tw.Off),
		tablewriter.WithTrimSpace(tw.On),
		tablewriter.WithHeaderAlignmentConfig(tw.CellAlignment{Global: tw.AlignLeft}),
		tablewriter.WithRowAlignmentConfig(tw.CellAlignment{
			PerColumn: []tw.Align{
				tw.AlignLeft,
				tw.AlignLeft,
				tw.AlignRight,
				tw.AlignRight,
				tw.AlignRight,
				tw.AlignRight,
				tw.AlignRight,
				tw.AlignRight,
			},
		}),
		tablewriter.WithTrimLine(tw.On),
		tablewriter.WithRendition(tw.Rendition{
			Borders: tw.Border{
				Left:   tw.Off,
				Right:  tw.Off,
				Top:    tw.Off,
				Bottom: tw.Off,
			},
			Settings: tw.Settings{
				Separators: tw.Separators{
					BetweenColumns: tw.Off,
					BetweenRows:    tw.Off,
				},
				Lines: tw.Lines{
					ShowHeaderLine: tw.Off,
				},
			},
		}),
	)

	for _, r := range sorted {
		tier := strings.ToUpper(r.Tier)
		if tier == "MEDIUM" {
			tier = "MED"
		}

		row := []string{
			tier,
			r.Domain,
			strconv.Itoa(r.EndpointCount),
			strconv.Itoa(r.MethodCount),
			strconv.Itoa(r.SQLQueryCount),
			strconv.Itoa(r.JobCount),
			strconv.Itoa(r.MiddlewareCount),
			strconv.Itoa(r.Score),
		}

		table.Append(row)
	}
	table.Render()

	// Critical/High detail
	var hotspots []*DomainComplexity
	for _, r := range sorted {
		if r.Tier == "critical" || r.Tier == "high" {
			hotspots = append(hotspots, r)
		}
	}
	if len(hotspots) > 0 {
		fmt.Printf("\n┌─ HOTSPOT DOMAINS (score ≥ 50)\n")
		for _, r := range hotspots {
			fmt.Printf("  • %s (score=%d): %d endpoints, %d methods, %d SQL queries, %d jobs\n",
				r.Domain, r.Score, r.EndpointCount, r.MethodCount, r.SQLQueryCount, r.JobCount)
			fmt.Printf("    Recommendation: Consider splitting %s into sub-domains or extracting shared logic.\n", r.Domain)
		}
	}

	return nil
}

func printMarkdown(sorted []*DomainComplexity, all map[string]*DomainComplexity, totalScore, maxScore, critical, high int) error {
	now := time.Now().Format("2006-01-02")
	fmt.Printf("# Graph Complexity Report\n\n")
	fmt.Printf("Generated: %s\n\n", now)

	fmt.Printf("## Summary\n\n")
	fmt.Printf("- Total domains: %d\n", len(all))
	fmt.Printf("- Critical domains: %d\n", critical)
	fmt.Printf("- High complexity domains: %d\n", high)
	fmt.Printf("- Total complexity score: %d\n", totalScore)
	fmt.Printf("- Max domain score: %d\n\n", maxScore)

	fmt.Printf("## Domain Complexity\n\n")
	fmt.Printf("| Tier | Domain | Endpoints | Methods | SQL | Jobs | MW | Score |\n")
	fmt.Printf("| :--- | :--- | ---: | ---: | ---: | ---: | ---: | ---: |\n")

	for _, r := range sorted {
		tier := r.Tier
		if tier == "critical" {
			tier = "**CRITICAL**"
		} else if tier == "high" {
			tier = "**HIGH**"
		} else if tier == "medium" {
			tier = "MED"
		} else {
			tier = "LOW"
		}
		fmt.Printf("| %s | %s | %d | %d | %d | %d | %d | %d |\n",
			tier, r.Domain, r.EndpointCount, r.MethodCount, r.SQLQueryCount, r.JobCount, r.MiddlewareCount, r.Score)
	}

	var hotspots []*DomainComplexity
	for _, r := range sorted {
		if r.Tier == "critical" || r.Tier == "high" {
			hotspots = append(hotspots, r)
		}
	}
	if len(hotspots) > 0 {
		fmt.Printf("\n## Hotspot Domains\n\n")
		for _, r := range hotspots {
			fmt.Printf("### %s (score=%d)\n\n", r.Domain, r.Score)
			fmt.Printf("- Endpoints: %d\n- Methods: %d\n- SQL queries: %d\n- Jobs: %d\n\n",
				r.EndpointCount, r.MethodCount, r.SQLQueryCount, r.JobCount)
			fmt.Printf("**Recommendation:** Consider splitting `%s` into sub-domains or extracting shared logic.\n\n", r.Domain)
		}
	}

	return nil
}

// getDomain extracts the domain name from an object's properties or name.
// Tries "domain" property first, then derives from "file"/"path" property.
func getDomain(o *sdkgraph.GraphObject) string {
	if d, ok := o.Properties["domain"]; ok {
		if s, ok := d.(string); ok && s != "" {
			return s
		}
	}
	// Try file/path
	for _, key := range []string{"file", "path"} {
		if v, ok := o.Properties[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				if d := domainFromPath(s); d != "" {
					return d
				}
			}
		}
	}
	return ""
}

// domainFromPath extracts domain name from a file path like
// "apps/server/domain/agents/handler.go" -> "agents"
func domainFromPath(path string) string {
	const prefix = "apps/server/domain/"
	idx := strings.Index(path, prefix)
	if idx < 0 {
		return ""
	}
	rest := path[idx+len(prefix):]
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func strProp(o *sdkgraph.GraphObject, key string) string {
	if v, ok := o.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func listAllObjects(ctx context.Context, g *sdkgraph.Client, objType string) ([]*sdkgraph.GraphObject, error) {
	const pageSize = 500
	var all []*sdkgraph.GraphObject
	var cursor string
	for {
		resp, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{
			Type:   objType,
			Limit:  pageSize,
			Cursor: cursor,
		})
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

func listAllRelationships(ctx context.Context, g *sdkgraph.Client, relType string) ([]*sdkgraph.GraphRelationship, error) {
	const pageSize = 500
	var all []*sdkgraph.GraphRelationship
	var cursor string
	for {
		resp, err := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{
			Type:   relType,
			Limit:  pageSize,
			Cursor: cursor,
		})
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
