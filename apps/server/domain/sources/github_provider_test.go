package sources

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/github"
)

// fakeGitHubTransport implements GitHubTransport without HTTP.
type fakeGitHubTransport struct {
	repos    []github.Repo
	blobs    []github.Blob
	contents map[string]string
	treeErr  error
	fetchErr error

	fetched   []string
	testRepo  string
	testRef   string
	listedCfg bool
}

func (f *fakeGitHubTransport) GetRepo(context.Context, string) (*github.Repo, error) {
	return &github.Repo{FullName: "acme/docs", DefaultBranch: "main"}, nil
}

func (f *fakeGitHubTransport) ListRepos(context.Context) ([]github.Repo, error) {
	return f.repos, nil
}

func (f *fakeGitHubTransport) ListTree(_ context.Context, repo, ref string) ([]github.Blob, error) {
	f.listedCfg = true
	f.testRepo = repo
	f.testRef = ref
	if f.treeErr != nil {
		return nil, f.treeErr
	}
	return f.blobs, nil
}

func (f *fakeGitHubTransport) FetchBlob(_ context.Context, _, sha string) ([]byte, error) {
	f.fetched = append(f.fetched, sha)
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return []byte(f.contents[sha]), nil
}

func (f *fakeGitHubTransport) TestConnection(_ context.Context, repo, ref string) error {
	f.testRepo = repo
	f.testRef = ref
	if f.treeErr != nil {
		return f.treeErr
	}
	return nil
}

func newFakeGitHubProvider(fake *fakeGitHubTransport) SourceProvider {
	return NewGitHubProvider(func(context.Context, github.Credentials) (GitHubTransport, error) {
		return fake, nil
	})
}

func TestGitHubProvider_Catalog(t *testing.T) {
	p := newGitHubProvider("")
	c := p.Catalog()
	if c.Type != ProviderTypeGitHub {
		t.Errorf("Catalog().Type = %q, want %q", c.Type, ProviderTypeGitHub)
	}
	if c.Name != "GitHub" {
		t.Errorf("Catalog().Name = %q, want GitHub", c.Name)
	}
	if !c.Available {
		t.Error("Catalog().Available = false, want true")
	}
	if c.SourceType != SourceTypeRepo {
		t.Errorf("Catalog().SourceType = %q, want %q", c.SourceType, SourceTypeRepo)
	}
}

func TestGitHubProvider_ConfigSchema(t *testing.T) {
	schema := newGitHubProvider("").ConfigSchema()
	if !slices.Contains(schema.Required, "repo") {
		t.Fatalf("Required = %v, want repo", schema.Required)
	}
	for _, key := range []string{"repo", "ref", "paths", "extensions"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("schema properties missing %q", key)
		}
	}
}

func TestGitHubProvider_ValidateConfig(t *testing.T) {
	p := newGitHubProvider("")

	valid := []map[string]any{
		{"repo": "owner/name"},
		{"repo": "owner/name", "ref": "release/1.x"},
		{"repo": "owner/name", "ref": "release/1.x@2"},
		{"repo": "owner/name", "paths": []any{"docs/**", "README.md"}},
		{"repo": "owner/name", "paths": []any{"docs/**/", "**/"}},
		{"repo": "owner/name", "extensions": []any{".md", "txt"}},
	}
	for _, cfg := range valid {
		if err := p.ValidateConfig(cfg); err != nil {
			t.Errorf("ValidateConfig(%v) = %v, want nil", cfg, err)
		}
	}

	invalid := []map[string]any{
		{},
		{"repo": "owner"},
		{"repo": "owner/name/extra"},
		{"repo": "owner/../name"},
		{"repo": "owner/na me"},
		{"repo": "owner/name", "ref": "bad ref!"},
		{"repo": "owner/name", "paths": []any{"../../etc/passwd"}},
		{"repo": "owner/name", "extensions": []any{"bad/ext"}},
		{"repo": "owner/name", "extensions": []any{"*.md"}},
		// The API origin is deployment-configured, never per source (SSRF).
		{"repo": "owner/name", "api_base_url": "http://169.254.169.254"},
		{"repo": "owner/name", "base_url": "http://example.invalid"},
	}
	for _, cfg := range invalid {
		if err := p.ValidateConfig(cfg); err == nil {
			t.Errorf("ValidateConfig(%v) = nil, want error", cfg)
		}
	}
}

