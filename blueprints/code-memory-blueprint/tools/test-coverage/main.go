// Deprecated: use `codebase check coverage` instead. Run `codebase --help` for details.
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
	"github.com/olekukonko/tablewriter/tw"
)

type DomainReport struct {
	Domain        string   `json:"domain"`
	Services      []string `json:"services"`
	MethodCount   int      `json:"method_count"`
	TestedBy      []string `json:"tested_by"`
	TestFileCount int      `json:"test_file_count"`
	JobCount      int      `json:"job_count"`
	HasTests      bool     `json:"has_tests"`
	CoveragePct   float64  `json:"coverage_pct"`
	RiskScore     int      `json:"risk_score"`
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
	domainFilter := flag.String("domain", "", "Filter to a specific domain")
	minMethods := flag.Int("min-methods", 0, "Minimum method count to include in report")
	sortBy := flag.String("sort", "domain", "Sort by: domain, coverage, methods, risk")
	failOnUntested := flag.Bool("fail-on-untested", false, "Exit with code 2 if high-risk untested domains exist")
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
	var services, methods, testSuites, jobs []*sdkgraph.GraphObject
	var testedByRels, definedInRels []*sdkgraph.GraphRelationship
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
		methods, err = listAllObjects(ctx, client.Graph, "Method")
		return err
	})
	fetch(func() error {
		var err error
		testSuites, err = listAllObjects(ctx, client.Graph, "TestSuite")
		return err
	})
	fetch(func() error {
		var err error
		jobs, err = listAllObjects(ctx, client.Graph, "Job")
		return err
	})
	fetch(func() error {
		var err error
		testedByRels, err = listAllRelationships(ctx, client.Graph, "tested_by")
		return err
	})
	fetch(func() error {
		var err error
		definedInRels, err = listAllRelationships(ctx, client.Graph, "defined_in")
		return err
	})

	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("fetch errors: %v", errs)
	}

	// Indexing
	objByID := make(map[string]*sdkgraph.GraphObject)
	for _, o := range services {
		objByID[o.ID] = o
		objByID[o.EntityID] = o
	}
	for _, o := range testSuites {
		objByID[o.ID] = o
		objByID[o.EntityID] = o
	}

	// defined_in: ID -> SourceFile ID
	definedInMap := make(map[string]string)
	for _, r := range definedInRels {
		definedInMap[r.SrcID] = r.DstID
	}

	// tested_by: Service ID -> []TestSuite ID
	testedByMap := make(map[string][]string)
	for _, r := range testedByRels {
		testedByMap[r.SrcID] = append(testedByMap[r.SrcID], r.DstID)
	}

	reports := make(map[string]*DomainReport)
	getReport := func(domain string) *DomainReport {
		if r, ok := reports[domain]; ok {
			return r
		}
		r := &DomainReport{Domain: domain}
		reports[domain] = r
		return r
	}

	for _, s := range services {
		domain := getDomain(s)
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}
		r := getReport(domain)
		r.Services = append(r.Services, s.Properties["name"].(string))

		if tsIDs, ok := testedByMap[s.ID]; ok {
			testFiles := make(map[string]struct{})
			for _, tsID := range tsIDs {
				ts, ok := objByID[tsID]
				if !ok {
					continue
				}
				r.TestedBy = append(r.TestedBy, ts.Properties["name"].(string))
				if sfID, ok := definedInMap[tsID]; ok {
					testFiles[sfID] = struct{}{}
				}
			}
			r.TestFileCount += len(testFiles)
			r.HasTests = true
		}
	}

	for _, m := range methods {
		domain := getDomain(m)
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}
		r := getReport(domain)
		r.MethodCount++
	}

	for _, j := range jobs {
		domain := getDomain(j)
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}
		r := getReport(domain)
		r.JobCount++
	}

	var finalReports []*DomainReport
	for _, r := range reports {
		// CoveragePct: 100% if any service in domain has tests, 0% otherwise.
		// (Binary per domain — a domain is either tested or not.)
		if r.HasTests {
			r.CoveragePct = 100
		}
		r.RiskScore = computeRisk(r)
		if r.MethodCount >= *minMethods {
			finalReports = append(finalReports, r)
		}
	}

	sortReports(finalReports, *sortBy)

	var printErr error
	switch *format {
	case "json":
		printErr = printJSON(finalReports)
	case "markdown":
		printErr = printMarkdown(finalReports, *projectID)
	default:
		printErr = printTable(finalReports, *projectID)
	}
	if printErr != nil {
		return printErr
	}

	if *failOnUntested {
		for _, r := range finalReports {
			if r.RiskScore >= 60 && !r.HasTests {
				os.Exit(2)
			}
		}
	}

	return nil
}

