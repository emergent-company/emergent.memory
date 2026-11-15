package market

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// inProductClock is a movable clock shared by the client and the fake market.
type inProductClock struct {
	mu      sync.Mutex
	current time.Time
}

func newInProductClock() *inProductClock {
	return &inProductClock{current: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *inProductClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *inProductClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(delta)
}

// signInProductJWT is the error-returning test signer (signLicense fails the
// test, which is awkward inside an HTTP handler goroutine).
func signInProductJWT(key *ecdsa.PrivateKey, kid string, claims map[string]any) (string, error) {
	header := map[string]any{"alg": "ES256", "typ": "JWT", "kid": kid}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64(headerBytes) + "." + b64(claimBytes)
	sum := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	signature := append(pad32(r), pad32(s)...)
	return signingInput + "." + b64(signature), nil
}

// fakeInProductMarket is a programmable market for the in-product contract.
type fakeInProductMarket struct {
	t      *testing.T
	clock  *inProductClock
	server *httptest.Server

	mu      sync.Mutex
	key     *ecdsa.PrivateKey
	kid     string
	offline bool

	jwksHits      int
	registrations []RegisterRequest
	catalogCalls  []url.Values
	authorizes    []authorizeRequest
	issued        map[string]struct{}
	usageBodies   []string

	catalogHandler   func(url.Values) (any, int)
	trustHandler     func(string) (any, int)
	claimHandler     func(claimRequest) (any, int)
	authorizeHandler func(string, authorizeRequest) (any, int)
	usageHandler     func(UsageReport) (any, int)
	registerHandler  func(RegisterRequest) (any, int)
}

func newFakeInProductMarket(t *testing.T, clock *inProductClock) *fakeInProductMarket {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	market := &fakeInProductMarket{t: t, clock: clock, key: key, kid: "k1", issued: make(map[string]struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/market-keys.json", market.handleJWKS)
	mux.HandleFunc("/v1/instances", market.handleRegister)
	mux.HandleFunc("/v1/catalog", market.handleCatalog)
	mux.HandleFunc("/v1/listings/", market.handleTrust)
	mux.HandleFunc("/v1/entitlements/claim", market.handleClaim)
	mux.HandleFunc("/v1/entitlements/", market.handleAuthorize)
	mux.HandleFunc("/v1/usage", market.handleUsage)
	market.server = httptest.NewServer(mux)
	t.Cleanup(market.server.Close)
	return market
}

func (f *fakeInProductMarket) issuer() string { return f.server.URL }

func (f *fakeInProductMarket) setOffline(offline bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = offline
}

func (f *fakeInProductMarket) unavailable(w http.ResponseWriter) bool {
	f.mu.Lock()
	offline := f.offline
	f.mu.Unlock()
	if offline {
		http.Error(w, "market is offline", http.StatusServiceUnavailable)
		return true
	}
	return false
}

func (f *fakeInProductMarket) keySnapshot() (*ecdsa.PrivateKey, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.key, f.kid
}

func (f *fakeInProductMarket) rotateKey() {
	f.mu.Lock()
	defer f.mu.Unlock()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.t.Errorf("rotate key: %v", err)
		return
	}
	// The same kid is deliberate: a cached key set must be refreshed when the
	// key material behind a kid rotates.
	f.key = key
}

func (f *fakeInProductMarket) jwksHitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jwksHits
}

func (f *fakeInProductMarket) lastRegistration() (RegisterRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.registrations) == 0 {
		return RegisterRequest{}, false
	}
	return f.registrations[len(f.registrations)-1], true
}

func (f *fakeInProductMarket) lastCatalogQuery() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.catalogCalls) == 0 {
		return nil
	}
	return f.catalogCalls[len(f.catalogCalls)-1]
}

func (f *fakeInProductMarket) lastAuthorizeRequest() (authorizeRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.authorizes) == 0 {
		return authorizeRequest{}, false
	}
	return f.authorizes[len(f.authorizes)-1], true
}

func (f *fakeInProductMarket) authorizeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.authorizes)
}