func TestGitHubProvider_ExtractCredentials(t *testing.T) {
	p := newGitHubProvider("")
	if _, err := p.ExtractCredentials(map[string]any{}); err == nil {
		t.Fatal("ExtractCredentials({}) = nil, want error")
	}
	creds, err := p.ExtractCredentials(map[string]any{"access_token": "at", "client_id": "cid", "client_secret": "cs"})
	if err != nil {
		t.Fatalf("ExtractCredentials() error = %v", err)
	}
	if creds["access_token"] != "at" {
		t.Errorf("creds[access_token] = %v, want at", creds["access_token"])
	}
	if creds["client_id"] != "cid" || creds["client_secret"] != "cs" {
		t.Errorf("creds = %v, want client_id/client_secret carried", creds)
	}
}

func TestGitHubProvider_CredentialKeys(t *testing.T) {
	p := newGitHubProvider("")
	keys := p.CredentialKeys()
	if !slices.Contains(keys, "access_token") {
		t.Errorf("CredentialKeys() = %v, want access_token", keys)
	}
}

func TestGitHubProvider_AuthMethods(t *testing.T) {
	p := newGitHubProvider("")
	methods := p.AuthMethods()
	if !slices.Contains(methods, AuthMethodOAuth) {
		t.Errorf("AuthMethods() = %v, want oauth", methods)
	}
	if !slices.Contains(methods, AuthMethodCredentials) {
		t.Errorf("AuthMethods() = %v, want credentials", methods)
	}
}

func TestGitHubProvider_ListResources(t *testing.T) {
	p := newFakeGitHubProvider(&fakeGitHubTransport{repos: []github.Repo{
		{FullName: "acme/private-docs", Private: true},
		{FullName: "acme/public-docs"},
	}})
	resources, err := p.ListResources(context.Background(), Credentials{"access_token": "at"}, nil)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources) != 2 || resources[0].ID != "acme/private-docs" || resources[1].Type != "repo" {
		t.Fatalf("ListResources() = %+v, want the two repositories", resources)
	}
}

