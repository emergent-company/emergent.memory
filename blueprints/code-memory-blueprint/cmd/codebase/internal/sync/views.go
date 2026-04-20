package synccmd

// sync views — extracts React/frontend view files as Context graph objects.
//
// Strategy:
//   - Walk the glob pattern for view files (e.g. apps/web/src/views/**/*.tsx)
//   - Derive a human-readable name from the file path (e.g. "Meeting Details")
//   - Attempt to match a route from the routes file by scanning string literals
//   - Create/update Context objects with context_type=web-view, type=screen
//
// Key naming: ctx-<slug> derived from the relative path.

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

type viewsOptions struct {
	repo    string
	glob    string
	routes  string
	sync    bool
	verbose bool
	deps    bool
}

func newViewsCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	opts := &viewsOptions{}
	cwd, _ := os.Getwd()

	cmd := &cobra.Command{
		Use:   "views",
		Short: "Sync frontend view files as Context objects",
		Long: `Walks view files matching a glob pattern and creates Context objects in the graph.

Each view file becomes a Context with:
  context_type: web-view
  type:         screen
  route:        matched from routes file (if configured)

Configure in .codebase.yml:
  sync:
    views:
      glob: apps/web/src/views/**/*.tsx
      routes_file: apps/web/src/routes.ts

Or pass flags directly:
  codebase sync views --glob "apps/web/src/views/**/*.tsx" --routes apps/web/src/routes.ts --sync`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runViews(opts, flagProjectID, flagBranch, flagFormat)
		},
	}

	cmd.Flags().StringVar(&opts.repo, "repo", cwd, "Path to repository root")
	cmd.Flags().StringVar(&opts.glob, "glob", "", "Glob pattern for view files (relative to repo root)")
	cmd.Flags().StringVar(&opts.routes, "routes", "", "Path to routes file for route matching (relative to repo root)")
	cmd.Flags().BoolVar(&opts.sync, "sync", false, "Create missing and update stale Context objects")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "Print every view in the summary table")
	cmd.Flags().BoolVar(&opts.deps, "deps", false, "Wire uses_component relationships from Context to UIComponent based on imports")

	return cmd
}

// viewInfo holds extracted metadata for a view file.
type viewInfo struct {
	name        string
	route       string
	description string
}

// viewRecord is a view file with its graph key and extracted info.
type viewRecord struct {
	rel  string
	key  string
	info *viewInfo
}

