package webcrawl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// httpFetcher adapts an http.Client to the Fetcher interface, hitting the raw
// URL directly (the crawler does not perform SSRF checks — that is the
// injected fetcher's job in production).
type httpFetcher struct {
	client *http.Client
}

func newHTTPFetcher() *httpFetcher {
	return &httpFetcher{client: &http.Client{}}
}

func (h *httpFetcher) Get(ctx context.Context, rawURL string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &Response{URL: resp.Request.URL.String(), Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// suppressSleep makes the per-host delay a no-op for the duration of a test.
func suppressSleep(t *testing.T) {
	t.Helper()
	orig := sleepFunc
	sleepFunc = func(time.Duration) {}
	t.Cleanup(func() { sleepFunc = orig })
}

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *httpFetcher) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, newHTTPFetcher()
}

func TestCrawlPagesOnlyDoesNotFollowLinks(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			_, _ = w.Write([]byte(`<html><body><a href="/b">b</a></body></html>`))
		case "/b":
			_, _ = w.Write([]byte("B"))
		}
	})

	res, err := Crawl(context.Background(), f, Options{Seeds: []string{srv.URL + "/a"}, CrawlMode: CrawlModePagesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1 (must not follow links in pages_only)", len(res.Pages))
	}
	if !res.Complete {
		t.Error("Complete = false, want true")
	}
}

func TestCrawlSiteFollowsLinks(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			_, _ = w.Write([]byte(`<html><body><a href="/b">b</a></body></html>`))
		case "/b":
			_, _ = w.Write([]byte(`<html><body><a href="/c">c</a></body></html>`))
		case "/c":
			_, _ = w.Write([]byte("C"))
		}
	})

	res, err := Crawl(context.Background(), f, Options{Seeds: []string{srv.URL + "/a"}, CrawlMode: CrawlModeSite, MaxDepth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 3 {
		t.Fatalf("pages = %d, want 3 (a, b, c)", len(res.Pages))
	}
	if !res.Complete {
		t.Error("Complete = false, want true")
	}
}

func TestCrawlDepthCap(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			_, _ = w.Write([]byte(`<a href="/b">b</a>`))
		case "/b":
			_, _ = w.Write([]byte(`<a href="/c">c</a>`))
		case "/c":
			_, _ = w.Write([]byte("C"))
		}
	})

	res, err := Crawl(context.Background(), f, Options{Seeds: []string{srv.URL + "/a"}, CrawlMode: CrawlModeSite, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("pages = %d, want 2 (depth cap stops at /b)", len(res.Pages))
	}
	// /b is at depth 1 and links to /c, which the depth cap clips: that is a
	// frontier truncation, so Complete must be false.
	if !res.Truncated {
		t.Error("Truncated = false, want true (link clipped by MaxDepth)")
	}
	if res.Complete {
		t.Error("Complete = true, want false (link clipped by MaxDepth)")
	}
}

func TestCrawlPageCapTruncates(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body"))
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:     []string{srv.URL + "/a", srv.URL + "/b"},
		CrawlMode: CrawlModePagesOnly,
		MaxPages:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(res.Pages))
	}
	if !res.Truncated {
		t.Error("Truncated = false, want true")
	}
	if res.Complete {
		t.Error("Complete = true, want false")
	}
}

func TestCrawlCycleSafety(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			_, _ = w.Write([]byte(`<a href="/b">b</a>`))
		case "/b":
			_, _ = w.Write([]byte(`<a href="/a">a</a>`))
		}
	})

	res, err := Crawl(context.Background(), f, Options{Seeds: []string{srv.URL + "/a"}, CrawlMode: CrawlModeSite, MaxDepth: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("pages = %d, want 2 (cycle must terminate)", len(res.Pages))
	}
	if !res.Complete {
		t.Error("Complete = false, want true")
	}
}

