package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// --- resolveAvatar ---

func TestResolveAvatar(t *testing.T) {
	for _, c := range []struct {
		name     string
		override string
		picture  string
		want     string
	}{
		{"override wins when both set", "/api/user/avatar?v=k1", "https://idp/x.png", "/api/user/avatar?v=k1"},
		{"override wins over empty picture", "/api/user/avatar?v=k1", "", "/api/user/avatar?v=k1"},
		{"picture used when no override", "", "https://idp/x.png", "https://idp/x.png"},
		{"neither set", "", "", ""},
	} {
		if got := resolveAvatar(c.override, c.picture); got != c.want {
			t.Errorf("resolveAvatar(%q, %q) = %q, want %q", c.override, c.picture, got, c.want)
		}
	}
}

// --- session claims / account / context mapping ---

// TestSessionAvatarOverrideCarriedThrough asserts the avatar override survives
// the claims ↔ registry mapping and the per-request context attachment.
func TestSessionAvatarOverrideCarriedThrough(t *testing.T) {
	in := sessionClaims{
		AccessToken:       "at",
		RefreshToken:      "rt",
		Sub:               "sub-a",
		Name:              "Ada",
		Picture:           "https://idp/ada.png",
		AvatarOverrideURL: "/api/user/avatar?v=k1",
		ActiveProjectID:   "proj-1",
		OrgID:             "org-1",
		ExpiresAt:         time.Now().Add(time.Hour).Unix(),
	}

	// registry round trip (switch flow)
	back := accountToSession(sessionToAccount(in))
	if back.AvatarOverrideURL != in.AvatarOverrideURL || back.Picture != in.Picture {
		t.Errorf("claims→account→claims lost override/picture: %+v", back)
	}

	// attachSession threads the override into the request context
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	s.attachSession(c, &in)
	sc, ok := sessionContextFrom(c.Request().Context())
	if !ok {
		t.Fatal("no session context attached")
	}
	if sc.AvatarOverrideURL != in.AvatarOverrideURL {
		t.Errorf("sessionContext.AvatarOverrideURL = %q, want %q", sc.AvatarOverrideURL, in.AvatarOverrideURL)
	}
}

// --- profile page templ ---

func TestProfileAvatarRender(t *testing.T) {
	withURL := renderHTML(t, profileAvatar(&UserProfileDto{DisplayName: "Ada Lovelace", AvatarUrl: "/api/user/avatar?v=k1"}))
	if !strings.Contains(withURL, `<img src="/api/user/avatar?v=k1"`) {
		t.Errorf("profileAvatar with AvatarUrl must render the photo <img>, got %q", withURL)
	}
	if strings.Contains(withURL, ">AL<") {
		t.Error("initials must not render when a photo is shown")
	}

	without := renderHTML(t, profileAvatar(&UserProfileDto{DisplayName: "Ada Lovelace"}))
	if strings.Contains(without, "<img") || !strings.Contains(without, ">AL<") {
		t.Errorf("profileAvatar without AvatarUrl must render initials, got %q", without)
	}
}

// TestProfileAvatarModalRender asserts the profile-photo dialog renders the
// multipart upload widget always, and the remove form only when an override
// (AvatarUrl) exists.
func TestProfileAvatarModalRender(t *testing.T) {
	withPhoto := renderHTML(t, profileAvatarModal(&UserProfileDto{AvatarUrl: "/api/user/avatar?v=k1"}))
	for _, want := range []string{
		`<dialog id="profile-avatar-modal" class="modal"`,
		`action="/profile/avatar"`,
		`enctype="multipart/form-data"`,
		`name="file"`,
		`type="file"`,
		"Upload photo",
		`action="/profile/avatar/remove"`,
		"Remove photo",
	} {
		if !strings.Contains(withPhoto, want) {
			t.Errorf("profile photo modal missing %q", want)
		}
	}

	noPhoto := renderHTML(t, profileAvatarModal(&UserProfileDto{}))
	if !strings.Contains(noPhoto, `action="/profile/avatar"`) {
		t.Error("upload form must always render")
	}
	if strings.Contains(noPhoto, "Remove photo") || strings.Contains(noPhoto, `action="/profile/avatar/remove"`) {
		t.Error("remove form must not render without an existing photo")
	}
}

