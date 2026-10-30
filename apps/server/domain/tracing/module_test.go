package tracing

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// The OTLP endpoint we document and set everywhere (compose, deploy heredoc,
// ansible group_vars, preview env) is a base URL such as http://tempo:4318.
// otlptracehttp.WithEndpointURL sends to the URL exactly as given and does NOT
// append the signal path, so a base URL exported to "/" and the collector
// answered 404 ("traces export: failed to send to http://tempo:4318/: 404 Not
// Found") — no span ever reached Tempo. The provider must target /v1/traces.
func TestNewTracerProviderExportsToSignalPath(t *testing.T) {
	tests := []struct {
		name    string
		suffix  string
		wantURL string
	}{
		{name: "base URL appends the signal path", suffix: "", wantURL: "/v1/traces"},
		{name: "trailing slash appends the signal path", suffix: "/", wantURL: "/v1/traces"},
		{name: "explicit path is left untouched", suffix: "/otlp", wantURL: "/otlp"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paths := make(chan string, 8)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case paths <- r.URL.Path:
				default:
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			out, err := NewTracerProvider(&config.Config{Otel: config.OtelConfig{
				ExporterEndpoint: srv.URL + tc.suffix,
				ServiceName:      "memory-server-test",
				SamplingRate:     1,
			}}, log)
			if err != nil {
				t.Fatalf("NewTracerProvider: %v", err)
			}
			tp := out.SDKProvider
			if tp == nil {
				t.Fatal("NewTracerProvider returned no SDK provider for a configured endpoint")
			}

			ctx, span := tp.Tracer("test").Start(context.Background(), "export-me")
			span.End()
			if err := tp.ForceFlush(ctx); err != nil {
				t.Fatalf("ForceFlush: %v", err)
			}
			if err := tp.Shutdown(ctx); err != nil {
				t.Fatalf("Shutdown: %v", err)
			}

			select {
			case got := <-paths:
				if got != tc.wantURL {
					t.Fatalf("exporter POSTed to %q, want %q", got, tc.wantURL)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("exporter sent no request to the test collector")
			}
		})
	}
}
