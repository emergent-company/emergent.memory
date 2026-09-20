// Package backups provides the Backups and Restores service client for the
// Emergent API SDK.
package backups

import (
	"encoding/base64"
	"encoding/json"
	"io"
)

// Backup status constants.
const (
	BackupStatusCreating = "creating"
	BackupStatusReady    = "ready"
	BackupStatusFailed   = "failed"
	BackupStatusDeleted  = "deleted"
)

// Restore status constants.
const (
	RestoreStatusPending   = "pending"
	RestoreStatusRunning   = "running"
	RestoreStatusCompleted = "completed"
	RestoreStatusFailed    = "failed"
)

// Backup represents a project backup as returned by the API.
type Backup struct {
	ID               string         `json:"id"`
	OrganizationID   string         `json:"organizationId"`
	ProjectID        string         `json:"projectId"`
	ProjectName      string         `json:"projectName"`
	StorageKey       string         `json:"storageKey"`
	SizeBytes        int64          `json:"sizeBytes"`
	Status           string         `json:"status"`
	Progress         int            `json:"progress"`
	ErrorMessage     *string        `json:"errorMessage,omitempty"`
	BackupType       string         `json:"backupType"`
	Includes         map[string]any `json:"includes"`
	Stats            map[string]any `json:"stats,omitempty"`
	CreatedAt        string         `json:"createdAt"`
	CreatedBy        *string        `json:"createdBy,omitempty"`
	CompletedAt      *string        `json:"completedAt,omitempty"`
	ExpiresAt        *string        `json:"expiresAt,omitempty"`
	DeletedAt        *string        `json:"deletedAt,omitempty"`
	ManifestChecksum *string        `json:"manifestChecksum,omitempty"`
	ContentChecksum  *string        `json:"contentChecksum,omitempty"`
	ParentBackupID   *string        `json:"parentBackupId,omitempty"`
	BaselineBackupID *string        `json:"baselineBackupId,omitempty"`
	ChangeWindow     map[string]any `json:"changeWindow,omitempty"`
	Imported         bool           `json:"imported"`
}

// Restore represents a restore job as returned by the API.
type Restore struct {
	ID              string         `json:"id"`
	OrganizationID  string         `json:"organizationId"`
	BackupID        string         `json:"backupId"`
	Mode            string         `json:"mode"`
	SourceProjectID *string        `json:"sourceProjectId,omitempty"`
	TargetProjectID *string        `json:"targetProjectId,omitempty"`
	Status          string         `json:"status"`
	Progress        int            `json:"progress"`
	ErrorMessage    *string        `json:"errorMessage,omitempty"`
	Stats           map[string]any `json:"stats,omitempty"`
	CreatedAt       string         `json:"createdAt"`
	CreatedBy       *string        `json:"createdBy,omitempty"`
	CompletedAt     *string        `json:"completedAt,omitempty"`
}

// CreateBackupRequest is the request body for creating a backup.
type CreateBackupRequest struct {
	IncludeDeleted bool `json:"includeDeleted"`
	IncludeChat    bool `json:"includeChat"`
	IncludeJournal bool `json:"includeJournal"`
	RetentionDays  int  `json:"retentionDays"`
}

// ListBackupsOptions holds options for listing backups.
type ListBackupsOptions struct {
	ProjectID string // Filter by project ID
	Limit     int    // Max results (1-100); 0 means server default
	Cursor    string // Pagination cursor from a previous response
}

// Cursor represents a pagination cursor.
type Cursor struct {
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
}

// Encode serializes the cursor to the opaque string the server accepts as the
// `cursor` query parameter: base64url-encoded JSON of {createdAt, id}. The
// server's own ParseCursor is the inverse, so the value printed by the CLI can
// be passed straight back to --cursor.
func (c *Cursor) Encode() string {
	if c == nil {
		return ""
	}
	data, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(data)
}

// ListBackupsResult is the response from listing backups.
type ListBackupsResult struct {
	Backups    []Backup `json:"backups"`
	Total      int      `json:"total"`
	NextCursor *Cursor  `json:"nextCursor,omitempty"`
}

// ImportBackupInput carries the archive and metadata for importing a backup.
// The archive is streamed (never buffered in memory), so Reader may be a large
// file.
type ImportBackupInput struct {
	Filename      string    // name attached to the multipart "file" field
	Reader        io.Reader // archive content
	RetentionDays int       // 1-365; 0 means omit (server defaults to 30)
}

// RestoreRequest is the request body for creating a clone restore. The clone
// route forces clone mode and ignores `mode`, so it is not exposed here.
type RestoreRequest struct {
	BackupID          string `json:"backupId"`
	IncludeJournal    bool   `json:"includeJournal"`
	TargetProjectName string `json:"targetProjectName"`
}
