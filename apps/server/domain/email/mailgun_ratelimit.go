package email

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxTrackedWebhookClients bounds the per-IP limiter map so a caller rotating
// X-Forwarded-For cannot grow it without bound.
const maxTrackedWebhookClients = 10000

// webhookClientIdleTTL is how long a client's limiter is retained after its
// last request before it becomes eligible for eviction.
const webhookClientIdleTTL = 10 * time.Minute

// clientLimiter pairs a token-bucket limiter with the time it was last used.
type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// webhookRateLimiter enforces a per-client and a global request budget on the
// public Mailgun webhook (#1223).
//
// The per-client key is echo's RealIP(), which honours X-Forwarded-For and is
// only trustworthy behind a proxy that overwrites the header. The global
// limiter therefore backstops a spoofed key: a caller cannot escape the global
// cap by rotating the per-client key. The upstream WAF/proxy remains the coarse
// first layer of defence; these in-process limiters bound the per-request work
// a flood can force.
type webhookRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientLimiter

	perMin int
	burst  int
	global *rate.Limiter

	now func() time.Time
}

// newWebhookRateLimiter builds a limiter with a per-client budget and a global
// backstop. A nil clock defaults to time.Now.
func newWebhookRateLimiter(perMin, burst, globalPerMin, globalBurst int, now func() time.Time) *webhookRateLimiter {
	if now == nil {
		now = time.Now
	}
	if perMin <= 0 {
		perMin = defaultWebhookRatePerMin
	}
	if burst <= 0 {
		burst = defaultWebhookRateBurst
	}
	if globalPerMin <= 0 {
		globalPerMin = defaultWebhookGlobalRatePerMin
	}
	if globalBurst <= 0 {
		globalBurst = defaultWebhookGlobalRateBurst
	}

	return &webhookRateLimiter{
		clients: make(map[string]*clientLimiter),
		perMin:  perMin,
		burst:   burst,
		global:  rate.NewLimiter(rate.Every(time.Minute/time.Duration(globalPerMin)), globalBurst),
		now:     now,
	}
}

// allow reports whether a request from key may proceed. The global limiter is
// checked first so a shared cap is enforced even when the client key is spoofed.
func (l *webhookRateLimiter) allow(key string) bool {
	if l.global != nil && !l.global.Allow() {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	client, ok := l.clients[key]
	if !ok {
		if len(l.clients) >= maxTrackedWebhookClients {
			l.evictLocked(now)
		}
		client = &clientLimiter{
			limiter: rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.perMin)), l.burst),
		}
		l.clients[key] = client
	}
	client.lastSeen = now

	return client.limiter.Allow()
}

// evictLocked drops idle clients and, if the map is still at its cap, the
// least-recently-seen entries. Callers hold l.mu.
func (l *webhookRateLimiter) evictLocked(now time.Time) {
	for key, client := range l.clients {
		if now.Sub(client.lastSeen) > webhookClientIdleTTL {
			delete(l.clients, key)
		}
	}
	for len(l.clients) >= maxTrackedWebhookClients {
		var oldestKey string
		var oldestSeen time.Time
		first := true
		for key, client := range l.clients {
			if first || client.lastSeen.Before(oldestSeen) {
				oldestKey, oldestSeen, first = key, client.lastSeen, false
			}
		}
		delete(l.clients, oldestKey)
	}
}
