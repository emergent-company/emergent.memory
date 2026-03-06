package workspace

import (
	"context"
	"fmt"
	"time"
)

// Provider defines the interface that all workspace providers (Firecracker, E2B, gVisor) must implement.
type Provider interface {
	// Create provisions a new workspace container/VM and returns its provider-specific ID.
	Create(ctx context.Context, req *CreateContainerRequest) (*CreateContainerResult, error)

	// Destroy permanently removes a workspace and all its resources.
	Destroy(ctx context.Context, providerID string) error

	// Stop pauses/suspends a workspace without destroying it.
	Stop(ctx context.Context, providerID string) error

	// Resume resumes a previously stopped workspace.
	Resume(ctx context.Context, providerID string) error

	// Exec executes a command inside a workspace container.
	Exec(ctx context.Context, providerID string, req *ExecRequest) (*ExecResult, error)

	// ReadFile reads file content from a workspace container.
	ReadFile(ctx context.Context, providerID string, req *FileReadRequest) (*FileReadResult, error)

	// WriteFile writes file content to a workspace container.
	WriteFile(ctx context.Context, providerID string, req *FileWriteRequest) error

	// ListFiles returns files matching a glob pattern inside a workspace container.
	ListFiles(ctx context.Context, providerID string, req *FileListRequest) (*FileListResult, error)

	// Snapshot creates a point-in-time snapshot of a workspace's filesystem state.
	// Returns a snapshot ID that can be used with CreateFromSnapshot.
	// Returns ErrSnapshotNotSupported if the provider doesn't support snapshots.
	Snapshot(ctx context.Context, providerID string) (string, error)

	// CreateFromSnapshot creates a new workspace from a previously-taken snapshot.
	// Returns a new provider ID for the restored workspace.
	// Returns ErrSnapshotNotSupported if the provider doesn't support snapshots.
	CreateFromSnapshot(ctx context.Context, snapshotID string, req *CreateContainerRequest) (*CreateContainerResult, error)

	// Health checks the health of this provider.
	Health(ctx context.Context) (*HealthStatus, error)

	// Capabilities returns what this provider supports.
	Capabilities() *ProviderCapabilities
}

// ErrSnapshotNotSupported is returned by providers that do not support snapshot operations.
var ErrSnapshotNotSupported = fmt.Errorf("snapshot operations not supported by this provider")

// ProviderCapabilities describes what a provider supports.
type ProviderCapabilities struct {
	Name                string       `json:"name"`
	SupportsPersistence bool         `json:"supports_persistence"`
	SupportsSnapshots   bool         `json:"supports_snapshots"`
	SupportsWarmPool    bool         `json:"supports_warm_pool"`
	RequiresKVM         bool         `json:"requires_kvm"`
	EstimatedStartupMs  int          `json:"estimated_startup_ms"`
	ProviderType        ProviderType `json:"provider_type"`
}

// CreateContainerRequest holds parameters for creating a new workspace container.
type CreateContainerRequest struct {
	ContainerType  ContainerType     `json:"container_type"`
	ResourceLimits *ResourceLimits   `json:"resource_limits,omitempty"`
	BaseImage      string            `json:"base_image,omitempty"` // Docker image for gVisor/MCP
	Labels         map[string]string `json:"labels,omitempty"`     // Metadata labels on the container
	// MCP-specific fields for container customization
	Cmd          []string          `json:"cmd,omitempty"`           // Override container entrypoint command
	Env          map[string]string `json:"env,omitempty"`           // Environment variables
	ExtraVolumes []string          `json:"extra_volumes,omitempty"` // Additional volume mount paths (e.g. "/data")
	AttachStdin  bool              `json:"attach_stdin,omitempty"`  // Keep stdin open for stdio bridge
}

// CreateContainerResult holds the result of a container creation.
type CreateContainerResult struct {
	ProviderID string `json:"provider_id"` // Provider-specific container/VM ID
}

// ExecRequest holds parameters for executing a command inside a workspace.
type ExecRequest struct {
	Command   string `json:"command"`
	Workdir   string `json:"workdir,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"` // 0 = default (120000ms)
}

// ExecResult holds the result of a command execution.
type ExecResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// FileReadRequest holds parameters for reading a file from a workspace.
type FileReadRequest struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"` // 1-indexed line number to start from
	Limit    int    `json:"limit,omitempty"`  // Max lines to read (0 = all)
}

// FileReadResult holds the result of a file read operation.
type FileReadResult struct {
	Content    string `json:"content"`
	IsDir      bool   `json:"is_dir,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	IsBinary   bool   `json:"is_binary,omitempty"`
}

// FileWriteRequest holds parameters for writing a file to a workspace.
type FileWriteRequest struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// FileListRequest holds parameters for listing files matching a glob pattern.
type FileListRequest struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"` // Directory to search in
}

// FileListResult holds the result of a file listing.
type FileListResult struct {
	Files []FileInfo `json:"files"`
}

// FileInfo describes a file returned from a listing.
type FileInfo struct {
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir,omitempty"`
	Size       int64     `json:"size,omitempty"`
	ModifiedAt time.Time `json:"modified_at,omitempty"`
}

// HealthStatus reports the health of a provider.
type HealthStatus struct {
	Healthy       bool   `json:"healthy"`
	Message       string `json:"message,omitempty"`
	ActiveCount   int    `json:"active_count"`    // Currently running workspaces
	AvailableSlot int    `json:"available_slots"` // Remaining capacity
}
