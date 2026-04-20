package synccmd

// sync scenarios — reads a scenarios definition YAML and creates Scenario +
// ScenarioStep objects in the graph.
//
// The scenarios YAML is a human/AI-authored file that lives in the repo
// (e.g. .codebase/scenarios.yml). It describes user flows in a structured
// format that maps directly to the graph schema.
//
// Use --discover to scan the codebase and generate a starter YAML from
// router files and store state machines.
//
// Key naming:
//   Scenario:     scn-<domain>-<slug>
//   ScenarioStep: step-<domain>-<slug>-<n>

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
	"gopkg.in/yaml.v3"
)

// ─── YAML schema ────────────────────────────────────────────────────────────

// ScenariosDef is the top-level structure of the scenarios definition YAML.
type ScenariosDef struct {
	Scenarios []ScenarioDef `yaml:"scenarios"`
}

// ScenarioDef defines a single scenario.
type ScenarioDef struct {
	Key         string    `yaml:"key"`    // e.g. scn-meeting-create
	Domain      string    `yaml:"domain"` // e.g. meeting
	Title       string    `yaml:"title"`  // e.g. "Create Meeting"
	Given       string    `yaml:"given"`  // precondition
	When        string    `yaml:"when"`   // triggering action
	Then        string    `yaml:"then"`   // expected outcome
	Description string    `yaml:"description"`
	Steps       []StepDef `yaml:"steps"`
}

// StepDef defines a single step within a scenario.
type StepDef struct {
	Name        string `yaml:"name"` // e.g. "Select meeting type"
	Description string `yaml:"description"`
	ContextKey  string `yaml:"context_key"` // optional: key of existing Context object
}

// ─── Command ─────────────────────────────────────────────────────────────────

type scenariosOptions struct {
	repo     string
	file     string
	discover bool
	router   string
	store    string
	sync     bool
	verbose  bool
}

func newScenariosCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	opts := &scenariosOptions{}
	cwd, _ := os.Getwd()

	cmd := &cobra.Command{
		Use:   "scenarios",
		Short: "Sync Scenario and ScenarioStep objects from a definition file",
		Long: `Reads a scenarios YAML definition and creates Scenario + ScenarioStep objects.

The scenarios YAML lives in your repo (e.g. .codebase/scenarios.yml) and
describes user flows. Use --discover to generate a starter YAML from your
router and store files, then refine it manually.

Configure in .codebase.yml:
  sync:
    scenarios:
      file: .codebase/scenarios.yml
      router_file: apps/web/src/App.tsx   # for --discover
      store_glob: apps/web/src/store/**/*.ts

Workflow:
  1. codebase sync scenarios --discover   # generate starter YAML
  2. Edit .codebase/scenarios.yml         # refine scenarios and steps
  3. codebase sync scenarios --sync       # push to graph`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScenarios(opts, flagProjectID, flagBranch, flagFormat)
		},
	}

	cmd.Flags().StringVar(&opts.repo, "repo", cwd, "Path to repository root")
	cmd.Flags().StringVar(&opts.file, "file", "", "Path to scenarios YAML (relative to repo root)")
	cmd.Flags().BoolVar(&opts.discover, "discover", false, "Scan codebase and generate starter scenarios YAML")
	cmd.Flags().StringVar(&opts.router, "router", "", "Router file for --discover (relative to repo root)")
	cmd.Flags().StringVar(&opts.store, "store", "", "Store glob for --discover (relative to repo root)")
	cmd.Flags().BoolVar(&opts.sync, "sync", false, "Create/update Scenario and ScenarioStep objects in graph")
	cmd.Flags().BoolVar(&opts.verbose, "verbose", false, "Print all steps in the summary")

	return cmd
}

