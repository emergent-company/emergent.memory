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

// TestFetch_FileCountCap verifies the extracted-entry-count cap (20000 in
// production) is enforced at the boundary: exactly maxFiles regular files are
// extracted, maxFiles+1 is rejected as too large (413).
func TestFetch_FileCountCap(t *testing.T) {
	t.Parallel()

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: "name: cap\nversion: 1.0.0\nobjectTypes:\n  - name: Thing\n"},
		{name: "repo-HEAD/a.txt", body: "a"},
		{name: "repo-HEAD/b.txt", body: "b"},
	})

	t.Run("at cap accepted", func(t *testing.T) {
		t.Parallel()
		ts := servingTarball(t, archive)
		f := testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 3)
		root, cleanup, err := f.Fetch(context.Background(), "org", "repo", "HEAD", "")
		require.NoError(t, err)
		t.Cleanup(cleanup)
		require.NotEmpty(t, root)
	})

	t.Run("over cap rejected", func(t *testing.T) {
		t.Parallel()
		ts := servingTarball(t, archive)
		f := testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 2)
		_, _, err := f.Fetch(context.Background(), "org", "repo", "HEAD", "")
		requireAppError(t, err, http.StatusRequestEntityTooLarge)
	})
}

// TestFetch_ExtractedBytesCap verifies the total extracted-bytes cap (200 MiB
// in production) is enforced at the boundary: exactly maxExtractBytes of regular
// file content is extracted, one byte more is rejected as too large (413).
func TestFetch_ExtractedBytesCap(t *testing.T) {
	t.Parallel()

	// Two 4-byte files = 8 extracted content bytes; directory headers carry no
	// content and must not count toward the byte cap.
	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/", typ: tar.TypeDir},
		{name: "repo-HEAD/a.txt", body: "aaaa"},
		{name: "repo-HEAD/b.txt", body: "bbbb"},
	})

	t.Run("at cap accepted", func(t *testing.T) {
		t.Parallel()
		ts := servingTarball(t, archive)
		f := testGitHubFetcher(t, ts.URL, 1<<20, 8, 100)
		root, cleanup, err := f.Fetch(context.Background(), "org", "repo", "HEAD", "")
		require.NoError(t, err)
		t.Cleanup(cleanup)
		require.NotEmpty(t, root)
	})

	t.Run("over cap rejected", func(t *testing.T) {
		t.Parallel()
		ts := servingTarball(t, archive)
		f := testGitHubFetcher(t, ts.URL, 1<<20, 7, 100)
		_, _, err := f.Fetch(context.Background(), "org", "repo", "HEAD", "")
		requireAppError(t, err, http.StatusRequestEntityTooLarge)
	})
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

// TestService_ImportFromGitHub_PersistsSourceProvenance verifies a GitHub import
// records its source provenance on the created blueprint: kind "github", the
// normalized https://github.com/<org>/<repo> URL (no ref, no .git), and the ref
// with the "HEAD" sentinel normalized to "" (repo default branch). It also
// asserts the canonical manifest checksum is non-empty.
func TestService_ImportFromGitHub_PersistsSourceProvenance(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	cases := []struct {
		name        string
		url         string
		refOverride string
		wantRef     string
	}{
		{"default HEAD normalizes to empty", "https://github.com/acme/widgets", "", ""},
		{"git suffix normalizes URL", "https://github.com/acme/widgets.git", "", ""},
		{"explicit ref override", "https://github.com/acme/widgets", "v1.2.3", "v1.2.3"},
		{"tree ref", "https://github.com/acme/widgets/tree/main", "", "main"},
		{"fragment ref", "https://github.com/acme/widgets#v2.0.0", "", "v2.0.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archive := buildTarGz(t, []tarEntry{
				{name: "repo-HEAD/packs/pack.yaml", body: "name: " + uniqueName("prov") + "\nversion: 1.0.0\nobjectTypes:\n  - name: Thing\n"},
			})
			ts := servingTarball(t, archive)
			svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

			bp, err := svc.ImportFromGitHub(ctx, "", &ImportGitHubRequest{URL: tc.url, Ref: tc.refOverride})
			require.NoError(t, err)

			assert.Equal(t, "github", bp.SourceKind)
			assert.Equal(t, "https://github.com/acme/widgets", bp.SourceURL)
			assert.Equal(t, tc.wantRef, bp.SourceRef)
			assert.NotEmpty(t, bp.Checksum, "imported blueprint must carry a canonical manifest checksum")

			// Provenance is persisted on the row, not just the returned struct.
			stored, err := svc.repo.GetByID(ctx, "", bp.ID)
			require.NoError(t, err)
			assert.Equal(t, "github", stored.SourceKind)
			assert.Equal(t, "https://github.com/acme/widgets", stored.SourceURL)
			assert.Equal(t, tc.wantRef, stored.SourceRef)
			assert.NotEmpty(t, stored.Checksum)
		})
	}
}

