// Deprecated: use `codebase check complexity --recommendations` instead. Run `codebase --help` for details.
// refactor-advisor: cross-joins complexity and test coverage data from the
// Memory knowledge graph to produce prioritized refactoring recommendations.
//
// Priority score = complexity_score × test_penalty
//   - test_penalty = 1.0 (no tests) → 0.2 (well-tested)
//   - penalty = max(0.2, 1.0 - test_file_count × 0.1)
//
// Each domain gets one or more actionable recommendations based on its profile:
//   - CRITICAL untested → "Add tests before any refactor"
//   - High endpoint count, low methods → "Extract service layer"
//   - High SQL, no methods → "Add repository abstraction"
//   - High complexity, tested → "Safe to refactor: split domain"
//
// Usage:
//
//	MEMORY_API_KEY=<token> MEMORY_PROJECT_ID=<id> MEMORY_SERVER_URL=https://... \
//	  go run . [--format table|markdown|json] [--top N] [--min-priority N]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// DomainProfile combines complexity and coverage data for one domain.
type DomainProfile struct {
	Domain          string   `json:"domain"`
	ComplexityScore int      `json:"complexity_score"`
	ComplexityTier  string   `json:"complexity_tier"`
	EndpointCount   int      `json:"endpoint_count"`
	MethodCount     int      `json:"method_count"`
	SQLQueryCount   int      `json:"sql_query_count"`
	JobCount        int      `json:"job_count"`
	HasTests        bool     `json:"has_tests"`
	TestFileCount   int      `json:"test_file_count"`
	TestSuites      []string `json:"test_suites,omitempty"`
	PriorityScore   int      `json:"priority_score"`
	PriorityTier    string   `json:"priority_tier"` // P0/P1/P2/P3
	Actions         []Action `json:"actions"`
}

