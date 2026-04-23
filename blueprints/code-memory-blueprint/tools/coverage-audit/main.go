// Deprecated: use `codebase check coverage` instead. Run `codebase --help` for details.
// coverage-audit: graph-only tool reporting test coverage gaps across domains/services.
// Reads tested_by rels, Services, Domains, APIEndpoints, Methods from the graph.
// No file scanning — pure graph analysis.
//
// Usage:
//
//	MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... go run ./...
//	  --format table|json|markdown   (default: table)
//	  --domain <name>                filter to one domain
//	  --min-coverage <0-100>         only show domains below this threshold (default: 100)
//	  --branch <id>                  read from a specific branch (default: main)
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
	"github.com/olekukonko/tablewriter"
)

// ── flags ────────────────────────────────────────────────────────────────────

var (
	flagFormat      = flag.String("format", "table", "Output format: table, json, markdown")
	flagDomain      = flag.String("domain", "", "Filter to a specific domain")
	flagMinCoverage = flag.Int("min-coverage", 100, "Only show domains below this coverage % (0=show all)")
	flagBranch      = flag.String("branch", "", "Branch ID to read from (default: main branch)")
)

// ── data model ───────────────────────────────────────────────────────────────

type DomainReport struct {
	Domain          string   `json:"domain"`
	ServiceName     string   `json:"service_name"`
	ServiceID       string   `json:"service_id"`
	EndpointCount   int      `json:"endpoint_count"`
	MethodCount     int      `json:"method_count"`
	TestSuiteCount  int      `json:"test_suite_count"`
	TestedByCount   int      `json:"tested_by_count"`
	CoveragePercent int      `json:"coverage_percent"` // real tests only; planned don't count
	HasTests        bool     `json:"has_tests"`
	HasPlanned      bool     `json:"has_planned"`      // planned TestSuites exist (intent)
	TestSuiteNames  []string `json:"test_suite_names,omitempty"`
}