// TestService_ImportFromGitHub_ProjectScoped is the positive project-scope path
// (spec: "valid manifest creates a published blueprint in the caller's
// project"): a valid archive imported for a project produces a published
// blueprint owned by that project, visible to it and invisible to any other.
func TestService_ImportFromGitHub_ProjectScoped(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	proj := uuid.NewString()
	other := uuid.NewString()

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: "name: " + uniqueName("proj-import") + "\nversion: 2.0.0\ndescription: Project import\nauthor: importer\nobjectTypes:\n  - name: Thing\n    label: Thing\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	bp, err := svc.ImportFromGitHub(ctx, proj, &ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)
	require.NotNil(t, bp.ProjectID, "import must be scoped to the caller's project")
	assert.Equal(t, proj, *bp.ProjectID)
	assert.Equal(t, "project", bp.Scope())
	assert.Equal(t, StatusPublished, bp.Status)

	// Visible to the owning project, invisible to any other.
	got, err := svc.GetBlueprint(ctx, proj, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, got.ID)
	_, err = svc.GetBlueprint(ctx, other, bp.ID)
	assertAppErrorCode(t, err, apperror.ErrNotFound.Code)

	// The persisted row carries the project scope, not just the returned struct.
	stored, err := svc.repo.GetByID(ctx, proj, bp.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.ProjectID, "persisted row must carry project_id")
	assert.Equal(t, proj, *stored.ProjectID)
	assert.Equal(t, StatusPublished, stored.Status)
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

// TestService_ImportFromGitHub_ModelOverrides_RewritesManifest verifies a
// modelOverrides entry rewrites the named agent's model in the persisted
// manifest before the blueprint is created/published, while a non-matching key
// leaves other agents untouched.
func TestService_ImportFromGitHub_ModelOverrides_RewritesManifest(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/agents/imp.yaml", body: "name: imp\ndescription: d\nmodel:\n  name: gemini-2.5-flash\n"},
		{name: "repo-HEAD/agents/other.yaml", body: "name: other\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	bp, err := svc.ImportFromGitHub(ctx, "", &ImportGitHubRequest{
		URL:            "https://github.com/org/repo",
		ModelOverrides: map[string]string{"imp": "openai/gpt-4o"},
	})
	require.NoError(t, err)
	assert.Equal(t, StatusPublished, bp.Status)

	var m BlueprintManifest
	require.NoError(t, json.Unmarshal(bp.Manifest, &m))
	require.Len(t, m.Agents, 2)
	byName := map[string]AgentManifest{}
	for _, a := range m.Agents {
		byName[a.Name] = a
	}
	require.NotNil(t, byName["imp"].Model, "overridden agent must carry a model")
	assert.Equal(t, "openai/gpt-4o", byName["imp"].Model.Name)
	assert.Nil(t, byName["other"].Model, "non-matching agent must be untouched")
}

// TestService_ImportFromGitHub_ModelOverrides_EmptyClearsName verifies an empty
// string override clears the model name (project default) rather than leaving a
// bare name that would fail apply.
func TestService_ImportFromGitHub_ModelOverrides_EmptyClearsName(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/agents/imp2.yaml", body: "name: imp2\nmodel:\n  name: gemini-2.5-flash\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	bp, err := svc.ImportFromGitHub(ctx, "", &ImportGitHubRequest{
		URL:            "https://github.com/org/repo",
		ModelOverrides: map[string]string{"imp2": ""},
	})
	require.NoError(t, err)

	var m BlueprintManifest
	require.NoError(t, json.Unmarshal(bp.Manifest, &m))
	require.Len(t, m.Agents, 1)
	require.NotNil(t, m.Agents[0].Model, "empty override still sets a model struct")
	assert.Empty(t, m.Agents[0].Model.Name)
}

// TestService_ImportFromGitHub_AtomicRollback verifies that when Apply=true and
// apply fails, the just-published row is hard-deleted so a retry re-imports
// cleanly (no 409) instead of colliding on the (name, version).
func TestService_ImportFromGitHub_AtomicRollback(t *testing.T) {
	db := connectTestDB(t)
	svc := newApplyService(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	// A bare model name fails validateManifest at apply time (the exact
	// web-UI import bug). No packs/skills/seed, so the retry's apply succeeds.
	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/agents/imp.yaml", body: "name: imp\nmodel:\n  name: gemini-2.5-flash\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	// First import+apply fails: the bare model is rejected.
	_, err := svc.ImportFromGitHub(ctx, proj, &ImportGitHubRequest{
		URL:   "https://github.com/org/repo",
		Apply: true,
	})
	require.Error(t, err)
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)

	// The failed apply must have hard-deleted the row: no (name, version)
	// collision, no partial blueprint left behind.
	exists, err := svc.repo.ExistsByNameVersion(ctx, &proj, "imp", "1.0.0")
	require.NoError(t, err)
	assert.False(t, exists, "failed atomic import must not leave a published row")

	// Retry with the model mapped succeeds cleanly (no 409).
	bp, err := svc.ImportFromGitHub(ctx, proj, &ImportGitHubRequest{
		URL:            "https://github.com/org/repo",
		Apply:          true,
		ModelOverrides: map[string]string{"imp": "openai/gpt-4o"},
	})
	require.NoError(t, err)
	assert.Equal(t, StatusPublished, bp.Status)
}

