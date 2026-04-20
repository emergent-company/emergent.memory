package constitutioncmd

import (
	"context"
	"fmt"
	"os"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

func newCreateCmd(flagProjectID *string, flagBranch *string) *cobra.Command {
	var flagName string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create constitution-v1 with starter rules",
		Long: `Create the constitution-v1 object and seed it with starter rules.

This is called automatically by 'codebase onboard'. Use this command directly
if you want to (re)create the constitution without running the full onboard.

Starter rules cover:
  - Naming conventions (key prefixes for APIEndpoint, Service, Domain)
  - API quality (method, path, domain, auth_required must be set)
  - Coverage (high-risk domains must have tests)
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			ctx := context.Background()
			return runCreate(ctx, c.Graph, flagName)
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "Codebase Constitution v1", "Constitution name")
	return cmd
}

// starterRules mirrors the ones in onboard/constitution.go.
// Kept in sync manually — both seed the same set.
var starterRules = []struct {
	Key       string
	Name      string
	Statement string
	Category  string
	AppliesTo string
	AutoCheck string
	PropCheck string // JSON propCheckSpec
}{
	{
		Key:       "rule-naming-api-endpoint-key",
		Name:      "APIEndpoint key prefix",
		Statement: "Every APIEndpoint key must start with 'ep-' followed by domain and handler slug.",
		Category:  "naming",
		AppliesTo: "APIEndpoint",
		AutoCheck: `^ep-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-naming-service-key",
		Name:      "Service key prefix",
		Statement: "Every Service key must start with 'svc-' followed by domain slug.",
		Category:  "naming",
		AppliesTo: "Service",
		AutoCheck: `^svc-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-naming-domain-key",
		Name:      "Domain key prefix",
		Statement: "Every Domain key must start with 'domain-' followed by the domain slug.",
		Category:  "naming",
		AppliesTo: "Domain",
		AutoCheck: `^domain-[a-z][a-z0-9-]+$`,
	},
	{
		Key:       "rule-api-has-method",
		Name:      "APIEndpoint must have HTTP method",
		Statement: "Every APIEndpoint must have a non-empty 'method' property (GET, POST, PUT, DELETE, PATCH).",
		Category:  "api",
		AppliesTo: "APIEndpoint",
		PropCheck: `{"field":"method","nonempty":true}`,
	},
	{
		Key:       "rule-api-has-path",
		Name:      "APIEndpoint must have path",
		Statement: "Every APIEndpoint must have a non-empty 'path' property starting with '/'.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
		PropCheck: `{"field":"path","prefix":"/"}`,
	},
	{
		Key:       "rule-api-has-domain",
		Name:      "APIEndpoint must have domain",
		Statement: "Every APIEndpoint must have a 'domain' property matching its owning domain slug.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
		PropCheck: `{"field":"domain","nonempty":true}`,
	},
	{
		Key:       "rule-api-auth-documented",
		Name:      "APIEndpoint auth must be documented",
		Statement: "Every APIEndpoint must have 'auth_required' set to true or false — never absent.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
		PropCheck: `{"field":"auth_required","bool":true}`,
	},
	{
		Key:       "rule-coverage-high-risk-tested",
		Name:      "High-risk domains must have tests",
		Statement: "Domains with 10+ endpoints and no test coverage are high-risk and must have at least one test file.",
		Category:  "service",
		AppliesTo: "Domain",
	},
}

func runCreate(ctx context.Context, gc *sdkgraph.Client, name string) error {
	constKey := "constitution-v1"
	constObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
		Type: "Constitution",
		Key:  &constKey,
		Properties: map[string]any{
			"name":        name,
			"description": "Non-negotiable constraints for this codebase's knowledge graph.",
			"version":     "1",
		},
	})
	if err != nil {
		return fmt.Errorf("creating constitution: %w", err)
	}
	fmt.Printf("created  constitution-v1  (%s)\n", constObj.EntityID)

	for _, rule := range starterRules {
		key := rule.Key
		props := map[string]any{
			"name":      rule.Name,
			"statement": rule.Statement,
			"category":  rule.Category,
		}
		if rule.AppliesTo != "" {
			props["applies_to"] = rule.AppliesTo
		}
		if rule.AutoCheck != "" {
			props["auto_check"] = rule.AutoCheck
		}
		if rule.PropCheck != "" {
			props["prop_check"] = rule.PropCheck
		}

		ruleObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
			Type:       "Rule",
			Key:        &key,
			Properties: props,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: rule %s: %v\n", rule.Key, err)
			continue
		}

		_, err = gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
			Type:  "includes",
			SrcID: constObj.EntityID,
			DstID: ruleObj.EntityID,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: wiring %s: %v\n", rule.Key, err)
			continue
		}
		fmt.Printf("  rule  %s\n", rule.Key)
	}

	fmt.Printf("\n%d rules wired to constitution-v1\n", len(starterRules))
	return nil
}
