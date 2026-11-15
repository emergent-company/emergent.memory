package market

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type testMarket struct {
	server   *httptest.Server
	key      *ecdsa.PrivateKey
	kid      string
	issuer   string
	jwksHits atomic.Int32
	reports  atomic.Int32
}

func newTestMarket(t *testing.T, kid string) *testMarket {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	m := &testMarket{key: key, kid: kid}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/market-keys.json", func(w http.ResponseWriter, r *http.Request) {
		m.jwksHits.Add(1)
		writeJSON(w, map[string]any{"keys": []map[string]string{jwkFor(key, kid)}})
	})
	mux.HandleFunc("/v1/installs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		m.reports.Add(1)
		writeJSON(w, map[string]any{"ok": true})
	})
	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	m.issuer = m.server.URL
	return m
}

func (m *testMarket) license(t *testing.T, claims map[string]any) string {
	t.Helper()
	return signLicense(t, m.key, m.kid, claims)
}

func (m *testMarket) addInstallRoutes(t *testing.T, slug, digest string, bundle []byte, license string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/market-keys.json", func(w http.ResponseWriter, r *http.Request) {
		m.jwksHits.Add(1)
		writeJSON(w, map[string]any{"keys": []map[string]string{jwkFor(m.key, m.kid)}})
	})
	mux.HandleFunc("/v1/installs/exchange", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("exchange content-type = %q", r.Header.Get("Content-Type"))
		}
		writeJSON(w, map[string]any{
			"install_id": "inst-1",
			"license":    license,
			"bundle_url": m.issuer + "/bundle",
			"digest":     digest,
			"listing": map[string]any{
				"slug":      slug,
				"version":   "1.2.3",
				"kind":      "blueprint",
				"delivery":  "install",
				"compat":    map[string]any{"memory": ">=0.1 <99.0"},
				"publisher": map[string]any{"slug": "acme", "name": "Acme"},
				"listing":   map[string]any{"title": "Test Listing"},
				"capabilities": map[string]any{
					"reads":  []string{"documents"},
					"writes": []string{"graph"},
					"tools":  []string{"search"},
				},
			},
		})
	})
	mux.HandleFunc("/bundle", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+license {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(bundle)
	})
	mux.HandleFunc("/v1/installs", func(w http.ResponseWriter, r *http.Request) {
		m.reports.Add(1)
		writeJSON(w, map[string]any{"ok": true})
	})
	// Swap the server's handler in place; httptest.Server starts with the
	// constructor's mux, so construct a fresh one for install routes.
	m.server.Config.Handler = mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jwkFor(key *ecdsa.PrivateKey, kid string) map[string]string {
	return map[string]string{
		"kty": "EC",
		"kid": kid,
		"alg": "ES256",
		"use": "sig",
		"crv": "P-256",
		"x":   b64(pad32(key.X)),
		"y":   b64(pad32(key.Y)),
	}
}

func signLicense(t *testing.T, key *ecdsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "ES256", "typ": "JWT", "kid": kid}
	hb, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signingInput := b64(hb) + "." + b64(cb)
	sum := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sig := append(pad32(r), pad32(s)...)
	return signingInput + "." + b64(sig)
}

func pad32(n *big.Int) []byte {
	out := make([]byte, 32)
	b := n.Bytes()
	copy(out[32-len(b):], b)
	return out
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func baseClaims(issuer string, digest string, exp time.Time) map[string]any {
	return map[string]any{
		"iss":          issuer,
		"aud":          LicenseAudience,
		"exp":          exp.Unix(),
		"iat":          time.Now().Add(-time.Minute).Unix(),
		"jti":          "jti-1",
		"digest":       "sha256:" + digest,
		"listing_slug": "demo",
		"semver":       "1.2.3",
	}
}

func expectCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", want)
	}
	if got := ErrorCode(err); got != want {
		t.Fatalf("error code = %q, want %q (err: %v)", got, want, err)
	}
}

