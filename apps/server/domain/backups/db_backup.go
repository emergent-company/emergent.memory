package backups

import (
	"time"

	"github.com/uptrace/bun"
)

// DatabaseBackup represents a full database backup record
type DatabaseBackup struct {
	bun.BaseModel `bun:"table:kb.database_backups,alias:dbb"`

	ID          string     `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	Status      string     `bun:"status,notnull" json:"status"`
	StorageKey  *string    `bun:"storage_key" json:"storageKey,omitempty"`
	SizeBytes   *int64     `bun:"size_bytes" json:"sizeBytes,omitempty"`
	StartedAt   *time.Time `bun:"started_at" json:"startedAt,omitempty"`
	CompletedAt *time.Time `bun:"completed_at" json:"completedAt,omitempty"`
	Error       *string    `bun:"error" json:"error,omitempty"`
	CreatedAt   time.Time  `bun:"created_at,notnull" json:"createdAt"`
}

// DatabaseBackupResponse is the API response shape for a database backup record
type DatabaseBackupResponse struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	StorageKey  *string    `json:"storageKey,omitempty"`
	SizeBytes   *int64     `json:"sizeBytes,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Error       *string    `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}
