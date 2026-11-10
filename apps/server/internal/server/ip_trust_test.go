package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// TestNewEchoClientIPTrust proves the server constructor wires the configured
// client-IP trust model into echo, and that the unset default is fail-closed:
// a spoofed X-Forwarded-For from an untrusted peer cannot become c.RealIP(),
// which is the advisory's rate-limiter bypass (GHSA-246p-cpwv-v3jq).
func TestNewEchoClientIPTrust(t *testing.T) {
	tests := []struct {
		name  string
		cidrs []string
		peer  string
		xff   string
		want  string
	}{
		{
			name: "unset fails closed on the socket peer",
			peer: "203.0.113.9:5555",
			xff:  "1.2.3.4",
			want: "203.0.113.9",
		},
		{
			name: "invalid CIDR falls back to the socket peer",
			// NewConfig rejects this at startup; the constructor must still
			// fail closed rather than inherit echo's permissive default.
			cidrs: []string{"not-a-cidr"},
			peer:  "203.0.113.9:5555",
			xff:   "1.2.3.4",
			want:  "203.0.113.9",
		},
		{
			name:  "configured proxy resolves the real client",
			cidrs: []string{"127.0.0.1/32"},
			peer:  "127.0.0.1:5555",
			xff:   "1.2.3.4",
			want:  "1.2.3.4",
		},
		{
			name:  "configured proxy does not trust an untrusted peer",
			cidrs: []string{"127.0.0.1/32"},
			peer:  "203.0.113.9:5555",
			xff:   "1.2.3.4",
			want:  "203.0.113.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			e := NewEcho(EchoParams{
				Config:     &config.Config{TrustedProxyCIDRs: tt.cidrs},
				Log:        log,
				HTTPLogger: &logger.HTTPLogger{},
			})
			e.GET("/client-ip", func(c echo.Context) error {
				return c.String(http.StatusOK, c.RealIP())
			})

			req := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
			req.RemoteAddr = tt.peer
			if tt.xff != "" {
				req.Header.Set(echo.HeaderXForwardedFor, tt.xff)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
			}
			if got := rec.Body.String(); got != tt.want {
				t.Fatalf("RealIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
