// Deprecated: use `codebase analyze scenarios` instead. Run `codebase --help` for details.
// scenario-context-map: per-scenario scoped view of Context→Action relationships.
//
// Problem solved: global Context→Action aggregation is noisy because many scenarios
// share the same coarse contexts (e.g. "CLI Terminal"). This tool scopes the query
// per-scenario: for each Scenario, walks its ScenarioSteps, and for each step shows
// which Context it occurs in and which Actions are linked.
//
// Traversal:
//   Scenario →[has_step]→ ScenarioStep →[occurs_in]→ Context
//                                       →[has_action]→ Action
//
// Usage:
//
//	MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... go run ./...
//	  --domain <slug>       filter to scenarios in one domain (key prefix match)
//	  --scenario <key>      filter to one scenario by key
//	  --context <key>       filter to scenarios that use a specific context
//	  --format tree|json|csv|summary  (default: tree)
//	  --show-empty          include steps with no action (default: hidden)
//	  --min-steps N         only show scenarios with >= N steps (default: 0)
//	  --no-action-only      only show scenarios where ALL steps have no action
package main

import (
	"context"
	"encoding/csv"
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
)

var (
	flagDomain      = flag.String("domain", "", "Filter by domain slug (key prefix, e.g. 'agents')")
	flagScenario    = flag.String("scenario", "", "Filter to one scenario by key")
	flagContext     = flag.String("context", "", "Filter to scenarios using a specific context key")
	flagFormat      = flag.String("format", "tree", "Output format: tree, json, csv, summary")
	flagShowEmpty   = flag.Bool("show-empty", false, "Include steps with no action")
	flagMinSteps    = flag.Int("min-steps", 0, "Only show scenarios with >= N steps")
	flagNoActOnly   = flag.Bool("no-action-only", false, "Only show scenarios where ALL steps have no action")
)

// ── data model ────────────────────────────────────────────────────────────────

type StepView struct {
	StepKey     string   `json:"step_key"`
	StepName    string   `json:"step_name"`
	StepOrder   int      `json:"step_order"`
	ContextKey  string   `json:"context_key"`
	ContextName string   `json:"context_name"`
	ContextType string   `json:"context_type"`
	Actions     []string `json:"actions"` // action labels
}

type ScenarioView struct {
	Key    string     `json:"key"`
	Name   string     `json:"name"`
	Domain string     `json:"domain"`
	Status string     `json:"status"`
	Steps  []StepView `json:"steps"`
}

type Report struct {
	Generated string         `json:"generated"`
	Scenarios []ScenarioView `json:"scenarios"`
	Stats     Stats          `json:"stats"`
}

type Stats struct {
	TotalScenarios    int `json:"total_scenarios"`
	TotalSteps        int `json:"total_steps"`
	StepsWithAction   int `json:"steps_with_action"`
	StepsWithContext  int `json:"steps_with_context"`
	StepsNoAction     int `json:"steps_no_action"`
	UniqueContexts    int `json:"unique_contexts"`
}

