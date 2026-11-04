package outbound

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

// withResolver substitutes the DNS resolver used by the SSRF guard for the
// duration of the test, so no real DNS is required.
func withResolver(t *testing.T, fn func(ctx context.Context, host string) ([]netip.Addr, error)) {
	t.Helper()
	orig := LookupHostIPs
	LookupHostIPs = fn
	t.Cleanup(func() { LookupHostIPs = orig })
}

// TestValidateURLRejectsInternal is the rejected-URL matrix: literal loopback,
// RFC1918 private, link-local/metadata, unspecified, ULA, CGNAT, internal
// hostnames, bad schemes, and userinfo must all be refused under the default
// policy.
func TestValidateURLRejectsInternal(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/mcp",
		"http://[::1]/mcp",
		"http://10.1.2.3/mcp",
		"http://172.16.5.4/mcp",
		"http://192.168.0.1/mcp",
		"http://169.254.169.254/latest/meta-data/",
		"http://169.254.1.1/mcp",
		"http://0.0.0.0/mcp",
		"http://[fc00::1]/mcp",
		"http://[fe80::1]/mcp",
		"http://100.64.0.1/mcp",
		"http://240.0.0.1/mcp",
		"http://localhost/mcp",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://db.internal/mcp",
		"http://printer.local/mcp",
		"ftp://8.8.8.8/mcp",
		"file:///etc/passwd",
		"http://user:pass@8.8.8.8/mcp",
	}
	for _, raw := range blocked {
		t.Run("reject "+raw, func(t *testing.T) {
			if err := ValidateURL(context.Background(), raw, DefaultPolicy()); err == nil {
				t.Fatalf("expected %q rejected, got nil", raw)
			}
		})
	}
}

// TestValidateURLAcceptsPublicLiteral is the accepted-URL matrix for literal
// public addresses (no DNS needed).
func TestValidateURLAcceptsPublicLiteral(t *testing.T) {
	accepted := []string{
		"https://8.8.8.8/mcp",
		"http://1.1.1.1:8080/sse",
		"https://[2606:4700:4700::1111]/mcp",
	}
	for _, raw := range accepted {
		t.Run("accept "+raw, func(t *testing.T) {
			if err := ValidateURL(context.Background(), raw, DefaultPolicy()); err != nil {
				t.Fatalf("expected %q accepted, got %v", raw, err)
			}
		})
	}
}

// TestValidateURLResolvesHostnames proves the guard resolves hostnames and
// fails closed when they point into blocked ranges — the hostname-based SSRF
// gap the literal-only guard misses.
func TestValidateURLResolvesHostnames(t *testing.T) {
	public, _ := netip.ParseAddr("1.2.3.4")
	metadata, _ := netip.ParseAddr("169.254.169.254")
	private, _ := netip.ParseAddr("10.0.0.5")

	t.Run("public hostname accepted", func(t *testing.T) {
		withResolver(t, func(_ context.Context, host string) ([]netip.Addr, error) {
			if host != "mcp.example.com" {
				t.Fatalf("resolver got host %q, want mcp.example.com", host)
			}
			return []netip.Addr{public}, nil
		})
		if err := ValidateURL(context.Background(), "https://mcp.example.com/mcp", DefaultPolicy()); err != nil {
			t.Fatalf("expected accepted, got %v", err)
		}
	})

	t.Run("hostname resolving to metadata rejected", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{metadata}, nil
		})
		if err := ValidateURL(context.Background(), "https://looks-public.example/mcp", DefaultPolicy()); err == nil {
			t.Fatal("expected hostname resolving to metadata rejected")
		}
	})

	t.Run("hostname with any private address rejected", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{public, private}, nil
		})
		if err := ValidateURL(context.Background(), "https://dual-homed.example/mcp", DefaultPolicy()); err == nil {
			t.Fatal("expected hostname with a private address rejected")
		}
	})

	t.Run("resolution failure rejected", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return nil, errors.New("nxdomain")
		})
		if err := ValidateURL(context.Background(), "https://does-not-exist.example/mcp", DefaultPolicy()); err == nil {
			t.Fatal("expected unresolved hostname rejected")
		}
	})
}

