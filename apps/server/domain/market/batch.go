package market

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AuthorizationBatch is one market-issued assertion plus its pre-issued
// request_ids. The assertion is presented to the seller; the seller echoes the
// request_id it served, and that id is what usage reporting names.
//
// A batch carries at most MaxBatchCalls ids and is usable for at most
// MaxBatchTTL. The client enforces both locally, so a market that hands out a
// longer-lived batch cannot widen the attribution window. An assertion that is
// not yet valid (nbf in the future, within the clock-skew leeway) is honoured
// too: the batch is retained, but Take hands out no id until that instant.
type AuthorizationBatch struct {
	EntitlementID string     `json:"entitlement_id,omitempty"`
	Assertion     string     `json:"assertion"`
	RequestIDs    []string   `json:"request_ids"`
	IssuedAt      MarketTime `json:"issued_at,omitempty"`
	ExpiresAt     MarketTime `json:"expires_at,omitempty"`
	ExpiresIn     int64      `json:"expires_in,omitempty"`
	// NotBefore is the instant the assertion becomes valid (its nbf claim, or
	// the authorize response's own nbf). Zero means "no constraint".
	NotBefore MarketTime `json:"nbf,omitempty"`
	KeyID     string     `json:"key_id,omitempty"`

	mu    sync.Mutex
	next  int
	clock func() time.Time
}

// Take consumes and returns the next unused request id. It returns false when
// the batch is exhausted, before the assertion's not-before instant, and also
// once its window has closed: an id handed out outside the window can be served
// but never attributed, because the seller rejects a call it cannot attribute
// and the market rejects a report naming an expired (or not-yet-valid) batch.
// The client therefore stops handing them out at the same instant the report
// path stops accepting them.
func (b *AuthorizationBatch) Take() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.next >= len(b.RequestIDs) {
		return "", false
	}
	now := b.now()
	if now.Before(b.NotBefore.Time) {
		return "", false
	}
	if !now.Before(b.ExpiresAt.Time) {
		return "", false
	}
	id := b.RequestIDs[b.next]
	b.next++
	return id, true
}

// Reserve takes n unused request ids out of the batch in one step, returning
// them (and false when fewer than n are available or the batch is not currently
// usable). It advances the batch's counter, so a second Reserve over the same
// batch can never hand back an id the first one already owns. Authorize uses it
// to make reuse of a cached batch a real reservation rather than a look at
// Remaining, which two callers could both pass before either consumed an id.
func (b *AuthorizationBatch) Reserve(n int) ([]string, bool) {
	if n <= 0 {
		return nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.next+n > len(b.RequestIDs) {
		return nil, false
	}
	now := b.now()
	if now.Before(b.NotBefore.Time) || !now.Before(b.ExpiresAt.Time) {
		return nil, false
	}
	ids := append([]string(nil), b.RequestIDs[b.next:b.next+n]...)
	b.next += n
	return ids, true
}

// grant returns a view of the batch that carries only the ids reserved for one
// caller. The shared batch's counter has already advanced, so two grants over
// one batch hand out disjoint id sets. The view keeps the assertion, the
// timestamps and the clock, so Take on it refuses outside the same window.
func (b *AuthorizationBatch) grant(ids []string) *AuthorizationBatch {
	return &AuthorizationBatch{
		EntitlementID: b.EntitlementID,
		Assertion:     b.Assertion,
		RequestIDs:    ids,
		IssuedAt:      b.IssuedAt,
		ExpiresAt:     b.ExpiresAt,
		ExpiresIn:     b.ExpiresIn,
		NotBefore:     b.NotBefore,
		KeyID:         b.KeyID,
		clock:         b.clock,
	}
}

// now returns the batch's clock, defaulting to the wall clock.
func (b *AuthorizationBatch) now() time.Time {
	if b.clock != nil {
		return b.clock()
	}
	return time.Now()
}

// Remaining reports how many pre-issued calls are still unused and unreserved.
func (b *AuthorizationBatch) Remaining() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.RequestIDs) - b.next
}

// Expired reports whether the batch's validity window has closed.
func (b *AuthorizationBatch) Expired(now time.Time) bool {
	return !now.Before(b.ExpiresAt.Time)
}