// ── main ──────────────────────────────────────────────────────────────────────

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

	fmt.Fprintln(os.Stderr, "→ Fetching graph data...")

	var (
		scenarios  []*sdkgraph.GraphObject
		steps      []*sdkgraph.GraphObject
		contexts   []*sdkgraph.GraphObject
		actions    []*sdkgraph.GraphObject
		hasStep    []*sdkgraph.GraphRelationship
		occursIn   []*sdkgraph.GraphRelationship
		hasAction  []*sdkgraph.GraphRelationship
		mu         sync.Mutex
		wg         sync.WaitGroup
		fetchErr   error
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

	fetch(func() error { r, e := listAll(ctx, client.Graph, "Scenario"); scenarios = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "ScenarioStep"); steps = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "Context"); contexts = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "Action"); actions = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "has_step"); hasStep = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "occurs_in"); occursIn = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "has_action"); hasAction = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	fmt.Fprintf(os.Stderr, "  Scenarios:%d Steps:%d Contexts:%d Actions:%d has_step:%d occurs_in:%d has_action:%d\n",
		len(scenarios), len(steps), len(contexts), len(actions), len(hasStep), len(occursIn), len(hasAction))

	// ── Build indexes ─────────────────────────────────────────────────────────

	stepByID := map[string]*sdkgraph.GraphObject{}
	for _, s := range steps {
		stepByID[s.EntityID] = s
	}

	ctxByID := map[string]*sdkgraph.GraphObject{}
	for _, c := range contexts {
		ctxByID[c.EntityID] = c
	}

	actByID := map[string]*sdkgraph.GraphObject{}
	for _, a := range actions {
		actByID[a.EntityID] = a
	}

	// scenarioID → []stepID (from has_step rels)
	scenToSteps := map[string][]string{}
	for _, r := range hasStep {
		scenToSteps[r.SrcID] = append(scenToSteps[r.SrcID], r.DstID)
	}

	// stepID → contextID (from occurs_in rels; one step → one context)
	stepToCtx := map[string]string{}
	for _, r := range occursIn {
		stepToCtx[r.SrcID] = r.DstID
	}

	// stepID → []actionID (from has_action rels)
	stepToActions := map[string][]string{}
	for _, r := range hasAction {
		stepToActions[r.SrcID] = append(stepToActions[r.SrcID], r.DstID)
	}

	// ── Apply context filter: collect scenario IDs that use the target context ─
	var contextFilterIDs map[string]bool
	if *flagContext != "" {
		contextFilterIDs = map[string]bool{}
		// Find context by key
		for _, c := range contexts {
			if derefStr(c.Key) == *flagContext {
				// Find all steps that occur_in this context
				for _, r := range occursIn {
					if r.DstID == c.EntityID {
						// Find all scenarios that have this step
						for _, r2 := range hasStep {
							if r2.DstID == r.SrcID {
								contextFilterIDs[r2.SrcID] = true
							}
						}
					}
				}
				break
			}
		}
		if len(contextFilterIDs) == 0 {
			fmt.Fprintf(os.Stderr, "  Warning: no scenarios found using context key %q\n", *flagContext)
		}
	}

	// ── Build scenario views ──────────────────────────────────────────────────
	var views []ScenarioView
	usedContexts := map[string]bool{}

	for _, sc := range scenarios {
		scKey := derefStr(sc.Key)
		scName := sp(sc, "name")
		scDomain := sp(sc, "domain")
		if scDomain == "" {
			scDomain = domainFromKey(scKey)
		}
		scStatus := derefStr(sc.Status)
		if scStatus == "" {
			scStatus = sp(sc, "status")
		}

		// Domain filter (match derived domain slug)
		if *flagDomain != "" && scDomain != *flagDomain {
			continue
		}

		// Scenario key filter
		if *flagScenario != "" && scKey != *flagScenario {
			continue
		}

		// Context filter
		if contextFilterIDs != nil && !contextFilterIDs[sc.EntityID] {
			continue
		}

		// Build steps
		stepIDs := scenToSteps[sc.EntityID]
		if *flagMinSteps > 0 && len(stepIDs) < *flagMinSteps {
			continue
		}

		var stepViews []StepView
		for _, stepID := range stepIDs {
			step, ok := stepByID[stepID]
			if !ok {
				continue
			}

			// Context
			ctxID := stepToCtx[stepID]
			ctxKey := ""
			ctxName := ""
			ctxType := ""
			if ctxID != "" {
				if c, ok := ctxByID[ctxID]; ok {
					ctxKey = derefStr(c.Key)
					ctxName = sp(c, "name")
					ctxType = sp(c, "context_type")
					usedContexts[ctxID] = true
				}
			}

			// Actions
			actIDs := stepToActions[stepID]
			var actLabels []string
			for _, actID := range actIDs {
				if act, ok := actByID[actID]; ok {
					label := sp(act, "label")
					if label == "" {
						label = sp(act, "name")
					}
					if label == "" {
						label = derefStr(act.Key)
					}
					actLabels = append(actLabels, label)
				}
			}
			sort.Strings(actLabels)

			// Skip empty steps unless --show-empty
			if !*flagShowEmpty && len(actLabels) == 0 && ctxID == "" {
				continue
			}

			// Parse order
			order := 0
			if v, ok := step.Properties["order"]; ok {
				switch n := v.(type) {
				case float64:
					order = int(n)
				case int:
					order = n
				}
			}

			stepViews = append(stepViews, StepView{
				StepKey:     derefStr(step.Key),
				StepName:    sp(step, "name"),
				StepOrder:   order,
				ContextKey:  ctxKey,
				ContextName: ctxName,
				ContextType: ctxType,
				Actions:     actLabels,
			})
		}

		// Sort steps by order, then key
		sort.Slice(stepViews, func(i, j int) bool {
			if stepViews[i].StepOrder != stepViews[j].StepOrder {
				return stepViews[i].StepOrder < stepViews[j].StepOrder
			}
			return stepViews[i].StepKey < stepViews[j].StepKey
		})

		// --no-action-only: skip scenarios that have ANY action
		if *flagNoActOnly {
			hasAny := false
			for _, sv := range stepViews {
				if len(sv.Actions) > 0 {
					hasAny = true
					break
				}
			}
			if hasAny {
				continue
			}
		}

		views = append(views, ScenarioView{
			Key:    scKey,
			Name:   scName,
			Domain: scDomain,
			Status: scStatus,
			Steps:  stepViews,
		})
	}

	// Sort scenarios by domain then name
	sort.Slice(views, func(i, j int) bool {
		if views[i].Domain != views[j].Domain {
			return views[i].Domain < views[j].Domain
		}
		return views[i].Name < views[j].Name
	})

	// ── Compute stats ─────────────────────────────────────────────────────────
	stats := Stats{
		TotalScenarios: len(views),
		UniqueContexts: len(usedContexts),
	}
	for _, v := range views {
		stats.TotalSteps += len(v.Steps)
		for _, sv := range v.Steps {
			if sv.ContextKey != "" {
				stats.StepsWithContext++
			}
			if len(sv.Actions) > 0 {
				stats.StepsWithAction++
			} else {
				stats.StepsNoAction++
			}
		}
	}

	report := Report{
		Generated: time.Now().Format("2006-01-02"),
		Scenarios: views,
		Stats:     stats,
	}

	switch *flagFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "csv":
		printCSV(report)
	case "summary":
		printSummary(report)
	default:
		printTree(report)
	}
	return nil
}

