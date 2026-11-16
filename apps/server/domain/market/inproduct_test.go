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
	registerStatus   int
	catalogStatus    int
	trustStatus      int
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
	// nextRequestID hands out a fresh id per call, so two batches never share an
	// id unless a test asks for that through reissueIDs.
	nextRequestID int
	// catalogBody, when set, is written verbatim as the catalog's 200 body, so a
	// test can make the market answer 2xx with an unusable document.
	catalogBody []byte
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
		m.mu.Lock()
		status := m.registerStatus
		m.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
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
		status := m.catalogStatus
		m.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		m.mu.Lock()
		m.browseQueries = append(m.browseQueries, r.URL.Query())
		trust := m.trustToken
		raw := m.catalogBody
		m.mu.Unlock()
		if raw != nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(raw)
			return
		}
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
			for i := 0; i < calls; i++ {
				m.nextRequestID++
				ids = append(ids, fmt.Sprintf("req-%d", m.nextRequestID))
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
		status := m.trustStatus
		token := m.trustToken
		m.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
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
		"request_ids":    requestIDs(fakeRequestIDs),
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
		"request_ids":    requestIDs(fakeRequestIDs),
	})
}

// fakeRequestIDs is how many ids the fake market's assertions cover. The market
// hands out a fresh id per batch, so a test that mints several batches stays
// inside the assertion's id set.
const fakeRequestIDs = 500

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
	if claims.ListingSlug != "hosted-mcp" || claims.Verified == nil || !*claims.Verified {
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
	if !batch.IssuedAt.Before(batch.ExpiresAt.Time) {
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
	if got := m.authorizeHits.Load(); got != 1 {
		t.Fatalf("authorize hits = %d, want 1: the fallback must not mint a new batch", got)
	}
	if cached.Assertion != batch.Assertion {
		t.Fatal("the fallback did not return the cached batch")
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
// after the first batch's first id is reported, a second authorize asks the
// market for a batch and the market re-issues that id. Reporting it again is
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

	m.mu.Lock()
	m.reissueIDs = append([]string(nil), first.RequestIDs...)
	m.mu.Unlock()

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

// TestClockSkewWithinLeewayIsTolerated: the market and the instance keep
// independent clocks, so a token minted there can look a couple of seconds early
// or a couple of seconds old here. Both must verify. Beyond the leeway the token
// is genuinely not valid and must still be refused.
func TestClockSkewWithinLeewayIsTolerated(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()
	now := clock.Now()

	assertionClaims := func(exp time.Time, nbf *time.Time) map[string]any {
		claims := map[string]any{
			"iss":            m.url,
			"aud":            "https://seller.example/mcp",
			"exp":            exp.Unix(),
			"entitlement_id": "ent-1",
		}
		if nbf != nil {
			claims["nbf"] = nbf.Unix()
		}
		return claims
	}
	trustClaims := func(exp time.Time, nbf *time.Time) map[string]any {
		claims := map[string]any{
			"iss":          m.url,
			"aud":          TrustDocumentAudience,
			"sub":          "hosted-mcp",
			"listing_slug": "hosted-mcp",
			"verified":     true,
			"exp":          exp.Unix(),
		}
		if nbf != nil {
			claims["nbf"] = nbf.Unix()
		}
		return claims
	}

	t.Run("assertion valid but 2s in the future", func(t *testing.T) {
		nbf := now.Add(2 * time.Second)
		token := m.sign(t, m.key, assertionClaims(now.Add(time.Hour), &nbf))
		if _, err := client.VerifyAssertion(ctx, m.url, token, "https://seller.example/mcp"); err != nil {
			t.Fatalf("a 2s clock skew must not refuse a valid assertion: %v", err)
		}
	})

	t.Run("assertion expired 2s ago", func(t *testing.T) {
		token := m.sign(t, m.key, assertionClaims(now.Add(-2*time.Second), nil))
		if _, err := client.VerifyAssertion(ctx, m.url, token, "https://seller.example/mcp"); err != nil {
			t.Fatalf("a 2s clock skew must not refuse a just-expired assertion: %v", err)
		}
	})

	t.Run("trust document valid but 2s in the future", func(t *testing.T) {
		nbf := now.Add(2 * time.Second)
		token := m.sign(t, m.key, trustClaims(now.Add(time.Hour), &nbf))
		if _, err := client.VerifyTrustDocument(ctx, m.url, token); err != nil {
			t.Fatalf("a 2s clock skew must not refuse a valid trust document: %v", err)
		}
	})

	t.Run("trust document expired 2s ago", func(t *testing.T) {
		token := m.sign(t, m.key, trustClaims(now.Add(-2*time.Second), nil))
		if _, err := client.VerifyTrustDocument(ctx, m.url, token); err != nil {
			t.Fatalf("a 2s clock skew must not refuse a just-expired trust document: %v", err)
		}
	})

	t.Run("assertion not valid beyond the leeway", func(t *testing.T) {
		nbf := now.Add(clockSkewLeeway + time.Second)
		token := m.sign(t, m.key, assertionClaims(now.Add(time.Hour), &nbf))
		_, err := client.VerifyAssertion(ctx, m.url, token, "https://seller.example/mcp")
		expectCode(t, err, CodeAssertionInvalid)
		if !strings.Contains(err.Error(), "not yet valid") {
			t.Fatalf("error does not explain the not-before failure: %v", err)
		}
	})

	t.Run("assertion expired beyond the leeway", func(t *testing.T) {
		token := m.sign(t, m.key, assertionClaims(now.Add(-clockSkewLeeway-time.Second), nil))
		_, err := client.VerifyAssertion(ctx, m.url, token, "https://seller.example/mcp")
		expectCode(t, err, CodeAssertionInvalid)
		if !strings.Contains(err.Error(), "expired") {
			t.Fatalf("error does not explain the expiry: %v", err)
		}
	})

	t.Run("trust document expired beyond the leeway", func(t *testing.T) {
		token := m.sign(t, m.key, trustClaims(now.Add(-clockSkewLeeway-time.Second), nil))
		_, err := client.VerifyTrustDocument(ctx, m.url, token)
		expectCode(t, err, CodeTrustInvalid)
	})
}

// TestMarketRejectionIsNotAnOutage pins the status classification: a 4xx the
// market returns is a refusal of our request, so it must not be reported as
// market_unreachable (which drives retry/backoff and usage replay). 5xx and 408
// stay outages.
func TestMarketRejectionIsNotAnOutage(t *testing.T) {
	cases := []struct {
		name   string
		route  string
		status int
		call   func(ctx context.Context, c *Client, m *fakeInProductMarket) error
		want   string
	}{
		{
			name:   "browse 400",
			route:  "catalog",
			status: http.StatusBadRequest,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Browse(ctx, m.url, BrowseQuery{Kind: KindMCP})
				return err
			},
			want: CodeMarketRejected,
		},
		{
			name:   "browse 401",
			route:  "catalog",
			status: http.StatusUnauthorized,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Browse(ctx, m.url, BrowseQuery{Kind: KindMCP})
				return err
			},
			want: CodeMarketRejected,
		},
		{
			name:   "browse 403",
			route:  "catalog",
			status: http.StatusForbidden,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Browse(ctx, m.url, BrowseQuery{Kind: KindMCP})
				return err
			},
			want: CodeMarketRejected,
		},
		{
			name:   "trust document 403",
			route:  "trust",
			status: http.StatusForbidden,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.FetchTrustDocument(ctx, m.url, "hosted-mcp")
				return err
			},
			want: CodeMarketRejected,
		},
		{
			name:   "claim 400",
			route:  "claim",
			status: http.StatusBadRequest,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Claim(ctx, m.url, ClaimRequest{ListingSlug: "hosted-mcp"})
				return err
			},
			want: CodeClaimFailed,
		},
		{
			name:   "claim 405",
			route:  "claim",
			status: http.StatusMethodNotAllowed,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Claim(ctx, m.url, ClaimRequest{ListingSlug: "hosted-mcp"})
				return err
			},
			want: CodeClaimFailed,
		},
		{
			name:   "usage 401",
			route:  "usage",
			status: http.StatusUnauthorized,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				batch, err := c.Authorize(ctx, m.url, "ent-1", 1)
				if err != nil {
					return err
				}
				id, ok := batch.Take()
				if !ok {
					t.Fatal("authorize handed out no request id")
				}
				return c.ReportUsage(ctx, m.url, UsageReport{RequestID: id})
			},
			want: CodeUsageRejected,
		},
		{
			name:   "register 400",
			route:  "register",
			status: http.StatusBadRequest,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Register(ctx, m.url, RegisterRequest{})
				return err
			},
			want: CodeRegistrationFailed,
		},
		{
			name:   "register 502 is still an outage",
			route:  "register",
			status: http.StatusBadGateway,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Register(ctx, m.url, RegisterRequest{})
				return err
			},
			want: CodeMarketUnreach,
		},
		{
			name:   "browse 500 is still an outage",
			route:  "catalog",
			status: http.StatusInternalServerError,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Browse(ctx, m.url, BrowseQuery{Kind: KindMCP})
				return err
			},
			want: CodeMarketUnreach,
		},
		{
			name:   "claim 503 is still an outage",
			route:  "claim",
			status: http.StatusServiceUnavailable,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Claim(ctx, m.url, ClaimRequest{ListingSlug: "hosted-mcp"})
				return err
			},
			want: CodeMarketUnreach,
		},
		{
			name:   "browse 408 is still an outage",
			route:  "catalog",
			status: http.StatusRequestTimeout,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Browse(ctx, m.url, BrowseQuery{Kind: KindMCP})
				return err
			},
			want: CodeMarketUnreach,
		},
		{
			name:   "browse 425 is still an outage",
			route:  "catalog",
			status: http.StatusTooEarly,
			call: func(ctx context.Context, c *Client, m *fakeInProductMarket) error {
				_, err := c.Browse(ctx, m.url, BrowseQuery{Kind: KindMCP})
				return err
			},
			want: CodeMarketUnreach,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newFakeInProductMarket(t)
			m.mu.Lock()
			switch tc.route {
			case "register":
				m.registerStatus = tc.status
			case "trust":
				m.trustStatus = tc.status
			case "claim":
				m.claimStatus = tc.status
			case "usage":
				m.usageStatus = tc.status
			default:
				m.catalogStatus = tc.status
			}
			m.mu.Unlock()
			client := newInProductClient(t, m, newFakeClock(time.Now()), nil)
			err := tc.call(context.Background(), client, m)
			expectCode(t, err, tc.want)
		})
	}
}