// cachedBatch is one authorization batch retained for the outage policy.
type cachedBatch struct {
	batch     *AuthorizationBatch
	expiresAt time.Time
}

// issuedRequest records which entitlement a request_id came from and when the
// id stops being acceptable for usage reporting.
type issuedRequest struct {
	entitlementID string
	expiresAt     time.Time
}

// inProductState is the client's process-local state for the in-product flow:
// registration identity, the current authorization batch per entitlement,
// pre-issued request ids, the reported/queued set, and the usage replay queue.
// It is process-local by design: the batch lives at most 60 seconds, and a
// restart must ask the market for a new one.
type inProductState struct {
	mu            sync.Mutex
	now           func() time.Time
	batchLimit    int
	registrations map[string]*Registration
	batches       map[string]*cachedBatch
	issued        map[string]issuedRequest
	reported      map[string]struct{}
	pendingIDs    map[string]struct{}
	pending       map[string][]UsageReport
}

func newInProductState(now func() time.Time, batchLimit int) *inProductState {
	return &inProductState{
		now:           now,
		batchLimit:    batchLimit,
		registrations: make(map[string]*Registration),
		batches:       make(map[string]*cachedBatch),
		issued:        make(map[string]issuedRequest),
		reported:      make(map[string]struct{}),
		pendingIDs:    make(map[string]struct{}),
		pending:       make(map[string][]UsageReport),
	}
}

func batchKey(issuer, entitlementID string) string {
	return issuer + "\x00" + entitlementID
}

func requestKey(issuer, requestID string) string {
	return issuer + "\x00" + requestID
}

func (c *Client) cachedRegistration(issuer string) *Registration {
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()
	registration := c.inProduct.registrations[issuer]
	if registration == nil {
		return nil
	}
	copied := *registration
	return &copied
}

func (c *Client) storeRegistration(issuer string, registration *Registration) {
	copied := *registration
	c.inProduct.mu.Lock()
	c.inProduct.registrations[issuer] = &copied
	c.inProduct.mu.Unlock()
}

func (c *Client) registeredInstanceID(issuer string) string {
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()
	if registration := c.inProduct.registrations[issuer]; registration != nil {
		return registration.InstanceID
	}
	return ""
}

