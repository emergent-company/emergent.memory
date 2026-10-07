package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain swaps the provider probe client for a plain one so existing
// provider tests can reach their httptest (loopback) servers. The SSRF guard
// itself is covered by constructing newSSRFSafeHTTPClient directly.
func TestMain(m *testing.M) {
	providerProbeHTTPClient = &http.Client{Timeout: 10 * time.Second}
	os.Exit(m.Run())
}

func TestIsDialablePublicIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", false},
		{"::1", false},
		{"10.0.0.1", false},
		{"172.16.5.4", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false},
		{"100.100.100.200", false},
		{"fc00::1", false},
		{"0.0.0.0", false},
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
	}
	for _, tc := range tests {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("bad test ip %q", tc.ip)
		}
		if got := isDialablePublicIP(ip); got != tc.want {
			t.Errorf("isDialablePublicIP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestSSRFSafeClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newSSRFSafeHTTPClient(2*time.Second, nil)
	resp, err := client.Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected loopback request to be blocked, got success")
	}
}

func TestSSRFSafeClientAllowsListedHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newSSRFSafeHTTPClient(2*time.Second, newDialAllowlist([]string{"127.0.0.1"}))
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("allowlisted loopback host should be dialable: %v", err)
	}
	_ = resp.Body.Close()
}

func TestSSRFSafeClientAllowsListedCIDR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newSSRFSafeHTTPClient(2*time.Second, newDialAllowlist([]string{"127.0.0.0/8"}))
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("allowlisted loopback CIDR should be dialable: %v", err)
	}
	_ = resp.Body.Close()
}

func TestSSRFSafeClientBlocksUnlistedPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newSSRFSafeHTTPClient(2*time.Second, newDialAllowlist([]string{"example.com"}))
	resp, err := client.Get(srv.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("unlisted loopback should stay blocked")
	}
}

func TestDialAllowlistPermits(t *testing.T) {
	a := newDialAllowlist([]string{"Litellm", "100.64.0.0/10", "  ", "not a host"})
	if a == nil {
		t.Fatal("expected allowlist")
	}
	if !a.permitsHost("litellm") || !a.permitsHost("LITELLM") {
		t.Error("allowlist host matching should be case-insensitive")
	}
	if a.permitsHost("other") {
		t.Error("unlisted host should not be permitted")
	}
	if !a.permitsIP(net.ParseIP("100.113.48.6")) {
		t.Error("100.113.48.6 should be inside 100.64.0.0/10")
	}
	if a.permitsIP(net.ParseIP("169.254.169.254")) {
		t.Error("cloud metadata address must stay blocked")
	}
	if a.permitsIP(net.ParseIP("8.8.8.8")) {
		t.Error("public address should not match an allowlisted private CIDR")
	}
}

func TestNewDialAllowlistIgnoresInvalid(t *testing.T) {
	a := newDialAllowlist([]string{"999.999.999.999/33", ""})
	if a != nil {
		t.Fatalf("expected nil allowlist for invalid-only entries, got %+v", a)
	}
}

// TestDialAllowlistRejectsLookalikeHosts pins the matcher as EXACT: an
// allowlisted host must not be reachable via a suffix/prefix/substring match
// (no ".internal"-style wildcard). Regression guard for the SSRF allowlist.
func TestDialAllowlistRejectsLookalikeHosts(t *testing.T) {
	a := newDialAllowlist([]string{"litellm"})
	for _, h := range []string{
		"evil.litellm", "litellm.evil.example", "litellm.evil.com",
		"notlitellm", "litellm.", "litellm-", "xlitellm",
	} {
		if a.permitsHost(h) {
			t.Errorf("lookalike host %q must NOT be permitted", h)
		}
	}
	if !a.permitsHost("litellm") || !a.permitsHost("LITELLM") {
		t.Error("exact host must be permitted case-insensitively")
	}
}

// TestSSRFSafeClientBlocksRedirectToUnlistedPrivate confirms a redirect chain is
// re-validated per hop: an allowlisted origin server redirecting to an
// unlisted non-public host is still refused at dial time.
func TestSSRFSafeClientBlocksRedirectToUnlistedPrivate(t *testing.T) {
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.255.255.1/models", http.StatusFound)
	}))
	defer redirector.Close()

	client := newSSRFSafeHTTPClient(2*time.Second, newDialAllowlist([]string{"127.0.0.1"}))
	resp, err := client.Get(redirector.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("redirect to an unlisted private host must be blocked")
	}
	if !strings.Contains(err.Error(), "refusing to connect to non-public address") {
		t.Fatalf("expected SSRF refusal, got: %v", err)
	}
}
