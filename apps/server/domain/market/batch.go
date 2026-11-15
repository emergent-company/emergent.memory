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
// longer-lived batch cannot widen the attribution window.
type AuthorizationBatch struct {
	EntitlementID string     `json:"entitlement_id,omitempty"`
	Assertion     string     `json:"assertion"`
	RequestIDs    []string   `json:"request_ids"`
	IssuedAt      MarketTime `json:"issued_at,omitempty"`
	ExpiresAt     MarketTime `json:"expires_at,omitempty"`
	ExpiresIn     int64      `json:"expires_in,omitempty"`
	KeyID         string     `json:"key_id,omitempty"`

	mu   sync.Mutex
	next int
}

// Take consumes and returns the next unused request id. It returns false when
// the batch is exhausted.
func (b *AuthorizationBatch) Take() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.next >= len(b.RequestIDs) {
		return "", false
	}
	id := b.RequestIDs[b.next]
	b.next++
	return id, true
}

// Remaining reports how many pre-issued calls are still unused.
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
	default:
		return c.fallbackBatch(base, entitlementID,
			ErrMarketUnreachable(fmt.Sprintf("market authorize returned HTTP %d", res.StatusCode)))
	}

	batch, err := decodeAuthorizationBatch(res.Body)
	if err != nil {
		return nil, err
	}
	now := c.now()
	if err := normalizeBatch(batch, entitlementID, calls, now); err != nil {
		return nil, err
	}
	c.storeBatch(base, entitlementID, batch)
	// A successful authorize is the best moment to drain deferred usage: the
	// market is demonstrably reachable again.
	if err := c.FlushUsage(ctx, base); err != nil && isRetryableMarketError(err) {
		c.log.Warn("usage replay deferred again after a successful authorize",
			"issuer", base, "error", err)
	}
	return batch, nil
}

func (c *Client) batchLimit() int {
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()
	return c.inProduct.batchLimit
}

func decodeAuthorizationBatch(body []byte) (*AuthorizationBatch, error) {
	var direct AuthorizationBatch
	if err := json.Unmarshal(body, &direct); err == nil && (direct.Assertion != "" || len(direct.RequestIDs) > 0) {
		return &direct, nil
	}
	var envelope struct {
		Batch         *AuthorizationBatch `json:"batch"`
		Authorization *AuthorizationBatch `json:"authorization"`
		Authorize     *AuthorizationBatch `json:"authorize"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		for _, candidate := range []*AuthorizationBatch{envelope.Batch, envelope.Authorization, envelope.Authorize} {
			if candidate != nil {
				return candidate, nil
			}
		}
	}
	if err := json.Unmarshal(body, &direct); err != nil {
		return nil, ErrMarketUnreachable("market returned an invalid authorization batch")
	}
	return nil, ErrMarketUnreachable("market returned an authorization batch with no assertion")
}

// normalizeBatch enforces the 20-call / 60-second contract and fills in the
// timestamps the market left implicit.
func normalizeBatch(batch *AuthorizationBatch, entitlementID string, requested int, now time.Time) error {
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
	// Local hard ceiling: never trust a batch for more than 60 seconds from now.
	if maxExpiry := now.Add(MaxBatchTTL); expiresAt.After(maxExpiry) {
		expiresAt = maxExpiry
	}
	if !now.Before(expiresAt) {
		return ErrBatchExpired("market issued an authorization batch that has already expired")
	}
	batch.IssuedAt = MarketTime{Time: issuedAt}
	batch.ExpiresAt = MarketTime{Time: expiresAt}
	batch.mu.Lock()
	batch.next = 0
	batch.mu.Unlock()
	return nil
}

func (c *Client) storeBatch(issuer, entitlementID string, batch *AuthorizationBatch) {
	c.inProduct.mu.Lock()
	defer c.inProduct.mu.Unlock()
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
