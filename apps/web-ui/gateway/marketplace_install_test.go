package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func newMarketInstallServer(f *fakeMemory, cfg Config) (*Server, *echo.Echo) {
	s := &Server{cfg: cfg, memory: f}
	e := echo.New()
	e.GET("/marketplace/install", s.uiMarketInstall)
	e.POST("/marketplace/install", s.uiMarketInstallConfirm)
	return s, e
}

func marketTestConfig() Config {
	return Config{
		DefaultAgent:         "memory",
		MemoryProjectID:      "proj-1",
		MarketURL:            "https://market.example",
		MarketTrustedIssuers: []string{"https://market.example"},
	}
}

func marketResolveResult() *MarketResolveResult {
	return &MarketResolveResult{
		Listing: MarketListingPreview{
			Slug:      "demo",
			Title:     "Demo Listing",
			Publisher: "Acme",
			Semver:    "1.2.3",
			Digest:    "sha256:abc123",
		},
		Capabilities: MarketCapabilities{
			Reads:     []string{"documents"},
			Writes:    []string{"graph"},
			Egress:    []string{"api.acme.example"},
			MCPs:      []MarketMCPCapability{{Slug: "search", Scopes: []string{"read"}}},
			Schedules: true,
			Model:     &MarketCapabilityModel{Provider: "openai", Name: "gpt-5"},
		},
		Compat: MarketCompat{Memory: ">=1.0 <2.0"},
	}
}

func TestMarketInstallReviewTrustedIssuer(t *testing.T) {
	f := &fakeMemory{marketResolveRes: marketResolveResult()}
	s, e := newMarketInstallServer(f, marketTestConfig())

	q := url.Values{"code": {"abc"}, "market": {"https://market.example"}}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/marketplace/install?"+q.Encode(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.marketResolveCalls != 1 || f.marketResolveMarket != "https://market.example" || f.marketResolveCode != "abc" {
		t.Fatalf("resolve args = calls:%d market:%q code:%q", f.marketResolveCalls, f.marketResolveMarket, f.marketResolveCode)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="market-install-review"`,
		"Demo Listing",
		"Acme",
		"1.2.3",
		"sha256:abc123",
		"&gt;=1.0 &lt;2.0", // compat (templ escapes < and >)
		"documents",        // reads
		"graph",            // writes
		"api.acme.example", // egress
		"search (read)",    // MCP + scopes
		"Can create scheduled runs",
		"openai/gpt-5", // model
		`data-testid="market-install-confirm"`,
		`data-testid="market-install-back"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("review body missing %q", want)
		}
	}
	if strings.Contains(body, "data-error-code") {
		t.Errorf("happy-path review must not render an error: %s", body)
	}
	if s.cfg.MarketURL == "" {
		t.Fatal("test misconfigured: no market URL")
	}
}

func TestMarketInstallReviewUntrustedIssuerMakesNoBackendCall(t *testing.T) {
	f := &fakeMemory{marketResolveRes: marketResolveResult()}
	_, e := newMarketInstallServer(f, marketTestConfig())

	q := url.Values{"code": {"abc"}, "market": {"https://evil.example"}}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/marketplace/install?"+q.Encode(), nil))

	if f.marketResolveCalls != 0 {
		t.Fatalf("untrusted market reached the backend %d times", f.marketResolveCalls)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-error-code="market_issuer_untrusted"`) {
		t.Errorf("missing untrusted error code: %s", body)
	}
	if !strings.Contains(body, "not trusted by this instance") {
		t.Errorf("missing named untrusted message: %s", body)
	}
	// The back link points at the configured primary market, never the
	// untrusted origin.
	if strings.Contains(body, "https://evil.example") {
		t.Errorf("untrusted origin leaked into the page: %s", body)
	}
}

func TestMarketInstallReviewMapsServerErrors(t *testing.T) {
	cases := []struct {
		code     string
		contains string
	}{
		{"digest_mismatch", "does not match its licensed digest"},
		{"compat_unsatisfied", "not compatible with this Memory version"},
		{"license_invalid", "install licence is invalid"},
		{"install_code_invalid", "invalid, expired, or already used"},
		{"market_unreachable", "could not be reached"},
		{"undeclared_tools", "does not licence for install delivery"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			f := &fakeMemory{marketResolveErr: &memoryHTTPError{Status: http.StatusBadRequest, Code: tc.code, Message: "backend said no"}}
			_, e := newMarketInstallServer(f, marketTestConfig())

			q := url.Values{"code": {"abc"}, "market": {"https://market.example"}}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/marketplace/install?"+q.Encode(), nil))

			if f.marketResolveCalls != 1 {
				t.Fatalf("resolve calls = %d, want 1", f.marketResolveCalls)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `data-error-code="`+tc.code+`"`) {
				t.Errorf("missing error code %q: %s", tc.code, body)
			}
			if !strings.Contains(body, tc.contains) {
				t.Errorf("missing named message %q: %s", tc.contains, body)
			}
		})
	}
}

func TestMarketInstallConfirmRedirectsToBlueprint(t *testing.T) {
	f := &fakeMemory{marketApplyRes: &MarketApplyResult{BlueprintID: "bp-42", InstallID: "inst-1"}}
	_, e := newMarketInstallServer(f, marketTestConfig())

	form := url.Values{"market": {"https://market.example"}, "code": {"abc"}}
	req := httptest.NewRequest(http.MethodPost, "/marketplace/install", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/blueprints/bp-42" {
		t.Fatalf("Location = %q, want /blueprints/bp-42", got)
	}
	if f.marketApplyCalls != 1 || f.marketApplyProject != "proj-1" || f.marketApplyCode != "abc" {
		t.Fatalf("apply args = calls:%d project:%q code:%q", f.marketApplyCalls, f.marketApplyProject, f.marketApplyCode)
	}
}

func TestMarketInstallConfirmErrorRerendersReview(t *testing.T) {
	f := &fakeMemory{
		marketApplyErr:   &memoryHTTPError{Status: http.StatusBadRequest, Code: "digest_mismatch", Message: "bad digest"},
		marketResolveRes: marketResolveResult(),
	}
	_, e := newMarketInstallServer(f, marketTestConfig())

	form := url.Values{"market": {"https://market.example"}, "code": {"abc"}}
	req := httptest.NewRequest(http.MethodPost, "/marketplace/install", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-error-code="digest_mismatch"`) {
		t.Errorf("missing digest error: %s", body)
	}
	// The failed apply re-resolves so the reader still sees what failed.
	if !strings.Contains(body, `data-testid="market-install-review"`) || !strings.Contains(body, "Demo Listing") {
		t.Errorf("apply failure lost the review context: %s", body)
	}
}