// Action is a concrete refactoring recommendation.
type Action struct {
	Code        string `json:"code"`        // e.g. "ADD_TESTS"
	Description string `json:"description"` // human-readable
	Effort      string `json:"effort"`      // low / medium / high
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
	topN := flag.Int("top", 0, "Show only top N domains by priority (0 = all)")
	minPriority := flag.Int("min-priority", 0, "Minimum priority score to include")
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

	// Fetch all object types in parallel
	var wg sync.WaitGroup
	var services, endpoints, methods, sqlqueries, jobs, testSuites, sourceFiles []*sdkgraph.GraphObject
	var definedInRels, testedByRels, handlesRels []*sdkgraph.GraphRelationship
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

	fetch(func() error { var e error; services, e = listAllObjects(ctx, client.Graph, "Service"); return e })
	fetch(func() error { var e error; endpoints, e = listAllObjects(ctx, client.Graph, "APIEndpoint"); return e })
	fetch(func() error { var e error; methods, e = listAllObjects(ctx, client.Graph, "Method"); return e })
	fetch(func() error { var e error; sqlqueries, e = listAllObjects(ctx, client.Graph, "SQLQuery"); return e })
	fetch(func() error { var e error; jobs, e = listAllObjects(ctx, client.Graph, "Job"); return e })
	fetch(func() error { var e error; testSuites, e = listAllObjects(ctx, client.Graph, "TestSuite"); return e })
	fetch(func() error { var e error; sourceFiles, e = listAllObjects(ctx, client.Graph, "SourceFile"); return e })
	fetch(func() error { var e error; definedInRels, e = listAllRelationships(ctx, client.Graph, "defined_in"); return e })
	fetch(func() error { var e error; testedByRels, e = listAllRelationships(ctx, client.Graph, "tested_by"); return e })
	fetch(func() error { var e error; handlesRels, e = listAllRelationships(ctx, client.Graph, "handles"); return e })

	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("fetch errors: %v", errs)
	}

	// ── Index building ──────────────────────────────────────────────────────

	// SourceFile ID -> domain
	sfIDToDomain := make(map[string]string)
	for _, sf := range sourceFiles {
		path := strProp(sf, "path")
		if d := domainFromPath(path); d != "" {
			sfIDToDomain[sf.ID] = d
			sfIDToDomain[sf.EntityID] = d
		}
	}

	// Object ID -> domain via defined_in -> SourceFile
	objIDToDomain := make(map[string]string)
	for _, r := range definedInRels {
		if d, ok := sfIDToDomain[r.DstID]; ok {
			objIDToDomain[r.SrcID] = d
		}
	}

	// handles: endpoint ID -> service ID
	endpointToService := make(map[string]string)
	for _, r := range handlesRels {
		endpointToService[r.DstID] = r.SrcID
	}

	// Service ID -> domain (index both version ID and entity ID)
	serviceIDToDomain := make(map[string]string)
	for _, s := range services {
		d := getDomain(s)
		if d == "" {
			d = objIDToDomain[s.ID]
		}
		if d == "" {
			d = objIDToDomain[s.EntityID]
		}
		if d != "" {
			serviceIDToDomain[s.ID] = d
			serviceIDToDomain[s.EntityID] = d
		}
	}

	// TestSuite ID -> name
	tsIDToName := make(map[string]string)
	for _, ts := range testSuites {
		tsIDToName[ts.ID] = strProp(ts, "name")
		tsIDToName[ts.EntityID] = strProp(ts, "name")
	}

	// tested_by: service ID -> []test suite IDs
	testedByMap := make(map[string][]string)
	for _, r := range testedByRels {
		testedByMap[r.SrcID] = append(testedByMap[r.SrcID], r.DstID)
	}

	// ── Build domain profiles ───────────────────────────────────────────────

	profiles := make(map[string]*DomainProfile)
	get := func(domain string) *DomainProfile {
		if p, ok := profiles[domain]; ok {
			return p
		}
		p := &DomainProfile{Domain: domain}
		profiles[domain] = p
		return p
	}

	domainOf := func(o *sdkgraph.GraphObject) string {
		d := getDomain(o)
		if d == "" {
			d = objIDToDomain[o.ID]
		}
		return d
	}

	// Services + test coverage
	for _, s := range services {
		d := domainOf(s)
		if d == "" {
			d = serviceIDToDomain[s.ID]
		}
		if d == "" || (*domainFilter != "" && d != *domainFilter) {
			continue
		}
		p := get(d)
		// tested_by rels use EntityID (stable) as src_id
		for _, lookupID := range []string{s.EntityID, s.ID} {
			if tsIDs, ok := testedByMap[lookupID]; ok {
				p.HasTests = true
				seen := make(map[string]struct{})
				for _, tsID := range tsIDs {
					name := tsIDToName[tsID]
					if name == "" {
						name = tsIDToName[tsID]
					}
					if name != "" {
						if _, dup := seen[name]; !dup {
							p.TestSuites = append(p.TestSuites, name)
							seen[name] = struct{}{}
						}
					}
				}
				p.TestFileCount = len(seen)
				break
			}
		}
	}

	// Endpoints
	for _, ep := range endpoints {
		d := domainOf(ep)
		if d == "" {
			if svcID, ok := endpointToService[ep.ID]; ok {
				d = serviceIDToDomain[svcID]
			}
		}
		if d == "" || (*domainFilter != "" && d != *domainFilter) {
			continue
		}
		get(d).EndpointCount++
	}

	// Methods
	for _, m := range methods {
		d := domainOf(m)
		if d == "" || (*domainFilter != "" && d != *domainFilter) {
			continue
		}
		get(d).MethodCount++
	}

	// SQL queries
	for _, sq := range sqlqueries {
		d := domainOf(sq)
		if d == "" {
			d = domainFromPath(strProp(sq, "file"))
		}
		if d == "" || (*domainFilter != "" && d != *domainFilter) {
			continue
		}
		get(d).SQLQueryCount++
	}

	// Jobs
	for _, j := range jobs {
		d := domainOf(j)
		if d == "" || (*domainFilter != "" && d != *domainFilter) {
			continue
		}
		get(d).JobCount++
	}

	// ── Score and recommend ─────────────────────────────────────────────────

	for _, p := range profiles {
		// Complexity score (same formula as graph-complexity tool)
		p.ComplexityScore = p.EndpointCount*3 + p.MethodCount*2 + p.SQLQueryCount + p.JobCount*2
		switch {
		case p.ComplexityScore >= 100:
			p.ComplexityTier = "critical"
		case p.ComplexityScore >= 50:
			p.ComplexityTier = "high"
		case p.ComplexityScore >= 20:
			p.ComplexityTier = "medium"
		default:
			p.ComplexityTier = "low"
		}

		// Test penalty: 1.0 = no tests, 0.2 = well-tested (≥8 test files)
		testPenalty := 1.0
		if p.HasTests {
			testPenalty = math.Max(0.2, 1.0-float64(p.TestFileCount)*0.1)
		}
		p.PriorityScore = int(float64(p.ComplexityScore) * testPenalty)

		switch {
		case p.PriorityScore >= 80:
			p.PriorityTier = "P0"
		case p.PriorityScore >= 40:
			p.PriorityTier = "P1"
		case p.PriorityScore >= 15:
			p.PriorityTier = "P2"
		default:
			p.PriorityTier = "P3"
		}

		// Generate actions
		p.Actions = generateActions(p)
	}

	// ── Sort and filter ─────────────────────────────────────────────────────

	sorted := make([]*DomainProfile, 0, len(profiles))
	for _, p := range profiles {
		if p.PriorityScore >= *minPriority {
			sorted = append(sorted, p)
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].PriorityScore != sorted[j].PriorityScore {
			return sorted[i].PriorityScore > sorted[j].PriorityScore
		}
		return sorted[i].Domain < sorted[j].Domain
	})
	if *topN > 0 && *topN < len(sorted) {
		sorted = sorted[:*topN]
	}

	// ── Output ──────────────────────────────────────────────────────────────

	switch *format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(sorted)
	case "markdown":
		return printMarkdown(sorted, profiles)
	default:
		return printTable(sorted, profiles)
	}
}

