package market

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/version"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// InProductConfig configures the in-product market client. A zero value is a
// stock instance: the public market is used and the instance announces a
// process-generated origin_id.
type InProductConfig struct {
	// Issuer is the market origin. Empty uses DefaultIssuer.
	Issuer string
	// Client overrides the transport and issuer allowlist (tests, or a caller
	// that already shares one client). When nil, a Client trusting Issuer (or
	// DefaultIssuer) is built.
	Client *Client
	// AccountToken is the market account credential. When empty, the token the
	// market returns from registration is used.
	AccountToken string
	// OriginID is this instance's stable origin identity. Empty generates one
	// for the process; a product caller should persist and reuse it.
	OriginID string
	// InstanceName is the name announced at registration. Empty uses "Memory".
	InstanceName string
	// MemoryVersion is the version announced at registration. Empty uses the
	// running build version.
	MemoryVersion string
	// Endpoint is the instance's public URL, announced at registration.
	Endpoint string
	// Now overrides the clock (tests). When nil the client clock is used.
	Now func() time.Time
	// Log receives outage and attribution warnings. Nil uses slog.Default.
	Log *slog.Logger
}

// InProductClient calls the market from inside the product: it registers the
// instance, browses and verifies a listing, claims a free entitlement, authorizes
// a call batch, presents the assertion to a seller's MCP endpoint and reports
// usage against the pre-issued request ids.
//
// A batch is cached per entitlement for up to MaxBatchTTL, so the instance keeps
// serving calls while the market is unreachable. Usage reports that cannot be
// delivered are queued and logged; FlushUsage sends them when the market
// returns.
type InProductClient struct {
	issuer          string
	client          *Client
	originID        string
	instanceName    string
	memoryVersion   string
	endpoint        string
	accountToken    string
	now             func() time.Time
	log             *slog.Logger
	batches         *batchCache
	registrationMu  sync.Mutex
	mu              sync.Mutex
	registered      bool
	instanceID      string
	token           string
	pendingMu       sync.Mutex
	pending         []UsageReport
	pendingRequests map[string]struct{}
}

// NewInProductClient builds the in-product market client. Every configured
// issuer must be an absolute http(s) origin and, when a Client is supplied, must
// already be trusted by it.
func NewInProductClient(cfg InProductConfig) (*InProductClient, error) {
	issuer := strings.TrimSpace(cfg.Issuer)
	if issuer == "" {
		issuer = DefaultIssuer
	}
	normalized, err := NormalizeIssuer(issuer)
	if err != nil {
		return nil, err
	}
	client := cfg.Client
	if client == nil {
		client, err = NewClient(ClientConfig{Issuers: []string{normalized}, Now: cfg.Now})
		if err != nil {
			return nil, err
		}
	}
	if !client.IsTrusted(normalized) {
		return nil, fmt.Errorf("market issuer %q is not trusted by the supplied client", normalized)
	}
	now := cfg.Now
	if now == nil {
		now = client.now
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	name := strings.TrimSpace(cfg.InstanceName)
	if name == "" {
		name = "Memory"
	}
	memoryVersion := strings.TrimSpace(cfg.MemoryVersion)
	if memoryVersion == "" {
		memoryVersion = version.Version
	}
	originID := strings.TrimSpace(cfg.OriginID)
	if originID == "" {
		originID = uuid.NewString()
	}
	return &InProductClient{
		issuer:          normalized,
		client:          client,
		originID:        originID,
		instanceName:    name,
		memoryVersion:   memoryVersion,
		endpoint:        strings.TrimSpace(cfg.Endpoint),
		accountToken:    strings.TrimSpace(cfg.AccountToken),
		now:             now,
		log:             log.With(logger.Scope("market.inproduct")),
		batches:         newBatchCache(now),
		pendingRequests: make(map[string]struct{}),
	}, nil
}

// Issuer returns the normalized market origin.
func (c *InProductClient) Issuer() string { return c.issuer }

// OriginID returns the instance identity announced at registration.
func (c *InProductClient) OriginID() string { return c.originID }

// InstanceID returns the market-assigned instance id, or "" before registration.
func (c *InProductClient) InstanceID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.instanceID
}

