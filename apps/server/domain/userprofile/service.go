package userprofile

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// profileRepo is the subset of Repository used by the service. Defined as an
// interface so tests can substitute a fake without a database connection.
type profileRepo interface {
	GetByID(ctx context.Context, id string) (*Profile, error)
	GetByZitadelUserID(ctx context.Context, zitadelUserID string) (*Profile, error)
	Update(ctx context.Context, id string, req *UpdateProfileRequest) (*Profile, error)
	GetEmail(ctx context.Context, userID string) (string, error)
	SetAvatar(ctx context.Context, id string, key *string) error
}

// avatarStore is the subset of storage.Service used for avatar objects.
// Defined as an interface so tests can substitute a fake without an S3 client.
type avatarStore interface {
	Upload(ctx context.Context, key string, data io.Reader, size int64, opts storage.UploadOptions) (*storage.UploadResult, error)
	Delete(ctx context.Context, key string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Enabled() bool
}

// Service handles business logic for user profiles
type Service struct {
	repo    profileRepo
	storage avatarStore
	log     *slog.Logger
}

// NewService creates a new user profile service
func NewService(repo *Repository, storage *storage.Service, log *slog.Logger) *Service {
	return &Service{
		repo:    repo,
		storage: storage,
		log:     log.With(logger.Scope("userprofile.svc")),
	}
}

// GetByID retrieves a user profile by internal ID and returns as DTO
func (s *Service) GetByID(ctx context.Context, id string) (*ProfileDTO, error) {
	profile, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Fetch email
	email, _ := s.repo.GetEmail(ctx, profile.ID)

	dto := profile.ToDTO(email)
	return &dto, nil
}

// GetByZitadelUserID retrieves a user profile by Zitadel user ID and returns as DTO
func (s *Service) GetByZitadelUserID(ctx context.Context, zitadelUserID string) (*ProfileDTO, error) {
	profile, err := s.repo.GetByZitadelUserID(ctx, zitadelUserID)
	if err != nil {
		return nil, err
	}

	// Fetch email
	email, _ := s.repo.GetEmail(ctx, profile.ID)

	dto := profile.ToDTO(email)
	return &dto, nil
}

// Update updates a user profile and returns the updated DTO
func (s *Service) Update(ctx context.Context, id string, req *UpdateProfileRequest) (*ProfileDTO, error) {
	profile, err := s.repo.Update(ctx, id, req)
	if err != nil {
		return nil, err
	}

	// Fetch email
	email, _ := s.repo.GetEmail(ctx, profile.ID)

	dto := profile.ToDTO(email)
	return &dto, nil
}

// UploadAvatar uploads a new avatar image for the profile, replacing (and
// deleting) any previously stored avatar object. Returns the updated profile DTO.
func (s *Service) UploadAvatar(ctx context.Context, id string, data io.Reader, size int64, contentType string) (*ProfileDTO, error) {
	if !s.storage.Enabled() {
		return nil, apperror.ErrServiceUnavailable.WithMessage("storage disabled")
	}

	if _, ok := avatarExtForContentType(contentType); !ok {
		return nil, apperror.ErrBadRequest.WithMessage("unsupported image type")
	}

	// Normalize the image before it is stored: center-crop to a square, scale
	// to avatarOutputSize and re-encode. Re-encoding strips EXIF/GPS metadata,
	// and animated GIFs are flattened to their first frame. The output format
	// (PNG with alpha preserved, otherwise JPEG) determines the object key
	// extension and content type.
	raw, err := io.ReadAll(data)
	if err != nil {
		return nil, apperror.NewBadRequest("corrupt image data")
	}
	normalized, err := normalizeAvatar(raw)
	if err != nil {
		// The HTTP handler fully decodes validated uploads before calling this
		// service, so a decode failure here means the bytes changed in transit
		// or the format is unsupported by the registered decoders.
		return nil, apperror.NewBadRequest("corrupt image data")
	}
	key := "avatars/" + uuid.New().String() + normalized.ext

	// Get the current profile first so the previous avatar object can be
	// cleaned up after the new one is in place.
	profile, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Upload the new object before touching the stored reference so a failed
	// upload leaves the previous avatar untouched.
	if _, err := s.storage.Upload(ctx, key, bytes.NewReader(normalized.data), int64(len(normalized.data)), storage.UploadOptions{ContentType: normalized.contentType}); err != nil {
		return nil, apperror.ErrInternal.WithInternal(err)
	}

	// Persist the new key. On failure, best-effort remove the just-uploaded
	// object so it is not orphaned; the profile still references the old key,
	// which remains valid.
	if err := s.repo.SetAvatar(ctx, id, &key); err != nil {
		if delErr := s.storage.Delete(ctx, key); delErr != nil {
			s.log.Warn("failed to clean up avatar object after persist failure",
				slog.String("key", key),
				logger.Error(delErr),
			)
		}
		return nil, err
	}

	// The previous object can now be removed. Failure is non-fatal: the new
	// avatar is already live and the orphan is a storage-hygiene concern.
	if profile.AvatarObjectKey != nil && *profile.AvatarObjectKey != "" && *profile.AvatarObjectKey != key {
		if delErr := s.storage.Delete(ctx, *profile.AvatarObjectKey); delErr != nil {
			s.log.Warn("failed to delete previous avatar object",
				slog.String("key", *profile.AvatarObjectKey),
				logger.Error(delErr),
			)
		}
	}

	return s.GetByID(ctx, id)
}

// RemoveAvatar deletes the profile's avatar object (if any) and clears the
// reference. Returns the updated profile DTO.
func (s *Service) RemoveAvatar(ctx context.Context, id string) (*ProfileDTO, error) {
	profile, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if profile.AvatarObjectKey != nil && *profile.AvatarObjectKey != "" {
		// Delete the object before clearing the reference. If deletion fails the
		// reference is kept so a client retry can succeed, instead of the profile
		// pointing at a key whose object still exists in storage.
		if err := s.storage.Delete(ctx, *profile.AvatarObjectKey); err != nil {
			s.log.Warn("failed to delete avatar object",
				slog.String("key", *profile.AvatarObjectKey),
				logger.Error(err),
			)
			return nil, apperror.ErrInternal.WithInternal(err)
		}
	}

	if err := s.repo.SetAvatar(ctx, id, nil); err != nil {
		return nil, err
	}

	return s.GetByID(ctx, id)
}

// GetAvatar streams the profile's avatar image body. Returns the object body
// and its content type. Returns ErrNotFound when the profile has no avatar.
func (s *Service) GetAvatar(ctx context.Context, id string) (io.ReadCloser, string, error) {
	profile, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, "", err
	}

	if profile.AvatarObjectKey == nil || *profile.AvatarObjectKey == "" {
		return nil, "", apperror.ErrNotFound.WithMessage("user has no avatar")
	}

	body, err := s.storage.Download(ctx, *profile.AvatarObjectKey)
	if err != nil {
		return nil, "", apperror.ErrInternal.WithInternal(err)
	}

	return body, avatarContentTypeForKey(*profile.AvatarObjectKey), nil
}

// avatarExtForContentType maps an allowed image content type to its storage
// key extension. Returns false for unsupported types.
func avatarExtForContentType(contentType string) (string, bool) {
	switch contentType {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/webp":
		return ".webp", true
	case "image/gif":
		return ".gif", true
	default:
		return "", false
	}
}

// avatarContentTypeForKey derives the served content type from the avatar
// object key extension.
func avatarContentTypeForKey(key string) string {
	switch {
	case strings.HasSuffix(key, ".png"):
		return "image/png"
	case strings.HasSuffix(key, ".jpg"), strings.HasSuffix(key, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(key, ".webp"):
		return "image/webp"
	case strings.HasSuffix(key, ".gif"):
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}