func runScenarios(opts *scenariosOptions, flagProjectID *string, flagBranch *string, flagFormat *string) error {
	yml := config.LoadYML()
	absRoot, err := filepath.Abs(opts.repo)
	if err != nil {
		return fmt.Errorf("resolving root: %w", err)
	}

	// Resolve config from .codebase.yml
	scenFile := opts.file
	routerFile := opts.router
	storeGlob := opts.store
	if yml != nil {
		if scenFile == "" {
			scenFile = yml.Sync.Scenarios.File
		}
		if routerFile == "" {
			routerFile = yml.Sync.Scenarios.RouterFile
		}
		if storeGlob == "" {
			storeGlob = yml.Sync.Scenarios.StoreGlob
		}
	}
	if scenFile == "" {
		scenFile = ".codebase/scenarios.yml"
	}

	absScenFile := filepath.Join(absRoot, scenFile)

	// --discover: generate starter YAML
	if opts.discover {
		return runDiscover(absRoot, absScenFile, routerFile, storeGlob)
	}

	// Load scenarios YAML
	data, err := os.ReadFile(absScenFile)
	if err != nil {
		return fmt.Errorf("reading scenarios file %s: %w\n\nRun with --discover to generate a starter file.", absScenFile, err)
	}
	var def ScenariosDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return fmt.Errorf("parsing scenarios YAML: %w", err)
	}
	fmt.Printf("→ Loaded %d scenarios from %s\n", len(def.Scenarios), scenFile)

	c, err := config.New(*flagProjectID, *flagBranch)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Fetch existing Scenario and ScenarioStep objects
	fmt.Println("→ Fetching existing Scenario objects from graph...")
	existingScenarios, err := listAllObjects(ctx, c.Graph, "Scenario")
	if err != nil {
		return fmt.Errorf("fetching scenarios: %w", err)
	}
	scenByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range existingScenarios {
		if derefKey(obj.Key) != "" {
			scenByKey[derefKey(obj.Key)] = obj
		}
	}

	existingSteps, err := listAllObjects(ctx, c.Graph, "ScenarioStep")
	if err != nil {
		return fmt.Errorf("fetching steps: %w", err)
	}
	stepByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range existingSteps {
		if derefKey(obj.Key) != "" {
			stepByKey[derefKey(obj.Key)] = obj
		}
	}

	// Diff
	var toCreateScen, toUpdateScen, upToDateScen []scenRecord
	for _, s := range def.Scenarios {
		key := s.Key
		if key == "" {
			key = scenarioKey(s.Domain, s.Title)
			s.Key = key
		}
		rec := scenRecord{def: s, key: key}
		if obj, exists := scenByKey[key]; exists {
			if needsScenarioUpdate(obj, s) {
				toUpdateScen = append(toUpdateScen, rec)
			} else {
				upToDateScen = append(upToDateScen, rec)
			}
		} else {
			toCreateScen = append(toCreateScen, rec)
		}
	}

	// Count steps
	totalSteps := 0
	newSteps := 0
	for _, s := range def.Scenarios {
		key := s.Key
		if key == "" {
			key = scenarioKey(s.Domain, s.Title)
		}
		for i, step := range s.Steps {
			totalSteps++
			stepKey := stepKey(key, i+1, step.Name)
			if _, exists := stepByKey[stepKey]; !exists {
				newSteps++
			}
		}
	}

	printHeader("SCENARIOS SYNC STATUS")
	fmt.Printf("  Defined scenarios : %d\n", len(def.Scenarios))
	fmt.Printf("  Graph scenarios   : %d\n", len(scenByKey))
	fmt.Printf("  To create         : %d\n", len(toCreateScen))
	fmt.Printf("  To update         : %d\n", len(toUpdateScen))
	fmt.Printf("  Up to date        : %d\n", len(upToDateScen))
	fmt.Printf("  Total steps       : %d\n", totalSteps)
	fmt.Printf("  New steps         : %d\n", newSteps)

	if opts.verbose || len(def.Scenarios) <= 20 {
		fmt.Println()
		printHeader("SCENARIOS")
		for _, s := range def.Scenarios {
			key := s.Key
			if key == "" {
				key = scenarioKey(s.Domain, s.Title)
			}
			status := "+"
			if _, exists := scenByKey[key]; exists {
				status = "~"
			}
			fmt.Printf("  %s [%s] %s (%d steps)\n", status, s.Domain, s.Title, len(s.Steps))
			if opts.verbose {
				for i, step := range s.Steps {
					fmt.Printf("      %d. %s\n", i+1, step.Name)
				}
			}
		}
	}

	if !opts.sync {
		if len(toCreateScen) > 0 || len(toUpdateScen) > 0 || newSteps > 0 {
			fmt.Println("\nRun with --sync to apply changes.")
		}
		return nil
	}

	fmt.Println()
	printHeader("SYNCING")

	// Create/update scenarios
	if len(toCreateScen) > 0 {
		fmt.Printf("Creating %d Scenario objects...\n", len(toCreateScen))
		created, failed := batchCreateScenarios(ctx, c.Graph, toCreateScen)
		fmt.Printf("  Created: %d  Failed: %d\n", created, failed)
	}
	if len(toUpdateScen) > 0 {
		fmt.Printf("Updating %d Scenario objects...\n", len(toUpdateScen))
		updated, failed := updateScenarios(ctx, c.Graph, toUpdateScen, scenByKey)
		fmt.Printf("  Updated: %d  Failed: %d\n", updated, failed)
	}

	// Re-fetch scenarios to get IDs for step relationships
	updatedScenarios, err := listAllObjects(ctx, c.Graph, "Scenario")
	if err != nil {
		return fmt.Errorf("re-fetching scenarios: %w", err)
	}
	scenByKeyFresh := map[string]*sdkgraph.GraphObject{}
	for _, obj := range updatedScenarios {
		if derefKey(obj.Key) != "" {
			scenByKeyFresh[derefKey(obj.Key)] = obj
		}
	}

	// Create steps
	if newSteps > 0 {
		fmt.Printf("Creating %d ScenarioStep objects...\n", newSteps)
		created, failed := createSteps(ctx, c.Graph, def.Scenarios, stepByKey, scenByKeyFresh)
		fmt.Printf("  Created: %d  Failed: %d\n", created, failed)
	}

	// Re-fetch all steps (including newly created) for relationship wiring
	allSteps, err := listAllObjects(ctx, c.Graph, "ScenarioStep")
	if err != nil {
		return fmt.Errorf("re-fetching steps: %w", err)
	}
	stepByKeyFresh := map[string]*sdkgraph.GraphObject{}
	for _, obj := range allSteps {
		if derefKey(obj.Key) != "" {
			stepByKeyFresh[derefKey(obj.Key)] = obj
		}
	}

	// Fetch all Context objects for occurs_in wiring
	fmt.Println("→ Fetching Context objects for relationship wiring...")
	allContexts, err := listAllObjects(ctx, c.Graph, "Context")
	if err != nil {
		return fmt.Errorf("fetching contexts: %w", err)
	}
	ctxByKey := map[string]*sdkgraph.GraphObject{}
	for _, obj := range allContexts {
		if derefKey(obj.Key) != "" {
			ctxByKey[derefKey(obj.Key)] = obj
		}
	}

	// Wire relationships: has_step (Scenario→ScenarioStep) and occurs_in (ScenarioStep→Context)
	fmt.Println("→ Wiring relationships (has_step, occurs_in)...")
	wired, skipped, missing := wireRelationships(ctx, c.Graph, def.Scenarios, scenByKeyFresh, stepByKeyFresh, ctxByKey)
	fmt.Printf("  Wired: %d  Skipped (exists): %d  Missing context: %d\n", wired, skipped, missing)

	fmt.Println("\nSync complete.")
	return nil
}

