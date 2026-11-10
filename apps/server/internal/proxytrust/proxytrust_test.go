package proxytrust

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name  string
		cidrs []string
		peer  string
		xff   string
		want  string
	}{
		{
			name: "fail closed: unset ignores spoofed XFF",
			peer: "203.0.113.9:5555",
			xff:  "1.2.3.4",
			want: "203.0.113.9",
		},
		{
			name: "fail closed: loopback is not implicitly trusted",
			peer: "127.0.0.1:5555",
			xff:  "1.2.3.4",
			want: "127.0.0.1",
		},
		{
			name: "fail closed: private peer is not implicitly trusted",
			peer: "192.168.1.5:5555",
			xff:  "1.2.3.4",
			want: "192.168.1.5",
		},
		{
			name:  "trusted proxy resolves the client",
			cidrs: []string{"10.0.0.0/8"},
			peer:  "10.0.0.5:443",
			xff:   "1.2.3.4",
			want:  "1.2.3.4",
		},
		{
			name:  "trusted proxy resolves right-most untrusted hop",
			cidrs: []string{"10.0.0.0/8"},
			peer:  "10.0.0.5:443",
			xff:   "1.2.3.4, 10.0.0.7, 10.1.2.3",
			want:  "1.2.3.4",
		},
		{
			name:  "trusted range does not extend to an untrusted peer",
			cidrs: []string{"10.0.0.0/8"},
			peer:  "203.0.113.9:443",
			xff:   "1.2.3.4",
			want:  "203.0.113.9",
		},
		{
			name:  "private hop is not trusted unless the range says so",
			cidrs: []string{"10.0.0.0/8"},
			peer:  "10.0.0.5:443",
			xff:   "1.2.3.4, 192.168.5.5",
			want:  "192.168.5.5",
		},
		{
			name:  "no header resolves to the peer",
			cidrs: []string{"10.0.0.0/8"},
			peer:  "10.0.0.5:443",
			want:  "10.0.0.5",
		},
		{
			name:  "malformed header resolves to the peer",
			cidrs: []string{"10.0.0.0/8"},
			peer:  "10.0.0.5:443",
			xff:   "not-an-ip",
			want:  "10.0.0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor, _, err := New(tt.cidrs)
			if err != nil {
				t.Fatalf("New(%v): %v", tt.cidrs, err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.peer
			if tt.xff != "" {
				req.Header.Set(echo.HeaderXForwardedFor, tt.xff)
			}
			if got := extractor(req); got != tt.want {
				t.Fatalf("extractor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewRejectsInvalidCIDR(t *testing.T) {
	if _, _, err := New([]string{"10.0.0.0/8", "not-a-cidr"}); err == nil {
		t.Fatal("New accepted an invalid CIDR; want an error so startup fails loudly")
	}
}

func TestNewDescription(t *testing.T) {
	_, direct, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil): %v", err)
	}
	if !strings.Contains(direct, EnvVar) {
		t.Fatalf("direct-mode description %q does not name %s", direct, EnvVar)
	}

	_, proxied, err := New([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("New(trusted): %v", err)
	}
	if !strings.Contains(proxied, "10.0.0.0/8") || !strings.Contains(proxied, EnvVar) {
		t.Fatalf("trusted-mode description %q does not name the range and %s", proxied, EnvVar)
	}
}

func TestParseTrimsAndDropsEmpty(t *testing.T) {
	ranges, err := Parse([]string{" 10.0.0.0/8 ", "", " 192.168.0.0/16"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(ranges) != 2 {
		t.Fatalf("Parse returned %d ranges, want 2", len(ranges))
	}
	if ranges[0].String() != "10.0.0.0/8" || ranges[1].String() != "192.168.0.0/16" {
		t.Fatalf("Parse returned %v, want the two trimmed ranges", ranges)
	}
}
