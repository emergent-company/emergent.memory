package skills

import (
	"fmt"
	"os"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Skill-capture modes. The project-level value is the master switch; a typed
// per-agent override may only constrain it (see ResolveCaptureMode).
const (
	// SkillCaptureOff disables capture entirely (the default for every project).
	SkillCaptureOff = "off"
	// SkillCaptureSuggest stages proposals for human approval.
	SkillCaptureSuggest = "suggest"
	// SkillCaptureAuto applies proposals that satisfy every guardrail.
	SkillCaptureAuto = "auto"
)

// EnvSkillCaptureDisabled is the process-wide kill switch. Any value other than
// "" / "0" / "false" disables capture for every project without a database
// migration, so an operator can stop the loop during an incident.
const EnvSkillCaptureDisabled = "MEMORY_SKILL_CAPTURE_DISABLED"

// ProjectSkillCaptureConfig is the project-level capture policy stored in
// kb.projects.skill_capture. The zero value (all fields empty) means "off".
//
// Linter and Scan are pointers so an explicit false is distinguishable from
// "unset, therefore default true".
type ProjectSkillCaptureConfig struct {
	Mode string `json:"mode,omitempty"`
	// AutoApply is the project-level second key. Auto-application requires this
	// to be true AND mode == "auto" AND the per-agent override not to opt out.
	AutoApply bool `json:"autoApply,omitempty"`
	// ReviewModel optionally overrides the model used by the background review.
	// Empty means the run's model, then the project default.
	ReviewModel string `json:"reviewModel,omitempty"`
	// NudgeInterval is how many completed runs may pass without a capture
	// attempt before the next run is reviewed. 0 means the default (3).
	NudgeInterval int `json:"nudgeInterval,omitempty"`
	// MaxInputTokens bounds the digest sent to the review model.
	MaxInputTokens int `json:"maxInputTokens,omitempty"`
	// MaxCandidates bounds how many proposals one review may stage.
	MaxCandidates int `json:"maxCandidates,omitempty"`
	// MaxSkillBytes bounds a captured skill's content size.
	MaxSkillBytes int `json:"maxSkillBytes,omitempty"`
	// Linter enables the advisory linter (default true). The security scan is
	// deliberately not configurable: it is an unconditional hard gate on every
	// staged candidate.
	Linter *bool `json:"linter,omitempty"`
	// MinApprovedBeforeAuto is the earned-trust floor: the agent must have had
	// at least this many proposals approved by a human before auto-apply starts.
	MinApprovedBeforeAuto int `json:"minApprovedBeforeAuto,omitempty"`
	// AutoApplyConfidence is the minimum model confidence for auto-apply (0..1).
	AutoApplyConfidence float64 `json:"autoApplyConfidence,omitempty"`
	// AutoCreatesPerDay / AutoPatchesPerDay are per-agent rate caps.
	AutoCreatesPerDay int `json:"autoCreatesPerDay,omitempty"`
	AutoPatchesPerDay int `json:"autoPatchesPerDay,omitempty"`
	// MaxAgentSkills caps how many learned skills one agent may own.
	MaxAgentSkills int `json:"maxAgentSkills,omitempty"`
	// DedupThreshold is the cosine similarity above which a candidate is
	// considered a duplicate of an existing skill.
	DedupThreshold float64 `json:"dedupThreshold,omitempty"`
	// StaleAfterDays / ArchiveAfterDays drive the deterministic curator for
	// agent-created, unpinned, unlocked skills.
	StaleAfterDays   int `json:"staleAfterDays,omitempty"`
	ArchiveAfterDays int `json:"archiveAfterDays,omitempty"`
}

// AgentSkillCapture is the typed per-agent override stored in
// kb.agent_definitions.skill_capture. Both fields are pointers so "unset"
// inherits the project value; the override can only narrow the project policy.
type AgentSkillCapture struct {
	Mode      *string `json:"mode,omitempty"`
	AutoApply *bool   `json:"autoApply,omitempty"`
}

// Defaults for the normalized policy.
const (
	defaultNudgeInterval         = 3
	defaultMaxInputTokens        = 12000
	defaultMaxCandidates         = 3
	defaultMaxSkillBytes         = 32 * 1024
	defaultMinApprovedBeforeAuto = 1
	defaultAutoApplyConfidence   = 0.80
	defaultAutoCreatesPerDay     = 1
	defaultAutoPatchesPerDay     = 3
	defaultMaxAgentSkills        = 25
	defaultDedupThreshold        = 0.85
	defaultStaleAfterDays        = 45
	defaultArchiveAfterDays      = 120
)

// SkillCaptureDisabled returns true when the process-wide kill switch is set.
func SkillCaptureDisabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(EnvSkillCaptureDisabled)))
	return v != "" && v != "0" && v != "false" && v != "no"
}

