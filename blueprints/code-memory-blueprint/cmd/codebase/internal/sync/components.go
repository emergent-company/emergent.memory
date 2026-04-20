package synccmd

// sync components — extracts UI component files as UIComponent graph objects.
//
// Strategy:
//   - Walk the glob pattern for component files (e.g. libs/shared-web/src/components/**/*.tsx)
//   - Derive name from file base (e.g. "Button", "DataTable")
//   - Infer component type from path (atoms/molecules/organisms → primitive/composite/layout)
//   - Create/update UIComponent objects in the graph
//   - Parse imports to wire contains relationships (UIComponent → UIComponent dependencies)
//
// Key naming: ui-<slug> derived from the relative path.

import (
	"bufio"
	"context"
	"fmt"
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

type componentsOptions struct {
	repo    string
	glob    string
	sync    bool
	verbose bool
	deps    bool
}

func newComponentsCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	opts := &componentsOptions{}
	cwd, _ := os.Getwd()

	cmd := &cobra.Command{
		Use:   "components",
		Short: "Sync UI component files as UIComponent objects",
		Long: `Walks component files matching a glob pattern and creates UIComponent objects in the graph.

Each component file becomes a UIComponent with:
  type:        inferred from path (primitive/composite/layout/container)
  description: derived from file name and path

Configure in .codebase.yml:
  sync:
    components:
      glob: libs/shared-web/src/components/**/*.tsx

Or pass flags directly:
  codebase sync components --glob "libs/shared-web/src/components/**/*.tsx" --sync`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runComponents(opts, flagProjectID, flagBranch, flagFormat)
		},
	}

	cmd.Flags().StringVar(&opts.repo, "repo", cwd, "Path to repository root")
	cmd.Flags().StringVar(&opts.glob, "glob", "", "Glob pattern for component files (relative to repo root)")
	cmd.Flags().BoolVar(&opts.sync, "sync", false, "Create missing UIComponent objects")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "Print every component in the summary table")
	cmd.Flags().BoolVar(&opts.deps, "deps", false, "Wire contains relationships between components based on import analysis")

	return cmd
}

// componentInfo holds extracted metadata for a component file.
type componentInfo struct {
	name        string
	compType    string
	description string
	objectType  string // "UIComponent" or "Helper"
}

// componentRecord is a component file with its graph key and extracted info.
type componentRecord struct {
	rel  string
	key  string
	info *componentInfo
}