// TestValidateURLAllowPrivate pins the test/dev escape hatch semantics: private
// and loopback addresses are permitted, but link-local/metadata (the dominant
// SSRF target) stays blocked under every policy.
func TestValidateURLAllowPrivate(t *testing.T) {
	loopback := netip.MustParseAddr("127.0.0.1")
	private := netip.MustParseAddr("10.0.0.5")
	metadata := netip.MustParseAddr("169.254.169.254")

	t.Run("private literal allowed", func(t *testing.T) {
		if err := ValidateURL(context.Background(), "http://10.0.0.5/", Policy{AllowPrivate: true}); err != nil {
			t.Fatalf("private literal must be allowed: %v", err)
		}
	})
	t.Run("loopback literal allowed", func(t *testing.T) {
		if err := ValidateURL(context.Background(), "http://127.0.0.1/", Policy{AllowPrivate: true}); err != nil {
			t.Fatalf("loopback literal must be allowed: %v", err)
		}
	})
	t.Run("metadata literal still blocked", func(t *testing.T) {
		if err := ValidateURL(context.Background(), "http://169.254.169.254/latest/meta-data/", Policy{AllowPrivate: true}); err == nil {
			t.Fatal("metadata literal must stay blocked under AllowPrivate")
		}
	})
	t.Run("link-local literal still blocked", func(t *testing.T) {
		if err := ValidateURL(context.Background(), "http://169.254.1.1/", Policy{AllowPrivate: true}); err == nil {
			t.Fatal("link-local literal must stay blocked under AllowPrivate")
		}
	})
	t.Run("private-resolving hostname allowed", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{private}, nil
		})
		if err := ValidateURL(context.Background(), "https://internal.example/", Policy{AllowPrivate: true}); err != nil {
			t.Fatalf("private-resolving hostname must be allowed: %v", err)
		}
	})
	t.Run("internal hostname allowed", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{loopback}, nil
		})
		if err := ValidateURL(context.Background(), "http://db.internal/", Policy{AllowPrivate: true}); err != nil {
			t.Fatalf("internal hostname must be allowed under AllowPrivate: %v", err)
		}
	})
	t.Run("metadata-resolving hostname still blocked", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{metadata}, nil
		})
		if err := ValidateURL(context.Background(), "https://metadata.google.internal/", Policy{AllowPrivate: true}); err == nil {
			t.Fatal("metadata-resolving hostname must stay blocked under AllowPrivate")
		}
	})
}

// TestValidateURLAllowedHosts pins the AllowedHosts allowlist: exact hosts and
// "*.suffix" wildcards bypass the private/internal rejection, but a wildcard
// must not match the bare domain.
func TestValidateURLAllowedHosts(t *testing.T) {
	private := netip.MustParseAddr("10.0.0.5")

	t.Run("exact host allowed", func(t *testing.T) {
		if err := ValidateURL(context.Background(), "https://wiki.internal/", Policy{AllowedHosts: []string{"wiki.internal"}}); err != nil {
			t.Fatalf("exact allowed host must be accepted: %v", err)
		}
	})
	t.Run("wildcard subdomain allowed", func(t *testing.T) {
		if err := ValidateURL(context.Background(), "https://foo.example.com/", Policy{AllowedHosts: []string{"*.example.com"}}); err != nil {
			t.Fatalf("wildcard subdomain must be accepted: %v", err)
		}
	})
	t.Run("wildcard does not match bare domain", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{private}, nil
		})
		if err := ValidateURL(context.Background(), "https://example.com/", Policy{AllowedHosts: []string{"*.example.com"}}); err == nil {
			t.Fatal("bare domain must not match *.example.com allowlist")
		}
	})
	t.Run("exact host does not match partial suffix", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{private}, nil
		})
		if err := ValidateURL(context.Background(), "https://notevil.internal/", Policy{AllowedHosts: []string{"evil.internal"}}); err == nil {
			t.Fatal("partial suffix must not match an exact allowlist entry")
		}
	})
}

// TestRedirectPolicy validates redirect targets and rejects internal and
// private ones.
func TestRedirectPolicy(t *testing.T) {
	pol := DefaultPolicy()

	internal, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/next", nil)
	if err := redirectPolicy(pol)(internal, nil); err == nil {
		t.Fatal("internal redirect target must be blocked")
	}

	private, _ := http.NewRequest(http.MethodGet, "http://10.0.0.1/next", nil)
	if err := redirectPolicy(pol)(private, nil); err == nil {
		t.Fatal("private redirect target must be blocked")
	}

	public, _ := http.NewRequest(http.MethodGet, "https://8.8.8.8/next", nil)
	if err := redirectPolicy(pol)(public, nil); err != nil {
		t.Fatalf("public redirect target should be allowed, got %v", err)
	}

	tooMany := make([]*http.Request, 5)
	if err := redirectPolicy(pol)(public, tooMany); err == nil {
		t.Fatal("redirect chain over the cap must be blocked")
	}
}

// TestGuardedDialContextBlocksLiteral proves the dial-time guard refuses a
// blocked literal address without invoking the underlying dialer.
func TestGuardedDialContextBlocksLiteral(t *testing.T) {
	dial := GuardedDialContext(func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("underlying dialer must not be called for a blocked address")
		return nil, nil
	}, DefaultPolicy())
	if _, err := dial(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("expected loopback dial blocked")
	}
	if _, err := dial(context.Background(), "tcp", "169.254.169.254:80"); err == nil {
		t.Fatal("expected metadata dial blocked")
	}
}

// TestGuardedDialContextBlocksResolvedPrivate proves the dial-time guard
// resolves hostnames and blocks private results (DNS-rebinding defence).
func TestGuardedDialContextBlocksResolvedPrivate(t *testing.T) {
	private, _ := netip.ParseAddr("10.0.0.9")
	withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{private}, nil
	})

	dial := GuardedDialContext(func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("underlying dialer must not be called for a blocked address")
		return nil, nil
	}, DefaultPolicy())
	if _, err := dial(context.Background(), "tcp", "rebind.example:443"); err == nil {
		t.Fatal("expected rebinding hostname dial blocked")
	}
}

