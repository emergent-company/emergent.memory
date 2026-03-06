package workspaceimages

import (
	"time"

	"github.com/uptrace/bun"
)

// ImageType distinguishes built-in rootfs images from custom Docker images.
type ImageType string

const (
	ImageTypeBuiltIn ImageType = "built_in"
	ImageTypeCustom  ImageType = "custom"
)

// ImageStatus tracks whether an image is ready for use.
type ImageStatus string

const (
	ImageStatusPending ImageStatus = "pending"
	ImageStatusPulling ImageStatus = "pulling"
	ImageStatusReady   ImageStatus = "ready"
	ImageStatusError   ImageStatus = "error"
)

// ProviderName identifies which workspace provider handles this image.
type ProviderName string

const (
	ProviderFirecracker ProviderName = "firecracker"
	ProviderGVisor      ProviderName = "gvisor"
)

// WorkspaceImage represents a workspace image entry in the catalog.
type WorkspaceImage struct {
	bun.BaseModel `bun:"table:kb.workspace_images,alias:wi"`

	ID        string       `bun:"id,pk,type:uuid,default:gen_random_uuid()" json:"id"`
	Name      string       `bun:"name,notnull" json:"name"`
	Type      ImageType    `bun:"type,notnull,default:'custom'" json:"type"`
	DockerRef *string      `bun:"docker_ref" json:"docker_ref,omitempty"`
	Provider  ProviderName `bun:"provider,notnull,default:'firecracker'" json:"provider"`
	Status    ImageStatus  `bun:"status,notnull,default:'pending'" json:"status"`
	ErrorMsg  *string      `bun:"error_msg" json:"error_msg,omitempty"`
	ProjectID string       `bun:"project_id,notnull,type:uuid" json:"project_id"`
	CreatedAt time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time    `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// ToDTO converts the entity to its API representation.
func (w *WorkspaceImage) ToDTO() WorkspaceImageDTO {
	dto := WorkspaceImageDTO{
		ID:        w.ID,
		Name:      w.Name,
		Type:      string(w.Type),
		Provider:  string(w.Provider),
		Status:    string(w.Status),
		ProjectID: w.ProjectID,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}
	if w.DockerRef != nil {
		dto.DockerRef = *w.DockerRef
	}
	if w.ErrorMsg != nil {
		dto.ErrorMsg = *w.ErrorMsg
	}
	return dto
}

// WorkspaceImageDTO is the API response representation.
type WorkspaceImageDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	DockerRef string    `json:"docker_ref,omitempty"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	ErrorMsg  string    `json:"error_msg,omitempty"`
	ProjectID string    `json:"project_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateWorkspaceImageRequest is the API request to register a new image.
type CreateWorkspaceImageRequest struct {
	Name      string `json:"name" validate:"required"`
	DockerRef string `json:"docker_ref,omitempty"`
	Provider  string `json:"provider,omitempty"` // defaults based on docker_ref presence
}

// Validate checks the create request for basic correctness.
func (r *CreateWorkspaceImageRequest) Validate() error {
	if r.Name == "" {
		return ErrNameRequired
	}
	return nil
}

// APIResponse wraps a response with a standard envelope.
type APIResponse[T any] struct {
	Data T `json:"data"`
}

// ListResponse wraps a list of items.
type ListResponse[T any] struct {
	Data []T `json:"data"`
}