func runViews(opts *viewsOptions, flagProjectID *string, flagBranch *string, flagFormat *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}

	yml := config.LoadYML()
	glob := opts.glob
	routesFile := opts.routes
	if yml != nil {
		if glob == "" {
			glob = yml.Sync.Views.Glob
		}
		if routesFile == "" {
			routesFile = yml.Sync.Views.RoutesFile
		}
	}
	if glob == "" {
		return fmt.Errorf("no glob pattern configured — set sync.views.glob in .codebase.yml or pass --glob")
	}

	absRoot, err := filepath.Abs(opts.repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Load routes file for route matching
	routeMap := map[string]string{}
	if routesFile != "" {
		absRoutes := filepath.Join(absRoot, routesFile)
		routeMap = parseRoutesFile(absRoutes)
		fmt.Printf("→ Loaded %d route patterns from %s\n", len(routeMap), routesFile)
	}

	// Walk disk files matching glob
	fmt.Printf("→ Scanning views with glob: %s\n", glob)
	var diskViews []viewRecord
	if err := walkGlob(absRoot, glob, func(rel string) {
		info := extractViewInfo(rel, routeMap)
		diskViews = append(diskViews, viewRecord{rel: rel, key: viewKey(rel), info: info})
	}); err != nil {
		return fmt.Errorf("walking views: %w", err)
	}
	sort.Slice(diskViews, func(i, j int) bool { return diskViews[i].rel < diskViews[j].rel })
	fmt.Printf("  %d view files found\n", len(diskViews))

	// Fetch existing Context objects from graph
	fmt.Println("→ Fetching existing Context objects from graph...")
	graphObjs, err := listAllObjects(ctx, c.Graph, "Context")
	if err != nil {
		return fmt.Errorf("fetching contexts: %w", err)
	}
	graphByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range graphObjs {
		if derefKey(obj.Key) != "" {
			graphByKey[derefKey(obj.Key)] = obj
		}
	}

	// Diff
	var toCreate, toUpdate, upToDate []viewRecord
	for _, v := range diskViews {
		obj, exists := graphByKey[v.key]
		if !exists {
			toCreate = append(toCreate, v)
		} else if needsViewUpdate(obj, v.info) {
			toUpdate = append(toUpdate, v)
		} else {
			upToDate = append(upToDate, v)
		}
	}

	printHeader("VIEWS SYNC STATUS")
	fmt.Printf("  Disk views      : %d\n", len(diskViews))
	fmt.Printf("  Graph contexts  : %d\n", len(graphByKey))
	fmt.Printf("  To create       : %d\n", len(toCreate))
	fmt.Printf("  To update       : %d\n", len(toUpdate))
	fmt.Printf("  Up to date      : %d\n", len(upToDate))

	if opts.verbose {
		fmt.Println()
		printHeader("ALL VIEWS")
		fmt.Printf("  %-50s  %-30s  %s\n", "File", "Key", "Route")
		fmt.Printf("  %-50s  %-30s  %s\n", strings.Repeat("─", 50), strings.Repeat("─", 30), strings.Repeat("─", 30))
		for _, v := range diskViews {
			fmt.Printf("  %-50s  %-30s  %s\n", truncate(v.rel, 50), truncate(v.key, 30), v.info.route)
		}
	}

	if len(toCreate) > 0 {
		fmt.Println()
		printHeader(fmt.Sprintf("TO CREATE (%d)", len(toCreate)))
		for _, v := range toCreate {
			fmt.Printf("  + %-50s  route=%s\n", v.rel, v.info.route)
		}
	}
	if len(toUpdate) > 0 {
		fmt.Println()
		printHeader(fmt.Sprintf("TO UPDATE (%d)", len(toUpdate)))
		for _, v := range toUpdate {
			fmt.Printf("  ~ %-50s  route=%s\n", v.rel, v.info.route)
		}
	}

	if !opts.sync && !opts.deps {
		if len(toCreate) > 0 || len(toUpdate) > 0 {
			fmt.Println("\nRun with --sync to apply changes.")
		}
		return nil
	}

	fmt.Println()
	printHeader("SYNCING")

	if opts.sync {
		if len(toCreate) > 0 {
			fmt.Printf("Creating %d Context objects...\n", len(toCreate))
			created, failed := batchCreateContexts(ctx, c.Graph, toCreate)
			fmt.Printf("  Created: %d  Failed: %d\n", created, failed)
		}
		if len(toUpdate) > 0 {
			fmt.Printf("Updating %d Context objects...\n", len(toUpdate))
			updated, failed := updateContexts(ctx, c.Graph, toUpdate, graphByKey)
			fmt.Printf("  Updated: %d  Failed: %d\n", updated, failed)
		}
	}

	if opts.deps {
		fmt.Println()
		printHeader("WIRING CONTEXT → COMPONENT DEPENDENCIES")

		// Refresh Context objects after potential creates
		graphCtxObjs, err := listAllObjects(ctx, c.Graph, "Context")
		if err != nil {
			return fmt.Errorf("fetching contexts for dep wiring: %w", err)
		}
		ctxByKey := map[string]*sdkgraph.GraphObject{}
		for _, obj := range graphCtxObjs {
			if derefKey(obj.Key) != "" {
				ctxByKey[derefKey(obj.Key)] = obj
			}
		}

		// Fetch all UIComponent objects
		uiObjs, err := listAllObjects(ctx, c.Graph, "UIComponent")
		if err != nil {
			return fmt.Errorf("fetching UIComponents for dep wiring: %w", err)
		}
		uiByKey := map[string]*sdkgraph.GraphObject{}
		for _, obj := range uiObjs {
			if derefKey(obj.Key) != "" {
				uiByKey[derefKey(obj.Key)] = obj
			}
		}

		// Also fetch Helper objects (hooks) — views may import hooks
		helperObjs, err := listAllObjects(ctx, c.Graph, "Helper")
		if err != nil {
			return fmt.Errorf("fetching Helpers for dep wiring: %w", err)
		}
		for _, obj := range helperObjs {
			if derefKey(obj.Key) != "" {
				uiByKey[derefKey(obj.Key)] = obj
			}
		}

		// Collect all UIComponent keys for import resolution
		uiKeys := make([]string, 0, len(uiByKey))
		for k := range uiByKey {
			uiKeys = append(uiKeys, k)
		}

		wired, skipped, missing := wireViewComponentDeps(ctx, c.Graph, absRoot, diskViews, ctxByKey, uiByKey, uiKeys, opts.verbose)
		fmt.Printf("  Wired: %d  Skipped (exists): %d  Unresolved imports: %d\n", wired, skipped, missing)
	}

	fmt.Println("\nSync complete.")
	return nil
}

