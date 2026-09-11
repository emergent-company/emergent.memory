package userprofile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeProfileRepo is an in-memory profileRepo implementation.
type fakeProfileRepo struct {
	profiles     map[string]Profile
	email        string
	setAvatarErr error
}

func newFakeProfileRepo() *fakeProfileRepo {
	return &fakeProfileRepo{profiles: map[string]Profile{}, email: ""}
}

func (f *fakeProfileRepo) seed(p Profile) { f.profiles[p.ID] = p }

func cloneProfile(p Profile) *Profile {
	c := p
	if p.FirstName != nil {
		v := *p.FirstName
		c.FirstName = &v
	}
	if p.LastName != nil {
		v := *p.LastName
		c.LastName = &v
	}
	if p.DisplayName != nil {
		v := *p.DisplayName
		c.DisplayName = &v
	}
	if p.PhoneE164 != nil {
		v := *p.PhoneE164
		c.PhoneE164 = &v
	}
	if p.AvatarObjectKey != nil {
		v := *p.AvatarObjectKey
		c.AvatarObjectKey = &v
	}
	if p.DeletedAt != nil {
		v := *p.DeletedAt
		c.DeletedAt = &v
	}
	if p.DeletedBy != nil {
		v := *p.DeletedBy
		c.DeletedBy = &v
	}
	return &c
}

func (f *fakeProfileRepo) GetByID(_ context.Context, id string) (*Profile, error) {
	p, ok := f.profiles[id]
	if !ok {
		return nil, apperror.ErrNotFound.WithMessage("user profile not found")
	}
	return cloneProfile(p), nil
}

func (f *fakeProfileRepo) GetByZitadelUserID(_ context.Context, zitadelUserID string) (*Profile, error) {
	for _, p := range f.profiles {
		if p.ZitadelUserID == zitadelUserID {
			return cloneProfile(p), nil
		}
	}
	return nil, apperror.ErrNotFound.WithMessage("user profile not found")
}

func (f *fakeProfileRepo) Update(_ context.Context, id string, req *UpdateProfileRequest) (*Profile, error) {
	p, ok := f.profiles[id]
	if !ok {
		return nil, apperror.ErrNotFound.WithMessage("user profile not found")
	}
	if req.FirstName != nil {
		v := *req.FirstName
		p.FirstName = &v
	}
	if req.LastName != nil {
		v := *req.LastName
		p.LastName = &v
	}
	if req.DisplayName != nil {
		v := *req.DisplayName
		p.DisplayName = &v
	}
	if req.PhoneE164 != nil {
		v := *req.PhoneE164
		p.PhoneE164 = &v
	}
	f.profiles[id] = p
	return cloneProfile(p), nil
}

func (f *fakeProfileRepo) GetEmail(_ context.Context, _ string) (string, error) {
	return f.email, nil
}

func (f *fakeProfileRepo) SetAvatar(_ context.Context, id string, key *string) error {
	if f.setAvatarErr != nil {
		return f.setAvatarErr
	}
	p, ok := f.profiles[id]
	if !ok {
		return apperror.ErrNotFound.WithMessage("user profile not found")
	}
	if key != nil {
		v := *key
		p.AvatarObjectKey = &v
	} else {
		p.AvatarObjectKey = nil
	}
	f.profiles[id] = p
	return nil
}

// fakeAvatarStore is an in-memory avatarStore implementation.
type fakeAvatarStore struct {
	enabled     bool
	objects     map[string][]byte
	uploadErr   error
	downloadErr error
	deleteErr   error
	deletedKeys []string
	uploaded    []string
}

func newFakeAvatarStore(enabled bool) *fakeAvatarStore {
	return &fakeAvatarStore{enabled: enabled, objects: map[string][]byte{}}
}

func (f *fakeAvatarStore) Enabled() bool { return f.enabled }

func (f *fakeAvatarStore) Upload(_ context.Context, key string, data io.Reader, _ int64, _ storage.UploadOptions) (*storage.UploadResult, error) {
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	body, err := io.ReadAll(data)
	if err != nil {
		return nil, err
	}
	f.objects[key] = body
	f.uploaded = append(f.uploaded, key)
	return &storage.UploadResult{Key: key, Size: int64(len(body))}, nil
}

func (f *fakeAvatarStore) Delete(_ context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.objects, key)
	f.deletedKeys = append(f.deletedKeys, key)
	return nil
}

func (f *fakeAvatarStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	body, ok := f.objects[key]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestAvatarService(repo profileRepo, store avatarStore) *Service {
	return &Service{repo: repo, storage: store, log: testLogger()}
}

func assertAppErrorStatus(t *testing.T, err error, wantStatus int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T: %v", err, err)
	}
	if appErr.HTTPStatus != wantStatus {
		t.Errorf("HTTPStatus = %d, want %d", appErr.HTTPStatus, wantStatus)
	}
}

