package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
)

// UserProfileDto is the signed-in user's profile (GET/PUT /api/user/profile).
type UserProfileDto struct {
	ID              string `json:"id"`
	SubjectID       string `json:"subjectId,omitempty"`
	ZitadelUserID   string `json:"zitadelUserId,omitempty"`
	FirstName       string `json:"firstName,omitempty"`
	LastName        string `json:"lastName,omitempty"`
	DisplayName     string `json:"displayName,omitempty"`
	Email           string `json:"email,omitempty"`
	PhoneE164       string `json:"phoneE164,omitempty"`
	AvatarObjectKey string `json:"avatarObjectKey,omitempty"`
	// AvatarUrl is a gateway-relative path (/api/user/avatar?v=<key>) when the
	// user has an uploaded avatar photo, else empty. The gateway serves the
	// bytes at that path via avatarProxy.
	AvatarUrl string `json:"avatarUrl,omitempty"`
}

// UpdateUserProfileDto is the request to update the profile (PUT /api/user/profile).
type UpdateUserProfileDto struct {
	FirstName   string `json:"firstName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	PhoneE164   string `json:"phoneE164,omitempty"`
}

// GetProfile returns the signed-in user's profile (GET /api/user/profile).
func (m *MemoryClient) GetProfile(ctx context.Context) (*UserProfileDto, error) {
	var out UserProfileDto
	if err := m.do(ctx, http.MethodGet, "/api/user/profile", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateProfile updates the signed-in user's profile (PUT /api/user/profile).
func (m *MemoryClient) UpdateProfile(ctx context.Context, in UpdateUserProfileDto) (*UserProfileDto, error) {
	var out UserProfileDto
	if err := m.do(ctx, http.MethodPut, "/api/user/profile", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadAvatar uploads a profile photo for the signed-in user (multipart PUT
// /api/user/avatar, form field "file") and returns the updated profile, whose
// AvatarUrl is the gateway-relative path to serve the photo from.
func (m *MemoryClient) UploadAvatar(ctx context.Context, filename string, r io.Reader) (*UserProfileDto, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, r); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, m.baseURL+"/api/user/avatar", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	req.Header.Set("Content-Type", w.FormDataContentType())
	for k, v := range sessionHeaders(ctx) {
		req.Header.Set(k, v)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, parseMemoryError(resp.StatusCode, raw)
	}
	var out UserProfileDto
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAvatar removes the signed-in user's profile photo (DELETE
// /api/user/avatar) and returns the updated profile (AvatarUrl now empty).
func (m *MemoryClient) DeleteAvatar(ctx context.Context) (*UserProfileDto, error) {
	var out UserProfileDto
	if err := m.do(ctx, http.MethodDelete, "/api/user/avatar", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAvatar fetches the signed-in user's avatar photo bytes (GET
// /api/user/avatar). The raw body and its Content-Type are returned
// unparsed — the caller streams them to the response and owns the returned
// ReadCloser. A non-2xx response (e.g. 404 when no photo exists) is an error.
func (m *MemoryClient) GetAvatar(ctx context.Context) (io.ReadCloser, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+"/api/user/avatar", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	for k, v := range sessionHeaders(ctx) {
		req.Header.Set(k, v)
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if rerr != nil {
			return nil, "", rerr
		}
		return nil, "", parseMemoryError(resp.StatusCode, raw)
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}
