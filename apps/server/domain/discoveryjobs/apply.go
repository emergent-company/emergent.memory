package discoveryjobs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/pipeline"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// PipelinePolicyReader is the slice of the projects domain that auto-apply
// needs: the project's resolved pipeline policy, and a way to bump the schema
// generation after an install. Implemented by *projects.Service; discovery
// never imports projects, so the dependency is an interface.
type PipelinePolicyReader interface {
	Policy(ctx context.Context, projectID uuid.UUID) (pipeline.Policy, error)
	BumpSchemaGeneration(ctx context.Context, projectID uuid.UUID) error
}

// SetPipelinePolicyReader injects the project-policy reader. Nil-safe: without
// it, auto-apply never runs and discovery keeps its manual finalize-only path.
func (s *Service) SetPipelinePolicyReader(r PipelinePolicyReader) {
	s.policyReader = r
}

// appliedTypeDecision is one type's apply outcome in the job report.
type appliedTypeDecision struct {
	Type    string `json:"type"`
	Applied bool   `json:"applied"`
	Reason  string `json:"reason,omitempty"`
}

// gateApplyTypes applies the per-type acceptance gates: relationship-like types
// are always declined, malformed type names are declined, and a type below
// min_confidence is declined. It returns the accepted type names and the full
// decision list.
func gateApplyTypes(pol pipeline.Policy, types []DiscoveredType) (accepted []string, decisions []appliedTypeDecision) {
	accepted = make([]string, 0, len(types))
	decisions = make([]appliedTypeDecision, 0, len(types))
	for _, t := range types {
		switch {
		case extraction.IsRelationshipObjectType(t.TypeName):
			decisions = append(decisions, appliedTypeDecision{Type: t.TypeName, Reason: "relationship-like type"})
		case !validTypeName(t.TypeName):
			decisions = append(decisions, appliedTypeDecision{Type: t.TypeName, Reason: "invalid type name"})
		case t.Confidence < pol.Discovery.MinConfidence:
			decisions = append(decisions, appliedTypeDecision{Type: t.TypeName, Reason: "below min_confidence"})
		default:
			accepted = append(accepted, t.TypeName)
			decisions = append(decisions, appliedTypeDecision{Type: t.TypeName, Applied: true})
		}
	}
	return accepted, decisions
}

