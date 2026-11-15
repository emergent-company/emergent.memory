package market

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// fakeInProductMarket is a controllable market for the in-product client tests.
// It implements the same routes the client calls and keeps the seller-side
// request_id discipline: usage is accepted only for an id it pre-issued.
type fakeInProductMarket struct {
	t       *testing.T
	server  *httptest.Server
	url     string
	key     *ecdsa.PrivateKey
	rotated *ecdsa.PrivateKey
	kid     string

	rotate  atomic.Bool
	offline atomic.Bool

	jwksHits      atomic.Int32
	authorizeHits atomic.Int32
	usageAttempts atomic.Int32
	usageAccepted atomic.Int32

	mu               sync.Mutex
	registrations    []map[string]any
	browseQueries    []url.Values
	claimBodies      []map[string]any
	authorizeBodies  []map[string]any
	usageBodies      []string
	issued           map[string]string
	claimStatus      int
	authorizeStatus  int
	usageStatus      int
	authorizeExpires time.Time
	batchCalls       int
	forgetIssued     bool
	trustToken       string
	assertionToken   string
	// authField makes the authorize response name the assertion `auth`, the
	// spelling in the marketplace design (§8.1), instead of `assertion`.
	authField bool
	// reissueIDs, when set, is the exact id list handed out for every batch, so
	// a test can make the market re-issue ids it already issued.
	reissueIDs []string
}