// Registered reports whether the instance has announced itself to the market.
func (c *InProductClient) Registered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.registered
}

// PendingUsage returns the number of queued usage reports awaiting delivery.
func (c *InProductClient) PendingUsage() int {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	return len(c.pending)
}

// VerifyAssertion verifies one of this client's market assertions. Callers that
// only need the seller-side contract can use (*Client).VerifyAssertion directly.
func (c *InProductClient) VerifyAssertion(ctx context.Context, token string) (*AssertionClaims, error) {
	return c.client.VerifyAssertion(ctx, c.issuer, token)
}

// Register announces the instance and its origin_id. It is idempotent: a second
// call after a successful registration is a no-op.
func (c *InProductClient) Register(ctx context.Context) error {
	return c.ensureRegistered(ctx)
}

func (c *InProductClient) ensureRegistered(ctx context.Context) error {
	c.registrationMu.Lock()
	defer c.registrationMu.Unlock()
	c.mu.Lock()
	registered := c.registered
	c.mu.Unlock()
	if registered {
		return nil
	}
	request := RegisterRequest{
		OriginID: c.originID,
		Name:     c.instanceName,
		Version:  c.memoryVersion,
		Endpoint: c.endpoint,
	}
	body, status, err := c.inProductDo(ctx, http.MethodPost, c.issuer+"/v1/instances", request)
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrMarketUnauthorized("market refused to register this instance")
	case status == http.StatusTooManyRequests:
		return ErrMarketRateLimited("instance registration is rate limited")
	case status < 200 || status >= 300:
		return ErrMarketUnreachable(fmt.Sprintf("market registration returned HTTP %d", status))
	}
	var registration Registration
	if err := json.Unmarshal(body, &registration); err != nil {
		return ErrMarketUnreachable("market returned an invalid registration document")
	}
	if registration.InstanceID == "" {
		var envelope struct {
			Instance Registration `json:"instance"`
		}
		if err := json.Unmarshal(body, &envelope); err == nil {
			registration = envelope.Instance
		}
	}
	if registration.InstanceID == "" {
		return ErrMarketUnreachable("market registration is missing the instance id")
	}
	if registration.OriginID == "" {
		registration.OriginID = c.originID
	}
	if registration.OriginID != c.originID {
		return ErrMarketUnreachable("market registered a different origin_id")
	}
	c.mu.Lock()
	c.registered = true
	c.instanceID = registration.InstanceID
	if registration.Token != "" && c.accountToken == "" {
		c.token = registration.Token
	}
	c.mu.Unlock()
	return nil
}

// Browse searches the catalog along the kind × delivery axes. Results are
// filtered once more on the instance side, so a market that ignores a filter
// cannot present a listing on the wrong axis or a paid listing as free.
func (c *InProductClient) Browse(ctx context.Context, query BrowseQuery) (*Catalog, error) {
	params := url.Values{}
	if value := strings.TrimSpace(query.Kind); value != "" {
		params.Set("kind", value)
	}
	if value := strings.TrimSpace(query.Delivery); value != "" {
		params.Set("delivery", value)
	}
	if value := strings.TrimSpace(query.Search); value != "" {
		params.Set("q", value)
	}
	if query.FreeOnly {
		params.Set("free", "true")
	}
	if query.Limit > 0 {
		params.Set("limit", strconv.Itoa(query.Limit))
	}
	if value := strings.TrimSpace(query.Cursor); value != "" {
		params.Set("cursor", value)
	}
	endpoint := c.issuer + "/v1/catalog"
	if encoded := params.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	body, status, err := c.inProductDo(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, ErrMarketUnauthorized("market refused the catalog request")
	case status == http.StatusTooManyRequests:
		return nil, ErrMarketRateLimited("market catalog search is rate limited")
	case status < 200 || status >= 300:
		return nil, ErrMarketUnreachable(fmt.Sprintf("market catalog returned HTTP %d", status))
	}
	var catalog Catalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		var listings []Listing
		if err := json.Unmarshal(body, &listings); err != nil {
			return nil, ErrMarketUnreachable("market returned an invalid catalog document")
		}
		catalog.Listings = listings
	}
	return filterCatalog(&catalog, query), nil
}

