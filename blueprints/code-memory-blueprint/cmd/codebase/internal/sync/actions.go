package synccmd

// sync actions — extracts store/action methods as Action graph objects.
//
// Strategy:
//   - Walk the glob pattern for store files (e.g. apps/web/src/store/**/*.ts)
//   - Parse exported async functions, class methods, and arrow function assignments
//     that look like actions (not getters/computed)
//   - Infer action type from name prefix (navigate*, fetch*/load* → navigation/mutation/trigger)
//   - Create Action objects in the graph
//
// Key naming: act-<store-slug>-<method-slug>

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

type actionsOptions struct {
	repo    string
	glob    string
	pattern string
	sync    bool
	verbose bool
	defines bool
}

func newActionsCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	opts := &actionsOptions{}
	cwd, _ := os.Getwd()

	cmd := &cobra.Command{
		Use:   "actions",
		Short: "Sync store action methods as Action objects",
		Long: `Walks store files matching a glob pattern and creates Action objects in the graph.

Each exported function or class method becomes an Action with:
  type:          inferred from name (navigation/mutation/trigger/toggle/external)
  display_label: derived from method name
  description:   derived from store file and method name

Configure in .codebase.yml:
  sync:
    actions:
      glob: apps/web/src/store/**/*.ts
      pattern: mobx   # mobx | redux | zustand (default: mobx)

Or pass flags directly:
  codebase sync actions --glob "apps/web/src/store/**/*.ts" --sync`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runActions(opts, flagProjectID, flagBranch, flagFormat)
		},
	}

	cmd.Flags().StringVar(&opts.repo, "repo", cwd, "Path to repository root")
	cmd.Flags().StringVar(&opts.glob, "glob", "", "Glob pattern for store files (relative to repo root)")
	cmd.Flags().StringVar(&opts.pattern, "pattern", "", "Store pattern: mobx, redux, zustand (default: mobx)")
	cmd.Flags().BoolVar(&opts.sync, "sync", false, "Create missing Action objects")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "Print every action in the summary table")
	cmd.Flags().BoolVar(&opts.defines, "defines", false, "Wire defines relationships from SourceFile to Action objects")

	return cmd
}

// actionInfo holds extracted metadata for a store action.
type actionInfo struct {
	name        string // method name, e.g. "fetchMeetings"
	displayName string // human label, e.g. "Fetch Meetings"
	actionType  string // navigation/mutation/trigger/toggle/external
	description string
	store       string // store name, e.g. "meetings-store"
}

// actionRecord is an action with its graph key and extracted info.
type actionRecord struct {
	key  string
	info *actionInfo
}

