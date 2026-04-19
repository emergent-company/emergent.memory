// api-audit: graph-only quality checker for APIEndpoint objects.
//
// Checks performed (all graph-only, no file scanning):
//
//   - NO_PATH     APIEndpoint missing path property
//   - NO_METHOD   APIEndpoint missing method property
//   - NO_HANDLER  APIEndpoint missing handler property
//   - NO_FILE     APIEndpoint missing file property (run graph-sync-routes to fix)
//   - NO_DOMAIN   APIEndpoint missing domain property
//   - ORPHAN      APIEndpoint has no `handles` relationship to a Service
//   - DUPLICATE   Two or more APIEndpoints share the same method+path
//
// Usage:
//
//	MEMORY_API_KEY=<token> MEMORY_PROJECT_ID=<id> MEMORY_SERVER_URL=https://... \
//	  go run . [--format table|markdown|json] [--domain X] [--checks no_path,no_method,...]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// Finding is a single audit result.
type Finding struct {
	Check   string // NO_PATH / NO_METHOD / NO_HANDLER / NO_FILE / NO_DOMAIN / ORPHAN / DUPLICATE
	Domain  string
	Method  string
	Path    string
	Handler string
	Key     string
	Detail  string
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
	checksFlag := flag.String("checks", "no_path,no_method,no_handler,no_file,no_domain,orphan,duplicate", "Comma-separated checks to run")
	flag.Parse()

	if *apiKey == "" {
		return fmt.Errorf("--api-key or MEMORY_API_KEY is required")
	}
	if *projectID == "" {
		return fmt.Errorf("--project-id or MEMORY_PROJECT_ID is required")
	}

	enabled := make(map[string]bool)
	for _, c := range strings.Split(*checksFlag, ",") {
		enabled[strings.TrimSpace(strings.ToLower(c))] = true
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

	// Fetch APIEndpoints and handles relationships in parallel
	type fetchResult struct {
		eps  []*sdkgraph.GraphObject
		rels []*sdkgraph.GraphRelationship
		err  error
	}
	epCh := make(chan fetchResult, 1)
	relCh := make(chan fetchResult, 1)

	go func() {
		eps, err := listAllObjects(ctx, client.Graph, "APIEndpoint")
		epCh <- fetchResult{eps: eps, err: err}
	}()
	go func() {
		rels, err := listAllRelationships(ctx, client.Graph, "handles")
		relCh <- fetchResult{rels: rels, err: err}
	}()

	epRes := <-epCh
	relRes := <-relCh
	if epRes.err != nil {
		return fmt.Errorf("fetching APIEndpoints: %w", epRes.err)
	}
	if relRes.err != nil {
		return fmt.Errorf("fetching handles rels: %w", relRes.err)
	}

	graphEPs := epRes.eps
	handlesRels := relRes.rels

	fmt.Fprintf(os.Stderr, "  APIEndpoints: %d\n", len(graphEPs))
	fmt.Fprintf(os.Stderr, "  handles rels: %d\n", len(handlesRels))

	// Build set of endpoint EntityIDs that have a handles rel (as target)
	hasHandles := make(map[string]bool)
	for _, r := range handlesRels {
		hasHandles[r.DstID] = true
	}

	// ── Audit ─────────────────────────────────────────────────────────────────
	var findings []Finding

	// Index for DUPLICATE check: method+path → endpoints
	type epKey struct{ method, path string }
	byMethodPath := make(map[epKey][]*sdkgraph.GraphObject)

	for _, ep := range graphEPs {
		domain := strProp(ep, "domain")
		if *domainFilter != "" && domain != *domainFilter {
			continue
		}

		method := strings.ToUpper(strProp(ep, "method"))
		path := strProp(ep, "path")
		handler := strProp(ep, "handler")
		file := strProp(ep, "file")
		key := derefKey(ep.Key)

		f := func(check, detail string) {
			findings = append(findings, Finding{
				Check:   check,
				Domain:  domain,
				Method:  method,
				Path:    path,
				Handler: handler,
				Key:     key,
				Detail:  detail,
			})
		}

		if enabled["no_path"] && path == "" {
			f("NO_PATH", "missing path property")
		}
		if enabled["no_method"] && method == "" {
			f("NO_METHOD", "missing method property")
		}
		if enabled["no_handler"] && handler == "" {
			f("NO_HANDLER", "missing handler property")
		}
		if enabled["no_file"] && file == "" {
			f("NO_FILE", "missing file property — run graph-sync-routes to populate")
		}
		if enabled["no_domain"] && domain == "" {
			f("NO_DOMAIN", "missing domain property")
		}
		if enabled["orphan"] && !hasHandles[ep.EntityID] {
			f("ORPHAN", "no `handles` relationship to a Service")
		}
		if enabled["duplicate"] && method != "" && path != "" {
			k := epKey{method, path}
			byMethodPath[k] = append(byMethodPath[k], ep)
		}
	}

	// DUPLICATE check
	if enabled["duplicate"] {
		for k, eps := range byMethodPath {
			if len(eps) <= 1 {
				continue
			}
			keys := make([]string, 0, len(eps))
			for _, ep := range eps {
				keys = append(keys, derefKey(ep.Key))
			}
			sort.Strings(keys)
			detail := fmt.Sprintf("%d endpoints share %s %s: %s", len(eps), k.method, k.path, strings.Join(keys, ", "))
			for _, ep := range eps {
				domain := strProp(ep, "domain")
				if *domainFilter != "" && domain != *domainFilter {
					continue
				}
				findings = append(findings, Finding{
					Check:   "DUPLICATE",
					Domain:  domain,
					Method:  k.method,
					Path:    k.path,
					Handler: strProp(ep, "handler"),
					Key:     derefKey(ep.Key),
					Detail:  detail,
				})
			}
		}
	}

	// Sort: check → domain → path
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Check != findings[j].Check {
			return findings[i].Check < findings[j].Check
		}
		if findings[i].Domain != findings[j].Domain {
			return findings[i].Domain < findings[j].Domain
		}
		return findings[i].Path < findings[j].Path
	})

	// Summary counts
	counts := make(map[string]int)
	for _, f := range findings {
		counts[f.Check]++
	}

	switch *format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"summary": map[string]any{
				"total_endpoints": len(graphEPs),
				"findings":        len(findings),
				"by_check":        counts,
			},
			"findings": findings,
		})
	case "markdown":
		return printMarkdown(findings, counts, len(graphEPs))
	default:
		return printTable(findings, counts, len(graphEPs))
	}
}