// validTypeName rejects discovered type names that would be unsafe to promote
// into live schema: they become the strict extraction allowlist and later feed
// prompts, so an LLM- or document-derived name must be a short, plain
// identifier. This closes the prompt-injection/data-poisoning path a fully
// automatic install would otherwise open.
func validTypeName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		case r == ' ' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// maybeAutoApply runs the auto-apply gate after a discovery job has reconciled
// its pack. It is best-effort and never fails the job: when the policy is not
// "auto", there is no reader, or the gate declines the diff, the pack simply
// stays in pending review and the decision is recorded on the job.
func (s *Service) maybeAutoApply(ctx context.Context, projectID, jobID, schemaID uuid.UUID, refinedTypes []DiscoveredType, registeredTypes []string) {
	if s.policyReader == nil {
		return
	}
	pol, err := s.policyReader.Policy(ctx, projectID)
	if err != nil {
		s.log.Warn("auto-apply: policy lookup failed; leaving pack pending review",
			slog.String("job_id", jobID.String()), logger.Error(err))
		return
	}
	if pol.Mode != pipeline.ModeAuto {
		return
	}

	registered := make(map[string]bool, len(registeredTypes))
	for _, name := range registeredTypes {
		registered[name] = true
	}

	decline := func(reason string) {
		report := JSONMap{
			"mode":     pol.Mode,
			"applied":  false,
			"declined": reason,
			"pack_id":  schemaID.String(),
		}
		if _, err := s.repo.MarkAutoApplied(ctx, jobID, report); err != nil {
			s.log.Warn("auto-apply: failed to record decline", slog.String("job_id", jobID.String()), logger.Error(err))
		}
		s.log.Info("auto-apply: declined discovered schema",
			slog.String("job_id", jobID.String()), slog.String("reason", reason))
	}

	// Additive-only is a hard invariant in v1: apply never removes or retypes a
	// registered type. Quarantine of dropped extraction types is P3.
	if len(refinedTypes) == 0 {
		decline("no types discovered")
		return
	}

	accepted, decisions := gateApplyTypes(pol, refinedTypes)
	if len(accepted) == 0 {
		decline("all types declined by gates")
		return
	}

	// An installed discovered pack means this is an incremental extend; on the
	// first apply (no installed pack) the new-type cap would wrongly block the
	// bootstrap, so it is only enforced when extending.
	installed, err := s.repo.InstalledDiscoveredSchema(ctx, projectID)
	if err != nil {
		s.log.Warn("auto-apply: installed-pack lookup failed", logger.Error(err))
		return
	}
	isExtend := installed != nil

	newTypes := make([]string, 0, len(accepted))
	for _, name := range accepted {
		if !registered[name] {
			newTypes = append(newTypes, name)
		}
	}

	switch {
	case isExtend && len(newTypes) > pol.Apply.MaxNewTypesPerRun:
		decline("too many new types in one run")
		return
	case len(accepted) > pol.Discovery.MaxTotalTypes:
		decline("would exceed max_total_types")
		return
	}

	// Filter the reconciled pack down to the accepted types (dropping
	// relationship-like / low-confidence / invalid-name types) so a declined
	// type is never installed.
	if err := s.filterDiscoveredPack(ctx, schemaID, accepted); err != nil {
		s.log.Warn("auto-apply: filter pack failed", logger.Error(err))
		return
	}

	report := JSONMap{
		"mode":      pol.Mode,
		"applied":   true,
		"new_types": newTypes,
		"accepted":  accepted,
		"decisions": decisions,
		"pack_id":   schemaID.String(),
	}

	// Apply: fence + review + deactivate + install run in one serialized
	// transaction; the fence is what makes a replay idempotent.
	applied, err := s.repo.ApplyDiscoveredSchema(ctx, projectID, schemaID, jobID, report)
	if err != nil {
		s.log.Warn("auto-apply: failed to apply pack", slog.String("pack_id", schemaID.String()), logger.Error(err))
		return
	}
	if !applied {
		s.log.Warn("auto-apply: apply already recorded; skipping generation bump",
			slog.String("job_id", jobID.String()))
		return
	}
	if err := s.policyReader.BumpSchemaGeneration(ctx, projectID); err != nil {
		s.log.Warn("auto-apply: failed to bump schema generation", logger.Error(err))
		return
	}
	s.maybeAutoBackfill(ctx, projectID, pol)
	s.log.Info("auto-applied discovered schema",
		slog.String("job_id", jobID.String()),
		slog.String("pack_id", schemaID.String()),
		slog.Int("accepted", len(accepted)),
		slog.Int("new_types", len(newTypes)))
}

// filterDiscoveredPack rewrites the pending discovered pack's object and
// relationship schemas so only the accepted type names remain, so a type the
// gate declined is never installed.
func (s *Service) filterDiscoveredPack(ctx context.Context, packID uuid.UUID, accepted []string) error {
	pack, err := s.repo.GetMemorySchema(ctx, packID)
	if err != nil {
		return err
	}
	keep := make(map[string]bool, len(accepted))
	for _, name := range accepted {
		keep[name] = true
	}

	obj := make(JSONMap, len(accepted))
	for name, schema := range pack.ObjectTypeSchemas {
		if keep[name] {
			obj[name] = schema
		}
	}

	rel := make(JSONMap)
	for name, raw := range pack.RelationshipTypeSchemas {
		if !relationshipUsesOnly(raw, keep) {
			continue
		}
		rel[name] = raw
	}

	ui := make(JSONMap, len(accepted))
	for name, cfg := range pack.UIConfigs {
		if keep[name] {
			ui[name] = cfg
		}
	}

	return s.repo.UpdateMemorySchema(ctx, packID, obj, rel, ui)
}

// relationshipUsesOnly reports whether a relationship schema's source and
// target types are all in keep. Source/target names may be single strings or
// arrays under the sourceType(s)/targetType(s) keys.
func relationshipUsesOnly(raw any, keep map[string]bool) bool {
	m, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	allIn := func(v any) bool {
		switch t := v.(type) {
		case string:
			return keep[t]
		case []any:
			for _, e := range t {
				if s, ok := e.(string); !ok || !keep[s] {
					return false
				}
			}
			return true
		case []string:
			for _, s := range t {
				if !keep[s] {
					return false
				}
			}
			return true
		}
		return false
	}
	src := m["sourceTypes"]
	if src == nil {
		src = m["sourceType"]
	}
	dst := m["targetTypes"]
	if dst == nil {
		dst = m["targetType"]
	}
	return allIn(src) && allIn(dst)
}
