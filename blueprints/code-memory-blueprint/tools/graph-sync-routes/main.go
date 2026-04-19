// graph-sync-routes: populates APIEndpoint graph objects with route metadata
// extracted from framework route registration files.
//
// This is a graph POPULATION tool — it reads route files once and writes
// path, method, handler, and file properties into the graph so that
// audit and analysis tools can work purely from graph data.
//
// Matching strategy (in order):
//  1. Exact key match: ep-<domain>-<handler-lowercase>
//  2. Handler + domain match across all APIEndpoint objects
//  3. Handler-only match (cross-domain, flagged as ambiguous)
//
// Unmatched code routes are reported as candidates for new APIEndpoint objects.
// Unmatched graph endpoints (no code route found) are reported as stale.
//
// Usage:
//
//	MEMORY_API_KEY=<token> MEMORY_PROJECT_ID=<id> MEMORY_SERVER_URL=https://... \
//	  go run . --repo /path/to/repo [--domain X] [--dry-run] [--format table|json]
//
// The tool is intentionally project-aware for the population step only.
// Once the graph is populated, all downstream tools are graph-only.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// CodeRoute is a route extracted from a route registration file.
type CodeRoute struct {
	Domain  string
	Method  string
	Path    string
	Handler string
	File    string // relative path to the route file
	Line    int
}

// MatchResult describes how a code route was matched to a graph endpoint.
type MatchResult struct {
	Route      CodeRoute
	GraphKey   string
	GraphID    string // EntityID
	MatchType  string // "key", "handler+domain", "handler-only", "unmatched"
	Ambiguous  bool
	OldPath    string
	OldMethod  string
	OldHandler string
	OldFile    string
}

