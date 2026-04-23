// Deprecated: use `codebase fix rewire` instead. Run `codebase --help` for details.
// context-rewire: splits coarse Context objects by rewiring occurs_in relationships.
//
// For each ScenarioStep that occurs_in a coarse context, determines the correct
// granular context based on the step's parent scenario's domain (key prefix),
// deletes the old occurs_in rel, and creates a new one to the granular context.
//
// Coarse → granular mapping is driven by scenario domain slug extracted from key prefix.
//
// Usage:
//
//	MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... go run ./...
//	  --dry-run     print plan without making changes (default: true)
//	  --apply       actually delete+create rels
//	  --domain <d>  only process scenarios in this domain
package main

import (
	"context"
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
	flagDryRun = flag.Bool("dry-run", true, "Print plan without making changes")
	flagApply  = flag.Bool("apply", false, "Apply changes (delete old + create new occurs_in rels)")
	flagDomain = flag.String("domain", "", "Only process scenarios in this domain slug")
)

// coarseContextIDs: entity IDs of the coarse contexts to split.
// Key = entity_id, Value = human label for logging.
var coarseContextIDs = map[string]string{
	"60e0c550-b91e-46b6-b0eb-fce7774e4c54": "ctx-cli-agents-trigger",   // memory agents trigger
	"c8e5f3a2-1234-0000-0000-000000000000": "ctx-cli-terminal",          // CLI Terminal — placeholder, resolved at runtime
	"00000000-0000-0000-0000-000000000001": "cli-login",                 // memory login — placeholder
	"00000000-0000-0000-0000-000000000002": "cli-graph-objects",         // memory graph objects — placeholder
	"00000000-0000-0000-0000-000000000003": "cli-extraction",            // memory extraction — placeholder
}

// oldCtxDomainToNewCtxKey: (old_context_key, domain_slug) → new granular context key.
// Two-level key avoids ambiguity when same domain appears in multiple coarse contexts.
// auth stays in cli-login — no rewire needed.
var oldCtxDomainToNewCtxKey = map[string]map[string]string{
	"cli-graph-objects": {
		"graph":          "ctx-cli-graph-core",
		"integrations":   "ctx-cli-integrations",
		"tasks":          "ctx-cli-tasks",
		"githubapp":      "ctx-cli-github",
		"sandboximages":  "ctx-cli-sandbox-images",
		"graph-branches": "cli-branches",
		"graph-journal":  "cli-journal",
		// cli-domain scenarios that ended up in graph-objects context
		"cli":            "ctx-cli-graph-core",
	},
	"ctx-cli-agents-trigger": {
		"agents":         "ctx-cli-agents",
		"agents-sandbox": "ctx-cli-agents-sandbox",
		"agents-batch":   "ctx-cli-agents-batch",
		// cli-domain scenarios that use agents-trigger context stay in agents
		"cli":            "ctx-cli-agents",
	},
	"ctx-cli-terminal": {
		"cli":           "ctx-cli-core",
		"org-user":      "ctx-cli-org-user",
		"org":           "ctx-cli-org-user",
		"chat":          "ctx-cli-chat",
		"observability": "ctx-cli-observability",
		// agents scenarios that ended up in CLI Terminal
		"agents":        "ctx-cli-agents",
	},
	"cli-extraction": {
		"extraction":    "ctx-cli-extraction-jobs",
		"datasources":   "ctx-cli-datasources",
		"discoveryjobs": "ctx-cli-discovery-jobs",
		"cli":           "ctx-cli-extraction-jobs",
	},
}

type rewireOp struct {
	oldRelID    string
	stepID      string
	stepKey     string
	scenKey     string
	domainSlug  string
	oldCtxKey   string
	newCtxKey   string
	newCtxID    string
}