type Report struct {
	Generated      string         `json:"generated"`
	TotalDomains   int            `json:"total_domains"`
	CoveredDomains int            `json:"covered_domains"`
	UncoveredCount int            `json:"uncovered_count"`
	Domains        []DomainReport `json:"domains"`
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	branch := *flagBranch
	branchLabel := "main"
	if branch != "" {
		branchLabel = branch
	}
	fmt.Fprintf(os.Stderr, "→ Fetching graph data (branch: %s)...\n", branchLabel)

	// Fetch in parallel: Services, TestSuites, tested_by rels, APIEndpoints, Methods, Domains
	var (
		services     []*sdkgraph.GraphObject
		testSuites   []*sdkgraph.GraphObject
		testedByRels []*sdkgraph.GraphRelationship
		endpoints    []*sdkgraph.GraphObject
		methods      []*sdkgraph.GraphObject
		domains      []*sdkgraph.GraphObject
		mu           sync.Mutex
		wg           sync.WaitGroup
		fetchErr     error
	)

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				fetchErr = err
				mu.Unlock()
			}
		}()
	}

	fetch(func() error {
		r, e := listAllObjects(ctx, client.Graph, "Service", branch)
		services = r; return e
	})
	fetch(func() error {
		r, e := listAllObjects(ctx, client.Graph, "TestSuite", branch)
		testSuites = r; return e
	})
	fetch(func() error {
		r, e := listAllRels(ctx, client.Graph, "tested_by", branch)
		testedByRels = r; return e
	})
	fetch(func() error {
		r, e := listAllObjects(ctx, client.Graph, "APIEndpoint", branch)
		endpoints = r; return e
	})
	fetch(func() error {
		r, e := listAllObjects(ctx, client.Graph, "Method", branch)
		methods = r; return e
	})
	fetch(func() error {
		r, e := listAllObjects(ctx, client.Graph, "Domain", branch)
		domains = r; return e
	})

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	fmt.Fprintf(os.Stderr, "  Services: %d, TestSuites: %d, tested_by rels: %d, Endpoints: %d, Methods: %d, Domains: %d\n",
		len(services), len(testSuites), len(testedByRels), len(endpoints), len(methods), len(domains))

	// ── Build indexes ─────────────────────────────────────────────────────────

	// tested_by: src (Service/Method) → []dst (TestSuite) entity IDs
	testedBySrc := make(map[string][]string)
	for _, r := range testedByRels {
		testedBySrc[r.SrcID] = append(testedBySrc[r.SrcID], r.DstID)
	}

	// TestSuite by entity ID → name; track planned vs real
	tsByID := make(map[string]string)
	tsPlanned := make(map[string]bool) // entity ID → true if status=planned
	for _, ts := range testSuites {
		name := strProp(ts, "name")
		tsByID[ts.EntityID] = name
		if strProp(ts, "status") == "planned" {
			tsPlanned[ts.EntityID] = true
		}
	}

	// Endpoints by domain
	epByDomain := make(map[string]int)
	for _, ep := range endpoints {
		d := strProp(ep, "domain")
		if d != "" {
			epByDomain[strings.ToLower(d)]++
		}
	}

	// Methods by domain
	methodByDomain := make(map[string]int)
	for _, m := range methods {
		d := strProp(m, "domain")
		if d != "" {
			methodByDomain[strings.ToLower(d)]++
		}
	}

	// Domain name → Domain object
	domainByName := make(map[string]*sdkgraph.GraphObject)
	for _, d := range domains {
		name := strings.ToLower(strProp(d, "name"))
		if name == "" {
			// fallback: derive from key
			if d.Key != nil {
				parts := strings.SplitN(*d.Key, "-", 2)
				if len(parts) == 2 {
					name = parts[1]
				}
			}
		}
		if name != "" {
			domainByName[name] = d
		}
	}

	// ── Build per-service report ──────────────────────────────────────────────

	var reports []DomainReport
	for _, svc := range services {
		svcName := strProp(svc, "name")
		// Derive domain from service name: "AgentsService" → "agents"
		domain := strings.ToLower(strings.TrimSuffix(svcName, "Service"))

		if *flagDomain != "" && !strings.EqualFold(domain, *flagDomain) {
			continue
		}

		tsIDs := testedBySrc[svc.EntityID]
		tsNames := make([]string, 0, len(tsIDs))
		realCount := 0
		plannedCount := 0
		for _, id := range tsIDs {
			if name, ok := tsByID[id]; ok {
				tsNames = append(tsNames, name)
			}
			if tsPlanned[id] {
				plannedCount++
			} else {
				realCount++
			}
		}
		sort.Strings(tsNames)

		hasRealTests := realCount > 0
		hasPlanned := plannedCount > 0
		epCount := epByDomain[domain]
		methodCount := methodByDomain[domain]

		// Coverage: real tests → 100%, planned only → 0% (intent not reality)
		coverage := 0
		if hasRealTests {
			coverage = 100
		}

		reports = append(reports, DomainReport{
			Domain:          domain,
			ServiceName:     svcName,
			ServiceID:       svc.EntityID,
			EndpointCount:   epCount,
			MethodCount:     methodCount,
			TestSuiteCount:  realCount + plannedCount,
			TestedByCount:   realCount,
			CoveragePercent: coverage,
			HasTests:        hasRealTests,
			HasPlanned:      hasPlanned,
			TestSuiteNames: tsNames,
		})
	}

	// Sort: no tests + no planned first, then planned-only, then covered
	sort.Slice(reports, func(i, j int) bool {
		si := statusRank(reports[i])
		sj := statusRank(reports[j])
		if si != sj {
			return si < sj
		}
		return reports[i].Domain < reports[j].Domain
	})

	// Filter by min-coverage
	if *flagMinCoverage < 100 {
		filtered := reports[:0]
		for _, r := range reports {
			if r.CoveragePercent < *flagMinCoverage {
				filtered = append(filtered, r)
			}
		}
		reports = filtered
	}

	// ── Summary stats ─────────────────────────────────────────────────────────
	covered, planned := 0, 0
	for _, r := range reports {
		if r.HasTests {
			covered++
		} else if r.HasPlanned {
			planned++
		}
	}

	report := Report{
		Generated:      time.Now().Format("2006-01-02"),
		TotalDomains:   len(reports),
		CoveredDomains: covered,
		UncoveredCount: len(reports) - covered - planned,
		Domains:        reports,
	}

	// ── Output ────────────────────────────────────────────────────────────────
	switch *flagFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "markdown":
		printMarkdown(report)
	default:
		printTable(report)
	}
	return nil
}