// Authorize asks the market for an assertion and up to calls pre-issued
// request_ids (POST /v1/entitlements/{id}/authorize). calls <= 0 asks for a
// full 20-call batch.
//
// A live cached batch is reserved, not merely re-examined: when it still holds
// at least calls unused ids, those ids are taken out of the shared batch under
// its mutex and returned in a grant view. Two concurrent authorizations
// therefore never receive the same request_id, and a hot path (one
// authorization per outbound call) does not buy a batch per call. Only when the
// cache is empty, expired or too small for the request is a new batch minted.
//
// When the market is unreachable or rate-limiting, a still-valid cached batch
// is returned instead. If the cached batch is empty or expired the caller gets
// ErrBatchExhausted / ErrBatchExpired: the instance has no attributable call
// left and must not silently serve one.
func (c *Client) Authorize(ctx context.Context, issuer, entitlementID string, calls int) (*AuthorizationBatch, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	entitlementID = strings.TrimSpace(entitlementID)
	if entitlementID == "" {
		return nil, ErrEntitlementInactive("entitlement id is required")
	}
	limit := c.batchLimit()
	if calls <= 0 {
		calls = limit
	}
	if calls > limit {
		return nil, ErrBatchTooLarge(calls, limit)
	}
	if batch := c.reserveFromCachedBatch(base, entitlementID, calls); batch != nil {
		return batch, nil
	}

	payload := authorizeRequest{
		Calls:      calls,
		OriginID:   c.originID(),
		InstanceID: c.registeredInstanceID(base),
	}
	endpoint := base + fmt.Sprintf(pathEntitlementAuthorize, url.PathEscape(entitlementID))
	res, err := c.call(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return c.fallbackBatch(base, entitlementID, err)
	}

	switch {
	case res.ok():
	case res.StatusCode == http.StatusUnauthorized ||
		res.StatusCode == http.StatusForbidden ||
		res.StatusCode == http.StatusNotFound ||
		res.StatusCode == http.StatusGone ||
		res.StatusCode == http.StatusConflict ||
		res.StatusCode == http.StatusBadRequest ||
		res.StatusCode == http.StatusUnprocessableEntity:
		c.dropBatch(base, entitlementID)
		return nil, ErrEntitlementInactive(fmt.Sprintf("market authorize returned HTTP %d", res.StatusCode))
	case res.StatusCode == http.StatusTooManyRequests:
		return c.fallbackBatch(base, entitlementID, ErrMarketRateLimited("authorization is rate limited"))
	case isClientRejection(res.StatusCode):
		// Any other 4xx is the market refusing to authorize this entitlement;
		// serving from a cached batch would attribute calls the market has just
		// declined, so the batch is dropped and nothing is served.
		c.dropBatch(base, entitlementID)
		return nil, ErrEntitlementInactive(fmt.Sprintf("market authorize returned HTTP %d", res.StatusCode))
	default:
		return c.fallbackBatch(base, entitlementID,
			ErrMarketUnreachable(fmt.Sprintf("market authorize returned HTTP %d", res.StatusCode)))
	}

	batch, err := decodeAuthorizationBatch(res.Body)
	if err != nil {
		return nil, err
	}
	now := c.now()
	// The assertion is the authority the seller enforces, so verify it here too
	// and check it covers what this batch hands out. An instance that skipped
	// this would serve calls, and then report usage, against an assertion the
	// seller refuses — attributing a served call to nobody.
	claims, err := c.verifyAuthorizedBatch(ctx, base, batch, entitlementID)
	if err != nil {
		return nil, err
	}
	if err := normalizeBatch(batch, entitlementID, calls, now, claims); err != nil {
		return nil, err
	}
	batch.clock = c.now
	if err := c.storeBatch(base, entitlementID, batch); err != nil {
		return nil, err
	}
	// A successful authorize is the best moment to drain deferred usage: the
	// market is demonstrably reachable again.
	if err := c.FlushUsage(ctx, base); err != nil && isRetryableMarketError(err) {
		c.log.Warn("usage replay deferred again after a successful authorize",
			"issuer", base, "error", err)
	}
	return batch, nil
}

// reserveFromCachedBatch reserves calls ids from the cached batch for this
// entitlement and returns a grant carrying exactly those ids, or nil when the
// caller must ask the market for a new batch.
//
// The reservation is the point: Remaining is only a snapshot, so two callers
// could both see "enough left" and then race for the same ids. Reserve advances
// the shared counter atomically, which turns the check into an allocation. The
// size rule stays conservative — a grant of fewer ids than asked for would make
// the caller serve un-attributable calls — and an expired or not-yet-valid
// batch is never reserved, because the seller refuses attribution for it.
func (c *Client) reserveFromCachedBatch(issuer, entitlementID string, calls int) *AuthorizationBatch {
	c.inProduct.mu.Lock()
	cached := c.inProduct.batches[batchKey(issuer, entitlementID)]
	c.inProduct.mu.Unlock()
	if cached == nil || !c.now().Before(cached.expiresAt) {
		return nil
	}
	ids, ok := cached.batch.Reserve(calls)
	if !ok {
		return nil
	}
	return cached.batch.grant(ids)
}

func (c *Client) batchLimit() int {
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()
	return c.inProduct.batchLimit
}

// authorizationBatchWire is the decoded shape of one authorize response. It is
// a separate struct from AuthorizationBatch because the response is decoded into
// it (a mutex must not be copied) and because the market names the assertion
// `auth` in the design of record (§8.1) while older/other spellings use
// `assertion`; both are accepted, `auth` first.
type authorizationBatchWire struct {
	EntitlementID string     `json:"entitlement_id,omitempty"`
	Assertion     string     `json:"assertion,omitempty"`
	Auth          string     `json:"auth,omitempty"`
	RequestIDs    []string   `json:"request_ids"`
	IssuedAt      MarketTime `json:"issued_at,omitempty"`
	ExpiresAt     MarketTime `json:"expires_at,omitempty"`
	ExpiresIn     int64      `json:"expires_in,omitempty"`
	NotBefore     MarketTime `json:"nbf,omitempty"`
	KeyID         string     `json:"key_id,omitempty"`
}