func (f *fakeInProductMarket) usageReports() []UsageReport {
	f.mu.Lock()
	defer f.mu.Unlock()
	reports := make([]UsageReport, 0, len(f.usageBodies))
	for _, raw := range f.usageBodies {
		var report UsageReport
		if err := json.Unmarshal([]byte(raw), &report); err == nil {
			reports = append(reports, report)
		}
	}
	return reports
}

func (f *fakeInProductMarket) handleJWKS(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	key, kid := f.keySnapshot()
	f.mu.Lock()
	f.jwksHits++
	f.mu.Unlock()
	writeJSON(w, map[string]any{"keys": []map[string]string{jwkFor(key, kid)}})
}

func (f *fakeInProductMarket) handleRegister(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		f.t.Errorf("decode registration: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	request.OriginID = strings.TrimSpace(request.OriginID)
	f.mu.Lock()
	f.registrations = append(f.registrations, request)
	handler := f.registerHandler
	f.mu.Unlock()
	if request.OriginID == "" {
		f.t.Errorf("registration carried no origin_id")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if handler != nil {
		body, status := handler(request)
		w.WriteHeader(status)
		writeJSON(w, body)
		return
	}
	writeJSON(w, map[string]any{
		"instance_id": "instance-1",
		"origin_id":   request.OriginID,
		"account_id":  "account-1",
	})
}

func (f *fakeInProductMarket) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	query := r.URL.Query()
	f.mu.Lock()
	f.catalogCalls = append(f.catalogCalls, query)
	handler := f.catalogHandler
	f.mu.Unlock()
	if handler == nil {
		writeJSON(w, map[string]any{"listings": []any{}})
		return
	}
	body, status := handler(query)
	w.WriteHeader(status)
	writeJSON(w, body)
}

func (f *fakeInProductMarket) handleTrust(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	raw := strings.TrimPrefix(r.URL.Path, "/v1/listings/")
	slug, ok := strings.CutSuffix(raw, "/trust")
	if !ok || slug == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if unescaped, err := url.PathUnescape(slug); err == nil {
		slug = unescaped
	}
	f.mu.Lock()
	handler := f.trustHandler
	f.mu.Unlock()
	if handler == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, status := handler(slug)
	w.WriteHeader(status)
	writeJSON(w, body)
}

func (f *fakeInProductMarket) handleClaim(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request claimRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		f.t.Errorf("decode claim: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	handler := f.claimHandler
	f.mu.Unlock()
	if handler != nil {
		body, status := handler(request)
		w.WriteHeader(status)
		writeJSON(w, body)
		return
	}
	writeJSON(w, map[string]any{
		"entitlement": map[string]any{
			"id":           "ent-1",
			"listing_slug": request.ListingSlug,
			"origin_id":    request.OriginID,
			"state":        "active",
		},
	})
}

func (f *fakeInProductMarket) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	raw := strings.TrimPrefix(r.URL.Path, "/v1/entitlements/")
	entitlementID, ok := strings.CutSuffix(raw, "/authorize")
	if !ok || entitlementID == "" || strings.Contains(entitlementID, "/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if unescaped, err := url.PathUnescape(entitlementID); err == nil {
		entitlementID = unescaped
	}
	var request authorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		f.t.Errorf("decode authorize: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.authorizes = append(f.authorizes, request)
	handler := f.authorizeHandler
	f.mu.Unlock()
	if handler != nil {
		body, status := handler(entitlementID, request)
		w.WriteHeader(status)
		writeJSON(w, body)
		return
	}
	body, status := f.defaultAuthorize(entitlementID, request)
	w.WriteHeader(status)
	writeJSON(w, body)
}

func (f *fakeInProductMarket) defaultAuthorize(entitlementID string, request authorizeRequest) (any, int) {
	requestIDs := make([]string, request.Calls)
	for index := range requestIDs {
		requestIDs[index] = fmt.Sprintf("%s-req-%d", entitlementID, index+1)
	}
	key, kid := f.keySnapshot()
	now := f.clock.Now()
	claims := map[string]any{
		"iss":            f.issuer(),
		"aud":            AssertionAudience,
		"sub":            request.OriginID,
		"exp":            now.Add(time.Duration(request.TTLSeconds) * time.Second).Unix(),
		"iat":            now.Unix(),
		"jti":            "assert-1",
		"entitlement_id": entitlementID,
		"listing_slug":   "weather",
		"origin_id":      request.OriginID,
		"request_ids":    requestIDs,
	}
	token, err := signInProductJWT(key, kid, claims)
	if err != nil {
		f.t.Errorf("sign assertion: %v", err)
		return map[string]string{"error": "sign"}, http.StatusInternalServerError
	}
	f.mu.Lock()
	for _, id := range requestIDs {
		f.issued[id] = struct{}{}
	}
	f.mu.Unlock()
	return map[string]any{"assertion": token, "request_ids": requestIDs}, http.StatusOK
}

func (f *fakeInProductMarket) handleUsage(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("read usage body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var report UsageReport
	if err := json.Unmarshal(raw, &report); err != nil {
		f.t.Errorf("decode usage report: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.usageBodies = append(f.usageBodies, string(raw))
	handler := f.usageHandler
	_, known := f.issued[report.RequestID]
	f.mu.Unlock()
	if handler != nil {
		body, status := handler(report)
		w.WriteHeader(status)
		writeJSON(w, body)
		return
	}
	if !known {
		w.WriteHeader(http.StatusUnprocessableEntity)
		writeJSON(w, map[string]any{"code": CodeUsageRequestUnknown})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// trustToken signs a valid listing trust document for slug.
func (f *fakeInProductMarket) trustToken(t *testing.T, slug string) string {
	t.Helper()
	key, kid := f.keySnapshot()
	now := f.clock.Now()
	claims := map[string]any{
		"iss":          f.issuer(),
		"aud":          TrustAudience,
		"sub":          "listing:" + slug,
		"exp":          now.Add(time.Hour).Unix(),
		"iat":          now.Unix(),
		"jti":          "trust-1",
		"listing_slug": slug,
		"publisher":    "acme",
		"kind":         "mcp",
		"delivery":     "hosted",
		"status":       TrustStatusActive,
	}
	token, err := signInProductJWT(key, kid, claims)
	if err != nil {
		t.Fatalf("sign trust document: %v", err)
	}
	return token
}

func (f *fakeInProductMarket) newAssertion(t *testing.T, entitlementID, slug string, requestIDs []string, ttl time.Duration) string {
	t.Helper()
	key, kid := f.keySnapshot()
	now := f.clock.Now()
	claims := map[string]any{
		"iss":            f.issuer(),
		"aud":            AssertionAudience,
		"exp":            now.Add(ttl).Unix(),
		"iat":            now.Unix(),
		"jti":            "assert-1",
		"entitlement_id": entitlementID,
		"listing_slug":   slug,
		"origin_id":      "origin-test",
		"request_ids":    requestIDs,
	}
	token, err := signInProductJWT(key, kid, claims)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	return token
}

func newTestInProductClient(t *testing.T, market *fakeInProductMarket, clock *inProductClock, logs *bytes.Buffer) *InProductClient {
	t.Helper()
	cfg := InProductConfig{
		Issuer:       market.issuer(),
		OriginID:     "origin-test",
		InstanceName: "test-instance",
		Now:          clock.Now,
	}
	if logs != nil {
		cfg.Log = slog.New(slog.NewTextHandler(logs, nil))
	}
	client, err := NewInProductClient(cfg)
	if err != nil {
		t.Fatalf("new in-product client: %v", err)
	}
	return client
}

func i64ptr(value int) *int { return &value }

func boolptr(value bool) *bool { return &value }

func TestInProductAcquireFreeHostedAndReportUsage(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	ctx := context.Background()

	var servedMu sync.Mutex
	var served []string
	sellerClient, err := NewClient(ClientConfig{Issuers: []string{market.issuer()}, Now: clock.Now})
	if err != nil {
		t.Fatalf("seller client: %v", err)
	}
	seller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		token := strings.TrimPrefix(r.Header.Get(HeaderAssertion), "Bearer ")
		claims, err := sellerClient.VerifyAssertion(r.Context(), market.issuer(), token)
		if err != nil {
			http.Error(w, "bad assertion: "+err.Error(), http.StatusUnauthorized)
			return
		}
		if claims.ListingSlug != "weather" {
			http.Error(w, "assertion names "+claims.ListingSlug, http.StatusForbidden)
			return
		}
		requestID := strings.TrimSpace(r.Header.Get(HeaderRequestID))
		if !containsRequestID(claims.RequestIDs, requestID) {
			http.Error(w, "request_id is not covered by the assertion", http.StatusBadRequest)
			return
		}
		servedMu.Lock()
		served = append(served, requestID)
		servedMu.Unlock()
		w.Header().Set(HeaderRequestID, requestID)
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "result": map[string]any{"ok": true}})
	}))
	t.Cleanup(seller.Close)

	market.catalogHandler = func(query url.Values) (any, int) {
		return map[string]any{"listings": []map[string]any{
			{"slug": "paid", "kind": "mcp", "delivery": "hosted", "free": false, "endpoint": seller.URL},
			{"slug": "blueprint", "kind": "blueprint", "delivery": "hosted", "free": true, "endpoint": seller.URL},
			{"slug": "install", "kind": "mcp", "delivery": "install", "free": true, "endpoint": seller.URL},
			{"slug": "weather", "kind": "mcp", "delivery": "hosted", "free": true, "endpoint": seller.URL},
		}}, http.StatusOK
	}
	market.trustHandler = func(slug string) (any, int) {
		if slug != "weather" {
			return nil, http.StatusNotFound
		}
		return map[string]any{"trust": market.trustToken(t, slug)}, http.StatusOK
	}
	market.claimHandler = func(request claimRequest) (any, int) {
		return map[string]any{"entitlement": map[string]any{
			"id":           "ent-1",
			"listing_slug": request.ListingSlug,
			"origin_id":    request.OriginID,
			"state":        "active",
		}}, http.StatusOK
	}

	var logs bytes.Buffer
	client := newTestInProductClient(t, market, clock, &logs)

	acquisition, err := client.AcquireFreeHosted(ctx, BrowseQuery{}, 20)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquisition.Listing.Slug != "weather" {
		t.Fatalf("listing = %q, want weather", acquisition.Listing.Slug)
	}
	if acquisition.Trust == nil || acquisition.Trust.ListingSlug != "weather" || acquisition.Trust.Status != TrustStatusActive {
		t.Fatalf("trust = %+v", acquisition.Trust)
	}
	if acquisition.Entitlement == nil || acquisition.Entitlement.ID != "ent-1" || acquisition.Entitlement.AlreadyHeld {
		t.Fatalf("entitlement = %+v", acquisition.Entitlement)
	}
	if len(acquisition.Batch.RequestIDs) != 20 {
		t.Fatalf("request ids = %d, want 20", len(acquisition.Batch.RequestIDs))
	}
	seenIDs := make(map[string]struct{}, len(acquisition.Batch.RequestIDs))
	for _, id := range acquisition.Batch.RequestIDs {
		if _, dup := seenIDs[id]; dup {
			t.Fatalf("duplicate request id %q in batch", id)
		}
		seenIDs[id] = struct{}{}
	}

	query := market.lastCatalogQuery()
	if query.Get("kind") != "mcp" || query.Get("delivery") != "hosted" || query.Get("free") != "true" {
		t.Fatalf("catalog query = %v", query)
	}
	registration, ok := market.lastRegistration()
	if !ok || registration.OriginID != client.OriginID() || registration.Name != "test-instance" {
		t.Fatalf("registration = %+v (ok=%v)", registration, ok)
	}
	authorizeRequest, ok := market.lastAuthorizeRequest()
	if !ok || authorizeRequest.Calls != 20 || authorizeRequest.TTLSeconds != int(MaxBatchTTL/time.Second) || authorizeRequest.OriginID != client.OriginID() {
		t.Fatalf("authorize request = %+v (ok=%v)", authorizeRequest, ok)
	}

	for index := 0; index < 2; index++ {
		response, err := client.CallSeller(ctx, acquisition.Entitlement.ID, seller.URL, []byte(`{"jsonrpc":"2.0","method":"tools/call"}`))
		if err != nil {
			t.Fatalf("call seller %d: %v", index, err)
		}
		if response.StatusCode != http.StatusOK || response.RequestID != acquisition.Batch.RequestIDs[index] {
			t.Fatalf("seller response = %+v", response)
		}
		if response.Outcome != UsageOutcomeOK {
			t.Fatalf("outcome = %q", response.Outcome)
		}
	}
	servedMu.Lock()
	if len(served) != 2 || served[0] != acquisition.Batch.RequestIDs[0] || served[1] != acquisition.Batch.RequestIDs[1] {
		t.Fatalf("seller served %v", served)
	}
	servedMu.Unlock()

	reports := market.usageReports()
	if len(reports) != 2 {
		t.Fatalf("usage reports = %d, want 2", len(reports))
	}
	for index, report := range reports {
		if report.RequestID != acquisition.Batch.RequestIDs[index] || report.EntitlementID != "ent-1" || report.ListingSlug != "weather" {
			t.Fatalf("usage report %d = %+v", index, report)
		}
		if report.Outcome != UsageOutcomeOK {
			t.Fatalf("usage outcome = %q", report.Outcome)
		}
	}
	if pending := client.PendingUsage(); pending != 0 {
		t.Fatalf("pending usage = %d, want 0", pending)
	}
}

func TestInProductClaimAlreadyHeldIsIdempotent(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	client := newTestInProductClient(t, market, clock, nil)
	ctx := context.Background()

	market.claimHandler = func(request claimRequest) (any, int) {
		return map[string]any{
			"code": CodeEntitlementAlreadyHeld,
			"entitlement": map[string]any{
				"id":           "ent-existing",
				"listing_slug": request.ListingSlug,
				"origin_id":    request.OriginID,
			},
		}, http.StatusConflict
	}
	entitlement, err := client.Claim(ctx, "weather")
	if err != nil {
		t.Fatalf("claim already held: %v", err)
	}
	if entitlement.ID != "ent-existing" || !entitlement.AlreadyHeld {
		t.Fatalf("entitlement = %+v", entitlement)
	}

	market.claimHandler = func(claimRequest) (any, int) {
		return map[string]any{"code": CodeEntitlementAlreadyHeld}, http.StatusConflict
	}
	_, err = client.Claim(ctx, "weather")
	expectCode(t, err, CodeEntitlementClaim)
}

func TestInProductAssertionVerifiesAgainstRotatedKey(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	client, err := NewClient(ClientConfig{Issuers: []string{market.issuer()}, Now: clock.Now})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx := context.Background()

	trustClaims, err := client.VerifyTrustToken(ctx, market.issuer(), market.trustToken(t, "weather"))
	if err != nil {
		t.Fatalf("verify trust before rotation: %v", err)
	}
	if trustClaims.ListingSlug != "weather" || market.jwksHitCount() != 1 {
		t.Fatalf("trust claims = %+v, jwks hits = %d", trustClaims, market.jwksHitCount())
	}

	// A rotation under the same kid must be picked up by the forced refresh;
	// move past the anti-hammer window first.
	clock.Advance(2 * time.Second)
	market.rotateKey()
	assertion := market.newAssertion(t, "ent-1", "weather", []string{"req-1", "req-2"}, MaxBatchTTL)
	claims, err := client.VerifyAssertion(ctx, market.issuer(), assertion)
	if err != nil {
		t.Fatalf("verify assertion after rotation: %v", err)
	}
	if len(claims.RequestIDs) != 2 || claims.EntitlementID != "ent-1" {
		t.Fatalf("assertion claims = %+v", claims)
	}
	if hits := market.jwksHitCount(); hits != 2 {
		t.Fatalf("JWKS hits = %d, want 2 (one refresh for the rotated key)", hits)
	}
}

func TestInProductExpiredBatchIsRefused(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	var logs bytes.Buffer
	client := newTestInProductClient(t, market, clock, &logs)
	ctx := context.Background()

	batch, err := client.Authorize(ctx, "ent-1", 2)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	clock.Advance(MaxBatchTTL + time.Second)
	market.setOffline(true)

	_, err = client.NextInvocation(ctx, "ent-1")
	expectCode(t, err, CodeBatchExpired)
	if !strings.Contains(logs.String(), "cached authorization batch expired") {
		t.Fatalf("expiry was not logged: %s", logs.String())
	}

	// The same assertion is expired for a seller too, whichever path verifies it.
	_, err = client.VerifyAssertion(ctx, batch.Assertion)
	expectCode(t, err, CodeAuthorizationInvalid)
}

func TestInProductUsageRefusesUnissuedAndDuplicatedRequestIDs(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	client := newTestInProductClient(t, market, clock, nil)
	ctx := context.Background()

	batch, err := client.Authorize(ctx, "ent-1", 2)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}

	// A request_id the market never issued is refused before any outbound call.
	err = client.ReportUsage(ctx, UsageReport{RequestID: "fabricated", EntitlementID: "ent-1"})
	expectCode(t, err, CodeUsageRequestUnknown)
	if got := len(market.usageReports()); got != 0 {
		t.Fatalf("fabricated report reached the market %d times", got)
	}

	// An id issued under another entitlement is unknown for this one.
	err = client.ReportUsage(ctx, UsageReport{RequestID: batch.RequestIDs[0], EntitlementID: "ent-other"})
	expectCode(t, err, CodeUsageRequestUnknown)

	if err := client.ReportUsage(ctx, UsageReport{RequestID: batch.RequestIDs[0], EntitlementID: "ent-1"}); err != nil {
		t.Fatalf("first report: %v", err)
	}
	err = client.ReportUsage(ctx, UsageReport{RequestID: batch.RequestIDs[0], EntitlementID: "ent-1"})
	expectCode(t, err, CodeUsageRequestDuplicate)
	if got := len(market.usageReports()); got != 1 {
		t.Fatalf("usage reports = %d, want 1", got)
	}

	// If the market disagrees with the instance's record, its refusal is
	// surfaced rather than swallowed.
	market.usageHandler = func(UsageReport) (any, int) {
		return map[string]any{"code": CodeUsageRequestUnknown}, http.StatusUnprocessableEntity
	}
	err = client.ReportUsage(ctx, UsageReport{RequestID: batch.RequestIDs[1], EntitlementID: "ent-1"})
	expectCode(t, err, CodeUsageRequestUnknown)
}

func TestInProductOutageServesCachedBatchAndDefersUsage(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	var logs bytes.Buffer
	client := newTestInProductClient(t, market, clock, &logs)
	ctx := context.Background()

	batch, err := client.Authorize(ctx, "ent-1", 3)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	clock.Advance(10 * time.Second)
	market.setOffline(true)

	var reported []string
	for index := 0; index < 2; index++ {
		invocation, err := client.NextInvocation(ctx, "ent-1")
		if err != nil {
			t.Fatalf("next invocation %d during outage: %v", index, err)
		}
		if invocation.Assertion != batch.Assertion || invocation.RequestID != batch.RequestIDs[index] {
			t.Fatalf("invocation %d = %+v", index, invocation)
		}
		reported = append(reported, invocation.RequestID)
		if err := client.ReportUsage(ctx, UsageReport{RequestID: invocation.RequestID, EntitlementID: "ent-1"}); err != nil {
			t.Fatalf("report usage during outage: %v", err)
		}
	}
	if pending := client.PendingUsage(); pending != 2 {
		t.Fatalf("pending usage = %d, want 2", pending)
	}
	if !strings.Contains(logs.String(), "usage report deferred rather than dropped") {
		t.Fatalf("deferral was not logged: %s", logs.String())
	}

	// Asking for more calls than remain forces a market attempt; the live cache
	// covers what it can and says so.
	cached, err := client.EnsureBatch(ctx, "ent-1", 2)
	if err != nil {
		t.Fatalf("ensure from cache: %v", err)
	}
	if cached.Assertion != batch.Assertion {
		t.Fatalf("cache returned a different batch")
	}
	if !strings.Contains(logs.String(), "serving from cached authorization batch") {
		t.Fatalf("cache fallback was not logged: %s", logs.String())
	}

	// Still offline: flushing leaves the reports queued instead of dropping them.
	delivered, err := client.FlushUsage(ctx)
	if delivered != 0 {
		t.Fatalf("delivered while offline = %d", delivered)
	}
	expectCode(t, err, CodeMarketUnreach)
	if pending := client.PendingUsage(); pending != 2 {
		t.Fatalf("pending after failed flush = %d, want 2", pending)
	}

	market.setOffline(false)
	delivered, err = client.FlushUsage(ctx)
	if err != nil || delivered != 2 {
		t.Fatalf("flush after recovery = %d/%v", delivered, err)
	}
	if pending := client.PendingUsage(); pending != 0 {
		t.Fatalf("pending after recovery = %d, want 0", pending)
	}
	reports := market.usageReports()
	if len(reports) != 2 {
		t.Fatalf("usage reports = %d, want 2", len(reports))
	}
	for index, report := range reports {
		if report.RequestID != reported[index] {
			t.Fatalf("report %d request id = %q, want %q", index, report.RequestID, reported[index])
		}
	}
}

func TestInProductOutageWithoutCacheFailsLoudly(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	market.setOffline(true)
	var logs bytes.Buffer
	client := newTestInProductClient(t, market, clock, &logs)
	ctx := context.Background()

	_, err := client.EnsureBatch(ctx, "ent-1", 1)
	expectCode(t, err, CodeMarketUnreach)
	if !strings.Contains(logs.String(), "no authorization batch is cached") {
		t.Fatalf("missing cache was not logged: %s", logs.String())
	}
}

func TestInProductAuthorizeRejectsOversizedBatches(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	client := newTestInProductClient(t, market, clock, nil)
	ctx := context.Background()

	_, err := client.Authorize(ctx, "ent-1", MaxBatchCalls+1)
	expectCode(t, err, CodeAuthorizationInvalid)
	_, err = client.Authorize(ctx, "ent-1", 0)
	expectCode(t, err, CodeAuthorizationInvalid)
	if calls := market.authorizeCount(); calls != 0 {
		t.Fatalf("oversized batch reached the market %d times", calls)
	}
}

func TestInProductClientDefaultsToPublicMarket(t *testing.T) {
	client, err := NewInProductClient(InProductConfig{})
	if err != nil {
		t.Fatalf("new stock client: %v", err)
	}
	if client.Issuer() != DefaultIssuer {
		t.Fatalf("issuer = %q, want %q", client.Issuer(), DefaultIssuer)
	}
	if client.OriginID() == "" {
		t.Fatal("stock client has no origin_id")
	}
	if client.Registered() {
		t.Fatal("stock client should register lazily")
	}
	if !client.client.IsTrusted(DefaultIssuer) {
		t.Fatal("default issuer is not trusted by the stock client")
	}
}

func TestInProductBrowseFiltersBothAxes(t *testing.T) {
	clock := newInProductClock()
	market := newFakeInProductMarket(t, clock)
	client := newTestInProductClient(t, market, clock, nil)
	ctx := context.Background()

	market.catalogHandler = func(url.Values) (any, int) {
		return map[string]any{"listings": []map[string]any{
			{"slug": "hosted-mcp-free", "kind": "mcp", "delivery": "hosted", "pricing": map[string]any{"free": true}},
			{"slug": "hosted-mcp-paid", "kind": "mcp", "delivery": "hosted", "price_cents": i64ptr(499), "free": boolptr(false)},
			{"slug": "install-mcp", "kind": "mcp", "delivery": "install", "free": true},
			{"slug": "hosted-blueprint", "kind": "blueprint", "delivery": "hosted", "free": true},
			{"slug": "unknown-price", "kind": "mcp", "delivery": "hosted"},
		}}, http.StatusOK
	}

	catalog, err := client.Browse(ctx, BrowseQuery{Kind: "mcp", Delivery: "hosted", FreeOnly: true})
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if len(catalog.Listings) != 1 || catalog.Listings[0].Slug != "hosted-mcp-free" {
		t.Fatalf("filtered listings = %+v", catalog.Listings)
	}
}
