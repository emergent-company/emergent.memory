package onboardcmd

import (
	"context"
	"fmt"
	"os"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// starterRules are seeded into constitution-v1 on first onboard.
// They encode naming conventions and quality constraints for a Go API codebase.
var starterRules = []struct {
	Key       string
	Name      string
	Statement string
	Category  string
	AppliesTo string
	AutoCheck string
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
	},
	{
		Key:       "rule-api-has-path",
		Name:      "APIEndpoint must have path",
		Statement: "Every APIEndpoint must have a non-empty 'path' property starting with '/'.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
	},
	{
		Key:       "rule-api-has-domain",
		Name:      "APIEndpoint must have domain",
		Statement: "Every APIEndpoint must have a 'domain' property matching its owning domain slug.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
	},
	{
		Key:       "rule-api-auth-documented",
		Name:      "APIEndpoint auth must be documented",
		Statement: "Every APIEndpoint must have 'auth_required' set to true or false — never absent.",
		Category:  "api",
		AppliesTo: "APIEndpoint",
	},
	{
		Key:       "rule-coverage-high-risk-tested",
		Name:      "High-risk domains must have tests",
		Statement: "Domains with 10+ endpoints and no test coverage are high-risk and must have at least one test file.",
		Category:  "service",
		AppliesTo: "Domain",
	},
}

// createConstitution creates constitution-v1 and seeds starter rules, wiring
// each rule to the constitution via an 'includes' relationship.
func createConstitution(ctx context.Context, gc *sdkgraph.Client) error {
	constKey := "constitution-v1"
	constObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
		Type: "Constitution",
		Key:  &constKey,
		Properties: map[string]any{
			"name":        "Codebase Constitution v1",
			"description": "Non-negotiable constraints for this codebase's knowledge graph. Created by codebase onboard.",
			"version":     "1",
		},
	})
	if err != nil {
		return fmt.Errorf("creating constitution: %w", err)
	}

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

		ruleObj, err := gc.UpsertObject(ctx, &sdkgraph.CreateObjectRequest{
			Type:       "Rule",
			Key:        &key,
			Properties: props,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: creating rule %s: %v\n", rule.Key, err)
			continue
		}

		_, err = gc.UpsertRelationship(ctx, &sdkgraph.CreateRelationshipRequest{
			Type:  "includes",
			SrcID: constObj.EntityID,
			DstID: ruleObj.EntityID,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: wiring rule %s: %v\n", rule.Key, err)
		}
	}

	return nil
}
