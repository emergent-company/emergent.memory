// Deprecated: use `codebase analyze contexts` instead. Run `codebase --help` for details.
// context-action-map: lists every Context with its reachable Actions.
//
// Traversal (pure graph, no file scanning):
//   Context ←[occurs_in]← ScenarioStep →[has_action]→ Action
//
// Usage:
//
//	MEMORY_API_KEY=... MEMORY_PROJECT_ID=... MEMORY_SERVER_URL=... go run ./...
//	  --context <key>        filter to one context by key
//	  --type <cli|server>    filter by context_type
//	  --format table|json    (default: table)
//	  --show-empty           include contexts with no actions (default: hidden)
package main

import (
	"context"
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
	"github.com/olekukonko/tablewriter"
)

var (
	flagContext   = flag.String("context", "", "Filter to one context by key")
	flagType      = flag.String("type", "", "Filter by context_type (cli, server, ...)")
	flagFormat    = flag.String("format", "table", "Output format: table, json")
	flagShowEmpty = flag.Bool("show-empty", false, "Include contexts with no actions")
)

type ContextRow struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	ContextType string   `json:"context_type"`
	Description string   `json:"description"`
	Actions     []string `json:"actions"`
}

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

	// ── parallel fetch ────────────────────────────────────────────────────────
	var (
		contexts  []*sdkgraph.GraphObject
		actions   []*sdkgraph.GraphObject
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

	fetch(func() error { r, e := listAllObj(ctx, client.Graph, "Context"); contexts = r; return e })
	fetch(func() error { r, e := listAllObj(ctx, client.Graph, "Action"); actions = r; return e })
	fetch(func() error { r, e := listAllRel(ctx, client.Graph, "occurs_in"); occursIn = r; return e })
	fetch(func() error { r, e := listAllRel(ctx, client.Graph, "has_action"); hasAction = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	fmt.Fprintf(os.Stderr, "→ contexts=%d actions=%d occurs_in=%d has_action=%d\n",
		len(contexts), len(actions), len(occursIn), len(hasAction))

	// ── build indexes ─────────────────────────────────────────────────────────

	actionByID := map[string]*sdkgraph.GraphObject{}
	for _, a := range actions {
		actionByID[a.EntityID] = a
	}

	// step → set of context IDs
	stepToCtx := map[string]map[string]bool{}
	for _, r := range occursIn {
		if stepToCtx[r.SrcID] == nil {
			stepToCtx[r.SrcID] = map[string]bool{}
		}
		stepToCtx[r.SrcID][r.DstID] = true
	}

	// step → action IDs
	stepToActions := map[string][]string{}
	for _, r := range hasAction {
		stepToActions[r.SrcID] = append(stepToActions[r.SrcID], r.DstID)
	}

	// context ID → set of action IDs (via steps)
	ctxToActions := map[string]map[string]bool{}
	for stepID, ctxIDs := range stepToCtx {
		for _, aid := range stepToActions[stepID] {
			for ctxID := range ctxIDs {
				if ctxToActions[ctxID] == nil {
					ctxToActions[ctxID] = map[string]bool{}
				}
				ctxToActions[ctxID][aid] = true
			}
		}
	}

	// ── build rows ────────────────────────────────────────────────────────────

	var rows []ContextRow
	for _, c := range contexts {
		key := c.Key
		if key == nil {
			s := c.EntityID
			key = &s
		}
		ctype := sp(c, "context_type")
		name := sp(c, "name")
		desc := sp(c, "description")

		// filters
		if *flagContext != "" && !strings.EqualFold(*key, *flagContext) {
			continue
		}
		if *flagType != "" && !strings.EqualFold(ctype, *flagType) {
			continue
		}

		// collect action labels
		var actLabels []string
		for aid := range ctxToActions[c.EntityID] {
			a, ok := actionByID[aid]
			if !ok {
				continue
			}
			p := a.Properties
			lbl := strProp(p, "name")
			if lbl == "" {
				lbl = strProp(p, "label")
			}
			if lbl == "" {
				lbl = strProp(p, "description")
			}
			if lbl != "" {
				actLabels = append(actLabels, lbl)
			}
		}
		sort.Strings(actLabels)

		if !*flagShowEmpty && len(actLabels) == 0 {
			continue
		}

		rows = append(rows, ContextRow{
			Key:         *key,
			Name:        name,
			ContextType: ctype,
			Description: desc,
			Actions:     actLabels,
		})
	}

	// sort: context_type asc, key asc
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ContextType != rows[j].ContextType {
			return rows[i].ContextType < rows[j].ContextType
		}
		return rows[i].Key < rows[j].Key
	})

	// ── output ────────────────────────────────────────────────────────────────

	switch *flagFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	default:
		printTable(rows)
	}
	return nil
}

func printTable(rows []ContextRow) {
	fmt.Printf("\n┌─ CONTEXT → ACTION MAP  (%d contexts)\n\n", len(rows))
	for _, r := range rows {
		fmt.Printf("  ┌─ %-45s [%s]\n", r.Key, r.ContextType)
		fmt.Printf("  │  name: %s\n", r.Name)
		if r.Description != "" {
			fmt.Printf("  │  desc: %s\n", r.Description)
		}
		fmt.Printf("  │  actions (%d):\n", len(r.Actions))
		if len(r.Actions) == 0 {
			fmt.Printf("  │    (none)\n")
		}
		for _, a := range r.Actions {
			fmt.Printf("  │    • %s\n", a)
		}
		fmt.Println()
	}

	// summary table
	fmt.Println("─── SUMMARY ───────────────────────────────────────────────────────────────")
	t := tablewriter.NewWriter(os.Stdout)
	t.Header("Key", "Type", "Name", "Actions")
	for _, r := range rows {
		t.Append([]string{r.Key, r.ContextType, r.Name, fmt.Sprintf("%d", len(r.Actions))})
	}
	t.Render()
}

// ── helpers ───────────────────────────────────────────────────────────────────

func listAllObj(ctx context.Context, g *sdkgraph.Client, objType string) ([]*sdkgraph.GraphObject, error) {
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

func listAllRel(ctx context.Context, g *sdkgraph.Client, relType string) ([]*sdkgraph.GraphRelationship, error) {
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
	return strProp(o.Properties, key)
}

func strProp(p map[string]interface{}, key string) string {
	if v, ok := p[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
