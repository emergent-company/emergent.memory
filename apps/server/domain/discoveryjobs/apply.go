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
// are always declined, and a type below min_confidence is declined. It returns
// the accepted type names and the full decision list.
func gateApplyTypes(pol pipeline.Policy, types []DiscoveredType) (accepted []string, decisions []appliedTypeDecision) {
	accepted = make([]string, 0, len(types))
	decisions = make([]appliedTypeDecision, 0, len(types))
	for _, t := range types {
		switch {
		case extraction.IsRelationshipObjectType(t.TypeName):
			decisions = append(decisions, appliedTypeDecision{Type: t.TypeName, Reason: "relationship-like type"})
		case t.Confidence < pol.Discovery.MinConfidence:
			decisions = append(decisions, appliedTypeDecision{Type: t.TypeName, Reason: "below min_confidence"})
		default:
			accepted = append(accepted, t.TypeName)
			decisions = append(decisions, appliedTypeDecision{Type: t.TypeName, Applied: true})
		}
	}
	return accepted, decisions
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

	accepted, decisions := gateApplyTypes(pol, refinedTypes)
	newTypes := make([]string, 0, len(accepted))
	for _, name := range accepted {
		if !registered[name] {
			newTypes = append(newTypes, name)
		}
	}

	declinedReason := ""
	switch {
	case len(refinedTypes) == 0:
		declinedReason = "no types discovered"
	case len(accepted) == 0:
		declinedReason = "all types declined by gates"
	case len(newTypes) > pol.Apply.MaxNewTypesPerRun:
		declinedReason = "too many new types in one run"
	case len(accepted) > pol.Discovery.MaxTotalTypes:
		declinedReason = "would exceed max_total_types"
	}

	report := JSONMap{
		"mode":      pol.Mode,
		"applied":   declinedReason == "",
		"declined":  declinedReason,
		"new_types": newTypes,
		"accepted":  accepted,
		"decisions": decisions,
		"pack_id":   schemaID.String(),
	}
	if declinedReason != "" {
		if err := s.repo.MarkAutoApplied(ctx, jobID, report); err != nil {
			s.log.Warn("auto-apply: failed to record decline", slog.String("job_id", jobID.String()), logger.Error(err))
		}
		s.log.Info("auto-apply: declined discovered schema",
			slog.String("job_id", jobID.String()), slog.String("reason", declinedReason))
		return
	}

	// Install: mark the reconciled pack reviewed, deactivate any prior
	// discovered-pack assignment (one active auto pack per project), install,
	// then bump the schema generation so extraction backfill can react.
	if err := s.repo.MarkDiscoveredSchemaReviewed(ctx, schemaID); err != nil {
		s.log.Warn("auto-apply: failed to mark pack reviewed", slog.String("pack_id", schemaID.String()), logger.Error(err))
		return
	}
	if err := s.repo.DeactivateOtherDiscoveredAssignments(ctx, projectID, schemaID); err != nil {
		s.log.Warn("auto-apply: failed to deactivate prior discovered packs", logger.Error(err))
	}
	if err := s.repo.InstallSchemaToProject(ctx, projectID, schemaID); err != nil {
		s.log.Warn("auto-apply: failed to install pack", slog.String("pack_id", schemaID.String()), logger.Error(err))
		return
	}
	if err := s.policyReader.BumpSchemaGeneration(ctx, projectID); err != nil {
		s.log.Warn("auto-apply: failed to bump schema generation", logger.Error(err))
		return
	}
	if err := s.repo.MarkAutoApplied(ctx, jobID, report); err != nil {
		s.log.Warn("auto-apply: failed to record apply", slog.String("job_id", jobID.String()), logger.Error(err))
		return
	}
	s.log.Info("auto-applied discovered schema",
		slog.String("job_id", jobID.String()),
		slog.String("pack_id", schemaID.String()),
		slog.Int("accepted", len(accepted)),
		slog.Int("new_types", len(newTypes)))
}