func newFakeInProductMarket(t *testing.T) *fakeInProductMarket {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	rotated, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate rotated key: %v", err)
	}
	m := &fakeInProductMarket{
		t:       t,
		key:     key,
		rotated: rotated,
		kid:     "k1",
		issued:  make(map[string]string),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/market-keys.json", func(w http.ResponseWriter, r *http.Request) {
		m.jwksHits.Add(1)
		key := m.key
		if m.rotate.Load() {
			key = m.rotated
		}
		writeJSON(w, map[string]any{"keys": []map[string]string{jwkFor(key, m.kid)}})
	})
	mux.HandleFunc(pathInstances, func(w http.ResponseWriter, r *http.Request) {
		if m.offline.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.registrations = append(m.registrations, body)
		m.mu.Unlock()
		origin := fmt.Sprint(body["origin_id"])
		writeJSON(w, map[string]any{
			"instance_id":   "instance-1",
			"origin_id":     origin,
			"registered_at": time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc(pathCatalog, func(w http.ResponseWriter, r *http.Request) {
		if m.offline.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		m.mu.Lock()
		m.browseQueries = append(m.browseQueries, r.URL.Query())
		trust := m.trustToken
		m.mu.Unlock()
		writeJSON(w, map[string]any{"listings": []map[string]any{
			{
				"slug":           "hosted-mcp",
				"kind":           KindMCP,
				"delivery":       DeliveryHosted,
				"title":          "Hosted MCP",
				"trust_document": trust,
			},
			{
				"slug":     "install-blueprint",
				"kind":     KindBlueprint,
				"delivery": DeliveryInstall,
				"title":    "Install Blueprint",
			},
		}})
	})
	mux.HandleFunc(pathEntitlements, func(w http.ResponseWriter, r *http.Request) {
		if m.offline.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		m.mu.Lock()
		status := m.claimStatus
		m.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.claimBodies = append(m.claimBodies, body)
		m.mu.Unlock()
		writeJSON(w, map[string]any{
			"id":           "ent-1",
			"listing_slug": "hosted-mcp",
			"status":       "active",
			"delivery":     DeliveryHosted,
		})
	})
	mux.HandleFunc("/v1/entitlements/", func(w http.ResponseWriter, r *http.Request) {
		if m.offline.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/authorize") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		m.authorizeHits.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.authorizeBodies = append(m.authorizeBodies, body)
		status := m.authorizeStatus
		expires := m.authorizeExpires
		batchCalls := m.batchCalls
		assertion := m.assertionToken
		m.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		calls := 20
		if raw, ok := body["calls"].(float64); ok && int(raw) > 0 {
			calls = int(raw)
		}
		if batchCalls > 0 && calls > batchCalls {
			calls = batchCalls
		}
		if calls > MaxBatchCalls {
			calls = MaxBatchCalls
		}
		ids := make([]string, 0, calls)
		m.mu.Lock()
		reissue := append([]string(nil), m.reissueIDs...)
		authField := m.authField
		if len(reissue) > 0 {
			ids = append(ids, reissue...)
		} else {
			for i := 1; i <= calls; i++ {
				ids = append(ids, fmt.Sprintf("req-%d", i))
			}
		}
		for _, id := range ids {
			m.issued[id] = "ent-1"
		}
		m.mu.Unlock()
		if expires.IsZero() {
			expires = time.Now().Add(MaxBatchTTL)
		}
		response := map[string]any{
			"entitlement_id": "ent-1",
			"request_ids":    ids,
			"expires_at":     expires.UTC().Format(time.RFC3339Nano),
			"remaining_usd":  12.40,
		}
		// The marketplace design (§8.1) names the assertion `auth`; the client
		// must accept that spelling as well as `assertion`.
		if authField {
			response["auth"] = assertion
		} else {
			response["assertion"] = assertion
		}
		writeJSON(w, response)
	})
	mux.HandleFunc("/v1/listings/", func(w http.ResponseWriter, r *http.Request) {
		if m.offline.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		m.mu.Lock()
		token := m.trustToken
		m.mu.Unlock()
		if token == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"trust_document": token})
	})
	mux.HandleFunc(pathUsage, func(w http.ResponseWriter, r *http.Request) {
		if m.offline.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		m.usageAttempts.Add(1)
		raw, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.usageBodies = append(m.usageBodies, string(raw))
		status := m.usageStatus
		forget := m.forgetIssued
		m.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		var report UsageReport
		if err := json.Unmarshal(raw, &report); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		entitlement, ok := m.issued[report.RequestID]
		m.mu.Unlock()
		if forget || !ok {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		if report.EntitlementID != "" && report.EntitlementID != entitlement {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.usageAccepted.Add(1)
		writeJSON(w, map[string]any{"ok": true})
	})
	m.server = httptest.NewServer(mux)
	m.url = m.server.URL
	t.Cleanup(m.server.Close)
	m.refreshTokens(t)
	return m
}

// refreshTokens signs one assertion and one trust document with the current
// key. Tests re-sign after a rotation with assertionFor/trustFor.
func (m *fakeInProductMarket) refreshTokens(t *testing.T) {
	t.Helper()
	m.mu.Lock()
	m.assertionToken = m.sign(t, m.key, map[string]any{
		"iss":            m.url,
		"aud":            "https://seller.example/mcp",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Add(-time.Minute).Unix(),
		"jti":            "assertion-1",
		"entitlement_id": "ent-1",
		"listing_slug":   "hosted-mcp",
		"request_ids":    requestIDs(20),
	})
	m.trustToken = m.sign(t, m.key, map[string]any{
		"iss":          m.url,
		"aud":          TrustDocumentAudience,
		"sub":          "hosted-mcp",
		"listing_slug": "hosted-mcp",
		"publisher":    "acme",
		"verified":     true,
		"exp":          time.Now().Add(time.Hour).Unix(),
		"iat":          time.Now().Add(-time.Minute).Unix(),
	})
	m.mu.Unlock()
}

func (m *fakeInProductMarket) sign(t *testing.T, key *ecdsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	return signLicense(t, key, m.kid, claims)
}

func (m *fakeInProductMarket) assertionFor(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	return m.sign(t, key, map[string]any{
		"iss":            m.url,
		"aud":            "https://seller.example/mcp",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Add(-time.Minute).Unix(),
		"jti":            "assertion-1",
		"entitlement_id": "ent-1",
		"listing_slug":   "hosted-mcp",
		"request_ids":    requestIDs(20),
	})
}

func requestIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, fmt.Sprintf("req-%d", i))
	}
	return ids
}

func (m *fakeInProductMarket) lastRegistration(t *testing.T) map[string]any {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.registrations) == 0 {
		t.Fatalf("market received no registration")
	}
	return m.registrations[len(m.registrations)-1]
}

func (m *fakeInProductMarket) lastBrowse(t *testing.T) url.Values {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.browseQueries) == 0 {
		t.Fatalf("market received no catalog request")
	}
	return m.browseQueries[len(m.browseQueries)-1]
}

func (m *fakeInProductMarket) lastClaim(t *testing.T) map[string]any {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.claimBodies) == 0 {
		t.Fatalf("market received no claim")
	}
	return m.claimBodies[len(m.claimBodies)-1]
}