func TestCrawl404Tombstone(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	res, err := Crawl(context.Background(), f, Options{Seeds: []string{srv.URL + "/missing"}, CrawlMode: CrawlModePagesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 0 {
		t.Fatalf("pages = %d, want 0", len(res.Pages))
	}
	if len(res.Tombstones) != 1 {
		t.Fatalf("tombstones = %d, want 1", len(res.Tombstones))
	}
	if !res.Complete {
		t.Error("Complete = false, want true (a tombstone is not a fetch error)")
	}
}

func TestCrawl500ErrorIncomplete(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	res, err := Crawl(context.Background(), f, Options{Seeds: []string{srv.URL + "/error"}, CrawlMode: CrawlModePagesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(res.Errors))
	}
	if res.Complete {
		t.Error("Complete = true, want false on a 5xx")
	}
}

func TestCrawlPerHostDelay(t *testing.T) {
	var sleeps []time.Duration
	origNow, origSleep := nowFunc, sleepFunc
	fixed := time.Now()
	nowFunc = func() time.Time { return fixed }
	sleepFunc = func(d time.Duration) { sleeps = append(sleeps, d) }
	t.Cleanup(func() { nowFunc = origNow; sleepFunc = origSleep })

	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body"))
	})

	_, err := Crawl(context.Background(), f, Options{
		Seeds:        []string{srv.URL + "/a", srv.URL + "/b"},
		CrawlMode:    CrawlModePagesOnly,
		PerHostDelay: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sleeps) < 1 {
		t.Fatal("expected at least one sleep between same-host fetches")
	}
	if sleeps[0] < 250*time.Millisecond {
		t.Errorf("first sleep = %v, want >= 250ms", sleeps[0])
	}
}

func TestCrawlByteBudgetTruncates(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// ~20 bytes each.
		_, _ = w.Write([]byte(strings.Repeat("x", 20)))
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:         []string{srv.URL + "/a", srv.URL + "/b"},
		CrawlMode:     CrawlModePagesOnly,
		MaxTotalBytes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1 (second page exceeds byte budget)", len(res.Pages))
	}
	if !res.Truncated {
		t.Error("Truncated = false, want true")
	}
	if res.Complete {
		t.Error("Complete = true, want false")
	}
}

func TestCrawlCacheLookupHitAndEviction(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			_, _ = w.Write([]byte("AAAA"))
		case "/b":
			_, _ = w.Write([]byte("BBBB"))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:         []string{srv.URL + "/a", srv.URL + "/b"},
		CrawlMode:     CrawlModePagesOnly,
		MaxCacheBytes: 6, // fits one 4-byte body; the second evicts the first
	})
	if err != nil {
		t.Fatal(err)
	}

	canonA, _ := Canonical(srv.URL+"/a", nil)
	canonB, _ := Canonical(srv.URL+"/b", nil)

	a, ok := res.Lookup(canonA)
	if !ok {
		t.Fatal("Lookup(a) = not found, want found")
	}
	if a.Body != nil {
		t.Errorf("Lookup(a).Body = %q, want nil (evicted from cache)", a.Body)
	}

	b, ok := res.Lookup(canonB)
	if !ok {
		t.Fatal("Lookup(b) = not found, want found")
	}
	if string(b.Body) != "BBBB" {
		t.Errorf("Lookup(b).Body = %q, want BBBB", b.Body)
	}
}

func TestCrawlSameOriginRestriction(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a" {
			_, _ = w.Write([]byte(`<a href="http://external.example/x">x</a>`))
			return
		}
		_, _ = w.Write([]byte("other"))
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:      []string{srv.URL + "/a"},
		CrawlMode:  CrawlModeSite,
		MaxDepth:   3,
		SameOrigin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1 (external link must not be followed)", len(res.Pages))
	}
}

func TestCrawlRobotsDisallow(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /blocked\n"))
		case "/ok":
			_, _ = w.Write([]byte("ok"))
		case "/blocked":
			_, _ = w.Write([]byte("blocked"))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:         []string{srv.URL + "/ok", srv.URL + "/blocked"},
		CrawlMode:     CrawlModePagesOnly,
		RespectRobots: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1 (/blocked must be skipped)", len(res.Pages))
	}
	if res.Pages[0].URL != srv.URL+"/ok" {
		t.Errorf("fetched URL = %q, want /ok", res.Pages[0].URL)
	}
}

func TestCrawlSitemapDiscovery(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + base + `/page1</loc></url><url><loc>` + base + `/page2</loc></url></urlset>`))
		default:
			_, _ = w.Write([]byte("page " + r.URL.Path))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:     []string{srv.URL + "/"},
		CrawlMode: CrawlModeSite,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 3 {
		t.Fatalf("pages = %d, want 3 (seed + page1 + page2)", len(res.Pages))
	}
}