func (w authorizationBatchWire) batch() *AuthorizationBatch {
	assertion := strings.TrimSpace(w.Auth)
	if assertion == "" {
		assertion = w.Assertion
	}
	return &AuthorizationBatch{
		EntitlementID: w.EntitlementID,
		Assertion:     assertion,
		RequestIDs:    w.RequestIDs,
		IssuedAt:      w.IssuedAt,
		ExpiresAt:     w.ExpiresAt,
		ExpiresIn:     w.ExpiresIn,
		NotBefore:     w.NotBefore,
		KeyID:         w.KeyID,
	}
}

func decodeAuthorizationBatch(body []byte) (*AuthorizationBatch, error) {
	var direct authorizationBatchWire
	if err := json.Unmarshal(body, &direct); err != nil {
		return nil, ErrMarketUnreachable("market returned an invalid authorization batch")
	}
	batch := direct.batch()
	if batch.Assertion == "" || len(batch.RequestIDs) == 0 {
		// Some markets wrap the batch in a named envelope. Try those before
		// deciding the response is unusable.
		var envelope struct {
			Batch         *authorizationBatchWire `json:"batch"`
			Authorization *authorizationBatchWire `json:"authorization"`
			Authorize     *authorizationBatchWire `json:"authorize"`
		}
		if err := json.Unmarshal(body, &envelope); err == nil {
			for _, candidate := range []*authorizationBatchWire{envelope.Batch, envelope.Authorization, envelope.Authorize} {
				if candidate == nil {
					continue
				}
				if wrapped := candidate.batch(); wrapped.Assertion != "" {
					batch = wrapped
					break
				}
			}
		}
	}
	if strings.TrimSpace(batch.Assertion) == "" {
		return nil, ErrMarketUnreachable("market returned an authorization batch with no assertion")
	}
	return batch, nil
}

// verifyAuthorizedBatch verifies the batch assertion offline and checks that it
// covers what the batch hands out: a call is only attributable if the assertion
// the seller verifies authorizes that request_id and that entitlement.
//
// No audience is passed: the assertion's audience is the seller's, which the
// instance cannot know, so requiring one here would refuse every valid batch.
// The claim that matters locally is the id set and the entitlement.
func (c *Client) verifyAuthorizedBatch(ctx context.Context, base string, batch *AuthorizationBatch, entitlementID string) (*AssertionClaims, error) {
	claims, err := c.VerifyAssertion(ctx, base, batch.Assertion)
	if err != nil {
		return nil, err
	}
	if claims.EntitlementID != "" && claims.EntitlementID != entitlementID {
		return nil, ErrAssertionInvalid(fmt.Sprintf(
			"assertion is for entitlement %q but the batch authorizes %q", claims.EntitlementID, entitlementID))
	}
	if len(claims.RequestIDs) == 0 {
		return claims, nil
	}
	authorized := make(map[string]struct{}, len(claims.RequestIDs))
	for _, id := range claims.RequestIDs {
		authorized[strings.TrimSpace(id)] = struct{}{}
	}
	for _, id := range batch.RequestIDs {
		if _, ok := authorized[strings.TrimSpace(id)]; !ok {
			return nil, ErrAssertionInvalid(fmt.Sprintf(
				"assertion does not authorize request id %q", id))
		}
	}
	return claims, nil
}

