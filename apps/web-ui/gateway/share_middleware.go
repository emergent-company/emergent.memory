package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/time/rate"
)

// maxShareRateKeys bounds the in-memory bucket maps for the share surface so a
// flood of distinct IPs/links cannot grow them without bound.
const maxShareRateKeys = 10000

// rateBucketIdle is how long a bucket stays cached after its last use before an
// at-capacity sweep evicts it.
const rateBucketIdle = 5 * time.Minute

// rateBucket pairs a token bucket with its last-seen time for eviction.
type rateBucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// keyedRateLimiter is a bounded in-memory token-bucket limiter keyed by an
// opaque string (client IP, or a hash of the share token). Buckets are
// per-instance: a multi-replica deployment does not share rate state.
type keyedRateLimiter struct {
	mu      sync.Mutex
	perMin  int
	burst   int
	buckets map[string]rateBucket
}

func newKeyedRateLimiter(perMin, burst int) *keyedRateLimiter {
	return &keyedRateLimiter{
		perMin:  perMin,
		burst:   burst,
		buckets: make(map[string]rateBucket),
	}
}

// allow reports whether one request is permitted for key. An empty key is not
// limited (there is no dimension to key on — the request is denied elsewhere).
func (l *keyedRateLimiter) allow(key string) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxShareRateKeys {
			l.evictIdle(now)
		}
		if len(l.buckets) >= maxShareRateKeys {
			// Still at capacity after sweeping idle buckets: fail closed. Deny
			// the new key rather than silently allowing it, so a flood of
			// distinct valid tokens can no longer fill the map and disable the
			// per-link limit — the authoritative, non-spoofable dimension.
			return false
		}
		b = rateBucket{lim: rate.NewLimiter(rate.Limit(float64(l.perMin)/60.0), l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now
	l.buckets[key] = b
	return b.lim.Allow()
}

func (l *keyedRateLimiter) evictIdle(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) > rateBucketIdle {
			delete(l.buckets, k)
		}
	}
}

// hashShareToken derives the per-link bucket key from the share token, so the
// raw token is never used as a map key or logged.
func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// shareRateLimit bounds the public share surface: per client IP (advisory,
// best-effort) always, and per share link (authoritative) when the request
// carries a share cookie. On trip it returns 429 with the client-facing
// rate-limited code.
//
// The client IP is resolved via c.RealIP(), backed by the echo IPExtractor set
// in main.go (ExtractIPFromXFFHeader), so behind traefik the bucket keys on the
// real client IP from X-Forwarded-For rather than collapsing every visitor into
// the proxy's socket address. The per-link bucket (SHA-256 of the share token)
// remains the authoritative, non-spoofable dimension.
func (s *Server) shareRateLimit(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if s.shareIPLimiter == nil || s.shareLinkLimiter == nil {
			return next(c)
		}
		if !s.shareIPLimiter.allow(c.RealIP()) {
			return shareRateLimited(c)
		}
		if key := s.shareLinkKey(c); key != "" {
			if !s.shareLinkLimiter.allow(key) {
				return shareRateLimited(c)
			}
		}
		return next(c)
	}
}

func shareRateLimited(c echo.Context) error {
	return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded", "code": "rate-limited"})
}

// shareLinkKey returns the per-link bucket key for a request: the SHA-256 of the
// decrypted share token. Returns "" when the request has no (valid) share cookie
// (e.g. the first exchange), in which case per-IP alone applies.
func (s *Server) shareLinkKey(c echo.Context) string {
	if s.cfg.ShareCookieSecret == "" {
		return ""
	}
	ck, err := c.Cookie(shareCookieName)
	if err != nil {
		return ""
	}
	claims, err := openShareCookie(s.cfg.ShareCookieSecret, ck.Value)
	if err != nil {
		return ""
	}
	return hashShareToken(claims.Token)
}

// shareSecurityHeaders hardens the public share surface only (never the
// authenticated app). The CSP allows inline styles/scripts because the share
// page uses an inline bootstrap; third-party scripts (e.g. the Sentry CDN) are
// deliberately disallowed, so the share page must not emit them.
func shareSecurityHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		h := c.Response().Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		return next(c)
	}
}