func runComponents(opts *componentsOptions, flagProjectID *string, flagBranch *string, flagFormat *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}

	yml := config.LoadYML()
	glob := opts.glob
	if yml != nil && glob == "" {
		glob = yml.Sync.Components.Glob
	}
	if glob == "" {
		return fmt.Errorf("no glob pattern configured — set sync.components.glob in .codebase.yml or pass --glob")
	}

	absRoot, err := filepath.Abs(opts.repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Printf("→ Scanning components with glob: %s\n", glob)
	var diskComponents []componentRecord
	if err := walkGlob(absRoot, glob, func(rel string) {
		// Skip story/test/index files
		base := filepath.Base(rel)
		if strings.Contains(base, ".stories.") || strings.Contains(base, ".test.") ||
			strings.Contains(base, ".spec.") || base == "index.tsx" || base == "index.ts" {
			return
		}
		info := extractComponentInfo(rel)
		diskComponents = append(diskComponents, componentRecord{rel: rel, key: componentKey(rel), info: info})
	}); err != nil {
		return fmt.Errorf("walking components: %w", err)
	}
	sort.Slice(diskComponents, func(i, j int) bool { return diskComponents[i].rel < diskComponents[j].rel })
	fmt.Printf("  %d component files found\n", len(diskComponents))

	fmt.Println("→ Fetching existing UIComponent objects from graph...")
	graphObjs, err := listAllObjects(ctx, c.Graph, "UIComponent")
	if err != nil {
		return fmt.Errorf("fetching components: %w", err)
	}
	graphByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range graphObjs {
		if derefKey(obj.Key) != "" {
			graphByKey[derefKey(obj.Key)] = obj
		}
	}

	// Also fetch Helper objects (hooks) — they share the same key namespace
	helperObjs, err := listAllObjects(ctx, c.Graph, "Helper")
	if err != nil {
		return fmt.Errorf("fetching helpers: %w", err)
	}
	for _, obj := range helperObjs {
		if derefKey(obj.Key) != "" {
			graphByKey[derefKey(obj.Key)] = obj
		}
	}

	var toCreate, upToDate []componentRecord
	for _, comp := range diskComponents {
		if _, exists := graphByKey[comp.key]; exists {
			upToDate = append(upToDate, comp)
		} else {
			toCreate = append(toCreate, comp)
		}
	}

	uiCount := 0
	helperCount := 0
	for _, comp := range diskComponents {
		if comp.info.objectType == "Helper" {
			helperCount++
		} else {
			uiCount++
		}
	}

	printHeader("COMPONENTS SYNC STATUS")
	fmt.Printf("  Disk components : %d (%d UIComponent, %d Helper)\n", len(diskComponents), uiCount, helperCount)
	fmt.Printf("  Graph objects   : %d\n", len(graphByKey))
	fmt.Printf("  To create       : %d\n", len(toCreate))
	fmt.Printf("  Up to date      : %d\n", len(upToDate))

	if opts.verbose {
		fmt.Println()
		printHeader("ALL COMPONENTS")
		fmt.Printf("  %-50s  %-20s  %s\n", "File", "Key", "Type")
		fmt.Printf("  %-50s  %-20s  %s\n", strings.Repeat("─", 50), strings.Repeat("─", 20), strings.Repeat("─", 15))
		for _, comp := range diskComponents {
			fmt.Printf("  %-50s  %-20s  %s\n", truncate(comp.rel, 50), truncate(comp.key, 20), comp.info.compType)
		}
	}

	if len(toCreate) > 0 {
		fmt.Println()
		printHeader(fmt.Sprintf("TO CREATE (%d)", len(toCreate)))
		for _, comp := range toCreate {
			fmt.Printf("  + %-50s  type=%s\n", comp.rel, comp.info.compType)
		}
	}

	if !opts.sync && !opts.deps {
		if len(toCreate) > 0 {
			fmt.Println("\nRun with --sync to apply changes.")
		}
		return nil
	}

	fmt.Println()
	printHeader("SYNCING")

	if opts.sync && len(toCreate) > 0 {
		fmt.Printf("Creating %d UIComponent objects...\n", len(toCreate))
		created, failed := batchCreateComponents(ctx, c.Graph, toCreate)
		fmt.Printf("  Created: %d  Failed: %d\n", created, failed)
	} else if !opts.sync && len(toCreate) > 0 {
		fmt.Printf("  (skipping %d new components — run with --sync to create)\n", len(toCreate))
	}

	if opts.deps {
		fmt.Println()
		printHeader("WIRING COMPONENT DEPENDENCIES")

		// Refresh graph objects after potential creates
		graphObjs2, err := listAllObjects(ctx, c.Graph, "UIComponent")
		if err != nil {
			return fmt.Errorf("fetching components for dep wiring: %w", err)
		}
		graphByKey2 := map[string]*sdkgraph.GraphObject{}
		for _, obj := range graphObjs2 {
			if derefKey(obj.Key) != "" {
				graphByKey2[derefKey(obj.Key)] = obj
			}
		}

		// Build a map from relative path → component key for import resolution
		relToKey := map[string]string{}
		for _, comp := range diskComponents {
			relToKey[comp.rel] = comp.key
		}

		wired, skipped, missing := wireComponentDeps(ctx, c.Graph, absRoot, diskComponents, relToKey, graphByKey2, opts.verbose)
		fmt.Printf("  Wired: %d  Skipped (exists): %d  Unresolved imports: %d\n", wired, skipped, missing)
	}

	fmt.Println("\nSync complete.")
	return nil
}

func extractComponentInfo(rel string) *componentInfo {
	base := filepath.Base(rel)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	// Remove secondary extension (e.g. .component, .widget)
	for _, suffix := range []string{".component", ".widget", ".container"} {
		base = strings.TrimSuffix(base, suffix)
	}

	name := toTitle(base)

	// Detect hooks — files in /hooks/ directories or named use-*
	lower := strings.ToLower(filepath.ToSlash(rel))
	isHook := strings.Contains(lower, "/hooks/") ||
		strings.HasPrefix(strings.ToLower(base), "use-") ||
		strings.HasPrefix(strings.ToLower(base), "use_")

	if isHook {
		return &componentInfo{
			name:        name,
			compType:    "hook",
			description: fmt.Sprintf("React hook: %s", name),
			objectType:  "Helper",
		}
	}

	compType := inferComponentType(rel)
	desc := fmt.Sprintf("%s UI component", name)
	if compType != "" {
		desc = fmt.Sprintf("%s UI component (%s)", name, compType)
	}

	return &componentInfo{name: name, compType: compType, description: desc, objectType: "UIComponent"}
}

