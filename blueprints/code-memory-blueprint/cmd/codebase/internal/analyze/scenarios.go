package analyzecmd

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newScenariosCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagDomain    string
		flagScenario  string
		flagContext   string
		flagShowEmpty bool
		flagMinSteps  int
		flagNoActOnly bool
	)

	cmd := &cobra.Command{
		Use:   "scenarios",
		Short: "Map scenarios to contexts and actions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			return runScenarios(cfg.SDK, flagDomain, flagScenario, flagContext, flagShowEmpty, flagMinSteps, flagNoActOnly, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter by domain slug")
	cmd.Flags().StringVar(&flagScenario, "scenario", "", "Filter to one scenario by key")
	cmd.Flags().StringVar(&flagContext, "context", "", "Filter to scenarios using a specific context key")
	cmd.Flags().BoolVar(&flagShowEmpty, "show-empty", false, "Include steps with no action")
	cmd.Flags().IntVar(&flagMinSteps, "min-steps", 0, "Only show scenarios with >= N steps")
	cmd.Flags().BoolVar(&flagNoActOnly, "no-action-only", false, "Only show scenarios where ALL steps have no action")

	return cmd
}

func runScenarios(client *sdk.Client, domainFilter, scenarioFilter, contextFilter string, showEmpty bool, minSteps int, noActOnly bool, format string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fmt.Fprintln(os.Stderr, "→ Fetching graph data...")

	var (
		scenarios []*sdkgraph.GraphObject
		steps     []*sdkgraph.GraphObject
		contexts  []*sdkgraph.GraphObject
		actions   []*sdkgraph.GraphObject
		hasStep   []*sdkgraph.GraphRelationship
		occursIn  []*sdkgraph.GraphRelationship
		hasAction []*sdkgraph.GraphRelationship
		mu        sync.Mutex
		wg        sync.WaitGroup
		fetchErr  error
	)

	fetch := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := fn(); e != nil {
				mu.Lock()
				fetchErr = e
				mu.Unlock()
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

	scenToSteps := map[string][]string{}
	for _, r := range hasStep {
		scenToSteps[r.SrcID] = append(scenToSteps[r.SrcID], r.DstID)
	}
	stepToCtx := map[string]string{}
	for _, r := range occursIn {
		stepToCtx[r.SrcID] = r.DstID
	}
	stepToActions := map[string][]string{}
	for _, r := range hasAction {
		stepToActions[r.SrcID] = append(stepToActions[r.SrcID], r.DstID)
	}

	var contextFilterIDs map[string]bool
	if contextFilter != "" {
		contextFilterIDs = map[string]bool{}
		for _, c := range contexts {
			if derefStr(c.Key) == contextFilter {
				for _, r := range occursIn {
					if r.DstID == c.EntityID {
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
	}

	var views []ScenarioView
	usedContexts := map[string]bool{}

	for _, sc := range scenarios {
		scKey := derefStr(sc.Key)
		scDomain := sp(sc, "domain")
		if scDomain == "" {
			scDomain = domainFromKey(scKey)
		}

		if domainFilter != "" && scDomain != domainFilter {
			continue
		}
		if scenarioFilter != "" && scKey != scenarioFilter {
			continue
		}
		if contextFilterIDs != nil && !contextFilterIDs[sc.EntityID] {
			continue
		}

		stepIDs := scenToSteps[sc.EntityID]
		if minSteps > 0 && len(stepIDs) < minSteps {
			continue
		}

		var stepViews []StepView
		for _, stepID := range stepIDs {
			step, ok := stepByID[stepID]
			if !ok {
				continue
			}
			ctxID := stepToCtx[stepID]
			ctxKey, ctxName, ctxType := "", "", ""
			if ctxID != "" {
				if c, ok := ctxByID[ctxID]; ok {
					ctxKey, ctxName, ctxType = derefStr(c.Key), sp(c, "name"), sp(c, "context_type")
					usedContexts[ctxID] = true
				}
			}
			actIDs := stepToActions[stepID]
			var actLabels []string
			for _, aid := range actIDs {
				if act, ok := actByID[aid]; ok {
					lbl := sp(act, "label")
					if lbl == "" {
						lbl = sp(act, "name")
					}
					if lbl == "" {
						lbl = derefStr(act.Key)
					}
					actLabels = append(actLabels, lbl)
				}
			}
			sort.Strings(actLabels)
			if !showEmpty && len(actLabels) == 0 && ctxID == "" {
				continue
			}
			stepViews = append(stepViews, StepView{
				StepKey: derefStr(step.Key), StepName: sp(step, "name"), StepOrder: getPropInt(step, "order"),
				ContextKey: ctxKey, ContextName: ctxName, ContextType: ctxType, Actions: actLabels,
			})
		}
		sort.Slice(stepViews, func(i, j int) bool {
			if stepViews[i].StepOrder != stepViews[j].StepOrder {
				return stepViews[i].StepOrder < stepViews[j].StepOrder
			}
			return stepViews[i].StepKey < stepViews[j].StepKey
		})

		if noActOnly {
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
			Key: scKey, Name: sp(sc, "name"), Domain: scDomain, Status: derefStr(sc.Status), Steps: stepViews,
		})
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].Domain != views[j].Domain {
			return views[i].Domain < views[j].Domain
		}
		return views[i].Name < views[j].Name
	})

	report := ScenarioReport{
		Generated: time.Now().Format("2006-01-02"),
		Scenarios: views,
	}

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "csv":
		return printScenariosCSV(report)
	default:
		printScenariosTree(report)
	}
	return nil
}

type StepView struct {
	StepKey     string   `json:"step_key"`
	StepName    string   `json:"step_name"`
	StepOrder   int      `json:"step_order"`
	ContextKey  string   `json:"context_key"`
	ContextName string   `json:"context_name"`
	ContextType string   `json:"context_type"`
	Actions     []string `json:"actions"`
}

type ScenarioView struct {
	Key    string     `json:"key"`
	Name   string     `json:"name"`
	Domain string     `json:"domain"`
	Status string     `json:"status"`
	Steps  []StepView `json:"steps"`
}

type ScenarioReport struct {
	Generated string         `json:"generated"`
	Scenarios []ScenarioView `json:"scenarios"`
}

func domainFromKey(key string) string {
	if !strings.HasPrefix(key, "s-") {
		return ""
	}
	rest := key[2:]
	idx := strings.Index(rest, "-")
	if idx < 0 {
		return rest
	}
	return rest[:idx]
}

func printScenariosTree(r ScenarioReport) {
	fmt.Printf("\n╔══ SCENARIO CONTEXT MAP  %s\n", r.Generated)
	lastDomain := ""
	for _, sc := range r.Scenarios {
		if sc.Domain != lastDomain {
			fmt.Printf("\n══ %s\n", sc.Domain)
			lastDomain = sc.Domain
		}
		fmt.Printf("  ┌─ %s  [%d steps]\n", sc.Name, len(sc.Steps))
		for i, sv := range sc.Steps {
			connector := "├"
			if i == len(sc.Steps)-1 {
				connector = "└"
			}
			ctxLabel := sv.ContextName
			if ctxLabel == "" {
				ctxLabel = "(no context)"
			}
			if len(sv.Actions) == 0 {
				fmt.Printf("  %s─ step %d  ctx:%-30s  (no action)\n", connector, sv.StepOrder, ctxLabel)
			} else {
				fmt.Printf("  %s─ step %d  ctx:%-30s  → %s\n", connector, sv.StepOrder, ctxLabel, sv.Actions[0])
			}
		}
	}
}

func printScenariosCSV(r ScenarioReport) error {
	w := csv.NewWriter(os.Stdout)
	_ = w.Write([]string{"scenario_key", "scenario_name", "domain", "status", "step_key", "step_order", "context_key", "context_name", "context_type", "actions"})
	for _, sc := range r.Scenarios {
		for _, sv := range sc.Steps {
			_ = w.Write([]string{sc.Key, sc.Name, sc.Domain, sc.Status, sv.StepKey, fmt.Sprintf("%d", sv.StepOrder), sv.ContextKey, sv.ContextName, sv.ContextType, strings.Join(sv.Actions, "|")})
		}
	}
	w.Flush()
	return nil
}
