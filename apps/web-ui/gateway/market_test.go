package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestLoadMarketConfigDefaultsAllowlistToPrimary(t *testing.T) {
	t.Setenv("MEMORY_MARKET_URL", "https://Market.Example/")
	t.Setenv("MEMORY_MARKET_TRUSTED_ISSUERS", "")
	primary, issuers, err := loadMarketConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if primary != "https://market.example" {
		t.Fatalf("primary = %q", primary)
	}
	if len(issuers) != 1 || issuers[0] != "https://market.example" {
		t.Fatalf("issuers = %#v", issuers)
	}
}

func TestLoadMarketConfigMultipleIssuersPrimaryFirst(t *testing.T) {
	t.Setenv("MEMORY_MARKET_URL", "https://market.example")
	t.Setenv("MEMORY_MARKET_TRUSTED_ISSUERS", "https://dev.market.example, https://market.example, https://dev.market.example")
	_, issuers, err := loadMarketConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"https://dev.market.example", "https://market.example"}
	if len(issuers) != len(want) {
		t.Fatalf("issuers = %#v", issuers)
	}
	for i := range want {
		if issuers[i] != want[i] {
			t.Fatalf("issuers[%d] = %q, want %q", i, issuers[i], want[i])
		}
	}
}

func TestLoadMarketConfigRejectsMalformedIssuers(t *testing.T) {
	cases := []string{
		"https://market.example/path",
		"https://market.example?query=1",
		"https://user:pass@market.example",
		"ftp://market.example",
		"market.example",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("MEMORY_MARKET_URL", raw)
			t.Setenv("MEMORY_MARKET_TRUSTED_ISSUERS", "")
			if _, _, err := loadMarketConfig(); err == nil {
				t.Fatalf("expected %q to be refused", raw)
			}
		})
	}
}

func TestMarketSidebarItemsLabelsAndExternalAttrs(t *testing.T) {
	s := &Server{cfg: Config{
		MarketURL:            "https://market.example",
		MarketTrustedIssuers: []string{"https://market.example", "https://dev.market.example"},
	}}
	items := s.marketSidebarItems()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Label != "Marketplace" || items[0].Href != "https://market.example" {
		t.Fatalf("primary = %+v", items[0])
	}
	if items[1].Label != "Marketplace (dev)" || items[1].Href != "https://dev.market.example" {
		t.Fatalf("secondary = %+v", items[1])
	}
	for i, item := range items {
		if item.Icon != "lucide--store" {
			t.Fatalf("item %d icon = %q", i, item.Icon)
		}
		if item.Attrs["target"] != "_blank" || item.Attrs["rel"] != "noopener noreferrer" {
			t.Fatalf("item %d attrs = %#v", i, item.Attrs)
		}
	}
}

func TestMarketSidebarItemsHiddenWhenUnconfigured(t *testing.T) {
	s := &Server{}
	if items := s.marketSidebarItems(); len(items) != 0 {
		t.Fatalf("items = %#v, want none", items)
	}
	for _, group := range sidebarGroupsWithMarket("", false, sidebarWarns{}, s.marketSidebarItems()) {
		for _, item := range group.Items {
			if item.Label == "Marketplace" {
				t.Fatalf("Marketplace rendered without configuration")
			}
		}
	}
}

func TestMarketSidebarSecondaryLabelsNeverCollide(t *testing.T) {
	s := &Server{cfg: Config{
		MarketURL:            "https://market.example",
		MarketTrustedIssuers: []string{"https://prod.market.example", "https://prod2.market.example"},
	}}
	seen := map[string]bool{}
	for _, item := range s.marketSidebarItems() {
		if seen[item.Label] {
			t.Fatalf("duplicate accessible name %q", item.Label)
		}
		seen[item.Label] = true
	}
}

func TestMemoryInstanceWellKnown(t *testing.T) {
	s := &Server{cfg: Config{MarketTrustedIssuers: []string{"https://market.example"}}}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/memory-instance", nil)
	rec := httptest.NewRecorder()
	if err := s.memoryInstanceWellKnown(e.NewContext(req, rec)); err != nil {
		t.Fatalf("well-known: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("cache = %q", got)
	}
	var doc struct {
		Name    string   `json:"name"`
		Version string   `json:"version"`
		Install bool     `json:"install"`
		Markets []string `json:"markets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Name != "Memory" || !doc.Install || len(doc.Markets) != 1 || doc.Markets[0] != "https://market.example" {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestPublicAuthPathAllowsWellKnown(t *testing.T) {
	if !publicAuthPath("/.well-known/memory-instance") {
		t.Fatalf("well-known must be public")
	}
}