func TestExchangeVerifyDownloadAndReport(t *testing.T) {
	m := newTestMarket(t, "k1")
	bundle := []byte("canonical tar bytes")
	sum := sha256.Sum256(bundle)
	digest := fmt.Sprintf("%x", sum)
	license := m.license(t, baseClaims(m.issuer, digest, time.Now().Add(time.Hour)))
	m.addInstallRoutes(t, "demo", "sha256:"+digest, bundle, license)

	client, err := NewClient(ClientConfig{Issuers: []string{m.issuer}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx := context.Background()

	exchange, err := client.Exchange(ctx, m.issuer, "code-1")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if exchange.Listing.Slug != "demo" || exchange.InstallID != "inst-1" {
		t.Fatalf("unexpected exchange: %+v", exchange)
	}
	claims, err := client.VerifyLicense(ctx, m.issuer, exchange.License)
	if err != nil {
		t.Fatalf("verify license: %v", err)
	}
	if CleanDigest(claims.Digest) != digest {
		t.Fatalf("license digest = %q, want %q", claims.Digest, digest)
	}
	data, err := client.DownloadBundle(ctx, m.issuer, exchange.BundleURL, exchange.License)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if err := VerifyDigest(claims.Digest, data); err != nil {
		t.Fatalf("digest should verify: %v", err)
	}
	if err := client.ReportInstall(ctx, m.issuer, ReportRequest{InstallID: "inst-1", State: "applied"}, exchange.License); err != nil {
		t.Fatalf("report: %v", err)
	}
	if m.reports.Load() != 1 {
		t.Fatalf("reports = %d, want 1", m.reports.Load())
	}
}

func TestUntrustedIssuerMakesNoOutboundCall(t *testing.T) {
	trusted := newTestMarket(t, "k1")
	untrusted := newTestMarket(t, "k2")
	client, err := NewClient(ClientConfig{Issuers: []string{trusted.issuer}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx := context.Background()

	_, err = client.Exchange(ctx, untrusted.issuer, "code")
	expectCode(t, err, CodeIssuerUntrusted)
	_, err = client.DownloadBundle(ctx, untrusted.issuer, untrusted.issuer+"/bundle", "lic")
	expectCode(t, err, CodeIssuerUntrusted)
	_, err = client.VerifyLicense(ctx, untrusted.issuer, "a.b.c")
	expectCode(t, err, CodeIssuerUntrusted)

	if untrusted.jwksHits.Load() != 0 {
		t.Fatalf("untrusted market received %d JWKS hits, want 0", untrusted.jwksHits.Load())
	}
}

func TestDevLicenseDoesNotVerifyAtProd(t *testing.T) {
	dev := newTestMarket(t, "dev-key")
	prod := newTestMarket(t, "prod-key")
	client, err := NewClient(ClientConfig{Issuers: []string{dev.issuer, prod.issuer}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	devToken := dev.license(t, baseClaims(dev.issuer, digest, time.Now().Add(time.Hour)))
	_, err = client.VerifyLicense(context.Background(), prod.issuer, devToken)
	expectCode(t, err, CodeLicenseInvalid)

	prodToken := prod.license(t, baseClaims(prod.issuer, digest, time.Now().Add(time.Hour)))
	_, err = client.VerifyLicense(context.Background(), dev.issuer, prodToken)
	expectCode(t, err, CodeLicenseInvalid)
}

func TestBadIssuerAndExpiredLicense(t *testing.T) {
	m := newTestMarket(t, "k1")
	client, err := NewClient(ClientConfig{Issuers: []string{m.issuer}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	digest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	claims := baseClaims(m.issuer, digest, time.Now().Add(time.Hour))
	claims["iss"] = "https://someone-else.example"
	_, err = client.VerifyLicense(context.Background(), m.issuer, m.license(t, claims))
	expectCode(t, err, CodeLicenseInvalid)

	expired := baseClaims(m.issuer, digest, time.Now().Add(-time.Hour))
	_, err = client.VerifyLicense(context.Background(), m.issuer, m.license(t, expired))
	expectCode(t, err, CodeLicenseInvalid)

	badAud := baseClaims(m.issuer, digest, time.Now().Add(time.Hour))
	badAud["aud"] = "something-else"
	_, err = client.VerifyLicense(context.Background(), m.issuer, m.license(t, badAud))
	expectCode(t, err, CodeLicenseInvalid)
}

func TestDigestMismatch(t *testing.T) {
	data := []byte("tampered")
	err := VerifyDigest("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", data)
	expectCode(t, err, CodeDigestMismatch)
}

func TestJWKSCacheIsPerIssuer(t *testing.T) {
	a := newTestMarket(t, "a")
	b := newTestMarket(t, "b")
	client, err := NewClient(ClientConfig{Issuers: []string{a.issuer, b.issuer}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	digest := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := client.VerifyLicense(ctx, a.issuer, a.license(t, baseClaims(a.issuer, digest, time.Now().Add(time.Hour)))); err != nil {
			t.Fatalf("verify a: %v", err)
		}
	}
	if got := a.jwksHits.Load(); got != 1 {
		t.Fatalf("issuer a JWKS hits = %d, want 1 (cached)", got)
	}
	if _, err := client.VerifyLicense(ctx, b.issuer, b.license(t, baseClaims(b.issuer, digest, time.Now().Add(time.Hour)))); err != nil {
		t.Fatalf("verify b: %v", err)
	}
	if got := b.jwksHits.Load(); got != 1 {
		t.Fatalf("issuer b JWKS hits = %d, want 1", got)
	}
}
