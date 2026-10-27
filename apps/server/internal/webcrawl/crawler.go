package webcrawl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/net/html"
)

const (
	defaultMaxDepth       = 2
	defaultMaxPages       = 200
	defaultMaxPageBytes   = 2 << 20  // 2 MiB
	defaultMaxTotalBytes  = 64 << 20 // 64 MiB
	defaultMaxCacheBytes  = 8 << 20  // 8 MiB
	defaultRequestTimeout = 30 * time.Second
	defaultUserAgent      = "EmergentMemoryBot/1.0 (+https://memory.emergent-company.ai)"

	// maxSitemapFetches caps the total number of sitemap documents fetched per
	// crawl. It is independent of the transport's per-response read cap: each
	// sitemap fetch is a separate request, and a hostile sitemapindex could
	// otherwise name thousands of nested sitemaps, each fetched sequentially
	// (and each up to the byte cap) outside the per-host delay and the
	// MaxTotalBytes budget. Reaching the cap is a silent degradation — the crawl
	// keeps going with link-following — consistent with the failures-degrade
	// contract.
	maxSitemapFetches = 25
)

// nowFunc and sleepFunc are variables so tests can inject a deterministic clock
// and observe (or suppress) the per-host delay without real sleeping.
var (
	nowFunc   = time.Now
	sleepFunc = time.Sleep
)