// ── output ────────────────────────────────────────────────────────────────────

func printTree(r Report) {
	s := r.Stats
	fmt.Printf("\n╔══ SCENARIO CONTEXT MAP  %s\n", r.Generated)
	fmt.Printf("║   %d scenarios · %d steps · %d with action · %d without · %d unique contexts\n\n",
		s.TotalScenarios, s.TotalSteps, s.StepsWithAction, s.StepsNoAction, s.UniqueContexts)

	lastDomain := ""
	for _, sc := range r.Scenarios {
		if sc.Domain != lastDomain {
			if lastDomain != "" {
				fmt.Println()
			}
			fmt.Printf("══ %s\n", domainLabel(sc.Domain))
			lastDomain = sc.Domain
		}

		statusMark := "·"
		switch sc.Status {
		case "planned":
			statusMark = "◌"
		case "implemented":
			statusMark = "✓"
		}

		stepsWithAct := 0
		for _, sv := range sc.Steps {
			if len(sv.Actions) > 0 {
				stepsWithAct++
			}
		}

		fmt.Printf("  ┌─ %s %s  [%d steps, %d with action]\n", statusMark, sc.Name, len(sc.Steps), stepsWithAct)

		for i, sv := range sc.Steps {
			connector := "├"
			if i == len(sc.Steps)-1 {
				connector = "└"
			}

			ctxLabel := sv.ContextName
			if ctxLabel == "" {
				ctxLabel = "(no context)"
			}
			if sv.ContextType != "" {
				ctxLabel += " [" + sv.ContextType + "]"
			}

			if len(sv.Actions) == 0 {
				fmt.Printf("  %s─ step %d  ctx:%-30s  (no action)\n", connector, sv.StepOrder, ctxLabel)
			} else {
				fmt.Printf("  %s─ step %d  ctx:%-30s  → %s\n", connector, sv.StepOrder, ctxLabel, sv.Actions[0])
				for _, a := range sv.Actions[1:] {
					fmt.Printf("  │  %s%-35s    → %s\n", strings.Repeat(" ", 10), "", a)
				}
			}
		}
	}

	fmt.Printf("\n── Stats ──────────────────────────────────────────────────────\n")
	fmt.Printf("  Scenarios:       %d\n", s.TotalScenarios)
	fmt.Printf("  Steps total:     %d\n", s.TotalSteps)
	fmt.Printf("  Steps w/ action: %d (%.0f%%)\n", s.StepsWithAction, pct(s.StepsWithAction, s.TotalSteps))
	fmt.Printf("  Steps no action: %d (%.0f%%)\n", s.StepsNoAction, pct(s.StepsNoAction, s.TotalSteps))
	fmt.Printf("  Steps w/ ctx:    %d (%.0f%%)\n", s.StepsWithContext, pct(s.StepsWithContext, s.TotalSteps))
	fmt.Printf("  Unique contexts: %d\n", s.UniqueContexts)
}

