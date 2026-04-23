// Deprecated: use `codebase sync middleware` instead. Run `codebase --help` for details.
// graph-sync-middleware: wires applies_to relationships between Middleware and APIEndpoint
// objects in the Memory graph.
//
// Strategy:
//  1. Parse all route files to extract group middleware inheritance.
//  2. For each route, resolve the full middleware stack (inherited + direct).
//  3. Fetch all Middleware and APIEndpoint objects from graph.
//  4. Match routes to APIEndpoint objects (same key strategy as graph-sync-routes).
//  5. Upsert applies_to relationships: Middleware → APIEndpoint.
//  6. Also update the `scopes` property on each APIEndpoint with resolved scopes.
//
// Middleware name mapping (code call → graph Middleware name):
//
//	RequireAuth()                    → RequireAuth
//	RequireAPITokenScopes("x:y")     → RequireAPITokenScopes
//	RequireProjectScope()            → RequireProjectScope
//	RequireProjectID()               → RequireProjectID
//	RequireScopes(...)               → RequireScopes
//	ToolAuditMiddleware(...)         → ToolAuditMiddleware
//	ToolRestrictionMiddleware(...)   → ToolRestrictionMiddleware
//
// Global middleware (CORS, RequestLogger, etc.) are NOT wired per-endpoint —
// they apply to all routes and are already documented on the Middleware objects.
//
// Usage:
//
//	MEMORY_API_KEY=<token> MEMORY_PROJECT_ID=<id> MEMORY_SERVER_URL=https://... \
//	  go run . --repo /path/to/repo [--dry-run] [--format table|json]
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

// routeMiddleware describes the middleware stack for a single route.
type routeMiddleware struct {
	Domain      string
	Handler     string
	Middleware  []string // ordered list of middleware names applied
	Scopes      []string // extracted scope strings from RequireAPITokenScopes / RequireScopes
	IsPublic    bool     // true if no RequireAuth in stack
}

