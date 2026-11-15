package market

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/internal/version"
)

// Defaults bounding a single install's network work.
const (
	defaultHTTPTimeout  = 30 * time.Second
	defaultJWKSCacheTTL = 15 * time.Minute
	defaultMaxBundle    = 200 << 20 // 200 MiB, matching the extraction cap
	maxJSONBody         = 2 << 20
)

// Client is the instance-side market HTTP client. It enforces the issuer
// allowlist before every request and caches JWKS per issuer.
//
// It carries two related surfaces: the install flow (Exchange, VerifyLicense,
// DownloadBundle, ReportInstall) and the in-product flow (Register, Browse,
// VerifyTrustDocument, Claim, Authorize, ReportUsage). The latter keeps a
// short-lived authorization batch and a usage-replay queue so the instance can
// keep serving when the market is briefly unreachable.
type Client struct {
	issuers        map[string]struct{}
	httpClient     *http.Client
	jwks           *jwksCache
	maxBundleBytes int64
	now            func() time.Time

	originOnce  sync.Once
	origin      string
	instanceURL string
	name        string
	version     string
	log         *slog.Logger
	inProduct   *inProductState
}

// ClientConfig configures a Client.
type ClientConfig struct {
	// Issuers is the explicit allowlist of normalized market origins.
	Issuers []string
	// HTTPClient overrides the transport (tests). The default client refuses
	// redirects; a supplied client follows its own CheckRedirect policy, so a
	// caller that needs the no-redirect guarantee must set one.
	HTTPClient *http.Client
	// JWKSCacheTTL bounds how long one issuer's key set is reused. Zero uses
	// the default.
	JWKSCacheTTL time.Duration
	// MaxBundleBytes caps a downloaded bundle. Zero uses 200 MiB.
	MaxBundleBytes int64
	// Now overrides the clock (tests).
	Now func() time.Time
	// OriginID is the stable identity the instance announces at registration.
	// When empty the client generates one (and keeps it for the life of the
	// process). Set MEMORY_MARKET_ORIGIN_ID to make it stable across restarts.
	OriginID string
	// InstanceName is the human-readable instance name sent at registration.
	// Zero uses "Memory".
	InstanceName string
	// InstanceVersion is the instance version sent at registration. Zero uses
	// the build version.
	InstanceVersion string
	// InstanceURL is this instance's public origin, announced at registration.
	InstanceURL string
	// Logger receives outage/deferral diagnostics. Zero discards them.
	Logger *slog.Logger
	// MaxBatchCalls caps one authorization batch. Zero uses MaxBatchCalls (20).
	MaxBatchCalls int
}

