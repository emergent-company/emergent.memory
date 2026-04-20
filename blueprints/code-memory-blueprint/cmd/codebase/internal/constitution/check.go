package constitutioncmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/spf13/cobra"
)

// propCheckSpec is the parsed form of a rule's prop_check JSON property.
// Example: {"field":"method","nonempty":true}
// Example: {"field":"path","prefix":"/"}
// Example: {"field":"auth_required","bool":true}
type propCheckSpec struct {
	Field    string `json:"field"`
	Nonempty bool   `json:"nonempty"` // value must be non-empty string
	Prefix   string `json:"prefix"`   // value must start with this prefix
	Bool     bool   `json:"bool"`     // value must be "true" or "false" (not absent)
}

func newCheckCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var (
		flagObjType  string
		flagCategory string
		flagDomain   string
		flagRepo     string
	)
	cwd, _ := os.Getwd()

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Run constitution rules against graph objects and source files",
		Long: `Evaluate constitution rules. Three modes depending on the rule:

  auto_check   — regex applied to object keys. Fully automatic: pass/fail.
  scan_pattern — ripgrep run against source files. Shows match count + samples.
  (neither)    — shows how_to_verify hint for the AI to act on.

Examples:
  codebase constitution check --type APIEndpoint
  codebase constitution check --type APIEndpoint --category api
  codebase constitution check --type Domain --repo /path/to/repo
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagObjType == "" {
				return fmt.Errorf("--type is required")
			}
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}
			ctx := context.Background()
			return runCheck(ctx, c.Graph, flagObjType, flagCategory, flagDomain, flagRepo, *flagFormat)
		},
	}

	cmd.Flags().StringVar(&flagObjType, "type", "", "Object type to check (e.g. APIEndpoint, Service, Domain)")
	cmd.Flags().StringVar(&flagCategory, "category", "", "Filter rules by category")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter objects by domain property")
	cmd.Flags().StringVar(&flagRepo, "repo", cwd, "Repo root for scan_pattern searches")
	return cmd
}

// ruleEval is the result of evaluating one rule.
type ruleEval struct {
	Rule *sdkgraph.GraphObject
	Mode string // "auto", "prop", "scan", "review"
	// auto + prop mode
	Passes []string
	Fails  []string
	// scan mode
	ScanMatches    int
	ScanSamples    []string // up to 5 lines
	ScanViolations []string // files with 0 matches when matches expected, or vice versa
	// review mode — nothing to compute, just show how_to_verify
}

func runCheck(ctx context.Context, gc *sdkgraph.Client, objType, category, domain, repo, format string) error {
	// Load objects and rules in parallel
	objCh := make(chan []*sdkgraph.GraphObject, 1)
	ruleCh := make(chan []*sdkgraph.GraphObject, 1)

	go func() {
		resp, _ := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: 1000})
		if resp != nil {
			objCh <- resp.Items
		} else {
			objCh <- nil
		}
	}()
	go func() {
		resp, _ := gc.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: "Rule", Limit: 500})
		if resp != nil {
			ruleCh <- resp.Items
		} else {
			ruleCh <- nil
		}
	}()

	objs := <-objCh
	rules := <-ruleCh

	// Filter objects by domain
	if domain != "" {
		var filtered []*sdkgraph.GraphObject
		for _, o := range objs {
			if strProp(o, "domain") == domain {
				filtered = append(filtered, o)
			}
		}
		objs = filtered
	}

	// Filter rules
	var relevantRules []*sdkgraph.GraphObject
	for _, r := range rules {
		if category != "" && !strings.EqualFold(strProp(r, "category"), category) {
			continue
		}
		appliesTo := strProp(r, "applies_to")
		if appliesTo != "" && !containsType(appliesTo, objType) {
			continue
		}
		relevantRules = append(relevantRules, r)
	}

	if len(relevantRules) == 0 {
		fmt.Printf("No rules found for type %q", objType)
		if category != "" {
			fmt.Printf(" category %q", category)
		}
		fmt.Println(". Add rules with: codebase constitution add-rule")
		return nil
	}

	// Evaluate each rule
	var evals []ruleEval
	for _, rule := range relevantRules {
		ev := ruleEval{Rule: rule}
		autoCheck := strProp(rule, "auto_check")
		scanPattern := strProp(rule, "scan_pattern")
		scanTarget := strProp(rule, "scan_target")

		propCheckRaw := strProp(rule, "prop_check")

		switch {
		case autoCheck != "":
			ev.Mode = "auto"
			rx, err := regexp.Compile(autoCheck)
			if err != nil {
				ev.Mode = "review"
				break
			}
			for _, obj := range objs {
				key := derefKey(obj.Key)
				if rx.MatchString(key) {
					ev.Passes = append(ev.Passes, key)
				} else {
					ev.Fails = append(ev.Fails, key)
				}
			}

		case propCheckRaw != "":
			ev.Mode = "prop"
			var spec propCheckSpec
			if err := json.Unmarshal([]byte(propCheckRaw), &spec); err != nil {
				ev.Mode = "review"
				break
			}
			for _, obj := range objs {
				key := derefKey(obj.Key)
				val := anyPropStr(obj, spec.Field)
				ok := checkPropSpec(spec, val)
				if ok {
					ev.Passes = append(ev.Passes, key)
				} else {
					ev.Fails = append(ev.Fails, fmt.Sprintf("%s  (%s=%q)", key, spec.Field, val))
				}
			}

		case scanPattern != "":
			ev.Mode = "scan"
			matches, samples, err := runRgScan(repo, scanPattern, scanTarget)
			if err != nil {
				ev.Mode = "review" // rg not available, fall back
			} else {
				ev.ScanMatches = matches
				ev.ScanSamples = samples
			}

		default:
			ev.Mode = "review"
		}

		evals = append(evals, ev)
	}

	if format == "json" {
		return printCheckJSON(evals, objs)
	}

	printCheckTable(evals, objs, objType)
	return nil
}

// checkPropSpec returns true if val satisfies the propCheckSpec.
func checkPropSpec(spec propCheckSpec, val string) bool {
	if spec.Bool {
		// must be exactly "true" or "false"
		return val == "true" || val == "false"
	}
	if spec.Prefix != "" {
		return strings.HasPrefix(val, spec.Prefix)
	}
	if spec.Nonempty {
		return strings.TrimSpace(val) != ""
	}
	// default: just non-empty
	return strings.TrimSpace(val) != ""
}

// runRgScan runs ripgrep and returns match count + up to 5 sample lines.
func runRgScan(repo, pattern, target string) (int, []string, error) {
	// Expand glob target relative to repo
	var paths []string
	if target != "" {
		matches, err := filepath.Glob(filepath.Join(repo, target))
		if err != nil || len(matches) == 0 {
			// Try without repo prefix (absolute or already resolved)
			matches, _ = filepath.Glob(target)
		}
		paths = matches
	}

	args := []string{"--no-heading", "-n", pattern}
	if len(paths) > 0 {
		args = append(args, paths...)
	} else {
		args = append(args, repo)
	}

	cmd := exec.Command("rg", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	_ = cmd.Run() // rg exits 1 when no matches — not an error for us

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var nonEmpty []string
	for _, l := range lines {
		if l != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}

	samples := nonEmpty
	if len(samples) > 5 {
		samples = samples[:5]
	}
	return len(nonEmpty), samples, nil
}

func printCheckTable(evals []ruleEval, objs []*sdkgraph.GraphObject, objType string) {
	// Sort: fails first (auto+prop), then scan, then review, then passes
	sort.Slice(evals, func(i, j int) bool {
		priority := func(e ruleEval) int {
			if (e.Mode == "auto" || e.Mode == "prop") && len(e.Fails) > 0 {
				return 0
			}
			if e.Mode == "scan" {
				return 1
			}
			if e.Mode == "review" {
				return 2
			}
			return 3 // auto/prop pass
		}
		return priority(evals[i]) < priority(evals[j])
	})

	fmt.Printf("Constitution check: %s  (%d objects · %d rules)\n", objType, len(objs), len(evals))
	fmt.Println(strings.Repeat("─", 72))

	autoFails := 0
	autoPass := 0
	scanRules := 0
	reviewRules := 0

	for _, ev := range evals {
		rKey := derefKey(ev.Rule.Key)
		stmt := strProp(ev.Rule, "statement")

		switch ev.Mode {
		case "auto", "prop":
			if len(ev.Fails) == 0 {
				autoPass++
				fmt.Printf("✓  %-42s  %d/%d pass\n", rKey, len(ev.Passes), len(ev.Passes))
			} else {
				autoFails += len(ev.Fails)
				fmt.Printf("✗  %-42s  %d fail / %d pass\n", rKey, len(ev.Fails), len(ev.Passes))
				shown := ev.Fails
				if len(shown) > 5 {
					shown = shown[:5]
				}
				for _, f := range shown {
					fmt.Printf("     • %s\n", f)
				}
				if len(ev.Fails) > 5 {
					fmt.Printf("     … and %d more\n", len(ev.Fails)-5)
				}
			}

		case "scan":
			scanRules++
			scanPattern := strProp(ev.Rule, "scan_pattern")
			howTo := strProp(ev.Rule, "how_to_verify")
			fmt.Printf("~  %-42s  scan: %d matches\n", rKey, ev.ScanMatches)
			fmt.Printf("   pattern: %s\n", scanPattern)
			if ev.ScanMatches > 0 {
				fmt.Printf("   samples:\n")
				for _, s := range ev.ScanSamples {
					// trim long lines
					if len(s) > 120 {
						s = s[:117] + "..."
					}
					fmt.Printf("     %s\n", s)
				}
				if ev.ScanMatches > 5 {
					fmt.Printf("     … %d more matches\n", ev.ScanMatches-5)
				}
			} else {
				fmt.Printf("   no matches found\n")
			}
			if howTo != "" {
				fmt.Printf("   verify: %s\n", howTo)
			}

		case "review":
			reviewRules++
			howTo := strProp(ev.Rule, "how_to_verify")
			fmt.Printf("?  %-42s\n", rKey)
			if howTo != "" {
				fmt.Printf("   verify: %s\n", howTo)
			} else {
				if len(stmt) > 100 {
					stmt = stmt[:97] + "..."
				}
				fmt.Printf("   rule:   %s\n", stmt)
			}
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("─", 72))
	fmt.Printf("Summary:  ✓ %d auto-pass  ✗ %d auto-fail  ~ %d scan  ? %d review\n",
		autoPass, autoFails, scanRules, reviewRules)
	fmt.Println()
	fmt.Println("Legend: ✓ auto-verified  ✗ violation found  ~ scan evidence (AI interprets)  ? AI must verify")
}

func printCheckJSON(evals []ruleEval, objs []*sdkgraph.GraphObject) error {
	type jsonEval struct {
		RuleKey     string   `json:"rule_key"`
		Mode        string   `json:"mode"`
		Passes      []string `json:"passes,omitempty"`
		Fails       []string `json:"fails,omitempty"`
		PassCount   int      `json:"pass_count,omitempty"`
		FailCount   int      `json:"fail_count,omitempty"`
		ScanMatches int      `json:"scan_matches,omitempty"`
		ScanSamples []string `json:"scan_samples,omitempty"`
		HowToVerify string   `json:"how_to_verify,omitempty"`
		Statement   string   `json:"statement,omitempty"`
	}
	var out []jsonEval
	for _, ev := range evals {
		e := jsonEval{
			RuleKey:     derefKey(ev.Rule.Key),
			Mode:        ev.Mode,
			ScanMatches: ev.ScanMatches,
			ScanSamples: ev.ScanSamples,
			HowToVerify: strProp(ev.Rule, "how_to_verify"),
			Statement:   strProp(ev.Rule, "statement"),
			PassCount:   len(ev.Passes),
			FailCount:   len(ev.Fails),
		}
		// For auto/prop: include fails (violations), omit full passes list to keep output compact
		if ev.Mode == "auto" || ev.Mode == "prop" {
			e.Fails = ev.Fails
		}
		out = append(out, e)
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func containsType(appliesTo, typ string) bool {
	for _, t := range strings.Split(appliesTo, ",") {
		if strings.EqualFold(strings.TrimSpace(t), typ) {
			return true
		}
	}
	return false
}