// relRecord is a pending applies_to relationship to create.
type relRecord struct {
	MiddlewareName string
	MiddlewareID   string
	EndpointKey    string
	EndpointID     string
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
	dryRun := flag.Bool("dry-run", false, "Print what would be written without writing to graph")
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

	// ── 1. Fetch Middleware objects from graph ────────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Fetching Middleware objects from graph...")
	middlewareObjs, err := listAllObjects(ctx, client.Graph, "Middleware")
	if err != nil {
		return fmt.Errorf("listing Middleware: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Found %d Middleware objects\n", len(middlewareObjs))

	// Build name → ID map for route-level middleware
	mwByName := make(map[string]string) // name → entityID
	for _, m := range middlewareObjs {
		name := strProp(m, "name")
		if name != "" {
			mwByName[name] = m.EntityID
		}
	}

	// ── 2. Fetch APIEndpoint objects from graph ───────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Fetching APIEndpoint objects from graph...")
	epObjs, err := listAllObjects(ctx, client.Graph, "APIEndpoint")
	if err != nil {
		return fmt.Errorf("listing APIEndpoints: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Found %d APIEndpoint objects\n", len(epObjs))

	// Build lookup indexes (same as graph-sync-routes)
	byKey := make(map[string]*sdkgraph.GraphObject)
	byDomainHandler := make(map[string][]*sdkgraph.GraphObject)

	for _, ep := range epObjs {
		if derefKey(ep.Key) != "" {
			byKey[derefKey(ep.Key)] = ep
		}
		domain := strProp(ep, "domain")
		handler := strings.ToLower(strProp(ep, "handler"))
		if domain != "" && handler != "" {
			k := domain + ":" + handler
			byDomainHandler[k] = append(byDomainHandler[k], ep)
		}
	}

	// ── 3. Fetch existing applies_to relationships ────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Fetching existing applies_to relationships...")
	existingRels, err := listAllRelationships(ctx, client.Graph, "applies_to")
	if err != nil {
		return fmt.Errorf("listing applies_to relationships: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Found %d existing applies_to relationships\n", len(existingRels))

	// Build set of existing (src, dst) pairs to avoid duplicates
	existingPairs := make(map[string]bool)
	for _, r := range existingRels {
		existingPairs[r.SrcID+":"+r.DstID] = true
	}

	// ── 4. Parse route files ──────────────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Parsing route files...")
	var allRoutes []routeMiddleware
	for _, pattern := range strings.Split(*routeGlob, ",") {
		pattern = strings.TrimSpace(pattern)
		matches, err := filepath.Glob(filepath.Join(*repoRoot, pattern))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warn: glob %q: %v\n", pattern, err)
			continue
		}
		for _, f := range matches {
			domain := extractDomain(f)
			routes, err := parseRouteFileMiddleware(f, domain)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  warn: parsing %s: %v\n", f, err)
				continue
			}
			allRoutes = append(allRoutes, routes...)
		}
	}
	fmt.Fprintf(os.Stderr, "  Parsed %d route-middleware entries\n", len(allRoutes))

	// ── 5. Match routes to graph endpoints and build relationship list ─────────
	var toCreate []relRecord
	var scopeUpdates []scopeUpdate
	unmatched := 0
	matched := 0

	// Deduplicate: track (middlewareID, endpointID) pairs we've already queued
	queued := make(map[string]bool)
	// Deduplicate scope updates: track endpointID → already queued
	scopeQueued := make(map[string]bool)

	for _, route := range allRoutes {
		// Find graph endpoint
		ep := resolveEndpoint(route.Domain, route.Handler, byKey, byDomainHandler)
		if ep == nil {
			unmatched++
			continue
		}
		matched++
		epID := ep.EntityID
		epKey := derefKey(ep.Key)

		// Queue applies_to relationships for each route-level middleware
		for _, mwName := range route.Middleware {
			mwID, ok := mwByName[mwName]
			if !ok {
				fmt.Fprintf(os.Stderr, "  warn: middleware %q not found in graph (route %s.%s)\n", mwName, route.Domain, route.Handler)
				continue
			}
			pairKey := mwID + ":" + epID
			if existingPairs[pairKey] || queued[pairKey] {
				continue
			}
			queued[pairKey] = true
			toCreate = append(toCreate, relRecord{
				MiddlewareName: mwName,
				MiddlewareID:   mwID,
				EndpointKey:    epKey,
				EndpointID:     epID,
			})
		}

		// Queue scope update if scopes differ (deduplicated per endpoint)
		if len(route.Scopes) > 0 && !scopeQueued[epID] {
			currentScopes := strProp(ep, "scopes")
			newScopes := strings.Join(route.Scopes, ",")
			if currentScopes != newScopes {
				scopeQueued[epID] = true
				scopeUpdates = append(scopeUpdates, scopeUpdate{
					EndpointID:  epID,
					EndpointKey: epKey,
					OldScopes:   currentScopes,
					NewScopes:   newScopes,
				})
			}
		}
	}

	sort.Slice(toCreate, func(i, j int) bool {
		if toCreate[i].MiddlewareName != toCreate[j].MiddlewareName {
			return toCreate[i].MiddlewareName < toCreate[j].MiddlewareName
		}
		return toCreate[i].EndpointKey < toCreate[j].EndpointKey
	})
	sort.Slice(scopeUpdates, func(i, j int) bool {
		return scopeUpdates[i].EndpointKey < scopeUpdates[j].EndpointKey
	})

	// ── 6. Apply relationships ────────────────────────────────────────────────
	createdRels := 0
	failedRels := 0
	if !*dryRun && len(toCreate) > 0 {
		fmt.Fprintf(os.Stderr, "→ Creating %d applies_to relationships...\n", len(toCreate))
		const batchSize = 100
		for start := 0; start < len(toCreate); start += batchSize {
			end := start + batchSize
			if end > len(toCreate) {
				end = len(toCreate)
			}
			batch := toCreate[start:end]

			items := make([]sdkgraph.CreateRelationshipRequest, 0, len(batch))
			for _, r := range batch {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type:  "applies_to",
					SrcID: r.MiddlewareID,
					DstID: r.EndpointID,
				})
			}

			resp, err := client.Graph.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: items})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error in bulk create batch %d-%d: %v\n", start, end, err)
				failedRels += len(batch)
				continue
			}
			createdRels += resp.Success
			failedRels += resp.Failed
			for _, r := range resp.Results {
				if !r.Success && r.Error != nil {
					fmt.Fprintf(os.Stderr, "  error creating rel %d: %s\n", r.Index, *r.Error)
				}
			}
		}
		fmt.Fprintf(os.Stderr, "  Created: %d, Failed: %d\n", createdRels, failedRels)
	}

	// ── 7. Apply scope updates ────────────────────────────────────────────────
	appliedScopes := 0
	failedScopes := 0
	if !*dryRun && len(scopeUpdates) > 0 {
		fmt.Fprintf(os.Stderr, "→ Updating scopes on %d endpoints...\n", len(scopeUpdates))
		const batchSize = 100
		for start := 0; start < len(scopeUpdates); start += batchSize {
			end := start + batchSize
			if end > len(scopeUpdates) {
				end = len(scopeUpdates)
			}
			batch := scopeUpdates[start:end]

			items := make([]sdkgraph.BulkUpdateObjectItem, 0, len(batch))
			for _, u := range batch {
				items = append(items, sdkgraph.BulkUpdateObjectItem{
					ID:         u.EndpointID,
					Properties: map[string]any{"scopes": u.NewScopes},
				})
			}

			resp, err := client.Graph.BulkUpdateObjects(ctx, &sdkgraph.BulkUpdateObjectsRequest{Items: items})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error in scope update batch %d-%d: %v\n", start, end, err)
				failedScopes += len(batch)
				continue
			}
			appliedScopes += resp.Success
			failedScopes += resp.Failed
		}
		fmt.Fprintf(os.Stderr, "  Scope updates applied: %d, Failed: %d\n", appliedScopes, failedScopes)
	}

	// ── 8. Output ─────────────────────────────────────────────────────────────
	switch *format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"summary": map[string]any{
				"middleware_objects":    len(middlewareObjs),
				"endpoint_objects":      len(epObjs),
				"routes_parsed":         len(allRoutes),
				"routes_matched":        matched,
				"routes_unmatched":      unmatched,
				"existing_rels":         len(existingRels),
				"relationships_to_add":  len(toCreate),
				"relationships_created": createdRels,
				"relationships_failed":  failedRels,
				"scope_updates":         len(scopeUpdates),
				"scopes_applied":        appliedScopes,
				"scopes_failed":         failedScopes,
				"dry_run":               *dryRun,
			},
			"relationships": toCreate,
			"scope_updates": scopeUpdates,
		})
	default:
		return printTable(toCreate, scopeUpdates, createdRels, failedRels, appliedScopes, failedScopes,
			*dryRun, len(middlewareObjs), len(epObjs), len(allRoutes), matched, unmatched, len(existingRels))
	}
}

