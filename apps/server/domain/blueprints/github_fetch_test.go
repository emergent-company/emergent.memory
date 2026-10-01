package blueprints

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/google/uuid"
)

// ──────────────────────────────────────────────
// URL guard
// ──────────────────────────────────────────────

func TestParseGitHubURL_Guard(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name  string
		in    string
		refOv string
		org   string
		repo  string
		ref   string
	}{
		{"bare", "https://github.com/org/repo", "", "org", "repo", "HEAD"},
		{"git suffix", "https://github.com/org/repo.git", "", "org", "repo", "HEAD"},
		{"fragment ref", "https://github.com/org/repo#v1.2.3", "", "org", "repo", "v1.2.3"},
		{"tree ref", "https://github.com/org/repo/tree/main", "", "org", "repo", "main"},
		{"tree nested ref", "https://github.com/org/repo/tree/feature/x", "", "org", "repo", "feature/x"},
		{"explicit override wins", "https://github.com/org/repo#dev", "v9", "org", "repo", "v9"},
		{"trailing slash", "https://github.com/org/repo/", "", "org", "repo", "HEAD"},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			org, repo, ref, err := parseGitHubURL(tc.in, tc.refOv)
			require.NoError(t, err)
			assert.Equal(t, tc.org, org)
			assert.Equal(t, tc.repo, repo)
			assert.Equal(t, tc.ref, ref)
		})
	}

	invalid := []struct {
		name string
		in   string
	}{
		{"http scheme", "http://github.com/org/repo"},
		{"git scheme", "git://github.com/org/repo"},
		{"non-github host", "https://gitlab.com/org/repo"},
		{"lookalike host", "https://github.com.evil.com/org/repo"},
		{"ip literal", "https://127.0.0.1/org/repo"},
		{"ip literal v6", "https://[::1]/org/repo"},
		{"localhost", "https://localhost/org/repo"},
		{"port", "https://github.com:8443/org/repo"},
		{"credentials", "https://user:pass@github.com/org/repo"},
		{"no repo", "https://github.com/org"},
		{"no path", "https://github.com"},
		{"unexpected path", "https://github.com/org/repo/issues/1"},
		{"empty", ""},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, _, err := parseGitHubURL(tc.in, "")
			require.Error(t, err)
			var apErr *apperror.Error
			require.True(t, errors.As(err, &apErr), "want *apperror.Error, got %T", err)
			assert.Equal(t, http.StatusBadRequest, apErr.HTTPStatus)
		})
	}
}

// ──────────────────────────────────────────────
// Fetch guard: redirects, size cap, unsafe archives
// ──────────────────────────────────────────────

type tarEntry struct {
	name string
	body string
	typ  byte
	link string
}

func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: typ, Linkname: e.link}
		if typ == tar.TypeReg || typ == tar.TypeRegA {
			hdr.Size = int64(len(e.body))
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if typ == tar.TypeReg || typ == tar.TypeRegA {
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func servingTarball(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// testGitHubFetcher binds a fetcher to an httptest origin with the production
// redirect policy and caller-supplied limits.
func testGitHubFetcher(t *testing.T, baseURL string, maxArchive, maxExtract int64, maxFiles int) *GitHubFetcher {
	t.Helper()
	u, err := url.Parse(baseURL)
	require.NoError(t, err)
	f := &GitHubFetcher{
		codeloadBaseURL: baseURL,
		codeloadHost:    u.Host,
		maxArchiveBytes: maxArchive,
		maxExtractBytes: maxExtract,
		maxFiles:        maxFiles,
	}
	f.client = f.newClient()
	return f
}

func requireAppError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var apErr *apperror.Error
	require.True(t, errors.As(err, &apErr), "want *apperror.Error, got %T: %v", err, err)
	assert.Equal(t, status, apErr.HTTPStatus, "status = %d, want %d: %v", apErr.HTTPStatus, status, err)
}

func TestFetch_RejectsRedirectToNonAllowlistedHost(t *testing.T) {
	t.Parallel()

	evil := servingTarball(t, buildTarGz(t, []tarEntry{{name: "repo-HEAD/packs/p.yaml", body: "name: x\n"}}))
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/org/repo/tar.gz/HEAD", http.StatusFound)
	}))
	t.Cleanup(allowed.Close)

	f := testGitHubFetcher(t, allowed.URL, 1<<20, 1<<20, 100)
	_, _, err := f.Fetch(context.Background(), "org", "repo", "HEAD", "")
	requireAppError(t, err, http.StatusBadGateway)
}