func printTable(r Report) {
	fmt.Printf("\n┌─ TEST COVERAGE AUDIT\n  Generated: %s\n\n", r.Generated)
	planned := r.TotalDomains - r.CoveredDomains - r.UncoveredCount
	fmt.Printf("┌─ SUMMARY\n")
	fmt.Printf("  Total services  : %d\n", r.TotalDomains)
	fmt.Printf("  Covered (real)  : %d\n", r.CoveredDomains)
	fmt.Printf("  Planned (gap)   : %d\n", planned)
	fmt.Printf("  No tests at all : %d\n", r.UncoveredCount)
	pct := 0
	if r.TotalDomains > 0 {
		pct = r.CoveredDomains * 100 / r.TotalDomains
	}
	fmt.Printf("  Coverage        : %d%%\n\n", pct)

	if len(r.Domains) == 0 {
		fmt.Println("✓ All services have test coverage.")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Domain", "Service", "Endpoints", "Methods", "Test Suites", "Status")

	for _, d := range r.Domains {
		status := statusLabel(d)
		table.Append([]string{
			d.Domain,
			d.ServiceName,
			fmt.Sprintf("%d", d.EndpointCount),
			fmt.Sprintf("%d", d.MethodCount),
			fmt.Sprintf("%d", d.TestedByCount),
			status,
		})
	}
	table.Render()
	fmt.Println("  Legend: ✓ covered  ◌ planned (not written)  ✗ no tests")
}

func printMarkdown(r Report) {
	fmt.Printf("# Test Coverage Audit\n\nGenerated: %s\n\n", r.Generated)
	pct := 0
	if r.TotalDomains > 0 {
		pct = r.CoveredDomains * 100 / r.TotalDomains
	}
	fmt.Printf("**Coverage: %d%% (%d/%d services)**\n\n", pct, r.CoveredDomains, r.TotalDomains)
	fmt.Println("| Domain | Service | Endpoints | Methods | Real Tests | Status |")
	fmt.Println("|--------|---------|-----------|---------|------------|--------|")
	for _, d := range r.Domains {
		fmt.Printf("| %s | %s | %d | %d | %d | %s |\n",
			d.Domain, d.ServiceName, d.EndpointCount, d.MethodCount, d.TestedByCount, statusLabel(d))
	}
}

func statusLabel(d DomainReport) string {
	if d.HasTests {
		return "✓ covered"
	}
	if d.HasPlanned {
		return "◌ planned"
	}
	return "✗ NO TESTS"
}

func statusRank(d DomainReport) int {
	if d.HasTests {
		return 2 // covered last
	}
	if d.HasPlanned {
		return 1 // planned middle
	}
	return 0 // no tests first
}

// ── helpers ──────────────────────────────────────────────────────────────────

func listAllObjects(ctx context.Context, g *sdkgraph.Client, objType, branch string) ([]*sdkgraph.GraphObject, error) {
	const pageSize = 500
	var all []*sdkgraph.GraphObject
	cursor := ""
	for {
		resp, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: pageSize, Cursor: cursor, BranchID: branch})
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

func listAllRels(ctx context.Context, g *sdkgraph.Client, relType, branch string) ([]*sdkgraph.GraphRelationship, error) {
	const pageSize = 500
	var all []*sdkgraph.GraphRelationship
	cursor := ""
	for {
		resp, err := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: relType, Limit: pageSize, Cursor: cursor, BranchID: branch})
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

func strProp(o *sdkgraph.GraphObject, key string) string {
	if v, ok := o.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