// inferComponentType infers the component type from path segments.
// Supports atomic design (atoms/molecules/organisms) and common folder names.
func inferComponentType(rel string) string {
	lower := strings.ToLower(filepath.ToSlash(rel))
	switch {
	case strings.Contains(lower, "/atoms/") || strings.Contains(lower, "/atom/"):
		return "primitive"
	case strings.Contains(lower, "/molecules/") || strings.Contains(lower, "/molecule/"):
		return "composite"
	case strings.Contains(lower, "/organisms/") || strings.Contains(lower, "/organism/"):
		return "composite"
	case strings.Contains(lower, "/templates/") || strings.Contains(lower, "/template/"):
		return "layout"
	case strings.Contains(lower, "/layouts/") || strings.Contains(lower, "/layout/"):
		return "layout"
	case strings.Contains(lower, "/containers/") || strings.Contains(lower, "/container/"):
		return "container"
	case strings.Contains(lower, "/pages/") || strings.Contains(lower, "/page/"):
		return "container"
	case strings.Contains(lower, "/forms/") || strings.Contains(lower, "/form/"):
		return "composite"
	case strings.Contains(lower, "/table/") || strings.Contains(lower, "/tables/"):
		return "composite"
	case strings.Contains(lower, "/modal/") || strings.Contains(lower, "/modals/"):
		return "composite"
	default:
		return "composite"
	}
}

// componentKey derives an opaque stable key from a file path.
//
// Keys encode only the component name — no path, no domain, no extension.
// Structural information (which app, which domain, which file) is carried
// by graph relationships (defines, belongs_to, contains), not the key.
//
// Format:
//
//	ui-<stem>   for UIComponents  (e.g. ui-button, ui-attendance-box)
//	hook-<stem> for Helpers/hooks (e.g. hook-use-document-users)
//
// Collisions (two files with the same stem) are disambiguated by appending
// the immediate parent folder slug: ui-search-input-search-bar.
func componentKey(rel string) string {
	return componentKeyWithSiblings(rel, nil)
}

// componentKeyWithSiblings computes the key, disambiguating against a set of
// sibling relative paths that share the same stem.
func componentKeyWithSiblings(rel string, siblings []string) string {
	rel = filepath.ToSlash(rel)
	lower := strings.ToLower(rel)
	isHook := strings.Contains(lower, "/hooks/")

	base := filepath.Base(rel)
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
		base = strings.TrimSuffix(base, ext)
	}
	slug := stemSlug(base)
	prefix := "ui-"
	if isHook {
		prefix = "hook-"
	}
	key := prefix + slug

	// Disambiguate if a sibling produces the same key
	for _, sib := range siblings {
		if sib == rel {
			continue
		}
		sibBase := filepath.Base(sib)
		for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
			sibBase = strings.TrimSuffix(sibBase, ext)
		}
		if stemSlug(sibBase) == slug {
			// Append immediate parent folder (after stripping components/hooks)
			parent := parentSlug(rel)
			if parent != "" {
				key = prefix + slug + "-" + parent
			}
			break
		}
	}
	return key
}

// stemSlug converts a filename stem to a dash-slug, deduplicating adjacent repeated words.
func stemSlug(s string) string {
	s = strings.NewReplacer("_", "-", ".", "-").Replace(s)
	s = strings.ToLower(s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	parts := strings.Split(s, "-")
	deduped := make([]string, 0, len(parts))
	for i, p := range parts {
		if i > 0 && p == parts[i-1] {
			continue
		}
		deduped = append(deduped, p)
	}
	return strings.Join(deduped, "-")
}

// parentSlug returns the immediate parent folder slug, stripping components/hooks segments.
func parentSlug(rel string) string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	// Remove filename
	if len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}
	// Strip trailing components/hooks
	for len(parts) > 0 && (parts[len(parts)-1] == "components" || parts[len(parts)-1] == "hooks") {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return ""
	}
	return stemSlug(parts[len(parts)-1])
}

// pathToSlug is kept for backward compatibility with import resolution.
func pathToSlug(p string) string {
	p = filepath.ToSlash(p)
	p = strings.NewReplacer("/", "-", "_", "-", ".", "-").Replace(p)
	p = strings.ToLower(p)
	for strings.Contains(p, "--") {
		p = strings.ReplaceAll(p, "--", "-")
	}
	return strings.Trim(p, "-")
}

// importRe matches ES import statements: import ... from '...' or import ... from "..."
var importRe = regexp.MustCompile(`(?m)from\s+['"]([^'"]+)['"]`)

// parseComponentImports reads a .tsx/.ts file and returns all import paths.
func parseComponentImports(absPath string) []string {
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var imports []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		matches := importRe.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			if len(m) > 1 {
				imports = append(imports, m[1])
			}
		}
	}
	return imports
}