func extractViewInfo(rel string, routeMap map[string]string) *viewInfo {
	base := filepath.Base(rel)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	for _, suffix := range []string{"-view", "-page", "-screen"} {
		base = strings.TrimSuffix(base, suffix)
	}

	name := toTitle(base)
	route := matchRoute(rel, routeMap)

	parts := strings.Split(filepath.ToSlash(rel), "/")
	domain := ""
	for i, p := range parts {
		if p == "views" && i+1 < len(parts) {
			domain = parts[i+1]
			break
		}
	}
	desc := fmt.Sprintf("View: %s", name)
	if domain != "" && domain != base {
		desc = fmt.Sprintf("%s view in %s domain", name, toTitle(domain))
	}

	return &viewInfo{name: name, route: route, description: desc}
}

func viewKey(rel string) string {
	key := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(rel)
	key = strings.ToLower(key)
	for _, prefix := range []string{"apps-web-src-views-", "apps-web-src-", "src-views-"} {
		if strings.HasPrefix(key, prefix) {
			key = strings.TrimPrefix(key, prefix)
			break
		}
	}
	return "ctx-" + key
}

func needsViewUpdate(obj *sdkgraph.GraphObject, info *viewInfo) bool {
	return info.route != "" && strProp(obj, "route") != info.route
}

func batchCreateContexts(ctx context.Context, g *sdkgraph.Client, views []viewRecord) (int, int) {
	const batchSize = 100
	created, failed := 0, 0
	for i := 0; i < len(views); i += batchSize {
		end := i + batchSize
		if end > len(views) {
			end = len(views)
		}
		batch := views[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, v := range batch {
			key := v.key
			props := map[string]any{
				"context_type": "web-view",
				"type":         "screen",
				"description":  v.info.description,
			}
			if v.info.route != "" {
				props["route"] = v.info.route
			}
			props["name"] = v.info.name
			items = append(items, sdkgraph.CreateObjectRequest{
				Type:       "Context",
				Key:        &key,
				Properties: props,
			})
		}
		resp, err := g.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: items})
		if err != nil {
			failed += len(batch)
			continue
		}
		created += resp.Success
		failed += resp.Failed
	}
	return created, failed
}

func updateContexts(ctx context.Context, g *sdkgraph.Client, views []viewRecord, graphByKey map[string]*sdkgraph.GraphObject) (int, int) {
	updated, failed := 0, 0
	for _, v := range views {
		obj, ok := graphByKey[v.key]
		if !ok {
			failed++
			continue
		}
		props := map[string]any{}
		if v.info.route != "" {
			props["route"] = v.info.route
		}
		if len(props) == 0 {
			continue
		}
		if _, err := g.UpdateObject(ctx, obj.EntityID, &sdkgraph.UpdateObjectRequest{Properties: props}); err != nil {
			failed++
		} else {
			updated++
		}
	}
	return updated, failed
}

