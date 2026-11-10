// Package githubapp owns the GitHub App connection model for the review
// producer: App-level credentials, per-installation routing, ingress dedupe,
// and per-head review claims. It does not post reviews itself; review tools and
// the producer live in their own change.
package githubapp

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Review claim states.
const (
	ClaimPending    = "pending"
	ClaimRunning    = "running"
	ClaimDone       = "done"
	ClaimSuperseded = "superseded"
	ClaimCancelled  = "cancelled"
)

// GitHubApp is the App-level credential row. One App can back several
// installations and shares one private key and one webhook secret.
type GitHubApp struct {
	bun.BaseModel `bun:"table:core.github_apps,alias:ga"`

	ID    uuid.UUID `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	OrgID uuid.UUID `bun:"org_id,notnull,type:uuid" json:"orgId"`
	AppID int64     `bun:"app_id,notnull" json:"appId"`
	Slug  string    `bun:"slug,notnull" json:"slug"`
	// Credentials are write-only: never serialised on a response.
	PrivateKeyEncrypted    string    `bun:"private_key_encrypted,notnull" json:"-"`
	WebhookSecretEncrypted string    `bun:"webhook_secret_encrypted,notnull" json:"-"`
	CreatedAt              time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt              time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// Installation binds a GitHub App installation to a project, a repository
// allowlist, a runtime review agent, and a queue.
type Installation struct {
	bun.BaseModel `bun:"table:core.github_app_installations,alias:gi"`

	ID             uuid.UUID `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	AppPK          uuid.UUID `bun:"app_pk,notnull,type:uuid" json:"appPk"`
	OrgID          uuid.UUID `bun:"org_id,notnull,type:uuid" json:"orgId"`
	ProjectID      uuid.UUID `bun:"project_id,notnull,type:uuid" json:"projectId"`
	InstallationID int64     `bun:"installation_id,notnull" json:"installationId"`
	Repos          []string  `bun:"repos,type:text[],notnull" json:"repos"`
	// AgentID is a runtime kb.agents id (what CreateRunQueued resolves), not an
	// agent-definition id.
	AgentID   uuid.UUID `bun:"agent_id,notnull,type:uuid" json:"agentId"`
	Queue     string    `bun:"queue,notnull,default:'default'" json:"queue"`
	Enabled   bool      `bun:"enabled,notnull,default:true" json:"enabled"`
	CreatedAt time.Time `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// Delivery is the ingress dedupe record. Several deliveries may share a head
// SHA, so it carries no per-head uniqueness.
type Delivery struct {
	bun.BaseModel `bun:"table:core.github_app_deliveries,alias:gd"`

	DeliveryID     string    `bun:"delivery_id,pk" json:"deliveryId"`
	InstallationID int64     `bun:"installation_id,notnull" json:"installationId"`
	Repo           string    `bun:"repo,notnull" json:"repo"`
	PullRequest    int       `bun:"pull_request,notnull" json:"pullRequest"`
	HeadSHA        string    `bun:"head_sha,notnull" json:"headSha"`
	ReceivedAt     time.Time `bun:"received_at,notnull,default:now()" json:"receivedAt"`
}

// ReviewClaim is the per-head idempotency record.
type ReviewClaim struct {
	bun.BaseModel `bun:"table:core.github_pr_review_claims,alias:gc"`

	ID             uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	InstallationID int64      `bun:"installation_id,notnull" json:"installationId"`
	Repo           string     `bun:"repo,notnull" json:"repo"`
	PullRequest    int        `bun:"pull_request,notnull" json:"pullRequest"`
	HeadSHA        string     `bun:"head_sha,notnull" json:"headSha"`
	RunID          *uuid.UUID `bun:"run_id,type:uuid" json:"runId,omitempty"`
	State          string     `bun:"state,notnull,default:'pending'" json:"state"`
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt      time.Time  `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// RepoAllowed reports whether fullName is in the installation allowlist. An
// empty allowlist matches nothing.
func (i *Installation) RepoAllowed(fullName string) bool {
	for _, repo := range i.Repos {
		if strings.EqualFold(repo, fullName) {
			return true
		}
	}
	return false
}