func runActions(opts *actionsOptions, flagProjectID *string, flagBranch *string, flagFormat *string) error {
	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}

	yml := config.LoadYML()
	glob := opts.glob
	pattern := opts.pattern
	if yml != nil {
		if glob == "" {
			glob = yml.Sync.Actions.Glob
		}
		if pattern == "" {
			pattern = yml.Sync.Actions.Pattern
		}
	}
	if glob == "" {
		return fmt.Errorf("no glob pattern configured — set sync.actions.glob in .codebase.yml or pass --glob")
	}
	if pattern == "" {
		pattern = "mobx"
	}

	absRoot, err := filepath.Abs(opts.repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Printf("→ Scanning store files with glob: %s (pattern: %s)\n", glob, pattern)
	var diskActions []actionRecord
	actionsByFile := map[string][]actionRecord{}
	if err := walkGlob(absRoot, glob, func(rel string) {
		base := filepath.Base(rel)
		// Skip test/spec/index files
		if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
			base == "index.ts" || base == "index.tsx" {
			return
		}
		actions := extractActions(filepath.Join(absRoot, rel), rel, pattern)
		diskActions = append(diskActions, actions...)
		if len(actions) > 0 {
			actionsByFile[rel] = append(actionsByFile[rel], actions...)
		}
	}); err != nil {
		return fmt.Errorf("walking store files: %w", err)
	}
	sort.Slice(diskActions, func(i, j int) bool { return diskActions[i].key < diskActions[j].key })
	fmt.Printf("  %d actions found\n", len(diskActions))

	fmt.Println("→ Fetching existing Action objects from graph...")
	graphObjs, err := listAllObjects(ctx, c.Graph, "Action")
	if err != nil {
		return fmt.Errorf("fetching actions: %w", err)
	}
	graphByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range graphObjs {
		if derefKey(obj.Key) != "" {
			graphByKey[derefKey(obj.Key)] = obj
		}
	}

	var toCreate, upToDate []actionRecord
	for _, a := range diskActions {
		if _, exists := graphByKey[a.key]; exists {
			upToDate = append(upToDate, a)
		} else {
			toCreate = append(toCreate, a)
		}
	}

	printHeader("ACTIONS SYNC STATUS")
	fmt.Printf("  Disk actions    : %d\n", len(diskActions))
	fmt.Printf("  Graph objects   : %d\n", len(graphByKey))
	fmt.Printf("  To create       : %d\n", len(toCreate))
	fmt.Printf("  Up to date      : %d\n", len(upToDate))

	if opts.verbose {
		fmt.Println()
		printHeader("ALL ACTIONS")
		fmt.Printf("  %-40s  %-15s  %s\n", "Key", "Type", "Store")
		fmt.Printf("  %-40s  %-15s  %s\n", strings.Repeat("─", 40), strings.Repeat("─", 15), strings.Repeat("─", 25))
		for _, a := range diskActions {
			fmt.Printf("  %-40s  %-15s  %s\n", truncate(a.key, 40), a.info.actionType, a.info.store)
		}
	}

	if len(toCreate) > 0 {
		fmt.Println()
		printHeader(fmt.Sprintf("TO CREATE (%d)", len(toCreate)))
		for _, a := range toCreate {
			fmt.Printf("  + %-40s  type=%s  store=%s\n", a.key, a.info.actionType, a.info.store)
		}
	}

	if !opts.sync && !opts.defines {
		if len(toCreate) > 0 {
			fmt.Println("\nRun with --sync to apply changes.")
		}
		return nil
	}

	fmt.Println()
	printHeader("SYNCING")

	if opts.sync {
		if len(toCreate) > 0 {
			fmt.Printf("Creating %d Action objects...\n", len(toCreate))
			created, failed := batchCreateActions(ctx, c.Graph, toCreate)
			fmt.Printf("  Created: %d  Failed: %d\n", created, failed)
		}
	}

	if opts.defines {
		fmt.Println()
		printHeader("WIRING SOURCEFILE → ACTION DEFINES")
		wired, skipped, missing := wireActionDefines(ctx, c.Graph, absRoot, diskActions, actionsByFile, opts.verbose)
		fmt.Printf("  Wired: %d  Skipped (exists): %d  Unresolved: %d\n", wired, skipped, missing)
	}

	fmt.Println("\nSync complete.")
	return nil
}

// Regexes for extracting action methods from store files.
var (
	// MobX/class method: "  fetchMeetings = async (" or "  fetchMeetings("
	mobxActionRe = regexp.MustCompile(`^\s{2,}((?:async\s+)?([a-zA-Z][a-zA-Z0-9_]+))\s*[=(]`)
	// Exported function: "export async function fetchMeetings(" or "export function fetchMeetings("
	exportedFnRe = regexp.MustCompile(`^export\s+(?:async\s+)?function\s+([a-zA-Z][a-zA-Z0-9_]+)\s*\(`)
	// Arrow function export: "export const fetchMeetings = async (" or "export const fetchMeetings = ("
	exportedArrowRe = regexp.MustCompile(`^export\s+const\s+([a-zA-Z][a-zA-Z0-9_]+)\s*=\s*(?:async\s*)?\(`)
	// Redux action creator: "export const fetchMeetings = createAsyncThunk("
	reduxThunkRe = regexp.MustCompile(`^export\s+const\s+([a-zA-Z][a-zA-Z0-9_]+)\s*=\s*createAsyncThunk\(`)
	// Zustand action in object: "  fetchMeetings: async (" or "  fetchMeetings: ("
	zustandActionRe = regexp.MustCompile(`^\s{2,}([a-zA-Z][a-zA-Z0-9_]+)\s*:\s*(?:async\s*)?\(`)
)

