package market

import (
	"encoding/json"
	"net/http"
	"time"
)

// In-product marketplace contract constants (Phase 0). The product half calls
// the market directly with a market-issued assertion; the edge is the other
// path into the same usage_reports rows (see docs/marketplace-v1.md §8.1).
const (
	// DefaultIssuer is the public market origin a stock instance uses when no
	// operator configuration is present. The in-product surface must work
	// without an operator editing a file; the install surface keeps its
	// explicit allowlist.
	DefaultIssuer = "https://market.memory.emergent-company.ai"

	// MaxBatchCalls is the largest authorization batch the market issues.
	MaxBatchCalls = 20
	// MaxBatchTTL is the longest-lived authorization batch. The instance asks
	// for exactly this, so a cached batch survives an outage of at least a
	// minute.
	MaxBatchTTL = 60 * time.Second

	// TrustAudience is the audience a listing trust document must carry.
	TrustAudience = "memory-listing-trust"
	// AssertionAudience is the audience a call assertion must carry. A seller
	// verifies it offline against the market's published key set.
	AssertionAudience = "memory-mcp"

	// TrustStatusActive is the only trust-document state that permits the
	// instance to offer a listing. An empty status is treated as active so a
	// document that predates the field still verifies.
	TrustStatusActive = "active"

	// HeaderAssertion is the header the instance uses to present the
	// market-issued assertion to a seller's MCP endpoint.
	HeaderAssertion = "Authorization"
	// HeaderRequestID is the header the seller echoes to name the pre-issued
	// request_id it served. The instance reports usage against that id.
	HeaderRequestID = "X-Market-Request-Id"
)

// Usage report outcomes. The market records them on the same usage_reports row
// the edge writes.
const (
	UsageOutcomeOK     = "ok"
	UsageOutcomeError  = "error"
	UsageOutcomeDenied = "denied"
)

// RegisterRequest is POST /v1/instances. The instance announces its stable
// origin identity so the market can bind entitlements and usage to it.
type RegisterRequest struct {
	OriginID string `json:"origin_id"`
	Name     string `json:"name,omitempty"`
	Version  string `json:"memory_version,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

// Registration is the market's instance registration. Token is the instance
// credential used for later in-product calls when the operator did not supply
// an account token.
type Registration struct {
	InstanceID string `json:"instance_id"`
	OriginID   string `json:"origin_id"`
	AccountID  string `json:"account_id,omitempty"`
	Token      string `json:"token,omitempty"`
}

// BrowseQuery is one catalog search. Kind and Delivery are the two marketplace
// axes (§6); Search narrows the text, and FreeOnly keeps only free listings.
type BrowseQuery struct {
	Kind     string
	Delivery string
	Search   string
	FreeOnly bool
	Limit    int
	Cursor   string
}

// TrustClaims is the signed listing trust document the instance verifies before
// offering a listing. The document is a market-issued ES256 JWT; the claims are
// carried so callers can display publisher/status and pin the digest.
type TrustClaims struct {
	Issuer      string          `json:"iss"`
	Audience    json.RawMessage `json:"aud"`
	Subject     string          `json:"sub,omitempty"`
	ExpiresAt   int64           `json:"exp"`
	IssuedAt    int64           `json:"iat,omitempty"`
	NotBefore   int64           `json:"nbf,omitempty"`
	JTI         string          `json:"jti,omitempty"`
	ListingSlug string          `json:"listing_slug,omitempty"`
	Listing     string          `json:"listing,omitempty"`
	Publisher   string          `json:"publisher,omitempty"`
	Kind        string          `json:"kind,omitempty"`
	Delivery    string          `json:"delivery,omitempty"`
	Digest      string          `json:"digest,omitempty"`
	Status      string          `json:"status,omitempty"`
}

// Slug returns the listing the trust document describes, accepting either
// spelling.
func (t *TrustClaims) Slug() string {
	if t == nil {
		return ""
	}
	if t.ListingSlug != "" {
		return t.ListingSlug
	}
	return t.Listing
}

// AssertionClaims is the market-issued ES256 assertion the instance presents to
// a seller. The seller verifies it offline against the market's key set, checks
// that ListingSlug names its listing, and echoes one of RequestIDs.
type AssertionClaims struct {
	Issuer        string          `json:"iss"`
	Audience      json.RawMessage `json:"aud"`
	Subject       string          `json:"sub,omitempty"`
	ExpiresAt     int64           `json:"exp"`
	IssuedAt      int64           `json:"iat,omitempty"`
	NotBefore     int64           `json:"nbf,omitempty"`
	JTI           string          `json:"jti,omitempty"`
	EntitlementID string          `json:"entitlement_id"`
	ListingSlug   string          `json:"listing_slug"`
	OriginID      string          `json:"origin_id,omitempty"`
	RequestIDs    []string        `json:"request_ids"`
}

// Entitlement is a claimed grant for one account/instance and listing.
// AlreadyHeld is set when the market said the entitlement already existed; a
// repeat claim is an ordinary success, not a failure.
type Entitlement struct {
	ID          string `json:"id"`
	ListingSlug string `json:"listing_slug,omitempty"`
	OriginID    string `json:"origin_id,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
	State       string `json:"state,omitempty"`
	AlreadyHeld bool   `json:"already_held,omitempty"`
}