// ─── Graph helpers ────────────────────────────────────────────────────────────

func scenarioKey(domain, title string) string {
	slug := slugify(strings.ReplaceAll(title, " ", "-"))
	if domain != "" {
		return "scn-" + slugify(domain) + "-" + slug
	}
	return "scn-" + slug
}

func stepKey(scenKey string, order int, name string) string {
	slug := slugify(strings.ReplaceAll(name, " ", "-"))
	return fmt.Sprintf("%s-step-%d-%s", scenKey, order, slug)
}

func needsScenarioUpdate(obj *sdkgraph.GraphObject, def ScenarioDef) bool {
	return strProp(obj, "title") != def.Title ||
		strProp(obj, "given") != def.Given ||
		strProp(obj, "when") != def.When ||
		strProp(obj, "then") != def.Then
}

type scenRecord struct {
	def ScenarioDef
	key string
}

func batchCreateScenarios(ctx context.Context, g *sdkgraph.Client, recs []scenRecord) (int, int) {
	const batchSize = 50
	created, failed := 0, 0
	for i := 0; i < len(recs); i += batchSize {
		end := i + batchSize
		if end > len(recs) {
			end = len(recs)
		}
		batch := recs[i:end]
		items := make([]sdkgraph.CreateObjectRequest, 0, len(batch))
		for _, r := range batch {
			key := r.key
			props := map[string]any{
				"name":        r.def.Title,
				"title":       r.def.Title,
				"description": r.def.Description,
			}
			if r.def.Given != "" {
				props["given"] = r.def.Given
			}
			if r.def.When != "" {
				props["when"] = r.def.When
			}
			if r.def.Then != "" {
				props["then"] = r.def.Then
			}
			items = append(items, sdkgraph.CreateObjectRequest{
				Type:       "Scenario",
				Key:        &key,
				Properties: props,
				Labels:     []string{r.def.Domain},
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

func updateScenarios(ctx context.Context, g *sdkgraph.Client, recs []scenRecord, byKey map[string]*sdkgraph.GraphObject) (int, int) {
	updated, failed := 0, 0
	for _, r := range recs {
		obj, ok := byKey[r.key]
		if !ok {
			failed++
			continue
		}
		props := map[string]any{
			"title":       r.def.Title,
			"description": r.def.Description,
		}
		if r.def.Given != "" {
			props["given"] = r.def.Given
		}
		if r.def.When != "" {
			props["when"] = r.def.When
		}
		if r.def.Then != "" {
			props["then"] = r.def.Then
		}
		if _, err := g.UpdateObject(ctx, obj.EntityID, &sdkgraph.UpdateObjectRequest{Properties: props}); err != nil {
			failed++
		} else {
			updated++
		}
	}
	return updated, failed
}

func createSteps(ctx context.Context, g *sdkgraph.Client, scenarios []ScenarioDef, existingSteps, scenByKey map[string]*sdkgraph.GraphObject) (int, int) {
	const batchSize = 100
	created, failed := 0, 0

	var items []sdkgraph.CreateObjectRequest
	for _, s := range scenarios {
		scenKey := s.Key
		if scenKey == "" {
			scenKey = scenarioKey(s.Domain, s.Title)
		}
		for i, step := range s.Steps {
			sk := stepKey(scenKey, i+1, step.Name)
			if _, exists := existingSteps[sk]; exists {
				continue
			}
			key := sk
			props := map[string]any{
				"name":        step.Name,
				"order":       i + 1,
				"description": step.Description,
			}
			items = append(items, sdkgraph.CreateObjectRequest{
				Type:       "ScenarioStep",
				Key:        &key,
				Properties: props,
			})
		}
	}

	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		resp, err := g.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: items[i:end]})
		if err != nil {
			failed += end - i
			continue
		}
		created += resp.Success
		failed += resp.Failed
	}
	return created, failed
}

// wireRelationships creates has_step (Scenario→ScenarioStep) and occurs_in
// (ScenarioStep→Context) relationships using upsert semantics so it is safe
// to run repeatedly.
//
// Returns (wired, skipped, missingContext) counts.
func wireRelationships(
	ctx context.Context,
	g *sdkgraph.Client,
	scenarios []ScenarioDef,
	scenByKey map[string]*sdkgraph.GraphObject,
	stepByKey map[string]*sdkgraph.GraphObject,
	ctxByKey map[string]*sdkgraph.GraphObject,
) (int, int, int) {
	const batchSize = 100

	var rels []sdkgraph.CreateRelationshipRequest
	missingCtx := 0

	for _, s := range scenarios {
		scenKey := s.Key
		if scenKey == "" {
			scenKey = scenarioKey(s.Domain, s.Title)
		}
		scenObj, ok := scenByKey[scenKey]
		if !ok {
			continue
		}

		for i, step := range s.Steps {
			sk := stepKey(scenKey, i+1, step.Name)
			stepObj, ok := stepByKey[sk]
			if !ok {
				continue
			}

			// has_step: Scenario → ScenarioStep
			rels = append(rels, sdkgraph.CreateRelationshipRequest{
				Type:   "has_step",
				SrcID:  scenObj.EntityID,
				DstID:  stepObj.EntityID,
				Upsert: true,
			})

			// occurs_in: ScenarioStep → Context (if context_key provided)
			if step.ContextKey != "" {
				ctxObj, ok := ctxByKey[step.ContextKey]
				if !ok {
					missingCtx++
				} else {
					rels = append(rels, sdkgraph.CreateRelationshipRequest{
						Type:   "occurs_in",
						SrcID:  stepObj.EntityID,
						DstID:  ctxObj.EntityID,
						Upsert: true,
					})
				}
			}
		}
	}

	wired := 0
	skipped := 0
	for i := 0; i < len(rels); i += batchSize {
		end := i + batchSize
		if end > len(rels) {
			end = len(rels)
		}
		resp, err := g.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: rels[i:end]})
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
	return wired, skipped, missingCtx
}

// ─── Discover mode ────────────────────────────────────────────────────────────

func runDiscover(absRoot, outFile, routerFile, storeGlob string) error {
	fmt.Println("→ Discovering scenarios from codebase...")

	var scenarios []ScenarioDef

	// Parse router file for route groups
	if routerFile != "" {
		absRouter := filepath.Join(absRoot, routerFile)
		routeScenarios := discoverFromRouter(absRouter)
		scenarios = append(scenarios, routeScenarios...)
		fmt.Printf("  Found %d route-based scenarios from %s\n", len(routeScenarios), routerFile)
	}

	// Parse store files for state machine flows
	if storeGlob != "" {
		storeScenarios := discoverFromStores(absRoot, storeGlob)
		scenarios = append(scenarios, storeScenarios...)
		fmt.Printf("  Found %d store-based scenarios\n", len(storeScenarios))
	}

	if len(scenarios) == 0 {
		fmt.Println("  No scenarios discovered. Add router_file and store_glob to .codebase.yml sync.scenarios config.")
		return nil
	}

	// Deduplicate by key
	seen := map[string]bool{}
	var unique []ScenarioDef
	for _, s := range scenarios {
		if !seen[s.Key] {
			seen[s.Key] = true
			unique = append(unique, s)
		}
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].Domain != unique[j].Domain {
			return unique[i].Domain < unique[j].Domain
		}
		return unique[i].Title < unique[j].Title
	})

	def := ScenariosDef{Scenarios: unique}
	data, err := yaml.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshaling YAML: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	header := "# Scenarios definition — generated by `codebase sync scenarios --discover`\n" +
		"# Edit this file to refine scenarios and steps, then run:\n" +
		"#   codebase sync scenarios --sync\n\n"

	if err := os.WriteFile(outFile, append([]byte(header), data...), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outFile, err)
	}

	fmt.Printf("\n✓ Generated %d scenarios → %s\n", len(unique), outFile)
	fmt.Println("  Review and edit the file, then run: codebase sync scenarios --sync")
	return nil
}