// parseRoutesFile reads a TypeScript/JS routes file and extracts string literals
// that look like URL paths (start with /).
func parseRoutesFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()

	routeRe := regexp.MustCompile(`['"]([/][^'"]{2,})['"]`)
	result := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		matches := routeRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			route := m[1]
			if !strings.HasPrefix(route, "/") || len(route) <= 1 {
				continue
			}
			parts := strings.Split(strings.Trim(route, "/"), "/")
			// Index by last non-param segment
			for i := len(parts) - 1; i >= 0; i-- {
				seg := parts[i]
				if seg != "" && !strings.HasPrefix(seg, ":") {
					if _, exists := result[seg]; !exists {
						result[seg] = route
					}
					break
				}
			}
			// Also index by first segment
			if len(parts) > 0 && parts[0] != "" && !strings.HasPrefix(parts[0], ":") {
				if _, exists := result[parts[0]]; !exists {
					result[parts[0]] = route
				}
			}
		}
	}
	return result
}

// matchRoute tries to find a route for a view file path.
func matchRoute(rel string, routeMap map[string]string) string {
	if len(routeMap) == 0 {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		seg := strings.ToLower(parts[i])
		for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
			seg = strings.TrimSuffix(seg, ext)
		}
		if route, ok := routeMap[seg]; ok {
			return route
		}
		seg2 := strings.ReplaceAll(seg, "-", "")
		if route, ok := routeMap[seg2]; ok {
			return route
		}
	}
	return ""
}

// walkGlob walks files matching a glob pattern relative to root.
func walkGlob(root, pattern string, fn func(rel string)) error {
	absPattern := filepath.Join(root, pattern)
	prefix := globPrefix(absPattern)

	return filepath.WalkDir(prefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "dist" || name == "__tests__" || name == "stories" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if matchesGlob(pattern, rel) {
			fn(rel)
		}
		return nil
	})
}

// globPrefix returns the longest non-glob prefix of a pattern.
func globPrefix(pattern string) string {
	sep := string(filepath.Separator)
	parts := strings.Split(pattern, sep)
	var prefix []string
	for _, p := range parts {
		if strings.ContainsAny(p, "*?[") {
			break
		}
		prefix = append(prefix, p)
	}
	if len(prefix) == 0 {
		return "."
	}
	return strings.Join(prefix, sep)
}

// matchesGlob checks if a relative path matches a glob pattern (supports **).
// Handles multiple ** segments by converting to a recursive match.
func matchesGlob(pattern, rel string) bool {
	pattern = filepath.ToSlash(pattern)
	rel = filepath.ToSlash(rel)
	if !strings.Contains(pattern, "**") {
		m, _ := filepath.Match(pattern, rel)
		return m
	}
	return matchDoubleStarGlob(pattern, rel)
}

// matchDoubleStarGlob matches a glob pattern containing ** against a path.
// ** matches zero or more path segments.
func matchDoubleStarGlob(pattern, path string) bool {
	patParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	return matchParts(patParts, pathParts)
}

func matchParts(patParts, pathParts []string) bool {
	for len(patParts) > 0 {
		p := patParts[0]
		if p == "**" {
			// ** can match zero or more path segments
			// Try matching the rest of the pattern against every suffix of pathParts
			rest := patParts[1:]
			for i := 0; i <= len(pathParts); i++ {
				if matchParts(rest, pathParts[i:]) {
					return true
				}
			}
			return false
		}
		if len(pathParts) == 0 {
			return false
		}
		m, _ := filepath.Match(p, pathParts[0])
		if !m {
			return false
		}
		patParts = patParts[1:]
		pathParts = pathParts[1:]
	}
	return len(pathParts) == 0
}

// slugify converts a name to a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	return strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(s)
}