// resolveImportToKey resolves an import path from a source file to a UIComponent graph key.
// Handles:
//   - Relative imports: ../button, ./table-header
//   - Alias imports: shared-web/components/button
//
// Returns "" if the import doesn't resolve to a known component.
func resolveImportToKey(importPath string, sourceRel string, relToKey map[string]string, absRoot string, glob string) string {
	// Determine the component root prefix from the glob (e.g. "libs/shared-web/src/components")
	// by stripping the trailing /**/*.tsx
	compRoot := strings.TrimSuffix(glob, "/**/*.tsx")
	compRoot = strings.TrimSuffix(compRoot, "/**/*.ts")

	// Handle alias: shared-web/components/* → libs/shared-web/src/components/*
	if strings.HasPrefix(importPath, "shared-web/components/") {
		rel := "libs/shared-web/src/" + strings.TrimPrefix(importPath, "shared-web/")
		// Try with extensions
		for _, ext := range []string{".tsx", ".ts", "/index.tsx", "/index.ts"} {
			candidate := rel + ext
			if key, ok := relToKey[candidate]; ok {
				return key
			}
		}
		return ""
	}

	// Handle relative imports
	if strings.HasPrefix(importPath, ".") {
		sourceDir := filepath.Dir(sourceRel)
		resolved := filepath.Join(sourceDir, importPath)
		resolved = filepath.ToSlash(resolved)
		for _, ext := range []string{".tsx", ".ts", "/index.tsx", "/index.ts"} {
			candidate := resolved + ext
			if key, ok := relToKey[candidate]; ok {
				return key
			}
		}
		return ""
	}

	return ""
}

// wireComponentDeps parses imports in each component file and creates contains relationships.
func wireComponentDeps(
	ctx context.Context,
	g *sdkgraph.Client,
	absRoot string,
	diskComponents []componentRecord,
	relToKey map[string]string,
	graphByKey map[string]*sdkgraph.GraphObject,
	verbose bool,
) (int, int, int) {
	// Fetch existing contains relationships to avoid duplicates
	existingRels := map[string]bool{}
	rels, _ := listAllRelationships(ctx, g, "contains")
	for _, r := range rels {
		existingRels[r.SrcID+":"+r.DstID] = true
	}

	// Read glob from config for alias resolution
	yml := config.LoadYML()
	glob := ""
	if yml != nil {
		glob = yml.Sync.Components.Glob
	}

	var toWire []sdkgraph.CreateRelationshipRequest
	unresolved := 0

	for _, comp := range diskComponents {
		srcObj, ok := graphByKey[comp.key]
		if !ok {
			continue
		}

		absPath := filepath.Join(absRoot, comp.rel)
		imports := parseComponentImports(absPath)

		for _, imp := range imports {
			dstKey := resolveImportToKey(imp, comp.rel, relToKey, absRoot, glob)
			if dstKey == "" {
				// Only count as unresolved if it looks like an internal component import
				if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "shared-web/components") {
					unresolved++
				}
				continue
			}
			dstObj, ok := graphByKey[dstKey]
			if !ok {
				unresolved++
				continue
			}
			// Skip self-references
			if srcObj.EntityID == dstObj.EntityID {
				continue
			}
			relKey := srcObj.EntityID + ":" + dstObj.EntityID
			if existingRels[relKey] {
				continue
			}
			existingRels[relKey] = true // mark to avoid duplicates within this run
			if verbose {
				fmt.Printf("  %s → %s\n", comp.key, dstKey)
			}
			toWire = append(toWire, sdkgraph.CreateRelationshipRequest{
				Type:   "contains",
				SrcID:  srcObj.EntityID,
				DstID:  dstObj.EntityID,
				Upsert: true,
			})
		}
	}

	fmt.Printf("  Found %d component dependency edges to wire\n", len(toWire))

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

func batchCreateComponents(ctx context.Context, g *sdkgraph.Client, comps []componentRecord) (int, int) {
	const batchSize = 100
	created, failed := 0, 0
	for i := 0; i < len(comps); i += batchSize {
		end := i + batchSize
		if end > len(comps) {
			end = len(comps)
		}
		batch := comps[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, comp := range batch {
			key := comp.key
			objType := comp.info.objectType
			if objType == "" {
				objType = "UIComponent"
			}
			items = append(items, sdkgraph.CreateObjectRequest{
				Type: objType,
				Key:  &key,
				Properties: map[string]any{
					"name":        comp.info.name,
					"type":        comp.info.compType,
					"description": comp.info.description,
				},
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
