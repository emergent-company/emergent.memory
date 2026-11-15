package market

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// In-product market endpoints. They are named here (rather than inlined at each
// call site) so a change in the market's routing is a one-line change and the
// fake market used by the tests pins the same paths.
const (
	pathInstances            = "/v1/instances"
	pathCatalog              = "/v1/catalog"
	pathEntitlements         = "/v1/entitlements"
	pathEntitlementAuthorize = "/v1/entitlements/%s/authorize"
	pathUsage                = "/v1/usage"
	pathListingTrust         = "/v1/listings/%s/trust"
)

// The authorization batch contract: at most 20 calls from one assertion, and
// the assertion is valid for at most 60 seconds. The instance enforces both
// locally even if a future market issues a longer-lived batch.
const (
	MaxBatchCalls = 20
	MaxBatchTTL   = 60 * time.Second
)

// TrustDocumentAudience is the audience a listing trust document must carry.
const TrustDocumentAudience = "memory-listing-trust"

// The browse axes from the marketplace design (§6). They are advisory: an
// unknown future value still passes through to the market rather than being
// refused by an older instance, but the two axes are always sent together.
const (
	KindBlueprint = "blueprint"
	KindAgent     = "agent"
	KindMCP       = "mcp"
	KindSkill     = "skill"

	DeliveryInstall = "install"
	DeliveryHosted  = "hosted"
	DeliveryEdge    = "edge"
)

// Usage outcomes. An empty Outcome is sent as "success".
const (
	UsageOutcomeSuccess = "success"
	UsageOutcomeError   = "error"
)

// MarketTime accepts the two timestamp spellings the market may use: an RFC3339
// string or a Unix-seconds number. It marshals as RFC3339.
type MarketTime struct {
	time.Time
}

func (t *MarketTime) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" || raw == `""` {
		t.Time = time.Time{}
		return nil
	}
	var unix int64
	if err := json.Unmarshal(data, &unix); err == nil {
		t.Time = time.Unix(unix, 0).UTC()
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("market time must be RFC3339 or unix seconds: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		t.Time = time.Time{}
		return nil
	}
	if unix, err := strconv.ParseInt(text, 10, 64); err == nil {
		t.Time = time.Unix(unix, 0).UTC()
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z0700"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			t.Time = parsed.UTC()
			return nil
		}
	}
	return fmt.Errorf("market time %q is not RFC3339", text)
}

func (t MarketTime) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Time.UTC().Format(time.RFC3339Nano))
}

// apiResult is one market HTTP response whose status the caller maps itself.
type apiResult struct {
	StatusCode int
	Body       []byte
}

func (r *apiResult) ok() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }

