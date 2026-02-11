package backups

import (
	"time"

	"github.com/uptrace/bun"
)

// Backup represents a project backup in the kb.backups table
type Backup struct {
	bun.BaseModel `bun:"table:kb.backups,alias:b"`

	ID             string `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	OrganizationID string `bun:"organization_id,notnull,type:uuid" json:"organizationId"`
	ProjectID      string `bun:"project_id,notnull,type:uuid" json:"projectId"`
	ProjectName    string `bun:"project_name,notnull" json:"projectName"`

	// Storage
	StorageKey string `bun:"storage_key,notnull" json:"storageKey"`
	SizeBytes  int64  `bun:"size_bytes,notnull,default:0" json:"sizeBytes"`

	// Status
	Status       string  `bun:"status,notnull,default:'creating'" json:"status"`
	Progress     int     `bun:"progress,notnull,default:0" json:"progress"`
	ErrorMessage *string `bun:"error_message" json:"errorMessage,omitempty"`

	// Metadata
	BackupType string         `bun:"backup_type,notnull,default:'full'" json:"backupType"`
	Includes   map[string]any `bun:"includes,type:jsonb,default:'{}'" json:"includes"`

	// Statistics
	Stats map[string]any `bun:"stats,type:jsonb" json:"stats,omitempty"`

	// Lifecycle
	CreatedAt   time.Time  `bun:"created_at,notnull,default:now()" json:"createdAt"`
	CreatedBy   *string    `bun:"created_by,type:uuid" json:"createdBy,omitempty"`
	CompletedAt *time.Time `bun:"completed_at" json:"completedAt,omitempty"`
	ExpiresAt   *time.Time `bun:"expires_at" json:"expiresAt,omitempty"`
	DeletedAt   *time.Time `bun:"deleted_at" json:"deletedAt,omitempty"`

	// Checksums
	ManifestChecksum *string `bun:"manifest_checksum" json:"manifestChecksum,omitempty"`
	ContentChecksum  *string `bun:"content_checksum" json:"contentChecksum,omitempty"`

	// Incremental backup support (v1.1+)
	ParentBackupID   *string        `bun:"parent_backup_id,type:uuid" json:"parentBackupId,omitempty"`
	BaselineBackupID *string        `bun:"baseline_backup_id,type:uuid" json:"baselineBackupId,omitempty"`
	ChangeWindow     map[string]any `bun:"change_window,type:jsonb" json:"changeWindow,omitempty"`
}

// BackupStatus constants
const (
	BackupStatusCreating = "creating"
	BackupStatusReady    = "ready"
	BackupStatusFailed   = "failed"
	BackupStatusDeleted  = "deleted"
)

// BackupType constants
const (
	BackupTypeFull        = "full"
	BackupTypeIncremental = "incremental"
)

// ListParams contains parameters for listing backups
type ListParams struct {
	OrganizationID string
	ProjectID      *string // Optional: filter by project
	Status         *string // Optional: filter by status
	Limit          int
	Cursor         *Cursor
}

// Cursor represents pagination cursor
type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

// ListResult contains the result of listing backups
type ListResult struct {
	Backups    []Backup `json:"backups"`
	Total      int      `json:"total"`
	NextCursor *Cursor  `json:"nextCursor,omitempty"`
}

// CreateBackupRequest contains parameters for creating a backup
type CreateBackupRequest struct {
	ProjectID      string
	OrganizationID string
	CreatedBy      string
	IncludeDeleted bool
	IncludeChat    bool
	RetentionDays  int // Default: 30
}

// BackupStats represents statistics about a backup
type BackupStats struct {
	Documents          int   `json:"documents"`
	Chunks             int   `json:"chunks"`
	GraphObjects       int   `json:"graphObjects"`
	GraphRelationships int   `json:"graphRelationships"`
	ChatConversations  int   `json:"chatConversations"`
	ChatMessages       int   `json:"chatMessages"`
	ExtractionJobs     int   `json:"extractionJobs"`
	ProjectMemberships int   `json:"projectMemberships"`
	Files              int   `json:"files"`
	TotalSizeBytes     int64 `json:"totalSizeBytes"`
}

// Manifest represents the manifest.json file inside a backup ZIP
type Manifest struct {
	Version       string         `json:"version"`
	SchemaVersion string         `json:"schemaVersion"`
	CreatedAt     time.Time      `json:"createdAt"`
	BackupType    string         `json:"backupType"`
	Project       ProjectInfo    `json:"project"`
	Contents      BackupStats    `json:"contents"`
	Checksums     Checksums      `json:"checksums"`
	Metadata      map[string]any `json:"metadata"`
}

// ProjectInfo represents project information in the manifest
type ProjectInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	OrganizationID string `json:"organizationId"`
}

// Checksums represents file checksums in the manifest
type Checksums struct {
	Manifest string `json:"manifest"`
	Database string `json:"database"`
	Files    string `json:"files"`
}

// Metadata represents the metadata.json file stored alongside backup.zip
type Metadata struct {
	BackupID       string      `json:"backupId"`
	OrganizationID string      `json:"organizationId"`
	ProjectID      string      `json:"projectId"`
	ProjectName    string      `json:"projectName"`
	CreatedAt      time.Time   `json:"createdAt"`
	CreatedBy      string      `json:"createdBy"`
	Status         string      `json:"status"`
	SizeBytes      int64       `json:"sizeBytes"`
	Stats          BackupStats `json:"stats"`
	Checksums      Checksums   `json:"checksums"`
}
