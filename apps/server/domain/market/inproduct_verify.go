package market

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// invalidError builds the named error a verification helper returns for a
// malformed or unverifiable document.
type invalidError func(detail string) *apperror.Error

// VerifyTrustToken verifies a listing's signed trust document against the
// issuer's cached key set. The document must be a market-issued ES256 JWT with
// audience memory-listing-trust, must not be expired, and must name a listing
// that is active. The issuer allowlist is enforced before any JWKS fetch.
func (c *Client) VerifyTrustToken(ctx context.Context, issuer, token string) (*TrustClaims, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	_, payload, err := c.verifyInProductJWT(ctx, base, token, ErrListingUntrusted)
	if err != nil {
		return nil, err
	}
	var claims TrustClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrListingUntrusted("listing trust document payload is malformed")
	}
	if claims.Issuer != base {
		return nil, ErrListingUntrusted("listing trust document issuer does not match the market")
	}
	if !audienceMatches(claims.Audience, TrustAudience) {
		return nil, ErrListingUntrusted("listing trust document audience must be " + TrustAudience)
	}
	now := c.now().Unix()
	if claims.ExpiresAt == 0 || now >= claims.ExpiresAt {
		return nil, ErrListingUntrusted("listing trust document has expired")
	}
	if claims.NotBefore != 0 && now < claims.NotBefore {
		return nil, ErrListingUntrusted("listing trust document is not yet valid")
	}
	if claims.Slug() == "" {
		return nil, ErrListingUntrusted("listing trust document names no listing")
	}
	if claims.Status != "" && claims.Status != TrustStatusActive {
		return nil, ErrListingUntrusted("listing is " + claims.Status + " in the market")
	}
	return &claims, nil
}

// VerifyAssertion verifies a market-issued call assertion against the issuer's
// cached key set. This is the seller-side contract too: a seller that trusts
// the issuer fetches the market's published key set once and verifies offline,
// then serves the call only for a request id the assertion covers.
func (c *Client) VerifyAssertion(ctx context.Context, issuer, token string) (*AssertionClaims, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	_, payload, err := c.verifyInProductJWT(ctx, base, token, ErrAuthorizationInvalid)
	if err != nil {
		return nil, err
	}
	var claims AssertionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrAuthorizationInvalid("call assertion payload is malformed")
	}
	if claims.Issuer != base {
		return nil, ErrAuthorizationInvalid("call assertion issuer does not match the market")
	}
	if !audienceMatches(claims.Audience, AssertionAudience) {
		return nil, ErrAuthorizationInvalid("call assertion audience must be " + AssertionAudience)
	}
	now := c.now().Unix()
	if claims.ExpiresAt == 0 || now >= claims.ExpiresAt {
		return nil, ErrAuthorizationInvalid("call assertion has expired")
	}
	if claims.NotBefore != 0 && now < claims.NotBefore {
		return nil, ErrAuthorizationInvalid("call assertion is not yet valid")
	}
	if strings.TrimSpace(claims.EntitlementID) == "" {
		return nil, ErrAuthorizationInvalid("call assertion names no entitlement")
	}
	if len(claims.RequestIDs) == 0 {
		return nil, ErrAuthorizationInvalid("call assertion carries no request ids")
	}
	if len(claims.RequestIDs) > MaxBatchCalls {
		return nil, ErrAuthorizationInvalid("call assertion carries more request ids than the batch limit")
	}
	seen := make(map[string]struct{}, len(claims.RequestIDs))
	for _, id := range claims.RequestIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			return nil, ErrAuthorizationInvalid("call assertion carries an empty request id")
		}
		if trimmed != id {
			return nil, ErrAuthorizationInvalid("call assertion carries a malformed request id")
		}
		if _, dup := seen[id]; dup {
			return nil, ErrAuthorizationInvalid("call assertion carries a duplicate request id")
		}
		seen[id] = struct{}{}
	}
	return &claims, nil
}

// verifyInProductJWT parses an ES256 JWT, verifies its signature against the
// issuer's cached key set and returns its decoded payload. A failed signature
// triggers one forced key-set refresh so a rotated key verifies without a
// restart; the anti-hammer window in keysFor bounds the refresh.
func (c *Client) verifyInProductJWT(ctx context.Context, issuer, token string, invalid invalidError) (licenseHeader, []byte, error) {
	var header licenseHeader
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return header, nil, invalid("document is not a JWT")
	}
	headerBytes, err := decodeJWTPart(parts[0])
	if err != nil {
		return header, nil, invalid("document header is malformed")
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return header, nil, invalid("document header is malformed")
	}
	if header.Alg != "ES256" {
		return header, nil, invalid("document alg must be ES256")
	}
	payload, err := decodeJWTPart(parts[1])
	if err != nil {
		return header, nil, invalid("document payload is malformed")
	}
	sig, err := decodeJWTPart(parts[2])
	if err != nil {
		return header, nil, invalid("document signature is malformed")
	}
	keys, err := c.keysFor(ctx, issuer, header.Kid, false)
	if err != nil {
		return header, nil, err
	}
	signingInput := []byte(parts[0] + "." + parts[1])
	if !verifySignature(keys, header.Kid, signingInput, sig) {
		keys, rerr := c.keysFor(ctx, issuer, header.Kid, true)
		if rerr != nil {
			return header, nil, rerr
		}
		if !verifySignature(keys, header.Kid, signingInput, sig) {
			return header, nil, invalid("document signature did not verify")
		}
	}
	return header, payload, nil
}

// extractSignedDocument pulls a JWT out of the trust-document envelopes the
// market may use (a JSON string, a named field, or a bare JWT body).
func extractSignedDocument(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	var direct string
	if err := json.Unmarshal(trimmed, &direct); err == nil {
		return strings.TrimSpace(direct)
	}
	var envelope struct {
		Trust     string `json:"trust"`
		Document  string `json:"document"`
		Token     string `json:"token"`
		Assertion string `json:"assertion"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err == nil {
		for _, candidate := range []string{envelope.Trust, envelope.Document, envelope.Token, envelope.Assertion} {
			if candidate = strings.TrimSpace(candidate); candidate != "" {
				return candidate
			}
		}
	}
	if strings.Count(string(trimmed), ".") == 2 && !bytes.ContainsAny(trimmed, "{}[]\"") {
		return string(trimmed)
	}
	return ""
}

// marketErrorFields decodes the market's error envelope, accepting the common
// spellings (code, error_code, error as a string or an object).
func marketErrorFields(body []byte) (code, message string) {
	var envelope struct {
		Code      string          `json:"code"`
		ErrorCode string          `json:"error_code"`
		Message   string          `json:"message"`
		Error     json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", ""
	}
	code = firstNonEmptyString(envelope.Code, envelope.ErrorCode)
	message = envelope.Message
	if len(envelope.Error) > 0 {
		var text string
		if err := json.Unmarshal(envelope.Error, &text); err == nil {
			code = firstNonEmptyString(code, text)
			message = firstNonEmptyString(message, text)
		}
		var object struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(envelope.Error, &object); err == nil {
			code = firstNonEmptyString(code, object.Code)
			message = firstNonEmptyString(message, object.Message)
		}
	}
	return code, message
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// sameStringSet reports whether a and b carry the same unique values.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, value := range a {
		set[value] = struct{}{}
	}
	if len(set) != len(a) {
		return false
	}
	for _, value := range b {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}
