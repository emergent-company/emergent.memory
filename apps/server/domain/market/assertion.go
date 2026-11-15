package market

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// AssertionClaims is the subset of the market's entitlement assertion a seller
// verifies. Unknown claims are ignored.
type AssertionClaims struct {
	Issuer        string          `json:"iss"`
	Audience      json.RawMessage `json:"aud"`
	Subject       string          `json:"sub,omitempty"`
	ExpiresAt     int64           `json:"exp"`
	IssuedAt      int64           `json:"iat,omitempty"`
	NotBefore     int64           `json:"nbf,omitempty"`
	JTI           string          `json:"jti,omitempty"`
	EntitlementID string          `json:"entitlement_id,omitempty"`
	ListingSlug   string          `json:"listing_slug,omitempty"`
	RequestIDs    []string        `json:"request_ids,omitempty"`
}

// VerifyAssertion verifies an ES256 assertion against the issuer's published
// key set. It is the seller-side contract the instance also uses in tests and
// in its own in-product path: no market call is made beyond fetching/caching
// JWKS.
//
// When one or more audiences are supplied, at least one must match. An empty
// audience list accepts any non-empty audience; a seller should pass its own
// canonical endpoint so an assertion minted for another seller cannot be
// replayed against it.
func (c *Client) VerifyAssertion(ctx context.Context, issuer, token string, audiences ...string) (*AssertionClaims, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	var claims AssertionClaims
	if err := c.verifyMarketToken(ctx, base, token, "market assertion", &claims, ErrAssertionInvalid); err != nil {
		return nil, err
	}
	if claims.Issuer != base {
		return nil, ErrAssertionInvalid("assertion issuer does not match the market")
	}
	if !audienceMatchesAny(claims.Audience, audiences) {
		if len(audiences) == 0 {
			return nil, ErrAssertionInvalid("assertion carries no audience")
		}
		return nil, ErrAssertionInvalid("assertion audience does not match the seller")
	}
	now := c.now().Unix()
	if claims.ExpiresAt == 0 || now >= claims.ExpiresAt {
		return nil, ErrAssertionInvalid("assertion has expired")
	}
	if claims.NotBefore != 0 && now < claims.NotBefore {
		return nil, ErrAssertionInvalid("assertion is not yet valid")
	}
	return &claims, nil
}

// VerifyTrustDocument verifies a listing trust document against the issuer's
// key set. The document must be an ES256 JWT with aud=memory-listing-trust, a
// matching issuer, a live expiry, and a listing subject.
func (c *Client) VerifyTrustDocument(ctx context.Context, issuer, token string) (*TrustClaims, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	var claims TrustClaims
	if err := c.verifyMarketToken(ctx, base, token, "listing trust document", &claims, ErrTrustInvalid); err != nil {
		return nil, err
	}
	if claims.Issuer != base {
		return nil, ErrTrustInvalid("trust document issuer does not match the market")
	}
	if !audienceMatchesAny(claims.Audience, []string{TrustDocumentAudience}) {
		return nil, ErrTrustInvalid("trust document audience must be " + TrustDocumentAudience)
	}
	now := c.now().Unix()
	if claims.ExpiresAt == 0 || now >= claims.ExpiresAt {
		return nil, ErrTrustInvalid("trust document has expired")
	}
	if claims.NotBefore != 0 && now < claims.NotBefore {
		return nil, ErrTrustInvalid("trust document is not yet valid")
	}
	if claims.ListingSlug == "" {
		claims.ListingSlug = claims.Subject
	}
	if strings.TrimSpace(claims.ListingSlug) == "" {
		return nil, ErrTrustInvalid("trust document names no listing")
	}
	return &claims, nil
}

// verifyMarketToken parses an ES256 JWT, decodes its payload into out, and
// verifies the signature against the issuer's key set. A key-set rotation is
// the recoverable failure mode: the first mismatch refreshes JWKS once and
// retries, so a seller that cached an old key set keeps working across a
// rotation.
func (c *Client) verifyMarketToken(ctx context.Context, base, token, label string, out any, invalid func(string) *apperror.Error) error {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return invalid(label + " is not a JWT")
	}
	headerBytes, err := decodeJWTPart(parts[0])
	if err != nil {
		return invalid(label + " header is malformed")
	}
	var header licenseHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return invalid(label + " header is malformed")
	}
	if header.Alg != "ES256" {
		return invalid(label + " alg must be ES256")
	}
	payloadBytes, err := decodeJWTPart(parts[1])
	if err != nil {
		return invalid(label + " payload is malformed")
	}
	if err := json.Unmarshal(payloadBytes, out); err != nil {
		return invalid(label + " payload is malformed")
	}
	sig, err := decodeJWTPart(parts[2])
	if err != nil {
		return invalid(label + " signature is malformed")
	}

	keys, err := c.keysFor(ctx, base, header.Kid, false)
	if err != nil {
		if ErrorCode(err) == CodeLicenseInvalid {
			return invalid("market key set is invalid")
		}
		return err
	}
	signingInput := []byte(parts[0] + "." + parts[1])
	if !verifySignature(keys, header.Kid, signingInput, sig) {
		keys, rerr := c.keysFor(ctx, base, header.Kid, true)
		if rerr != nil {
			if ErrorCode(rerr) == CodeLicenseInvalid {
				return invalid("market key set is invalid")
			}
			return rerr
		}
		if !verifySignature(keys, header.Kid, signingInput, sig) {
			return invalid(label + " signature did not verify")
		}
	}
	return nil
}

// audienceMatchesAny reports whether the JWT audience claim (a string or a list
// of strings) contains one of wants. With no wants, any non-empty audience is
// accepted; that is only used by callers that cannot know the seller audience.
func audienceMatchesAny(raw json.RawMessage, wants []string) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if len(wants) == 0 {
			return strings.TrimSpace(single) != ""
		}
		for _, want := range wants {
			if single == want {
				return true
			}
		}
		return false
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		if len(wants) == 0 {
			return len(list) > 0
		}
		for _, aud := range list {
			for _, want := range wants {
				if aud == want {
					return true
				}
			}
		}
	}
	return false
}
