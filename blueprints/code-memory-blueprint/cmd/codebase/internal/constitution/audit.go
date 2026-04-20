package constitutioncmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

// auditRuleDef is a security/performance rule seeded by `constitution audit --seed`.
type auditRuleDef struct {
	Key         string
	Name        string
	Statement   string
	Category    string
	AppliesTo   string
	AuditType   string // "security", "performance"
	PropCheck   string
	AutoCheck   string
	HowToVerify string
}

// securityPerformanceRules is the canonical set of security + performance audit rules.
var securityPerformanceRules = []auditRuleDef{
	// ── Security ──────────────────────────────────────────────────────────────
	{
		Key:       "rule-security-auth-required-set",
		Name:      "All endpoints must declare auth_required",
		Statement: "Every APIEndpoint must have auth_required set to true or false — never absent. Absence means unknown security posture.",
		Category:  "security",
		AppliesTo: "APIEndpoint",
		AuditType: "security",
		PropCheck: `{"field":"auth_required","bool":true}`,
	},
	{
		Key:         "rule-security-public-endpoints-documented",
		Name:        "Public endpoints must be intentional",
		Statement:   "Every APIEndpoint with auth_required=false must have a non-empty 'summary' explaining why it is public.",
		Category:    "security",
		AppliesTo:   "APIEndpoint",
		AuditType:   "security",
		HowToVerify: "List all APIEndpoints where auth_required=false and check that summary is non-empty. Flag any that lack justification.",
	},
	{
		Key:         "rule-security-no-wildcard-scopes",
		Name:        "No wildcard permission scopes",
		Statement:   "No APIEndpoint should have scopes containing '*' — use explicit minimal scopes.",
		Category:    "security",
		AppliesTo:   "APIEndpoint",
		AuditType:   "security",
		HowToVerify: "Check all APIEndpoints where scopes contains '*'. Each is a potential over-permission.",
	},
	{
		Key:         "rule-security-sensitive-domains-auth",
		Name:        "Sensitive domains must require auth",
		Statement:   "Domains classified as sensitive (auth, user, admin, billing, payment) must have 100% of their endpoints with auth_required=true.",
		Category:    "security",
		AppliesTo:   "Domain",
		AuditType:   "security",
		HowToVerify: "For each domain named auth, user, admin, billing, or payment: list its APIEndpoints and verify all have auth_required=true.",
	},
	{
		Key:         "rule-security-no-debug-handlers",
		Name:        "No debug/test handlers in production",
		Statement:   "No APIEndpoint handler name should contain 'debug', 'seed', or 'mock' unless the domain is explicitly a test domain.",
		Category:    "security",
		AppliesTo:   "APIEndpoint",
		AuditType:   "security",
		HowToVerify: "List APIEndpoints whose handler contains 'debug', 'seed', or 'mock' and are not in a test-e2e domain.",
	},
	// ── Performance ───────────────────────────────────────────────────────────
	{
		Key:         "rule-performance-list-endpoints-paginated",
		Name:        "List endpoints must support pagination",
		Statement:   "Every GET APIEndpoint whose path ends in a collection (no trailing :id) should document pagination via limit/cursor or page/size query params.",
		Category:    "performance",
		AppliesTo:   "APIEndpoint",
		AuditType:   "performance",
		HowToVerify: "List GET APIEndpoints whose path does not end in a path param (/:id, /:uuid, etc.). Check that their summary or handler name implies pagination support.",
	},
	{
		Key:         "rule-performance-high-cardinality-domains",
		Name:        "High-cardinality domains must document DB indexes",
		Statement:   "Domains with 20+ APIEndpoints are high-cardinality and must have at least one SourceFile documenting DB indexes or migrations.",
		Category:    "performance",
		AppliesTo:   "Domain",
		AuditType:   "performance",
		HowToVerify: "For each Domain with 20+ APIEndpoints: verify at least one SourceFile in that domain contains a migration or index definition.",
	},
	{
		Key:         "rule-performance-no-n-plus-one",
		Name:        "No N+1 query patterns in service methods",
		Statement:   "ServiceMethod implementations must not call database queries inside loops. Use batch queries or joins instead.",
		Category:    "performance",
		AppliesTo:   "Service",
		AuditType:   "performance",
		HowToVerify: "Scan service files for patterns like 'for ... { await ... find' or 'forEach ... query'. Flag any that appear to loop over DB calls.",
	},
	{
		Key:         "rule-performance-unbounded-queries",
		Name:        "Large responses must be paginated or streamed",
		Statement:   "Any endpoint that could return more than 1000 records must implement pagination (limit/cursor) or streaming. Unbounded queries are a performance risk.",
		Category:    "performance",
		AppliesTo:   "APIEndpoint",
		AuditType:   "performance",
		HowToVerify: "Review GET list endpoints and their corresponding service methods. Check that all list queries have a LIMIT clause or equivalent pagination guard.",
	},
}

func newAuditCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagAuditType string
		flagSeed      bool
		flagDomain    string
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Run security and performance audit rules against the graph",
		Long: `Run security and/or performance audit rules against graph objects.

Audit rules are constitution rules tagged with audit_type=security or
audit_type=performance. They focus on risk, not just naming conventions.

Flags:
  --type security     — run only security rules
  --type performance  — run only performance rules
  (omit)              — run all audit rules
  --seed              — seed the 9 built-in security/performance rules (safe to re-run)
  --domain            — filter APIEndpoint objects by domain

Examples:
  codebase constitution audit --seed
  codebase constitution audit --type security
  codebase constitution audit --type performance
  codebase constitution audit --type security --domain auth
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			ctx := context.Background()
			if flagSeed {
				return runAuditSeed(ctx, c.Graph)
			}
			return runAudit(ctx, c.Graph, flagAuditType, flagDomain, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagAuditType, "type", "", "Audit type filter: security | performance (default: all)")
	cmd.Flags().BoolVar(&flagSeed, "seed", false, "Seed built-in security/performance rules into the constitution")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter APIEndpoint objects by domain")
	return cmd
}

// runAuditSeed upserts all built-in audit rules and wires them to constitution-v1.
func runAuditSeed(ctx context.Context, gc *sdkgraph.Client) error {
	constResp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Key: "constitution-v1", Type: "Constitution", Limit: 1})
	if err != nil {
		return fmt.Errorf("listing constitution: %w", err)
	}
	var constID string
	for _, obj := range constResp.Items {
		if obj.Key != nil && *obj.Key == "constitution-v1" {
			constID = obj.EntityID
			break
		}
	}
	if constID == "" {
		return fmt.Errorf("constitution-v1 not found — run 'codebase constitution create' first")
	}

	seeded := 0
	for _, rule := range securityPerformanceRules {
		key := rule.Key
		props := map[string]any{
			"name":       rule.Name,
			"statement":  rule.Statement,
			"category":   rule.Category,
			"audit_type": rule.AuditType,
		}
		if rule.AppliesTo != "" {
			props["applies_to"] = rule.AppliesTo
		}
		if rule.PropCheck != "" {
			props["prop_check"] = rule.PropCheck
		}
		if rule.AutoCheck != "" {
			props["auto_check"] = rule.AutoCheck
		}
		if rule.HowToVerify != "" {
			props["how_to_verify"] = rule.HowToVerify
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
			SrcID: constID,
			DstID: ruleObj.EntityID,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: wiring %s: %v\n", rule.Key, err)
		}
		fmt.Printf("  seeded  %-52s  [%s]\n", rule.Key, rule.AuditType)
		seeded++
	}

	fmt.Printf("\n%d audit rules seeded into constitution-v1\n", seeded)
	return nil
}

// auditResult is the evaluation of one audit rule against graph objects.
type auditResult struct {
	Rule       *sdkgraph.GraphObject
	AuditType  string
	Mode       string // "prop", "auto", "review"
	Passes     []string
	Fails      []string
	ReviewHint string
}

func runAudit(ctx context.Context, gc *sdkgraph.Client, auditTypeFilter, domain, format string) error {
	rulesResp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: "Rule", Limit: 500})
	if err != nil {
		return fmt.Errorf("listing rules: %w", err)
	}

	// Filter to audit rules matching the type filter
	var auditRules []*sdkgraph.GraphObject
	for _, r := range rulesResp.Items {
		at := anyPropStr(r, "audit_type")
		if at == "" {
			continue
		}
		if auditTypeFilter != "" && !strings.Contains(strings.ToLower(at), strings.ToLower(auditTypeFilter)) {
			continue
		}
		auditRules = append(auditRules, r)
	}

	if len(auditRules) == 0 {
		fmt.Println("No audit rules found. Run: codebase constitution audit --seed")
		return nil
	}

	// Collect all object types needed
	neededTypes := map[string]bool{}
	for _, r := range auditRules {
		for _, t := range strings.Split(strProp(r, "applies_to"), ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				neededTypes[t] = true
			}
		}
	}

	// Fetch objects per type
	objsByType := map[string][]*sdkgraph.GraphObject{}
	for t := range neededTypes {
		resp, err := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: t, Limit: 1000})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: listing %s: %v\n", t, err)
			continue
		}
		items := resp.Items
		if domain != "" && t == "APIEndpoint" {
			var filtered []*sdkgraph.GraphObject
			for _, o := range items {
				if strProp(o, "domain") == domain {
					filtered = append(filtered, o)
				}
			}
			items = filtered
		}
		objsByType[t] = items
	}

	// Evaluate each audit rule
	var results []auditResult
	for _, rule := range auditRules {
		at := anyPropStr(rule, "audit_type")
		appliesTo := strings.TrimSpace(strings.Split(strProp(rule, "applies_to"), ",")[0])
		objs := objsByType[appliesTo]

		res := auditResult{
			Rule:      rule,
			AuditType: at,
		}

		propCheckRaw := strProp(rule, "prop_check")
		autoCheck := strProp(rule, "auto_check")
		howTo := strProp(rule, "how_to_verify")

		switch {
		case propCheckRaw != "":
			res.Mode = "prop"
			var spec propCheckSpec
			if err := json.Unmarshal([]byte(propCheckRaw), &spec); err != nil {
				res.Mode = "review"
				res.ReviewHint = howTo
				break
			}
			for _, obj := range objs {
				key := derefKey(obj.Key)
				val := anyPropStr(obj, spec.Field)
				if checkPropSpec(spec, val) {
					res.Passes = append(res.Passes, key)
				} else {
					res.Fails = append(res.Fails, fmt.Sprintf("%s  (%s=%q)", key, spec.Field, val))
				}
			}

		case autoCheck != "":
			res.Mode = "auto"
			rx, err := regexp.Compile(autoCheck)
			if err != nil {
				res.Mode = "review"
				res.ReviewHint = howTo
				break
			}
			for _, obj := range objs {
				key := derefKey(obj.Key)
				if rx.MatchString(key) {
					res.Passes = append(res.Passes, key)
				} else {
					res.Fails = append(res.Fails, key)
				}
			}

		default:
			res.Mode = "review"
			res.ReviewHint = howTo
		}

		results = append(results, res)
	}

	// Sort: fails first, then review, then passes
	sort.Slice(results, func(i, j int) bool {
		pri := func(r auditResult) int {
			if (r.Mode == "prop" || r.Mode == "auto") && len(r.Fails) > 0 {
				return 0
			}
			if r.Mode == "review" {
				return 1
			}
			return 2
		}
		return pri(results[i]) < pri(results[j])
	})

	if format == "json" {
		return printAuditJSON(results)
	}
	printAuditReport(results, auditTypeFilter, domain)
	return nil
}

func printAuditReport(results []auditResult, typeFilter, domain string) {
	now := time.Now().Format("2006-01-02")
	title := "SECURITY + PERFORMANCE AUDIT"
	if typeFilter != "" {
		title = strings.ToUpper(typeFilter) + " AUDIT"
	}
	fmt.Printf("┌─ %s\n", title)
	fmt.Printf("  Generated: %s", now)
	if domain != "" {
		fmt.Printf("  · domain: %s", domain)
	}
	fmt.Printf("\n\n")

	secFails, perfFails, reviewCount, passCount := 0, 0, 0, 0

	for _, res := range results {
		rKey := derefKey(res.Rule.Key)
		name := strProp(res.Rule, "name")
		at := res.AuditType
		atTag := fmt.Sprintf("[%s]", strings.ToUpper(at))

		switch res.Mode {
		case "prop", "auto":
			if len(res.Fails) == 0 {
				passCount++
				fmt.Printf("✓  %-14s  %-44s  %d/%d pass\n", atTag, rKey, len(res.Passes), len(res.Passes))
			} else {
				if strings.Contains(at, "security") {
					secFails += len(res.Fails)
				} else {
					perfFails += len(res.Fails)
				}
				fmt.Printf("✗  %-14s  %-44s  %d fail / %d pass\n", atTag, rKey, len(res.Fails), len(res.Passes))
				fmt.Printf("   %s\n", name)
				shown := res.Fails
				if len(shown) > 8 {
					shown = shown[:8]
				}
				for _, f := range shown {
					fmt.Printf("     • %s\n", f)
				}
				if len(res.Fails) > 8 {
					fmt.Printf("     … and %d more\n", len(res.Fails)-8)
				}
			}

		case "review":
			reviewCount++
			fmt.Printf("?  %-14s  %-44s\n", atTag, rKey)
			fmt.Printf("   %s\n", name)
			if res.ReviewHint != "" {
				hint := res.ReviewHint
				if len(hint) > 200 {
					hint = hint[:197] + "..."
				}
				fmt.Printf("   → %s\n", hint)
			}
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("─", 72))
	fmt.Printf("Summary:  ✓ %d pass  ✗ %d security violations  ✗ %d performance violations  ? %d manual review\n",
		passCount, secFails, perfFails, reviewCount)
	fmt.Println()
	fmt.Println("Legend: ✓ verified  ✗ violation  ? AI must review manually")
}

func printAuditJSON(results []auditResult) error {
	type jsonResult struct {
		RuleKey    string   `json:"rule_key"`
		Name       string   `json:"name"`
		AuditType  string   `json:"audit_type"`
		Mode       string   `json:"mode"`
		PassCount  int      `json:"pass_count"`
		FailCount  int      `json:"fail_count"`
		Fails      []string `json:"fails,omitempty"`
		ReviewHint string   `json:"review_hint,omitempty"`
	}
	var out []jsonResult
	for _, r := range results {
		jr := jsonResult{
			RuleKey:    derefKey(r.Rule.Key),
			Name:       strProp(r.Rule, "name"),
			AuditType:  r.AuditType,
			Mode:       r.Mode,
			PassCount:  len(r.Passes),
			FailCount:  len(r.Fails),
			ReviewHint: r.ReviewHint,
		}
		if len(r.Fails) > 0 {
			jr.Fails = r.Fails
		}
		out = append(out, jr)
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