// Crawl runs a breadth-first crawl over o.Seeds using the injected Fetcher. It
// never fails hard on a bad seed or a failed page: those are reported in
// Result.Errors; the returned error is reserved for caller misuse (e.g. a nil
// Fetcher).
//
// Truncation/complete semantics are subtle and deliberate:
//
//   - Truncated is true only when a cap — MaxPages, MaxDepth, MaxTotalBytes, or
//     Deadline — clipped the frontier before it drained.
//   - Complete is true only when the crawl drained without truncation and
//     without any page-level fetch error (5xx/other non-2xx status, timeout,
//     size-cap). Advisory entries (invalid seeds, robots.txt fail-open
//     warnings) are in Errors but do not by themselves force Complete=false.
func Crawl(ctx context.Context, f Fetcher, o Options) (*Result, error) {
	if f == nil {
		return nil, errors.New("webcrawl: fetcher is required")
	}
	o = applyDefaults(o)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if o.Deadline > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.Deadline)
		defer cancel()
	}

	res := &Result{}
	cache := newBodyCache(o.MaxCacheBytes)
	robots := newRobotsChecker()

	type item struct {
		canonical string
		depth     int
	}
	var queue []item
	seen := map[string]bool{}

	// Seed the frontier. An invalid seed is a per-seed error, not a hard
	// failure.
	for _, seed := range o.Seeds {
		canonical, err := Canonical(seed, nil)
		if err != nil {
			res.Errors = append(res.Errors, PageError{URL: seed, Err: fmt.Errorf("invalid seed: %w", err), Advisory: true})
			continue
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		queue = append(queue, item{canonical, 0})
	}

	// Sitemap discovery (site mode only); failures degrade to link-following.
	// Every sitemap-derived loc is held to the same crawl scope as a followed
	// link (InScope): a sitemap is an untrusted document, so a cross-origin or
	// excluded loc must never enter the frontier. User-supplied seeds stay
	// exempt — an explicit seed is always fetched. The filter runs here, at
	// enqueue time, so an out-of-scope loc costs no request.
	if o.CrawlMode == CrawlModeSite {
		for _, su := range discoverSitemaps(ctx, f, o, robots) {
			canonical, err := Canonical(su, nil)
			if err != nil {
				continue
			}
			canonURL, perr := url.Parse(canonical)
			if perr != nil || !InScope(canonURL, o) {
				continue
			}
			if !seen[canonical] {
				seen[canonical] = true
				queue = append(queue, item{canonical, 0})
			}
		}
	}

	lastFetch := map[string]time.Time{}
	totalBytes := 0
	truncated := false
	hadFetchError := false

	for len(queue) > 0 {
		if ctx.Err() != nil {
			truncated = true
			break
		}
		if len(res.Pages) >= o.MaxPages {
			truncated = true
			break
		}

		cur := queue[0]
		queue = queue[1:]

		curURL, perr := url.Parse(cur.canonical)
		if perr != nil {
			hadFetchError = true
			res.Errors = append(res.Errors, PageError{URL: cur.canonical, Depth: cur.depth, Err: fmt.Errorf("parse canonical URL: %w", perr)})
			continue
		}

		// Per-host serialization: pause until PerHostDelay has elapsed since the
		// previous fetch to this host.
		host := curURL.Host
		if last, ok := lastFetch[host]; ok {
			if d := o.PerHostDelay - nowFunc().Sub(last); d > 0 {
				sleepFunc(d)
			}
		}
		lastFetch[host] = nowFunc()

		// Robots check.
		if o.RespectRobots {
			allowed, warn := robots.check(ctx, f, o, originOf(curURL), curURL.Path)
			if warn != nil {
				res.Errors = append(res.Errors, PageError{URL: cur.canonical, Err: warn, Advisory: true})
			}
			if !allowed {
				continue
			}
		}

		reqCtx, reqCancel := context.WithTimeout(ctx, defaultRequestTimeout)
		resp, err := f.Get(reqCtx, cur.canonical)
		reqCancel()
		res.Visited++

		if err != nil {
			hadFetchError = true
			res.Errors = append(res.Errors, PageError{URL: cur.canonical, Depth: cur.depth, Err: err})
			continue
		}

		switch {
		case resp.Status == http.StatusNotFound || resp.Status == http.StatusGone:
			res.Tombstones = append(res.Tombstones, cur.canonical)
			continue
		case resp.Status >= 200 && resp.Status < 300:
			// success, fall through
		default:
			hadFetchError = true
			res.Errors = append(res.Errors, PageError{URL: cur.canonical, Depth: cur.depth, Err: fmt.Errorf("HTTP %d", resp.Status)})
			continue
		}

		// Per-page size cap: Content-Length pre-check plus actual body length.
		if o.MaxPageBytes > 0 {
			if cl := resp.Header.Get("Content-Length"); cl != "" {
				if n, cerr := strconv.ParseInt(cl, 10, 64); cerr == nil && n > int64(o.MaxPageBytes) {
					hadFetchError = true
					res.Errors = append(res.Errors, PageError{URL: cur.canonical, Depth: cur.depth, Err: fmt.Errorf("content-length %d exceeds MaxPageBytes %d", n, o.MaxPageBytes)})
					continue
				}
			}
			if len(resp.Body) > o.MaxPageBytes {
				hadFetchError = true
				res.Errors = append(res.Errors, PageError{URL: cur.canonical, Depth: cur.depth, Err: fmt.Errorf("body size %d exceeds MaxPageBytes %d", len(resp.Body), o.MaxPageBytes)})
				continue
			}
		}

		// Total byte budget.
		if o.MaxTotalBytes > 0 && totalBytes+len(resp.Body) > o.MaxTotalBytes {
			truncated = true
			break
		}
		totalBytes += len(resp.Body)

		// Record page metadata; the body is resolved against the cache at the
		// end so a body evicted (or too large for) the bounded cache is
		// reported empty, never duplicated past the budget.
		res.Pages = append(res.Pages, Page{
			URL:          resp.URL,
			CanonicalURL: cur.canonical,
			Status:       resp.Status,
			ContentType:  resp.Header.Get("Content-Type"),
			Depth:        cur.depth,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		})
		cache.put(cur.canonical, resp.Body)

		// A redirect means resp.URL (the post-redirect final URL) differs from
		// cur.canonical (the requested URL). Mark the final URL's canonical as
		// seen so a later link to it is not fetched a second time under a second
		// identity, which would duplicate the same content. The page keeps the
		// requested canonical as its id: changing item identity on redirect would
		// churn documents that already exist under the requested URL.
		if resp.URL != "" {
			if finalCanon, cerr := Canonical(resp.URL, nil); cerr == nil && finalCanon != cur.canonical {
				seen[finalCanon] = true
			}
		}

		// Follow links in site mode. A link that would exceed MaxDepth clips the
		// frontier (Truncated), matching the documented truncation semantics.
		if o.CrawlMode == CrawlModeSite {
			for _, link := range extractLinks(resp.Body) {
				ref, err := url.Parse(link)
				if err != nil {
					continue
				}
				canonical, err := Canonical(curURL.ResolveReference(ref).String(), nil)
				if err != nil {
					continue
				}
				canonURL, _ := url.Parse(canonical)
				if !InScope(canonURL, o) {
					continue
				}
				if seen[canonical] {
					continue
				}
				if cur.depth+1 > o.MaxDepth {
					truncated = true
					continue
				}
				seen[canonical] = true
				queue = append(queue, item{canonical, cur.depth + 1})
			}
		}
	}

	// Resolve page bodies against the (bounded, LRU) cache.
	for i := range res.Pages {
		res.Pages[i].Body = cache.get(res.Pages[i].CanonicalURL)
	}

	res.Truncated = truncated
	res.Complete = !truncated && !hadFetchError
	return res, nil
}

