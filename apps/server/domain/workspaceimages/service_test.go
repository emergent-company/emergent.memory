package workspaceimages

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Entity Tests
// =============================================================================

func TestWorkspaceImage_ToDTO(t *testing.T) {
	dockerRef := "python:3.12-slim"
	errMsg := "pull failed"

	img := &WorkspaceImage{
		ID:        "img-1",
		Name:      "py-ml",
		Type:      ImageTypeCustom,
		DockerRef: &dockerRef,
		Provider:  ProviderGVisor,
		Status:    ImageStatusError,
		ErrorMsg:  &errMsg,
		ProjectID: "proj-1",
	}

	dto := img.ToDTO()
	assert.Equal(t, "img-1", dto.ID)
	assert.Equal(t, "py-ml", dto.Name)
	assert.Equal(t, "custom", dto.Type)
	assert.Equal(t, "python:3.12-slim", dto.DockerRef)
	assert.Equal(t, "gvisor", dto.Provider)
	assert.Equal(t, "error", dto.Status)
	assert.Equal(t, "pull failed", dto.ErrorMsg)
	assert.Equal(t, "proj-1", dto.ProjectID)
}

func TestWorkspaceImage_ToDTO_NilOptionals(t *testing.T) {
	img := &WorkspaceImage{
		ID:        "img-2",
		Name:      "coder",
		Type:      ImageTypeBuiltIn,
		DockerRef: nil,
		Provider:  ProviderFirecracker,
		Status:    ImageStatusReady,
		ErrorMsg:  nil,
		ProjectID: "proj-1",
	}

	dto := img.ToDTO()
	assert.Equal(t, "", dto.DockerRef)
	assert.Equal(t, "", dto.ErrorMsg)
	assert.Equal(t, "built_in", dto.Type)
	assert.Equal(t, "firecracker", dto.Provider)
}

// =============================================================================
// Request Validation Tests
// =============================================================================

func TestCreateWorkspaceImageRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateWorkspaceImageRequest
		wantErr bool
	}{
		{
			name:    "valid name only",
			req:     CreateWorkspaceImageRequest{Name: "my-image"},
			wantErr: false,
		},
		{
			name:    "valid with docker ref",
			req:     CreateWorkspaceImageRequest{Name: "py-ml", DockerRef: "python:3.12-slim"},
			wantErr: false,
		},
		{
			name:    "empty name",
			req:     CreateWorkspaceImageRequest{Name: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, ErrNameRequired, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Utility Tests
// =============================================================================

func TestFileExists(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ext4")
	err := os.WriteFile(path, []byte("test"), 0644)
	require.NoError(t, err)

	assert.True(t, fileExists(path))
	assert.False(t, fileExists(filepath.Join(dir, "nonexistent.ext4")))
	assert.False(t, fileExists(dir)) // directory, not file
}

// =============================================================================
// Service Logic Tests (in-memory, no DB)
// =============================================================================

func TestBuiltInVariants(t *testing.T) {
	// Verify all expected variants are defined
	expected := []string{"base", "coder", "researcher", "reviewer"}
	for _, v := range expected {
		_, ok := builtInVariants[v]
		assert.True(t, ok, "expected built-in variant %q to be defined", v)
	}
	assert.Equal(t, 4, len(builtInVariants))
}

func TestServiceConfig_DefaultDataDir(t *testing.T) {
	svc := NewService(nil, testLogger(), ServiceConfig{})
	assert.Equal(t, "/var/lib/firecracker", svc.config.FirecrackerDataDir)
}

func TestServiceConfig_CustomDataDir(t *testing.T) {
	svc := NewService(nil, testLogger(), ServiceConfig{
		FirecrackerDataDir: "/custom/path",
	})
	assert.Equal(t, "/custom/path", svc.config.FirecrackerDataDir)
}

func TestService_RootfsPath(t *testing.T) {
	svc := NewService(nil, testLogger(), ServiceConfig{
		FirecrackerDataDir: "/var/lib/firecracker",
	})

	tests := []struct {
		name     string
		expected string
	}{
		{"base", "/var/lib/firecracker/rootfs-base.ext4"},
		{"coder", "/var/lib/firecracker/rootfs-coder.ext4"},
		{"researcher", "/var/lib/firecracker/rootfs-researcher.ext4"},
		{"reviewer", "/var/lib/firecracker/rootfs-reviewer.ext4"},
		{"unknown", "/var/lib/firecracker/rootfs-unknown.ext4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, svc.rootfsPath(tt.name))
		})
	}
}

func TestService_RootfsExists(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nil, testLogger(), ServiceConfig{
		FirecrackerDataDir: dir,
	})

	// No file exists yet
	assert.False(t, svc.rootfsExists("coder"))

	// Create the file
	path := filepath.Join(dir, "rootfs-coder.ext4")
	err := os.WriteFile(path, []byte("fake rootfs"), 0644)
	require.NoError(t, err)

	assert.True(t, svc.rootfsExists("coder"))
}

// =============================================================================
// Image Type & Status Constants
// =============================================================================

func TestImageTypeConstants(t *testing.T) {
	assert.Equal(t, ImageType("built_in"), ImageTypeBuiltIn)
	assert.Equal(t, ImageType("custom"), ImageTypeCustom)
}

func TestImageStatusConstants(t *testing.T) {
	assert.Equal(t, ImageStatus("pending"), ImageStatusPending)
	assert.Equal(t, ImageStatus("pulling"), ImageStatusPulling)
	assert.Equal(t, ImageStatus("ready"), ImageStatusReady)
	assert.Equal(t, ImageStatus("error"), ImageStatusError)
}

func TestProviderNameConstants(t *testing.T) {
	assert.Equal(t, ProviderName("firecracker"), ProviderFirecracker)
	assert.Equal(t, ProviderName("gvisor"), ProviderGVisor)
}

// =============================================================================
// Error Constants
// =============================================================================

func TestErrors(t *testing.T) {
	assert.Error(t, ErrNameRequired)
	assert.Error(t, ErrNameConflict)
	assert.Error(t, ErrNotFound)
	assert.Error(t, ErrBuiltInImmutable)

	assert.Contains(t, ErrNameRequired.Error(), "name")
	assert.Contains(t, ErrNameConflict.Error(), "already exists")
	assert.Contains(t, ErrNotFound.Error(), "not found")
	assert.Contains(t, ErrBuiltInImmutable.Error(), "built-in")
}