// UpdateRecord is a pending graph update.
type UpdateRecord struct {
	GraphID string
	Key     string
	Props   map[string]any
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
	repoRoot := flag.String("repo", envOr("MEMORY_REPO_ROOT", "."), "Path to repository root")
	domainFilter := flag.String("domain", "", "Sync only a specific domain")
	dryRun := flag.Bool("dry-run", false, "Print what would be updated without writing to graph")
	format := flag.String("format", "table", "Output format: table, json")
	routeGlob := flag.String("route-glob",
		"apps/server/domain/*/*routes*.go,apps/server/domain/*/module.go",
		"Comma-separated glob patterns for route files, relative to --repo")
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// ── 1. Fetch all APIEndpoint objects from graph ───────────────────────────
	fmt.Fprintln(os.Stderr, "→ Fetching APIEndpoint objects from graph...")
	graphEPs, err := listAllObjects(ctx, client.Graph, "APIEndpoint")
	if err != nil {
		return fmt.Errorf("listing APIEndpoints: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Found %d APIEndpoint objects\n", len(graphEPs))

	// Build lookup indexes
	byKey := make(map[string]*sdkgraph.GraphObject)          // key → object
	byDomainHandler := make(map[string][]*sdkgraph.GraphObject) // "domain:handler" → objects
	byHandler := make(map[string][]*sdkgraph.GraphObject)    // handler → objects

	for _, ep := range graphEPs {
		if derefKey(ep.Key) != "" {
			byKey[derefKey(ep.Key)] = ep
		}
		domain := strProp(ep, "domain")
		handler := strings.ToLower(strProp(ep, "handler"))
		if domain != "" && handler != "" {
			k := domain + ":" + handler
			byDomainHandler[k] = append(byDomainHandler[k], ep)
		}
		if handler != "" {
			byHandler[handler] = append(byHandler[handler], ep)
		}
	}

	// ── 2. Scan route files ───────────────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Scanning route files...")
	var codeRoutes []CodeRoute
	for _, pattern := range strings.Split(*routeGlob, ",") {
		pattern = strings.TrimSpace(pattern)
		matches, err := filepath.Glob(filepath.Join(*repoRoot, pattern))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warn: glob %q: %v\n", pattern, err)
			continue
		}
		for _, f := range matches {
			domain := extractDomain(f)
			if *domainFilter != "" && domain != *domainFilter {
				continue
			}
			routes, err := parseRouteFile(f, domain, *repoRoot)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  warn: parsing %s: %v\n", f, err)
				continue
			}
			codeRoutes = append(codeRoutes, routes...)
		}
	}
	fmt.Fprintf(os.Stderr, "  Found %d code routes\n", len(codeRoutes))

	// ── 3. Match code routes to graph endpoints ───────────────────────────────
	var results []MatchResult
	matchedGraphIDs := make(map[string]bool)

	for _, r := range codeRoutes {
		res := MatchResult{Route: r}

		// Try key match: ep-<domain>-<handler-lower>
		candidateKey := "ep-" + r.Domain + "-" + strings.ToLower(r.Handler)
		if ep, ok := byKey[candidateKey]; ok {
			res.MatchType = "key"
			res.GraphKey = derefKey(ep.Key)
			res.GraphID = ep.EntityID
			res.OldPath = strProp(ep, "path")
			res.OldMethod = strProp(ep, "method")
			res.OldHandler = strProp(ep, "handler")
			res.OldFile = strProp(ep, "file")
			matchedGraphIDs[ep.EntityID] = true
			results = append(results, res)
			continue
		}

		// Try domain:handler match
		dhKey := r.Domain + ":" + strings.ToLower(r.Handler)
		if eps, ok := byDomainHandler[dhKey]; ok {
			if len(eps) == 1 {
				ep := eps[0]
				res.MatchType = "handler+domain"
				res.GraphKey = derefKey(ep.Key)
				res.GraphID = ep.EntityID
				res.OldPath = strProp(ep, "path")
				res.OldMethod = strProp(ep, "method")
				res.OldHandler = strProp(ep, "handler")
				res.OldFile = strProp(ep, "file")
				matchedGraphIDs[ep.EntityID] = true
			} else {
				res.MatchType = "handler+domain"
				res.Ambiguous = true
				res.GraphKey = derefKey(eps[0].Key) + " (+" + fmt.Sprint(len(eps)-1) + " more)"
			}
			results = append(results, res)
			continue
		}

		// Try handler-only match
		hKey := strings.ToLower(r.Handler)
		if eps, ok := byHandler[hKey]; ok {
			ep := eps[0]
			res.MatchType = "handler-only"
			res.Ambiguous = len(eps) > 1
			res.GraphKey = derefKey(ep.Key)
			res.GraphID = ep.EntityID
			res.OldPath = strProp(ep, "path")
			res.OldMethod = strProp(ep, "method")
			res.OldHandler = strProp(ep, "handler")
			res.OldFile = strProp(ep, "file")
			if !res.Ambiguous {
				matchedGraphIDs[ep.EntityID] = true
			}
			results = append(results, res)
			continue
		}

		// No match
		res.MatchType = "unmatched"
		results = append(results, res)
	}

	// ── 4. Find stale graph endpoints (no code route matched) ─────────────────
	var stale []*sdkgraph.GraphObject
	for _, ep := range graphEPs {
		if !matchedGraphIDs[ep.EntityID] {
			if *domainFilter == "" || strProp(ep, "domain") == *domainFilter {
				stale = append(stale, ep)
			}
		}
	}

	// ── 5. Build update list ──────────────────────────────────────────────────
	var updates []UpdateRecord
	var skipped []MatchResult

	for _, res := range results {
		if res.Ambiguous || res.MatchType == "unmatched" || res.GraphID == "" {
			skipped = append(skipped, res)
			continue
		}
		needsUpdate := res.OldPath != res.Route.Path ||
			!strings.EqualFold(res.OldMethod, res.Route.Method) ||
			res.OldFile != res.Route.File
		if !needsUpdate {
			continue
		}
		updates = append(updates, UpdateRecord{
			GraphID: res.GraphID,
			Key:     res.GraphKey,
			Props: map[string]any{
				"path":    res.Route.Path,
				"method":  res.Route.Method,
				"handler": res.Route.Handler,
				"file":    res.Route.File,
			},
		})
	}

	sort.Slice(updates, func(i, j int) bool { return updates[i].Key < updates[j].Key })

	// ── 6. Apply updates via bulk API ─────────────────────────────────────────
	applied := 0
	failed := 0
	if !*dryRun && len(updates) > 0 {
		fmt.Fprintf(os.Stderr, "→ Applying %d updates via bulk API...\n", len(updates))
		const batchSize = 100
		for start := 0; start < len(updates); start += batchSize {
			end := start + batchSize
			if end > len(updates) {
				end = len(updates)
			}
			batch := updates[start:end]

			items := make([]sdkgraph.BulkUpdateObjectItem, 0, len(batch))
			for _, u := range batch {
				items = append(items, sdkgraph.BulkUpdateObjectItem{
					ID:         u.GraphID,
					Properties: u.Props,
				})
			}

			resp, err := client.Graph.BulkUpdateObjects(ctx, &sdkgraph.BulkUpdateObjectsRequest{Items: items})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error in bulk update batch %d-%d: %v\n", start, end, err)
				failed += len(batch)
				continue
			}
			applied += resp.Success
			failed += resp.Failed
			for _, r := range resp.Results {
				if !r.Success && r.Error != nil {
					fmt.Fprintf(os.Stderr, "  error updating item %d: %s\n", r.Index, *r.Error)
				}
			}
		}
		fmt.Fprintf(os.Stderr, "  Applied: %d, Failed: %d\n", applied, failed)
	}

	// ── 7. Output ─────────────────────────────────────────────────────────────
	switch *format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"summary": map[string]any{
				"graph_endpoints": len(graphEPs),
				"code_routes":     len(codeRoutes),
				"updates_needed":  len(updates),
				"applied":         applied,
				"failed":          failed,
				"skipped":         len(skipped),
				"stale":           len(stale),
				"dry_run":         *dryRun,
			},
			"updates": updates,
			"skipped": skipped,
			"stale":   staleKeys(stale),
		})
	default:
		return printTable(updates, skipped, stale, applied, failed, *dryRun, len(graphEPs), len(codeRoutes))
	}
}