func TestGitHubConnector_EnumerateFiltersScope(t *testing.T) {
	fake := &fakeGitHubTransport{blobs: []github.Blob{
		{Path: "docs/guide.md", SHA: "sha-guide"},
		{Path: "docs/nested/api.md", SHA: "sha-api"},
		{Path: "docs/notes.txt", SHA: "sha-notes"},
		{Path: "README.md", SHA: "sha-readme"},
		{Path: "assets/logo.png", SHA: "sha-logo"},
	}}
	p := newFakeGitHubProvider(fake)
	connector, err := p.NewConnector(context.Background(), Credentials{"access_token": "at"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	items, err := connector.Enumerate(context.Background(), map[string]any{
		"repo":  "acme/private-docs",
		"ref":   "main",
		"paths": []any{"docs/**"},
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if fake.testRepo != "acme/private-docs" || fake.testRef != "main" {
		t.Fatalf("ListTree repo/ref = %q/%q, want acme/private-docs/main", fake.testRepo, fake.testRef)
	}

	var ids []string
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	want := []string{"docs/guide.md", "docs/nested/api.md"}
	if !slices.Equal(ids, want) {
		t.Fatalf("Enumerate() ids = %v, want %v (non-markdown and out-of-scope paths excluded)", ids, want)
	}
	if items[0].RevisionID != "sha-guide" || items[0].MimeType != githubMarkdownMimeType {
		t.Errorf("item = %+v, want blob sha revision and markdown MIME type", items[0])
	}
	if items[0].Name != "docs/guide.md" {
		t.Errorf("item.Name = %q, want repository-relative path", items[0].Name)
	}
}

func TestGitHubConnector_EnumerateExtensionsOverride(t *testing.T) {
	fake := &fakeGitHubTransport{blobs: []github.Blob{
		{Path: "docs/guide.md", SHA: "sha-md"},
		{Path: "docs/notes.txt", SHA: "sha-txt"},
	}}
	p := newFakeGitHubProvider(fake)
	connector, err := p.NewConnector(context.Background(), Credentials{"access_token": "at"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	items, err := connector.Enumerate(context.Background(), map[string]any{
		"repo":       "acme/private-docs",
		"extensions": []any{"txt"},
	})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "docs/notes.txt" {
		t.Fatalf("Enumerate() = %+v, want only the .txt file", items)
	}
	if items[0].MimeType != "text/plain" {
		t.Errorf("mime type = %q, want text/plain", items[0].MimeType)
	}
}

func TestGitHubConnector_Fetch(t *testing.T) {
	fake := &fakeGitHubTransport{
		blobs:    []github.Blob{{Path: "docs/guide.md", SHA: "sha-guide"}},
		contents: map[string]string{"sha-guide": "# Guide\n\nhello"},
	}
	p := newFakeGitHubProvider(fake)
	connector, err := p.NewConnector(context.Background(), Credentials{"access_token": "at"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}

	if _, _, err := connector.Fetch(context.Background(), SourceItem{ID: "docs/guide.md", RevisionID: "sha-guide"}); err == nil {
		t.Fatal("Fetch() before Enumerate() = nil error, want an error")
	}

	items, err := connector.Enumerate(context.Background(), map[string]any{"repo": "acme/private-docs"})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	content, mimeType, err := connector.Fetch(context.Background(), items[0])
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if content != "# Guide\n\nhello" {
		t.Errorf("Fetch() content = %q, want blob content", content)
	}
	if mimeType != githubMarkdownMimeType {
		t.Errorf("Fetch() mimeType = %q, want %q", mimeType, githubMarkdownMimeType)
	}
}

func TestGitHubConnector_FetchRejectsNonUTF8(t *testing.T) {
	fake := &fakeGitHubTransport{
		blobs:    []github.Blob{{Path: "docs/binary.md", SHA: "sha-bin"}},
		contents: map[string]string{"sha-bin": string([]byte{0xff, 0xfe, 'x'})},
	}
	p := newFakeGitHubProvider(fake)
	connector, err := p.NewConnector(context.Background(), Credentials{"access_token": "at"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	items, err := connector.Enumerate(context.Background(), map[string]any{"repo": "acme/private-docs"})
	if err != nil {
		t.Fatalf("Enumerate() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Enumerate() returned %d items, want 1", len(items))
	}
	if _, _, err := connector.Fetch(context.Background(), items[0]); err == nil {
		t.Fatal("Fetch() = nil error, want a not-UTF-8 error")
	} else if !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("Fetch() error = %v, want it to mention UTF-8", err)
	}
}

func TestGitHubConnector_EnumeratePropagatesTreeError(t *testing.T) {
	fake := &fakeGitHubTransport{treeErr: errors.New("boom")}
	p := newFakeGitHubProvider(fake)
	connector, err := p.NewConnector(context.Background(), Credentials{"access_token": "at"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	if _, err := connector.Enumerate(context.Background(), map[string]any{"repo": "acme/private-docs"}); err == nil {
		t.Fatal("Enumerate() = nil error, want the transport error")
	}
}

func TestGitHubConnector_TestConnectionUsesConfig(t *testing.T) {
	fake := &fakeGitHubTransport{}
	p := newFakeGitHubProvider(fake)
	connector, err := p.NewConnector(context.Background(), Credentials{"access_token": "at"}, ConnectorDeps{})
	if err != nil {
		t.Fatalf("NewConnector() error = %v", err)
	}
	if err := connector.TestConnection(context.Background(), map[string]any{"repo": "acme/private-docs", "ref": "release/1.x"}); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if fake.testRepo != "acme/private-docs" || fake.testRef != "release/1.x" {
		t.Fatalf("TestConnection repo/ref = %q/%q, want the configured target", fake.testRepo, fake.testRef)
	}
}

func TestGitHubPathMatcher_RejectsInvalidUTF8(t *testing.T) {
	if _, err := newGitHubPathMatcher("docs/\xff/**"); err == nil {
		t.Fatal("newGitHubPathMatcher() accepted a pattern that is not valid UTF-8")
	}
}

func TestGitHubPathMatcher_TrailingSlashOnWildcardIsIgnored(t *testing.T) {
	// Documents the reading the matcher comment states: a slash on a wildcard
	// pattern does not make it directory-only.
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"docs/x*/", "docs/x", true},
		{"docs/x*/", "docs/x1/guide.md", false},
		{"docs/x*/**", "docs/x1/guide.md", true},
	}
	for _, tc := range cases {
		m, err := newGitHubPathMatcher(tc.pattern)
		if err != nil {
			t.Fatalf("newGitHubPathMatcher(%q) error = %v", tc.pattern, err)
		}
		if got := m.match(tc.path); got != tc.want {
			t.Errorf("match(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestGitHubPathMatcher_UnicodeNormalizationIsExact(t *testing.T) {
	// NFC "café" (U+00E9) versus NFD "café" (e + U+0301): matching is by exact
	// code point, so the two forms are different patterns.
	const nfc = "docs/caf\u00e9/**"
	const nfd = "docs/cafe\u0301/**"
	m, err := newGitHubPathMatcher(nfc)
	if err != nil {
		t.Fatalf("newGitHubPathMatcher(%q) error = %v", nfc, err)
	}
	if !m.match("docs/caf\u00e9/guide.md") {
		t.Error("NFC pattern must match the NFC path")
	}
	if m.match("docs/cafe\u0301/guide.md") {
		t.Error("NFC pattern must not match the NFD path")
	}
	if _, err := newGitHubPathMatcher(nfd); err != nil {
		t.Fatalf("newGitHubPathMatcher(%q) error = %v", nfd, err)
	}
}

func TestGitHubPathMatcher(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"docs/**", "docs/guide.md", true},
		{"docs/**", "docs/nested/api.md", true},
		{"docs/**", "docs", false},
		{"docs/**", "docsx/guide.md", false},
		{"**/*.md", "README.md", true},
		{"**/*.md", "docs/nested/api.md", true},
		{"**/*.md", "docs/nested/api.txt", false},
		{"docs", "docs/guide.md", true},
		{"docs", "docs", true},
		{"docs", "docsx/guide.md", false},
		{"docs/", "docs/guide.md", true},
		{"docs/**/", "docs/guide.md", true},
		{"docs/**/", "docs/nested/api.md", true},
		{"docs/**/", "docsx/guide.md", false},
		{"docs/*/", "docs/guide.md", true},
		{"docs/*/", "docs/nested/api.md", false},
		{"**/", "README.md", true},
		{"**/", "docs/nested/api.md", true},
		{"docs//", "docs/guide.md", true},
		{"docs/café/**", "docs/café/guide.md", true},
		{"docs/café/**", "docs/café/nested/api.md", true},
		{"docs/café/**", "docs/cafe/guide.md", false},
		{"réseau/*.md", "réseau/guide.md", true},
		{"réseau/*.md", "reseau/guide.md", false},
		{"caf?/*.md", "café/guide.md", true},
		{"docs/*.md", "docs/guide.md", true},
		{"docs/*.md", "docs/nested/api.md", false},
		{"docs/?.md", "docs/a.md", true},
		{"docs/?.md", "docs/ab.md", false},
	}
	for _, tc := range cases {
		m, err := newGitHubPathMatcher(tc.pattern)
		if err != nil {
			t.Fatalf("newGitHubPathMatcher(%q) error = %v", tc.pattern, err)
		}
		if got := m.match(tc.path); got != tc.want {
			t.Errorf("match(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}