func (m *fakeInProductMarket) lastUsage(t *testing.T) UsageReport {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.usageBodies) == 0 {
		t.Fatalf("market received no usage report")
	}
	var report UsageReport
	if err := json.Unmarshal([]byte(m.usageBodies[len(m.usageBodies)-1]), &report); err != nil {
		t.Fatalf("decode usage body: %v", err)
	}
	return report
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newInProductClient(t *testing.T, m *fakeInProductMarket, clock *fakeClock, logBuf *bytes.Buffer) *Client {
	t.Helper()
	var handler slog.Handler
	if logBuf != nil {
		handler = slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		handler = slog.DiscardHandler
	}
	client, err := NewClient(ClientConfig{
		Issuers: []string{m.url},
		Now:     clock.Now,
		Logger:  slog.New(handler),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

// TestInProductPathRegisterBrowseVerifyClaimAuthorizeServeAndReport exercises
// the whole stock-instance path. The instance has no configured origin id; it
// registers, browses the two axes, verifies the listing, claims it, authorizes
// a 20-call batch, the seller verifies the assertion against the market's key
// set and echoes a pre-issued request id, and usage is reported with that id.
func TestInProductPathRegisterBrowseVerifyClaimAuthorizeServeAndReport(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	registration, err := client.Register(ctx, m.url, RegisterRequest{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if registration.InstanceID != "instance-1" {
		t.Fatalf("instance id = %q", registration.InstanceID)
	}
	regBody := m.lastRegistration(t)
	origin := fmt.Sprint(regBody["origin_id"])
	if !strings.HasPrefix(origin, "inst_") {
		t.Fatalf("registration origin_id = %q, want a generated inst_ id", origin)
	}

	catalog, err := client.Browse(ctx, m.url, BrowseQuery{Kind: KindMCP, Delivery: DeliveryHosted})
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if len(catalog.Listings) != 1 || catalog.Listings[0].Slug != "hosted-mcp" {
		t.Fatalf("browse listings = %+v, want only the hosted mcp listing", catalog.Listings)
	}
	query := m.lastBrowse(t)
	if query.Get("kind") != KindMCP || query.Get("delivery") != DeliveryHosted {
		t.Fatalf("browse query = %v, want both axes", query)
	}

	claims, err := client.VerifyListingTrust(ctx, m.url, &catalog.Listings[0])
	if err != nil {
		t.Fatalf("verify trust: %v", err)
	}
	if claims.ListingSlug != "hosted-mcp" || !claims.Verified {
		t.Fatalf("trust claims = %+v", claims)
	}

	entitlement, err := client.Claim(ctx, m.url, ClaimRequest{ListingSlug: "hosted-mcp"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if entitlement.ID != "ent-1" {
		t.Fatalf("entitlement id = %q", entitlement.ID)
	}
	claimBody := m.lastClaim(t)
	if claimBody["instance_id"] != "instance-1" {
		t.Fatalf("claim body did not carry the registered instance id: %v", claimBody)
	}

	batch, err := client.Authorize(ctx, m.url, entitlement.ID, 20)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if len(batch.RequestIDs) != 20 || batch.Remaining() != 20 {
		t.Fatalf("batch ids = %d remaining = %d, want 20", len(batch.RequestIDs), batch.Remaining())
	}
	if !batch.IssuedAt.Time.Before(batch.ExpiresAt.Time) {
		t.Fatalf("batch timestamps are not ordered: %+v", batch)
	}

	// The seller verifies the assertion offline against the market's key set
	// and echoes the request_id it served.
	sellerClient := newInProductClient(t, m, clock, nil)
	seller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		served := r.Header.Get("X-Market-Request-ID")
		assertion, verr := sellerClient.VerifyAssertion(r.Context(), m.url, token, "https://seller.example/mcp")
		if verr != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		found := false
		for _, id := range assertion.RequestIDs {
			if id == served {
				found = true
				break
			}
		}
		if !found {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"request_id": served})
	}))
	defer seller.Close()

	requestID, ok := batch.Take()
	if !ok {
		t.Fatalf("batch had no request id")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, seller.URL, bytes.NewReader([]byte(`{"jsonrpc":"2.0"}`)))
	if err != nil {
		t.Fatalf("build seller request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+batch.Assertion)
	req.Header.Set("X-Market-Request-ID", requestID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call seller: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seller returned HTTP %d", resp.StatusCode)
	}
	var served struct {
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&served); err != nil || served.RequestID != requestID {
		t.Fatalf("seller echoed %+v (%v), want %q", served, err, requestID)
	}

	if err := client.ReportUsage(ctx, m.url, UsageReport{RequestID: served.RequestID}); err != nil {
		t.Fatalf("report usage: %v", err)
	}
	usage := m.lastUsage(t)
	if usage.RequestID != requestID || usage.EntitlementID != "ent-1" || usage.Calls != 1 {
		t.Fatalf("usage report = %+v", usage)
	}
	if batch.Remaining() != 19 {
		t.Fatalf("batch remaining = %d, want 19", batch.Remaining())
	}
}

func TestClaimAlreadyHeldIsNotAnOutage(t *testing.T) {
	m := newFakeInProductMarket(t)
	m.claimStatus = http.StatusConflict
	client := newInProductClient(t, m, newFakeClock(time.Now()), nil)

	_, err := client.Claim(context.Background(), m.url, ClaimRequest{ListingSlug: "hosted-mcp"})
	expectCode(t, err, CodeEntitlementHeld)
}

func TestAssertionVerifiesAgainstRotatedKey(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	oldToken := m.assertionFor(t, m.key)
	if _, err := client.VerifyAssertion(ctx, m.url, oldToken, "https://seller.example/mcp"); err != nil {
		t.Fatalf("verify old assertion: %v", err)
	}
	if got := m.jwksHits.Load(); got != 1 {
		t.Fatalf("JWKS hits = %d, want 1", got)
	}

	// Rotate the signing key but keep the same kid: the cached key set now has
	// the wrong public key, so verification must refresh JWKS once and retry.
	// The forced refresh is throttled to once per second, so move the client
	// clock past that guard before retrying.
	m.rotate.Store(true)
	clock.Advance(2 * time.Second)
	newToken := m.assertionFor(t, m.rotated)
	claims, err := client.VerifyAssertion(ctx, m.url, newToken, "https://seller.example/mcp")
	if err != nil {
		t.Fatalf("verify rotated assertion: %v", err)
	}
	if claims.Issuer != m.url {
		t.Fatalf("issuer = %q", claims.Issuer)
	}
	if got := m.jwksHits.Load(); got < 2 {
		t.Fatalf("JWKS hits = %d, want at least 2 after rotation", got)
	}
}

func TestExpiredAuthorizationBatchIsRefused(t *testing.T) {
	t.Run("market issues an already expired batch", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		m.authorizeExpires = clock.Now().Add(-time.Second)
		client := newInProductClient(t, m, clock, nil)

		_, err := client.Authorize(context.Background(), m.url, "ent-1", 20)
		expectCode(t, err, CodeBatchExpired)
	})

	t.Run("cached batch expires while the market is down", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, clock, nil)
		ctx := context.Background()
		if _, err := client.Authorize(ctx, m.url, "ent-1", 20); err != nil {
			t.Fatalf("authorize: %v", err)
		}
		clock.Advance(MaxBatchTTL + time.Second)
		m.offline.Store(true)
		_, err := client.Authorize(ctx, m.url, "ent-1", 20)
		expectCode(t, err, CodeBatchExpired)
	})
}

func TestUsageReportWithUnissuedRequestIDIsRefused(t *testing.T) {
	t.Run("client refuses an id the market never issued", func(t *testing.T) {
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, newFakeClock(time.Now()), nil)

		err := client.ReportUsage(context.Background(), m.url, UsageReport{RequestID: "fabricated"})
		expectCode(t, err, CodeUsageRequestUnknown)
		if got := m.usageAttempts.Load(); got != 0 {
			t.Fatalf("market received %d usage attempts for a locally refused id", got)
		}
	})

	t.Run("market refuses an id it does not recognise", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, clock, nil)
		ctx := context.Background()
		batch, err := client.Authorize(ctx, m.url, "ent-1", 1)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		m.forgetIssued = true
		id, _ := batch.Take()
		err = client.ReportUsage(ctx, m.url, UsageReport{RequestID: id})
		expectCode(t, err, CodeUsageRejected)
	})

	t.Run("a repeated report is refused", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, clock, nil)
		ctx := context.Background()
		batch, err := client.Authorize(ctx, m.url, "ent-1", 1)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		id, _ := batch.Take()
		if err := client.ReportUsage(ctx, m.url, UsageReport{RequestID: id}); err != nil {
			t.Fatalf("first usage: %v", err)
		}
		err = client.ReportUsage(ctx, m.url, UsageReport{RequestID: id})
		expectCode(t, err, CodeUsageRequestRepeated)
	})
}