// ── Output ────────────────────────────────────────────────────────────────────

func printTable(updates []UpdateRecord, skipped []MatchResult, stale []*sdkgraph.GraphObject, applied, failed int, dryRun bool, graphTotal, codeTotal int) error {
	now := time.Now().Format("2006-01-02")
	dryTag := ""
	if dryRun {
		dryTag = " [DRY RUN]"
	}
	fmt.Printf("┌─ GRAPH SYNC ROUTES%s\n", dryTag)
	fmt.Printf("  Generated: %s\n\n", now)

	fmt.Printf("┌─ SUMMARY\n")
	fmt.Printf("  Graph endpoints : %d\n", graphTotal)
	fmt.Printf("  Code routes     : %d\n", codeTotal)
	fmt.Printf("  Updates needed  : %d\n", len(updates))
	if !dryRun {
		fmt.Printf("  Applied         : %d\n", applied)
		fmt.Printf("  Failed          : %d\n", failed)
	}
	fmt.Printf("  Skipped         : %d  (ambiguous or unmatched)\n", len(skipped))
	fmt.Printf("  Stale in graph  : %d  (graph endpoint, no code route found)\n", len(stale))
	fmt.Println()

	if len(updates) > 0 {
		fmt.Printf("┌─ UPDATES\n")
		t := tablewriter.NewWriter(os.Stdout)
		t.Header("KEY", "PATH", "METHOD", "FILE")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.Alignment.PerColumn = []tw.Align{
				tw.AlignLeft,
				tw.AlignLeft,
				tw.AlignCenter,
				tw.AlignLeft,
			}
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{
				1: 60,
				3: 55,
			}
		})
		for _, u := range updates {
			t.Append([]string{
				u.Key,
				fmt.Sprint(u.Props["path"]),
				fmt.Sprint(u.Props["method"]),
				fmt.Sprint(u.Props["file"]),
			})
		}
		t.Render()
		fmt.Println()
	}

	if len(skipped) > 0 {
		fmt.Printf("┌─ SKIPPED (ambiguous or unmatched — manual review needed)\n")
		t := tablewriter.NewWriter(os.Stdout)
		t.Header("MATCH", "DOMAIN", "METHOD", "PATH", "HANDLER", "NOTE")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{3: 55, 5: 40}
		})
		for _, s := range skipped {
			note := s.MatchType
			if s.Ambiguous {
				note += " (ambiguous: " + s.GraphKey + ")"
			}
			t.Append([]string{s.MatchType, s.Route.Domain, s.Route.Method, s.Route.Path, s.Route.Handler, note})
		}
		t.Render()
		fmt.Println()
	}

	if len(stale) > 0 {
		fmt.Printf("┌─ STALE GRAPH ENDPOINTS (no matching code route)\n")
		t := tablewriter.NewWriter(os.Stdout)
		t.Header("KEY", "DOMAIN", "METHOD", "PATH", "HANDLER")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{3: 60}
		})
		for _, ep := range stale {
			t.Append([]string{
				derefKey(ep.Key),
				strProp(ep, "domain"),
				strProp(ep, "method"),
				strProp(ep, "path"),
				strProp(ep, "handler"),
			})
		}
		t.Render()
	}

	return nil
}