// assertAppError checks both the status and the message of a returned app
// error, so handler tests can pin down which validation stage rejected an
// image (header vs. dimensions vs. full-decode corruption).
func assertAppError(t *testing.T, err error, wantStatus int, wantMessage string) {
	t.Helper()
	assertAppErrorStatus(t, err, wantStatus)
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("expected *apperror.Error, got %T: %v", err, err)
	}
	if appErr.Message != wantMessage {
		t.Errorf("Message = %q, want %q", appErr.Message, wantMessage)
	}
}

func wantAvatarURL(key string) string {
	return "/api/user/avatar?v=" + url.QueryEscape(key)
}

// ---------------------------------------------------------------------------
// Service tests
// ---------------------------------------------------------------------------

func TestService_UploadAvatar_StoresKeyAndReturnsDTO(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	svc := newTestAvatarService(repo, store)

	png := testPNGBytes
	dto, err := svc.UploadAvatar(context.Background(), "profile-1", bytes.NewReader(png), int64(len(png)), "image/png")
	if err != nil {
		t.Fatalf("UploadAvatar returned error: %v", err)
	}

	if dto.AvatarObjectKey == nil || *dto.AvatarObjectKey == "" {
		t.Fatalf("expected avatar object key to be set, got %v", dto.AvatarObjectKey)
	}
	key := *dto.AvatarObjectKey
	if len(key) < len("avatars/") || key[:len("avatars/")] != "avatars/" {
		t.Errorf("key = %q, want prefix %q", key, "avatars/")
	}
	if key[len(key)-len(".png"):] != ".png" {
		t.Errorf("key = %q, want suffix %q", key, ".png")
	}
	if dto.AvatarURL != wantAvatarURL(key) {
		t.Errorf("AvatarURL = %q, want %q", dto.AvatarURL, wantAvatarURL(key))
	}

	// Object is present in storage and profile row is updated.
	if _, ok := store.objects[key]; !ok {
		t.Errorf("object %q not found in store", key)
	}
	got, err := repo.GetByID(context.Background(), "profile-1")
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if got.AvatarObjectKey == nil || *got.AvatarObjectKey != key {
		t.Errorf("profile avatar key = %v, want %q", got.AvatarObjectKey, key)
	}
}

func TestService_UploadAvatar_DeletesPriorObject(t *testing.T) {
	oldKey := "avatars/old-avatar.png"
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1", AvatarObjectKey: &oldKey})
	store := newFakeAvatarStore(true)
	store.objects[oldKey] = []byte("old image")
	svc := newTestAvatarService(repo, store)

	png := testPNGBytes
	dto, err := svc.UploadAvatar(context.Background(), "profile-1", bytes.NewReader(png), int64(len(png)), "image/png")
	if err != nil {
		t.Fatalf("UploadAvatar returned error: %v", err)
	}

	if _, ok := store.objects[oldKey]; ok {
		t.Errorf("old avatar object %q was not deleted", oldKey)
	}
	if *dto.AvatarObjectKey == oldKey {
		t.Errorf("profile still references old key %q", oldKey)
	}
}

func TestService_UploadAvatar_UnsupportedContentType(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	svc := newTestAvatarService(repo, store)

	_, err := svc.UploadAvatar(context.Background(), "profile-1", bytes.NewReader([]byte("data")), 4, "text/plain")
	assertAppErrorStatus(t, err, 400)
	if len(store.objects) != 0 {
		t.Errorf("expected no object uploaded, got %v", store.objects)
	}
}

func TestService_UploadAvatar_StorageDisabled(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(false)
	svc := newTestAvatarService(repo, store)

	_, err := svc.UploadAvatar(context.Background(), "profile-1", bytes.NewReader([]byte("data")), 4, "image/png")
	assertAppErrorStatus(t, err, http.StatusServiceUnavailable)
}

func TestService_UploadAvatar_UploadFails_KeepsExistingAvatar(t *testing.T) {
	oldKey := "avatars/old-avatar.png"
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1", AvatarObjectKey: &oldKey})
	store := newFakeAvatarStore(true)
	store.objects[oldKey] = []byte("old image")
	store.uploadErr = errors.New("upload failed")
	svc := newTestAvatarService(repo, store)

	png := testPNGBytes
	_, err := svc.UploadAvatar(context.Background(), "profile-1", bytes.NewReader(png), int64(len(png)), "image/png")
	assertAppErrorStatus(t, err, 500)

	// The old object must survive a failed upload and the profile must keep
	// referencing it.
	if _, ok := store.objects[oldKey]; !ok {
		t.Errorf("existing avatar object %q was deleted despite failed upload", oldKey)
	}
	if len(store.deletedKeys) != 0 {
		t.Errorf("expected no storage deletes, got %v", store.deletedKeys)
	}
	got, err := repo.GetByID(context.Background(), "profile-1")
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if got.AvatarObjectKey == nil || *got.AvatarObjectKey != oldKey {
		t.Errorf("profile avatar key = %v, want %q", got.AvatarObjectKey, oldKey)
	}
}

