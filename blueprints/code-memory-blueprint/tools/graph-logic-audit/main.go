// Deprecated: use `codebase check logic` instead. Run `codebase --help` for details.
// graph-logic-audit: cross-object consistency checker for the Memory knowledge graph.
// Pure graph analysis — no file scanning.
//
// Checks:
//   CONFIGVAR_ORPHAN     ConfigVar config_group has no plausible domain mapping
//   DOMAIN_NO_ENDPOINTS  Domain exists but has zero APIEndpoints
//   DOMAIN_NO_SERVICE    Domain exists but has no matching Service
//   DOMAIN_NO_TESTS      Domain has endpoints but zero real TestSuites
//   ENDPOINT_HEAVY       Domain has 3x average endpoint count (god domain)
//   ENDPOINT_SPARSE      Domain has endpoints but only 1-2 (stub domain)
//   JOB_NO_ENDPOINT      Job domain has zero APIEndpoints (no trigger surface)
//   EVENT_NO_ENDPOINT    Event domain has no SSE/stream endpoint
//   SERVICE_NO_DOMAIN    Service name doesn't map to any Domain
//   SCENARIO_NO_ENDPOINT Scenario domain has zero APIEndpoints (planned but unimplemented)
//   SCENARIO_PLANNED     Scenario with status=planned (implementation gap summary)
//   EXTDEP_DUPLICATE     Multiple ExternalDependencies in same category (consolidation candidate)
//
// Usage:
//   MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... go run ./...
//     --format table|json|markdown   (default: table)
//     --checks CHECK1,CHECK2,...      (default: all)
//     --domain <name>                filter to one domain
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
	flagFormat  = flag.String("format", "table", "Output format: table, json, markdown")
	flagChecks  = flag.String("checks", "", "Comma-separated checks to run (default: all)")
	flagDomain  = flag.String("domain", "", "Filter findings to a specific domain")
	flagVerbose = flag.Bool("verbose", false, "Include passing checks in output")
)

// ── data model ───────────────────────────────────────────────────────────────

type Finding struct {
	Check   string `json:"check"`
	Domain  string `json:"domain"`
	Object  string `json:"object"`
	Detail  string `json:"detail"`
	Tier    int    `json:"tier"` // 1=bug, 2=design, 3=concept
}

type CheckSummary struct {
	Check    string `json:"check"`
	Count    int    `json:"count"`
	Tier     int    `json:"tier"`
	Desc     string `json:"desc"`
}

type Report struct {
	Generated string         `json:"generated"`
	Findings  []Finding      `json:"findings"`
	Summary   []CheckSummary `json:"summary"`
}

// configGroup → domain name mappings (best-effort)
var configGroupToDomain = map[string]string{
	"agents":        "agents",
	"ai":            "provider",
	"auth":          "authinfo",
	"database":      "graph",
	"email":         "email",
	"embeddings":    "extraction",
	"features":      "",   // cross-cutting
	"graph":         "graph",
	"llm":           "provider",
	"observability": "",   // cross-cutting
	"parsing":       "extraction",
	"sandbox":       "sandbox",
	"search":        "search",
	"server":        "",   // cross-cutting
	"standalone":    "standalone",
	"storage":       "datasource",
	"transcription": "extraction",
}

