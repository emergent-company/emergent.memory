package sources

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/emergent-company/emergent.memory/internal/github"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// githubDefaultExtensions are the file extensions treated as documentation when
// a source does not override them. Only text formats are ingested: the GitHub
// connector never downloads binaries.
var githubDefaultExtensions = []string{".md", ".markdown", ".mdx"}

// githubMarkdownMimeType is the MIME type stamped on markdown documents.
const githubMarkdownMimeType = "text/markdown"

// githubSyncConfig is the parsed enumeration scope of a GitHub source config.
type githubSyncConfig struct {
	repo       string
	ref        string
	paths      []*githubPathMatcher
	extensions []string
}

// includes reports whether a repository-relative path is in scope: its
// extension must be allowed and, when path filters are configured, its path
// must match at least one filter.
func (c *githubSyncConfig) includes(path string) bool {
	if !hasAllowedExtension(path, c.extensions) {
		return false
	}
	if len(c.paths) == 0 {
		return true
	}
	for _, m := range c.paths {
		if m.match(path) {
			return true
		}
	}
	return false
}

// githubPathMatcher is one compiled `paths` glob. `*` matches within a segment,
// `**` crosses segments, `?` matches one non-separator character, and a plain
// path ending in `/` matches the whole subtree. A trailing slash is ignored on
// wildcard patterns, so it does not make the match directory-only: `docs/**/`
// behaves like `docs/**`, and `docs/x*/` behaves like `docs/x*` — it matches the
// direct entry `docs/x` but not `docs/x1/file.md` (use `docs/x*/**` for that).
// Patterns are compiled rune-wise, so non-ASCII path segments work.
//
// Matching is exact by code point: neither the pattern nor the repository path
// is Unicode-normalized, so an NFD pattern (`cafe` + U+0301) does not match an
// NFC path (`café` with U+00E9) and vice versa. Submit patterns in the same
// normalization form as the repository's paths (NFC, GitHub's usual form).
type githubPathMatcher struct {
	pattern string
	re      *regexp.Regexp
}

func newGitHubPathMatcher(pattern string) (*githubPathMatcher, error) {
	p := strings.TrimSpace(pattern)
	if !utf8.ValidString(p) {
		// Decoding a rune from invalid bytes yields U+FFFD, so such a pattern
		// would compile to a literal that silently matches nothing. Reject it
		// instead of accepting a filter that cannot work.
		return nil, apperror.NewValidation("config.paths entries must be valid UTF-8")
	}
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return nil, apperror.NewValidation("config.paths entries must be non-empty")
	}
	if strings.Contains(p, "..") {
		return nil, apperror.NewValidation("config.paths entries must not contain '..'")
	}

	// Strip the trailing slash *before* deciding how to compile: treating it
	// first would regexp.QuoteMeta the glob characters, turning `docs/**/`
	// into the literal `docs/**/` and silently matching nothing.
	dirOnly := strings.HasSuffix(p, "/")
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		return nil, apperror.NewValidation("config.paths entries must name a path")
	}
	globbed := strings.ContainsAny(trimmed, "*?")

	var expr string
	switch {
	case dirOnly && !globbed:
		expr = "^" + regexp.QuoteMeta(trimmed) + "/"
	case !globbed:
		// A plain path selects that file and, when it names a directory, the
		// directory's whole subtree.
		expr = "^" + regexp.QuoteMeta(trimmed) + "(?:$|/.*)"
	default:
		var b strings.Builder
		b.WriteString("^")
		for i := 0; i < len(trimmed); {
			switch {
			case strings.HasPrefix(trimmed[i:], "**/"):
				b.WriteString("(?:.*/)?")
				i += 3
			case strings.HasPrefix(trimmed[i:], "**"):
				b.WriteString(".*")
				i += 2
			case trimmed[i] == '*':
				b.WriteString("[^/]*")
				i++
			case trimmed[i] == '?':
				b.WriteString("[^/]")
				i++
			default:
				// Decode a full rune: taking one byte at a time would turn
				// non-ASCII segment names into a sequence of U+00xx runes and
				// compile a pattern that matches nothing.
				r, size := utf8.DecodeRuneInString(trimmed[i:])
				b.WriteString(regexp.QuoteMeta(string(r)))
				i += size
			}
		}
		b.WriteString("$")
		expr = b.String()
	}

	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, apperror.NewValidation("config.paths contains an invalid pattern: " + pattern)
	}
	return &githubPathMatcher{pattern: pattern, re: re}, nil
}

func (m *githubPathMatcher) match(path string) bool {
	return m.re.MatchString(strings.TrimPrefix(path, "/"))
}

// githubConnector implements Connector over a GitHub transport. The repo is
// captured during Enumerate because Fetch (part of the provider-neutral
// Connector contract) receives only the SourceItem.
type githubConnector struct {
	client GitHubTransport
	repo   string
}