func main() {
	flag.Parse()
	if *flagApply {
		*flagDryRun = false
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Fprintln(os.Stderr, "→ Fetching graph data...")

	var (
		scenarios  []*sdkgraph.GraphObject
		steps      []*sdkgraph.GraphObject
		contexts   []*sdkgraph.GraphObject
		hasStep    []*sdkgraph.GraphRelationship
		occursIn   []*sdkgraph.GraphRelationship
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
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "has_step"); hasStep = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "occurs_in"); occursIn = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	fmt.Fprintf(os.Stderr, "  Scenarios:%d Steps:%d Contexts:%d has_step:%d occurs_in:%d\n",
		len(scenarios), len(steps), len(contexts), len(hasStep), len(occursIn))

	// ── Build indexes ─────────────────────────────────────────────────────────

	// Context by key → entity_id
	ctxIDByKey := map[string]string{}
	ctxKeyByID := map[string]string{}
	for _, c := range contexts {
		k := derefStr(c.Key)
		ctxIDByKey[k] = c.EntityID
		ctxKeyByID[c.EntityID] = k
	}

	// Resolve coarse context IDs from keys (runtime resolution)
	coarseKeys := []string{"ctx-cli-agents-trigger", "ctx-cli-terminal", "cli-login", "cli-graph-objects", "cli-extraction"}
	coarseIDToKey := map[string]string{}
	for _, k := range coarseKeys {
		if id, ok := ctxIDByKey[k]; ok {
			coarseIDToKey[id] = k
		} else {
			fmt.Fprintf(os.Stderr, "  Warning: coarse context key %q not found in graph\n", k)
		}
	}

	// Resolve new context IDs from keys (collect all unique new ctx keys)
	allNewKeys := map[string]bool{}
	for _, domMap := range oldCtxDomainToNewCtxKey {
		for _, newKey := range domMap {
			allNewKeys[newKey] = true
		}
	}
	newCtxIDByKey := map[string]string{}
	for newKey := range allNewKeys {
		if id, ok := ctxIDByKey[newKey]; ok {
			newCtxIDByKey[newKey] = id
		} else {
			fmt.Fprintf(os.Stderr, "  Warning: new context key %q not found in graph\n", newKey)
		}
	}

	// Scenario by entity_id
	scenByID := map[string]*sdkgraph.GraphObject{}
	for _, sc := range scenarios {
		scenByID[sc.EntityID] = sc
	}

	// Step by entity_id
	stepByID := map[string]*sdkgraph.GraphObject{}
	for _, s := range steps {
		stepByID[s.EntityID] = s
	}

	// scenarioID → scenario (via has_step: scenID → stepID)
	stepToScen := map[string]string{} // stepID → scenID
	for _, r := range hasStep {
		stepToScen[r.DstID] = r.SrcID
	}

	// occurs_in rels pointing to coarse contexts
	// relID → (stepID, ctxID)
	type occursInRel struct {
		relID string
		stepID string
		ctxID  string
	}
	var coarseRels []occursInRel
	for _, r := range occursIn {
		if _, isCoarse := coarseIDToKey[r.DstID]; isCoarse {
			coarseRels = append(coarseRels, occursInRel{r.EntityID, r.SrcID, r.DstID})
		}
	}

	fmt.Fprintf(os.Stderr, "  occurs_in rels pointing to coarse contexts: %d\n", len(coarseRels))

	// ── Build rewire plan ─────────────────────────────────────────────────────
	var ops []rewireOp
	skipped := 0

	for _, rel := range coarseRels {
		step, ok := stepByID[rel.stepID]
		if !ok {
			fmt.Fprintf(os.Stderr, "  Warning: step %s not found\n", rel.stepID)
			continue
		}
		stepKey := derefStr(step.Key)

		scenID := stepToScen[rel.stepID]
		scen, ok := scenByID[scenID]
		if !ok {
			fmt.Fprintf(os.Stderr, "  Warning: scenario for step %s not found\n", stepKey)
			continue
		}
		scenKey := derefStr(scen.Key)
		domSlug := domainFromKey(scenKey)

		// Domain filter
		if *flagDomain != "" && domSlug != *flagDomain {
			continue
		}

		oldCtxKey := coarseIDToKey[rel.ctxID]

		// auth domain: stays in cli-login — skip
		if domSlug == "auth" {
			skipped++
			continue
		}

		domainMap, ok := oldCtxDomainToNewCtxKey[oldCtxKey]
		if !ok {
			fmt.Fprintf(os.Stderr, "  Warning: no domain map for old context %q\n", oldCtxKey)
			skipped++
			continue
		}
		newCtxKey, ok := domainMap[domSlug]
		if !ok {
			fmt.Fprintf(os.Stderr, "  Warning: no mapping for domain %q in context %q (step %s, scen %s)\n", domSlug, oldCtxKey, stepKey, scenKey)
			skipped++
			continue
		}

		newCtxID, ok := newCtxIDByKey[newCtxKey]
		if !ok {
			fmt.Fprintf(os.Stderr, "  Warning: new context %q has no entity_id\n", newCtxKey)
			skipped++
			continue
		}

		ops = append(ops, rewireOp{
			oldRelID:   rel.relID,
			stepID:     rel.stepID,
			stepKey:    stepKey,
			scenKey:    scenKey,
			domainSlug: domSlug,
			oldCtxKey:  oldCtxKey,
			newCtxKey:  newCtxKey,
			newCtxID:   newCtxID,
		})
	}

	// Sort for deterministic output
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].domainSlug != ops[j].domainSlug {
			return ops[i].domainSlug < ops[j].domainSlug
		}
		return ops[i].scenKey < ops[j].scenKey
	})

	// ── Print plan ────────────────────────────────────────────────────────────
	fmt.Printf("\n── REWIRE PLAN (%d ops, %d skipped) ──\n", len(ops), skipped)

	// Group by domain for readability
	byDomain := map[string][]rewireOp{}
	for _, op := range ops {
		byDomain[op.domainSlug] = append(byDomain[op.domainSlug], op)
	}
	domains := []string{}
	for d := range byDomain {
		domains = append(domains, d)
	}
	sort.Strings(domains)

	for _, dom := range domains {
		domOps := byDomain[dom]
		// All ops in same domain have same newCtxKey
		fmt.Printf("  %-20s (%3d steps)  %s → %s\n", dom, len(domOps), domOps[0].oldCtxKey, domOps[0].newCtxKey)
	}

	if *flagDryRun && !*flagApply {
		fmt.Printf("\n[dry-run] No changes made. Pass --apply to execute.\n")
		return nil
	}

	// ── Apply ─────────────────────────────────────────────────────────────────
	fmt.Printf("\n── APPLYING %d rewires ──\n", len(ops))

	deleted := 0
	created := 0
	errors := 0

	for i, op := range ops {
		// Delete old rel
		if err := client.Graph.DeleteRelationship(ctx, op.oldRelID); err != nil {
			fmt.Fprintf(os.Stderr, "  [%d/%d] DELETE FAILED %s: %v\n", i+1, len(ops), op.oldRelID, err)
			errors++
			continue
		}
		deleted++

		// Create new rel
		_, err := client.Graph.CreateRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
			Type:  "occurs_in",
			SrcID: op.stepID,
			DstID: op.newCtxID,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [%d/%d] CREATE FAILED step %s → %s: %v\n", i+1, len(ops), op.stepKey, op.newCtxKey, err)
			errors++
			continue
		}
		created++

		if (i+1)%50 == 0 {
			fmt.Fprintf(os.Stderr, "  progress: %d/%d\n", i+1, len(ops))
		}
	}

	fmt.Printf("\n── DONE: %d deleted, %d created, %d errors ──\n", deleted, created, errors)
	return nil
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
	twoPartEnd := strings.Index(rest[idx+1:], "-")
	if twoPartEnd >= 0 {
		twoSlug := rest[:idx+1+twoPartEnd]
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

// ── SDK helpers ───────────────────────────────────────────────────────────────

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

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