func printTable(findings []Finding, counts map[string]int, total int) error {
	now := time.Now().Format("2006-01-02")
	fmt.Printf("┌─ API AUDIT REPORT\n")
	fmt.Printf("  Generated: %s\n\n", now)

	fmt.Printf("┌─ SUMMARY\n")
	fmt.Printf("  Total endpoints : %d\n", total)
	fmt.Printf("  Total findings  : %d\n", len(findings))
	for _, check := range []string{"DUPLICATE", "NO_DOMAIN", "NO_FILE", "NO_HANDLER", "NO_METHOD", "NO_PATH", "ORPHAN"} {
		if n := counts[check]; n > 0 {
			fmt.Printf("  %-12s    : %d\n", check, n)
		}
	}
	fmt.Println()

	if len(findings) == 0 {
		fmt.Println("✓ No issues found.")
		return nil
	}

	fmt.Printf("┌─ FINDINGS\n")
	table := tablewriter.NewWriter(os.Stdout)
	table.Header("CHECK", "DOMAIN", "METHOD", "PATH", "KEY", "DETAIL")
	table.Configure(func(cfg *tablewriter.Config) {
		cfg.Behavior.TrimSpace = tw.On
		cfg.Row.Alignment.PerColumn = []tw.Align{
			tw.AlignCenter, // CHECK
			tw.AlignLeft,   // DOMAIN
			tw.AlignCenter, // METHOD
			tw.AlignLeft,   // PATH
			tw.AlignLeft,   // KEY
			tw.AlignLeft,   // DETAIL
		}
		cfg.Row.ColMaxWidths.PerColumn = map[int]int{
			3: 55, // PATH
			4: 40, // KEY
			5: 55, // DETAIL
		}
	})

	for _, f := range findings {
		table.Append([]string{f.Check, f.Domain, f.Method, f.Path, f.Key, f.Detail})
	}
	table.Render()
	return nil
}

func printMarkdown(findings []Finding, counts map[string]int, total int) error {
	now := time.Now().Format("2006-01-02")
	fmt.Printf("# API Audit Report\n\nGenerated: %s\n\n", now)
	fmt.Printf("## Summary\n\n- Total endpoints: %d\n- Total findings: %d\n\n", total, len(findings))
	for _, check := range []string{"DUPLICATE", "NO_DOMAIN", "NO_FILE", "NO_HANDLER", "NO_METHOD", "NO_PATH", "ORPHAN"} {
		if n := counts[check]; n > 0 {
			fmt.Printf("- %s: %d\n", check, n)
		}
	}
	if len(findings) == 0 {
		fmt.Println("\n✓ No issues found.")
		return nil
	}
	fmt.Printf("\n## Findings\n\n| Check | Domain | Method | Path | Key | Detail |\n| :--- | :--- | :--- | :--- | :--- | :--- |\n")
	for _, f := range findings {
		fmt.Printf("| **%s** | %s | %s | `%s` | %s | %s |\n", f.Check, f.Domain, f.Method, f.Path, f.Key, f.Detail)
	}
	return nil
}

// ── SDK helpers ───────────────────────────────────────────────────────────────

func listAllObjects(ctx context.Context, g *sdkgraph.Client, objType string) ([]*sdkgraph.GraphObject, error) {
	const pageSize = 500
	var all []*sdkgraph.GraphObject
	var cursor string
	for {
		resp, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: pageSize, Cursor: cursor})
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
		resp, err := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: relType, Limit: pageSize, Cursor: cursor})
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

func derefKey(k *string) string {
	if k == nil {
		return ""
	}
	return *k
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
