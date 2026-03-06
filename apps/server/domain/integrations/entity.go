package integrations

import (
	"time"

	"github.com/uptrace/bun"
)

// Integration represents a third-party integration in kb.integrations
type Integration struct {
	bun.BaseModel `bun:"table:kb.integrations,alias:i"`

	ID                string    `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	Name              string    `bun:"name,notnull" json:"name"`
	DisplayName       string    `bun:"display_name,notnull" json:"display_name"`
	Description       *string   `bun:"description" json:"description,omitempty"`
	Enabled           bool      `bun:"enabled,notnull,default:true" json:"enabled"`
	OrgID             string    `bun:"org_id,notnull" json:"org_id"`
	ProjectID         string    `bun:"project_id,notnull,type:uuid" json:"project_id"`
	SettingsEncrypted []byte    `bun:"settings_encrypted,type:bytea" json:"-"` // Never expose in JSON
	LogoURL           *string   `bun:"logo_url" json:"logo_url,omitempty"`
	WebhookSecret     *string   `bun:"webhook_secret" json:"-"` // Never expose in JSON
	CreatedBy         *string   `bun:"created_by" json:"created_by,omitempty"`
	CreatedAt         time.Time `bun:"created_at,notnull,default:now()" json:"created_at"`
	UpdatedAt         time.Time `bun:"updated_at,notnull,default:now()" json:"updated_at"`
}