// call performs one JSON request and returns the raw response, including a
// non-2xx status. Transport failures become ErrMarketUnreachable.
func (c *Client) call(ctx context.Context, method, endpoint string, payload any) (*apiResult, error) {
	var bodyReader io.Reader
	if payload != nil {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, ErrMarketUnreachable("failed to build market request")
		}
		bodyReader = bytes.NewReader(payloadBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, ErrMarketUnreachable("failed to build market request")
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, ErrMarketUnreachable("market request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBody+1))
	if err != nil {
		return nil, ErrMarketUnreachable("failed to read market response")
	}
	if len(body) > maxJSONBody {
		return nil, ErrMarketUnreachable("market response is too large")
	}
	return &apiResult{StatusCode: resp.StatusCode, Body: body}, nil
}

// RegisterRequest is POST /v1/instances.
type RegisterRequest struct {
	OriginID    string `json:"origin_id"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	InstanceURL string `json:"instance_url,omitempty"`
}

// Registration is the market's acknowledgement of an instance. InstanceID is
// the identity the market uses for entitlements and assertions; it is cached by
// the client so later claims can name it without re-registering.
type Registration struct {
	InstanceID   string     `json:"instance_id"`
	OriginID     string     `json:"origin_id,omitempty"`
	RegisteredAt MarketTime `json:"registered_at,omitempty"`
	MarketIssuer string     `json:"-"`
}

// Register announces this instance and its origin_id to the market
// (POST /v1/instances). It is idempotent from the caller's perspective: a
// successful registration is cached per issuer, and a later Register call
// returns the cached identity without a second outbound request.
func (c *Client) Register(ctx context.Context, issuer string, req RegisterRequest) (*Registration, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	if cached := c.cachedRegistration(base); cached != nil {
		return cached, nil
	}
	req.OriginID = strings.TrimSpace(req.OriginID)
	if req.OriginID == "" {
		req.OriginID = c.originID()
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = c.name
	}
	if strings.TrimSpace(req.Version) == "" {
		req.Version = c.version
	}
	if strings.TrimSpace(req.InstanceURL) == "" {
		req.InstanceURL = c.instanceURL
	}

	res, err := c.call(ctx, http.MethodPost, base+pathInstances, req)
	if err != nil {
		return nil, err
	}
	switch {
	case res.ok():
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, ErrMarketRateLimited("instance registration is rate limited")
	case res.StatusCode == http.StatusConflict:
		return nil, ErrRegistrationFailed("this origin_id is already registered with different details")
	default:
		return nil, ErrRegistrationFailed(fmt.Sprintf("market registration returned HTTP %d", res.StatusCode))
	}

	registration, err := decodeRegistration(res.Body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(registration.InstanceID) == "" {
		return nil, ErrRegistrationFailed("market registration returned no instance_id")
	}
	if registration.OriginID == "" {
		registration.OriginID = req.OriginID
	}
	registration.MarketIssuer = base
	c.storeRegistration(base, registration)
	return registration, nil
}

// decodeRegistration accepts the bare object and the {"instance": ...} envelope.
func decodeRegistration(body []byte) (*Registration, error) {
	var direct Registration
	if err := json.Unmarshal(body, &direct); err == nil && direct.InstanceID != "" {
		return &direct, nil
	}
	var envelope struct {
		Instance *Registration `json:"instance"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Instance != nil {
		return envelope.Instance, nil
	}
	if err := json.Unmarshal(body, &direct); err != nil {
		return nil, ErrMarketUnreachable("market registration returned an invalid document")
	}
	return &direct, nil
}

// BrowseQuery narrows a catalog search. Kind and Delivery are the two
// independent axes (§6); the rest narrow the card text or page the result.
type BrowseQuery struct {
	Kind      string
	Delivery  string
	Text      string
	Publisher string
	Cursor    string
	Limit     int
}

// Browse fetches GET /v1/catalog with the kind × delivery axes and optional
// text/publisher/page filters. Results are also filtered locally on the two
// axes, so a market that ignores an unknown filter cannot make the instance
// offer a kind or delivery the buyer did not ask for.
func (c *Client) Browse(ctx context.Context, issuer string, query BrowseQuery) (*Catalog, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	if kind := strings.TrimSpace(query.Kind); kind != "" {
		values.Set("kind", kind)
	}
	if delivery := strings.TrimSpace(query.Delivery); delivery != "" {
		values.Set("delivery", delivery)
	}
	if text := strings.TrimSpace(query.Text); text != "" {
		values.Set("q", text)
	}
	if publisher := strings.TrimSpace(query.Publisher); publisher != "" {
		values.Set("publisher", publisher)
	}
	if cursor := strings.TrimSpace(query.Cursor); cursor != "" {
		values.Set("cursor", cursor)
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	endpoint := base + pathCatalog
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	res, err := c.call(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	switch {
	case res.ok():
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, ErrMarketRateLimited("catalog browse is rate limited")
	default:
		return nil, ErrMarketUnreachable(fmt.Sprintf("catalog browse returned HTTP %d", res.StatusCode))
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(res.Body, &top); err != nil {
		return nil, ErrMarketUnreachable("market returned an invalid catalog document")
	}
	var catalog Catalog
	if raw, ok := top["listings"]; ok {
		if err := json.Unmarshal(raw, &catalog.Listings); err != nil {
			return nil, ErrMarketUnreachable("market returned an invalid catalog document")
		}
		if raw, ok := top["next_cursor"]; ok {
			_ = json.Unmarshal(raw, &catalog.NextCursor)
		}
	} else {
		// Some market versions wrap the catalog: {"catalog": {...}}.
		var envelope struct {
			Catalog *Catalog `json:"catalog"`
		}
		if err := json.Unmarshal(res.Body, &envelope); err != nil || envelope.Catalog == nil {
			return nil, ErrMarketUnreachable("market returned an invalid catalog document")
		}
		catalog = *envelope.Catalog
	}
	catalog.Listings = filterListings(catalog.Listings, query)
	return &catalog, nil
}

// BrowseVerified browses and verifies every listing's trust document before
// returning it. A listing whose document is missing, invalid, expired, for a
// different slug, or for a different digest is dropped and logged; the instance
// never offers an unverified listing.
func (c *Client) BrowseVerified(ctx context.Context, issuer string, query BrowseQuery) (*Catalog, error) {
	catalog, err := c.Browse(ctx, issuer, query)
	if err != nil {
		return nil, err
	}
	verified := make([]Listing, 0, len(catalog.Listings))
	for i := range catalog.Listings {
		listing := catalog.Listings[i]
		if _, err := c.VerifyListingTrust(ctx, issuer, &listing); err != nil {
			c.log.Warn("dropping unverified market listing",
				"issuer", issuer,
				"listing", listing.Slug,
				"error", err)
			continue
		}
		verified = append(verified, listing)
	}
	catalog.Listings = verified
	return catalog, nil
}

func filterListings(listings []Listing, query BrowseQuery) []Listing {
	kind := strings.ToLower(strings.TrimSpace(query.Kind))
	delivery := strings.ToLower(strings.TrimSpace(query.Delivery))
	if kind == "" && delivery == "" {
		return listings
	}
	filtered := make([]Listing, 0, len(listings))
	for _, listing := range listings {
		if kind != "" && strings.ToLower(strings.TrimSpace(listing.Kind)) != kind {
			continue
		}
		if delivery != "" && strings.ToLower(strings.TrimSpace(listing.Delivery)) != delivery {
			continue
		}
		filtered = append(filtered, listing)
	}
	return filtered
}

// TrustClaims is the signed listing trust document the instance verifies before
// offering a listing. Unknown claims are ignored.
type TrustClaims struct {
	Issuer      string          `json:"iss"`
	Audience    json.RawMessage `json:"aud"`
	Subject     string          `json:"sub,omitempty"`
	ExpiresAt   int64           `json:"exp"`
	IssuedAt    int64           `json:"iat,omitempty"`
	NotBefore   int64           `json:"nbf,omitempty"`
	JTI         string          `json:"jti,omitempty"`
	ListingSlug string          `json:"listing_slug,omitempty"`
	Publisher   string          `json:"publisher,omitempty"`
	Digest      string          `json:"digest,omitempty"`
	Verified    bool            `json:"verified,omitempty"`
}

// VerifyListingTrust verifies the listing's embedded trust document, fetching
// GET /v1/listings/{slug}/trust when the listing did not carry one. It checks
// the signature against the issuer's key set, the issuer, the trust audience,
// expiry, and that the document names the same listing (and digest, when both
// sides declare one).
func (c *Client) VerifyListingTrust(ctx context.Context, issuer string, listing *Listing) (*TrustClaims, error) {
	if listing == nil {
		return nil, ErrTrustInvalid("listing is required")
	}
	token := strings.TrimSpace(listing.TrustDocument)
	if token == "" {
		var err error
		token, err = c.FetchTrustDocument(ctx, issuer, listing.Slug)
		if err != nil {
			return nil, err
		}
	}
	claims, err := c.VerifyTrustDocument(ctx, issuer, token)
	if err != nil {
		return nil, err
	}
	if claims.ListingSlug != "" && listing.Slug != "" && claims.ListingSlug != listing.Slug {
		return nil, ErrTrustInvalid("trust document names a different listing")
	}
	if got, want := CleanDigest(claims.Digest), CleanDigest(listing.Digest()); got != "" && want != "" && got != want {
		return nil, ErrTrustInvalid("trust document names a different digest")
	}
	return claims, nil
}

// FetchTrustDocument reads GET /v1/listings/{slug}/trust. The market may answer
// with a JSON envelope ({"trust_document": "..."}, {"document": "..."} or
// {"trust": "..."}), a bare JSON string, or a text/plain JWT.
func (c *Client) FetchTrustDocument(ctx context.Context, issuer, slug string) (string, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return "", err
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", ErrTrustInvalid("listing slug is required")
	}
	res, err := c.call(ctx, http.MethodGet, base+fmt.Sprintf(pathListingTrust, url.PathEscape(slug)), nil)
	if err != nil {
		return "", err
	}
	switch {
	case res.ok():
	case res.StatusCode == http.StatusNotFound:
		return "", ErrTrustInvalid("market publishes no trust document for this listing")
	case res.StatusCode == http.StatusTooManyRequests:
		return "", ErrMarketRateLimited("trust document fetch is rate limited")
	default:
		return "", ErrMarketUnreachable(fmt.Sprintf("trust document fetch returned HTTP %d", res.StatusCode))
	}
	return decodeTrustDocument(res.Body)
}

func decodeTrustDocument(body []byte) (string, error) {
	var text string
	if err := json.Unmarshal(body, &text); err == nil {
		if token := strings.TrimSpace(text); token != "" {
			return token, nil
		}
	}
	var envelope struct {
		TrustDocument string `json:"trust_document"`
		Document      string `json:"document"`
		Trust         string `json:"trust"`
		JWT           string `json:"jwt"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		for _, candidate := range []string{envelope.TrustDocument, envelope.Document, envelope.Trust, envelope.JWT} {
			if token := strings.TrimSpace(candidate); token != "" {
				return token, nil
			}
		}
	}
	// A text/plain response is a bare JWT.
	if raw := strings.TrimSpace(string(body)); strings.Count(raw, ".") == 2 && !strings.ContainsAny(raw, " \n\t") {
		return raw, nil
	}
	return "", ErrTrustInvalid("market returned a malformed trust document")
}

// ClaimRequest is POST /v1/entitlements. InstanceID and OriginID are filled
// from the client's registration (and configured origin) when left empty.
type ClaimRequest struct {
	ListingSlug string `json:"listing_slug"`
	Version     string `json:"version,omitempty"`
	InstanceID  string `json:"instance_id,omitempty"`
	OriginID    string `json:"origin_id,omitempty"`
}

// Entitlement is the market's grant of a listing to this account/instance.
type Entitlement struct {
	ID             string     `json:"id"`
	ListingSlug    string     `json:"listing_slug,omitempty"`
	ListingVersion string     `json:"version,omitempty"`
	Status         string     `json:"status,omitempty"`
	Delivery       string     `json:"delivery,omitempty"`
	Plan           string     `json:"plan,omitempty"`
	GrantedAt      MarketTime `json:"granted_at,omitempty"`
	ExpiresAt      MarketTime `json:"expires_at,omitempty"`
	CallsRemaining int        `json:"calls_remaining,omitempty"`
}

// Claim claims a (usually free) entitlement for this account + listing. A 409
// from the market is surfaced as ErrEntitlementAlreadyHeld rather than being
// treated as an unreachable market.
func (c *Client) Claim(ctx context.Context, issuer string, req ClaimRequest) (*Entitlement, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	req.ListingSlug = strings.TrimSpace(req.ListingSlug)
	if req.ListingSlug == "" {
		return nil, ErrClaimFailed("listing_slug is required")
	}
	if strings.TrimSpace(req.InstanceID) == "" {
		if registration := c.cachedRegistration(base); registration != nil {
			req.InstanceID = registration.InstanceID
		}
	}
	if strings.TrimSpace(req.OriginID) == "" {
		req.OriginID = c.originID()
	}

	res, err := c.call(ctx, http.MethodPost, base+pathEntitlements, req)
	if err != nil {
		return nil, err
	}
	switch {
	case res.ok():
	case res.StatusCode == http.StatusConflict:
		return nil, ErrEntitlementAlreadyHeld(req.ListingSlug)
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, ErrMarketRateLimited("entitlement claim is rate limited")
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, ErrClaimFailed("this instance is not allowed to claim the listing")
	default:
		return nil, ErrClaimFailed(fmt.Sprintf("market claim returned HTTP %d", res.StatusCode))
	}
	entitlement, err := decodeEntitlement(res.Body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(entitlement.ID) == "" {
		return nil, ErrClaimFailed("market claim returned no entitlement id")
	}
	if entitlement.ListingSlug == "" {
		entitlement.ListingSlug = req.ListingSlug
	}
	return entitlement, nil
}

func decodeEntitlement(body []byte) (*Entitlement, error) {
	var direct Entitlement
	if err := json.Unmarshal(body, &direct); err == nil && direct.ID != "" {
		return &direct, nil
	}
	var envelope struct {
		Entitlement *Entitlement `json:"entitlement"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Entitlement != nil {
		return envelope.Entitlement, nil
	}
	if err := json.Unmarshal(body, &direct); err != nil {
		return nil, ErrMarketUnreachable("market returned an invalid entitlement document")
	}
	return &direct, nil
}

// authorizeRequest is POST /v1/entitlements/{id}/authorize.
type authorizeRequest struct {
	Calls      int    `json:"calls"`
	OriginID   string `json:"origin_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
}

// UsageReport is POST /v1/usage. RequestID must be one the market pre-issued in
// an authorization batch; the client refuses any other id before sending.
type UsageReport struct {
	EntitlementID string     `json:"entitlement_id,omitempty"`
	ListingSlug   string     `json:"listing_slug,omitempty"`
	RequestID     string     `json:"request_id"`
	Calls         int        `json:"calls,omitempty"`
	Outcome       string     `json:"outcome,omitempty"`
	OccurredAt    MarketTime `json:"occurred_at,omitempty"`
}

// ReportUsage reports one served call to POST /v1/usage using the pre-issued
// request_id. If the market is unreachable (or rate-limiting), the report is
// queued and the call returns nil: the instance keeps working and replays the
// report when the market is reachable again. The deferral is logged, never
// silently dropped. A request id the market never issued, or a repeated one, is
// refused locally.
func (c *Client) ReportUsage(ctx context.Context, issuer string, report UsageReport) error {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return err
	}
	report.RequestID = strings.TrimSpace(report.RequestID)
	if report.RequestID == "" {
		return ErrUsageRequestUnknown("request_id is required")
	}
	if report.Calls <= 0 {
		report.Calls = 1
	}
	if strings.TrimSpace(report.Outcome) == "" {
		report.Outcome = UsageOutcomeSuccess
	}
	entitlementID, err := c.reserveUsage(base, report)
	if err != nil {
		return err
	}
	report.EntitlementID = entitlementID
	if report.OccurredAt.IsZero() {
		report.OccurredAt = MarketTime{Time: c.now().UTC()}
	}

	if err := c.sendUsage(ctx, base, report); err != nil {
		if isRetryableMarketError(err) {
			c.deferUsage(base, report, err)
			c.log.Warn("market unavailable; usage report deferred for replay",
				"issuer", base,
				"entitlement_id", report.EntitlementID,
				"request_id", report.RequestID,
				"error", err)
			return nil
		}
		c.releaseUsage(base, report.RequestID)
		return err
	}
	c.markUsageReported(base, report.RequestID)
	return nil
}

func (c *Client) sendUsage(ctx context.Context, base string, report UsageReport) error {
	res, err := c.call(ctx, http.MethodPost, base+pathUsage, report)
	if err != nil {
		return err
	}
	switch {
	case res.ok():
		return nil
	case res.StatusCode == http.StatusConflict:
		// The market already has this request id. That is the idempotent
		// outcome for a replay, not a fabrication.
		return nil
	case res.StatusCode == http.StatusTooManyRequests:
		return ErrMarketRateLimited("usage report is rate limited")
	case res.StatusCode == http.StatusBadRequest ||
		res.StatusCode == http.StatusNotFound ||
		res.StatusCode == http.StatusUnprocessableEntity:
		return ErrUsageRejected("market refused the usage request id")
	default:
		return ErrMarketUnreachable(fmt.Sprintf("usage report returned HTTP %d", res.StatusCode))
	}
}

// FlushUsage replays queued usage reports for one issuer. It stops at the first
// transport/rate-limit failure (keeping the remaining reports queued) and
// returns nil once every queued report has been accepted or definitively
// refused by the market.
func (c *Client) FlushUsage(ctx context.Context, issuer string) error {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return err
	}
	pending := c.takePendingUsage(base)
	var lastErr error
	for i, report := range pending {
		if err := c.sendUsage(ctx, base, report); err != nil {
			if isRetryableMarketError(err) {
				c.requeuePendingUsage(base, pending[i:])
				c.log.Warn("market unavailable; usage replay stopped",
					"issuer", base,
					"remaining", len(pending)-i,
					"error", err)
				lastErr = err
				break
			}
			c.log.Warn("market refused deferred usage report; attribution dropped",
				"issuer", base,
				"entitlement_id", report.EntitlementID,
				"request_id", report.RequestID,
				"error", err)
			c.markUsageReported(base, report.RequestID)
			continue
		}
		c.markUsageReported(base, report.RequestID)
	}
	if lastErr != nil {
		return lastErr
	}
	return nil
}

// isRetryableMarketError reports whether an error means "the market could not
// answer now" rather than "the market answered no". Only the former is worth
// replaying from a cache/queue.
func isRetryableMarketError(err error) bool {
	code := ErrorCode(err)
	return code == CodeMarketUnreach || code == CodeMarketRateLimited
}