func getDomain(obj *sdkgraph.GraphObject) string {
	if d, ok := obj.Properties["domain"].(string); ok && d != "" {
		return d
	}
	name, _ := obj.Properties["name"].(string)
	name = strings.ToLower(name)
	name = strings.TrimSuffix(name, "service")
	return name
}

func computeRisk(r *DomainReport) int {
	score := 0
	if !r.HasTests {
		score += 40
	}
	if r.MethodCount >= 10 {
		score += 50 // 30 + 20
	} else if r.MethodCount >= 5 {
		score += 30
	}
	if r.JobCount > 0 {
		score += 10
	}
	if score > 100 {
		score = 100
	}
	return score
}

func sortReports(reports []*DomainReport, sortBy string) {
	sort.Slice(reports, func(i, j int) bool {
		switch sortBy {
		case "coverage":
			return reports[i].CoveragePct < reports[j].CoveragePct
		case "methods":
			return reports[i].MethodCount > reports[j].MethodCount
		case "risk":
			return reports[i].RiskScore > reports[j].RiskScore
		default:
			return reports[i].Domain < reports[j].Domain
		}
	})
}

func printTable(reports []*DomainReport, projectID string) error {
	fmt.Printf("┌─ TEST COVERAGE REPORT\n")
	fmt.Printf("  Generated: %s  Project: %s\n\n", time.Now().Format("2006-01-02"), projectID)

	summary := struct {
		Total, Tested, Untested, HighRisk int
		Services, Methods, TestSuites     int
	}{}
	summary.Total = len(reports)
	for _, r := range reports {
		if r.HasTests {
			summary.Tested++
		} else {
			summary.Untested++
		}
		if r.RiskScore >= 60 {
			summary.HighRisk++
		}
		summary.Services += len(reports) // This is wrong, should be len(r.Services)
	}
	// Fix summary.Services
	summary.Services = 0
	for _, r := range reports {
		summary.Services += len(r.Services)
		summary.Methods += r.MethodCount
		summary.TestSuites += len(r.TestedBy)
	}

	fmt.Printf("┌─ SUMMARY\n")
	fmt.Printf("  Total domains   : %d\n", summary.Total)
	pct := 0.0
	if summary.Total > 0 {
		pct = float64(summary.Tested) / float64(summary.Total) * 100
	}
	fmt.Printf("  Tested domains  : %d  (%.1f%%)\n", summary.Tested, pct)
	fmt.Printf("  Untested domains: %d\n", summary.Untested)
	fmt.Printf("  Total services  : %d\n", summary.Services)
	fmt.Printf("  Total methods   : %d\n", summary.Methods)
	fmt.Printf("  Total test suites: %d\n", summary.TestSuites)
	fmt.Printf("  High-risk domains: %d\n\n", summary.HighRisk)

	fmt.Printf("┌─ DOMAIN COVERAGE\n")
	table := tablewriter.NewWriter(os.Stdout)
	table.Header("RISK", "DOMAIN", "SERVICES", "METHODS", "TESTS", "JOBS", "COVERAGE")

	table.Configure(func(cfg *tablewriter.Config) {
		cfg.Behavior.TrimSpace = tw.On
		cfg.Row.Alignment.PerColumn = []tw.Align{
			tw.AlignLeft,  // RISK
			tw.AlignLeft,  // DOMAIN
			tw.AlignRight, // SERVICES
			tw.AlignRight, // METHODS
			tw.AlignRight, // TESTS
			tw.AlignRight, // JOBS
			tw.AlignRight, // COVERAGE
		}
	})

	for _, r := range reports {
		risk := "LOW"
		if r.RiskScore >= 80 {
			risk = "CRIT"
		} else if r.RiskScore >= 60 {
			risk = "HIGH"
		} else if r.RiskScore >= 40 {
			risk = "MED"
		}

		row := []string{
			risk,
			r.Domain,
			fmt.Sprintf("%d", len(r.Services)),
			fmt.Sprintf("%d", r.MethodCount),
			fmt.Sprintf("%d", len(r.TestedBy)),
			fmt.Sprintf("%d", r.JobCount),
			fmt.Sprintf("%.0f%%", r.CoveragePct),
		}
		table.Append(row)
	}
	table.Render()

	fmt.Printf("\n┌─ HIGH RISK DOMAINS (risk score ≥ 60)\n")
	for _, r := range reports {
		if r.RiskScore >= 60 {
			fmt.Printf("  • %s: %d methods, %d jobs. Recommendation: Add integration tests for %s.\n", r.Domain, r.MethodCount, r.JobCount, strings.Join(r.Services, ", "))
		}
	}

	fmt.Printf("\n┌─ UNTESTED SERVICES\n")
	for _, r := range reports {
		if !r.HasTests {
			for _, s := range r.Services {
				fmt.Printf("  • %s\n", s)
			}
		}
	}

	return nil
}