func TestService_UploadAvatar_PersistFails_DeletesNewObject(t *testing.T) {
	oldKey := "avatars/old-avatar.png"
	repo := newFakeProfileRepo()
	repo.setAvatarErr = errors.New("persist failed")
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1", AvatarObjectKey: &oldKey})
	store := newFakeAvatarStore(true)
	store.objects[oldKey] = []byte("old image")
	svc := newTestAvatarService(repo, store)

	png := testPNGBytes
	_, err := svc.UploadAvatar(context.Background(), "profile-1", bytes.NewReader(png), int64(len(png)), "image/png")
	if err == nil {
		t.Fatal("expected UploadAvatar to fail")
	}

	// The just-uploaded object must be cleaned up best-effort and the old
	// avatar must be untouched.
	if len(store.objects) != 1 {
		t.Errorf("expected only the old object to remain, got %d objects", len(store.objects))
	}
	if _, ok := store.objects[oldKey]; !ok {
		t.Errorf("existing avatar object %q was deleted", oldKey)
	}
	if len(store.deletedKeys) != 1 {
		t.Fatalf("expected exactly one cleanup delete, got %v", store.deletedKeys)
	}
	if store.deletedKeys[0] == oldKey || !strings.HasPrefix(store.deletedKeys[0], "avatars/") {
		t.Errorf("cleanup delete key = %q, want the newly uploaded key", store.deletedKeys[0])
	}
	got, err := repo.GetByID(context.Background(), "profile-1")
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if got.AvatarObjectKey == nil || *got.AvatarObjectKey != oldKey {
		t.Errorf("profile avatar key = %v, want %q", got.AvatarObjectKey, oldKey)
	}
}

func TestService_RemoveAvatar_ClearsKeyAndDeletesObject(t *testing.T) {
	key := "avatars/abc.png"
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1", AvatarObjectKey: &key})
	store := newFakeAvatarStore(true)
	store.objects[key] = []byte("image")
	svc := newTestAvatarService(repo, store)

	dto, err := svc.RemoveAvatar(context.Background(), "profile-1")
	if err != nil {
		t.Fatalf("RemoveAvatar returned error: %v", err)
	}

	if _, ok := store.objects[key]; ok {
		t.Errorf("avatar object %q was not deleted", key)
	}
	if dto.AvatarObjectKey != nil {
		t.Errorf("AvatarObjectKey = %q, want nil", *dto.AvatarObjectKey)
	}
	if dto.AvatarURL != "" {
		t.Errorf("AvatarURL = %q, want empty", dto.AvatarURL)
	}
	got, err := repo.GetByID(context.Background(), "profile-1")
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if got.AvatarObjectKey != nil {
		t.Errorf("profile avatar key = %q, want nil", *got.AvatarObjectKey)
	}
}

func TestService_RemoveAvatar_NoAvatar(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	svc := newTestAvatarService(repo, store)

	dto, err := svc.RemoveAvatar(context.Background(), "profile-1")
	if err != nil {
		t.Fatalf("RemoveAvatar returned error: %v", err)
	}
	if dto.AvatarObjectKey != nil || dto.AvatarURL != "" {
		t.Errorf("expected no avatar in DTO, got key=%v url=%q", dto.AvatarObjectKey, dto.AvatarURL)
	}
}

func TestService_GetAvatar_ReturnsBodyAndContentType(t *testing.T) {
	key := "avatars/xyz.jpg"
	image := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1", AvatarObjectKey: &key})
	store := newFakeAvatarStore(true)
	store.objects[key] = image
	svc := newTestAvatarService(repo, store)

	body, contentType, err := svc.GetAvatar(context.Background(), "profile-1")
	if err != nil {
		t.Fatalf("GetAvatar returned error: %v", err)
	}
	defer body.Close()

	if contentType != "image/jpeg" {
		t.Errorf("contentType = %q, want %q", contentType, "image/jpeg")
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if !bytes.Equal(got, image) {
		t.Errorf("body = %v, want %v", got, image)
	}
}

func TestService_GetAvatar_WebPContentTypeFromKey(t *testing.T) {
	key := "avatars/xyz.webp"
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1", AvatarObjectKey: &key})
	store := newFakeAvatarStore(true)
	store.objects[key] = []byte("webp")
	svc := newTestAvatarService(repo, store)

	body, contentType, err := svc.GetAvatar(context.Background(), "profile-1")
	if err != nil {
		t.Fatalf("GetAvatar returned error: %v", err)
	}
	defer body.Close()

	if contentType != "image/webp" {
		t.Errorf("contentType = %q, want %q", contentType, "image/webp")
	}
}

func TestService_GetAvatar_NoAvatar_ReturnsNotFound(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	svc := newTestAvatarService(repo, store)

	_, _, err := svc.GetAvatar(context.Background(), "profile-1")
	assertAppErrorStatus(t, err, 404)
}
