package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/internal/outbound"
	"github.com/emergent-company/emergent.memory/internal/webcrawl"
)

// Web connector constants. MaxPageBytes/MaxTotalBytes/MaxCacheBytes are the v1
// crawl budgets; MaxCacheBytes is larger than MaxTotalBytes so every fetched
// body (each ≤ MaxPageBytes) survives in the result cache and Fetch never has to
// re-request a page.
const (
	webMarkdownMimeType = "text/markdown"
	webMaxPageBytes     = 5 << 20  // 5 MiB per page
	webMaxTotalBytes    = 50 << 20 // 50 MiB per job
	webMaxCacheBytes    = 64 << 20 // 64 MiB body cache
	webCrawlDeadline    = 10 * time.Minute
	webPerHostDelay     = time.Second
	webTestTimeout      = 10 * time.Second
	webTestByteCap      = 5 << 20 // 5 MiB connection-test cap

	// webTombstoneRecheckMax and webTombstoneRecheckBudget bound the one-off
	// confirmation pass over 404/410 URLs before they count as deletion
	// evidence.
	webTombstoneRecheckMax    = 25
	webTombstoneRecheckBudget = 60 * time.Second

	// webUserAgent identifies the crawler to sites and robots.txt. It reads as a
	// browser-like UA while naming the bot so operators can recognize it.
	webUserAgent = "Mozilla/5.0 (compatible; EmergentMemoryBot/1.0; +https://memory.emergent-company.ai)"
)

// webFetcherI is the fetch surface the connector needs: one Get per URL. It is
// satisfied by *webFetcher; the indirection lets tests inject a fake fetcher so
// confirmTombstones can be driven deterministically without a live HTTP client.
type webFetcherI interface {
	Get(ctx context.Context, rawURL string) (*webcrawl.Response, error)
}

// webConnector implements Connector over webcrawl's engine and the
// outbound-guarded webFetcher. It is reusable: Enumerate stores the crawl Result
// on the connector and Fetch serves bodies from it.
type webConnector struct {
	fetcher    webFetcherI
	userAgent  string
	deps       ConnectorDeps
	result     *webcrawl.Result
	tombstones []string
}

func newWebConnector(policy outbound.Policy, userAgent string, deps ConnectorDeps) *webConnector {
	return &webConnector{
		fetcher:   newWebFetcher(policy, userAgent),
		userAgent: userAgent,
		deps:      deps,
	}
}

// webSyncConfig is the parsed enumeration scope of a web source config.
type webSyncConfig struct {
	startURLs    []string
	includeGlobs []string
	excludeGlobs []string
	maxPages     int
	maxDepth     int
}

// parseWebConfig validates the config and projects it onto the typed crawl
// scope, applying the v1 defaults for max_pages/max_depth when absent.
func parseWebConfig(cfg map[string]any) (*webSyncConfig, error) {
	if err := validateWebConfig(cfg); err != nil {
		return nil, err
	}
	out := &webSyncConfig{
		startURLs:    stringSlice(cfg["start_urls"]),
		includeGlobs: stringSlice(cfg["include_globs"]),
		excludeGlobs: stringSlice(cfg["exclude_globs"]),
		maxPages:     webDefaultMaxPages,
		maxDepth:     webDefaultMaxDepth,
	}
	if v, ok := intValue(cfg["max_pages"]); ok {
		out.maxPages = v
	}
	if v, ok := intValue(cfg["max_depth"]); ok {
		out.maxDepth = v
	}
	return out, nil
}