// TestUsageReportRejectionIsDroppedAndOutageIsQueued: a 4xx is refused for good
// (replaying it unchanged would be refused again), so it is surfaced and not
// queued; a 5xx is an outage, so the report is queued and replayed.
func TestUsageReportRejectionIsDroppedAndOutageIsQueued(t *testing.T) {
	t.Run("4xx is surfaced and not queued", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, clock, nil)
		ctx := context.Background()
		batch, err := client.Authorize(ctx, m.url, "ent-1", 1)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		id, _ := batch.Take()
		m.mu.Lock()
		m.usageStatus = http.StatusUnauthorized
		m.mu.Unlock()

		err = client.ReportUsage(ctx, m.url, UsageReport{RequestID: id})
		expectCode(t, err, CodeUsageRejected)

		attempts := m.usageAttempts.Load()
		if err := client.FlushUsage(ctx, m.url); err != nil {
			t.Fatalf("flush after a rejected report: %v", err)
		}
		if got := m.usageAttempts.Load(); got != attempts {
			t.Fatalf("flush replayed a rejected report: %d attempts became %d", attempts, got)
		}
	})

	t.Run("5xx is queued and replayed", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, clock, nil)
		ctx := context.Background()
		batch, err := client.Authorize(ctx, m.url, "ent-1", 1)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		id, _ := batch.Take()
		m.mu.Lock()
		m.usageStatus = http.StatusServiceUnavailable
		m.mu.Unlock()

		if err := client.ReportUsage(ctx, m.url, UsageReport{RequestID: id}); err != nil {
			t.Fatalf("an outage-shaped usage report must be deferred, not returned: %v", err)
		}
		if got := m.usageAccepted.Load(); got != 0 {
			t.Fatalf("market accepted %d reports while answering 503", got)
		}

		m.mu.Lock()
		m.usageStatus = 0
		m.mu.Unlock()
		if err := client.FlushUsage(ctx, m.url); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if got := m.usageAccepted.Load(); got != 1 {
			t.Fatalf("replayed reports accepted = %d, want 1", got)
		}
	})

	// F1: 425 Too Early means "ask again shortly", so it belongs with 408/429 on
	// the retryable side. Treating it as a rejection would drop the attribution
	// for a call that is already served.
	t.Run("425 is retryable and replayed", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, clock, nil)
		ctx := context.Background()
		batch, err := client.Authorize(ctx, m.url, "ent-1", 1)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		id, _ := batch.Take()
		m.mu.Lock()
		m.usageStatus = http.StatusTooEarly
		m.mu.Unlock()

		if err := client.ReportUsage(ctx, m.url, UsageReport{RequestID: id}); err != nil {
			t.Fatalf("a 425 usage report must be deferred, not dropped: %v", err)
		}
		if got := m.usageAccepted.Load(); got != 0 {
			t.Fatalf("market accepted %d reports while answering 425", got)
		}

		m.mu.Lock()
		m.usageStatus = 0
		m.mu.Unlock()
		if err := client.FlushUsage(ctx, m.url); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if got := m.usageAccepted.Load(); got != 1 {
			t.Fatalf("replayed reports accepted = %d, want 1", got)
		}
	})
}