// TestOutageKeepsServingFromCacheAndReplaysUsage pins the deliberate difference
// from the edge: the instance caches a ≥60 s batch, keeps serving when the
// market is unreachable, logs that attribution is deferred, and replays it when
// the market returns.
func TestOutageKeepsServingFromCacheAndReplaysUsage(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	var logBuf bytes.Buffer
	client := newInProductClient(t, m, clock, &logBuf)
	ctx := context.Background()

	batch, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	requestID, _ := batch.Take()

	m.offline.Store(true)

	// A second authorize is served from the cached batch, not from the market.
	cached, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("cached authorize: %v", err)
	}
	if cached != batch {
		t.Fatalf("expected the cached batch handle")
	}
	if cached.Remaining() != 1 {
		t.Fatalf("cached remaining = %d, want 1", cached.Remaining())
	}
	if !strings.Contains(logBuf.String(), "serving cached authorization batch") {
		t.Fatalf("log does not explain the cached batch:\n%s", logBuf.String())
	}

	// Usage cannot be delivered now. It is accepted for replay, not dropped.
	if err := client.ReportUsage(ctx, m.url, UsageReport{RequestID: requestID}); err != nil {
		t.Fatalf("deferred usage should return nil: %v", err)
	}
	if got := m.usageAccepted.Load(); got != 0 {
		t.Fatalf("market accepted %d reports while offline", got)
	}
	if !strings.Contains(logBuf.String(), "usage report deferred for replay") {
		t.Fatalf("log does not explain deferred attribution:\n%s", logBuf.String())
	}

	// The market returns: a permissive flush replays the queued report with the
	// original pre-issued request id.
	m.offline.Store(false)
	if err := client.FlushUsage(ctx, m.url); err != nil {
		t.Fatalf("flush usage: %v", err)
	}
	if got := m.usageAccepted.Load(); got != 1 {
		t.Fatalf("market accepted %d reports after flush, want 1", got)
	}
	usage := m.lastUsage(t)
	if usage.RequestID != requestID || usage.EntitlementID != "ent-1" {
		t.Fatalf("replayed usage = %+v", usage)
	}
}