// Batch is one market authorization: a single assertion plus the pre-issued
// request ids it covers. ExpiresAt comes from the assertion, never from a
// separate untrusted response field.
type Batch struct {
	Assertion     string
	RequestIDs    []string
	EntitlementID string
	ListingSlug   string
	OriginID      string
	IssuedAt      time.Time
	ExpiresAt     time.Time
}

// Expired reports whether a seller would refuse the batch's assertion.
func (b *Batch) Expired(now time.Time) bool {
	return b == nil || !b.ExpiresAt.After(now)
}

// Invocation is one authorized call: the assertion to present and the
// pre-issued request id the seller must echo.
type Invocation struct {
	Assertion     string
	RequestID     string
	EntitlementID string
	ListingSlug   string
	ExpiresAt     time.Time
}

// Apply presents the invocation on a seller request. The seller verifies the
// assertion offline and echoes RequestID in HeaderRequestID.
func (i *Invocation) Apply(req *http.Request) {
	if i == nil || req == nil {
		return
	}
	req.Header.Set(HeaderAssertion, "Bearer "+i.Assertion)
	req.Header.Set(HeaderRequestID, i.RequestID)
}

// SellerResponse is one completed seller call. RequestID is the seller's echo;
// it must equal the invocation's pre-issued id.
type SellerResponse struct {
	StatusCode int
	Body       []byte
	RequestID  string
	Outcome    string
}

// UsageReport is POST /v1/usage. RequestID must be one the market pre-issued in
// an authorization batch; the instance refuses to send anything else.
type UsageReport struct {
	RequestID     string `json:"request_id"`
	EntitlementID string `json:"entitlement_id"`
	ListingSlug   string `json:"listing_slug,omitempty"`
	OriginID      string `json:"origin_id,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
}

// Acquisition is the completed in-product acquisition path: a free hosted
// listing, its verified trust document, the claimed entitlement and an
// authorized batch.
type Acquisition struct {
	Listing     *Listing
	Trust       *TrustClaims
	Entitlement *Entitlement
	Batch       *Batch
}

// claimRequest is POST /v1/entitlements/claim.
type claimRequest struct {
	ListingSlug string `json:"listing_slug"`
	OriginID    string `json:"origin_id"`
}

// authorizeRequest is POST /v1/entitlements/{id}/authorize.
type authorizeRequest struct {
	OriginID   string `json:"origin_id"`
	Calls      int    `json:"calls"`
	TTLSeconds int    `json:"ttl_seconds"`
	Audience   string `json:"audience,omitempty"`
}

// authorizeResponse is the batch-authorize response. The assertion is
// authoritative; request_ids and expiry are cross-checked against its claims.
type authorizeResponse struct {
	Assertion  string   `json:"assertion"`
	RequestIDs []string `json:"request_ids,omitempty"`
}
