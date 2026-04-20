package constitutioncmd

import (
	"context"
	"fmt"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newAddRuleCmd(flagProjectID *string, flagBranch *string) *cobra.Command {
	var (
		flagKey       string
		flagName      string
		flagStatement string
		flagCategory  string
		flagAppliesTo string
		flagAutoCheck string
		flagPropCheck string
		flagRationale string
		flagAuditType string
	)

	cmd := &cobra.Command{
		Use:   "add-rule",
		Short: "Add a rule to the constitution",
		Long: `Create a Rule object and wire it to constitution-v1.

The AI agent uses this after analyzing the codebase to encode constraints
it discovers (naming conventions, required properties, structural patterns).

Examples:
  codebase constitution add-rule \
    --key rule-api-pagination \
    --name "List endpoints must support pagination" \
    --statement "Every GET endpoint returning a list must accept limit and cursor query params." \
    --category api \
    --applies-to APIEndpoint

  codebase constitution add-rule \
    --key rule-naming-service-key \
    --name "Service key prefix" \
    --statement "Every Service key must start with svc-" \
    --category naming \
    --applies-to Service \
    --auto-check "^svc-[a-z][a-z0-9-]+$"
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagKey == "" || flagName == "" || flagStatement == "" || flagCategory == "" {
				return fmt.Errorf("--key, --name, --statement, and --category are required")
			}
			validCategories := map[string]bool{
				"naming": true, "api": true, "service": true,
				"db": true, "scenario": true, "security": true, "performance": true,
			}
			if !validCategories[flagCategory] {
				return fmt.Errorf("invalid --category %q: must be one of: naming, api, service, db, scenario, security, performance", flagCategory)
			}
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			ctx := context.Background()
			return runAddRule(ctx, c.Graph, flagKey, flagName, flagStatement, flagCategory, flagAppliesTo, flagAutoCheck, flagPropCheck, flagRationale, flagAuditType)
		},
	}

	cmd.Flags().StringVar(&flagKey, "key", "", "Rule key (rule-<category>-<slug>)")
	cmd.Flags().StringVar(&flagName, "name", "", "Short human label")
	cmd.Flags().StringVar(&flagStatement, "statement", "", "The constraint in plain English")
	cmd.Flags().StringVar(&flagCategory, "category", "", "Category: naming | api | service | db | scenario | security | performance")
	cmd.Flags().StringVar(&flagAppliesTo, "applies-to", "", "Object type(s) this rule applies to (comma-separated)")
	cmd.Flags().StringVar(&flagAutoCheck, "auto-check", "", "Go regex applied to object key for automatic checking")
	cmd.Flags().StringVar(&flagPropCheck, "prop-check", "", `JSON spec for graph property check, e.g. '{"field":"method","nonempty":true}'`)
	cmd.Flags().StringVar(&flagRationale, "rationale", "", "Why this rule exists")
	cmd.Flags().StringVar(&flagAuditType, "audit-type", "", "Audit type(s) for filtering: security, performance (comma-separated)")
	return cmd
}

func runAddRule(ctx context.Context, gc *sdkgraph.Client, key, name, statement, category, appliesTo, autoCheck, propCheck, rationale, auditType string) error {
	props := map[string]any{
		"name":      name,
		"statement": statement,
		"category":  category,
	}
	if appliesTo != "" {
		props["applies_to"] = appliesTo
	}
	if autoCheck != "" {
		props["auto_check"] = autoCheck
	}
	if propCheck != "" {
		props["prop_check"] = propCheck
	}
	if rationale != "" {
		props["rationale"] = rationale
	}
	if auditType != "" {
		props["audit_type"] = auditType
	}

	ruleObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
		Type:       "Rule",
		Key:        &key,
		Properties: props,
	})
	if err != nil {
		return fmt.Errorf("creating rule: %w", err)
	}

	// Wire to constitution-v1 if it exists
	constResp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: "constitution-v1", Type: "Constitution", Limit: 1})
	if err == nil && len(constResp.Items) > 0 {
		constObj := constResp.Items[0]
		if constObj.Key != nil && *constObj.Key == "constitution-v1" {
			_, err = gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
				Type:  "includes",
				SrcID: constObj.EntityID,
				DstID: ruleObj.EntityID,
			})
			if err != nil {
				fmt.Fprintf(nil, "warn: wiring to constitution: %v\n", err)
			}
		}
	}

	fmt.Printf("created  %s  (%s)\n", key, ruleObj.EntityID)
	return nil
}