// Enumerate lists the markdown blobs in scope for the source config. The git
// blob SHA is the revision id: unchanged content keeps its SHA, so the sync
// worker skips it without fetching.
func (c *githubConnector) Enumerate(ctx context.Context, cfg map[string]any) ([]SourceItem, error) {
	parsed, err := parseGitHubSyncConfig(cfg)
	if err != nil {
		return nil, err
	}
	c.repo = parsed.repo

	blobs, err := c.client.ListTree(ctx, parsed.repo, parsed.ref)
	if err != nil {
		return nil, err
	}

	items := make([]SourceItem, 0, len(blobs))
	for _, blob := range blobs {
		if !parsed.includes(blob.Path) {
			continue
		}
		items = append(items, SourceItem{
			ID:         blob.Path,
			Name:       blob.Path,
			MimeType:   githubMimeType(blob.Path),
			RevisionID: blob.SHA,
		})
	}
	// The tree API is path-sorted, but sorting here keeps the sync order
	// deterministic for any transport (including the e2e wrapper).
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// Fetch returns the text content of one markdown blob.
func (c *githubConnector) Fetch(ctx context.Context, item SourceItem) (content, mimeType string, err error) {
	if c.repo == "" {
		return "", "", fmt.Errorf("github: connector used before enumeration")
	}
	data, err := c.client.FetchBlob(ctx, c.repo, item.RevisionID)
	if err != nil {
		return "", "", err
	}
	// Blobs are text; invalid UTF-8 would otherwise fail later as an opaque
	// Postgres encoding error for this item. Fail here with a clear reason so
	// the worker isolates it like any other per-item fetch error.
	if !utf8.Valid(data) {
		return "", "", fmt.Errorf("github: %s is not valid UTF-8 text", item.ID)
	}
	if item.MimeType == "" {
		return string(data), githubMimeType(item.ID), nil
	}
	return string(data), item.MimeType, nil
}

// TestConnection validates the token and the configured repository/ref.
func (c *githubConnector) TestConnection(ctx context.Context, cfg map[string]any) error {
	parsed, err := parseGitHubSyncConfig(cfg)
	if err != nil {
		return err
	}
	return c.client.TestConnection(ctx, parsed.repo, parsed.ref)
}

// parseGitHubSyncConfig validates and parses a GitHub source config.
func parseGitHubSyncConfig(cfg map[string]any) (*githubSyncConfig, error) {
	if err := validateGitHubConfig(cfg); err != nil {
		return nil, err
	}
	repo, _ := cfg["repo"].(string)
	ref, _ := cfg["ref"].(string)

	patterns := stringSlice(cfg["paths"])
	matchers := make([]*githubPathMatcher, 0, len(patterns))
	for _, p := range patterns {
		m, err := newGitHubPathMatcher(p)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, m)
	}

	extensions := normalizeExtensions(stringSlice(cfg["extensions"]))
	if len(extensions) == 0 {
		extensions = githubDefaultExtensions
	}
	return &githubSyncConfig{
		repo:       strings.TrimSpace(repo),
		ref:        strings.TrimSpace(ref),
		paths:      matchers,
		extensions: extensions,
	}, nil
}

// validateGitHubConfig enforces the github config contract.
//
// It deliberately rejects base-URL keys: the GitHub API origin is a deployment
// setting (GITHUB_API_BASE_URL), never a per-source value, so a source cannot
// point the server at an arbitrary host (SSRF).
func validateGitHubConfig(cfg map[string]any) error {
	repo, _ := cfg["repo"].(string)
	if strings.TrimSpace(repo) == "" {
		return apperror.NewValidation("config.repo is required")
	}
	if !validGitHubRepo(repo) {
		return apperror.NewValidation("config.repo must be a valid owner/name repository")
	}
	for _, key := range []string{"base_url", "api_base_url", "api_url", "endpoint"} {
		if _, ok := cfg[key]; ok {
			return apperror.NewValidation("config." + key + " is not supported; the GitHub API endpoint is deployment-configured")
		}
	}
	if ref, ok := cfg["ref"].(string); ok && strings.TrimSpace(ref) != "" {
		if !validGitHubRef(ref) {
			return apperror.NewValidation("config.ref contains invalid characters")
		}
	}
	for _, p := range stringSlice(cfg["paths"]) {
		if _, err := newGitHubPathMatcher(p); err != nil {
			return err
		}
	}
	for _, ext := range stringSlice(cfg["extensions"]) {
		if strings.ContainsAny(ext, "/\\ \t") || strings.EqualFold(ext, ".") {
			return apperror.NewValidation("config.extensions must be file extensions such as .md")
		}
		if strings.ContainsAny(ext, "*?") {
			return apperror.NewValidation("config.extensions must be literal extensions, not globs such as *.md")
		}
	}
	return nil
}

// validGitHubRepo accepts owner/name with GitHub-legal characters.
func validGitHubRepo(repo string) bool {
	// modelref:allow: repository identifiers are owner/name, not routed model names.
	parts := strings.Split(strings.TrimSpace(repo), "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '.' || r == '_' || r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// validGitHubRef accepts a branch, tag, or SHA; slashes are allowed (feature/x)
// and so is `@`, which git permits inside a ref name.
func validGitHubRef(ref string) bool {
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '/' || r == '+' || r == '-' || r == '@':
		default:
			return false
		}
	}
	return !strings.Contains(ref, "..")
}

// hasAllowedExtension reports whether path ends in an allowed extension.
func hasAllowedExtension(path string, extensions []string) bool {
	lower := strings.ToLower(path)
	for _, ext := range extensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// normalizeExtensions lowercases extensions and ensures each has a leading dot.
func normalizeExtensions(in []string) []string {
	out := make([]string, 0, len(in))
	for _, ext := range in {
		e := strings.ToLower(strings.TrimSpace(ext))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out = append(out, e)
	}
	return out
}

// githubMimeType maps a documentation file path to the MIME type recorded on
// the document.
func githubMimeType(path string) string {
	lower := strings.ToLower(path)
	for _, ext := range githubDefaultExtensions {
		if strings.HasSuffix(lower, ext) {
			return githubMarkdownMimeType
		}
	}
	return "text/plain"
}

// stringSlice projects a JSON-decoded config value onto a string slice. Values
// arrive as []any from the API and as []string from in-process callers.
func stringSlice(v any) []string {
	switch vals := v.(type) {
	case []string:
		return vals
	case []any:
		out := make([]string, 0, len(vals))
		for _, item := range vals {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// Compile-time assertion that the internal client satisfies the transport.
var _ GitHubTransport = (*github.Client)(nil)