// skipMethods are exact method names to ignore (MobX internals, lifecycle, etc.)
var skipMethods = map[string]bool{
	"constructor": true, "render": true, "get": true, "set": true,
	"toString": true, "toJSON": true, "dispose": true, "init": true,
	// MobX internals
	"makeAutoObservable": true, "makeObservable": true, "runInAction": true,
	"action": true, "observable": true, "computed": true, "reaction": true,
	"autorun": true, "when": true, "flow": true, "intercept": true, "observe": true,
	// Common non-action patterns
	"async": true, "return": true, "if": true, "else": true, "for": true,
	"while": true, "switch": true, "case": true, "break": true, "continue": true,
	"try": true, "catch": true, "finally": true, "throw": true, "new": true,
	"typeof": true, "instanceof": true, "void": true, "delete": true,
}

// skipMethodPatterns are prefixes that indicate non-action methods.
var skipPrefixes = []string{"get", "is", "has", "can", "should", "compute", "select"}

func extractActions(absPath, rel, pattern string) []actionRecord {
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	storeName := storeNameFromPath(rel)
	var results []actionRecord
	seen := map[string]bool{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		var methodName string

		switch pattern {
		case "redux":
			if m := reduxThunkRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			} else if m := exportedFnRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			} else if m := exportedArrowRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			}
		case "zustand":
			if m := zustandActionRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			}
		default: // mobx
			if m := mobxActionRe.FindStringSubmatch(line); m != nil {
				methodName = m[2]
			} else if m := exportedFnRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			} else if m := exportedArrowRe.FindStringSubmatch(line); m != nil {
				methodName = m[1]
			}
		}

		if methodName == "" || seen[methodName] {
			continue
		}
		// Skip short names (likely noise: id, if, p, etc.)
		if len(methodName) < 4 {
			continue
		}
		if skipMethods[methodName] {
			continue
		}
		if isGetterMethod(methodName) {
			continue
		}
		// Skip names that are clearly not actions (single words that are common JS patterns)
		if isNoiseName(methodName) {
			continue
		}

		seen[methodName] = true
		info := &actionInfo{
			name:        methodName,
			displayName: camelToTitle(methodName),
			actionType:  inferActionType(methodName),
			description: fmt.Sprintf("%s action in %s", camelToTitle(methodName), toTitle(storeName)),
			store:       storeName,
		}
		key := actionKey(storeName, methodName)
		results = append(results, actionRecord{key: key, info: info})
	}
	return results
}

// isNoiseName returns true for names that are clearly not user-facing actions.
func isNoiseName(name string) bool {
	noiseExact := map[string]bool{
		"elem": true, "item": true, "view": true, "type": true, "place": true,
		"count": true, "response": true, "endpoint": true, "hidden": true,
		"loading": true, "loader": true, "candidate": true, "reference": true,
		"take": true, "analyze": true, "cleanup": true, "parse": true,
	}
	return noiseExact[strings.ToLower(name)]
}

func isGetterMethod(name string) bool {
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			next := name[len(prefix)]
			if next >= 'A' && next <= 'Z' {
				return true
			}
		}
	}
	return false
}

// inferActionType infers the Action type from the method name.
func inferActionType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "navigate") || strings.HasPrefix(lower, "goto") || strings.HasPrefix(lower, "redirect"):
		return "navigation"
	case strings.HasPrefix(lower, "fetch") || strings.HasPrefix(lower, "load") || strings.HasPrefix(lower, "refresh"):
		return "trigger"
	case strings.HasPrefix(lower, "create") || strings.HasPrefix(lower, "add") || strings.HasPrefix(lower, "save") ||
		strings.HasPrefix(lower, "update") || strings.HasPrefix(lower, "delete") || strings.HasPrefix(lower, "remove") ||
		strings.HasPrefix(lower, "submit") || strings.HasPrefix(lower, "send") || strings.HasPrefix(lower, "upload"):
		return "mutation"
	case strings.HasPrefix(lower, "toggle") || strings.HasPrefix(lower, "open") || strings.HasPrefix(lower, "close") ||
		strings.HasPrefix(lower, "show") || strings.HasPrefix(lower, "hide") || strings.HasPrefix(lower, "expand") ||
		strings.HasPrefix(lower, "collapse"):
		return "toggle"
	case strings.HasPrefix(lower, "handle"):
		return "trigger"
	default:
		return "trigger"
	}
}

// storeNameFromPath derives a store name from the file path.
func storeNameFromPath(rel string) string {
	base := filepath.Base(rel)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.TrimSuffix(base, "-store")
	base = strings.TrimSuffix(base, ".store")
	return base
}