func generateActions(p *DomainProfile) []Action {
	var actions []Action

	// No tests at all on a complex domain → highest urgency
	if !p.HasTests && p.ComplexityScore >= 20 {
		actions = append(actions, Action{
			Code:        "ADD_TESTS",
			Description: fmt.Sprintf("Add integration tests for %sService — no test coverage on a %s-complexity domain", capitalize(p.Domain), p.ComplexityTier),
			Effort:      "high",
		})
	}

	// Many endpoints, few methods → thin handler layer, no service abstraction
	if p.EndpointCount >= 10 && p.MethodCount < p.EndpointCount/3 {
		actions = append(actions, Action{
			Code:        "EXTRACT_SERVICE_LAYER",
			Description: fmt.Sprintf("%d endpoints but only %d service methods — extract business logic from handlers into a proper service layer", p.EndpointCount, p.MethodCount),
			Effort:      "high",
		})
	}

	// Many SQL queries, no methods → repository without abstraction
	if p.SQLQueryCount >= 10 && p.MethodCount == 0 {
		actions = append(actions, Action{
			Code:        "ADD_REPOSITORY_ABSTRACTION",
			Description: fmt.Sprintf("%d SQL queries with no service methods — add a service layer to encapsulate data access logic", p.SQLQueryCount),
			Effort:      "medium",
		})
	}

	// Critical complexity, well-tested → safe to split
	if p.ComplexityTier == "critical" && p.HasTests && p.TestFileCount >= 3 {
		actions = append(actions, Action{
			Code:        "SPLIT_DOMAIN",
			Description: fmt.Sprintf("Domain is well-tested (%d test files) and critically complex (score=%d) — safe to split into sub-domains", p.TestFileCount, p.ComplexityScore),
			Effort:      "high",
		})
	}

	// High endpoint count → consider API versioning or grouping
	if p.EndpointCount >= 20 {
		actions = append(actions, Action{
			Code:        "GROUP_ENDPOINTS",
			Description: fmt.Sprintf("%d endpoints — consider grouping into sub-resources or introducing API versioning", p.EndpointCount),
			Effort:      "medium",
		})
	}

	// Jobs with no tests → async code is hardest to debug
	if p.JobCount >= 2 && !p.HasTests {
		actions = append(actions, Action{
			Code:        "TEST_ASYNC_JOBS",
			Description: fmt.Sprintf("%d background jobs with no test coverage — async failures are hardest to diagnose", p.JobCount),
			Effort:      "medium",
		})
	}

	// Low complexity, no tests → quick win
	if !p.HasTests && p.ComplexityScore < 20 && p.ComplexityScore > 0 {
		actions = append(actions, Action{
			Code:        "QUICK_WIN_TESTS",
			Description: "Low complexity domain with no tests — easy to add coverage, high ROI",
			Effort:      "low",
		})
	}

	// Nothing flagged → healthy
	if len(actions) == 0 {
		actions = append(actions, Action{
			Code:        "OK",
			Description: "No immediate refactoring needed",
			Effort:      "none",
		})
	}

	return actions
}

