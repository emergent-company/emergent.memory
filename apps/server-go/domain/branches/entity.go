package branches

import (
	"time"

	"github.com/uptrace/bun"
)

// Branch represents a branch in the kb.branches table
type Branch struct {
	bun.BaseModel `bun:"table:kb.branches,alias:b"`

	ID             string    `bun:"id,pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID      *string   `bun:"project_id,type:uuid"`
	Name           string    `bun:"name,notnull"`
	ParentBranchID *string   `bun:"parent_branch_id,type:uuid"`
	CreatedAt      time.Time `bun:"created_at,notnull,default:now()"`
}