type scopeUpdate struct {
	EndpointID  string
	EndpointKey string
	OldScopes   string
	NewScopes   string
}

// ── Output ────────────────────────────────────────────────────────────────────

func printTable(rels []relRecord, scopes []scopeUpdate, createdRels, failedRels, appliedScopes, failedScopes int,
	dryRun bool, mwCount, epCount, routesParsed, matched, unmatched, existingRels int) error {
	now := time.Now().Format("2006-01-02")
	dryTag := ""
	if dryRun {
		dryTag = " [DRY RUN]"
	}
	fmt.Printf("┌─ GRAPH SYNC MIDDLEWARE%s\n", dryTag)
	fmt.Printf("  Generated: %s\n\n", now)

	fmt.Printf("┌─ SUMMARY\n")
	fmt.Printf("  Middleware objects    : %d\n", mwCount)
	fmt.Printf("  Endpoint objects     : %d\n", epCount)
	fmt.Printf("  Routes parsed        : %d\n", routesParsed)
	fmt.Printf("  Routes matched       : %d\n", matched)
	fmt.Printf("  Routes unmatched     : %d\n", unmatched)
	fmt.Printf("  Existing rels        : %d\n", existingRels)
	fmt.Printf("  Relationships to add : %d\n", len(rels))
	if !dryRun {
		fmt.Printf("  Rels created         : %d\n", createdRels)
		fmt.Printf("  Rels failed          : %d\n", failedRels)
	}
	fmt.Printf("  Scope updates        : %d\n", len(scopes))
	if !dryRun {
		fmt.Printf("  Scopes applied       : %d\n", appliedScopes)
		fmt.Printf("  Scopes failed        : %d\n", failedScopes)
	}
	fmt.Println()

	if len(rels) > 0 {
		fmt.Printf("┌─ RELATIONSHIPS TO CREATE (applies_to: Middleware → APIEndpoint)\n")
		t := tablewriter.NewWriter(os.Stdout)
		t.Header("MIDDLEWARE", "ENDPOINT KEY")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{1: 70}
		})
		for _, r := range rels {
			t.Append([]string{r.MiddlewareName, r.EndpointKey})
		}
		t.Render()
		fmt.Println()
	}

	if len(scopes) > 0 {
		fmt.Printf("┌─ SCOPE UPDATES\n")
		t := tablewriter.NewWriter(os.Stdout)
		t.Header("ENDPOINT KEY", "OLD SCOPES", "NEW SCOPES")
		t.Configure(func(cfg *tablewriter.Config) {
			cfg.Behavior.TrimSpace = tw.On
			cfg.Row.ColMaxWidths.PerColumn = map[int]int{0: 50, 1: 30, 2: 30}
		})
		for _, s := range scopes {
			t.Append([]string{s.EndpointKey, s.OldScopes, s.NewScopes})
		}
		t.Render()
	}

	return nil
}