// Normalized returns a copy with every unset value replaced by its default.
// It never changes the mode: an unset mode stays "" (off) so the feature is
// opt-in only.
func (c ProjectSkillCaptureConfig) Normalized() ProjectSkillCaptureConfig {
	out := c
	out.Mode = strings.TrimSpace(strings.ToLower(out.Mode))
	if out.Mode == "" {
		out.Mode = SkillCaptureOff
	}
	if out.NudgeInterval <= 0 {
		out.NudgeInterval = defaultNudgeInterval
	}
	if out.MaxInputTokens <= 0 {
		out.MaxInputTokens = defaultMaxInputTokens
	}
	if out.MaxCandidates <= 0 {
		out.MaxCandidates = defaultMaxCandidates
	}
	if out.MaxSkillBytes <= 0 {
		out.MaxSkillBytes = defaultMaxSkillBytes
	}
	if out.Linter == nil {
		v := true
		out.Linter = &v
	}
	if out.MinApprovedBeforeAuto < 1 {
		// 0 / unset means the default floor. Autonomy is always earned: the
		// policy cannot be configured down to "the first proposal is automatic".
		out.MinApprovedBeforeAuto = defaultMinApprovedBeforeAuto
	}
	if out.AutoApplyConfidence <= 0 {
		out.AutoApplyConfidence = defaultAutoApplyConfidence
	}
	if out.AutoCreatesPerDay <= 0 {
		out.AutoCreatesPerDay = defaultAutoCreatesPerDay
	}
	if out.AutoPatchesPerDay <= 0 {
		out.AutoPatchesPerDay = defaultAutoPatchesPerDay
	}
	if out.MaxAgentSkills <= 0 {
		out.MaxAgentSkills = defaultMaxAgentSkills
	}
	if out.DedupThreshold <= 0 {
		out.DedupThreshold = defaultDedupThreshold
	}
	if out.StaleAfterDays <= 0 {
		out.StaleAfterDays = defaultStaleAfterDays
	}
	if out.ArchiveAfterDays <= 0 {
		out.ArchiveAfterDays = defaultArchiveAfterDays
	}
	return out
}

// Enabled reports whether capture should run for this project.
func (c ProjectSkillCaptureConfig) Enabled() bool {
	return c.Normalized().Mode != SkillCaptureOff
}

// LinterEnabled reports the normalized linter flag.
func (c ProjectSkillCaptureConfig) LinterEnabled() bool {
	n := c.Normalized()
	return n.Linter != nil && *n.Linter
}

// ResolveCaptureMode combines the project master switch with the optional agent
// override. The override may only constrain: "off" disables, "suggest" caps an
// "auto" project at suggest, and "auto" never widens a project that is off or
// suggest. An empty project mode is always off.
func ResolveCaptureMode(project *ProjectSkillCaptureConfig, agent *AgentSkillCapture) string {
	if project == nil {
		return SkillCaptureOff
	}
	base := project.Normalized().Mode
	if base != SkillCaptureSuggest && base != SkillCaptureAuto {
		return SkillCaptureOff
	}
	if agent == nil || agent.Mode == nil {
		return base
	}
	switch strings.TrimSpace(strings.ToLower(*agent.Mode)) {
	case SkillCaptureOff:
		return SkillCaptureOff
	case SkillCaptureSuggest:
		return SkillCaptureSuggest
	case SkillCaptureAuto:
		return base // never widen
	default:
		return base
	}
}

// ResolveAutoApply reports whether the project + agent contract permits
// automatic application. It does not check any per-candidate guardrail; the
// proposal service does that.
func ResolveAutoApply(project *ProjectSkillCaptureConfig, agent *AgentSkillCapture) bool {
	if project == nil {
		return false
	}
	n := project.Normalized()
	if n.Mode != SkillCaptureAuto || !n.AutoApply {
		return false
	}
	if agent != nil && agent.AutoApply != nil && !*agent.AutoApply {
		return false
	}
	return ResolveCaptureMode(project, agent) == SkillCaptureAuto
}