// Enumerate parses the config into crawl Options and runs the crawl. Per-page
// failures never abort the enumeration: pages found are returned and errors are
// left to the crawl Result. An error is returned only when the crawl produced
// nothing usable and had page errors.
func (c *webConnector) Enumerate(ctx context.Context, cfg map[string]any) ([]SourceItem, error) {
	parsed, err := parseWebConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Depth 0 is expressed as pages-only: the engine's applyDefaults rewrites a
	// MaxDepth <= 0 back to 3, so "ingest only the seeds" must switch modes
	// rather than pass MaxDepth 0 in site mode.
	mode := webcrawl.CrawlModeSite
	if parsed.maxDepth == 0 {
		mode = webcrawl.CrawlModePagesOnly
	}

	opts := webcrawl.Options{
		Seeds:           parsed.startURLs,
		CrawlMode:       mode,
		MaxPages:        parsed.maxPages,
		MaxDepth:        parsed.maxDepth,
		MaxPageBytes:    webMaxPageBytes,
		MaxTotalBytes:   webMaxTotalBytes,
		Deadline:        webCrawlDeadline,
		SameOrigin:      true,
		IncludePatterns: parsed.includeGlobs,
		ExcludePatterns: parsed.excludeGlobs,
		RespectRobots:   true,
		UserAgent:       c.userAgent,
		PerHostDelay:    webPerHostDelay,
		MaxCacheBytes:   webMaxCacheBytes,
	}

	result, err := webcrawl.Crawl(ctx, c.fetcher, opts)
	if err != nil {
		return nil, fmt.Errorf("web: crawl: %w", err)
	}
	c.result = result
	c.tombstones = c.confirmTombstones(ctx, result.Tombstones)

	items := make([]SourceItem, 0, len(result.Pages))
	for _, p := range result.Pages {
		items = append(items, webPageItem(p))
	}

	// Nothing usable and the engine recorded page errors (all seeds failed, or
	// every page errored): fail the enumeration rather than silently syncing
	// zero. A site whose pages all returned 404/410 produces tombstones, not
	// page errors, and is a legitimate (deleting) empty enumeration.
	if len(items) == 0 && len(result.Errors) > 0 {
		return nil, fmt.Errorf("web: crawl produced no pages: %v", result.Errors[0].Err)
	}
	return items, nil
}

// Fetch serves an item's text content from the crawl result cache. A body that
// was evicted (or otherwise empty) is re-fetched through the same guarded path.
// HTML and text are converted to Markdown; PDF/DOCX-style documents are routed
// through deps.Extract; binaries/archives/images are a per-item error.
func (c *webConnector) Fetch(ctx context.Context, item SourceItem) (content, mimeType string, err error) {
	page, ok := c.result.Lookup(item.ID)
	if !ok {
		return "", "", fmt.Errorf("web: page %q not found in the crawl result", item.ID)
	}
	body := page.Body
	contentType := page.ContentType
	if len(body) == 0 {
		resp, ferr := c.fetcher.Get(ctx, item.ID)
		if ferr != nil {
			return "", "", fmt.Errorf("web: re-fetch %q: %w", item.ID, ferr)
		}
		body = resp.Body
		contentType = resp.Header.Get("Content-Type")
	}
	return c.convert(ctx, body, contentType, item.Name)
}

// convert turns a fetched body into ingestion-ready text, returning the content
// and its effective MIME type.
func (c *webConnector) convert(ctx context.Context, body []byte, contentType, filename string) (string, string, error) {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))

	switch {
	case isWebTextMedia(mediaType):
		text, mime, err := webcrawl.ToMarkdown(body, contentType, "")
		if err != nil {
			return "", "", fmt.Errorf("web: convert %q: %w", contentType, err)
		}
		return text, mime, nil

	case isWebExtractableDoc(mediaType):
		if c.deps.Extract == nil {
			return "", "", fmt.Errorf("web: cannot extract %q content (%s): no text extractor configured", mediaType, filename)
		}
		text, err := c.deps.Extract(ctx, body, filename, mediaType)
		if err != nil {
			return "", "", fmt.Errorf("web: extract %q: %w", mediaType, err)
		}
		return text, "text/plain", nil

	default:
		return "", "", fmt.Errorf("web: skipping unsupported content type %q for %s", mediaType, filename)
	}
}

