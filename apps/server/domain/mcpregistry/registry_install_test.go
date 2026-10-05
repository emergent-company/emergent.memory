package mcpregistry

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestBlockedAddr(t *testing.T) {
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
			if got := blockedAddr(addr); got != tt.blocked {
				t.Errorf("blockedAddr(%s) = %v, want %v", tt.addr, got, tt.blocked)
			}
		})
	}
}

func TestValidateRegistryRemoteURL(t *testing.T) {
	accepted := []string{
		"https://8.8.8.8/mcp",
		"http://1.1.1.1:8080/sse",
		"https://[2606:4700:4700::1111]/mcp",
		// Hostnames are accepted without DNS: behind an egress proxy an
		// external name resolves to the proxy's private address, so the guard
		// must not reject on resolution.
		"https://api.githubcopilot.com/mcp/",
		"https://mcp.example.com/mcp",
	}
	for _, raw := range accepted {
		t.Run("accept "+raw, func(t *testing.T) {
			if err := validateRegistryRemoteURL(context.Background(), raw); err != nil {
				t.Fatalf("expected %q accepted, got %v", raw, err)
			}
		})
	}

	rejected := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"scheme", "ftp://8.8.8.8/mcp"},
		{"file scheme", "file:///etc/passwd"},
		{"loopback", "http://127.0.0.1/mcp"},
		{"loopback v6", "http://[::1]/mcp"},
		{"private", "http://10.1.2.3/mcp"},
		{"metadata", "http://169.254.169.254/latest/meta-data/"},
		{"unspecified", "http://0.0.0.0/mcp"},
		{"cgnat", "http://100.64.0.1/mcp"},
		{"ula v6", "http://[fc00::1]/mcp"},
		{"userinfo", "http://user:pass@8.8.8.8/mcp"},
		{"localhost name", "http://localhost/mcp"},
		{"metadata host", "http://metadata.google.internal/computeMetadata/v1/"},
		{"internal suffix", "http://db.internal/mcp"},
		{"local suffix", "http://printer.local/mcp"},
		{"no host", "http:///mcp"},
	}
	for _, tt := range rejected {
		t.Run("reject "+tt.name, func(t *testing.T) {
			if err := validateRegistryRemoteURL(context.Background(), tt.raw); err == nil {
				t.Fatalf("expected %q rejected, got nil", tt.raw)
			}
		})
	}
}

func TestSelectTransport(t *testing.T) {
	regWith := func(remotes []RegistryRemote, pkgs []RegistryPackage) RegistryServer {
		return RegistryServer{Name: "io.test/srv", Remotes: remotes, Packages: pkgs}
	}

	t.Run("prefers streamable-http over sse", func(t *testing.T) {
		got, err := selectTransport(context.Background(), regWith([]RegistryRemote{
			{Type: "sse", URL: "https://8.8.8.8/sse"},
			{Type: "streamable-http", URL: "https://8.8.8.8/mcp"},
		}, nil), "srv")
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != ServerTypeHTTP {
			t.Fatalf("type = %q, want http", got.Type)
		}
		if got.URL == nil || *got.URL != "https://8.8.8.8/mcp" {
			t.Fatalf("url = %v", got.URL)
		}
	})

	t.Run("falls back to sse", func(t *testing.T) {
		got, err := selectTransport(context.Background(), regWith([]RegistryRemote{
			{Type: "sse", URL: "https://8.8.8.8/sse"},
		}, nil), "srv")
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != ServerTypeSSE {
			t.Fatalf("type = %q, want sse", got.Type)
		}
	})

	t.Run("rejects ssrf remote", func(t *testing.T) {
		_, err := selectTransport(context.Background(), regWith([]RegistryRemote{
			{Type: "streamable-http", URL: "http://169.254.169.254/mcp"},
		}, nil), "srv")
		if !errors.Is(err, ErrRegistryRemoteNotAllowed) {
			t.Fatalf("err = %v, want ErrRegistryRemoteNotAllowed", err)
		}
	})

	t.Run("rejects stdio-only package", func(t *testing.T) {
		_, err := selectTransport(context.Background(), regWith(nil, []RegistryPackage{
			{RegistryType: "npm", Identifier: "@x/y", Transport: RegistryTransport{Type: "stdio"}},
		}), "srv")
		if !errors.Is(err, ErrUnsupportedRegistryTransport) {
			t.Fatalf("err = %v, want ErrUnsupportedRegistryTransport", err)
		}
		if !strings.Contains(err.Error(), "npm") {
			t.Fatalf("err should name offending package type: %v", err)
		}
	})

	t.Run("rejects empty definition", func(t *testing.T) {
		_, err := selectTransport(context.Background(), regWith(nil, nil), "srv")
		if !errors.Is(err, ErrUnsupportedRegistryTransport) {
			t.Fatalf("err = %v, want ErrUnsupportedRegistryTransport", err)
		}
	})
}

func TestBuildRemoteDTOSecretClassification(t *testing.T) {
	dto := buildRemoteDTO("srv", ServerTypeHTTP, RegistryRemote{
		Type: "streamable-http",
		URL:  "https://8.8.8.8/mcp",
		Headers: []RegistryHeader{
			{Name: "X-Api-Key", IsSecret: true, Value: "Bearer {api_key}"},
			{Name: "X-Trace", IsSecret: false, Value: "on"},
		},
		EnvironmentVariables: []RegistryEnvVar{
			{Name: "TOKEN", IsSecret: true},
			{Name: "LOG_LEVEL", IsSecret: false},
		},
	})

	if !contains(dto.SecretHeadersKeys, "X-Api-Key") {
		t.Errorf("X-Api-Key should be a secret header key; got %v", dto.SecretHeadersKeys)
	}
	if contains(dto.SecretHeadersKeys, "X-Trace") {
		t.Errorf("X-Trace must not be secret; got %v", dto.SecretHeadersKeys)
	}
	if !contains(dto.SecretEnvKeys, "TOKEN") {
		t.Errorf("TOKEN should be a secret env key; got %v", dto.SecretEnvKeys)
	}
	if contains(dto.SecretEnvKeys, "LOG_LEVEL") {
		t.Errorf("LOG_LEVEL must not be secret; got %v", dto.SecretEnvKeys)
	}
	if _, ok := dto.Headers["X-Trace"]; !ok {
		t.Errorf("non-secret header should remain in plaintext Headers map")
	}
}

func TestDeriveLocalName(t *testing.T) {
	tests := map[string]string{
		"io.github.github/github-mcp-server": "github-mcp-server",
		"io.github.user/my-server":           "my-server",
		"plainname":                          "plainname",
		"a/b/c":                              "c",
	}
	for in, want := range tests {
		if got := deriveLocalName(in); got != want {
			t.Errorf("deriveLocalName(%q) = %q, want %q", in, got, want)
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