func applyDefaults(o Options) Options {
	if o.CrawlMode == "" {
		o.CrawlMode = CrawlModePagesOnly
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = defaultMaxDepth
	}
	if o.MaxPages <= 0 {
		o.MaxPages = defaultMaxPages
	}
	if o.MaxPageBytes <= 0 {
		o.MaxPageBytes = defaultMaxPageBytes
	}
	if o.MaxTotalBytes <= 0 {
		o.MaxTotalBytes = defaultMaxTotalBytes
	}
	if o.MaxCacheBytes <= 0 {
		o.MaxCacheBytes = defaultMaxCacheBytes
	}
	if o.PerHostDelay <= 0 {
		o.PerHostDelay = time.Second
	}
	if o.UserAgent == "" {
		o.UserAgent = defaultUserAgent
	}
	return o
}

// extractLinks returns the href values of every <a> element in an HTML body, in
// document order.
func extractLinks(body []byte) []string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var links []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					links = append(links, a.Val)
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return links
}

// discoverSitemaps returns the page URLs discovered from each seed origin's
// sitemap(s): the default /sitemap.xml plus any Sitemap directives in
// robots.txt (when RespectRobots is on). A sitemapindex is recursed into once,
// and the total number of sitemap documents fetched across the whole crawl is
// capped at maxSitemapFetches (see that constant).
//
// Sitemap *documents* are scoped by origin only: a document that merely lists
// URLs is not a page, so the include/exclude globs (which describe which page
// paths to ingest) must not gate sitemap discovery — a cross-origin sitemap is
// skipped, but an in-scope sitemap under a glob-excluded path is still fetched.
// The page locs it yields are then held to the full InScope check at enqueue
// time in Crawl.
//
// Any fetch/parse failure, a sitemap dropped by the cap, or a cross-origin
// sitemap degrades to link-following (the URL set is simply dropped), never an
// error.
func discoverSitemaps(ctx context.Context, f Fetcher, o Options, robots *robotsChecker) []string {
	origins := map[string]bool{}
	for _, seed := range o.Seeds {
		u, err := url.Parse(seed)
		if err != nil {
			continue
		}
		origins[originOf(u)] = true
	}

	sameOrigin := sameOriginPredicate(o.Seeds)

	var out []string
	seenSitemap := map[string]bool{}
	fetches := 0

	for origin := range origins {
		if o.RespectRobots {
			_, _ = robots.check(ctx, f, o, origin, "/") // fetch robots.txt, collect Sitemap lines
		}
		candidates := []string{origin + "/sitemap.xml"}
		candidates = append(candidates, robots.sitemaps(origin)...)

		for _, su := range candidates {
			suURL, err := url.Parse(su)
			if err != nil || !sameOrigin(suURL) {
				continue
			}
			out = append(out, fetchSitemap(ctx, f, su, 0, seenSitemap, &fetches, sameOrigin)...)
		}
	}
	return out
}

// fetchSitemap fetches and expands one sitemap, recursing into a sitemapindex
// up to depth 1. The shared fetches counter enforces the maxSitemapFetches cap
// across the whole crawl: once it is reached, further sitemaps are silently
// skipped (the crawl continues with link-following). Nested sitemap documents
// are fetched only when same-origin with a seed (sameOrigin); the page locs they
// yield are scoped separately by Crawl's InScope filter.
func fetchSitemap(ctx context.Context, f Fetcher, su string, depth int, seen map[string]bool, fetches *int, sameOrigin func(*url.URL) bool) []string {
	if depth > 1 || su == "" || seen[su] || *fetches >= maxSitemapFetches {
		return nil
	}
	seen[su] = true
	*fetches++

	resp, err := f.Get(ctx, su)
	if err != nil || resp.Status != http.StatusOK {
		return nil
	}
	locs, isIndex, err := ParseSitemap(resp.Body)
	if err != nil {
		return nil
	}
	if !isIndex {
		return locs
	}

	base, _ := url.Parse(su)
	var out []string
	for _, nested := range locs {
		ref, err := url.Parse(nested)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(ref)
		if !sameOrigin(resolved) {
			continue
		}
		out = append(out, fetchSitemap(ctx, f, resolved.String(), depth+1, seen, fetches, sameOrigin)...)
	}
	return out
}