// TestConnection performs one bounded, guarded GET of the first start URL.
// Success requires a 2xx with an HTML/text content type; the error names the URL
// and the reason (HTTP status, blocked host, timeout).
func (c *webConnector) TestConnection(ctx context.Context, cfg map[string]any) error {
	if err := validateWebConfig(cfg); err != nil {
		return err
	}
	parsed, err := parseWebConfig(cfg)
	if err != nil {
		return err
	}
	first := parsed.startURLs[0]

	ctx, cancel := context.WithTimeout(ctx, webTestTimeout)
	defer cancel()
	resp, err := c.fetcher.Get(ctx, first)
	if err != nil {
		return fmt.Errorf("web: connection test to %q failed: %w", first, err)
	}
	if len(resp.Body) > webTestByteCap {
		return fmt.Errorf("web: %q response body exceeds the %d-byte connection-test cap", first, webTestByteCap)
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return fmt.Errorf("web: %q returned HTTP %d", first, resp.Status)
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	if !isWebTextMedia(mediaType) {
		return fmt.Errorf("web: %q returned content type %q (want HTML/text)", first, mediaType)
	}
	return nil
}

// EnumerationComplete implements EnumerationCompleteness: the crawl reported a
// complete enumeration (no truncation and no page error).
func (c *webConnector) EnumerationComplete() bool {
	return c.result != nil && c.result.Complete
}

// Tombstones implements SourceTombstones: the canonical URLs this crawl
// confirmed 404/410 after a single re-check (see confirmTombstones).
func (c *webConnector) Tombstones() []string {
	return c.tombstones
}

// confirmTombstones re-requests each 404/410 URL once before it is treated as
// deletion evidence. A page that comes back 2xx on the second request is a
// transient miss (sidebar deploy, flaky edge, rate-limit page served as 404),
// so it is dropped from the tombstone set and its existing document is left
// untouched. The re-check is bounded by a total budget and a URL count so a
// dying origin cannot extend a sync indefinitely.
//
// The re-check is best-effort: a candidate whose re-check cannot be performed —
// the budget or context expires first, or the re-request errors — is never
// treated as a removal. Only a confirmed 404/410 is kept in the tombstone set;
// everything else is dropped so an unverified candidate cannot delete an
// existing document.
func (c *webConnector) confirmTombstones(ctx context.Context, candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) > webTombstoneRecheckMax {
		candidates = candidates[:webTombstoneRecheckMax]
	}
	deadline := time.Now().Add(webTombstoneRecheckBudget)
	confirmed := make([]string, 0, len(candidates))
	for _, u := range candidates {
		if time.Now().After(deadline) || ctx.Err() != nil {
			// Budget or context exhausted: the remaining candidates were never
			// re-checked, so they are not deletion evidence. Stop here and leave
			// them out of the confirmed set — deleting on unverified evidence is
			// worse than keeping a possibly-stale page.
			break
		}
		reqCtx, cancel := context.WithTimeout(ctx, webTestTimeout)
		resp, err := c.fetcher.Get(reqCtx, u)
		cancel()
		switch {
		case err != nil:
			// Unreachable on the re-check is not proof of removal.
			continue
		case resp.Status == http.StatusNotFound || resp.Status == http.StatusGone:
			confirmed = append(confirmed, u)
		}
	}
	return confirmed
}

// webPageItem projects a crawled Page onto the provider-neutral SourceItem. The
// ID is the canonical URL (the stable identity key); the revision is ETag, then
// Last-Modified, then the body hash.
func webPageItem(p webcrawl.Page) SourceItem {
	return SourceItem{
		ID:           p.CanonicalURL,
		Name:         webPageName(p),
		MimeType:     webMarkdownMimeType,
		RevisionID:   webRevisionID(p),
		ModifiedTime: webModifiedTime(p.LastModified),
	}
}

// webPageName derives a stable, readable filename: host + path with path
// separators replaced by "_", an empty path mapped to "index", and always
// suffixed ".md".
func webPageName(p webcrawl.Page) string {
	u, err := url.Parse(p.CanonicalURL)
	if err != nil {
		return "page.md"
	}
	host := u.Hostname()
	path := strings.TrimPrefix(u.Path, "/")
	if path == "" {
		path = "index"
	}
	return host + "_" + strings.ReplaceAll(path, "/", "_") + ".md"
}

// webRevisionID returns the first available of ETag, Last-Modified, then the
// sha256 of the body (hex).
func webRevisionID(p webcrawl.Page) string {
	if p.ETag != "" {
		return p.ETag
	}
	if p.LastModified != "" {
		return p.LastModified
	}
	sum := sha256.Sum256(p.Body)
	return hex.EncodeToString(sum[:])
}

// webModifiedTime parses an HTTP Last-Modified header; a missing or unrecognised
// value yields the zero time (ModifiedTime is informational only).
func webModifiedTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := http.ParseTime(s); err == nil {
		return t
	}
	return time.Time{}
}

// isWebTextMedia reports whether the media type is content the connector can
// convert to Markdown (or pass through) itself: HTML and plain text.
func isWebTextMedia(mediaType string) bool {
	return strings.HasPrefix(mediaType, "text/") || mediaType == "application/xhtml+xml"
}

// isWebExtractableDoc reports whether the media type is a document the connector
// routes through the xberg extract path. Images, archives, and other binaries are
// deliberately excluded: a crawl must not ingest opaque blobs.
func isWebExtractableDoc(mediaType string) bool {
	switch mediaType {
	case "application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.oasis.opendocument.text",
		"application/rtf":
		return true
	}
	return false
}