// normalizeBatch enforces the 20-call / 60-second contract and fills in the
// timestamps the market left implicit.
func normalizeBatch(batch *AuthorizationBatch, entitlementID string, requested int, now time.Time, claims *AssertionClaims) error {
	if strings.TrimSpace(batch.Assertion) == "" {
		return ErrAssertionInvalid("market returned an empty assertion")
	}
	if len(batch.RequestIDs) == 0 {
		return ErrAssertionInvalid("market returned no pre-issued request ids")
	}
	if len(batch.RequestIDs) > requested || len(batch.RequestIDs) > MaxBatchCalls {
		return ErrBatchTooLarge(len(batch.RequestIDs), requested)
	}
	seen := make(map[string]struct{}, len(batch.RequestIDs))
	for i, id := range batch.RequestIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return ErrAssertionInvalid("market returned an empty request id")
		}
		if _, dup := seen[id]; dup {
			return ErrAssertionInvalid("market returned a duplicate request id")
		}
		seen[id] = struct{}{}
		batch.RequestIDs[i] = id
	}
	if batch.EntitlementID == "" {
		batch.EntitlementID = entitlementID
	}
	issuedAt := batch.IssuedAt.Time
	if issuedAt.IsZero() {
		issuedAt = now
	}
	expiresAt := batch.ExpiresAt.Time
	if expiresAt.IsZero() && batch.ExpiresIn > 0 {
		expiresAt = issuedAt.Add(time.Duration(batch.ExpiresIn) * time.Second)
	}
	if expiresAt.IsZero() {
		expiresAt = now.Add(MaxBatchTTL)
	}
	// The assertion is what the seller checks on every call, so a batch must not
	// outlive it: an id handed out after the assertion expired buys a call the
	// seller will refuse to attribute.
	if claims != nil && claims.ExpiresAt > 0 {
		if assertionExpiry := time.Unix(claims.ExpiresAt, 0); expiresAt.After(assertionExpiry) {
			expiresAt = assertionExpiry
		}
	}
	// Local hard ceiling: never trust a batch for more than 60 seconds from now.
	if maxExpiry := now.Add(MaxBatchTTL); expiresAt.After(maxExpiry) {
		expiresAt = maxExpiry
	}
	// An assertion that is not yet valid (its nbf is in the future, within the
	// clock-skew leeway verification allows) is retained but not servable: an id
	// handed out before the seller accepts the assertion buys a call nobody can
	// attribute, and its usage report is then refused as rejected. The batch
	// carries the instant it becomes usable, and Take refuses before it.
	notBefore := batch.NotBefore.Time
	if claims != nil && claims.NotBefore > 0 {
		if claimNotBefore := time.Unix(claims.NotBefore, 0); claimNotBefore.After(notBefore) {
			notBefore = claimNotBefore
		}
	}
	if !notBefore.IsZero() && !notBefore.Before(expiresAt) {
		return ErrAssertionInvalid("assertion is not valid before its expiry; no call could be attributed")
	}
	if !now.Before(expiresAt) {
		return ErrBatchExpired("market issued an authorization batch that has already expired")
	}
	batch.IssuedAt = MarketTime{Time: issuedAt}
	batch.ExpiresAt = MarketTime{Time: expiresAt}
	batch.NotBefore = MarketTime{Time: notBefore}
	batch.mu.Lock()
	batch.next = 0
	batch.mu.Unlock()
	return nil
}

// storeBatch caches the batch and records the ids it pre-issued. A market that
// re-issues an id this instance already reported, or already has queued for
// replay, cannot be attributed: the second call would be reported against an id
// the market has seen, so the report is refused and the served call is lost. The
// batch is refused instead, before any id is handed out, and the previous cached
// batch (if any) stays in place for the outage policy.
func (c *Client) storeBatch(issuer, entitlementID string, batch *AuthorizationBatch) error {
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()
	for _, requestID := range batch.RequestIDs {
		key := requestKey(issuer, requestID)
		if _, reported := c.inProduct.reported[key]; reported {
			return ErrAssertionInvalid(fmt.Sprintf(
				"market re-issued request id %q, which this instance already reported; attribution would be lost",
				requestID))
		}
		if _, queued := c.inProduct.pendingIDs[key]; queued {
			return ErrAssertionInvalid(fmt.Sprintf(
				"market re-issued request id %q, which is still queued for replay; attribution would be lost",
				requestID))
		}
	}
	c.inProduct.batches[batchKey(issuer, entitlementID)] = &cachedBatch{
		batch:     batch,
		expiresAt: batch.ExpiresAt.Time,
	}
	for _, requestID := range batch.RequestIDs {
		c.inProduct.issued[requestKey(issuer, requestID)] = issuedRequest{
			entitlementID: entitlementID,
			expiresAt:     batch.ExpiresAt.Time,
		}
	}
	return nil
}

func (c *Client) dropBatch(issuer, entitlementID string) {
	c.inProduct.mu.Lock()
	delete(c.inProduct.batches, batchKey(issuer, entitlementID))
	c.inProduct.mu.Unlock()
}