// TestClient_RedirectPolicy exercises the redirect allowlist directly: every
// hop must be https AND on the exact allowlisted codeload host.
func TestClient_RedirectPolicy(t *testing.T) {
	t.Parallel()

	f := newGitHubFetcher()

	mkReq := func(raw string) *http.Request {
		return httptest.NewRequest(http.MethodGet, raw, nil)
	}

	cases := []struct {
		name    string
		rawURL  string
		via     int
		wantErr bool
	}{
		{"same https codeload host allowed", "https://" + codeloadHost + "/org/repo/tar.gz/HEAD", 1, false},
		{"different host refused", "https://evil.example.com/org/repo/tar.gz/HEAD", 1, true},
		{"same host over http refused", "http://" + codeloadHost + "/org/repo/tar.gz/HEAD", 1, true},
		{"too many redirects refused", "https://" + codeloadHost + "/org/repo/tar.gz/HEAD", 10, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			via := make([]*http.Request, tc.via)
			err := f.client.CheckRedirect(mkReq(tc.rawURL), via)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFetch_SizeCapExceeded(t *testing.T) {
	t.Parallel()

	ts := servingTarball(t, bytes.Repeat([]byte("x"), 4096))
	f := testGitHubFetcher(t, ts.URL, 512, 1<<20, 100)
	_, _, err := f.Fetch(context.Background(), "org", "repo", "HEAD", "")
	requireAppError(t, err, http.StatusRequestEntityTooLarge)
}

func TestFetch_RejectsUnsafeArchiveEntries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry tarEntry
	}{
		{"path traversal", tarEntry{name: "../evil.txt", body: "pwned"}},
		{"nested traversal", tarEntry{name: "repo-HEAD/../../evil.txt", body: "pwned"}},
		{"absolute path", tarEntry{name: "/tmp/evil.txt", body: "pwned"}},
		{"escaping symlink", tarEntry{name: "repo-HEAD/link", typ: tar.TypeSymlink, link: "/etc/passwd"}},
		{"escaping hardlink", tarEntry{name: "repo-HEAD/link", typ: tar.TypeLink, link: "../../etc/passwd"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := servingTarball(t, buildTarGz(t, []tarEntry{tc.entry}))
			f := testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)
			_, _, err := f.Fetch(context.Background(), "org", "repo", "HEAD", "")
			requireAppError(t, err, http.StatusBadRequest)
		})
	}
}

func TestFetch_HappyPath(t *testing.T) {
	t.Parallel()

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/", typ: tar.TypeDir},
		{name: "repo-HEAD/packs/pack.yaml", body: "name: imported-pack\nversion: 1.2.3\ndescription: Imported\nauthor: tester\nobjectTypes:\n  - name: Thing\n    label: Thing\n"},
	})
	ts := servingTarball(t, archive)
	f := testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	root, cleanup, err := f.Fetch(context.Background(), "org", "repo", "HEAD", "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	desc, err := loadBlueprintDir(root, "repo")
	require.NoError(t, err)
	assert.Equal(t, "imported-pack", desc.Name)
	assert.Equal(t, "1.2.3", desc.Version)
	require.Len(t, desc.Packs, 1)
}

// TestFetch_TokenSentNeverLeaked verifies the token is sent as an outbound
// Authorization header but never appears in a fetch error.
func TestFetch_TokenSentNeverLeaked(t *testing.T) {
	t.Parallel()

	const token = "ghp_SUPER_SECRET_TOKEN"
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	f := testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)
	_, _, err := f.Fetch(context.Background(), "org", "repo", "HEAD", token)
	requireAppError(t, err, http.StatusBadGateway)
	assert.Equal(t, "token "+token, gotAuth, "token must be sent as an Authorization header")
	assert.NotContains(t, err.Error(), token, "token must never appear in an error")
}

// ──────────────────────────────────────────────
// Manifest loading
// ──────────────────────────────────────────────

func TestLoadBlueprintDir_ManifestFileWins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "packs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "packs", "p.yaml"),
		[]byte("name: from-dir\nversion: 0.0.1\nobjectTypes:\n  - name: A\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blueprint.yaml"),
		[]byte("name: from-file\nversion: 9.9.9\npacks:\n  - name: from-file\n    version: 9.9.9\n    objectTypes:\n      - name: A\n"), 0o644))

	desc, err := loadBlueprintDir(dir, "fallback")
	require.NoError(t, err)
	assert.Equal(t, "from-file", desc.Name)
	assert.Equal(t, "9.9.9", desc.Version)
}