func TestAuthorizeFallsBackOnlyOnceTheCachedBatchIsUsable(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	m.batchCalls = 1
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	batch, err := client.Authorize(ctx, m.url, "ent-1", 1)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if _, ok := batch.Take(); !ok {
		t.Fatalf("expected one request id")
	}
	m.offline.Store(true)
	_, err = client.Authorize(ctx, m.url, "ent-1", 1)
	expectCode(t, err, CodeBatchExhausted)
}

func TestAuthorizeRejectsAnOversizedBatch(t *testing.T) {
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, newFakeClock(time.Now()), nil)

	_, err := client.Authorize(context.Background(), m.url, "ent-1", MaxBatchCalls+1)
	expectCode(t, err, CodeBatchTooLarge)
	if got := m.authorizeHits.Load(); got != 0 {
		t.Fatalf("market received %d authorize requests for a locally refused batch", got)
	}
}

func TestBrowseVerifiedDropsAnUntrustedListing(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	// Sign the trust document for the other listing, so the hosted listing's
	// document names a different slug and must be dropped.
	m.mu.Lock()
	m.trustToken = m.sign(t, m.key, map[string]any{
		"iss":          m.url,
		"aud":          TrustDocumentAudience,
		"sub":          "install-blueprint",
		"listing_slug": "install-blueprint",
		"exp":          time.Now().Add(time.Hour).Unix(),
	})
	m.mu.Unlock()
	var logBuf bytes.Buffer
	client := newInProductClient(t, m, clock, &logBuf)

	catalog, err := client.BrowseVerified(context.Background(), m.url, BrowseQuery{Kind: KindMCP, Delivery: DeliveryHosted})
	if err != nil {
		t.Fatalf("browse verified: %v", err)
	}
	if len(catalog.Listings) != 0 {
		t.Fatalf("offered %d unverified listings", len(catalog.Listings))
	}
	if !strings.Contains(logBuf.String(), "dropping unverified market listing") {
		t.Fatalf("log does not explain the drop:\n%s", logBuf.String())
	}
}

