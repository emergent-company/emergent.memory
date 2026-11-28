// Package pipeline defines the per-project knowledge-pipeline policy: a single
// tunable configuration that governs how discovery, schema application,
// extraction and branch merging behave without a human in the loop.
//
// The policy is stored resolved (not as sparse overrides) so that reading it
// back always yields the exact behaviour the server will enforce. The project
// also stores a mode label (off | suggest | auto) as a convenience selector;
// DefaultPolicy(mode) is the baseline a PUT with only a mode restores.
package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Modes. Suggest is the safe default: discovery runs automatically and proposes
// a candidate pack, but nothing is applied or re-extracted without a person.
const (
	ModeOff     = "off"
	ModeSuggest = "suggest"
	ModeAuto    = "auto"
)

// Re-extraction modes for Extract.ReExtractOnSchemaChange: what an auto-apply
// re-runs against documents once the effective schema changes.
const (
	ReExtractOff         = "off"
	ReExtractIncremental = "incremental"
	ReExtractFull        = "full"
)

// ValidModes lists the accepted mode labels in display order.
var ValidModes = []string{ModeOff, ModeSuggest, ModeAuto}

// Policy is the resolved knowledge-pipeline configuration for one project.
type Policy struct {
	Mode string `json:"mode"`

	Discovery DiscoveryPolicy `json:"discovery"`
	Apply     ApplyPolicy     `json:"apply"`
	Extract   ExtractPolicy   `json:"extract"`
	Merge     MergePolicy     `json:"merge"`
	Budget    BudgetPolicy    `json:"budget"`
}

// DiscoveryPolicy governs schema-discovery runs.
type DiscoveryPolicy struct {
	MinConfidence        float32 `json:"min_confidence"`
	BatchSize            int     `json:"batch_size"`
	MaxDocsPerRun        int     `json:"max_docs_per_run"`
	MaxTypeBudget        int     `json:"max_type_budget"`
	MaxTotalTypes        int     `json:"max_total_types"`
	StabilityRuns        int     `json:"stability_runs"`
	IncludeRelationships bool    `json:"include_relationships"`
	DebounceHours        int     `json:"debounce_hours"`
}

// ApplyPolicy governs auto-applying a discovered schema.
type ApplyPolicy struct {
	AdditiveOnly           bool   `json:"additive_only"`
	MaxNewTypesPerRun      int    `json:"max_new_types_per_run"`
	QuarantineUnknownTypes bool   `json:"quarantine_unknown_types"`
	Target                 string `json:"target"` // "auto_pack" only in v1
}

// ExtractPolicy governs extraction and schema-change backfill.
type ExtractPolicy struct {
	ReExtractOnSchemaChange  string `json:"re_extract_on_schema_change"` // off | incremental | full
	BackfillLimitPerRun      int    `json:"backfill_limit_per_run"`
	HoldExtractionUntilApply bool   `json:"hold_extraction_until_apply"`
	StrictTypes              bool   `json:"strict_types"`
}

// MergePolicy governs auto-merging extraction staging branches.
type MergePolicy struct {
	// Policy selects the branch-merge preset: partial (safe default), enrich,
	// or partial_newest (partial plus opt-in same-entity drift resolution).
	Policy               string  `json:"policy"` // partial | enrich | partial_newest
	SimilarityThreshold  float32 `json:"similarity_threshold"`
	BranchRetentionDays  int     `json:"branch_retention_days"`
	SweepIntervalMinutes int     `json:"sweep_interval_minutes"`
}

// BudgetPolicy sets per-day spend ceilings for the automated parts of the
// pipeline, independent of the project's weekly/monthly budget windows.
type BudgetPolicy struct {
	DiscoverySpendCapUSDPerDay  float64 `json:"discovery_spend_cap_usd_per_day"`
	ExtractionSpendCapUSDPerDay float64 `json:"extraction_spend_cap_usd_per_day"`
}

// DefaultPolicy returns the baseline for a mode. Unknown modes fall back to
// suggest (fail-safe), matching Validate's rejection only after validation.
func DefaultPolicy(mode string) Policy {
	p := suggest()
	switch mode {
	case ModeOff:
		p = off()
	case ModeAuto:
		p = auto()
	}
	return p
}

func off() Policy {
	p := suggest()
	p.Mode = ModeOff
	p.Discovery.MinConfidence = 0.75
	return p
}