// NewClient builds a market client. Every configured issuer must be an
// absolute http(s) origin; a malformed allowlist is an error.
func NewClient(cfg ClientConfig) (*Client, error) {
	issuers := make(map[string]struct{}, len(cfg.Issuers))
	for _, raw := range cfg.Issuers {
		normalized, err := NormalizeIssuer(raw)
		if err != nil {
			return nil, err
		}
		issuers[normalized] = struct{}{}
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultHTTPTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	ttl := cfg.JWKSCacheTTL
	if ttl <= 0 {
		ttl = defaultJWKSCacheTTL
	}
	maxBundle := cfg.MaxBundleBytes
	if maxBundle <= 0 {
		maxBundle = defaultMaxBundle
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	name := strings.TrimSpace(cfg.InstanceName)
	if name == "" {
		name = "Memory"
	}
	batchLimit := cfg.MaxBatchCalls
	if batchLimit <= 0 || batchLimit > MaxBatchCalls {
		batchLimit = MaxBatchCalls
	}
	c := &Client{
		issuers:        issuers,
		httpClient:     httpClient,
		jwks:           newJWKSCache(ttl, now),
		maxBundleBytes: maxBundle,
		now:            now,
		origin:         strings.TrimSpace(cfg.OriginID),
		instanceURL:    strings.TrimRight(strings.TrimSpace(cfg.InstanceURL), "/"),
		name:           name,
		version:        instanceVersion(cfg.InstanceVersion),
		log:            log,
		inProduct:      newInProductState(now, batchLimit),
	}
	return c, nil
}

func instanceVersion(configured string) string {
	if v := strings.TrimSpace(configured); v != "" {
		return v
	}
	return version.Version
}

// IsTrusted reports whether issuer (normalized) is in the allowlist.
func (c *Client) IsTrusted(issuer string) bool {
	normalized, err := NormalizeIssuer(issuer)
	if err != nil {
		return false
	}
	_, ok := c.issuers[normalized]
	return ok
}

// NormalizeIssuer validates an absolute http(s) origin and returns its canonical
// scheme://host[:port] form.
func NormalizeIssuer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("market issuer is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid market issuer %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid market issuer %q: scheme must be http or https", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid market issuer %q: missing host", raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("invalid market issuer %q: credentials are not allowed", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid market issuer %q: query and fragment are not allowed", raw)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("invalid market issuer %q: must be an origin with no path", raw)
	}
	if u.Opaque != "" {
		return "", fmt.Errorf("invalid market issuer %q: opaque URLs are not allowed", raw)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

// trustedIssuer validates and normalizes an issuer, returning ErrIssuerUntrusted
// without making any outbound request when it is not allowlisted.
func (c *Client) trustedIssuer(issuer string) (string, error) {
	normalized, err := NormalizeIssuer(issuer)
	if err != nil || !c.IsTrusted(normalized) {
		return "", ErrIssuerUntrusted(issuer)
	}
	return normalized, nil
}

// Catalog fetches GET <issuer>/v1/catalog.
func (c *Client) Catalog(ctx context.Context, issuer string) (*Catalog, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	var out Catalog
	if err := c.doJSON(ctx, http.MethodGet, base+"/v1/catalog", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Listing fetches GET <issuer>/v1/listings/{slug}.
func (c *Client) Listing(ctx context.Context, issuer, slug string) (*Listing, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(slug) == "" {
		return nil, ErrInstallCodeInvalid("listing slug is required")
	}
	endpoint := base + "/v1/listings/" + url.PathEscape(slug)
	var envelope struct {
		Listing *Listing `json:"listing"`
	}
	body, err := c.do(ctx, http.MethodGet, endpoint, nil, "")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Listing != nil {
		return envelope.Listing, nil
	}
	var direct Listing
	if err := json.Unmarshal(body, &direct); err != nil {
		return nil, ErrMarketUnreachable("market returned an invalid listing document")
	}
	return &direct, nil
}

// Version fetches GET <issuer>/v1/listings/{slug}/versions/{semver}. This is
// the document the review and the tool-licence check read: compatibility and
// the capabilities/manifest live on the version, not on the exchange's card.
func (c *Client) Version(ctx context.Context, issuer, slug, semver string) (*Version, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(slug) == "" || strings.TrimSpace(semver) == "" {
		return nil, ErrInstallCodeInvalid("listing slug and version are required")
	}
	endpoint := base + "/v1/listings/" + url.PathEscape(slug) + "/versions/" + url.PathEscape(semver)
	body, err := c.do(ctx, http.MethodGet, endpoint, nil, "application/json")
	if err != nil {
		return nil, err
	}
	var v Version
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, ErrMarketUnreachable("market returned an invalid version document")
	}
	return &v, nil
}

// Exchange redeems the redirect code at the issuer that minted it. The issuer
// allowlist is enforced before the outbound POST.
func (c *Client) Exchange(ctx context.Context, issuer, code string) (*ExchangeResponse, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrInstallCodeInvalid("install code is required")
	}
	payload, err := json.Marshal(ExchangeRequest{Code: code})
	if err != nil {
		return nil, ErrMarketUnreachable("failed to build exchange request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/installs/exchange", bytes.NewReader(payload))
	if err != nil {
		return nil, ErrMarketUnreachable("failed to build exchange request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, ErrMarketUnreachable("market exchange failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBody+1))
	if err != nil {
		return nil, ErrMarketUnreachable("failed to read market exchange response")
	}
	if len(body) > maxJSONBody {
		return nil, ErrMarketUnreachable("market exchange response is too large")
	}
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusUnprocessableEntity {
		return nil, ErrInstallCodeInvalid("install code is invalid, expired or already used")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrMarketRateLimited("install code exchange is rate limited")
	}
	if resp.StatusCode == http.StatusConflict {
		return nil, ErrInstallNotInstallable("market refused the install as not installable")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrMarketUnreachable(fmt.Sprintf("market exchange returned HTTP %d", resp.StatusCode))
	}
	var out ExchangeResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, ErrMarketUnreachable("market exchange returned an invalid document")
	}
	out.Raw = append(json.RawMessage(nil), body...)
	out.Normalize()
	return &out, nil
}

// DownloadBundle fetches the licensed archive. The bundle URL must be on the
// same normalized origin as the trusted issuer, and the licence is sent as a
// bearer credential. The response is capped at the configured bundle limit.
func (c *Client) DownloadBundle(ctx context.Context, issuer, bundleURL, license string) ([]byte, error) {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bundleURL) == "" {
		return nil, ErrMarketUnreachable("market did not provide a bundle URL")
	}
	if !sameOrigin(bundleURL, base) {
		return nil, ErrMarketUnreachable("bundle URL is not on the trusted market origin")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bundleURL, nil)
	if err != nil {
		return nil, ErrMarketUnreachable("failed to build bundle request")
	}
	if license != "" {
		req.Header.Set("Authorization", "Bearer "+license)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, ErrMarketUnreachable("bundle download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrLicenseInvalid("market refused the install license")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrMarketUnreachable(fmt.Sprintf("bundle download returned HTTP %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBundleBytes+1))
	if err != nil {
		return nil, ErrMarketUnreachable("failed to read bundle")
	}
	if int64(len(data)) > c.maxBundleBytes {
		return nil, ErrMarketUnreachable("bundle exceeds the size limit")
	}
	return data, nil
}

// VerifyDigest compares the SHA-256 of data with the licensed digest (with or
// without a "sha256:" prefix). A mismatch returns ErrDigestMismatch.
func VerifyDigest(expected string, data []byte) error {
	want := CleanDigest(expected)
	if want == "" {
		return ErrLicenseInvalid("license carries no archive digest")
	}
	wantBytes, err := hex.DecodeString(want)
	if err != nil || len(wantBytes) != sha256.Size {
		return ErrLicenseInvalid("license archive digest is malformed")
	}
	sum := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(sum[:], wantBytes) != 1 {
		return ErrDigestMismatch("sha256:"+want, "sha256:"+hex.EncodeToString(sum[:]))
	}
	return nil
}

// ReportInstall posts the install outcome to POST /v1/installs. Reporting is
// best-effort at the call site; an error here never un-applies an install.
func (c *Client) ReportInstall(ctx context.Context, issuer string, report ReportRequest, license string) error {
	base, err := c.trustedIssuer(issuer)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return ErrMarketUnreachable("failed to build install report")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/installs", bytes.NewReader(payload))
	if err != nil {
		return ErrMarketUnreachable("failed to build install report")
	}
	req.Header.Set("Content-Type", "application/json")
	if license != "" {
		req.Header.Set("Authorization", "Bearer "+license)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ErrMarketUnreachable("install report failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxJSONBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrMarketUnreachable(fmt.Sprintf("install report returned HTTP %d", resp.StatusCode))
	}
	return nil
}

// sameOrigin reports whether raw is an absolute http(s) URL on base's exact
// origin. It allows a path because a bundle URL is a resource, not an issuer.
func sameOrigin(raw, base string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.User != nil {
		return false
	}
	return strings.ToLower(u.Scheme)+"://"+strings.ToLower(u.Host) == base
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, payload []byte, out any) error {
	body, err := c.do(ctx, method, endpoint, payload, "application/json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return ErrMarketUnreachable("market returned an invalid JSON document")
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, endpoint string, payload []byte, contentType string) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, ErrMarketUnreachable("failed to build market request")
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrMarketUnreachable(fmt.Sprintf("market returned HTTP %d", resp.StatusCode))
	}
	return body, nil
}

// originID returns the instance's stable registration identity. A client built
// without one generates it lazily and keeps it for its lifetime, so a stock
// instance registers without an operator editing a file.
func (c *Client) originID() string {
	c.originOnce.Do(func() {
		if c.origin == "" {
			c.origin = newOriginID()
		}
	})
	return c.origin
}
