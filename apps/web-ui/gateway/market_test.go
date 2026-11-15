package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emergent-company/go-daisy/components/layout"
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

func TestMarketIssuerLabelRealHosts(t *testing.T) {
	cases := []struct {
		issuer string
		want   string
	}{
		{"https://market.dev.memory.emergent-company.ai", "Marketplace (dev)"},
		{"https://dev.market.example", "Marketplace (dev)"},
		{"https://market.memory.emergent-company.ai", "Marketplace (prod)"},
		{"https://prod.market.example", "Marketplace (prod)"},
	}
	for _, tc := range cases {
		if got := marketIssuerLabel(tc.issuer); got != tc.want {
			t.Errorf("marketIssuerLabel(%q) = %q, want %q", tc.issuer, got, tc.want)
		}
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
	}
}

func TestMarketSidebarItemsHiddenWhenUnconfigured(t *testing.T) {
	s := &Server{}
	if items := s.marketSidebarItems(); len(items) != 0 {
		t.Fatalf("items = %#v, want none", items)
	}
	html := renderHTML(t, appSidebar(sidebarGroupsWithWarn("", false, sidebarWarns{}), s.marketSidebarItems(), false))
	if strings.Contains(html, `data-testid="sidebar-marketplace"`) {
		t.Fatal("Marketplace block rendered without configuration")
	}
}

func TestMarketplaceRendersAtSidebarBottom(t *testing.T) {
	items := []layout.SidebarItem{{Label: "Marketplace", Href: "https://market.example", Icon: "lucide--store"}}
	html := renderHTML(t, appSidebar(sidebarGroupsWithWarn("", false, sidebarWarns{}), items, false))
	footer := strings.Index(html, `data-testid="sidebar-marketplace"`)
	if footer < 0 {
		t.Fatal("marketplace block missing from the sidebar")
	}
	nav := strings.Index(html, `class="sidebar-menu grow overflow-y-auto"`)
	version := strings.Index(html, `data-testid="app-version"`)
	if nav < 0 || version < 0 {
		t.Fatalf("sidebar anchors missing (nav=%d version=%d)", nav, version)
	}
	if nav >= version || version >= footer {
		t.Fatalf("marketplace is not the sidebar's last element: nav=%d version=%d market=%d", nav, version, footer)
	}
	navOpen := strings.LastIndex(html[:nav], "<div")
	navClose := divCloseIndex(html, navOpen)
	if navClose < 0 || footer < navClose {
		t.Fatalf("marketplace block is inside the scrollable nav (nav close=%d market=%d), not pinned", navClose, footer)
	}
	blockStart := strings.LastIndex(html[:footer], "<div")
	if blockStart < 0 || !strings.Contains(html[blockStart:footer], "shrink-0") {
		t.Fatalf("marketplace block is not a shrink-0 sibling: %s", html[blockStart:footer])
	}
	if !strings.Contains(html[footer:], "Marketplace") {
		t.Fatal("marketplace label missing from its footer block")
	}
	block := html[footer:]
	marker := strings.Index(block, `data-testid="sidebar-marketplace-link"`)
	if marker < 0 {
		t.Fatal("marketplace link missing from its footer block")
	}
	linkStart := strings.LastIndex(block[:marker], "<a")
	if linkStart < 0 {
		t.Fatal("marketplace anchor start missing")
	}
	link := block[linkStart:]
	if end := strings.Index(link, "</a>"); end >= 0 {
		link = link[:end]
	}
	if strings.Contains(link, "hx-get") || strings.Contains(link, "hx-target") {
		t.Fatalf("marketplace link carries htmx attributes and the click is intercepted: %s", link)
	}
	if !strings.Contains(link, `target="_blank"`) || !strings.Contains(link, `rel="noopener noreferrer"`) {
		t.Fatalf("marketplace link is not an external noopener link: %s", link)
	}
}

// divCloseIndex returns the offset of the </div> that closes the <div> opening
// at open, counting nested divs. The sidebar test uses it to prove a block is a
// sibling after the scroll container rather than nested inside it.
func divCloseIndex(html string, open int) int {
	depth := 0
	for i := open; i < len(html); {
		switch {
		case strings.HasPrefix(html[i:], "<div"):
			depth++
			i += 4
		case strings.HasPrefix(html[i:], "</div>"):
			depth--
			if depth == 0 {
				return i
			}
			i += 6
		default:
			i++
		}
	}
	return -1
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