func suggest() Policy {
	return Policy{
		Mode: ModeSuggest,
		Discovery: DiscoveryPolicy{
			MinConfidence:        0.75,
			BatchSize:            5,
			MaxDocsPerRun:        200,
			MaxTypeBudget:        20,
			MaxTotalTypes:        40,
			StabilityRuns:        2,
			IncludeRelationships: true,
			DebounceHours:        6,
		},
		Apply: ApplyPolicy{
			AdditiveOnly:           true,
			MaxNewTypesPerRun:      5,
			QuarantineUnknownTypes: true,
			Target:                 "auto_pack",
		},
		Extract: ExtractPolicy{
			ReExtractOnSchemaChange: ReExtractOff,
			BackfillLimitPerRun:     50,
			StrictTypes:             true,
		},
		Merge: MergePolicy{
			Policy:               "partial",
			SimilarityThreshold:  0.92,
			BranchRetentionDays:  7,
			SweepIntervalMinutes: 30,
		},
	}
}

func auto() Policy {
	p := suggest()
	p.Mode = ModeAuto
	p.Extract.ReExtractOnSchemaChange = ReExtractIncremental
	return p
}

// IsZero reports whether p carries no configured value (an unmarshalled empty
// policy). A zero policy resolves to DefaultPolicy(mode).
func (p Policy) IsZero() bool {
	return reflect.DeepEqual(p, Policy{})
}

// ValidateMode checks a mode label in isolation (used by the mode-only update
// path, which validates the mode before resetting the policy to that preset).
func ValidateMode(mode string) error {
	if mode == "" {
		return fmt.Errorf("mode is required")
	}
	if !validMode(mode) {
		return fmt.Errorf("mode must be one of %v", ValidModes)
	}
	return nil
}

// Validate checks the policy's enum values and numeric ranges. Enum fields are
// required (no empty fallthrough): a policy is always fully resolved, so a
// blank enum means a malformed write rather than "use the default".
func Validate(p Policy) error {
	if err := ValidateMode(p.Mode); err != nil {
		return err
	}
	if p.Discovery.MinConfidence < 0 || p.Discovery.MinConfidence > 1 {
		return fmt.Errorf("discovery.min_confidence must be within [0,1]")
	}
	if p.Discovery.BatchSize < 0 || p.Discovery.BatchSize > 50 {
		return fmt.Errorf("discovery.batch_size must be within [0,50]")
	}
	if p.Discovery.MaxDocsPerRun < 0 || p.Discovery.MaxDocsPerRun > 5000 {
		return fmt.Errorf("discovery.max_docs_per_run must be within [0,5000]")
	}
	if p.Discovery.MaxTypeBudget < 0 || p.Discovery.MaxTypeBudget > 100 {
		return fmt.Errorf("discovery.max_type_budget must be within [0,100]")
	}
	if p.Discovery.MaxTotalTypes < 0 || p.Discovery.MaxTotalTypes > 200 {
		return fmt.Errorf("discovery.max_total_types must be within [0,200]")
	}
	if p.Discovery.StabilityRuns < 0 || p.Discovery.StabilityRuns > 10 {
		return fmt.Errorf("discovery.stability_runs must be within [0,10]")
	}
	if p.Discovery.DebounceHours < 0 || p.Discovery.DebounceHours > 168 {
		return fmt.Errorf("discovery.debounce_hours must be within [0,168]")
	}
	if p.Apply.MaxNewTypesPerRun < 0 || p.Apply.MaxNewTypesPerRun > 100 {
		return fmt.Errorf("apply.max_new_types_per_run must be within [0,100]")
	}
	if p.Apply.Target != "auto_pack" {
		return fmt.Errorf("apply.target must be \"auto_pack\"")
	}
	switch p.Extract.ReExtractOnSchemaChange {
	case ReExtractOff, ReExtractIncremental, ReExtractFull:
	default:
		return fmt.Errorf("extract.re_extract_on_schema_change must be off, incremental or full")
	}
	if p.Extract.BackfillLimitPerRun < 0 || p.Extract.BackfillLimitPerRun > 1000 {
		return fmt.Errorf("extract.backfill_limit_per_run must be within [0,1000]")
	}
	switch p.Merge.Policy {
	case "partial", "enrich", "partial_newest":
	default:
		return fmt.Errorf("merge.policy must be partial, enrich or partial_newest")
	}
	if p.Merge.SimilarityThreshold < 0 || p.Merge.SimilarityThreshold > 1 {
		return fmt.Errorf("merge.similarity_threshold must be within [0,1]")
	}
	if p.Merge.BranchRetentionDays < 0 || p.Merge.BranchRetentionDays > 365 {
		return fmt.Errorf("merge.branch_retention_days must be within [0,365]")
	}
	if p.Merge.SweepIntervalMinutes < 0 || p.Merge.SweepIntervalMinutes > 1440 {
		return fmt.Errorf("merge.sweep_interval_minutes must be within [0,1440]")
	}
	if p.Budget.DiscoverySpendCapUSDPerDay < 0 || p.Budget.ExtractionSpendCapUSDPerDay < 0 {
		return fmt.Errorf("budget caps must be non-negative")
	}
	return nil
}

