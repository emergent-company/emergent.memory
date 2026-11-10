package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/labstack/echo/v4"
)

// newShareRateTestEcho builds a bare echo server with the share rate-limit
// middleware, the given client-IP trust configuration, and a route that reports
// the resolved RealIP(). The limiters are 1 req/min, burst 1, so the second
// request from the same bucket is denied.
func newShareRateTestEcho(t *testing.T, cidrs []string) *echo.Echo {
	t.Helper()
	extractor, _, err := newIPExtractor(cidrs)
	if err != nil {
		t.Fatalf("newIPExtractor(%v): %v", cidrs, err)
	}
	s := &Server{
		cfg:              Config{},
		shareIPLimiter:   newKeyedRateLimiter(1, 1),
		shareLinkLimiter: newKeyedRateLimiter(60, 20),
	}
	e := echo.New()
	e.IPExtractor = extractor
	e.Use(s.shareRateLimit)
	e.GET("/share/ping", func(c echo.Context) error {
		return c.String(http.StatusOK, c.RealIP())
	})
	return e
}

func shareRateRequest(e *echo.Echo, peer, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/share/ping", nil)
	req.RemoteAddr = peer
	if xff != "" {
		req.Header.Set(echo.HeaderXForwardedFor, xff)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestShareRateLimitFailClosedSharedBucket is the advisory regression: with
// TRUSTED_PROXY_CIDRS unset, two requests from the same untrusted peer that
// carry different attacker-supplied X-Forwarded-For values must share one
// bucket. Before the fix the header moved the key, so each request got a fresh
// bucket and the limiter was bypassed.
func TestShareRateLimitFailClosedSharedBucket(t *testing.T) {
	e := newShareRateTestEcho(t, nil)

	if rec := shareRateRequest(e, "203.0.113.9:5555", "1.2.3.4"); rec.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	rec := shareRateRequest(e, "203.0.113.9:5555", "5.6.7.8")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request with a different spoofed XFF = %d, want 429: the limiter must key on the peer, not the header", rec.Code)
	}
}

// TestShareRateLimitTrustedProxyPerClient proves the deployed topology still
// rate-limits per real client: through a configured proxy the resolved client
// IP separates buckets, and the same client is still limited.
func TestShareRateLimitTrustedProxyPerClient(t *testing.T) {
	e := newShareRateTestEcho(t, []string{"127.0.0.1/32"})

	if rec := shareRateRequest(e, "127.0.0.1:443", "198.51.100.1"); rec.Code != http.StatusOK {
		t.Fatalf("client A first request status = %d, want 200", rec.Code)
	}
	if rec := shareRateRequest(e, "127.0.0.1:443", "198.51.100.2"); rec.Code != http.StatusOK {
		t.Fatalf("client B request = %d, want 200: a different client through the proxy must not share client A's bucket", rec.Code)
	}
	rec := shareRateRequest(e, "127.0.0.1:443", "198.51.100.1")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("client A second request = %d, want 429: the same client through the proxy must reuse its bucket", rec.Code)
	}
}

// TestKeyedRateLimiterDeniesAtCapacity fills the per-key map past its cap and
// asserts a NEW key is denied rather than silently allowed. Regression: the
// limiter used to return allow for every new key once at capacity, letting an
// attacker with enough distinct valid tokens disable the per-link limit.
func TestKeyedRateLimiterDeniesAtCapacity(t *testing.T) {
	l := newKeyedRateLimiter(60, 1)
	for i := range maxShareRateKeys {
		if !l.allow(strconv.Itoa(i)) {
			t.Fatalf("key %d unexpectedly denied while filling to capacity", i)
		}
	}
	if l.allow("overflow-key") {
		t.Fatal("limiter allowed a new key at capacity; expected it to deny")
	}
}