// actionKey returns the graph key for an action.
func actionKey(store, method string) string {
	storeSlug := slugify(store)
	methodSlug := slugify(camelToSlug(method))
	return "act-" + storeSlug + "-" + methodSlug
}

// camelToTitle converts camelCase to "Title Case".
func camelToTitle(s string) string {
	var result []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, ' ')
		}
		result = append(result, r)
	}
	title := string(result)
	if len(title) > 0 {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	return title
}

// camelToSlug converts camelCase to kebab-case.
func camelToSlug(s string) string {
	var result []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, '-')
		}
		result = append(result, r)
	}
	return strings.ToLower(string(result))
}

func batchCreateActions(ctx context.Context, g *sdkgraph.Client, actions []actionRecord) (int, int) {
	const batchSize = 100
	created, failed := 0, 0
	for i := 0; i < len(actions); i += batchSize {
		end := i + batchSize
		if end > len(actions) {
			end = len(actions)
		}
		batch := actions[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, a := range batch {
			key := a.key
			items = append(items, sdkgraph.CreateObjectRequest{
				Type: "Action",
				Key:  &key,
				Properties: map[string]any{
					"name":          a.info.displayName,
					"type":          a.info.actionType,
					"display_label": a.info.displayName,
					"description":   a.info.description,
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

// wireActionDefines wires SourceFile → Action defines relationships.
// It groups actions by store file, fetches the matching SourceFile object,
// and creates defines relationships.
func wireActionDefines(
	ctx context.Context,
	g *sdkgraph.Client,
	absRoot string,
	diskActions []actionRecord,
	actionsByFile map[string][]actionRecord, // rel path → actions in that file
	verbose bool,
) (int, int, int) {
	// Fetch all SourceFile objects
	sfObjs, err := listAllObjects(ctx, g, "SourceFile")
	if err != nil {
		fmt.Printf("  error fetching SourceFile objects: %v\n", err)
		return 0, 0, 0
	}
	sfByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range sfObjs {
		if derefKey(obj.Key) != "" {
			sfByKey[derefKey(obj.Key)] = obj
		}
	}

	// Fetch all Action objects
	actObjs, err := listAllObjects(ctx, g, "Action")
	if err != nil {
		fmt.Printf("  error fetching Action objects: %v\n", err)
		return 0, 0, 0
	}
	actByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range actObjs {
		if derefKey(obj.Key) != "" {
			actByKey[derefKey(obj.Key)] = obj
		}
	}

	// Fetch existing defines relationships to avoid duplicates
	existingRels := map[string]bool{}
	rels, _ := listAllRelationships(ctx, g, "defines")
	for _, r := range rels {
		existingRels[r.SrcID+":"+r.DstID] = true
	}

	var toWire []sdkgraph.CreateRelationshipRequest
	unresolved := 0

	for rel, actions := range actionsByFile {
		// Convert rel path to SourceFile key: replace / . _ with -, lowercase, prefix sf-
		sfKey := relToSourceFileKey(rel)
		sfObj, ok := sfByKey[sfKey]
		if !ok {
			if verbose {
				fmt.Printf("  no SourceFile for %s (key: %s)\n", rel, sfKey)
			}
			unresolved++
			continue
		}

		for _, a := range actions {
			actObj, ok := actByKey[a.key]
			if !ok {
				unresolved++
				continue
			}
			relKey := sfObj.EntityID + ":" + actObj.EntityID
			if existingRels[relKey] {
				continue
			}
			existingRels[relKey] = true
			if verbose {
				fmt.Printf("  %s → %s\n", sfKey, a.key)
			}
			toWire = append(toWire, sdkgraph.CreateRelationshipRequest{
				Type:   "defines",
				SrcID:  sfObj.EntityID,
				DstID:  actObj.EntityID,
				Upsert: true,
			})
		}
	}

	fmt.Printf("  Found %d SourceFile→Action defines edges to wire\n", len(toWire))

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

// relToSourceFileKey converts a relative file path to a SourceFile graph key.
// e.g. "apps/web/src/store/user-store.ts" → "sf-apps-web-src-store-user-store-ts"
func relToSourceFileKey(rel string) string {
	key := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(rel)
	key = strings.ToLower(key)
	for strings.Contains(key, "--") {
		key = strings.ReplaceAll(key, "--", "-")
	}
	key = strings.Trim(key, "-")
	return "sf-" + key
}