// TestProfileAvatarTriggerWiring asserts the profile page pairs the clickable
// avatar trigger (with its hover-edit affordance) with the profile-photo
// dialog, so clicking the avatar opens it via showModal. It also guards that
// the photo upload/remove widgets no longer render as a standalone page
// section — the avatar is the only entry point into them.
func TestProfileAvatarTriggerWiring(t *testing.T) {
	html := renderHTML(t, ProfilePage(profilePageData{Profile: &UserProfileDto{
		DisplayName: "Ada Lovelace",
		AvatarUrl:   "/api/user/avatar?v=k1",
	}}))
	for _, want := range []string{
		`<dialog id="profile-avatar-modal" class="modal"`,
		`aria-label="Edit profile photo"`,
		`document.getElementById('profile-avatar-modal').showModal()`,
		`<img src="/api/user/avatar?v=k1"`,
		`lucide--pencil`,
		`action="/profile/avatar"`,
		`action="/profile/avatar/remove"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("profile page missing %q", want)
		}
	}
	// the upload widgets must live inside the dialog, not as an inline section
	if i := strings.Index(html, `action="/profile/avatar"`); i < 0 || !strings.Contains(html[:i], `<dialog id="profile-avatar-modal"`) {
		t.Error("upload form must render inside the profile-avatar-modal dialog")
	}
}

// --- avatar upload/remove PRG handlers (session mode) ---

// avatarUIEcho wires the avatar PRG routes + proxy onto a bare echo, session
// mode, mirroring main.go.
func avatarUIEcho(s *Server) *echo.Echo {
	e := echo.New()
	e.POST("/profile/avatar", s.uiUploadAvatar)
	e.POST("/profile/avatar/remove", s.uiRemoveAvatar)
	e.GET("/api/user/avatar", s.avatarProxy)
	return e
}

// avatarSessionClaims returns valid session claims for the avatar flows.
func avatarSessionClaims() sessionClaims {
	return sessionClaims{
		AccessToken:     "at-session",
		RefreshToken:    "rt-session",
		ActiveProjectID: "proj-1",
		Name:            "Ada Lovelace",
		Email:           "ada@x.io",
		Sub:             "sub-a",
		ExpiresAt:       time.Now().Add(time.Hour).Unix(),
	}
}

// multipartUploadBody builds a multipart body with one file part named "file".
func multipartUploadBody(t *testing.T, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestUIUploadAvatar(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := avatarUIEcho(s)

	body, ctype := multipartUploadBody(t, "face.png", "fake-image-bytes")
	req := httptest.NewRequest(http.MethodPost, "/profile/avatar", body)
	req.Header.Set("Content-Type", ctype)
	addSessionCookie(t, req, "test-secret", avatarSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /profile/avatar = %d, want 303 (%s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/profile?avatar=1" {
		t.Errorf("Location = %q, want /profile?avatar=1", loc)
	}
	if len(f.avatarUploads) != 1 || f.avatarUploads[0] != "face.png" {
		t.Errorf("UploadAvatar filenames = %v, want [face.png]", f.avatarUploads)
	}

	// the session cookie is re-issued with the override recorded
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.AvatarOverrideURL != "/api/user/avatar?v=key-new" {
		t.Errorf("AvatarOverrideURL = %q, want the uploaded avatar URL", claims.AvatarOverrideURL)
	}
	if claims.Sub != "sub-a" {
		t.Errorf("Sub = %q, want preserved sub-a", claims.Sub)
	}
}

func TestUIUploadAvatarMissingFile(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: &fakeMemory{}}
	e := avatarUIEcho(s)
	req := httptest.NewRequest(http.MethodPost, "/profile/avatar", nil)
	addSessionCookie(t, req, "test-secret", avatarSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/profile?err=upload") {
		t.Errorf("fileless upload = %d %q, want profile error redirect", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIUploadAvatarDevModeRedirectsHome(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
	e := avatarUIEcho(s)
	body, ctype := multipartUploadBody(t, "face.png", "bytes")
	req := httptest.NewRequest(http.MethodPost, "/profile/avatar", body)
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Errorf("dev upload = %d %q, want 302 /", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIUploadAvatarOversize(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: &fakeMemory{}}
	e := avatarUIEcho(s)
	// A file larger than avatarMaxUploadSize is rejected before reaching memory.
	big := strings.Repeat("a", int(avatarMaxUploadSize)+1)
	body, ctype := multipartUploadBody(t, "big.png", big)
	req := httptest.NewRequest(http.MethodPost, "/profile/avatar", body)
	req.Header.Set("Content-Type", ctype)
	addSessionCookie(t, req, "test-secret", avatarSessionClaims())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "too+large") {
		t.Errorf("oversize upload = %d %q, want 'too large' error redirect", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIRemoveAvatar(t *testing.T) {
	f := &fakeMemory{
		avatarProfile: &UserProfileDto{ID: "u1", AvatarUrl: "/api/user/avatar?v=key-new"},
		profile:       &UserProfileDto{ID: "u1", AvatarUrl: "/api/user/avatar?v=key-new"},
	}
	s := &Server{cfg: Config{AuthMode: "session", SessionSecret: "test-secret"}, memory: f}
	e := avatarUIEcho(s)

	req := httptest.NewRequest(http.MethodPost, "/profile/avatar/remove", nil)
	claims := avatarSessionClaims()
	claims.AvatarOverrideURL = "/api/user/avatar?v=key-new"
	addSessionCookie(t, req, "test-secret", claims)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /profile/avatar/remove = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/profile?avatar=removed" {
		t.Errorf("Location = %q, want /profile?avatar=removed", loc)
	}
	if f.avatarProfile == nil || f.avatarProfile.AvatarUrl != "" {
		t.Errorf("DeleteAvatar must clear the stored avatar URL, got %+v", f.avatarProfile)
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not re-issued")
	}
	got, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.AvatarOverrideURL != "" {
		t.Errorf("AvatarOverrideURL = %q, want cleared after remove", got.AvatarOverrideURL)
	}
}

func TestUIRemoveAvatarDevModeRedirectsHome(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
	e := avatarUIEcho(s)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/profile/avatar/remove", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Errorf("dev remove = %d %q, want 302 /", rec.Code, rec.Header().Get("Location"))
	}
}

// --- avatar proxy ---

func TestAvatarProxyStreamsImage(t *testing.T) {
	f := &fakeMemory{avatarBody: []byte("PNG-BYTES"), avatarContentType: "image/png"}
	s := &Server{cfg: Config{}, memory: f}
	e := avatarUIEcho(s)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/user/avatar?v=k1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/user/avatar = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if rec.Body.String() != "PNG-BYTES" {
		t.Errorf("body = %q, want the raw image bytes", rec.Body.String())
	}
}

func TestAvatarProxyNoAvatar404(t *testing.T) {
	// no avatarBody → the fake answers 404 like memory
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := avatarUIEcho(s)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/user/avatar", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/user/avatar with no photo = %d, want 404", rec.Code)
	}
}

// --- authCallback override resolution ---

// TestAuthCallbackResolvesAvatarOverride asserts the callback fetches the
// Memory profile at login and records its AvatarUrl as the session's avatar
// override (falling back to the profile display name when the ID token omits
// name claims).
func TestAuthCallbackResolvesAvatarOverride(t *testing.T) {
	srv := newSignedZitadel(t, func(srvURL string, idp *testIDP) map[string]any {
		return map[string]any{
			"access_token":  "at-oidc",
			"refresh_token": "rt-oidc",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"id_token":      idp.sign(t, testClaims(srvURL, "nonce-1", map[string]any{"sub": "389329982813372426"})),
		}
	})

	s := &Server{
		cfg: oidcCfg(srv.URL),
		memory: &fakeMemory{profile: &UserProfileDto{
			DisplayName: "Maciej Kucharz",
			AvatarUrl:   "/api/user/avatar?v=key-1",
		}},
	}
	e := sessionServer(s)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=st-valid&code=the-code", nil)
	addOAuthCookie(t, req, "test-secret", "st-valid", "v3r1f13r", "nonce-1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (%s)", rec.Code, rec.Body.String())
	}
	ck := findCookie(rec, sessionCookieName)
	if ck == nil {
		t.Fatal("session cookie not set")
	}
	claims, err := verifySession("test-secret", ck.Value, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims.AvatarOverrideURL != "/api/user/avatar?v=key-1" {
		t.Errorf("AvatarOverrideURL = %q, want the profile AvatarUrl", claims.AvatarOverrideURL)
	}
	if claims.Name != "Maciej Kucharz" {
		t.Errorf("Name = %q, want the profile display-name fallback", claims.Name)
	}
	if claims.Picture != "" {
		t.Errorf("Picture = %q, want empty (no IdP picture, no override merge)", claims.Picture)
	}
}

// --- fetchUserInfo ---

func TestFetchUserInfo(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Ada Lovelace","email":"ada@x.io","picture":"https://idp/ada.png"}`))
	}))
	defer srv.Close()

	name, email, picture := fetchUserInfo(t.Context(), srv.URL, "at-1")
	if authHeader != "Bearer at-1" {
		t.Errorf("Authorization = %q, want Bearer at-1", authHeader)
	}
	if name != "Ada Lovelace" || email != "ada@x.io" || picture != "https://idp/ada.png" {
		t.Errorf("fetchUserInfo = (%q, %q, %q), want the userinfo claims", name, email, picture)
	}
}

func TestFetchUserInfoTolerant(t *testing.T) {
	if n, e, p := fetchUserInfo(t.Context(), "", "at"); n != "" || e != "" || p != "" {
		t.Errorf("empty endpoint must return empty claims, got (%q, %q, %q)", n, e, p)
	}
	if n, e, p := fetchUserInfo(t.Context(), "http://127.0.0.1:1/userinfo", ""); n != "" || e != "" || p != "" {
		t.Errorf("empty token must return empty claims, got (%q, %q, %q)", n, e, p)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if n, e, p := fetchUserInfo(t.Context(), srv.URL, "at"); n != "" || e != "" || p != "" {
		t.Errorf("non-2xx userinfo must return empty claims, got (%q, %q, %q)", n, e, p)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer bad.Close()
	if n, e, p := fetchUserInfo(t.Context(), bad.URL, "at"); n != "" || e != "" || p != "" {
		t.Errorf("malformed userinfo must return empty claims, got (%q, %q, %q)", n, e, p)
	}
}