func TestMarketRejectionIsNotRetryable(t *testing.T) {
	if isRetryableMarketError(ErrMarketRejected("catalog browse", http.StatusBadRequest)) {
		t.Fatal("a market rejection must not be replayed as if the market were down")
	}
	if !isRetryableMarketError(ErrMarketUnreachable("")) {
		t.Fatal("an unreachable market must stay retryable")
	}
}

// TestTrustDocumentThatDisclaimsVerificationIsRefused pins item 3: a document
// with a good signature but "verified": false must not let the listing through.
// BrowseVerified already promises to drop an unverified listing, so the claim is
// enforced rather than treated as informational.
func TestTrustDocumentThatDisclaimsVerificationIsRefused(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	m.mu.Lock()
	m.trustToken = m.sign(t, m.key, map[string]any{
		"iss":          m.url,
		"aud":          TrustDocumentAudience,
		"sub":          "hosted-mcp",
		"listing_slug": "hosted-mcp",
		"verified":     false,
		"exp":          time.Now().Add(time.Hour).Unix(),
	})
	m.mu.Unlock()

	_, err := client.VerifyListingTrust(ctx, m.url, &Listing{Slug: "hosted-mcp"})
	expectCode(t, err, CodeTrustInvalid)
	if !strings.Contains(err.Error(), "does not vouch") {
		t.Fatalf("error does not explain the disclaimed verification: %v", err)
	}

	var logBuf bytes.Buffer
	loggingClient := newInProductClient(t, m, clock, &logBuf)
	catalog, err := loggingClient.BrowseVerified(ctx, m.url, BrowseQuery{Kind: KindMCP, Delivery: DeliveryHosted})
	if err != nil {
		t.Fatalf("browse verified: %v", err)
	}
	if len(catalog.Listings) != 0 {
		t.Fatalf("offered %d listings the market disclaimed", len(catalog.Listings))
	}
	if !strings.Contains(logBuf.String(), "dropping unverified market listing") {
		t.Fatalf("log does not explain the drop:\n%s", logBuf.String())
	}
}