func printSummary(r Report) {
	s := r.Stats
	fmt.Printf("%-50s  %-20s  %5s  %5s  %5s\n", "Scenario", "Domain", "Steps", "w/Act", "w/Ctx")
	fmt.Println(strings.Repeat("─", 100))
	for _, sc := range r.Scenarios {
		withAct := 0
		withCtx := 0
		for _, sv := range sc.Steps {
			if len(sv.Actions) > 0 {
				withAct++
			}
			if sv.ContextKey != "" {
				withCtx++
			}
		}
		name := sc.Name
		if len(name) > 48 {
			name = name[:45] + "..."
		}
		fmt.Printf("%-50s  %-20s  %5d  %5d  %5d\n", name, domainLabel(sc.Domain), len(sc.Steps), withAct, withCtx)
	}
	fmt.Printf("\nTotal: %d scenarios, %d steps, %d with action (%.0f%%), %d unique contexts\n",
		s.TotalScenarios, s.TotalSteps, s.StepsWithAction, pct(s.StepsWithAction, s.TotalSteps), s.UniqueContexts)
}

func printCSV(r Report) {
	w := csv.NewWriter(os.Stdout)
	_ = w.Write([]string{"scenario_key", "scenario_name", "domain", "status", "step_key", "step_order", "context_key", "context_name", "context_type", "actions"})
	for _, sc := range r.Scenarios {
		for _, sv := range sc.Steps {
			_ = w.Write([]string{
				sc.Key,
				sc.Name,
				sc.Domain,
				sc.Status,
				sv.StepKey,
				fmt.Sprintf("%d", sv.StepOrder),
				sv.ContextKey,
				sv.ContextName,
				sv.ContextType,
				strings.Join(sv.Actions, "|"),
			})
		}
	}
	w.Flush()
}

// domainFromKey extracts domain slug from scenario key (s-<domain>-<rest>).
func domainFromKey(key string) string {
	if !strings.HasPrefix(key, "s-") {
		return ""
	}
	rest := key[2:]
	idx := strings.Index(rest, "-")
	if idx < 0 {
		return rest
	}
	// Try two-part domain slugs (e.g. "graph-objects", "org-user")
	twoPartEnd := strings.Index(rest[idx+1:], "-")
	if twoPartEnd >= 0 {
		twoSlug := rest[:idx+1+twoPartEnd]
		// Known two-part slugs
		switch twoSlug {
		case "graph-objects", "graph-relationships", "graph-branches", "graph-journal",
			"org-user", "agents-sandbox", "agents-batch", "agents-runs",
			"embedding-policies", "discovery-jobs", "schema-registry",
			"mcp-registry", "user-access", "user-activity", "user-profile":
			return twoSlug
		}
	}
	return rest[:idx]
}

// ── helpers ───────────────────────────────────────────────────────────────────

func domainLabel(d string) string {
	if d == "" {
		return "(unknown domain)"
	}
	return d
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

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

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