func printTable(sorted []*DomainProfile, all map[string]*DomainProfile) error {
	now := time.Now().Format("2006-01-02")
	p0, p1, p2, p3 := 0, 0, 0, 0
	for _, p := range all {
		switch p.PriorityTier {
		case "P0":
			p0++
		case "P1":
			p1++
		case "P2":
			p2++
		case "P3":
			p3++
		}
	}

	fmt.Printf("┌─ REFACTORING ADVISOR REPORT\n")
	fmt.Printf("  Generated: %s\n\n", now)

	fmt.Printf("┌─ SUMMARY\n")
	fmt.Printf("  Total domains: %d\n", len(all))
	fmt.Printf("  P0 (critical, act now): %d\n", p0)
	fmt.Printf("  P1 (high, plan soon)  : %d\n", p1)
	fmt.Printf("  P2 (medium, backlog)  : %d\n", p2)
	fmt.Printf("  P3 (low, monitor)     : %d\n\n", p3)

	fmt.Printf("┌─ DOMAIN PRIORITIES\n")
	table := tablewriter.NewWriter(os.Stdout)
	table.Header("PRI", "DOMAIN", "PRIORITY", "ENDPOINTS", "METHODS", "SQL", "TESTS", "CPLX")

	table.Configure(func(cfg *tablewriter.Config) {
		cfg.Behavior.TrimSpace = tw.On
		cfg.Row.Alignment.PerColumn = []tw.Align{
			tw.AlignCenter, // PRI
			tw.AlignLeft,   // DOMAIN
			tw.AlignRight,  // PRIORITY
			tw.AlignRight,  // ENDPOINTS
			tw.AlignRight,  // METHODS
			tw.AlignRight,  // SQL
			tw.AlignRight,  // TESTS
			tw.AlignRight,  // CPLX
		}
	})

	for _, p := range sorted {
		tested := "NONE"
		if p.HasTests {
			tested = fmt.Sprintf("%d", p.TestFileCount)
		}
		row := []string{
			p.PriorityTier, p.Domain, fmt.Sprintf("%d", p.PriorityScore),
			fmt.Sprintf("%d", p.EndpointCount), fmt.Sprintf("%d", p.MethodCount),
			fmt.Sprintf("%d", p.SQLQueryCount), tested, fmt.Sprintf("%d", p.ComplexityScore),
		}
		table.Append(row)
	}
	table.Render()

	// Detailed recommendations for P0/P1
	var urgent []*DomainProfile
	for _, p := range sorted {
		if p.PriorityTier == "P0" || p.PriorityTier == "P1" {
			urgent = append(urgent, p)
		}
	}
	if len(urgent) > 0 {
		fmt.Printf("\n┌─ RECOMMENDATIONS (P0 + P1)\n")
		recTable := tablewriter.NewWriter(os.Stdout)
		recTable.Header("PRIORITY", "DOMAIN", "CODE", "DESCRIPTION", "EFFORT")

		recTable.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.Alignment.Global = tw.AlignLeft
			cfg.Row.ColMaxWidths.Global = 50
		})

		for _, p := range urgent {
			for _, a := range p.Actions {
				if a.Code == "OK" {
					continue
				}
				row := []string{p.PriorityTier, p.Domain, a.Code, a.Description, a.Effort}
				recTable.Append(row)
			}
		}
		recTable.Render()
	}

	return nil
}

