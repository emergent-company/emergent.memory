package sources

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// SourceIssuePRLink is the server-owned link from a coding issue to its PR.
// Table: kb.github_issue_pull_requests.
type SourceIssuePRLink struct {
	bun.BaseModel `bun:"table:kb.github_issue_pull_requests,alias:ipr"`

	ProjectID        uuid.UUID `bun:"project_id,pk,type:uuid" json:"projectId"`
	Repo             string    `bun:"repo,pk" json:"repo"`
	PRNumber         int       `bun:"pr_number,pk" json:"prNumber"`
	IssueCanonicalID uuid.UUID `bun:"issue_canonical_id,type:uuid,notnull" json:"issueCanonicalId"`
	IssueNumber      int       `bun:"issue_number,notnull" json:"issueNumber"`
	IssueSourceID    uuid.UUID `bun:"issue_source_id,type:uuid,notnull" json:"issueSourceId"`
	PRURL            string    `bun:"pr_url,notnull,default:''" json:"prUrl"`
	Branch           string    `bun:"branch,notnull,default:''" json:"branch"`
	HeadSHA          string    `bun:"head_sha,notnull,default:''" json:"headSha"`
	State            string    `bun:"state,notnull,default:'open'" json:"state"`
	ClosesIssue      bool      `bun:"closes_issue,notnull,default:false" json:"closesIssue"`
	CreatedAt        time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt        time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}