// discoverFromRouter parses a React Router file and extracts route groups as scenarios.
func discoverFromRouter(routerFile string) []ScenarioDef {
	f, err := os.Open(routerFile)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Match path: "/foo/:bar" or path: '/foo'
	pathRe := regexp.MustCompile(`path:\s*['"]([^'"]+)['"]`)
	// Match component/element: SomeView or <SomeView
	elemRe := regexp.MustCompile(`(?:element|component):\s*(?:<)?([A-Z][a-zA-Z]+)`)

	type routeEntry struct {
		path    string
		element string
	}
	var routes []routeEntry

	scanner := bufio.NewScanner(f)
	var currentPath string
	for scanner.Scan() {
		line := scanner.Text()
		if m := pathRe.FindStringSubmatch(line); m != nil {
			currentPath = m[1]
		}
		if m := elemRe.FindStringSubmatch(line); m != nil && currentPath != "" {
			routes = append(routes, routeEntry{path: currentPath, element: m[1]})
			currentPath = ""
		}
	}

	// Group routes by top-level domain segment
	domainRoutes := map[string][]routeEntry{}
	for _, r := range routes {
		parts := strings.Split(strings.TrimPrefix(r.path, "/"), "/")
		domain := parts[0]
		if domain == "" || strings.HasPrefix(domain, ":") {
			domain = "general"
		}
		domainRoutes[domain] = append(domainRoutes[domain], r)
	}

	var scenarios []ScenarioDef
	for domain, routes := range domainRoutes {
		if len(routes) == 0 {
			continue
		}
		// Create one scenario per domain with steps for each route
		title := toTitle(domain) + " Navigation"
		key := scenarioKey(domain, title)
		var steps []StepDef
		for _, r := range routes {
			steps = append(steps, StepDef{
				Name:        "View " + toTitle(strings.ReplaceAll(r.element, "View", "")),
				Description: "Navigate to " + r.path,
				ContextKey:  "",
			})
		}
		scenarios = append(scenarios, ScenarioDef{
			Key:         key,
			Domain:      domain,
			Title:       title,
			Given:       "user is authenticated",
			When:        "user navigates to " + domain + " section",
			Then:        "user can access " + domain + " functionality",
			Description: "Navigation flow for " + domain + " domain",
			Steps:       steps,
		})
	}
	return scenarios
}

