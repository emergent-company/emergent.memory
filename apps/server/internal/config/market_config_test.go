package config

import "testing"

func TestMarketConfigNormalizeDefaultsAllowlistToPrimary(t *testing.T) {
	m := MarketConfig{URL: " https://Market.Example/ "}
	if err := m.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if m.URL != "https://market.example" {
		t.Fatalf("URL = %q", m.URL)
	}
	if len(m.TrustedIssuers) != 1 || m.TrustedIssuers[0] != "https://market.example" {
		t.Fatalf("trusted = %#v", m.TrustedIssuers)
	}
}

func TestMarketConfigNormalizeExplicitAllowlist(t *testing.T) {
	m := MarketConfig{
		URL:               "https://market.example",
		TrustedIssuersRaw: "https://dev.market.example, https://market.example, https://dev.market.example",
	}
	if err := m.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(m.TrustedIssuers) != 2 {
		t.Fatalf("trusted = %#v", m.TrustedIssuers)
	}
	if !m.IsTrustedIssuer("https://dev.market.example") || !m.IsTrustedIssuer("https://market.example") {
		t.Fatalf("expected both issuers trusted: %#v", m.TrustedIssuers)
	}
	if m.IsTrustedIssuer("https://prod.market.example") {
		t.Fatalf("unexpected trust for unlisted issuer")
	}
}

func TestMarketConfigNormalizeRejectsMalformed(t *testing.T) {
	cases := []struct {
		url     string
		trusted string
	}{
		{url: "https://market.example/path"},
		{url: "https://market.example?q=1"},
		{url: "https://market.example#frag"},
		{url: "https://user:pass@market.example"},
		{url: "ftp://market.example"},
		{url: "market.example"},
		{trusted: "https://market.example/path"},
	}
	for _, tc := range cases {
		m := MarketConfig{URL: tc.url, TrustedIssuersRaw: tc.trusted}
		if err := m.Normalize(); err == nil {
			t.Fatalf("expected refusal for url=%q trusted=%q", tc.url, tc.trusted)
		}
	}
}

func TestMarketConfigEmptyDisablesMarket(t *testing.T) {
	m := MarketConfig{}
	if err := m.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if m.Enabled() {
		t.Fatalf("empty market should be disabled")
	}
	if len(m.TrustedIssuers) != 0 {
		t.Fatalf("trusted = %#v", m.TrustedIssuers)
	}
}
