package userprofile

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

var (
	// testPNGBytes is a structurally valid 1x1 PNG produced by the stdlib
	// encoder. Upload validation now runs a real image decode, so the
	// signature-only fixtures previously used here would be rejected as
	// malformed.
	testPNGBytes = mustEncodePNG()
	// testJPEGBytes is a structurally valid 1x1 JPEG produced by the stdlib
	// encoder (JPEG has no header-only dimension profile, so validation needs
	// a genuinely decodable stream).
	testJPEGBytes = mustEncodeJPEG()
	// testGIFBytes is a single-frame 1x1 GIF produced by the stdlib encoder.
	testGIFBytes = mustEncodeGIF()
	// testWebPBytes is a genuine lossless WebP stream (75x100) copied from the
	// golang.org/x/image module testdata; it proves the x/image/webp decoder
	// registration actually decodes real WebP data rather than being accepted
	// on signature sniffing alone.
	testWebPBytes = mustLoadTestdata("gopher-doc.1bpp.lossless.webp")
	testSVGBytes  = []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`)
	testTextBytes = []byte("plain text, not an image")
)

func mustLoadTestdata(name string) []byte {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		panic("load testdata/" + name + ": " + err.Error())
	}
	return b
}

func mustEncodePNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("encode 1x1 png: " + err.Error())
	}
	return buf.Bytes()
}

func mustEncodeJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic("encode 1x1 jpeg: " + err.Error())
	}
	return buf.Bytes()
}

func mustEncodeGIF() []byte {
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), palette.Plan9)
	img.Set(0, 0, color.RGBA{R: 200, G: 30, B: 30, A: 0xFF})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		panic("encode 1x1 gif: " + err.Error())
	}
	return buf.Bytes()
}

// oversizeDimensionPNG rewrites the IHDR width/height of the 1x1 PNG fixture
// to maxAvatarDimension+1 on each axis and recomputes the IHDR CRC so the
// header parses cleanly. DecodeConfig trusts the IHDR header for the
// dimension pre-check, so the guard trips before the full decode — the stale
// 1x1 IDAT payload never gets decoded, so no genuinely large (and slow)
// pixel buffer is ever produced. PNG layout: signature[0:8], IHDR chunk
// length[8:12], type[12:16], width[16:20], height[20:24], data[24:29],
// CRC[29:33]; the CRC covers chunk type + data.
func oversizeDimensionPNG() []byte {
	b := append([]byte(nil), testPNGBytes...)
	binary.BigEndian.PutUint32(b[16:20], maxAvatarDimension+1)
	binary.BigEndian.PutUint32(b[20:24], maxAvatarDimension+1)
	crc := crc32.ChecksumIEEE(b[12:29])
	binary.BigEndian.PutUint32(b[29:33], crc)
	return b
}

// truncatedIDATPNG cuts the IDAT payload of the 1x1 PNG fixture in half. The
// signature and IHDR chunk stay intact — so image.DecodeConfig succeeds and
// reports dimensions — but the deflate stream ends prematurely, forcing the
// full image.Decode pass to fail. This exercises the decode-stage corruption
// branch that the malformed-header fixture (bad IHDR) never reaches.
func truncatedIDATPNG() []byte {
	b := testPNGBytes
	// Walk the chunk stream past the 8-byte signature to the first IDAT
	// chunk: 4-byte length, 4-byte type, payload, 4-byte CRC.
	for pos := 8; pos+8 <= len(b); {
		length := int(binary.BigEndian.Uint32(b[pos : pos+4]))
		payloadStart := pos + 8
		if string(b[pos+4:pos+8]) == "IDAT" {
			return append([]byte(nil), b[:payloadStart+length/2]...)
		}
		pos = payloadStart + length + 4 // skip payload + CRC
	}
	panic("no IDAT chunk found in png fixture")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newAvatarTestHandler(t *testing.T, repo profileRepo, store avatarStore) *Handler {
	t.Helper()
	return NewHandler(newTestAvatarService(repo, store))
}

func newAvatarEchoContext(method string, body io.Reader, contentType string, userID string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, "/api/user/avatar", body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)
	if userID != "" {
		c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: userID, Sub: "sub-1"})
	}
	return c, rec
}

func multipartAvatarBody(t *testing.T, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile("file", "avatar")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("failed to write form data: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}
	return buf, w.FormDataContentType()
}

func assertHandlerOutcome(t *testing.T, err error, rec *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if err != nil {
		assertAppErrorStatus(t, err, wantStatus)
		return
	}
	if rec.Code != wantStatus {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, wantStatus, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Upload handler tests
// ---------------------------------------------------------------------------

func TestUpload_ValidPNG_ReturnsProfile(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	body, ct := multipartAvatarBody(t, testPNGBytes)
	c, rec := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertHandlerOutcome(t, err, rec, http.StatusOK)

	var dto ProfileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if dto.AvatarObjectKey == nil || *dto.AvatarObjectKey == "" {
		t.Fatalf("expected avatar object key in response, got %v", dto.AvatarObjectKey)
	}
	if dto.AvatarURL == "" {
		t.Errorf("expected avatarUrl in response")
	}
}

func TestUpload_ValidJPEG_ReturnsProfile(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	body, ct := multipartAvatarBody(t, testJPEGBytes)
	c, rec := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertHandlerOutcome(t, err, rec, http.StatusOK)

	var dto ProfileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if dto.AvatarObjectKey == nil {
		t.Errorf("expected avatar object key in response")
	}
}

func TestUpload_ValidWebP_ReturnsProfile(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	body, ct := multipartAvatarBody(t, testWebPBytes)
	c, rec := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertHandlerOutcome(t, err, rec, http.StatusOK)

	if len(store.objects) != 1 {
		t.Errorf("expected one uploaded object, got %d", len(store.objects))
	}
	var dto ProfileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if dto.AvatarObjectKey == nil || *dto.AvatarObjectKey == "" {
		t.Errorf("expected avatar object key in response")
	}
}

func TestUpload_ValidGIF_ReturnsProfile(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	body, ct := multipartAvatarBody(t, testGIFBytes)
	c, rec := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertHandlerOutcome(t, err, rec, http.StatusOK)

	if len(store.objects) != 1 {
		t.Errorf("expected one uploaded object, got %d", len(store.objects))
	}
	var dto ProfileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if dto.AvatarObjectKey == nil || *dto.AvatarObjectKey == "" {
		t.Errorf("expected avatar object key in response")
	}
}

func TestUpload_SVG_Rejected(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	body, ct := multipartAvatarBody(t, testSVGBytes)
	c, rec := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertHandlerOutcome(t, err, rec, http.StatusBadRequest)
	if len(store.objects) != 0 {
		t.Errorf("expected no upload for rejected image type")
	}
}

func TestUpload_NonImage_Rejected(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	body, ct := multipartAvatarBody(t, testTextBytes)
	c, rec := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertHandlerOutcome(t, err, rec, http.StatusBadRequest)
	if len(store.objects) != 0 {
		t.Errorf("expected no upload for rejected content")
	}
}

func TestUpload_OversizedDimensions_Rejected(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	body, ct := multipartAvatarBody(t, oversizeDimensionPNG())
	c, _ := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertAppError(t, err, http.StatusBadRequest, "image dimensions too large")
	if len(store.objects) != 0 {
		t.Errorf("expected no upload for oversized-dimension image")
	}
}

func TestUpload_MalformedPNG_Rejected(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	// PNG signature prefix followed by garbage: http.DetectContentType still
	// sniffs image/png from the signature, but the IHDR chunk cannot be
	// parsed, so the DecodeConfig pass must reject it (header-stage failure).
	malformed := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("not a valid chunk stream")...)
	body, ct := multipartAvatarBody(t, malformed)
	c, _ := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertAppError(t, err, http.StatusBadRequest, "invalid image header")
	if len(store.objects) != 0 {
		t.Errorf("expected no upload for malformed image")
	}
}

func TestUpload_TruncatedIDAT_Rejected(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	// Signature and IHDR are intact (DecodeConfig succeeds) but the IDAT
	// deflate stream is cut short, so only the full image.Decode pass can
	// catch it. This asserts the decode-stage corruption branch returns the
	// "corrupt image data" error rather than the header-stage one.
	body, ct := multipartAvatarBody(t, truncatedIDATPNG())
	c, _ := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertAppError(t, err, http.StatusBadRequest, "corrupt image data")
	if len(store.objects) != 0 {
		t.Errorf("expected no upload for truncated image")
	}
}

func TestUpload_Oversize_Rejected(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	// Pad well past maxAvatarSize so the whole multipart body exceeds the
	// MaxBytesReader cap, not just the file contents.
	oversize := append(testPNGBytes, bytes.Repeat([]byte{0x00}, maxAvatarSize+2048)...)
	body, ct := multipartAvatarBody(t, oversize)
	c, rec := newAvatarEchoContext(http.MethodPut, body, ct, "profile-1")

	err := h.Upload(c)
	assertHandlerOutcome(t, err, rec, http.StatusRequestEntityTooLarge)
	if len(store.objects) != 0 {
		t.Errorf("expected no upload for oversize file")
	}
}

func TestUpload_MissingFile_Rejected(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}
	c, rec := newAvatarEchoContext(http.MethodPut, buf, w.FormDataContentType(), "profile-1")

	err := h.Upload(c)
	assertHandlerOutcome(t, err, rec, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// GetAvatar handler tests
// ---------------------------------------------------------------------------

func TestGetAvatar_StreamsImage(t *testing.T) {
	key := "avatars/abc.png"
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1", AvatarObjectKey: &key})
	store := newFakeAvatarStore(true)
	store.objects[key] = testPNGBytes
	h := newAvatarTestHandler(t, repo, store)

	c, rec := newAvatarEchoContext(http.MethodGet, nil, "", "profile-1")

	err := h.GetAvatar(c)
	assertHandlerOutcome(t, err, rec, http.StatusOK)

	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
	if !bytes.Equal(rec.Body.Bytes(), testPNGBytes) {
		t.Errorf("body = %d bytes, want %d bytes", rec.Body.Len(), len(testPNGBytes))
	}
}

func TestGetAvatar_NoAvatar_Returns404(t *testing.T) {
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1"})
	store := newFakeAvatarStore(true)
	h := newAvatarTestHandler(t, repo, store)

	c, rec := newAvatarEchoContext(http.MethodGet, nil, "", "profile-1")

	err := h.GetAvatar(c)
	assertHandlerOutcome(t, err, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// DeleteAvatar handler tests
// ---------------------------------------------------------------------------

func TestDeleteAvatar_ClearsAvatar(t *testing.T) {
	key := "avatars/abc.png"
	repo := newFakeProfileRepo()
	repo.seed(Profile{ID: "profile-1", ZitadelUserID: "zitadel-1", AvatarObjectKey: &key})
	store := newFakeAvatarStore(true)
	store.objects[key] = testPNGBytes
	h := newAvatarTestHandler(t, repo, store)

	c, rec := newAvatarEchoContext(http.MethodDelete, nil, "", "profile-1")

	err := h.DeleteAvatar(c)
	assertHandlerOutcome(t, err, rec, http.StatusOK)

	if _, ok := store.objects[key]; ok {
		t.Errorf("avatar object %q was not deleted", key)
	}
	var dto ProfileDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if dto.AvatarObjectKey != nil || dto.AvatarURL != "" {
		t.Errorf("expected cleared avatar in response, got key=%v url=%q", dto.AvatarObjectKey, dto.AvatarURL)
	}
	if strings.Contains(rec.Body.String(), "avatarUrl") {
		t.Errorf("expected avatarUrl omitted from response, body: %s", rec.Body.String())
	}
}
