package constitutioncmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newRulesCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagCategory string

	cmd := &cobra.Command{
		Use:   "rules",
		Short: "List constitution rules",
		Long: `List all Rule objects in the constitution.

Examples:
  codebase constitution rules
  codebase constitution rules --category naming
  codebase constitution rules --format json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			ctx := context.Background()
			return runRules(ctx, c.Graph, flagCategory, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagCategory, "category", "", "Filter by category: naming, api, service, db, scenario")
	return cmd
}

func runRules(ctx context.Context, gc *sdkgraph.Client, category, format string) error {
	resp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: "Rule", Limit: 500})
	if err != nil {
		return fmt.Errorf("listing rules: %w", err)
	}

	items := resp.Items
	if category != "" {
		var filtered []*sdkgraph.GraphObject
		for _, r := range items {
			if strings.EqualFold(strProp(r, "category"), category) {
				filtered = append(filtered, r)
			}
		}
		items = filtered
	}

	sort.Slice(items, func(i, j int) bool {
		ci, cj := strProp(items[i], "category"), strProp(items[j], "category")
		if ci != cj {
			return ci < cj
		}
		ki, kj := derefKey(items[i].Key), derefKey(items[j].Key)
		return ki < kj
	})

	if format == "json" {
		type row struct {
			Key       string `json:"key"`
			Name      string `json:"name"`
			Category  string `json:"category"`
			Statement string `json:"statement"`
			AppliesTo string `json:"applies_to,omitempty"`
			AutoCheck string `json:"auto_check,omitempty"`
		}
		var out []row
		for _, r := range items {
			out = append(out, row{
				Key:       derefKey(r.Key),
				Name:      strProp(r, "name"),
				Category:  strProp(r, "category"),
				Statement: strProp(r, "statement"),
				AppliesTo: strProp(r, "applies_to"),
				AutoCheck: strProp(r, "auto_check"),
			})
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	if len(items) == 0 {
		fmt.Println("No rules found. Run `codebase onboard` to create starter rules, or add one with:")
		fmt.Println("  codebase constitution add-rule --key rule-api-<slug> --name '...' --statement '...' --category api --applies-to APIEndpoint")
		return nil
	}

	fmt.Printf("%-10s %-40s %-28s %s\n", "CATEGORY", "KEY", "NAME", "STATEMENT")
	fmt.Println(strings.Repeat("─", 120))
	for _, r := range items {
		stmt := strProp(r, "statement")
		if len(stmt) > 60 {
			stmt = stmt[:57] + "..."
		}
		fmt.Printf("%-10s %-40s %-28s %s\n",
			strProp(r, "category"),
			derefKey(r.Key),
			strProp(r, "name"),
			stmt,
		)
	}
	fmt.Printf("\n%d rules\n", len(items))
	return nil
}

func strProp(obj *sdkgraph.GraphObject, key string) string {
	if obj.Properties == nil {
		return ""
	}
	v, _ := obj.Properties[key].(string)
	return v
}

// anyPropStr returns the property value as a string regardless of its stored type.
// Handles string, bool, float64 (JSON number), and int.
func anyPropStr(obj *sdkgraph.GraphObject, key string) string {
	if obj.Properties == nil {
		return ""
	}
	switch v := obj.Properties[key].(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%g", v)
	case int:
		return fmt.Sprintf("%d", v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func derefKey(k *string) string {
	if k == nil {
		return ""
	}
	return *k
}