// TestService_ImportFromGitHub_AtomicRollback_PackBearing is the regression
// guard for the rollback FK defect. A manifest that materializes a schema pack
// (creating a kb.blueprint_pack_claims row) and THEN fails during agent
// materialization must still have its just-created blueprint row deleted, so a
// retry re-imports cleanly instead of colliding on (name, version) with a 409.
// kb.blueprint_pack_claims.blueprint_id is a non-cascading FK to
// kb.blueprints(id), so before the rollback cleared the claims the delete failed
// (and the error was swallowed), leaving the row behind.
func TestService_ImportFromGitHub_AtomicRollback_PackBearing(t *testing.T) {
	db := connectTestDB(t)
	svc, _, _ := newMigrationService(t, db)
	ctx := context.Background()
	_, proj := seedProject(t, db)

	packName := uniqueName("rollback-pack")
	packYAML := `name: ` + packName + `
version: 1.0.0
objectTypes:
  - name: Thing
    label: Thing
`
	// failureLimit is an int on agents.AgentWorkConfig; a string makes
	// applyWorkConfig fail AFTER applyPacks has created and claimed the pack.
	agentYAML := `name: rollback-agent
description: d
workConfig:
  failureLimit: not-a-number
`
	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: packYAML},
		{name: "repo-HEAD/agents/agent.yaml", body: agentYAML},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	// Import+apply fails during agent materialization, after the pack claim.
	_, err := svc.ImportFromGitHub(ctx, proj, &ImportGitHubRequest{
		URL:   "https://github.com/org/repo",
		Apply: true,
	})
	require.Error(t, err)
	assertAppErrorCode(t, err, apperror.ErrBadRequest.Code)

	// The pack claim existed before the failure, so this asserts the rollback
	// cleared it before deleting the row: the row must be gone.
	exists, err := svc.repo.ExistsByNameVersion(ctx, &proj, packName, "1.0.0")
	require.NoError(t, err)
	assert.False(t, exists, "failed atomic import with a pack must not leave a published row")

	// Retry with the agent fixed succeeds cleanly (no 409).
	fixedAgent := `name: rollback-agent
description: d
`
	archive2 := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: packYAML},
		{name: "repo-HEAD/agents/agent.yaml", body: fixedAgent},
	})
	ts2 := servingTarball(t, archive2)
	svc.fetcher = testGitHubFetcher(t, ts2.URL, 1<<20, 1<<20, 100)

	bp, err := svc.ImportFromGitHub(ctx, proj, &ImportGitHubRequest{
		URL:   "https://github.com/org/repo",
		Apply: true,
	})
	require.NoError(t, err)
	assert.Equal(t, StatusPublished, bp.Status)
}

// service path performs the same fetch+parse (and model override) but creates no
// blueprint row.
func TestService_PreviewFromGitHub_CreatesNothing(t *testing.T) {
	db := connectTestDB(t)
	svc := newServiceFromBun(t, db)
	ctx := context.Background()

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: "name: preview-pack\nversion: 1.0.0\nobjectTypes:\n  - name: Thing\n"},
	})
	ts := servingTarball(t, archive)
	svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	preview, err := svc.PreviewFromGitHub(ctx, &ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)
	assert.Equal(t, "preview-pack", preview.Name)
	assert.Equal(t, "1.0.0", preview.Version)
	var m BlueprintManifest
	require.NoError(t, json.Unmarshal(preview.Manifest, &m))
	require.Len(t, m.Packs, 1)

	// Nothing created.
	list, err := svc.ListBlueprints(ctx, "", "")
	require.NoError(t, err)
	assert.Empty(t, list)
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

// TestHandler_ImportBlueprint_ProjectScoped is the positive project-scoping
// handler path (task 3.1): an authenticated caller with a project context
// imports a valid archive, receives 201, and the created blueprint is published
// and owned by the caller's project.
func TestHandler_ImportBlueprint_ProjectScoped(t *testing.T) {
	h, _ := newAuthzHandler(t)
	ctx := context.Background()
	proj := uuid.NewString()

	archive := buildTarGz(t, []tarEntry{
		{name: "repo-HEAD/packs/pack.yaml", body: "name: " + uniqueName("handler-proj-import") + "\nversion: 1.0.0\nobjectTypes:\n  - name: Thing\n"},
	})
	ts := servingTarball(t, archive)
	h.svc.fetcher = testGitHubFetcher(t, ts.URL, 1<<20, 1<<20, 100)

	body, err := json.Marshal(ImportGitHubRequest{URL: "https://github.com/org/repo"})
	require.NoError(t, err)

	c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints/import", body,
		&auth.AuthUser{ID: uuid.NewString(), ProjectID: proj})
	require.NoError(t, h.ImportBlueprint(c))
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var bp Blueprint
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bp))
	require.NotNil(t, bp.ProjectID, "handler import must be scoped to the caller's project")
	assert.Equal(t, proj, *bp.ProjectID)
	assert.Equal(t, StatusPublished, bp.Status)

	stored, err := h.svc.repo.GetByID(ctx, proj, bp.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.ProjectID)
	assert.Equal(t, proj, *stored.ProjectID)
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
