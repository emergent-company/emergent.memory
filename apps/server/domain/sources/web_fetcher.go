package sources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/internal/outbound"
	"github.com/emergent-company/emergent.memory/internal/webcrawl"
)

// webFetchCap is the maximum body a single fetch reads into memory. It is far
// larger than the crawl's MaxPageBytes so the engine's own size checks do the
// real capping; the fetcher's cap only guarantees it never reads unbounded. A
// body over this cap is a fetch error, reported as a per-page failure.
const webFetchCap = 64 << 20 // 64 MiB

// webFetcher implements webcrawl.Fetcher over the outbound SSRF guard. It builds
// one guarded *http.Client per origin and caches it (keyed by scheme://host), so
// a connector reuses the same guarded transports across its Enumerate and
// TestConnection calls without re-validating the same origin repeatedly.
type webFetcher struct {
	policy    outbound.Policy
	userAgent string

	mu      sync.Mutex
	clients map[string]*http.Client
}

func newWebFetcher(policy outbound.Policy, userAgent string) *webFetcher {
	return &webFetcher{
		policy:    policy,
		userAgent: userAgent,
		clients:   map[string]*http.Client{},
	}
}

// Get fetches rawURL through the guarded client for its origin and returns a
// *webcrawl.Response for any HTTP status with a nil error. It returns an error
// only for a blocked URL, a transport failure, or a body over the read cap;
// status classification (404/410 tombstone, other non-2xx page error) is the
// crawler's job.
func (f *webFetcher) Get(ctx context.Context, rawURL string) (*webcrawl.Response, error) {
	// Pre-flight the SSRF guard before the first request to an origin; a blocked
	// host is surfaced as a fetch error (a per-page failure), never a panic.
	if err := outbound.ValidateURL(ctx, rawURL, f.policy); err != nil {
		return nil, fmt.Errorf("web: blocked URL: %w", err)
	}

	client, err := f.clientFor(rawURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("web: build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,application/pdf;q=0.8,*/*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web: fetch %q: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read with a hard ceiling so a streaming/malformed server cannot exhaust
	// memory; Content-Length is reported faithfully to the caller via resp.Header.
	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchCap+1))
	if err != nil {
		return nil, fmt.Errorf("web: read body of %q: %w", rawURL, err)
	}
	if len(body) > webFetchCap {
		return nil, fmt.Errorf("web: body of %q exceeds the %d-byte read cap", rawURL, webFetchCap)
	}

	return &webcrawl.Response{
		URL:    resp.Request.URL.String(),
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   body,
	}, nil
}

// clientFor returns the cached guarded client for rawURL's origin, creating it
// on first use. The client carries a 30s timeout; the guard also validates each
// redirect hop and caps the chain length.
func (f *webFetcher) clientFor(rawURL string) (*http.Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("web: parse URL: %w", err)
	}
	key := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)

	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[key]; ok {
		return c, nil
	}
	c := outbound.NewClient(rawURL, f.policy)
	c.Timeout = 30 * time.Second
	f.clients[key] = c
	return c, nil
}