// ── Route file parsing ────────────────────────────────────────────────────────

// groupState tracks middleware accumulated for a group variable.
type groupState struct {
	prefix     string
	middleware []string // ordered middleware names
	scopes     []string // extracted scope strings
}

// parseRouteFileMiddleware parses a route file and returns per-handler middleware stacks.
func parseRouteFileMiddleware(path, domain string) ([]routeMiddleware, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Root vars (Echo instance)
	rootVars := map[string]bool{"e": true, "r": true, "router": true}

	// Regexes
	groupRe := regexp.MustCompile(`(\w+)\s*:?=\s*(\w+)\.Group\s*\(\s*"([^"]*)"`)
	useRe := regexp.MustCompile(`(\w+)\.Use\(`)
	routeRe := regexp.MustCompile(`(\w+)\.(GET|POST|PUT|DELETE|PATCH)\s*\(\s*"([^"]*)"[^,]*,\s*\w+\.(\w+)`)
	matchRe := regexp.MustCompile(`(\w+)\.Match\s*\(`)

	// Middleware call patterns
	requireAuthRe := regexp.MustCompile(`RequireAuth\(\)`)
	requireAPITokenRe := regexp.MustCompile(`RequireAPITokenScopes\("([^"]+)"\)`)
	requireProjectScopeRe := regexp.MustCompile(`RequireProjectScope\(\)`)
	requireProjectIDRe := regexp.MustCompile(`RequireProjectID\(\)`)
	requireScopesRe := regexp.MustCompile(`RequireScopes\(`)
	toolAuditRe := regexp.MustCompile(`ToolAuditMiddleware\(`)
	toolRestrictionRe := regexp.MustCompile(`ToolRestrictionMiddleware\(`)

	// groups: varName → groupState (inherits from parent)
	groups := make(map[string]*groupState)

	// First pass: build group prefix map (same as graph-sync-routes)
	groupPrefixes := make(map[string]string)
	for _, line := range lines {
		if m := groupRe.FindStringSubmatch(line); m != nil {
			varName, parentVar, groupPath := m[1], m[2], m[3]
			if rootVars[parentVar] {
				groupPrefixes[varName] = groupPath
			} else {
				groupPrefixes[varName] = groupPrefixes[parentVar] + groupPath
			}
		}
	}

	// Initialize group states with prefixes
	for varName, prefix := range groupPrefixes {
		groups[varName] = &groupState{prefix: prefix}
	}

	// Second pass: process Use() calls and route registrations in order
	// We need to track which group var a Use() call is on and accumulate middleware.
	// Since Go route files are sequential, we process line by line.

	// Reset and rebuild with middleware tracking
	groups = make(map[string]*groupState)
	for varName, prefix := range groupPrefixes {
		groups[varName] = &groupState{prefix: prefix}
	}

	// parentOf: when a group is created from another group, inherit its middleware
	parentOf := make(map[string]string) // child → parent
	for _, line := range lines {
		if m := groupRe.FindStringSubmatch(line); m != nil {
			varName, parentVar := m[1], m[2]
			if !rootVars[parentVar] {
				parentOf[varName] = parentVar
			}
		}
	}

	// inheritedMiddleware returns the middleware stack inherited from parent chain
	var inheritedMiddleware func(varName string) ([]string, []string)
	inheritedMiddleware = func(varName string) ([]string, []string) {
		parent, ok := parentOf[varName]
		if !ok {
			return nil, nil
		}
		parentMW, parentScopes := inheritedMiddleware(parent)
		g := groups[parent]
		if g == nil {
			return parentMW, parentScopes
		}
		mw := append(parentMW, g.middleware...)
		sc := append(parentScopes, g.scopes...)
		return mw, sc
	}

	// extractMiddlewareFromLine extracts middleware name(s) from a .Use(...) line
	extractMW := func(line string) (names []string, scopes []string) {
		if requireAuthRe.MatchString(line) {
			names = append(names, "RequireAuth")
		}
		if m := requireAPITokenRe.FindStringSubmatch(line); m != nil {
			names = append(names, "RequireAPITokenScopes")
			scopes = append(scopes, m[1])
		}
		if requireProjectScopeRe.MatchString(line) {
			names = append(names, "RequireProjectScope")
		}
		if requireProjectIDRe.MatchString(line) {
			names = append(names, "RequireProjectID")
		}
		if requireScopesRe.MatchString(line) {
			names = append(names, "RequireScopes")
		}
		if toolAuditRe.MatchString(line) {
			names = append(names, "ToolAuditMiddleware")
		}
		if toolRestrictionRe.MatchString(line) {
			names = append(names, "ToolRestrictionMiddleware")
		}
		return
	}

	// Process lines sequentially
	for _, line := range lines {
		// Handle group creation (already done above for prefixes)
		if m := groupRe.FindStringSubmatch(line); m != nil {
			varName := m[1]
			if groups[varName] == nil {
				groups[varName] = &groupState{prefix: groupPrefixes[varName]}
			}
		}

		// Handle .Use() calls — accumulate middleware on the group var
		if m := useRe.FindStringSubmatch(line); m != nil {
			varName := m[1]
			if rootVars[varName] {
				continue // skip global middleware (CORS, RequestLogger, etc.)
			}
			g := groups[varName]
			if g == nil {
				g = &groupState{prefix: groupPrefixes[varName]}
				groups[varName] = g
			}
			names, scopes := extractMW(line)
			g.middleware = append(g.middleware, names...)
			g.scopes = append(g.scopes, scopes...)
		}
	}

	// Third pass: extract routes with full middleware stacks
	var results []routeMiddleware
	seen := make(map[string]bool) // domain:handler dedup

	processRoute := func(varName, handler string) {
		key := domain + ":" + strings.ToLower(handler)
		if seen[key] {
			return
		}
		seen[key] = true

		// Build full middleware stack: inherited + own group
		inherited, inheritedScopes := inheritedMiddleware(varName)
		var ownMW []string
		var ownScopes []string
		if g := groups[varName]; g != nil {
			ownMW = g.middleware
			ownScopes = g.scopes
		}

		allMW := append(inherited, ownMW...)
		allScopes := append(inheritedScopes, ownScopes...)

		// Deduplicate middleware names (preserve order, keep first occurrence)
		seen2 := make(map[string]bool)
		var dedupMW []string
		for _, m := range allMW {
			if !seen2[m] {
				seen2[m] = true
				dedupMW = append(dedupMW, m)
			}
		}

		// Deduplicate scopes
		seenScope := make(map[string]bool)
		var dedupScopes []string
		for _, s := range allScopes {
			if !seenScope[s] {
				seenScope[s] = true
				dedupScopes = append(dedupScopes, s)
			}
		}

		isPublic := !seen2["RequireAuth"]

		results = append(results, routeMiddleware{
			Domain:     domain,
			Handler:    handler,
			Middleware: dedupMW,
			Scopes:     dedupScopes,
			IsPublic:   isPublic,
		})
	}

	for _, line := range lines {
		if m := routeRe.FindStringSubmatch(line); m != nil {
			varName, handler := m[1], m[4]
			if rootVars[varName] {
				// Route on root echo — no group middleware
				processRoute(varName, handler)
			} else {
				processRoute(varName, handler)
			}
		}
		// Handle Match() calls (e.g., g.Match([]string{"GET","POST","DELETE"}, "", handler))
		if matchRe.MatchString(line) {
			// Extract handler from Match call
			matchHandlerRe := regexp.MustCompile(`Match\s*\([^,]+,\s*"[^"]*"\s*,\s*\w+\.(\w+)`)
			if m := matchHandlerRe.FindStringSubmatch(line); m != nil {
				varName := matchRe.FindStringSubmatch(line)[1]
				handler := m[1]
				processRoute(varName, handler)
			}
		}
	}

	return results, nil
}

// resolveEndpoint finds a graph APIEndpoint for a given domain+handler.
func resolveEndpoint(domain, handler string, byKey map[string]*sdkgraph.GraphObject, byDomainHandler map[string][]*sdkgraph.GraphObject) *sdkgraph.GraphObject {
	// Try key match first
	candidateKey := "ep-" + domain + "-" + strings.ToLower(handler)
	if ep, ok := byKey[candidateKey]; ok {
		return ep
	}
	// Try domain:handler match
	dhKey := domain + ":" + strings.ToLower(handler)
	if eps, ok := byDomainHandler[dhKey]; ok && len(eps) == 1 {
		return eps[0]
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
	const pageSize = 1000
	var all []*sdkgraph.GraphRelationship
	var cursor string
	for {
		resp, err := g.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Type: relType, Limit: pageSize, Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("listing %s relationships: %w", relType, err)
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