func printMarkdown(sorted []*DomainProfile, all map[string]*DomainProfile) error {
	now := time.Now().Format("2006-01-02")
	p0, p1, p2, p3 := 0, 0, 0, 0
	for _, p := range all {
		switch p.PriorityTier {
		case "P0":
			p0++
		case "P1":
			p1++
		case "P2":
			p2++
		case "P3":
			p3++
		}
	}

	fmt.Printf("# Refactoring Advisor Report\n\nGenerated: %s\n\n", now)
	fmt.Printf("## Summary\n\n- Total domains: %d\n- P0 (act now): %d\n- P1 (plan soon): %d\n- P2 (backlog): %d\n- P3 (monitor): %d\n\n", len(all), p0, p1, p2, p3)

	fmt.Printf("## Domain Priorities\n\n")
	fmt.Printf("| Priority | Domain | Score | Endpoints | Methods | SQL | Tests | Complexity |\n")
	fmt.Printf("| :--- | :--- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, p := range sorted {
		tested := "none"
		if p.HasTests {
			tested = fmt.Sprintf("%d", p.TestFileCount)
		}
		tier := p.PriorityTier
		if tier == "P0" {
			tier = "**P0**"
		}
		fmt.Printf("| %s | %s | %d | %d | %d | %d | %s | %d |\n",
			tier, p.Domain, p.PriorityScore, p.EndpointCount, p.MethodCount, p.SQLQueryCount, tested, p.ComplexityScore)
	}

	fmt.Printf("\n## Recommendations\n\n")
	for _, p := range sorted {
		if p.PriorityTier == "P3" {
			continue
		}
		tested := "none"
		if p.HasTests {
			tested = fmt.Sprintf("%d test files", p.TestFileCount)
		}
		fmt.Printf("### [%s] %s\n\n**Priority score:** %d | **Complexity:** %d (%s) | **Tests:** %s\n\n",
			p.PriorityTier, p.Domain, p.PriorityScore, p.ComplexityScore, p.ComplexityTier, tested)
		for _, a := range p.Actions {
			fmt.Printf("- **[%s]** %s *(effort: %s)*\n", a.Code, a.Description, a.Effort)
		}
		fmt.Println()
	}

	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func getDomain(o *sdkgraph.GraphObject) string {
	if d, ok := o.Properties["domain"]; ok {
		if s, ok := d.(string); ok && s != "" {
			return s
		}
	}
	for _, key := range []string{"file", "path"} {
		if v, ok := o.Properties[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				if d := domainFromPath(s); d != "" {
					return d
				}
			}
		}
	}
	// Service objects: "AgentsService" -> "agents", "GraphService" -> "graph"
	if name, ok := o.Properties["name"]; ok {
		if s, ok := name.(string); ok && strings.HasSuffix(s, "Service") {
			return strings.ToLower(strings.TrimSuffix(s, "Service"))
		}
	}
	return ""
}

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

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