func validMode(m string) bool {
	for _, v := range ValidModes {
		if v == m {
			return true
		}
	}
	return false
}

// Equal reports whether two policies are identical (used to detect
// customization against a preset baseline).
func (p Policy) Equal(o Policy) bool {
	return reflect.DeepEqual(p, o)
}

// Marshal returns the JSON encoding of p.
func (p Policy) Marshal() json.RawMessage {
	b, _ := json.Marshal(p)
	return b
}

// Parse decodes raw into a Policy. An empty raw yields a zero Policy. Unknown
// keys are rejected so a typo cannot silently zero a knob.
func Parse(raw json.RawMessage) (Policy, error) {
	if len(raw) == 0 {
		return Policy{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p Policy
	if err := dec.Decode(&p); err != nil {
		return Policy{}, err
	}
	return p, nil
}

// Resolve returns the effective policy for a project: the stored policy, or the
// mode's preset when the stored value is empty (legacy rows). It is the single
// resolution path — callers must not parse the stored JSON independently, or
// they diverge from the auto-apply gate on mode/preset fallback.
func Resolve(mode string, raw json.RawMessage) (Policy, error) {
	p, err := Parse(raw)
	if err != nil {
		return Policy{}, err
	}
	if p.IsZero() {
		return DefaultPolicy(mode), nil
	}
	return p, nil
}

// parseSectionKeys are the top-level sections a complete policy must carry.
var parseSectionKeys = []string{"mode", "discovery", "apply", "extract", "merge", "budget"}

// ParseComplete decodes a policy for a write: every top-level section and every
// field inside it must be present, no unknown top-level or nested key is
// allowed, and Mode must be a non-empty known value. It rejects sparse
// documents so a partial PUT can never silently zero the safety defaults the
// caller did not mention.
func ParseComplete(raw json.RawMessage) (Policy, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Policy{}, fmt.Errorf("policy must be a JSON object: %w", err)
	}
	for _, k := range parseSectionKeys {
		if _, ok := obj[k]; !ok {
			return Policy{}, fmt.Errorf("policy is incomplete: missing %q — send the full resolved policy", k)
		}
	}
	for k := range obj {
		found := false
		for _, known := range parseSectionKeys {
			if k == known {
				found = true
				break
			}
		}
		if !found {
			return Policy{}, fmt.Errorf("unknown policy key %q", k)
		}
	}
	// Every nested field must be present too: a present-but-empty section is the
	// same silent-zero hazard the top-level check prevents.
	for _, section := range []struct {
		key    string
		sample any
	}{
		{"discovery", DiscoveryPolicy{}},
		{"apply", ApplyPolicy{}},
		{"extract", ExtractPolicy{}},
		{"merge", MergePolicy{}},
		{"budget", BudgetPolicy{}},
	} {
		var sub map[string]json.RawMessage
		if err := json.Unmarshal(obj[section.key], &sub); err != nil {
			return Policy{}, fmt.Errorf("policy.%s must be an object: %w", section.key, err)
		}
		t := reflect.TypeOf(section.sample)
		for i := 0; i < t.NumField(); i++ {
			name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			if _, ok := sub[name]; !ok {
				return Policy{}, fmt.Errorf("policy is incomplete: missing %q — send the full resolved policy", section.key+"."+name)
			}
		}
	}
	p, err := Parse(raw)
	if err != nil {
		return Policy{}, err
	}
	if p.Mode == "" {
		return Policy{}, fmt.Errorf("policy.mode is required")
	}
	return p, nil
}
