package mcpregistry

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

// withResolver substitutes the DNS resolver used by the SSRF guard for the
// duration of the test, so no real DNS is required.
func withResolver(t *testing.T, fn func(ctx context.Context, host string) ([]netip.Addr, error)) {
	t.Helper()
	orig := lookupHostIPs
	lookupHostIPs = fn
	t.Cleanup(func() { lookupHostIPs = orig })
}

// TestValidateOutboundFetchURLRejectsInternal is the rejected-URL matrix:
// literal loopback, RFC1918 private, link-local/metadata, unspecified, ULA,
// CGNAT, internal hostnames, bad schemes, and userinfo must all be refused.
func TestValidateOutboundFetchURLRejectsInternal(t *testing.T) {
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
			if err := validateOutboundFetchURL(context.Background(), raw); err == nil {
				t.Fatalf("expected %q rejected, got nil", raw)
			}
		})
	}
}

// TestValidateOutboundFetchURLAcceptsPublicLiteral is the accepted-URL matrix
// for literal public addresses (no DNS needed).
func TestValidateOutboundFetchURLAcceptsPublicLiteral(t *testing.T) {
	accepted := []string{
		"https://8.8.8.8/mcp",
		"http://1.1.1.1:8080/sse",
		"https://[2606:4700:4700::1111]/mcp",
	}
	for _, raw := range accepted {
		t.Run("accept "+raw, func(t *testing.T) {
			if err := validateOutboundFetchURL(context.Background(), raw); err != nil {
				t.Fatalf("expected %q accepted, got %v", raw, err)
			}
		})
	}
}

// TestValidateOutboundFetchURLResolvesHostnames proves the guard resolves
// hostnames and fails closed when they point into blocked ranges — the
// hostname-based SSRF gap the literal-only guard misses.
func TestValidateOutboundFetchURLResolvesHostnames(t *testing.T) {
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
		if err := validateOutboundFetchURL(context.Background(), "https://mcp.example.com/mcp"); err != nil {
			t.Fatalf("expected accepted, got %v", err)
		}
	})

	t.Run("hostname resolving to metadata rejected", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{metadata}, nil
		})
		if err := validateOutboundFetchURL(context.Background(), "https://looks-public.example/mcp"); err == nil {
			t.Fatal("expected hostname resolving to metadata rejected")
		}
	})

	t.Run("hostname with any private address rejected", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{public, private}, nil
		})
		if err := validateOutboundFetchURL(context.Background(), "https://dual-homed.example/mcp"); err == nil {
			t.Fatal("expected hostname with a private address rejected")
		}
	})

	t.Run("resolution failure rejected", func(t *testing.T) {
		withResolver(t, func(context.Context, string) ([]netip.Addr, error) {
			return nil, errors.New("nxdomain")
		})
		if err := validateOutboundFetchURL(context.Background(), "https://does-not-exist.example/mcp"); err == nil {
			t.Fatal("expected unresolved hostname rejected")
		}
	})
}

// TestGuardOutboundFetchURL covers the per-transport helper: stdio has no URL,
// remote types are guarded, and rejections are typed.
func TestGuardOutboundFetchURL(t *testing.T) {
	url := func(s string) *string { return &s }

	if err := guardOutboundFetchURL(context.Background(), ServerTypeStdio, nil); err != nil {
		t.Fatalf("stdio must not be guarded: %v", err)
	}

	for _, tc := range []struct {
		name string
		typ  MCPServerType
		raw  string
	}{
		{"loopback http", ServerTypeHTTP, "http://127.0.0.1/mcp"},
		{"metadata sse", ServerTypeSSE, "http://169.254.169.254/latest/meta-data/"},
		{"private http", ServerTypeHTTP, "http://192.168.1.10:8080/mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := guardOutboundFetchURL(context.Background(), tc.typ, url(tc.raw))
			if !errors.Is(err, ErrOutboundURLNotAllowed) {
				t.Fatalf("err = %v, want ErrOutboundURLNotAllowed", err)
			}
		})
	}

	if err := guardOutboundFetchURL(context.Background(), ServerTypeHTTP, url("https://8.8.8.8/mcp")); err != nil {
		t.Fatalf("public URL must be allowed: %v", err)
	}
}

// TestTestServerConfigRejectsSSRFURL is the integration/mutation check for the
// new /test probe: without the guard the probe would try to open an outbound
// connection to the metadata address rather than returning the typed 400 error.
func TestTestServerConfigRejectsSSRFURL(t *testing.T) {
	svc, err := NewService(nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	raw := "http://169.254.169.254/latest/meta-data/"
	_, err = svc.TestServerConfig(context.Background(), "p1", &CreateMCPServerDTO{
		Name: "ssrf-probe",
		Type: ServerTypeHTTP,
		URL:  &raw,
	}, nil)
	if !errors.Is(err, ErrOutboundURLNotAllowed) {
		t.Fatalf("err = %v, want ErrOutboundURLNotAllowed", err)
	}
}

// TestCreateServerRejectsSSRFURL covers the manual create path: the URL is
// rejected before any repository write.
func TestCreateServerRejectsSSRFURL(t *testing.T) {
	svc, err := NewService(nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	raw := "http://10.0.0.1/mcp"
	_, err = svc.CreateServer(context.Background(), "p1", &CreateMCPServerDTO{
		Name: "ssrf-create",
		Type: ServerTypeHTTP,
		URL:  &raw,
	})
	if !errors.Is(err, ErrOutboundURLNotAllowed) {
		t.Fatalf("err = %v, want ErrOutboundURLNotAllowed", err)
	}
}

// TestSafeRedirectPolicy validates redirect targets and rejects internal ones.
func TestSafeRedirectPolicy(t *testing.T) {
	internal, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/next", nil)
	if err := safeRedirectPolicy(internal, nil); err == nil {
		t.Fatal("internal redirect target must be blocked")
	}

	public, _ := http.NewRequest(http.MethodGet, "https://8.8.8.8/next", nil)
	if err := safeRedirectPolicy(public, nil); err != nil {
		t.Fatalf("public redirect target should be allowed, got %v", err)
	}

	tooMany := make([]*http.Request, 5)
	if err := safeRedirectPolicy(public, tooMany); err == nil {
		t.Fatal("redirect chain over the cap must be blocked")
	}
}

// TestGuardedDialContextBlocksLiteral proves the dial-time guard refuses a
// blocked literal address without invoking the underlying dialer.
func TestGuardedDialContextBlocksLiteral(t *testing.T) {
	dial := guardedDialContext(func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("underlying dialer must not be called for a blocked address")
		return nil, nil
	})
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

	dial := guardedDialContext(func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("underlying dialer must not be called for a blocked address")
		return nil, nil
	})
	if _, err := dial(context.Background(), "tcp", "rebind.example:443"); err == nil {
		t.Fatal("expected rebinding hostname dial blocked")
	}
}