// TestIsBlockedAddr is the blocked-address classification matrix.
func TestIsBlockedAddr(t *testing.T) {
	tests := []struct {
		addr    string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.5.4", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // cloud metadata
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		{"100.64.0.1", true}, // CGNAT
		{"240.0.0.1", true},  // reserved
		{"fc00::1", true},    // ULA
		{"fe80::1", true},    // link-local
		{"::ffff:127.0.0.1", true},
		{"224.0.0.1", true}, // multicast
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			addr, err := netip.ParseAddr(tt.addr)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.addr, err)
			}
			if got := IsBlockedAddr(addr); got != tt.blocked {
				t.Errorf("IsBlockedAddr(%s) = %v, want %v", tt.addr, got, tt.blocked)
			}
		})
	}
}

// TestIsInternalHostname covers the hostname denylist used by the
// install-time guard (no DNS involved).
func TestIsInternalHostname(t *testing.T) {
	internal := []string{
		"localhost",
		"metadata",
		"metadata.google.internal",
		"instance-data",
		"db.internal",
		"printer.local",
		"app.localhost",
	}
	for _, host := range internal {
		if !IsInternalHostname(host) {
			t.Errorf("IsInternalHostname(%q) = false, want true", host)
		}
	}
	public := []string{"example.com", "api.github.com", "8.8.8.8"}
	for _, host := range public {
		if IsInternalHostname(host) {
			t.Errorf("IsInternalHostname(%q) = true, want false", host)
		}
	}
}

// TestParseBlockedHost covers the literal-IP fast path of the install-time
// guard.
func TestParseBlockedHost(t *testing.T) {
	blocked := []string{"127.0.0.1", "169.254.169.254", "10.0.0.1", "::1", "fc00::1"}
	for _, host := range blocked {
		if !ParseBlockedHost(host) {
			t.Errorf("ParseBlockedHost(%q) = false, want true", host)
		}
	}
	public := []string{"8.8.8.8", "1.1.1.1", "example.com", "2606:4700:4700::1111"}
	for _, host := range public {
		if ParseBlockedHost(host) {
			t.Errorf("ParseBlockedHost(%q) = true, want false", host)
		}
	}
}

// TestGuardedDialContextHonoursAllowlistHost proves an AllowedHosts entry is
// honoured at dial time, not only at pre-flight: a host the operator trusts that
// resolves into private space is dialable, while the default policy still blocks
// it. Regression guard for the allowlist being ignored by the dial guard.
func TestGuardedDialContextHonoursAllowlistHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			conn.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listener addr: %v", err)
	}
	loopback, _ := netip.ParseAddr("127.0.0.1")
	withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{loopback}, nil
	})

	addr := net.JoinHostPort("trusted.internal", port)
	if _, err := GuardedDialContext(nil, DefaultPolicy())(context.Background(), "tcp", addr); err == nil {
		t.Fatal("expected a loopback resolution to be blocked under the default policy")
	}

	conn, err := GuardedDialContext(nil, Policy{AllowedHosts: []string{"trusted.internal"}})(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("allowlisted private host must be dialable, got %v", err)
	}
	conn.Close()
}

// TestGuardedDialContextAllowlistCannotOpenMetadata pins the invariant that no
// policy — allowlist entry or AllowPrivate — opens the always-blocked ranges.
func TestGuardedDialContextAllowlistCannotOpenMetadata(t *testing.T) {
	meta, _ := netip.ParseAddr("169.254.169.254")
	withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{meta}, nil
	})

	for name, pol := range map[string]Policy{
		"allowlisted host": {AllowedHosts: []string{"metadata.internal"}},
		"allow private":    {AllowPrivate: true},
		"both":             {AllowedHosts: []string{"metadata.internal"}, AllowPrivate: true},
	} {
		t.Run(name, func(t *testing.T) {
			dial := GuardedDialContext(func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("underlying dialer must not be called for the metadata address")
				return nil, nil
			}, pol)
			if _, err := dial(context.Background(), "tcp", "metadata.internal:80"); err == nil {
				t.Fatal("expected the metadata address to stay blocked")
			}
		})
	}
}

// TestValidateURLAllowlistCannotOpenMetadata proves an allowlist entry naming the
// metadata address, or AllowPrivate, cannot pass pre-flight validation.
func TestValidateURLAllowlistCannotOpenMetadata(t *testing.T) {
	cases := map[string]Policy{
		"literal in allowlist": {AllowedHosts: []string{"169.254.169.254"}},
		"allow private":        {AllowPrivate: true},
	}
	for name, pol := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateURL(context.Background(), "http://169.254.169.254/latest/meta-data/", pol); err == nil {
				t.Fatal("metadata address must be rejected under every policy")
			}
		})
	}
}
