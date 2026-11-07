package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Branch-scoped write support for the object/relationship verbs that predate
// the `branch` argument (#317).
//
// Entity create/update already take a branch and let the graph service write
// the new version on that branch. Delete already accepts a branch id, and its
// branch tombstone is applied by the merge machinery like any other object
// version. Restore and the relationship verbs identify their target by a row id
// rather than a canonical identity, so the MCP layer resolves the branch-local
// HEAD row here and hands its physical id to the graph service. This keeps the
// write on the branch's own version chain — a version chain cannot span
// branches — without adding a branch selector to those service signatures.

// objectVersionOnBranch returns the physical id of the entity's HEAD version on
// branchID, including a tombstone. It is the target entity-restore must patch so
// the restored version lands on the branch instead of main.
//
// Lookup order matters:
//   - an entity that exists only on the branch (created or forked there) is
//     returned directly by the include-deleted read when the requested id
//     resolves to that branch row;
//   - an entity that lives on main and carries a branch tombstone is found in
//     the version history.
func (s *Service) objectVersionOnBranch(ctx context.Context, projectUUID, entityID, branchID uuid.UUID) (uuid.UUID, error) {
	current, err := s.graphService.GetByIDIncludingDeleted(ctx, projectUUID, entityID)
	if err != nil {
		return uuid.Nil, err
	}
	if current.BranchID != nil && *current.BranchID == branchID {
		return current.ID, nil
	}

	history, err := s.graphService.GetHistory(ctx, projectUUID, current.CanonicalID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("entity %s has no readable version history while resolving branch %s: %w", entityID, branchID, err)
	}
	for _, version := range history.Versions {
		if version == nil || version.BranchID == nil || *version.BranchID != branchID {
			continue
		}
		if version.SupersedesID == nil { // the HEAD of that branch's chain
			return version.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("entity %s has no version on branch %s (create or delete it on that branch first, or omit branch to target the main graph)", entityID, branchID)
}

// relationshipVersionOnBranch returns the physical id of the relationship's HEAD
// version on branchID. The relationship may be addressed by physical id or
// canonical id, exactly like the relationship tools accept.
func (s *Service) relationshipVersionOnBranch(ctx context.Context, projectUUID, relID, branchID uuid.UUID) (uuid.UUID, error) {
	current, err := s.graphService.GetRelationship(ctx, projectUUID, relID)
	if err != nil {
		return uuid.Nil, err
	}
	if current.BranchID != nil && *current.BranchID == branchID {
		return current.ID, nil
	}

	versions, err := s.graphService.GetRelationshipHistory(ctx, projectUUID, relID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("relationship %s has no readable version history while resolving branch %s: %w", relID, branchID, err)
	}
	for _, version := range versions {
		if version == nil || version.BranchID == nil || *version.BranchID != branchID {
			continue
		}
		if version.SupersedesID == nil { // the HEAD of that branch's chain
			return version.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("relationship %s has no version on branch %s (create it on that branch first, or omit branch to target the main graph)", relID, branchID)
}