// discoverFromStores parses MobX store files for state machine patterns.
func discoverFromStores(absRoot, glob string) []ScenarioDef {
	// Look for status/state enums and transition methods
	statusRe := regexp.MustCompile(`(?i)(status|state|phase)\s*[=:]\s*['"]([A-Z_]+)['"]`)
	transitionRe := regexp.MustCompile(`(?i)(start|finish|complete|submit|cancel|approve|reject|sign|create|update|delete)\w*\s*[=(]`)

	type storeFlow struct {
		store    string
		statuses []string
		actions  []string
	}
	var flows []storeFlow

	_ = walkGlob(absRoot, glob, func(rel string) {
		base := filepath.Base(rel)
		if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
			return
		}
		f, err := os.Open(filepath.Join(absRoot, rel))
		if err != nil {
			return
		}
		defer f.Close()

		storeName := storeNameFromPath(rel)
		var statuses, actions []string
		seenStatus := map[string]bool{}
		seenAction := map[string]bool{}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if m := statusRe.FindStringSubmatch(line); m != nil {
				s := m[2]
				if !seenStatus[s] {
					seenStatus[s] = true
					statuses = append(statuses, s)
				}
			}
			if m := transitionRe.FindStringSubmatch(line); m != nil {
				a := strings.TrimRight(m[0], " =(")
				a = strings.TrimSpace(a)
				if !seenAction[a] && len(a) > 3 {
					seenAction[a] = true
					actions = append(actions, a)
				}
			}
		}

		if len(statuses) >= 2 || len(actions) >= 2 {
			flows = append(flows, storeFlow{store: storeName, statuses: statuses, actions: actions})
		}
	})

	var scenarios []ScenarioDef
	for _, flow := range flows {
		domain := flow.store
		// Build steps from state transitions
		var steps []StepDef
		if len(flow.statuses) >= 2 {
			// State machine: each status is a step
			for i, status := range flow.statuses {
				if i >= 8 {
					break
				}
				steps = append(steps, StepDef{
					Name:        toTitle(strings.ToLower(strings.ReplaceAll(status, "_", " "))),
					Description: "System is in " + status + " state",
				})
			}
		} else {
			// Action-based: each transition action is a step
			for i, action := range flow.actions {
				if i >= 6 {
					break
				}
				steps = append(steps, StepDef{
					Name:        camelToTitle(action),
					Description: "User performs " + camelToTitle(action),
				})
			}
		}

		if len(steps) < 2 {
			continue
		}

		title := toTitle(domain) + " Lifecycle"
		key := scenarioKey(domain, title)
		scenarios = append(scenarios, ScenarioDef{
			Key:         key,
			Domain:      domain,
			Title:       title,
			Given:       "user has access to " + domain,
			When:        "user initiates " + domain + " workflow",
			Then:        domain + " progresses through its lifecycle",
			Description: "Lifecycle flow for " + domain + " domain",
			Steps:       steps,
		})
	}
	return scenarios
}