// toTitle converts a slug/filename to a human-readable title.
func toTitle(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// importToUIKey converts an import path to a UIComponent/Helper graph key.
// Uses the same componentKey() logic as sync components so keys match exactly.
//
// Handles:
//   - Alias imports: shared-web/components/button → ui-button
//   - Relative imports: ../components/button → ui-button
//   - Hook imports: ./hooks/use-meeting → hook-use-meeting
//
// Returns "" if the import doesn't look like a component/hook import.
func importToUIKey(importPath string, sourceRel string) string {
	// Normalize to slash
	importPath = filepath.ToSlash(importPath)

	var resolvedBase string

	// Handle alias: shared-web/components/* → libs/shared-web/src/components/*
	if strings.HasPrefix(importPath, "shared-web/components/") {
		resolvedBase = "libs/shared-web/src/" + strings.TrimPrefix(importPath, "shared-web/")
	} else if strings.HasPrefix(importPath, ".") {
		// Handle relative imports — resolve against source file directory
		sourceDir := filepath.ToSlash(filepath.Dir(sourceRel))
		resolvedBase = filepath.ToSlash(filepath.Join(sourceDir, importPath))
	} else {
		return ""
	}

	// Try with common extensions — componentKey() needs the extension to detect hooks
	for _, ext := range []string{".tsx", ".ts"} {
		candidate := resolvedBase + ext
		return componentKey(candidate)
	}
	return ""
}

// wireViewComponentDeps parses imports in each view file and creates
// uses_component (contains) relationships from Context → UIComponent.
func wireViewComponentDeps(
	ctx context.Context,
	g *sdkgraph.Client,
	absRoot string,
	diskViews []viewRecord,
	ctxByKey map[string]*sdkgraph.GraphObject,
	uiByKey map[string]*sdkgraph.GraphObject,
	uiKeys []string,
	verbose bool,
) (int, int, int) {
	// Fetch existing contains relationships to avoid duplicates
	existingRels := map[string]bool{}
	rels, _ := listAllRelationships(ctx, g, "contains")
	for _, r := range rels {
		existingRels[r.SrcID+":"+r.DstID] = true
	}

	var toWire []sdkgraph.CreateRelationshipRequest
	unresolved := 0

	for _, view := range diskViews {
		ctxObj, ok := ctxByKey[view.key]
		if !ok {
			continue
		}

		absPath := filepath.Join(absRoot, view.rel)
		imports := parseComponentImports(absPath)

		for _, imp := range imports {
			// Only process component-like imports
			if !strings.HasPrefix(imp, ".") && !strings.HasPrefix(imp, "shared-web/components") {
				continue
			}

			uiKey := importToUIKey(imp, view.rel)
			if uiKey == "" {
				unresolved++
				continue
			}

			// Try exact key match first
			dstObj, ok := uiByKey[uiKey]
			if !ok {
				// Try with index.tsx suffix
				uiKeyIdx := strings.TrimSuffix(uiKey, "-tsx") + "-index-tsx"
				dstObj, ok = uiByKey[uiKeyIdx]
				if !ok {
					unresolved++
					continue
				}
			}

			relKey := ctxObj.EntityID + ":" + dstObj.EntityID
			if existingRels[relKey] {
				continue
			}
			existingRels[relKey] = true
			if verbose {
				fmt.Printf("  %s → %s\n", view.key, uiKey)
			}
			toWire = append(toWire, sdkgraph.CreateRelationshipRequest{
				Type:   "contains",
				SrcID:  ctxObj.EntityID,
				DstID:  dstObj.EntityID,
				Upsert: true,
			})
		}
	}

	fmt.Printf("  Found %d context→component dependency edges to wire\n", len(toWire))

	const batchSize = 100
	wired := 0
	skipped := 0
	for i := 0; i < len(toWire); i += batchSize {
		end := i + batchSize
		if end > len(toWire) {
			end = len(toWire)
		}
		resp, err := g.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: toWire[i:end]})
		if err != nil {
			skipped += end - i
			continue
		}
		for _, r := range resp.Results {
			if r.Error != nil {
				skipped++
			} else {
				wired++
			}
		}
	}
	return wired, skipped, unresolved
}
