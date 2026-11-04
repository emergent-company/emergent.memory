package userprofile

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
	_ "golang.org/x/image/webp"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// maxAvatarSize is the maximum accepted avatar upload size (512 KiB).
const maxAvatarSize = 1 << 19

// maxAvatarDimension caps the decoded pixel dimensions, rejecting
// "decompression bomb" images (small file, huge pixel buffer) before the
// full decode allocates memory. 1024x1024 bounds the worst-case decode at
// ~4 MiB (RGBA), ample for an avatar.
const maxAvatarDimension = 1024

// allowedAvatarTypes are the image content types accepted for avatars,
// matching the sniffed output of http.DetectContentType.
var allowedAvatarTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

// Handler handles HTTP requests for user profiles
type Handler struct {
	svc *Service
}

// NewHandler creates a new user profile handler
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Get returns the current user's profile
// @Summary      Get user profile
// @Description  Returns the authenticated user's profile information
// @Tags         user-profile
// @Accept       json
// @Produce      json
// @Success      200 {object} ProfileDTO "User profile"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "Profile not found"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user/profile [get]
// @Security     bearerAuth
func (h *Handler) Get(c echo.Context) error {
	user := auth.MustGetUser(c)

	profile, err := h.svc.GetByID(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, profile)
}

// Update updates the current user's profile
// @Summary      Update user profile
// @Description  Updates the authenticated user's profile information (name, phone, etc.)
// @Tags         user-profile
// @Accept       json
// @Produce      json
// @Param        request body UpdateProfileRequest true "Profile updates"
// @Success      200 {object} ProfileDTO "Updated profile"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user/profile [put]
// @Security     bearerAuth
func (h *Handler) Update(c echo.Context) error {
	user := auth.MustGetUser(c)

	var req UpdateProfileRequest
	if err := c.Bind(&req); err != nil {
		return apperror.ErrBadRequest.WithMessage("invalid request body")
	}

	// Validate request - at least one field should be provided
	if req.FirstName == nil && req.LastName == nil && req.DisplayName == nil && req.PhoneE164 == nil {
		return apperror.ErrBadRequest.WithMessage("at least one field must be provided")
	}

	// Validate field lengths if provided
	if req.FirstName != nil && len(*req.FirstName) > 100 {
		return apperror.ErrBadRequest.WithMessage("firstName must be at most 100 characters")
	}
	if req.LastName != nil && len(*req.LastName) > 100 {
		return apperror.ErrBadRequest.WithMessage("lastName must be at most 100 characters")
	}
	if req.DisplayName != nil && len(*req.DisplayName) > 200 {
		return apperror.ErrBadRequest.WithMessage("displayName must be at most 200 characters")
	}
	if req.PhoneE164 != nil && len(*req.PhoneE164) > 20 {
		return apperror.ErrBadRequest.WithMessage("phoneE164 must be at most 20 characters")
	}

	profile, err := h.svc.Update(c.Request().Context(), user.ID, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, profile)
}

// Upload handles avatar image uploads (multipart field "file").
// @Summary      Upload avatar
// @Description  Uploads a new avatar image for the authenticated user (PNG, JPEG, WEBP, GIF; max 512 KiB, max 1024x1024 pixels)
// @Tags         user-profile
// @Accept       multipart/form-data
// @Produce      json
// @Param        file formData file true "Avatar image file"
// @Success      200 {object} ProfileDTO "Updated profile"
// @Failure      400 {object} apperror.Error "Bad request"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      413 {object} apperror.Error "File too large"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user/avatar [put]
// @Security     bearerAuth
func (h *Handler) Upload(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	// Enforce the byte limit before the multipart body is parsed to temp disk.
	// Headroom above maxAvatarSize covers multipart framing overhead.
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxAvatarSize+1024)

	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return apperror.New(http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar image must be 512 KiB or smaller")
		}
		return apperror.ErrBadRequest.WithMessage("file is required")
	}

	src, err := file.Open()
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("failed to read file")
	}
	defer src.Close()

	// Enforce the size limit by reading at most maxAvatarSize+1 bytes. If the
	// read exceeds the limit the file is too large.
	data, err := io.ReadAll(io.LimitReader(src, maxAvatarSize+1))
	if err != nil {
		return apperror.ErrBadRequest.WithMessage("failed to read file")
	}
	if int64(len(data)) > maxAvatarSize {
		return apperror.New(http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar image must be 512 KiB or smaller")
	}

	// Sniff magic bytes rather than trusting the client-declared content type.
	contentType := http.DetectContentType(data)
	if !allowedAvatarTypes[contentType] {
		return apperror.ErrBadRequest.WithMessage("unsupported image type: only PNG, JPEG, WEBP, and GIF images are allowed")
	}

	// Validate the image decodes and its dimensions are sane. DecodeConfig is
	// header-only (cheap) and lets us reject oversized images before Decode
	// allocates the full pixel buffer.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return apperror.NewBadRequest("invalid image header")
	}
	if cfg.Width > maxAvatarDimension || cfg.Height > maxAvatarDimension {
		return apperror.NewBadRequest("image dimensions too large")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return apperror.NewBadRequest("corrupt image data")
	}

	profile, err := h.svc.UploadAvatar(c.Request().Context(), user.ID, bytes.NewReader(data), int64(len(data)), contentType)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, profile)
}

// GetAvatar streams the authenticated user's avatar image.
// @Summary      Get avatar
// @Description  Returns the authenticated user's avatar image
// @Tags         user-profile
// @Produce      image/png, image/jpeg, image/webp, image/gif
// @Success      200 {file} binary "Avatar image"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      404 {object} apperror.Error "No avatar"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user/avatar [get]
// @Security     bearerAuth
func (h *Handler) GetAvatar(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	body, contentType, err := h.svc.GetAvatar(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}
	defer body.Close()

	c.Response().Header().Set(echo.HeaderContentType, contentType)
	if _, err := io.Copy(c.Response(), body); err != nil {
		return apperror.ErrInternal.WithInternal(err)
	}

	return nil
}

// DeleteAvatar removes the authenticated user's avatar.
// @Summary      Delete avatar
// @Description  Removes the authenticated user's avatar image
// @Tags         user-profile
// @Produce      json
// @Success      200 {object} ProfileDTO "Updated profile"
// @Failure      401 {object} apperror.Error "Unauthorized"
// @Failure      500 {object} apperror.Error "Internal server error"
// @Router       /api/user/avatar [delete]
// @Security     bearerAuth
func (h *Handler) DeleteAvatar(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return apperror.ErrUnauthorized
	}

	profile, err := h.svc.RemoveAvatar(c.Request().Context(), user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, profile)
}
