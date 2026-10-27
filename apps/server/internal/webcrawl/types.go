// Package webcrawl is a pure, injectable crawl engine for the web data source.
// It has no wiring into domain/sources: a following lane supplies the Fetcher
// (backed by internal/outbound's guarded HTTP client) and hands the Result to
// the ingestion pipeline.
package webcrawl

import (
	"context"
	"net/http"
	"time"
)

// Fetcher retrieves a single URL. The crawler injects this so tests can drive a
// local httptest server and production can back it with a guarded HTTP client
// without the engine depending on either.
type Fetcher interface {
	Get(ctx context.Context, rawURL string) (*Response, error)
}

// Response is the result of a single fetch. Body is the raw response body; the
// crawler applies size caps and stores it in the bounded body cache.
type Response struct {
	URL    string
	Status int
	Header http.Header
	Body   []byte
}

// CrawlMode values.
const (
	CrawlModePagesOnly = "pages_only" // fetch seeds only, never follow links
	CrawlModeSite      = "site"       // follow in-scope links + sitemap URLs
)

// Options controls a crawl. Zero/negative values fall back to sensible defaults
// (see applyDefaults); set fields explicitly in tests to keep them fast.
type Options struct {
	// Seeds are the starting URLs. Each is canonicalized; an invalid seed is
	// recorded as a per-seed error, not a hard failure.
	Seeds []string
	// CrawlMode is CrawlModePagesOnly or CrawlModeSite.
	CrawlMode string
	// MaxPages caps the total number of successfully fetched pages. When the
	// cap clips the frontier, Result.Truncated is set and Complete is false.
	MaxPages int
	// MaxDepth bounds link-following depth (seeds are depth 0).
	MaxDepth int
	// MaxPageBytes caps a single response body (including a Content-Length
	// pre-check). A page over the cap is a size-cap error.
	MaxPageBytes int
	// MaxTotalBytes caps the total bytes stored across all pages; hitting it
	// clips the frontier.
	MaxTotalBytes int
	// Deadline bounds the whole crawl.
	Deadline time.Duration
	// SameOrigin restricts followed links (and sitemap URLs) to the seed
	// origin(s).
	SameOrigin bool
	// IncludePatterns / ExcludePatterns are Glob patterns matched against the
	// URL path; ExcludePatterns win. Applies to followed links in site mode.
	IncludePatterns []string
	ExcludePatterns []string
	// RespectRobots enables per-fetch robots.txt checks.
	RespectRobots bool
	// UserAgent is sent on every request.
	UserAgent string
	// PerHostDelay is the minimum pause between two fetches to the same host.
	PerHostDelay time.Duration
	// MaxCacheBytes bounds the in-memory body cache handed back to the caller;
	// bodies evicted from it are reported as empty in Result.Pages.
	MaxCacheBytes int
}

// Page is one successfully fetched URL, deduped by canonical URL.
type Page struct {
	URL          string // final URL after redirects, as reported by the fetcher
	CanonicalURL string // normalized identity key
	Status       int
	ContentType  string
	Body         []byte // raw body, or nil when it did not fit the body cache
	Depth        int
	ETag         string
	LastModified string
}

// PageError records a per-URL failure (invalid seed, 5xx, other non-2xx status,
// timeout, or size-cap). It is distinct from a Tombstone (a confirmed 404/410).
type PageError struct {
	URL   string
	Depth int
	Err   error
	// Advisory marks an entry the crawl tolerates without treating the URL as
	// failed: a robots.txt fail-open warning or a malformed seed. Advisory
	// entries stay in Errors so callers can log them, but they are not the
	// reason a crawl produced nothing — callers reporting a hard failure should
	// pick the first non-advisory entry.
	Advisory bool
}

func (e PageError) Error() string {
	if e.Err == nil {
		return e.URL
	}
	return e.URL + ": " + e.Err.Error()
}

// Result is the output of a crawl.
type Result struct {
	Pages      []Page
	Tombstones []string // canonical URLs that returned 404/410
	Errors     []PageError
	// Complete is true only when the frontier drained without hitting a
	// MaxPages/MaxDepth/byte/deadline cap and without any page fetch error.
	// Advisory warnings (robots.txt fail-open, invalid seeds) are in Errors but
	// do not by themselves force Complete=false.
	Complete bool
	// Truncated is true when a cap (pages, depth, bytes, deadline) clipped the
	// frontier before it drained.
	Truncated bool
	// Visited is the number of fetch attempts made.
	Visited int
}

// Lookup returns the Page with the given canonical URL, if it was fetched. The
// returned Page's Body is empty when its body did not fit the bounded cache.
func (r *Result) Lookup(canonicalURL string) (Page, bool) {
	if r == nil {
		return Page{}, false
	}
	for _, p := range r.Pages {
		if p.CanonicalURL == canonicalURL {
			return p, true
		}
	}
	return Page{}, false
}