func printMarkdown(reports []*DomainReport, projectID string) error {
	fmt.Printf("# Test Coverage Report\n\n")
	fmt.Printf("Generated: %s | Project: `%s`\n\n", time.Now().Format("2006-01-02"), projectID)

	fmt.Printf("## Summary\n\n")
	summary := struct {
		Total, Tested, Untested, HighRisk int
		Services, Methods, TestSuites     int
	}{}
	summary.Total = len(reports)
	for _, r := range reports {
		if r.HasTests {
			summary.Tested++
		} else {
			summary.Untested++
		}
		if r.RiskScore >= 60 {
			summary.HighRisk++
		}
		summary.Services += len(r.Services)
		summary.Methods += r.MethodCount
		summary.TestSuites += len(r.TestedBy)
	}
	pct := 0.0
	if summary.Total > 0 {
		pct = float64(summary.Tested) / float64(summary.Total) * 100
	}

	fmt.Printf("- Total domains: %d\n", summary.Total)
	fmt.Printf("- Tested domains: %d (%.1f%%)\n", summary.Tested, pct)
	fmt.Printf("- Untested domains: %d\n", summary.Untested)
	fmt.Printf("- Total services: %d\n", summary.Services)
	fmt.Printf("- Total methods: %d\n", summary.Methods)
	fmt.Printf("- Total test suites: %d\n", summary.TestSuites)
	fmt.Printf("- High-risk domains: %d\n\n", summary.HighRisk)

	fmt.Printf("## Domain Coverage\n\n")
	fmt.Printf("| Risk | Domain | Services | Methods | Tests | Jobs | Coverage |\n")
	fmt.Printf("| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")
	for _, r := range reports {
		risk := "LOW"
		if r.RiskScore >= 80 {
			risk = "**CRIT**"
		} else if r.RiskScore >= 60 {
			risk = "**HIGH**"
		} else if r.RiskScore >= 40 {
			risk = "MED"
		}
		fmt.Printf("| %s | %s | %d | %d | %d | %d | %.0f%% |\n", risk, r.Domain, len(r.Services), r.MethodCount, len(r.TestedBy), r.JobCount, r.CoveragePct)
	}

	fmt.Printf("\n## High Risk Domains\n\n")
	for _, r := range reports {
		if r.RiskScore >= 60 {
			fmt.Printf("- **%s**: %d methods, %d jobs. Recommendation: Add integration tests for %s.\n", r.Domain, r.MethodCount, r.JobCount, strings.Join(r.Services, ", "))
		}
	}

	return nil
}

func printJSON(reports []*DomainReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(reports)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func listAllObjects(ctx context.Context, g *sdkgraph.Client, objType string) ([]*sdkgraph.GraphObject, error) {
	var all []*sdkgraph.GraphObject
	cursor := ""
	for {
		opts := &sdkgraph.ListObjectsOptions{Type: objType, Limit: 500}
		if cursor != "" {
			opts.Cursor = cursor
		}
		resp, err := g.ListObjects(ctx, opts)
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

func listAllRelationships(ctx context.Context, g *sdkgraph.Client, relType string) ([]*sdkgraph.GraphRelationship, error) {
	var all []*sdkgraph.GraphRelationship
	cursor := ""
	for {
		opts := &sdkgraph.ListRelationshipsOptions{Type: relType, Limit: 500}
		if cursor != "" {
			opts.Cursor = cursor
		}
		resp, err := g.ListRelationships(ctx, opts)
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