// TestTrustDocumentWithoutVerificationClaimIsRefused pins item 4 of #908: the
// absence of the claim is not a pass. A signed document that never says the
// listing is verified is refused (fail closed), so "verified" cannot silently
// depend on the market's producer contract emitting the claim.
func TestTrustDocumentWithoutVerificationClaimIsRefused(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	m.mu.Lock()
	m.trustToken = m.sign(t, m.key, map[string]any{
		"iss":          m.url,
		"aud":          TrustDocumentAudience,
		"sub":          "hosted-mcp",
		"listing_slug": "hosted-mcp",
		"exp":          time.Now().Add(time.Hour).Unix(),
	})
	m.mu.Unlock()

	_, err := client.VerifyListingTrust(ctx, m.url, &Listing{Slug: "hosted-mcp"})
	expectCode(t, err, CodeTrustInvalid)
	if !strings.Contains(err.Error(), "does not state") {
		t.Fatalf("error does not explain the missing verification claim: %v", err)
	}

	var logBuf bytes.Buffer
	loggingClient := newInProductClient(t, m, clock, &logBuf)
	catalog, err := loggingClient.BrowseVerified(ctx, m.url, BrowseQuery{Kind: KindMCP, Delivery: DeliveryHosted})
	if err != nil {
		t.Fatalf("browse verified: %v", err)
	}
	if len(catalog.Listings) != 0 {
		t.Fatalf("offered %d listings whose trust document does not state verified:true", len(catalog.Listings))
	}
	if !strings.Contains(logBuf.String(), "dropping unverified market listing") {
		t.Fatalf("log does not explain the drop:\n%s", logBuf.String())
	}
}