// ── Route file parsing ────────────────────────────────────────────────────────

func extractDomain(filePath string) string {
	const marker = "/domain/"
	idx := strings.Index(filePath, marker)
	if idx < 0 {
		return ""
	}
	rest := filePath[idx+len(marker):]
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func parseRouteFile(path, domain, repoRoot string) ([]CodeRoute, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	relPath, _ := filepath.Rel(repoRoot, path)

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// rootVars: Echo/router root variables — no prefix
	rootVars := map[string]bool{"e": true, "r": true, "router": true}

	// First pass: collect group variable → full path prefix
	groupPrefixes := make(map[string]string)
	basePrefix := ""
	groupRe := regexp.MustCompile(`(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]*)"`)

	for _, line := range lines {
		if m := groupRe.FindStringSubmatch(line); m != nil {
			varName := m[1]
			parentVar := m[2]
			groupPath := m[3]
			if rootVars[parentVar] {
				groupPrefixes[varName] = groupPath
				if basePrefix == "" {
					basePrefix = groupPath
				}
			} else {
				parentPrefix := groupPrefixes[parentVar]
				groupPrefixes[varName] = parentPrefix + groupPath
			}
		}
	}

	// Second pass: extract route registrations
	// Matches: groupVar.METHOD("/path", handlerVar.FuncName) — any handler variable name
	routeRe := regexp.MustCompile(`(\w+)\.(GET|POST|PUT|DELETE|PATCH)\s*\(\s*"([^"]*)"[^,]*,\s*\w+\.(\w+)`)

	var routes []CodeRoute
	for i, line := range lines {
		if m := routeRe.FindStringSubmatch(line); m != nil {
			varName := m[1]
			method := m[2]
			subPath := m[3]
			handler := m[4]

			var prefix string
			if rootVars[varName] {
				prefix = ""
			} else {
				prefix = groupPrefixes[varName]
				if prefix == "" {
					prefix = basePrefix
				}
			}

			fullPath := prefix + subPath
			fullPath = strings.ReplaceAll(fullPath, "//", "/")
			fullPath = strings.TrimRight(fullPath, "/")
			if fullPath == "" {
				fullPath = "/"
			}

			routes = append(routes, CodeRoute{
				Domain:  domain,
				Method:  strings.ToUpper(method),
				Path:    fullPath,
				Handler: handler,
				File:    relPath,
				Line:    i + 1,
			})
		}
	}

	return routes, nil
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

func staleKeys(eps []*sdkgraph.GraphObject) []map[string]string {
	out := make([]map[string]string, 0, len(eps))
	for _, ep := range eps {
		out = append(out, map[string]string{
			"key":     derefKey(ep.Key),
			"domain":  strProp(ep, "domain"),
			"method":  strProp(ep, "method"),
			"path":    strProp(ep, "path"),
			"handler": strProp(ep, "handler"),
		})
	}
	return out
}
