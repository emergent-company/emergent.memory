package branches

import (
	"time"

	"github.com/uptrace/bun"
)

// Branch represents a branch in the kb.branches table
type Branch struct {
	bun.BaseModel `bun:"table:kb.branches,alias:b"`

	ID             string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID      *string    `bun:"project_id,type:uuid"`
	Name           string     `bun:"name,notnull"`
	Description    *string    `bun:"description"`
	ParentBranchID *string    `bun:"parent_branch_id,type:uuid"`
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()"`
	MergedAt       *time.Time `bun:"merged_at"`
	// Origin records why the branch exists: who or what created it. It is
	// metadata for labels, filters and cleanup — never a read permission: every
	// branch is read the same way (see issue #265).
	Origin string `bun:"origin,notnull,default:'manual'"`
	// OriginID is the creator's id — an extraction job, agent run or user —
	// polymorphic on purpose: a branch has exactly one creator.
	OriginID *string `bun:"origin_id,type:uuid"`
	// OriginLabel is the creator rendered for humans: the source document's
	// filename, the agent's name or the user's name. Denormalised so a branch
	// list renders without a join.
	OriginLabel *string `bun:"origin_label"`
}

// Branch origins. These describe *why* a branch exists, not who may read it.
const (
	// OriginExtraction marks a per-job staging branch written by the extraction
	// worker; OriginID is the extraction job and OriginLabel the source
	// document's filename.
	OriginExtraction = "extraction"
	// OriginAgent marks a branch created by an agent run.
	OriginAgent = "agent"
	// OriginManual marks a branch created by a person through the API or UI.
	OriginManual = "manual"
	// OriginSystem marks a branch created by a scheduled or blueprint-driven
	// process.
	OriginSystem = "system"
)

// ValidOrigin reports whether origin is one of the four stored values.
func ValidOrigin(origin string) bool {
	switch origin {
	case OriginExtraction, OriginAgent, OriginManual, OriginSystem:
		return true
	}
	return false
}