// TestAuthorizeReusesALiveBatchUntilItExpires: a hot path authorizes once per
// outbound call, so a live batch with enough ids must be handed back without a
// market round trip; only an empty, too-small or expired batch buys a new one.
func TestAuthorizeReusesALiveBatchUntilItExpires(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	first, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("first authorize: %v", err)
	}
	if got := m.authorizeHits.Load(); got != 1 {
		t.Fatalf("authorize hits = %d, want 1", got)
	}

	grant, err := client.Authorize(ctx, m.url, "ent-1", 1)
	if err != nil {
		t.Fatalf("second authorize: %v", err)
	}
	if grant == first {
		t.Fatal("the second authorize returned the shared batch instead of a grant")
	}
	if got := m.authorizeHits.Load(); got != 1 {
		t.Fatalf("reuse still bought a batch: authorize hits = %d, want 1", got)
	}
	if got := len(grant.RequestIDs); got != 1 {
		t.Fatalf("grant carries %d ids, want exactly the 1 reserved", got)
	}
	if got := freeIDS(t, grant); len(got) != 1 {
		t.Fatalf("grant handed out %d ids, want 1", len(got))
	}
	// The reservation is taken out of the shared batch, so the first handle no
	// longer offers that id.
	if got := first.Remaining(); got != 1 {
		t.Fatalf("shared batch remaining = %d, want 1 after a 1-id reservation", got)
	}

	// Past the local 60 s window the cache must not be reused; the market is
	// asked for a fresh batch.
	clock.Advance(MaxBatchTTL + time.Second)
	m.mu.Lock()
	m.authorizeExpires = clock.Now().Add(MaxBatchTTL)
	m.mu.Unlock()

	third, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("authorize after expiry: %v", err)
	}
	if third == first {
		t.Fatal("an expired batch was reused")
	}
	if got := m.authorizeHits.Load(); got != 2 {
		t.Fatalf("authorize hits after expiry = %d, want 2", got)
	}
}

// TestAuthorizeReservationsAreExclusive pins F5: two authorizations against the
// same live batch must be allocated disjoint ids, not both be told "there are
// enough left" and race for them.
func TestAuthorizeReservationsAreExclusive(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	if _, err := client.Authorize(ctx, m.url, "ent-1", 6); err != nil {
		t.Fatalf("first authorize: %v", err)
	}

	a, err := client.Authorize(ctx, m.url, "ent-1", 3)
	if err != nil {
		t.Fatalf("authorize A: %v", err)
	}
	b, err := client.Authorize(ctx, m.url, "ent-1", 3)
	if err != nil {
		t.Fatalf("authorize B: %v", err)
	}
	if got := m.authorizeHits.Load(); got != 1 {
		t.Fatalf("authorize hits = %d, want 1 (both reservations from one batch)", got)
	}

	idsA := freeIDS(t, a)
	idsB := freeIDS(t, b)
	if len(idsA) != 3 || len(idsB) != 3 {
		t.Fatalf("reserved %d and %d ids, want 3 and 3", len(idsA), len(idsB))
	}
	seen := make(map[string]struct{}, 6)
	for _, id := range append(idsA, idsB...) {
		if _, dup := seen[id]; dup {
			t.Fatalf("request id %q was handed to two authorizations", id)
		}
		seen[id] = struct{}{}
	}
}

// TestAuthorizeMintsWhenTheCachedBatchIsTooSmall pins the other half of the
// reuse rule: a live batch with fewer ids than requested is not handed out.
func TestAuthorizeMintsWhenTheCachedBatchIsTooSmall(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	first, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("first authorize: %v", err)
	}
	if _, err := client.Authorize(ctx, m.url, "ent-1", 1); err != nil {
		t.Fatalf("reserving the first id: %v", err)
	}
	if got := first.Remaining(); got != 1 {
		t.Fatalf("remaining = %d, want 1", got)
	}

	if _, err := client.Authorize(ctx, m.url, "ent-1", 2); err != nil {
		t.Fatalf("authorize for a batch larger than the cache: %v", err)
	}
	if got := m.authorizeHits.Load(); got != 2 {
		t.Fatalf("authorize hits = %d, want 2: a too-small batch must not be reused", got)
	}
}

