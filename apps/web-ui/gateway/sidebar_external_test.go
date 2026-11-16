package main

import (
	"strings"
	"testing"

	"github.com/emergent-company/go-daisy/components/layout"
)

// The Docs entry is a second external destination in the same bottom block as
// the Marketplace, so these tests pin both halves of the contract: it renders
// next to the marketplace (not in the scrolling nav), and it is hidden when no
// MEMORY_DOCS_URL is configured — a self-hosted instance must not be handed a
// link to somebody else's documentation.

func TestSidebarExternalLinksPutsDocsAfterMarketplace(t *testing.T) {
	s := &Server{cfg: Config{
		MarketURL: "https://market.example",
		DocsURL:   "https://docs.example",
	}}
	items := s.sidebarExternalLinks()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (marketplace, docs)", len(items))
	}
	if items[0].Label != "Marketplace" || items[0].Href != "https://market.example" {
		t.Fatalf("first item = %+v, want the marketplace", items[0])
	}
	if items[1].Label != "Docs" || items[1].Href != "https://docs.example" {
		t.Fatalf("second item = %+v, want docs", items[1])
	}
	if items[1].Icon != "lucide--book-open" {
		t.Fatalf("docs icon = %q, want lucide--book-open", items[1].Icon)
	}
}

func TestSidebarExternalLinksDocsOnly(t *testing.T) {
	// No marketplace configured, docs configured: the block must still exist,
	// with one entry. An `if len(market) > 0` gate on the wrapper would hide it.
	s := &Server{cfg: Config{DocsURL: "https://docs.example"}}
	html := renderHTML(t, appSidebar(sidebarGroupsWithWarn("", false, sidebarWarns{}), s.sidebarExternalLinks(), false))
	block := strings.Index(html, `data-testid="sidebar-external"`)
	if block < 0 {
		t.Fatal("docs entry did not render its own bottom block")
	}
	if !strings.Contains(html[block:], `>Docs</span>`) {
		t.Fatalf("docs label missing from the bottom block: %s", html[block:])
	}
	link := externalLinkAt(t, html, block, "https://docs.example")
	// Two entries now share one data-testid, so a bare getByTestId is a Playwright
	// strict-mode violation. data-href is the per-entry hook a selector can use.
	if !strings.Contains(link, `data-href="https://docs.example"`) {
		t.Fatalf("docs link carries no data-href selector hook: %s", link)
	}
	if strings.Contains(link, "hx-get") || strings.Contains(link, "hx-target") {
		t.Fatalf("docs link carries htmx attributes and the click is intercepted: %s", link)
	}
	if !strings.Contains(link, `target="_blank"`) || !strings.Contains(link, `rel="noopener noreferrer"`) {
		t.Fatalf("docs link is not an external noopener link: %s", link)
	}
}

func TestSidebarExternalBlockHiddenWhenNothingConfigured(t *testing.T) {
	s := &Server{}
	html := renderHTML(t, appSidebar(sidebarGroupsWithWarn("", false, sidebarWarns{}), s.sidebarExternalLinks(), false))
	if strings.Contains(html, `data-testid="sidebar-external"`) {
		t.Fatal("bottom block rendered with no external destination configured")
	}
}

// externalLinkAt returns the <a …>…</a> slice for href, searching from offset.
func externalLinkAt(t *testing.T, html string, from int, href string) string {
	t.Helper()
	marker := strings.Index(html[from:], `href="`+href+`"`)
	if marker < 0 {
		t.Fatalf("no anchor for %s in the bottom block", href)
	}
	marker += from
	start := strings.LastIndex(html[:marker], "<a")
	if start < 0 {
		t.Fatalf("anchor start missing for %s", href)
	}
	link := html[start:]
	if end := strings.Index(link, "</a>"); end >= 0 {
		link = link[:end]
	}
	return link
}

func TestLoadDocsURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"unset hides the entry", "", "", false},
		{"plain origin", "https://docs.memory.emergent-company.ai", "https://docs.memory.emergent-company.ai", false},
		{"trailing slash is dropped", "https://docs.example/", "https://docs.example", false},
		{"a path is allowed", "https://docs.example/guide", "https://docs.example/guide", false},
		{"whitespace is trimmed", "  https://docs.example  ", "https://docs.example", false},
		{"relative is refused", "/docs", "", true},
		{"no host is refused", "https://", "", true},
		{"non-http scheme is refused", "file:///srv/docs", "", true},
		{"credentials are refused", "https://user:pw@docs.example", "", true},
		{"a query is refused", "https://docs.example/?a=1", "", true},
		{"a fragment is refused", "https://docs.example/#intro", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MEMORY_DOCS_URL", tc.raw)
			got, err := loadDocsURL()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("loadDocsURL(%q) = %q, want an error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadDocsURL(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("loadDocsURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// The block must stay the sidebar's last element with Docs in it — the version
// footer test covers the marketplace alone, and this is the same assertion for
// the two-entry case.
func TestDocsEntryStaysInsideThePinnedBottomBlock(t *testing.T) {
	items := []layout.SidebarItem{
		{Label: "Marketplace", Href: "https://market.example", Icon: "lucide--store"},
		{Label: "Docs", Href: "https://docs.example", Icon: "lucide--book-open"},
	}
	html := renderHTML(t, appSidebar(sidebarGroupsWithWarn("", false, sidebarWarns{}), items, false))
	nav := strings.Index(html, `class="sidebar-menu grow overflow-y-auto"`)
	version := strings.Index(html, `data-testid="app-version"`)
	block := strings.Index(html, `data-testid="sidebar-external"`)
	if nav < 0 || version < 0 || block < 0 {
		t.Fatalf("anchors missing (nav=%d version=%d block=%d)", nav, version, block)
	}
	if nav >= version || version >= block {
		t.Fatalf("bottom block is not after the version footer: nav=%d version=%d block=%d", nav, version, block)
	}
	docs := strings.Index(html, `href="https://docs.example"`)
	if docs < block {
		t.Fatal("docs link is not inside the bottom block")
	}
	navOpen := strings.LastIndex(html[:nav], "<div")
	if navClose := divCloseIndex(html, navOpen); navClose < 0 || block < navClose {
		t.Fatalf("bottom block is inside the scrolling nav (nav close=%d block=%d)", navClose, block)
	}
}