func TestCrawlSitemapIndexRecursion(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>` + base + `/sitemap1.xml</loc></sitemap></sitemapindex>`))
		case "/sitemap1.xml":
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + base + `/page1</loc></url></urlset>`))
		default:
			_, _ = w.Write([]byte("page " + r.URL.Path))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:     []string{srv.URL + "/"},
		CrawlMode: CrawlModeSite,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 2 {
		t.Fatalf("pages = %d, want 2 (seed + page1 via sitemapindex)", len(res.Pages))
	}
}

func TestCrawlSitemapLocsAreScoped(t *testing.T) {
	suppressSleep(t)

	var externalHits atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalHits.Add(1)
		_, _ = w.Write([]byte("external"))
	}))
	t.Cleanup(external.Close)

	var page1Hits, excludedHits atomic.Int32
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
				`<url><loc>` + base + `/page1</loc></url>` +
				`<url><loc>` + base + `/excluded/secret</loc></url>` +
				`<url><loc>` + external.URL + `/cross</loc></url>` +
				`</urlset>`))
		case "/page1":
			page1Hits.Add(1)
			_, _ = w.Write([]byte("page1"))
		case "/excluded/secret":
			excludedHits.Add(1)
			_, _ = w.Write([]byte("secret"))
		default:
			_, _ = w.Write([]byte("page " + r.URL.Path))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:           []string{srv.URL + "/"},
		CrawlMode:       CrawlModeSite,
		MaxDepth:        1,
		SameOrigin:      true,
		ExcludePatterns: []string{"/excluded/**"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Pages) != 2 {
		t.Fatalf("pages = %d, want 2 (seed + in-scope /page1); got %v", len(res.Pages), pageURLs(res))
	}
	if excludedHits.Load() != 0 {
		t.Errorf("/excluded/secret fetched %d times, want 0 (exclude glob must filter sitemap locs)", excludedHits.Load())
	}
	if externalHits.Load() != 0 {
		t.Errorf("external origin fetched %d times, want 0 (cross-origin sitemap loc must be ignored)", externalHits.Load())
	}
	if page1Hits.Load() != 1 {
		t.Errorf("/page1 fetched %d times, want 1 (in-scope sitemap loc still crawled)", page1Hits.Load())
	}
}

func TestCrawlSitemapIndexCrossOriginIgnored(t *testing.T) {
	suppressSleep(t)

	var externalHits atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalHits.Add(1)
		_, _ = w.Write([]byte("external"))
	}))
	t.Cleanup(external.Close)

	var page1Hits atomic.Int32
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
				`<sitemap><loc>` + base + `/sitemap1.xml</loc></sitemap>` +
				`</sitemapindex>`))
		case "/sitemap1.xml":
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
				`<url><loc>` + base + `/page1</loc></url>` +
				`<url><loc>` + external.URL + `/cross</loc></url>` +
				`</urlset>`))
		case "/page1":
			page1Hits.Add(1)
			_, _ = w.Write([]byte("page1"))
		default:
			_, _ = w.Write([]byte("page " + r.URL.Path))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:      []string{srv.URL + "/"},
		CrawlMode:  CrawlModeSite,
		MaxDepth:   1,
		SameOrigin: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Pages) != 2 {
		t.Fatalf("pages = %d, want 2 (seed + in-scope /page1 via sitemapindex); got %v", len(res.Pages), pageURLs(res))
	}
	if externalHits.Load() != 0 {
		t.Errorf("external origin fetched %d times, want 0 (cross-origin nested loc must be ignored)", externalHits.Load())
	}
	if page1Hits.Load() != 1 {
		t.Errorf("/page1 fetched %d times, want 1 (in-scope nested loc still crawled)", page1Hits.Load())
	}
}

func TestCrawlSitemapDocumentCrossOriginRobotsSkipped(t *testing.T) {
	suppressSleep(t)

	var externalHits atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalHits.Add(1)
		_, _ = w.Write([]byte("external sitemap"))
	}))
	t.Cleanup(external.Close)

	var page1Hits atomic.Int32
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nSitemap: " + external.URL + "/sitemap.xml\n"))
		case "/":
			_, _ = w.Write([]byte(`<html><body><a href="/page1">p1</a></body></html>`))
		case "/page1":
			page1Hits.Add(1)
			_, _ = w.Write([]byte("page1"))
		default:
			_, _ = w.Write([]byte("page " + r.URL.Path))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:         []string{srv.URL + "/"},
		CrawlMode:     CrawlModeSite,
		MaxDepth:      1,
		SameOrigin:    true,
		RespectRobots: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if externalHits.Load() != 0 {
		t.Errorf("external host fetched %d times, want 0 (cross-origin robots Sitemap must not be fetched)", externalHits.Load())
	}
	if page1Hits.Load() != 1 {
		t.Errorf("/page1 fetched %d times, want 1 (in-scope link still crawled)", page1Hits.Load())
	}
	if len(res.Pages) != 2 {
		t.Errorf("pages = %d, want 2 (seed + /page1); got %v", len(res.Pages), pageURLs(res))
	}
}

func TestCrawlSitemapIndexCrossOriginDocumentSkipped(t *testing.T) {
	suppressSleep(t)

	var externalHits atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalHits.Add(1)
		_, _ = w.Write([]byte("external sitemap"))
	}))
	t.Cleanup(external.Close)

	var nestedHits atomic.Int32
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
				`<sitemap><loc>` + base + `/nested.xml</loc></sitemap>` +
				`<sitemap><loc>` + external.URL + `/evil.xml</loc></sitemap>` +
				`</sitemapindex>`))
		case "/nested.xml":
			nestedHits.Add(1)
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + base + `/page1</loc></url></urlset>`))
		case "/page1":
			_, _ = w.Write([]byte("page1"))
		default:
			_, _ = w.Write([]byte("page " + r.URL.Path))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:      []string{srv.URL + "/"},
		CrawlMode:  CrawlModeSite,
		MaxDepth:   1,
		SameOrigin: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if externalHits.Load() != 0 {
		t.Errorf("external host fetched %d times, want 0 (cross-origin nested sitemap must not be fetched)", externalHits.Load())
	}
	if nestedHits.Load() != 1 {
		t.Errorf("same-origin nested sitemap fetched %d times, want 1", nestedHits.Load())
	}
	if len(res.Pages) != 2 {
		t.Errorf("pages = %d, want 2 (seed + /page1 from nested sitemap); got %v", len(res.Pages), pageURLs(res))
	}
}

func TestCrawlSitemapDiscoveryNotGatedByIncludeGlobs(t *testing.T) {
	suppressSleep(t)

	var sitemapHits atomic.Int32
	var pageHits atomic.Int32
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			sitemapHits.Add(1)
			base := "http://" + r.Host
			_, _ = w.Write([]byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + base + `/docs/deep</loc></url></urlset>`))
		case "/docs/deep":
			pageHits.Add(1)
			_, _ = w.Write([]byte("deep page"))
		default:
			_, _ = w.Write([]byte("page " + r.URL.Path))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:           []string{srv.URL + "/docs/"},
		CrawlMode:       CrawlModeSite,
		MaxDepth:        1,
		SameOrigin:      true,
		IncludePatterns: []string{"/docs/**"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if sitemapHits.Load() != 1 {
		t.Errorf("/sitemap.xml fetched %d times, want 1 (include globs must not gate sitemap documents)", sitemapHits.Load())
	}
	if pageHits.Load() != 1 {
		t.Errorf("/docs/deep fetched %d times, want 1 (in-scope sitemap loc still crawled)", pageHits.Load())
	}
	if len(res.Pages) != 2 {
		t.Errorf("pages = %d, want 2 (seed /docs/ + /docs/deep); got %v", len(res.Pages), pageURLs(res))
	}
}

func TestCrawlSitemapFetchCountCapped(t *testing.T) {
	suppressSleep(t)

	var sitemapHits atomic.Int32
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/sitemap") && strings.HasSuffix(r.URL.Path, ".xml") {
			sitemapHits.Add(1)
			base := "http://" + r.Host
			if r.URL.Path == "/sitemap.xml" {
				var b strings.Builder
				b.WriteString(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
				for i := 0; i < 100; i++ {
					fmt.Fprintf(&b, `<sitemap><loc>%s/sitemap-%d.xml</loc></sitemap>`, base, i)
				}
				b.WriteString(`</sitemapindex>`)
				_, _ = w.Write([]byte(b.String()))
				return
			}
			_, _ = w.Write([]byte(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + base + `/page-1</loc></url></urlset>`))
			return
		}
		_, _ = w.Write([]byte("page " + r.URL.Path))
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:     []string{srv.URL + "/"},
		CrawlMode: CrawlModeSite,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sitemapHits.Load(); got != maxSitemapFetches {
		t.Errorf("sitemap documents fetched = %d, want exactly %d (cap must engage before 100)", got, maxSitemapFetches)
	}
	// The crawl still completes and ingests the seed plus the page the expanded
	// sitemaps named.
	if len(res.Pages) != 2 {
		t.Errorf("pages = %d, want 2 (seed + /page-1 from an expanded sitemap); got %v", len(res.Pages), pageURLs(res))
	}
}

func TestCrawlRedirectAliasNotDuplicated(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		case "/b":
			_, _ = w.Write([]byte(`<html><body><a href="/b">b</a></body></html>`))
		}
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:     []string{srv.URL + "/a"},
		CrawlMode: CrawlModeSite,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1 (the redirect alias /b must not be fetched again); got %v", len(res.Pages), pageURLs(res))
	}
}

// pageURLs returns the canonical URLs of a crawl result, for diagnostics.
func pageURLs(res *Result) []string {
	out := make([]string, len(res.Pages))
	for i, p := range res.Pages {
		out[i] = p.CanonicalURL
	}
	return out
}

func TestCrawlInvalidSeedIsNotHardFailure(t *testing.T) {
	suppressSleep(t)
	srv, f := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	res, err := Crawl(context.Background(), f, Options{
		Seeds:     []string{"ftp://bad/scheme", srv.URL + "/ok"},
		CrawlMode: CrawlModePagesOnly,
	})
	if err != nil {
		t.Fatalf("Crawl must not hard-fail on an invalid seed: %v", err)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("errors = %d, want 1 (the invalid seed)", len(res.Errors))
	}
	if !res.Errors[0].Advisory {
		t.Error("invalid seed error Advisory = false, want true (an invalid seed must not be reported as the crawl's blocking failure)")
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1 (the valid seed still crawled)", len(res.Pages))
	}
	if !res.Complete {
		t.Error("Complete = false, want true (invalid seed is advisory, not a fetch error)")
	}
}

func TestCrawlNilFetcherFails(t *testing.T) {
	if _, err := Crawl(context.Background(), nil, Options{}); err == nil {
		t.Error("nil fetcher must return an error")
	}
}

// blockedFetcher refuses every request, standing in for the outbound SSRF guard.
type blockedFetcher struct{ err error }

func (f blockedFetcher) Get(context.Context, string) (*Response, error) { return nil, f.err }

// TestCrawlBlockedSeedAdvisorySplit pins the split the source connector relies on
// when it reports a refused seed: the robots.txt fail-open warning is recorded
// first but is advisory, while the seed's own refusal is the blocking error.
func TestCrawlBlockedSeedAdvisorySplit(t *testing.T) {
	suppressSleep(t)
	blocked := errors.New("web: blocked URL: URL host 169.254.169.254 is in a disallowed address range")

	res, err := Crawl(context.Background(), blockedFetcher{err: blocked}, Options{
		Seeds:         []string{"http://169.254.169.254/"},
		CrawlMode:     CrawlModePagesOnly,
		RespectRobots: true,
	})
	if err != nil {
		t.Fatalf("Crawl must not hard-fail when every fetch is refused: %v", err)
	}
	if len(res.Pages) != 0 {
		t.Fatalf("pages = %d, want 0", len(res.Pages))
	}
	if len(res.Errors) < 2 {
		t.Fatalf("errors = %d, want at least 2 (robots warning + seed refusal)", len(res.Errors))
	}
	if first := res.Errors[0]; !first.Advisory || !strings.Contains(first.Err.Error(), "robots.txt") {
		t.Errorf("Errors[0] = %+v, want the advisory robots.txt warning first", first)
	}
	var blocking *PageError
	for i := range res.Errors {
		if !res.Errors[i].Advisory {
			blocking = &res.Errors[i]
			break
		}
	}
	if blocking == nil {
		t.Fatal("no non-advisory error recorded for the refused seed")
	}
	if blocking.URL != "http://169.254.169.254/" {
		t.Errorf("blocking error URL = %q, want the refused seed", blocking.URL)
	}
	if !strings.Contains(blocking.Err.Error(), "disallowed address range") {
		t.Errorf("blocking error = %v, want the outbound refusal", blocking.Err)
	}
	if res.Complete {
		t.Error("Complete = true, want false (the seed could not be fetched)")
	}
}