// SSE endpoint path patterns
var ssePathPatterns = []string{"stream", "events", "sse", "subscribe", "listen"}

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

	// Parse enabled checks
	enabledChecks := map[string]bool{}
	if *flagChecks != "" {
		for _, c := range strings.Split(*flagChecks, ",") {
			enabledChecks[strings.TrimSpace(strings.ToUpper(c))] = true
		}
	}
	checkEnabled := func(name string) bool {
		if len(enabledChecks) == 0 {
			return true
		}
		return enabledChecks[name]
	}

	fmt.Fprintln(os.Stderr, "→ Fetching graph data...")

	// ── Parallel fetch ────────────────────────────────────────────────────────
	var (
		domains     []*sdkgraph.GraphObject
		endpoints   []*sdkgraph.GraphObject
		services    []*sdkgraph.GraphObject
		testSuites  []*sdkgraph.GraphObject
		configVars  []*sdkgraph.GraphObject
		jobs        []*sdkgraph.GraphObject
		events      []*sdkgraph.GraphObject
		scenarios   []*sdkgraph.GraphObject
		extDeps     []*sdkgraph.GraphObject
		testedByRels  []*sdkgraph.GraphRelationship
		belongsToRels []*sdkgraph.GraphRelationship
		exposesRels   []*sdkgraph.GraphRelationship
		mu            sync.Mutex
		wg            sync.WaitGroup
		fetchErr      error
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
	fetch(func() error { r, e := listAll(ctx, client.Graph, "TestSuite"); testSuites = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "ConfigVar"); configVars = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "Job"); jobs = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "Event"); events = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "Scenario"); scenarios = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "ExternalDependency"); extDeps = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "tested_by"); testedByRels = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "belongs_to"); belongsToRels = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "exposes"); exposesRels = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	fmt.Fprintf(os.Stderr, "  Domains:%d Endpoints:%d Services:%d TestSuites:%d ConfigVars:%d Jobs:%d Events:%d Scenarios:%d ExtDeps:%d tested_by:%d belongs_to:%d exposes:%d\n",
		len(domains), len(endpoints), len(services), len(testSuites), len(configVars), len(jobs), len(events), len(scenarios), len(extDeps), len(testedByRels), len(belongsToRels), len(exposesRels))

	// ── Build indexes ─────────────────────────────────────────────────────────

	// Domain names (lowercase)
	domainNames := map[string]bool{}
	for _, d := range domains {
		domainNames[strings.ToLower(sp(d, "name"))] = true
	}

	// Endpoints per code-domain slug (for job/event checks that use ep.properties.domain)
	epByDomain := map[string][]string{} // codeDomainSlug → []paths
	for _, ep := range endpoints {
		d := strings.ToLower(sp(ep, "domain"))
		epByDomain[d] = append(epByDomain[d], sp(ep, "path"))
	}

	// Endpoint by entity_id (for graph traversal)
	epByID := map[string]*sdkgraph.GraphObject{}
	for _, ep := range endpoints {
		epByID[ep.EntityID] = ep
	}

	// Domain entity ID set
	domainIDSet := map[string]bool{}
	for _, d := range domains {
		domainIDSet[d.EntityID] = true
	}

	// Services by derived domain name (fallback string match)
	svcDomains := map[string]bool{}
	for _, svc := range services {
		name := sp(svc, "name")
		derived := strings.ToLower(strings.TrimSuffix(name, "Service"))
		svcDomains[derived] = true
	}

	// belongs_to indexes: svcID → set of domainIDs, domainID → set of svcIDs
	svcToDomains := map[string]map[string]bool{} // svcEntityID → domainEntityIDs
	domainToSvcs := map[string]map[string]bool{} // domainEntityID → svcEntityIDs
	for _, r := range belongsToRels {
		if domainIDSet[r.DstID] {
			if svcToDomains[r.SrcID] == nil {
				svcToDomains[r.SrcID] = map[string]bool{}
			}
			svcToDomains[r.SrcID][r.DstID] = true
			if domainToSvcs[r.DstID] == nil {
				domainToSvcs[r.DstID] = map[string]bool{}
			}
			domainToSvcs[r.DstID][r.SrcID] = true
		}
	}

	// Domain by entity_id (for graph traversal)
	domainByID := map[string]*sdkgraph.GraphObject{}
	for _, d := range domains {
		domainByID[d.EntityID] = d
	}

	// epByDomainName: domain name (lowercase) → []paths
	// Built via graph traversal: Domain←belongs_to←Service→exposes→APIEndpoint
	// This is the authoritative source for domain-level endpoint checks.
	epByDomainName := map[string][]string{}
	for _, r := range exposesRels {
		// r.SrcID = Service, r.DstID = APIEndpoint
		domainIDs, ok := svcToDomains[r.SrcID]
		if !ok {
			continue
		}
		ep, ok := epByID[r.DstID]
		if !ok {
			continue
		}
		path := sp(ep, "path")
		for domainID := range domainIDs {
			d, ok := domainByID[domainID]
			if !ok {
				continue
			}
			name := strings.ToLower(sp(d, "name"))
			epByDomainName[name] = append(epByDomainName[name], path)
		}
	}

	// TestSuites: real (non-planned) per domain
	realTSByDomain := map[string]int{}
	tsPlanned := map[string]bool{}
	for _, ts := range testSuites {
		if sp(ts, "status") == "planned" {
			tsPlanned[ts.EntityID] = true
			continue
		}
		d := strings.ToLower(sp(ts, "domain"))
		realTSByDomain[d]++
	}
	// Build: domainID → covered (via Domain←belongs_to←Service→tested_by→TestSuite)
	svcByID := map[string]*sdkgraph.GraphObject{}
	for _, svc := range services {
		svcByID[svc.EntityID] = svc
	}
	// svcID → has real TestSuite
	testedSvcIDs := map[string]bool{}
	for _, r := range testedByRels {
		if !tsPlanned[r.DstID] {
			testedSvcIDs[r.SrcID] = true
		}
	}
	// domainID → covered (any of its services has a real TestSuite)
	domainIDCovered := map[string]bool{}
	for _, r := range belongsToRels {
		if _, isDomain := domainByID[r.DstID]; isDomain {
			if testedSvcIDs[r.SrcID] {
				domainIDCovered[r.DstID] = true
			}
		}
	}
	// Also support legacy: TestSuite.domain property matches domain name
	svcDomainCovered := map[string]bool{} // kept for backward compat, unused now
	_ = svcDomainCovered

	// Scenarios per domain + status
	scenByDomain := map[string]int{}
	plannedScenByDomain := map[string][]string{}
	for _, s := range scenarios {
		d := strings.ToLower(sp(s, "domain"))
		scenByDomain[d]++
		if sp(s, "status") == "planned" {
			plannedScenByDomain[d] = append(plannedScenByDomain[d], sp(s, "name"))
		}
	}

	// ExternalDeps by category
	extDepByCategory := map[string][]string{}
	for _, ed := range extDeps {
		cat := sp(ed, "category")
		extDepByCategory[cat] = append(extDepByCategory[cat], sp(ed, "name"))
	}

	// Average endpoints per domain
	totalEPs := len(endpoints)
	avgEPs := 0
	if len(domainNames) > 0 {
		avgEPs = totalEPs / len(domainNames)
	}

	// ── Run checks ────────────────────────────────────────────────────────────
	var findings []Finding

	add := func(check, domain, object, detail string, tier int) {
		if *flagDomain != "" && !strings.EqualFold(domain, *flagDomain) {
			return
		}
		findings = append(findings, Finding{Check: check, Domain: domain, Object: object, Detail: detail, Tier: tier})
	}

	// CHECK: CONFIGVAR_ORPHAN
	// A ConfigVar is orphaned only if its config_group is not in the known-groups map.
	// The map encodes all valid functional categories (Auth, LLM, Database, etc.) which
	// are NOT the same as conceptual Domain object names — comparing against domainNames
	// would produce false positives due to the dual taxonomy.
	if checkEnabled("CONFIGVAR_ORPHAN") {
		for _, cv := range configVars {
			group := sp(cv, "config_group")
			groupLower := strings.ToLower(group)
			if _, hasMapped := configGroupToDomain[groupLower]; !hasMapped {
				add("CONFIGVAR_ORPHAN", "", sp(cv, "name"),
					fmt.Sprintf("config_group=%q is not a recognised functional category", group), 1)
			}
		}
	}

	// CHECK: DOMAIN_NO_ENDPOINTS
	// "cli workflows" is a conceptual domain — CLI commands map to API endpoints
	// in other domains. No direct endpoints by design.
	var domainNoEndpointsExempt = map[string]bool{
		"cli workflows": true,
	}
	if checkEnabled("DOMAIN_NO_ENDPOINTS") {
		for _, d := range domains {
			name := strings.ToLower(sp(d, "name"))
			if domainNoEndpointsExempt[name] {
				continue
			}
			if len(epByDomainName[name]) == 0 {
				add("DOMAIN_NO_ENDPOINTS", name, name,
					"domain has no APIEndpoints", 2)
			}
		}
	}

	// CHECK: DOMAIN_NO_SERVICE
	// Uses belongs_to rels (graph traversal) as primary signal.
	// Falls back to string-derived name match for domains with no rels yet.
	if checkEnabled("DOMAIN_NO_SERVICE") {
		for _, d := range domains {
			name := strings.ToLower(sp(d, "name"))
			hasViaRel := len(domainToSvcs[d.EntityID]) > 0
			hasViaName := svcDomains[name]
			if !hasViaRel && !hasViaName {
				add("DOMAIN_NO_SERVICE", name, sp(d, "name"),
					"domain has no Service linked via belongs_to", 2)
			}
		}
	}

	// CHECK: DOMAIN_NO_TESTS
	if checkEnabled("DOMAIN_NO_TESTS") {
		for _, d := range domains {
			name := strings.ToLower(sp(d, "name"))
			if len(epByDomainName[name]) > 0 && !domainIDCovered[d.EntityID] {
				epCount := len(epByDomainName[name])
				add("DOMAIN_NO_TESTS", name, name,
					fmt.Sprintf("%d endpoints, no real test coverage", epCount), 1)
			}
		}
	}

	// CHECK: ENDPOINT_HEAVY (god domain)
	// Threshold: 4x average, minimum 45. Raised to suppress known large-but-intentional
	// domains (ai agents=44, kg-objects=40, kg-relationships=38) that are architectural.
	if checkEnabled("ENDPOINT_HEAVY") {
		threshold := avgEPs * 4
		if threshold < 45 {
			threshold = 45
		}
		for domain, paths := range epByDomainName {
			if len(paths) >= threshold {
				add("ENDPOINT_HEAVY", domain, domain,
					fmt.Sprintf("%d endpoints (avg=%d, threshold=%d) — consider decomposition", len(paths), avgEPs, threshold), 3)
			}
		}
	}

	// CHECK: ENDPOINT_SPARSE (stub domain)
	// Only flag domains with exactly 1 endpoint — 2 is a legitimate small domain.
	// "real-time events" with 2 endpoints is intentional (SSE + health).
	if checkEnabled("ENDPOINT_SPARSE") {
		for _, d := range domains {
			name := strings.ToLower(sp(d, "name"))
			paths := epByDomainName[name]
			if len(paths) == 1 {
				add("ENDPOINT_SPARSE", name, name,
					fmt.Sprintf("%d endpoint — stub domain, consider merging", len(paths)), 3)
			}
		}
	}

	// CHECK: JOB_NO_ENDPOINT
	// SchedulerTask worker_type = intentional background cron — no HTTP trigger needed.
	// trigger_type=internal = pure internal worker (e.g. EmailJob) — no HTTP trigger by design.
	// Only flag Worker types that should have an API trigger surface.
	if checkEnabled("JOB_NO_ENDPOINT") {
		for _, j := range jobs {
			wt := sp(j, "worker_type")
			if wt == "SchedulerTask" {
				continue // cron jobs intentionally have no API trigger
			}
			if sp(j, "trigger_type") == "internal" {
				continue // internal workers have no HTTP trigger by design
			}
			domain := strings.ToLower(sp(j, "domain"))
			if len(epByDomain[domain]) == 0 {
				add("JOB_NO_ENDPOINT", domain, sp(j, "name"),
					fmt.Sprintf("worker_type=%s, domain has no API endpoints (no trigger surface)", wt), 1)
			}
		}
	}

	// CHECK: EVENT_NO_ENDPOINT
	if checkEnabled("EVENT_NO_ENDPOINT") {
		for _, ev := range events {
			domain := strings.ToLower(sp(ev, "domain"))
			transport := sp(ev, "transport")
			if transport != "sse" {
				continue // only check SSE events
			}
			// Check if domain has any SSE-pattern endpoint
			paths := epByDomain[domain]
			hasSSEEndpoint := false
			for _, p := range paths {
				pLower := strings.ToLower(p)
				for _, pat := range ssePathPatterns {
					if strings.Contains(pLower, pat) {
						hasSSEEndpoint = true
						break
					}
				}
				if hasSSEEndpoint {
					break
				}
			}
			if !hasSSEEndpoint {
				add("EVENT_NO_ENDPOINT", domain, sp(ev, "name"),
					fmt.Sprintf("SSE event has no stream/events endpoint in domain %q", domain), 1)
			}
		}
	}

	// CHECK: SERVICE_NO_DOMAIN
	// Uses belongs_to rels (graph traversal) as primary signal.
	// Falls back to string-derived name match for services with no rels yet.
	if checkEnabled("SERVICE_NO_DOMAIN") {
		for _, svc := range services {
			name := sp(svc, "name")
			derived := strings.ToLower(strings.TrimSuffix(name, "Service"))
			hasViaRel := len(svcToDomains[svc.EntityID]) > 0
			hasViaName := domainNames[derived]
			if !hasViaRel && !hasViaName {
				add("SERVICE_NO_DOMAIN", "", name,
					fmt.Sprintf("service %q has no Domain linked via belongs_to (derived name: %q)", name, derived), 2)
			}
		}
	}

	// CHECK: SCENARIO_NO_ENDPOINT
	if checkEnabled("SCENARIO_NO_ENDPOINT") {
		for domain, count := range scenByDomain {
			if domain == "" {
				continue
			}
			if len(epByDomain[domain]) == 0 && count > 0 {
				add("SCENARIO_NO_ENDPOINT", domain, domain,
					fmt.Sprintf("%d scenario(s) but zero APIEndpoints — fully unimplemented domain", count), 2)
			}
		}
	}

	// CHECK: SCENARIO_PLANNED
	if checkEnabled("SCENARIO_PLANNED") {
		type domainCount struct{ domain string; count int }
		var planned []domainCount
		for domain, names := range plannedScenByDomain {
			planned = append(planned, domainCount{domain, len(names)})
		}
		sort.Slice(planned, func(i, j int) bool { return planned[i].count > planned[j].count })
		for _, dc := range planned {
			add("SCENARIO_PLANNED", dc.domain, dc.domain,
				fmt.Sprintf("%d planned scenario(s) not yet implemented", dc.count), 2)
		}
	}

	// CHECK: EXTDEP_DUPLICATE
	// Threshold: >5 deps in same category. Raised from >1 to suppress known intentional
	// multi-lib stacks: database (pgx+bun+goose+pq+pgdialect=5), auth (oauth2+oidc+jwt+google=4),
	// observability (otel stack=5), ai (genai+vertex+openai=3), config, utility, infrastructure.
	// Only flag when a category has an unusually large number of deps (likely data quality issue).
	if checkEnabled("EXTDEP_DUPLICATE") {
		for cat, names := range extDepByCategory {
			if len(names) > 5 {
				add("EXTDEP_DUPLICATE", "", cat,
					fmt.Sprintf("%d deps in category %q: %s", len(names), cat, strings.Join(names, ", ")), 3)
			}
		}
	}

	// ── Sort findings: tier asc, check asc, domain asc ────────────────────────
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Tier != findings[j].Tier {
			return findings[i].Tier < findings[j].Tier
		}
		if findings[i].Check != findings[j].Check {
			return findings[i].Check < findings[j].Check
		}
		return findings[i].Domain < findings[j].Domain
	})

	// ── Build summary ─────────────────────────────────────────────────────────
	checkDescs := map[string]string{
		"CONFIGVAR_ORPHAN":     "ConfigVar group has no domain mapping",
		"DOMAIN_NO_ENDPOINTS":  "Domain with zero APIEndpoints",
		"DOMAIN_NO_SERVICE":    "Domain with no matching Service",
		"DOMAIN_NO_TESTS":      "Domain with endpoints but no real tests",
		"ENDPOINT_HEAVY":       "God domain (4x avg endpoint count)",
		"ENDPOINT_SPARSE":      "Stub domain (1 endpoint)",
		"EVENT_NO_ENDPOINT":    "SSE event with no stream endpoint",
		"JOB_NO_ENDPOINT":      "Job domain with no API trigger surface",
		"SCENARIO_NO_ENDPOINT": "Scenario domain with zero endpoints",
		"SCENARIO_PLANNED":     "Planned scenarios (implementation gap)",
		"SERVICE_NO_DOMAIN":    "Service with no matching Domain",
		"EXTDEP_DUPLICATE":     "Multiple deps in same category",
	}
	checkTiers := map[string]int{
		"CONFIGVAR_ORPHAN": 1, "DOMAIN_NO_TESTS": 1, "EVENT_NO_ENDPOINT": 1, "JOB_NO_ENDPOINT": 1,
		"DOMAIN_NO_ENDPOINTS": 2, "DOMAIN_NO_SERVICE": 2, "SCENARIO_NO_ENDPOINT": 2, "SCENARIO_PLANNED": 2, "SERVICE_NO_DOMAIN": 2,
		"ENDPOINT_HEAVY": 3, "ENDPOINT_SPARSE": 3, "EXTDEP_DUPLICATE": 3,
	}

	countByCheck := map[string]int{}
	for _, f := range findings {
		countByCheck[f.Check]++
	}

	var summary []CheckSummary
	for check, desc := range checkDescs {
		summary = append(summary, CheckSummary{
			Check: check,
			Count: countByCheck[check],
			Tier:  checkTiers[check],
			Desc:  desc,
		})
	}
	sort.Slice(summary, func(i, j int) bool {
		if summary[i].Tier != summary[j].Tier {
			return summary[i].Tier < summary[j].Tier
		}
		return summary[i].Check < summary[j].Check
	})

	report := Report{
		Generated: time.Now().Format("2006-01-02"),
		Findings:  findings,
		Summary:   summary,
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

// ── output ────────────────────────────────────────────────────────────────────

func printTable(r Report) {
	fmt.Printf("\n┌─ GRAPH LOGIC AUDIT\n  Generated: %s\n\n", r.Generated)

	// Summary table
	fmt.Printf("┌─ SUMMARY BY CHECK\n")
	st := tablewriter.NewWriter(os.Stdout)
	st.Header("Tier", "Check", "Findings", "Description")
	tierLabel := map[int]string{1: "T1 bug", 2: "T2 design", 3: "T3 concept"}
	for _, s := range r.Summary {
		marker := ""
		if s.Count > 0 {
			marker = fmt.Sprintf("%d ⚠", s.Count)
		} else {
			marker = "0 ✓"
		}
		st.Append([]string{tierLabel[s.Tier], s.Check, marker, s.Desc})
	}
	st.Render()

	if len(r.Findings) == 0 {
		fmt.Println("\n✓ No issues found.")
		return
	}

	fmt.Printf("\n┌─ FINDINGS (%d total)\n", len(r.Findings))
	ft := tablewriter.NewWriter(os.Stdout)
	ft.Header("Tier", "Check", "Domain", "Object", "Detail")
	ft.Configure(func(cfg *tablewriter.Config) {
		cfg.Row.ColMaxWidths.PerColumn = map[int]int{4: 70}
	})
	tierShort := map[int]string{1: "T1", 2: "T2", 3: "T3"}
	for _, f := range r.Findings {
		ft.Append([]string{tierShort[f.Tier], f.Check, f.Domain, f.Object, f.Detail})
	}
	ft.Render()
	fmt.Println("  Tier 1=likely bug  Tier 2=design gap  Tier 3=concept/smell")
}

func printMarkdown(r Report) {
	fmt.Printf("# Graph Logic Audit\n\nGenerated: %s\n\n", r.Generated)
	fmt.Println("## Summary\n")
	fmt.Println("| Tier | Check | Findings | Description |")
	fmt.Println("|------|-------|----------|-------------|")
	for _, s := range r.Summary {
		fmt.Printf("| T%d | %s | %d | %s |\n", s.Tier, s.Check, s.Count, s.Desc)
	}
	if len(r.Findings) == 0 {
		fmt.Println("\n✓ No issues found.")
		return
	}
	fmt.Printf("\n## Findings (%d)\n\n", len(r.Findings))
	fmt.Println("| Tier | Check | Domain | Object | Detail |")
	fmt.Println("|------|-------|--------|--------|--------|")
	for _, f := range r.Findings {
		fmt.Printf("| T%d | %s | %s | %s | %s |\n", f.Tier, f.Check, f.Domain, f.Object, f.Detail)
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