// TestAuthorizeRetainsABatchThatIsNotYetValid pins F8: an assertion whose nbf is
// a few seconds in the future passes verification under the clock-skew leeway,
// but the batch must not hand out a request id before that instant — a call
// served then cannot be attributed.
func TestAuthorizeRetainsABatchThatIsNotYetValid(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()
	notBefore := clock.Now().Add(30 * time.Second)

	m.mu.Lock()
	m.assertionToken = m.sign(t, m.key, map[string]any{
		"iss":            m.url,
		"aud":            "https://seller.example/mcp",
		"nbf":            notBefore.Unix(),
		"exp":            clock.Now().Add(time.Hour).Unix(),
		"entitlement_id": "ent-1",
		"request_ids":    requestIDs(20),
	})
	m.mu.Unlock()

	batch, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if id, ok := batch.Take(); ok {
		t.Fatalf("a not-yet-valid batch handed out %q", id)
	}
	if ids, ok := batch.Reserve(1); ok {
		t.Fatalf("a not-yet-valid batch reserved %v", ids)
	}

	// Once the not-before instant passes the same batch becomes usable; the
	// window is not thrown away, only withheld until the seller accepts it.
	clock.Advance(31 * time.Second)
	if id, ok := batch.Take(); !ok || id == "" {
		t.Fatal("the batch did not become usable after its not-before instant")
	}
}

// TestMintedBatchIsACopyOfTheCachedBatch pins item 1 of #908: Authorize caches
// the batch it minted and hands the caller a copy. Mutating the returned
// document must not rewrite what the cache will serve, while the ids the caller
// takes are still consumed for that cache and are never handed out twice.
func TestMintedBatchIsACopyOfTheCachedBatch(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	minted, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	originalAssertion := minted.Assertion
	if _, ok := minted.Take(); !ok {
		t.Fatal("minted batch handed out no id")
	}

	// A caller that rewrites the document it was handed must not corrupt the
	// cached batch that later authorizations reserve from.
	minted.Assertion = "tampered"
	minted.RequestIDs[0] = "tampered"
	minted.ExpiresAt = MarketTime{Time: clock.Now().Add(24 * time.Hour)}

	grant, err := client.Authorize(ctx, m.url, "ent-1", 1)
	if err != nil {
		t.Fatalf("second authorize: %v", err)
	}
	if got := m.authorizeHits.Load(); got != 1 {
		t.Fatalf("authorize hits = %d, want 1: the live cached batch must still be reused", got)
	}
	if grant.Assertion != originalAssertion {
		t.Fatalf("the cached assertion was rewritten through the minted handle: %q", grant.Assertion)
	}
	if len(grant.RequestIDs) != 1 || grant.RequestIDs[0] != "req-2" {
		t.Fatalf("grant ids = %v, want the single unreserved id req-2", grant.RequestIDs)
	}
	// The handle and the cache share consumption state: the reservation is
	// visible to the handle, which therefore offers nothing more.
	if got := minted.Remaining(); got != 0 {
		t.Fatalf("minted handle remaining = %d, want 0 after the cache reserved its id", got)
	}
	if id, ok := minted.Take(); ok {
		t.Fatalf("the minted handle handed out %q after the cache had reserved it", id)
	}
}

