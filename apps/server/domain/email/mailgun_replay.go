package email

import (
	"sync"
	"time"
)

// defaultReplayGuardMax bounds how many signature tokens are retained, so an
// unauthenticated flood cannot grow the store without bound. The rate limiter
// caps the fill rate; this cap bounds the worst case.
const defaultReplayGuardMax = 65536

// replayGuard records Mailgun signature tokens as single-use within the
// signature-tolerance window.
//
// Mailgun's HMAC binds only timestamp+token, not the request body (#1222), so a
// captured viable envelope can otherwise be re-paired with a forged body. The
// token is unique per Mailgun request; recording it once and rejecting reuse
// makes a replayed envelope unusable. This is a best-effort, in-process
// defence: the durable guarantee that an event is applied at most once remains
// the unique event-id index on kb.email_logs.
type replayGuard struct {
	mu   sync.Mutex
	seen map[string]time.Time

	ttl time.Duration
	max int
	now func() time.Time
}

// newReplayGuard creates a token store retaining each token for ttl, capped at
// max entries. A nil clock defaults to time.Now.
func newReplayGuard(ttl time.Duration, max int, now func() time.Time) *replayGuard {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = defaultWebhookTolerance
	}
	if max <= 0 {
		max = defaultReplayGuardMax
	}
	return &replayGuard{
		seen: make(map[string]time.Time),
		ttl:  ttl,
		max:  max,
		now:  now,
	}
}

// consume reports whether the token is fresh (true) and records it. A token
// seen within its TTL is a replay and returns false.
func (g *replayGuard) consume(token string) bool {
	if token == "" {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	if exp, ok := g.seen[token]; ok && now.Before(exp) {
		return false
	}

	g.seen[token] = now.Add(g.ttl)
	if len(g.seen) > g.max {
		g.evictLocked(now)
	}
	return true
}

// evictLocked drops expired entries and, if the store is still over its cap,
// the soonest-to-expire entries. Callers hold g.mu.
func (g *replayGuard) evictLocked(now time.Time) {
	for token, exp := range g.seen {
		if !now.Before(exp) {
			delete(g.seen, token)
		}
	}
	for len(g.seen) > g.max {
		var oldestToken string
		var oldestExp time.Time
		first := true
		for token, exp := range g.seen {
			if first || exp.Before(oldestExp) {
				oldestToken, oldestExp, first = token, exp, false
			}
		}
		delete(g.seen, oldestToken)
	}
}