func TestLoadBlueprintLayout_AllKinds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "schemas"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schemas", "crm.yaml"),
		[]byte("name: crm\nversion: 2.0.0\ndescription: CRM\nauthor: me\nobjectTypes:\n  - name: Contact\n    label: Contact\n"), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agents", "helper.json"),
		[]byte(`{"name":"helper","description":"helps","systemPrompt":"hi"}`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", "greeter"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skills", "greeter", "SKILL.md"),
		[]byte("---\nname: greeter\ndescription: says hi\n---\n\nHello.\n"), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "seed", "objects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "seed", "objects", "Contact.jsonl"),
		[]byte(`{"type":"Contact","key":"ada","properties":{"name":"Ada"}}`+"\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "seed", "relationships"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "seed", "relationships", "knows.jsonl"),
		[]byte(`{"type":"knows","srcKey":"ada","dstKey":"grace"}`+"\n"), 0o644))

	desc, err := loadBlueprintDir(dir, "fallback")
	require.NoError(t, err)
	assert.Equal(t, "crm", desc.Name)
	assert.Equal(t, "2.0.0", desc.Version)
	require.Len(t, desc.Packs, 1)
	require.Len(t, desc.Agents, 1)
	require.Len(t, desc.Skills, 1)
	require.NotNil(t, desc.Seed)
	assert.Len(t, desc.Seed.Objects, 1)
	assert.Len(t, desc.Seed.Relationships, 1)
}

func TestLoadBlueprintLayout_EmptyRejected(t *testing.T) {
	t.Parallel()
	desc, err := loadBlueprintDir(t.TempDir(), "fallback")
	require.Nil(t, desc)
	requireAppError(t, err, http.StatusBadRequest)
}

// ──────────────────────────────────────────────
// Service import (through the existing create/publish path)
// ──────────────────────────────────────────────

func TestService_ImportFromGitHub_CreatesPublishedBlueprint(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: "name: " + uniqueName("imported") + "\nversion: 3.1.4\ndescription: Imported pack\nauthor: importer\nobjectTypes:\n  - name: Thing\n    label: Thing\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	bp, err := svc.ImportFromGitHub(ctx, "", &ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)
	assert.NotEmpty(t, bp.ID)
	assert.Equal(t, StatusPublished, bp.Status)
	assert.Equal(t, "3.1.4", bp.Version)

	var m BlueprintManifest
	require.NoError(t, json.Unmarshal(bp.Manifest, &m))
	require.Len(t, m.Packs, 1)

	// The published row is readable through the normal service read path.
	got, err := svc.GetBlueprint(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)
}

func TestService_ImportFromGitHub_InvalidURL(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)

	_, err := svc.ImportFromGitHub(context.Background(), "", &ImportGitHubRequest{URL: "https://evil.example/org/repo"})
	requireAppError(t, err, http.StatusBadRequest)
}

// TestService_ImportFromGitHub_TokenNotPersisted verifies the private-repo
// token is used for the fetch but never lands in the stored blueprint.
func TestService_ImportFromGitHub_TokenNotPersisted(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	const token = "ghp_NEVER_STORE_ME"
	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: "name: " + uniqueName("tok") + "\nversion: 1.0.0\nobjectTypes:\n  - name: Thing\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	bp, err := svc.ImportFromGitHub(ctx, "", &ImportGitHubRequest{URL: "https://github.com/org/repo", Token: token})
	require.NoError(t, err)

	raw, err := json.Marshal(bp)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), token)

	stored, err := svc.repo.GetByID(ctx, "", bp.ID)
	require.NoError(t, err)
	assert.NotContains(t, string(stored.Manifest), token)
	assert.NotContains(t, stored.Description, token)
}

// ──────────────────────────────────────────────
// Handler authorization
// ──────────────────────────────────────────────

// TestHandler_ImportBlueprint_GlobalRequiresSuperadmin is the 403 case: a plain
// member with no project context importing to the global catalogue is denied
// before any network fetch happens.
func TestHandler_ImportBlueprint_GlobalRequiresSuperadmin(t *testing.T) {
	h, _ := newAuthzHandler(t)

	body, err := json.Marshal(ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)

	c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints/import", body, &auth.AuthUser{ID: uuid.NewString()})
	assertForbidden(t, h.ImportBlueprint(c))
}

// TestRoute_ImportBlueprint_Unauthenticated401 exercises the real route
// registration: a request with no credentials is rejected by RequireAuth.
func TestRoute_ImportBlueprint_Unauthenticated401(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "blueprints_import_auth")
	t.Cleanup(tdb.Close)

	log := testLogger()
	userSvc := auth.NewUserProfileService(tdb.DB, log)
	mw := auth.NewMiddleware(auth.MiddlewareParams{DB: tdb.DB, Cfg: tdb.Config, Log: log, UserSvc: userSvc})

	repo := NewRepository(tdb.DB, log)
	svc := NewService(ServiceParams{Repo: repo, Log: log})
	h := NewHandler(svc)

	e := echo.New()
	e.HideBanner = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	RegisterRoutes(e, h, mw)

	req := httptest.NewRequest(http.MethodPost, "/api/blueprints/import",
		strings.NewReader(`{"url":"https://github.com/org/repo"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
}