// TestAuthorizeRefusesAnIdFromALiveCachedBatch pins item 2 of #908: an id that a
// still-live batch already issued (reserved but not yet reported) must not be
// accepted again. Overwriting the cache would leave two served calls naming one
// id, and the second report would be refused.
func TestAuthorizeRefusesAnIdFromALiveCachedBatch(t *testing.T) {
	clock := newFakeClock(time.Now())
	m := newFakeInProductMarket(t)
	client := newInProductClient(t, m, clock, nil)
	ctx := context.Background()

	first, err := client.Authorize(ctx, m.url, "ent-1", 2)
	if err != nil {
		t.Fatalf("first authorize: %v", err)
	}

	// Ask for more than the cache holds, so the market is consulted, and have it
	// re-issue the ids that are still live in the cache.
	m.mu.Lock()
	m.reissueIDs = append([]string(nil), first.RequestIDs...)
	m.mu.Unlock()

	_, err = client.Authorize(ctx, m.url, "ent-1", 20)
	if got := ErrorCode(err); got != CodeAssertionInvalid {
		t.Fatalf("want %s for an id from a live batch, got %q (%v)", CodeAssertionInvalid, got, err)
	}
	if !strings.Contains(err.Error(), "already issued by a live batch") {
		t.Fatalf("error does not explain the live re-issue: %v", err)
	}

	// The previous cached batch stays usable: the refusal happens before it is
	// replaced, so its unreserved ids are still attributable.
	grant, err := client.Authorize(ctx, m.url, "ent-1", 1)
	if err != nil {
		t.Fatalf("reserving from the surviving cached batch: %v", err)
	}
	if len(grant.RequestIDs) != 1 || grant.RequestIDs[0] != "req-1" {
		t.Fatalf("grant ids = %v, want req-1 from the surviving batch", grant.RequestIDs)
	}
}

// TestMalformedSuccessBodyIsNotAnOutage pins item 3 of #908: a 2xx whose body
// the instance cannot use is a market bug, not an outage. It carries its own
// code and is not retried on the network cadence, so dashboards can tell a
// broken producer from a market that is down.
func TestMalformedSuccessBodyIsNotAnOutage(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "not JSON", body: "not json at all", want: "invalid catalog document"},
		{name: "JSON without listings or catalog", body: `{"unexpected":"shape"}`, want: "invalid catalog document"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newFakeInProductMarket(t)
			client := newInProductClient(t, m, newFakeClock(time.Now()), nil)
			m.mu.Lock()
			m.catalogBody = []byte(tc.body)
			m.mu.Unlock()

			_, err := client.Browse(context.Background(), m.url, BrowseQuery{Kind: KindMCP})
			expectCode(t, err, CodeMarketMalformed)
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error does not explain the malformed body: %v", err)
			}
			if isRetryableMarketError(err) {
				t.Fatalf("a malformed 2xx body must not be retried on the network cadence: %v", err)
			}
		})
	}
}

// freeIDS drains a batch through Take, failing the test if it runs dry.
func freeIDS(t *testing.T, batch *AuthorizationBatch) []string {
	t.Helper()
	var ids []string
	for {
		id, ok := batch.Take()
		if !ok {
			return ids
		}
		ids = append(ids, id)
	}
}

// TestAuthorizeFourHundredDropsTheCachedBatch pins the Authorize 4xx branch: a
// 402 says this entitlement may not be authorized, so the cached batch must not
// be served afterwards (its calls could not be attributed), while a 408 is an
// outage and stays retryable.
func TestAuthorizeFourHundredDropsTheCachedBatch(t *testing.T) {
	t.Run("402 is a refusal and drops the cached batch", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, clock, nil)
		ctx := context.Background()

		if _, err := client.Authorize(ctx, m.url, "ent-1", 2); err != nil {
			t.Fatalf("authorize: %v", err)
		}
		m.mu.Lock()
		m.authorizeStatus = http.StatusPaymentRequired
		m.mu.Unlock()

		// Ask for more than the cached batch holds, so the market is consulted
		// and answers 4xx on the live cache.
		_, err := client.Authorize(ctx, m.url, "ent-1", 20)
		expectCode(t, err, CodeEntitlementInactive)

		// The cached batch is gone: with the market now unreachable there is
		// nothing to fall back to, so the outage itself surfaces. Serving the
		// dropped batch would return nil here.
		m.mu.Lock()
		m.authorizeStatus = 0
		m.mu.Unlock()
		m.offline.Store(true)
		_, err = client.Authorize(ctx, m.url, "ent-1", 1)
		expectCode(t, err, CodeMarketUnreach)
	})

	t.Run("408 on a cold cache is a retryable outage", func(t *testing.T) {
		clock := newFakeClock(time.Now())
		m := newFakeInProductMarket(t)
		client := newInProductClient(t, m, clock, nil)
		m.mu.Lock()
		m.authorizeStatus = http.StatusRequestTimeout
		m.mu.Unlock()

		_, err := client.Authorize(context.Background(), m.url, "ent-1", 2)
		expectCode(t, err, CodeMarketUnreach)
		if !isRetryableMarketError(err) {
			t.Fatalf("a 408 on authorize must stay retryable: %v", err)
		}
	})
}
