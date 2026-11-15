package market

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LicenseAudience is the only audience an install license may carry.
const LicenseAudience = "memory-install"

// LicenseClaims is the subset of install-license claims the instance verifies
// and records. Unknown claims are ignored.
type LicenseClaims struct {
	Issuer      string          `json:"iss"`
	Audience    json.RawMessage `json:"aud"`
	Subject     string          `json:"sub,omitempty"`
	ExpiresAt   int64           `json:"exp"`
	IssuedAt    int64           `json:"iat,omitempty"`
	NotBefore   int64           `json:"nbf,omitempty"`
	JTI         string          `json:"jti,omitempty"`
	Digest      string          `json:"digest,omitempty"`
	ListingSlug string          `json:"listing_slug,omitempty"`
	Semver      string          `json:"semver,omitempty"`
	InstallID   string          `json:"install_id,omitempty"`
}

type licenseHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid,omitempty"`
	Typ string `json:"typ,omitempty"`
}

type jsonWebKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid,omitempty"`
	Alg string `json:"alg,omitempty"`
	Use string `json:"use,omitempty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

type jwksDocument struct {
	Keys []jsonWebKey `json:"keys"`
}

type verificationKey struct {
	kid string
	pub *ecdsa.PublicKey
}

type jwksEntry struct {
	keys      []verificationKey
	fetchedAt time.Time
}

type jwksCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]*jwksEntry
}

func newJWKSCache(ttl time.Duration, now func() time.Time) *jwksCache {
	return &jwksCache{ttl: ttl, now: now, entries: make(map[string]*jwksEntry)}
}

// VerifyLicense verifies an install license JWT against the issuer's JWKS and
// returns its claims. It enforces the issuer allowlist before the JWKS fetch,
// then the ES256 signature, iss, aud=memory-install, exp/nbf and a non-empty
// digest claim. Key sets are cached per issuer, so a dev key never verifies a
// prod license.
func (c *Client) VerifyLicense(ctx context.Context, issuer, token string) (*LicenseClaims, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrLicenseInvalid("install license is not a JWT")
	}
	headerBytes, err := decodeJWTPart(parts[0])
	if err != nil {
		return nil, ErrLicenseInvalid("install license header is malformed")
	}
	var header licenseHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, ErrLicenseInvalid("install license header is malformed")
	}
	if header.Alg != "ES256" {
		return nil, ErrLicenseInvalid("install license alg must be ES256")
	}
	payloadBytes, err := decodeJWTPart(parts[1])
	if err != nil {
		return nil, ErrLicenseInvalid("install license payload is malformed")
	}
	var claims LicenseClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, ErrLicenseInvalid("install license payload is malformed")
	}
	sig, err := decodeJWTPart(parts[2])
	if err != nil {
		return nil, ErrLicenseInvalid("install license signature is malformed")
	}

	keys, err := c.keysFor(ctx, base, header.Kid, false)
	if err != nil {
		return nil, err
	}
	signingInput := []byte(parts[0] + "." + parts[1])
	if !verifySignature(keys, header.Kid, signingInput, sig) {
		// A rotated key set is the one recoverable case: refresh once and retry.
		keys, rerr := c.keysFor(ctx, base, header.Kid, true)
		if rerr != nil {
			return nil, rerr
		}
		if !verifySignature(keys, header.Kid, signingInput, sig) {
			return nil, ErrLicenseInvalid("install license signature did not verify")
		}
	}

	if claims.Issuer != base {
		return nil, ErrLicenseInvalid("install license issuer does not match the market")
	}
	if !audienceMatches(claims.Audience, LicenseAudience) {
		return nil, ErrLicenseInvalid("install license audience must be " + LicenseAudience)
	}
	now := c.now().Unix()
	if claims.ExpiresAt == 0 || now >= claims.ExpiresAt {
		return nil, ErrLicenseInvalid("install license has expired")
	}
	if claims.NotBefore != 0 && now < claims.NotBefore {
		return nil, ErrLicenseInvalid("install license is not yet valid")
	}
	if CleanDigest(claims.Digest) == "" {
		return nil, ErrLicenseInvalid("install license carries no archive digest")
	}
	return &claims, nil
}

// keysFor returns the cached verification keys for one issuer, fetching the
// issuer's JWKS on a cold/expired cache (or when forced).
func (c *Client) keysFor(ctx context.Context, issuer, kid string, force bool) ([]verificationKey, error) {
	c.jwks.mu.Lock()
	defer c.jwks.mu.Unlock()

	entry := c.jwks.entries[issuer]
	if !force && entry != nil && c.now().Sub(entry.fetchedAt) < c.jwks.ttl {
		if kid == "" || hasKid(entry.keys, kid) {
			return entry.keys, nil
		}
	}
	if force && entry != nil && c.now().Sub(entry.fetchedAt) < time.Second {
		// Do not hammer a market on a burst of bad tokens.
		return entry.keys, nil
	}

	keys, err := c.fetchJWKS(ctx, issuer)
	if err != nil {
		return nil, err
	}
	c.jwks.entries[issuer] = &jwksEntry{keys: keys, fetchedAt: c.now()}
	return keys, nil
}

func (c *Client) fetchJWKS(ctx context.Context, issuer string) ([]verificationKey, error) {
	endpoint := issuer + "/.well-known/market-keys.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrMarketUnreachable("failed to build JWKS request")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, ErrMarketUnreachable("failed to fetch market JWKS")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrMarketUnreachable(fmt.Sprintf("market JWKS returned HTTP %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBody+1))
	if err != nil {
		return nil, ErrMarketUnreachable("failed to read market JWKS")
	}
	if len(body) > maxJSONBody {
		return nil, ErrMarketUnreachable("market JWKS is too large")
	}
	var doc jwksDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, ErrLicenseInvalid("market JWKS is malformed")
	}
	if len(doc.Keys) == 0 {
		return nil, ErrLicenseInvalid("market JWKS has no keys")
	}
	keys := make([]verificationKey, 0, len(doc.Keys))
	for _, jwk := range doc.Keys {
		pub, ok := parseECPublicKey(jwk)
		if !ok {
			continue
		}
		keys = append(keys, verificationKey{kid: jwk.Kid, pub: pub})
	}
	if len(keys) == 0 {
		return nil, ErrLicenseInvalid("market JWKS has no ES256 keys")
	}
	return keys, nil
}

func parseECPublicKey(jwk jsonWebKey) (*ecdsa.PublicKey, bool) {
	if jwk.Kty != "EC" {
		return nil, false
	}
	if jwk.Alg != "" && jwk.Alg != "ES256" {
		return nil, false
	}
	if jwk.Crv != "" && jwk.Crv != "P-256" {
		return nil, false
	}
	x, err := decodeJWTPart(jwk.X)
	if err != nil || len(x) != 32 {
		return nil, false
	}
	y, err := decodeJWTPart(jwk.Y)
	if err != nil || len(y) != 32 {
		return nil, false
	}
	pub := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}
	if !pub.IsOnCurve(pub.X, pub.Y) {
		return nil, false
	}
	return pub, true
}

func verifySignature(keys []verificationKey, kid string, signingInput, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	digest := sha256.Sum256(signingInput)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return false
	}
	for _, key := range keys {
		if kid != "" && key.kid != "" && key.kid != kid {
			continue
		}
		if ecdsa.Verify(key.pub, digest[:], r, s) {
			return true
		}
	}
	return false
}

func hasKid(keys []verificationKey, kid string) bool {
	for _, key := range keys {
		if key.kid == kid {
			return true
		}
	}
	return false
}

func audienceMatches(raw json.RawMessage, want string) bool {
	if len(raw) == 0 {
		return false
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single == want
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, aud := range list {
			if aud == want {
				return true
			}
		}
	}
	return false
}

func decodeJWTPart(part string) ([]byte, error) {
	if part == "" {
		return nil, fmt.Errorf("empty JWT segment")
	}
	if data, err := base64.RawURLEncoding.DecodeString(part); err == nil {
		return data, nil
	}
	return base64.URLEncoding.DecodeString(part)
}