func TestMarketInstallMissingProjectRedirectsToPicker(t *testing.T) {
	cfg := marketTestConfig()
	cfg.MemoryProjectID = ""
	f := &fakeMemory{marketResolveRes: marketResolveResult()}
	_, e := newMarketInstallServer(f, cfg)

	q := url.Values{"code": {"abc"}, "market": {"https://market.example"}}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/marketplace/install?"+q.Encode(), nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/orgs" {
		t.Fatalf("Location = %q, want /orgs", got)
	}
	if f.marketResolveCalls != 0 {
		t.Fatalf("missing project still resolved: %d calls", f.marketResolveCalls)
	}
}

func TestMarketInstallMissingProjectOnConfirmRedirects(t *testing.T) {
	cfg := marketTestConfig()
	cfg.MemoryProjectID = ""
	f := &fakeMemory{}
	_, e := newMarketInstallServer(f, cfg)

	form := url.Values{"market": {"https://market.example"}, "code": {"abc"}}
	req := httptest.NewRequest(http.MethodPost, "/marketplace/install", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/orgs" {
		t.Fatalf("status=%d location=%q, want 303 /orgs", rec.Code, rec.Header().Get("Location"))
	}
	if f.marketApplyCalls != 0 {
		t.Fatalf("missing project still applied: %d calls", f.marketApplyCalls)
	}
}

func TestMarketInstallReviewRendersModelMap(t *testing.T) {
	res := marketResolveResult()
	res.Agents = []MarketManifestAgent{
		{Name: "writer", Model: &MarketAgentModel{Name: "gpt-5", Provider: "openai"}},
		{Name: "reader"},
	}
	f := &fakeMemory{marketResolveRes: res}
	_, e := newMarketInstallServer(f, marketTestConfig())

	q := url.Values{"code": {"abc"}, "market": {"https://market.example"}}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/marketplace/install?"+q.Encode(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="market-install-model-review"`,
		`data-testid="blueprint-model-map-row"`,
		`name="model.writer"`,
		"Blueprint: openai/gpt-5",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("model-map review missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `name="model.reader"`) {
		t.Errorf("unpinned agent rendered a model row: %s", body)
	}
}

func TestMarketInstallReviewSkipsModelMapWhenUnpinned(t *testing.T) {
	f := &fakeMemory{marketResolveRes: marketResolveResult()}
	_, e := newMarketInstallServer(f, marketTestConfig())

	q := url.Values{"code": {"abc"}, "market": {"https://market.example"}}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/marketplace/install?"+q.Encode(), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `data-testid="market-install-model-review"`) {
		t.Errorf("unpinned listing rendered a model review: %s", rec.Body.String())
	}
}

func TestMarketInstallConfirmForwardsModelOverrides(t *testing.T) {
	f := &fakeMemory{marketApplyRes: &MarketApplyResult{BlueprintID: "bp-42", InstallID: "inst-1"}}
	_, e := newMarketInstallServer(f, marketTestConfig())

	form := url.Values{
		"market":       {"https://market.example"},
		"code":         {"abc"},
		"model.writer": {"openai/gpt-5"},
	}
	req := httptest.NewRequest(http.MethodPost, "/marketplace/install", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := f.marketApplyOverrides["writer"]; got != "openai/gpt-5" {
		t.Fatalf("model overrides = %#v", f.marketApplyOverrides)
	}
}
