package analyzecmd

import (
	"context"
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

func newContextsCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagContext   string
		flagType      string
		flagShowEmpty bool
	)

	cmd := &cobra.Command{
		Use:   "contexts",
		Short: "List contexts and their reachable actions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			return runContexts(cfg.SDK, flagContext, flagType, flagShowEmpty, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagContext, "context", "", "Filter to one context by key")
	cmd.Flags().StringVar(&flagType, "type", "", "Filter by context_type")
	cmd.Flags().BoolVar(&flagShowEmpty, "show-empty", false, "Include contexts with no actions")

	return cmd
}

func runContexts(client *sdk.Client, contextFilter, typeFilter string, showEmpty bool, format string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

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

	fetch(func() error { r, e := listAll(ctx, client.Graph, "Context"); contexts = r; return e })
	fetch(func() error { r, e := listAll(ctx, client.Graph, "Action"); actions = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "occurs_in"); occursIn = r; return e })
	fetch(func() error { r, e := listAllRels(ctx, client.Graph, "has_action"); hasAction = r; return e })

	wg.Wait()
	if fetchErr != nil {
		return fetchErr
	}

	actionByID := map[string]*sdkgraph.GraphObject{}
	for _, a := range actions {
		actionByID[a.EntityID] = a
	}

	stepToCtx := map[string]map[string]bool{}
	for _, r := range occursIn {
		if stepToCtx[r.SrcID] == nil {
			stepToCtx[r.SrcID] = map[string]bool{}
		}
		stepToCtx[r.SrcID][r.DstID] = true
	}

	stepToActions := map[string][]string{}
	for _, r := range hasAction {
		stepToActions[r.SrcID] = append(stepToActions[r.SrcID], r.DstID)
	}

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

	var rows []ContextRow
	for _, c := range contexts {
		key := derefStr(c.Key)
		ctype := sp(c, "context_type")
		if contextFilter != "" && !strings.EqualFold(key, contextFilter) {
			continue
		}
		if typeFilter != "" && !strings.EqualFold(ctype, typeFilter) {
			continue
		}

		var actLabels []string
		for aid := range ctxToActions[c.EntityID] {
			if a, ok := actionByID[aid]; ok {
				lbl := sp(a, "name")
				if lbl == "" {
					lbl = sp(a, "label")
				}
				if lbl != "" {
					actLabels = append(actLabels, lbl)
				}
			}
		}
		sort.Strings(actLabels)
		if !showEmpty && len(actLabels) == 0 {
			continue
		}

		rows = append(rows, ContextRow{
			Key: key, Name: sp(c, "name"), ContextType: ctype, Description: sp(c, "description"), Actions: actLabels,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ContextType != rows[j].ContextType {
			return rows[i].ContextType < rows[j].ContextType
		}
		return rows[i].Key < rows[j].Key
	})

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	fmt.Printf("\n┌─ CONTEXT → ACTION MAP  (%d contexts)\n\n", len(rows))
	for _, r := range rows {
		fmt.Printf("  ┌─ %-45s [%s]\n", r.Key, r.ContextType)
		fmt.Printf("  │  name: %s\n", r.Name)
		fmt.Printf("  │  actions (%d):\n", len(r.Actions))
		for _, a := range r.Actions {
			fmt.Printf("  │    • %s\n", a)
		}
		fmt.Println()
	}
	return nil
}

type ContextRow struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	ContextType string   `json:"context_type"`
	Description string   `json:"description"`
	Actions     []string `json:"actions"`
}
