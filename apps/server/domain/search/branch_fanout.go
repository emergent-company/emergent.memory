package search

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// fanOutGraphSearch reads the main graph and every unmerged branch in the run's
// scope, then fuses and deduplicates the result sets (#265).
//
// It fans out instead of widening the SQL predicates: a branch result is a
// different head of the same canonical object, and the graph search's ranking is
// only meaningful per branch. Fanning out keeps every existing query a
// single-branch query — the main-only path is untouched — and confines the
// multi-branch behaviour to runs that explicitly carry a scope.
//
// The returned results keep each item's native score (so the caller's min-score
// cut-off stays meaningful) but are ordered by reciprocal rank across sources,
// with the main graph first on ties and preferred on a canonical-id collision:
// merged fact wins over pending fact.
func (s *Service) fanOutGraphSearch(
	ctx context.Context,
	projectID uuid.UUID,
	base *graph.HybridSearchRequest,
	scope *branchscope.Scope,
	minScore *float32,
	mainResults []*UnifiedSearchGraphResult,
) ([]*UnifiedSearchGraphResult, *BranchScopeInfo) {
	info := &BranchScopeInfo{}

	candidates, err := s.repo.ListUnmergedBranches(ctx, projectID, scope.Origins())
	if err != nil {
		// Fail closed: a scope that cannot be resolved reads the main graph only,
		// and says so, rather than returning a silently incomplete answer.
		s.log.Warn("branch scope: failed to list unmerged branches; reading main only", logger.Error(err))
		info.Note = "the branch scope could not be resolved, so this search read the main graph only"
		return mainResults, info
	}

	targets := make([]*BranchTarget, 0, len(candidates))
	for _, b := range candidates {
		if !scope.AllowsOrigin(b.Origin) || !scope.AllowsName(b.Name) {
			continue
		}
		targets = append(targets, b)
	}
	if maxBranches := scope.MaxBranches(); len(targets) > maxBranches {
		info.Omitted = len(targets) - maxBranches
		targets = targets[:maxBranches]
	}

	limit := s.clampLimit(base.Limit)
	sources := make([][]*UnifiedSearchGraphResult, 0, len(targets)+1)
	sources = append(sources, mainResults)

	failed := 0
	for _, target := range targets {
		req := *base
		branchID := target.ID
		req.BranchID = &branchID
		// The query vector is computed once per search; a branch leg must not
		// issue one provider call per branch. With no vector the leg stays
		// lexical, exactly like the main leg's no-embedding fallback.
		if len(req.Vector) == 0 {
			req.DisableAutoEmbed = true
		}
		results, _, err := s.graphSearchOnce(ctx, projectID, &req, minScore, false)
		if err != nil {
			// One unreadable branch degrades the answer; it does not fail the
			// search. The note below tells the model the read was incomplete.
			failed++
			s.log.Warn("branch scope: reading an unmerged branch failed",
				logger.Error(err), slog.String("branch", target.Name))
			continue
		}
		attachBranchProvenance(results, target)
		sources = append(sources, results)
		info.Branches = append(info.Branches, BranchRead{
			ID:     target.ID.String(),
			Name:   target.Name,
			Origin: target.Origin,
			Label:  target.OriginLabel,
		})
	}

	fused := fuseScopedGraphResults(sources, s.rrfConstant, limit)
	switch {
	case info.Omitted > 0:
		info.Note = fmt.Sprintf(
			"read %d of %d matching unmerged branches; %d were omitted by the per-agent branch cap",
			len(info.Branches), len(info.Branches)+info.Omitted, info.Omitted)
	case failed > 0:
		info.Note = fmt.Sprintf("read %d unmerged branches; %d could not be read", len(info.Branches), failed)
	}
	return fused, info
}

// attachBranchProvenance marks every result from one branch with where it came
// from, so a consumer can hedge ("on the pending extraction branch…") instead of
// asserting merged fact.
func attachBranchProvenance(results []*UnifiedSearchGraphResult, target *BranchTarget) {
	for _, r := range results {
		id := target.ID.String()
		name := target.Name
		origin := target.Origin
		r.BranchID = &id
		r.BranchName = &name
		r.BranchOrigin = &origin
		if target.OriginLabel != nil {
			label := *target.OriginLabel
			r.BranchOriginLabel = &label
		}
	}
}

// fuseScopedGraphResults merges per-source ranked lists into one list using
// reciprocal rank fusion: every item scores 1/(k+rank) in its own source, each
// canonical id appears once, and the sources are ordered main first so a
// canonical-id collision keeps the merged (main) result. Native scores are left
// untouched; only the order and Rank are rewritten.
func fuseScopedGraphResults(sources [][]*UnifiedSearchGraphResult, k, limit int) []*UnifiedSearchGraphResult {
	if k <= 0 {
		k = 60
	}
	type scored struct {
		result *UnifiedSearchGraphResult
		rrf    float64
	}

	seen := make(map[string]bool)
	var fused []scored
	for _, source := range sources {
		for rank, r := range source {
			key := r.CanonicalID
			if key == "" {
				key = r.ObjectID
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			fused = append(fused, scored{result: r, rrf: 1.0 / float64(k+rank)})
		}
	}

	// SliceStable keeps source order on an equal score, which is the main-first
	// tie-break: all rank-0 items tie, so main comes first.
	sort.SliceStable(fused, func(i, j int) bool { return fused[i].rrf > fused[j].rrf })
	if limit > 0 && len(fused) > limit {
		fused = fused[:limit]
	}

	out := make([]*UnifiedSearchGraphResult, len(fused))
	for i, f := range fused {
		f.result.Rank = i + 1
		out[i] = f.result
	}
	return out
}

// graphObjectIDs returns the head object ids of a result set, used for the
// best-effort access-timestamp update.
func graphObjectIDs(results []*UnifiedSearchGraphResult) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(results))
	for _, r := range results {
		id, err := uuid.Parse(r.ObjectID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