// TestAuthorizeAcceptsTheAuthFieldFromTheDesignOfRecord pins the authorize
// response shape in docs/marketplace-v1.md §8.1 — `{"auth": "<assertion>",
// "request_ids": [...]}` — which previously decoded as an empty assertion and
// failed every batch a market built to that contract issued.
func TestAuthorizeAcceptsTheAuthFieldFromTheDesignOfRecord(t *testing.T) {
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, newFakeClock(time.Now()), nil)
	m.mu.Lock()
	m.authField = true
	m.mu.Unlock()

	batch, err := client.Authorize(context.Background(), m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("authorize with an `auth` response: %v", err)
	}
	if batch.Assertion == "" {
		t.Fatal("the `auth` field was not decoded as the assertion")
	}
	if len(batch.RequestIDs) != 2 {
		t.Fatalf("want 2 request ids, got %d", len(batch.RequestIDs))
	}
}

// TestAuthorizeRefusesAnAssertionThatDoesNotCoverTheRequestIDs: the assertion is
// what the seller verifies, so the ids this instance hands out must be the ids it
// authorizes. Otherwise a served call is reported against an id the seller never
// agreed to and the attribution is refused after the work was done.
func TestAuthorizeRefusesAnAssertionThatDoesNotCoverTheRequestIDs(t *testing.T) {
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, newFakeClock(time.Now()), nil)
	token := m.sign(t, m.key, map[string]any{
		"iss":            m.url,
		"aud":            "https://seller.example/mcp",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Add(-time.Minute).Unix(),
		"entitlement_id": "ent-1",
		"request_ids":    []string{"not-a-pre-issued-id"},
	})
	m.mu.Lock()
	m.assertionToken = token
	m.mu.Unlock()

	_, err := client.Authorize(context.Background(), m.url, "ent-1", 2)
	if got := ErrorCode(err); got != CodeAssertionInvalid {
		t.Fatalf("want %s, got %q (%v)", CodeAssertionInvalid, got, err)
	}
	if !strings.Contains(err.Error(), "does not authorize request id") {
		t.Fatalf("error does not name the uncovered id: %v", err)
	}
}

// TestAuthorizeRefusesAReissuedRequestID reproduces the silent-attribution loss:
// the fake market hands out req-1..N for every batch, so after the first batch's
// req-1 is reported a second authorize re-issues it. Reporting it again is
// refused by the market, and the served call is lost with only the client
// knowing — so the batch must be refused before any id is handed out.
func TestAuthorizeRefusesAReissuedRequestID(t *testing.T) {
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, newFakeClock(time.Now()), nil)
	ctx := context.Background()

	first, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("first authorize: %v", err)
	}
	id, ok := first.Take()
	if !ok {
		t.Fatal("first batch handed out no id")
	}
	if err := client.ReportUsage(ctx, m.url, UsageReport{
		RequestID:     id,
		EntitlementID: "ent-1",
		Outcome:       UsageOutcomeSuccess,
	}); err != nil {
		t.Fatalf("report usage: %v", err)
	}

	_, err = client.Authorize(ctx, m.url, "ent-1", 2)
	if got := ErrorCode(err); got != CodeAssertionInvalid {
		t.Fatalf("want %s for a re-issued id, got %q (%v)", CodeAssertionInvalid, got, err)
	}
	if !strings.Contains(err.Error(), "re-issued request id") {
		t.Fatalf("error does not explain the re-issue: %v", err)
	}
}

// TestTakeRefusesAnExpiredBatch: the seller refuses attribution once the
// assertion window closes, so the client must stop handing ids out at the same
// instant instead of buying served calls nobody can report.
func TestTakeRefusesAnExpiredBatch(t *testing.T) {
	m := newFakeInProductMarket(t)
	clock := newFakeClock(time.Now())
	client := newInProductClient(t, m, clock, nil)

	batch, err := client.Authorize(context.Background(), m.url, "ent-1", 3)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if id, ok := batch.Take(); !ok || id == "" {
		t.Fatal("a live batch handed out no id")
	}
	clock.Advance(MaxBatchTTL + time.Second)
	if id, ok := batch.Take(); ok {
		t.Fatalf("an expired batch handed out %q", id)
	}
}

// TestClientFromConfigReceivesTheProcessLogger: the outage and deferral
// diagnostics are only useful if the module wires the instance logger into the
// client — the zero value discards them, so a reporter that forgets is silent.
func TestClientFromConfigReceivesTheProcessLogger(t *testing.T) {
	cfg := &config.Config{}
	cfg.Market.TrustedIssuers = []string{"https://market.example"}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	client, err := NewClientFromConfig(cfg, log)
	if err != nil {
		t.Fatalf("new client from config: %v", err)
	}
	if client.log != log {
		t.Fatal("NewClientFromConfig dropped the process logger, so outage diagnostics go nowhere")
	}
}