// ValidateProjectSkillCapture validates a project policy. An empty/absent
// policy is valid and means "off".
func ValidateProjectSkillCapture(c *ProjectSkillCaptureConfig) error {
	if c == nil {
		return nil
	}
	switch m := strings.TrimSpace(strings.ToLower(c.Mode)); m {
	case "", SkillCaptureOff, SkillCaptureSuggest, SkillCaptureAuto:
	default:
		return apperror.ErrValidation.WithMessage("skill_capture.mode must be one of off, suggest, auto")
	}
	if c.AutoApplyConfidence < 0 || c.AutoApplyConfidence > 1 {
		return apperror.ErrValidation.WithMessage("skill_capture.autoApplyConfidence must be between 0 and 1")
	}
	if c.DedupThreshold < 0 || c.DedupThreshold > 1 {
		return apperror.ErrValidation.WithMessage("skill_capture.dedupThreshold must be between 0 and 1")
	}
	for name, v := range map[string]int{
		"nudgeInterval":         c.NudgeInterval,
		"maxInputTokens":        c.MaxInputTokens,
		"maxCandidates":         c.MaxCandidates,
		"maxSkillBytes":         c.MaxSkillBytes,
		"minApprovedBeforeAuto": c.MinApprovedBeforeAuto,
		"autoCreatesPerDay":     c.AutoCreatesPerDay,
		"autoPatchesPerDay":     c.AutoPatchesPerDay,
		"maxAgentSkills":        c.MaxAgentSkills,
		"staleAfterDays":        c.StaleAfterDays,
		"archiveAfterDays":      c.ArchiveAfterDays,
	} {
		if v < 0 {
			return apperror.ErrValidation.WithMessage(fmt.Sprintf("skill_capture.%s must not be negative", name))
		}
	}
	return nil
}

// Enabled reports whether the override asks for capture (mode set to suggest or
// auto). An override with mode "off" is not enabled.
func (a *AgentSkillCapture) Enabled() bool {
	if a == nil || a.Mode == nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(*a.Mode)) {
	case SkillCaptureSuggest, SkillCaptureAuto:
		return true
	default:
		return false
	}
}

// ValidateAgentSkillCapture validates a per-agent override. An empty override
// is valid and inherits the project policy.
func ValidateAgentSkillCapture(a *AgentSkillCapture) error {
	if a == nil || a.Mode == nil {
		return nil
	}
	switch m := strings.TrimSpace(strings.ToLower(*a.Mode)); m {
	case "", SkillCaptureOff, SkillCaptureSuggest, SkillCaptureAuto:
		return nil
	default:
		return apperror.ErrValidation.WithMessage("skillCapture.mode must be one of off, suggest, auto")
	}
}

// SlugifyAgentName converts an agent display name into a skill-name segment.
// Learned skills are named "{slug}.{topic}" and AutoLoadSkills matches that
// prefix, so the slugification must be shared by the writer and the loader.
func SlugifyAgentName(name string) string {
	var b strings.Builder
	lastDash := true // suppress a leading separator
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "agent"
	}
	// Keep room for ".{topic}" inside the 64-character name limit.
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}

// AgentSkillPrefix returns the auto-load prefix ("{slug}.") for an agent.
func AgentSkillPrefix(agentName string) string {
	return SlugifyAgentName(agentName) + "."
}

// LearnedSkillName returns the canonical learned-skill name for a topic. The
// topic is slugified and truncated so the whole name respects maxNameLength.
func LearnedSkillName(agentName, topic string) string {
	prefix := AgentSkillPrefix(agentName)
	topic = strings.Trim(strings.ToLower(topic), " ._-/")
	var b strings.Builder
	lastSep := true
	for _, r := range topic {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastSep = false
		default:
			if !lastSep {
				b.WriteByte('-')
				lastSep = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "lesson"
	}
	max := maxNameLength - len(prefix)
	if max < 1 {
		max = 1
	}
	if len(slug) > max {
		slug = strings.Trim(slug[:max], "-")
	}
	return prefix + slug
}