func filterCatalog(catalog *Catalog, query BrowseQuery) *Catalog {
	if catalog == nil {
		return nil
	}
	filtered := &Catalog{NextCursor: catalog.NextCursor}
	for _, listing := range catalog.Listings {
		if kind := strings.TrimSpace(query.Kind); kind != "" && !strings.EqualFold(listing.Kind, kind) {
			continue
		}
		if delivery := strings.TrimSpace(query.Delivery); delivery != "" && !strings.EqualFold(listing.Delivery, delivery) {
			continue
		}
		if query.FreeOnly && !listing.IsFree() {
			continue
		}
		filtered.Listings = append(filtered.Listings, listing)
	}
	return filtered
}

// VerifyTrust fetches and verifies a listing's signed trust document before the
// instance offers the listing.
func (c *InProductClient) VerifyTrust(ctx context.Context, slug string) (*TrustClaims, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, ErrListingUntrusted("listing slug is required")
	}
	endpoint := c.issuer + "/v1/listings/" + url.PathEscape(slug) + "/trust"
	body, status, err := c.inProductDo(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusNotFound:
		return nil, ErrListingUntrusted("listing trust document was not found")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, ErrMarketUnauthorized("market refused the trust document request")
	case status == http.StatusTooManyRequests:
		return nil, ErrMarketRateLimited("market trust document request is rate limited")
	case status < 200 || status >= 300:
		return nil, ErrMarketUnreachable(fmt.Sprintf("market trust document returned HTTP %d", status))
	}
	token := extractSignedDocument(body)
	if token == "" {
		return nil, ErrListingUntrusted("market returned no trust document")
	}
	claims, err := c.client.VerifyTrustToken(ctx, c.issuer, token)
	if err != nil {
		return nil, err
	}
	if claims.Slug() != slug {
		return nil, ErrListingUntrusted("trust document names a different listing")
	}
	return claims, nil
}

// Claim takes a free entitlement for this instance's account and the listing. A
// claim that is already held is an ordinary success: the market's existing
// entitlement is returned with AlreadyHeld set.
func (c *InProductClient) Claim(ctx context.Context, slug string) (*Entitlement, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, ErrEntitlementClaim("listing slug is required")
	}
	if err := c.ensureRegistered(ctx); err != nil {
		return nil, err
	}
	request := claimRequest{ListingSlug: slug, OriginID: c.originID}
	body, status, err := c.inProductDo(ctx, http.MethodPost, c.issuer+"/v1/entitlements/claim", request)
	if err != nil {
		return nil, err
	}
	switch {
	case status >= 200 && status < 300:
		return decodeEntitlement(body, slug, c.originID)
	case status == http.StatusConflict:
		code, message := marketErrorFields(body)
		entitlement, _ := decodeEntitlement(body, slug, c.originID)
		if code == CodeEntitlementAlreadyHeld || entitlement != nil {
			if entitlement == nil {
				return nil, ErrEntitlementClaim(firstNonEmptyString(message, "market said the entitlement is already held but returned no entitlement"))
			}
			entitlement.AlreadyHeld = true
			return entitlement, nil
		}
		return nil, ErrEntitlementClaim(firstNonEmptyString(message, "market refused the entitlement claim"))
	case status == http.StatusNotFound:
		return nil, ErrEntitlementClaim("listing is not available for this account")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, ErrMarketUnauthorized("market refused the entitlement claim")
	case status == http.StatusTooManyRequests:
		return nil, ErrMarketRateLimited("entitlement claim is rate limited")
	default:
		return nil, ErrMarketUnreachable(fmt.Sprintf("market entitlement claim returned HTTP %d", status))
	}
}

