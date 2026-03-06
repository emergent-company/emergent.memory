package workspaceimages

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// builtInVariants maps known Firecracker rootfs variant names to their rootfs file suffix.
// The auto-seed scanner looks for files matching "rootfs-{variant}.ext4".
var builtInVariants = map[string]string{
	"base":       "rootfs-base.ext4",
	"coder":      "rootfs-coder.ext4",
	"researcher": "rootfs-researcher.ext4",
	"reviewer":   "rootfs-reviewer.ext4",
}

// ServiceConfig holds configuration for the workspace images service.
type ServiceConfig struct {
	// FirecrackerDataDir is the directory where rootfs files live.
	// Defaults to /var/lib/firecracker.
	FirecrackerDataDir string
}

// Service provides business logic for workspace image management.
type Service struct {
	store  *Store
	config ServiceConfig
	log    *slog.Logger
	mu     sync.Mutex // protects concurrent Docker pulls
}

// NewService creates a new workspace images service.
func NewService(store *Store, log *slog.Logger, cfg ServiceConfig) *Service {
	if cfg.FirecrackerDataDir == "" {
		cfg.FirecrackerDataDir = "/var/lib/firecracker"
	}
	return &Service{
		store:  store,
		config: cfg,
		log:    log.With(slog.String("component", "workspaceimages.service")),
	}
}

// Resolve looks up an image by name in a project's catalog.
// Returns the image if found and ready, or an error explaining why it can't be used.
// This is the method called by the auto-provisioner at provision time.
func (s *Service) Resolve(ctx context.Context, projectID, name string) (*WorkspaceImage, error) {
	img, err := s.store.GetByName(ctx, projectID, name)
	if err != nil {
		return nil, fmt.Errorf("failed to look up image %q: %w", name, err)
	}
	if img == nil {
		return nil, fmt.Errorf("image %q not found in project catalog: %w", name, ErrNotFound)
	}
	if img.Status != ImageStatusReady {
		return nil, fmt.Errorf("image %q is not ready (status: %s)", name, img.Status)
	}
	return img, nil
}

// Create registers a new custom workspace image.
// If docker_ref is provided, the image is marked as "pulling" and a background goroutine
// does `docker pull`. If no docker_ref, the image is assumed to be a known Firecracker
// variant and marked ready immediately if the rootfs file exists.
func (s *Service) Create(ctx context.Context, projectID string, req *CreateWorkspaceImageRequest) (*WorkspaceImage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Check for duplicate name in project
	existing, err := s.store.GetByName(ctx, projectID, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing image: %w", err)
	}
	if existing != nil {
		return nil, ErrNameConflict
	}

	img := &WorkspaceImage{
		Name:      req.Name,
		ProjectID: projectID,
	}

	if req.DockerRef != "" {
		// Custom Docker image → gVisor provider, start with "pulling" status
		ref := req.DockerRef
		img.DockerRef = &ref
		img.Type = ImageTypeCustom
		img.Provider = ProviderGVisor
		img.Status = ImageStatusPulling

		// Allow explicit provider override
		if req.Provider != "" {
			img.Provider = ProviderName(req.Provider)
		}

		created, err := s.store.Create(ctx, img)
		if err != nil {
			return nil, fmt.Errorf("failed to create image record: %w", err)
		}

		// Start background Docker pull
		go s.pullDockerImage(created.ID, ref)

		return created, nil
	}

	// No docker_ref → assume built-in Firecracker variant
	img.Type = ImageTypeBuiltIn
	img.Provider = ProviderFirecracker
	if req.Provider != "" {
		img.Provider = ProviderName(req.Provider)
	}

	// Check if rootfs file exists on disk
	if s.rootfsExists(req.Name) {
		img.Status = ImageStatusReady
	} else {
		img.Status = ImageStatusPending
		s.log.Warn("rootfs file not found for built-in image",
			"name", req.Name,
			"expected_path", s.rootfsPath(req.Name),
		)
	}

	created, err := s.store.Create(ctx, img)
	if err != nil {
		return nil, fmt.Errorf("failed to create image record: %w", err)
	}
	return created, nil
}

// List returns all workspace images for a project.
func (s *Service) List(ctx context.Context, projectID string) ([]*WorkspaceImage, error) {
	return s.store.ListByProject(ctx, projectID)
}

// Get returns a workspace image by ID.
func (s *Service) Get(ctx context.Context, id string) (*WorkspaceImage, error) {
	return s.store.GetByID(ctx, id)
}

// Delete removes a custom workspace image. Built-in images cannot be deleted.
func (s *Service) Delete(ctx context.Context, id string) error {
	img, err := s.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get image: %w", err)
	}
	if img == nil {
		return ErrNotFound
	}
	if img.Type == ImageTypeBuiltIn {
		return ErrBuiltInImmutable
	}
	return s.store.Delete(ctx, id)
}

// SeedBuiltIns scans the Firecracker data directory for rootfs files and
// registers them as built-in images for the given project. This is called
// on server startup.
func (s *Service) SeedBuiltIns(ctx context.Context, projectID string) error {
	s.log.Info("seeding built-in workspace images",
		"project_id", projectID,
		"data_dir", s.config.FirecrackerDataDir,
	)

	seeded := 0
	for variant, filename := range builtInVariants {
		fullPath := filepath.Join(s.config.FirecrackerDataDir, filename)

		status := ImageStatusPending
		if fileExists(fullPath) {
			status = ImageStatusReady
		}

		img := &WorkspaceImage{
			Name:      variant,
			Type:      ImageTypeBuiltIn,
			Provider:  ProviderFirecracker,
			Status:    status,
			ProjectID: projectID,
		}

		_, err := s.store.UpsertByName(ctx, img)
		if err != nil {
			s.log.Error("failed to seed built-in image",
				"variant", variant,
				"error", err,
			)
			continue
		}

		s.log.Info("seeded built-in image",
			"variant", variant,
			"status", status,
			"path", fullPath,
		)
		seeded++
	}

	s.log.Info("built-in image seeding complete",
		"seeded", seeded,
		"total_variants", len(builtInVariants),
	)
	return nil
}

// pullDockerImage runs `docker pull` in the background and updates the image status.
func (s *Service) pullDockerImage(imageID, dockerRef string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()
	s.log.Info("pulling Docker image", "image_id", imageID, "docker_ref", dockerRef)

	cmd := exec.CommandContext(ctx, "docker", "pull", dockerRef)
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := fmt.Sprintf("docker pull failed: %s: %s", err.Error(), strings.TrimSpace(string(output)))
		s.log.Error("Docker pull failed",
			"image_id", imageID,
			"docker_ref", dockerRef,
			"error", errMsg,
		)
		_ = s.store.UpdateStatus(ctx, imageID, ImageStatusError, &errMsg)
		return
	}

	s.log.Info("Docker pull succeeded",
		"image_id", imageID,
		"docker_ref", dockerRef,
	)
	_ = s.store.UpdateStatus(ctx, imageID, ImageStatusReady, nil)
}

// rootfsPath returns the expected path for a named variant's rootfs file.
func (s *Service) rootfsPath(name string) string {
	filename, ok := builtInVariants[name]
	if !ok {
		filename = fmt.Sprintf("rootfs-%s.ext4", name)
	}
	return filepath.Join(s.config.FirecrackerDataDir, filename)
}

// rootfsExists checks whether the rootfs file for a named variant exists.
func (s *Service) rootfsExists(name string) bool {
	return fileExists(s.rootfsPath(name))
}