// fallbackBatch implements the outage policy shared by the in-product path:
// keep serving from a still-valid cached batch, say so in the log, and refuse
// closed once the batch is expired or exhausted.
func (c *Client) fallbackBatch(issuer, entitlementID string, cause error) (*AuthorizationBatch, error) {
	now := c.now()
	c.inProduct.mu.Lock()
	cached := c.inProduct.batches[batchKey(issuer, entitlementID)]
	c.inProduct.mu.Unlock()

	if cached == nil {
		return nil, cause
	}
	if now.Before(cached.expiresAt) && cached.batch.Remaining() > 0 {
		c.log.Warn("market unavailable; serving cached authorization batch",
			"issuer", issuer,
			"entitlement_id", entitlementID,
			"remaining_calls", cached.batch.Remaining(),
			"expires_at", cached.expiresAt.UTC(),
			"error", cause)
		return cached.batch, nil
	}
	if now.Before(cached.expiresAt) {
		return nil, ErrBatchExhausted(fmt.Sprintf("cached batch has no calls left; market is unavailable: %v", cause))
	}
	c.log.Warn("cached authorization batch expired; refusing to serve unattributed calls",
		"issuer", issuer,
		"entitlement_id", entitlementID,
		"expired_at", cached.expiresAt.UTC(),
		"error", cause)
	return nil, ErrBatchExpired(fmt.Sprintf("cached batch expired at %s; market is unavailable: %v",
		cached.expiresAt.UTC().Format(time.RFC3339), cause))
}

// reserveUsage validates a report's request_id against the ids the market
// pre-issued and marks it in flight. Unknown or repeated ids are refused before
// any outbound request, which is the client half of the market's
// fabricated/duplicated-usage control.
func (c *Client) reserveUsage(issuer string, report UsageReport) (string, error) {
	key := requestKey(issuer, report.RequestID)
	now := c.now()
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()

	issued, ok := c.inProduct.issued[key]
	if !ok {
		if _, reported := c.inProduct.reported[key]; reported {
			return "", ErrUsageRequestRepeated(report.RequestID)
		}
		return "", ErrUsageRequestUnknown(report.RequestID)
	}
	if !now.Before(issued.expiresAt) {
		return "", ErrBatchExpired(fmt.Sprintf("usage request id expired at %s",
			issued.expiresAt.UTC().Format(time.RFC3339)))
	}
	if _, done := c.inProduct.reported[key]; done {
		return "", ErrUsageRequestRepeated(report.RequestID)
	}
	if _, queued := c.inProduct.pendingIDs[key]; queued {
		return "", ErrUsageRequestRepeated(report.RequestID)
	}
	if report.EntitlementID != "" && report.EntitlementID != issued.entitlementID {
		return "", ErrUsageRequestUnknown("request id belongs to a different entitlement")
	}
	c.inProduct.pendingIDs[key] = struct{}{}
	return issued.entitlementID, nil
}

func (c *Client) deferUsage(issuer string, report UsageReport, _ error) {
	c.inProduct.mu.Lock()
	c.inProduct.pending[issuer] = append(c.inProduct.pending[issuer], report)
	c.inProduct.mu.Unlock()
}

func (c *Client) releaseUsage(issuer, requestID string) {
	c.inProduct.mu.Lock()
	delete(c.inProduct.pendingIDs, requestKey(issuer, requestID))
	c.inProduct.mu.Unlock()
}

func (c *Client) markUsageReported(issuer, requestID string) {
	key := requestKey(issuer, requestID)
	c.inProduct.mu.Lock()
	delete(c.inProduct.pendingIDs, key)
	c.inProduct.reported[key] = struct{}{}
	c.inProduct.mu.Unlock()
}

func (c *Client) takePendingUsage(issuer string) []UsageReport {
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()
	pending := c.inProduct.pending[issuer]
	if len(pending) == 0 {
		return nil
	}
	c.inProduct.pending[issuer] = nil
	return pending
}

func (c *Client) requeuePendingUsage(issuer string, reports []UsageReport) {
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()
	c.inProduct.pending[issuer] = append(append([]UsageReport(nil), reports...), c.inProduct.pending[issuer]...)
}

// newOriginID generates an opaque, stable-for-this-process instance origin id.
func newOriginID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand failing is not recoverable; the caller will surface the
		// registration failure rather than silently inventing a weak id.
		return ""
	}
	return "inst_" + hex.EncodeToString(raw[:])
}