func decodeEntitlement(body []byte, slug, originID string) (*Entitlement, error) {
	var direct Entitlement
	if err := json.Unmarshal(body, &direct); err == nil && direct.ID != "" {
		fillEntitlement(&direct, slug, originID)
		return &direct, nil
	}
	var envelope struct {
		Entitlement   *Entitlement `json:"entitlement"`
		EntitlementID string       `json:"entitlement_id"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Entitlement != nil && envelope.Entitlement.ID != "" {
			fillEntitlement(envelope.Entitlement, slug, originID)
			return envelope.Entitlement, nil
		}
		if id := strings.TrimSpace(envelope.EntitlementID); id != "" {
			return &Entitlement{ID: id, ListingSlug: slug, OriginID: originID}, nil
		}
	}
	return nil, ErrEntitlementClaim("market returned an invalid entitlement document")
}

func fillEntitlement(entitlement *Entitlement, slug, originID string) {
	if entitlement.ListingSlug == "" {
		entitlement.ListingSlug = slug
	}
	if entitlement.OriginID == "" {
		entitlement.OriginID = originID
	}
}

// Authorize asks the market for a batch of pre-issued request ids and a single
// ES256 assertion covering them. The batch is bounded to MaxBatchCalls calls and
// MaxBatchTTL, cached immediately, and its assertion is verified before use.
func (c *InProductClient) Authorize(ctx context.Context, entitlementID string, calls int) (*Batch, error) {
	entitlementID = strings.TrimSpace(entitlementID)
	if entitlementID == "" {
		return nil, ErrAuthorizationInvalid("entitlement id is required")
	}
	if calls < 1 || calls > MaxBatchCalls {
		return nil, ErrAuthorizationInvalid(fmt.Sprintf("calls must be between 1 and %d", MaxBatchCalls))
	}
	if err := c.ensureRegistered(ctx); err != nil {
		return nil, err
	}
	request := authorizeRequest{
		OriginID:   c.originID,
		Calls:      calls,
		TTLSeconds: int(MaxBatchTTL / time.Second),
		Audience:   AssertionAudience,
	}
	endpoint := c.issuer + "/v1/entitlements/" + url.PathEscape(entitlementID) + "/authorize"
	body, status, err := c.inProductDo(ctx, http.MethodPost, endpoint, request)
	if err != nil {
		return nil, err
	}
	switch {
	case status >= 200 && status < 300:
	case status == http.StatusNotFound:
		return nil, ErrAuthorizationInvalid("market does not know this entitlement")
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, ErrMarketUnauthorized("market refused the call authorization")
	case status == http.StatusTooManyRequests:
		return nil, ErrMarketRateLimited("call authorization is rate limited")
	default:
		return nil, ErrMarketUnreachable(fmt.Sprintf("market authorization returned HTTP %d", status))
	}
	var response authorizeResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, ErrAuthorizationInvalid("market returned an invalid authorization document")
	}
	if strings.TrimSpace(response.Assertion) == "" {
		return nil, ErrAuthorizationInvalid("market returned no assertion")
	}
	claims, err := c.VerifyAssertion(ctx, response.Assertion)
	if err != nil {
		return nil, err
	}
	if claims.EntitlementID != entitlementID {
		return nil, ErrAuthorizationInvalid("assertion names a different entitlement")
	}
	if len(claims.RequestIDs) != calls {
		return nil, ErrAuthorizationInvalid(fmt.Sprintf("assertion carries %d request ids, want %d", len(claims.RequestIDs), calls))
	}
	if len(response.RequestIDs) > 0 && !sameStringSet(response.RequestIDs, claims.RequestIDs) {
		return nil, ErrAuthorizationInvalid("authorization response and assertion disagree on request ids")
	}
	batch := &Batch{
		Assertion:     response.Assertion,
		RequestIDs:    claims.RequestIDs,
		EntitlementID: claims.EntitlementID,
		ListingSlug:   claims.ListingSlug,
		OriginID:      claims.OriginID,
		IssuedAt:      time.Unix(claims.IssuedAt, 0),
		ExpiresAt:     time.Unix(claims.ExpiresAt, 0),
	}
	if batch.Expired(c.now()) {
		return nil, ErrAuthorizationInvalid("market issued an already expired batch")
	}
	c.batches.put(batch)
	return batch, nil
}

// EnsureBatch returns a live batch for the entitlement, authorizing a new one
// when the cache cannot cover calls. If the market is unreachable and a cached
// batch still has request ids, the cached batch is used and the outage is
// logged; an expired or exhausted cache is refused rather than presented.
func (c *InProductClient) EnsureBatch(ctx context.Context, entitlementID string, calls int) (*Batch, error) {
	entitlementID = strings.TrimSpace(entitlementID)
	if entitlementID == "" {
		return nil, ErrAuthorizationInvalid("entitlement id is required")
	}
	if calls < 1 || calls > MaxBatchCalls {
		return nil, ErrAuthorizationInvalid(fmt.Sprintf("calls must be between 1 and %d", MaxBatchCalls))
	}
	now := c.now()
	if live := c.batches.live(entitlementID, now); live != nil && c.batches.remaining(entitlementID, now) >= calls {
		return live, nil
	}
	batch, err := c.Authorize(ctx, entitlementID, calls)
	if err == nil {
		return batch, nil
	}
	if ErrorCode(err) != CodeMarketUnreach {
		return nil, err
	}
	cached := c.batches.get(entitlementID)
	remaining := c.batches.remaining(entitlementID, now)
	switch {
	case cached != nil && remaining > 0 && !cached.Expired(now):
		c.log.Warn("market unreachable; serving from cached authorization batch",
			slog.String("entitlement_id", entitlementID),
			slog.Int("remaining", remaining),
			logger.Error(err))
		return cached, nil
	case cached != nil && cached.Expired(now):
		c.log.Warn("market unreachable and cached authorization batch expired",
			slog.String("entitlement_id", entitlementID),
			logger.Error(err))
		return nil, ErrBatchExpired(entitlementID)
	case cached != nil:
		c.log.Warn("market unreachable and cached authorization batch exhausted",
			slog.String("entitlement_id", entitlementID),
			logger.Error(err))
		return nil, ErrBatchExhausted(entitlementID)
	default:
		c.log.Warn("market unreachable and no authorization batch is cached",
			slog.String("entitlement_id", entitlementID),
			logger.Error(err))
		return nil, err
	}
}

// NextInvocation consumes one pre-issued request id and returns the assertion to
// present with it. The call is served from a cached batch when the market is
// unreachable and the batch is still valid.
func (c *InProductClient) NextInvocation(ctx context.Context, entitlementID string) (*Invocation, error) {
	batch, err := c.EnsureBatch(ctx, entitlementID, 1)
	if err != nil {
		return nil, err
	}
	requestID, err := c.batches.take(entitlementID, c.now())
	if err != nil {
		return nil, err
	}
	return &Invocation{
		Assertion:     batch.Assertion,
		RequestID:     requestID,
		EntitlementID: batch.EntitlementID,
		ListingSlug:   batch.ListingSlug,
		ExpiresAt:     batch.ExpiresAt,
	}, nil
}

// AcquireFreeHosted runs the config-free product path: browse free hosted
// listings, verify each trust document, claim the first usable one and authorize
// a calls-sized batch (20 by default). It skips listings that are untrusted or
// cannot be claimed; market outages abort the search.
func (c *InProductClient) AcquireFreeHosted(ctx context.Context, query BrowseQuery, calls int) (*Acquisition, error) {
	if strings.TrimSpace(query.Kind) == "" {
		query.Kind = "mcp"
	}
	if strings.TrimSpace(query.Delivery) == "" {
		query.Delivery = "hosted"
	}
	query.FreeOnly = true
	if calls <= 0 {
		calls = MaxBatchCalls
	}
	catalog, err := c.Browse(ctx, query)
	if err != nil {
		return nil, err
	}
	if catalog == nil || len(catalog.Listings) == 0 {
		return nil, ErrListingUntrusted("no free hosted listing is available")
	}
	var lastErr error
	for index := range catalog.Listings {
		listing := &catalog.Listings[index]
		if strings.TrimSpace(listing.Slug) == "" {
			continue
		}
		if listing.SellerEndpoint() == "" {
			lastErr = ErrInstallNotInstallable("hosted listing declares no MCP endpoint")
			continue
		}
		trust, err := c.VerifyTrust(ctx, listing.Slug)
		if err != nil {
			if isInfrastructureError(err) {
				return nil, err
			}
			lastErr = err
			continue
		}
		if trust.Digest != "" && listing.Digest() != "" && CleanDigest(trust.Digest) != CleanDigest(listing.Digest()) {
			lastErr = ErrListingUntrusted("trust document names a different digest")
			continue
		}
		entitlement, err := c.Claim(ctx, listing.Slug)
		if err != nil {
			if isInfrastructureError(err) {
				return nil, err
			}
			lastErr = err
			continue
		}
		batch, err := c.Authorize(ctx, entitlement.ID, calls)
		if err != nil {
			return nil, err
		}
		return &Acquisition{
			Listing:     listing,
			Trust:       trust,
			Entitlement: entitlement,
			Batch:       batch,
		}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrListingUntrusted("no free hosted listing with a valid trust document was found")
}

// CallSeller makes one authorized call to a seller's MCP endpoint. It presents
// the batch assertion and a pre-issued request id, requires the seller to echo
// the id it served, and reports usage against that id. A failed seller call is
// returned as a SellerResponse with a non-2xx status; only transport and
// attribution-contract failures are errors.
func (c *InProductClient) CallSeller(ctx context.Context, entitlementID, endpoint string, payload []byte) (*SellerResponse, error) {
	invocation, err := c.NextInvocation(ctx, entitlementID)
	if err != nil {
		return nil, err
	}
	return c.callSeller(ctx, invocation, endpoint, payload)
}

func (c *InProductClient) callSeller(ctx context.Context, invocation *Invocation, endpoint string, payload []byte) (*SellerResponse, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return nil, ErrSellerUnreachable("seller endpoint must be an absolute http(s) URL without credentials")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, ErrSellerUnreachable("failed to build the seller request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	invocation.Apply(request)
	response, err := c.client.httpClient.Do(request)
	if err != nil {
		return nil, ErrSellerUnreachable("seller request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJSONBody+1))
	if err != nil {
		return nil, ErrSellerUnreachable("failed to read the seller response")
	}
	if len(body) > maxJSONBody {
		return nil, ErrSellerUnreachable("seller response is too large")
	}
	echoed := strings.TrimSpace(response.Header.Get(HeaderRequestID))
	if echoed == "" {
		echoed = sellerEchoedRequestID(body)
	}
	if echoed == "" {
		return nil, ErrRequestIDMismatch("seller did not echo the presented request_id")
	}
	if echoed != invocation.RequestID {
		return nil, ErrRequestIDMismatch("seller echoed a request_id the instance did not present")
	}
	outcome := UsageOutcomeOK
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		outcome = UsageOutcomeError
	}
	if err := c.ReportUsage(ctx, UsageReport{
		RequestID:     invocation.RequestID,
		EntitlementID: invocation.EntitlementID,
		ListingSlug:   invocation.ListingSlug,
		Outcome:       outcome,
	}); err != nil {
		// Attribution must never turn a served call into a failed one. The
		// report is refused (not just deferred), so log it loudly.
		c.log.Warn("market refused the usage report for a served call",
			slog.String("request_id", invocation.RequestID),
			logger.Error(err))
	}
	return &SellerResponse{
		StatusCode: response.StatusCode,
		Body:       body,
		RequestID:  echoed,
		Outcome:    outcome,
	}, nil
}

func sellerEchoedRequestID(body []byte) string {
	var envelope struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	return strings.TrimSpace(envelope.RequestID)
}

// ReportUsage posts one usage report. A request id this instance never received
// is refused locally, and a duplicate report is refused locally; both are the
// instance-side half of §8's fabricated-or-duplicated usage report control. If
// the market is unreachable (or rate limiting), the report is queued, the
// deferral is logged, and nil is returned so the served call is unaffected;
// FlushUsage delivers it later.
func (c *InProductClient) ReportUsage(ctx context.Context, report UsageReport) error {
	report.RequestID = strings.TrimSpace(report.RequestID)
	report.EntitlementID = strings.TrimSpace(report.EntitlementID)
	if report.RequestID == "" {
		return ErrUsageRequestUnknown("")
	}
	if report.EntitlementID == "" {
		return ErrUsageRequestUnknown(report.RequestID)
	}
	if report.OriginID == "" {
		report.OriginID = c.originID
	}
	if report.Outcome == "" {
		report.Outcome = UsageOutcomeOK
	}
	if err := c.batches.reserve(report.EntitlementID, report.RequestID); err != nil {
		return err
	}
	if err := c.postUsage(ctx, report); err != nil {
		if isDeferrable(err) {
			c.enqueueUsage(report)
			c.log.Warn("market unreachable; usage report deferred rather than dropped",
				slog.String("request_id", report.RequestID),
				slog.String("entitlement_id", report.EntitlementID),
				logger.Error(err))
			return nil
		}
		return err
	}
	return nil
}

// FlushUsage delivers queued usage reports. A report the market permanently
// refuses is logged and dropped; a transport/rate-limit failure leaves the rest
// queued for the next attempt. It returns how many reports were delivered.
func (c *InProductClient) FlushUsage(ctx context.Context) (int, error) {
	c.pendingMu.Lock()
	queue := append([]UsageReport(nil), c.pending...)
	c.pending = nil
	c.pendingRequests = make(map[string]struct{})
	c.pendingMu.Unlock()
	delivered := 0
	for index, report := range queue {
		if err := c.postUsage(ctx, report); err != nil {
			if isDeferrable(err) {
				c.enqueueUsage(queue[index:]...)
				c.log.Warn("market still unreachable; usage reports remain queued",
					slog.Int("pending", len(queue)-index),
					logger.Error(err))
				return delivered, err
			}
			c.log.Warn("market refused a queued usage report; dropping it",
				slog.String("request_id", report.RequestID),
				logger.Error(err))
			continue
		}
		delivered++
	}
	if delivered > 0 {
		c.log.Info("queued usage reports delivered", slog.Int("delivered", delivered))
	}
	return delivered, nil
}

func (c *InProductClient) enqueueUsage(reports ...UsageReport) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for _, report := range reports {
		if _, queued := c.pendingRequests[report.RequestID]; queued {
			continue
		}
		c.pendingRequests[report.RequestID] = struct{}{}
		c.pending = append(c.pending, report)
	}
}

func (c *InProductClient) postUsage(ctx context.Context, report UsageReport) error {
	body, status, err := c.inProductDo(ctx, http.MethodPost, c.issuer+"/v1/usage", report)
	if err != nil {
		return err
	}
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrMarketUnauthorized("market refused the usage report")
	case status == http.StatusTooManyRequests:
		return ErrMarketRateLimited("usage reporting is rate limited")
	case status == http.StatusNotFound || status == http.StatusConflict || status == http.StatusUnprocessableEntity:
		code, message := marketErrorFields(body)
		switch code {
		case CodeUsageRequestDuplicate:
			return ErrUsageRequestDuplicated(report.RequestID)
		case CodeUsageRequestUnknown:
			return ErrUsageRequestUnknown(report.RequestID)
		}
		return ErrUsageReportRejected(firstNonEmptyString(message, "market rejected the usage report"))
	default:
		return ErrMarketUnreachable(fmt.Sprintf("market usage report returned HTTP %d", status))
	}
}

func isDeferrable(err error) bool {
	code := ErrorCode(err)
	return code == CodeMarketUnreach || code == CodeMarketRateLimited
}

func isInfrastructureError(err error) bool {
	switch ErrorCode(err) {
	case CodeMarketUnreach, CodeMarketRateLimited, CodeMarketUnauthorized:
		return true
	default:
		return false
	}
}

func (c *InProductClient) marketToken() string {
	if c.accountToken != "" {
		return c.accountToken
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// inProductDo sends a market request and returns the body and status. Transport
// and 5xx failures are reported as ErrMarketUnreachable; every other non-2xx
// status is left to the caller, which maps it to the contract's named error.
func (c *InProductClient) inProductDo(ctx context.Context, method, endpoint string, payload any) ([]byte, int, error) {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, ErrMarketUnreachable("failed to encode the market request")
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, 0, ErrMarketUnreachable("failed to build the market request")
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token := c.marketToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.client.httpClient.Do(request)
	if err != nil {
		return nil, 0, ErrMarketUnreachable("market request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJSONBody+1))
	if err != nil {
		return nil, response.StatusCode, ErrMarketUnreachable("failed to read the market response")
	}
	if len(body) > maxJSONBody {
		return nil, response.StatusCode, ErrMarketUnreachable("market response is too large")
	}
	if response.StatusCode >= 500 {
		return body, response.StatusCode, ErrMarketUnreachable(fmt.Sprintf("market returned HTTP %d", response.StatusCode))
	}
	return body, response.StatusCode, nil
}
