package tracing

import (
	"bytes"
	"context"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		// A prefix is joined, not "kept untouched": leaving it produced the same
		// 404 the signal path fixes (#2340).
		{name: "explicit prefix is joined with the signal path", suffix: "/otlp", wantURL: "/otlp/v1/traces"},
		{name: "explicit prefix with a trailing slash is joined", suffix: "/otlp/", wantURL: "/otlp/v1/traces"},
		// The SDK documents the full signal URL as a valid value; appending again
		// would export to /v1/traces/v1/traces.
		{name: "full signal URL is not doubled", suffix: "/v1/traces", wantURL: "/v1/traces"},
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

			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
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

			// A healthy start must not log a resource-detection failure (#2339):
			// the pinned schema URL conflicted with the detectors' own, so every
			// start warned and the env/process attributes were dropped.
			if strings.Contains(logs.String(), "resource detection failed") {
				t.Fatalf("NewTracerProvider logged a resource detection failure:\n%s", logs.String())
			}
		})
	}
}

// The resource carries what the detectors contributed, keeps its schema
// annotation, and raises no error. With the schema URL pinned, resource.New
// returned ErrSchemaURLConflict and a schemaless resource on every healthy start
// (#2339); the attributes themselves survived that conflict, so the error and
// the schema URL are what this pins — along with the attributes, so a future
// change that really did drop them is caught here.
func TestNewResourceIncludesDetectedAttributes(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=ci,service.version=test")

	res, err := newResource(context.Background(), "memory-server-test")
	if err != nil {
		t.Fatalf("newResource: %v", err)
	}
	if res == nil {
		t.Fatal("newResource returned no resource")
	}

	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	// The schema annotation is what the pin cost: a conflicted merge degrades to
	// SchemaURL() == "" even though the attributes survive.
	if got := res.SchemaURL(); got == "" {
		t.Error("resource has no schema URL: a conflicted schema merge degrades to schemaless")
	}
	for key, want := range map[string]string{
		"service.name":           "memory-server-test",
		"deployment.environment": "ci",
		"service.version":        "test",
	} {
		if got[key] != want {
			t.Errorf("resource attribute %q = %q, want %q (attributes: %v)", key, got[key], want, got)
		}
	}
}

// An https:// collector must be reached over TLS. `WithInsecure()` was set
// unconditionally, which downgraded it to cleartext — inert for our http://tempo
// deployments, wrong for anyone pointing at a TLS endpoint (#2340).
func TestNewTracerProviderHonoursHTTPS(t *testing.T) {
	paths := make(chan string, 8)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case paths <- r.URL.Path:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// WithEndpointURL takes the scheme from the URL; the certificate is the
	// exporter's own env knob, so the test does not need a production hook.
	certFile := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(certFile, pemBytes, 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", certFile)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	out, err := NewTracerProvider(&config.Config{Otel: config.OtelConfig{
		ExporterEndpoint: srv.URL,
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
		t.Fatalf("ForceFlush over TLS: %v", err)
	}
	if err := tp.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case got := <-paths:
		if got != "/v1/traces" {
			t.Fatalf("exporter POSTed to %q over TLS, want %q", got, "/v1/traces")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exporter sent no request to the TLS collector (cleartext to an https:// endpoint)")
	}
}

// A host-only endpoint cannot be used by the exporter; say so at startup instead
// of leaving the next person to find it in an export-error log line (#2340).
func TestNewTracerProviderWarnsOnSchemelessEndpoint(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	_, _ = NewTracerProvider(&config.Config{Otel: config.OtelConfig{
		ExporterEndpoint: "tempo:4318",
		ServiceName:      "memory-server-test",
		SamplingRate:     1,
	}}, log)
	if !strings.Contains(logs.String(), "has no http(s) scheme") {
		t.Fatalf("no warning about a scheme-less endpoint; logs were:\n%s", logs.String())
	}
}

// URL schemes are case-insensitive, so `HTTPS://collector` is a valid endpoint
// that used to warn because the check compared a raw prefix (#2360).
func TestNewTracerProviderAcceptsUppercaseScheme(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	out, err := NewTracerProvider(&config.Config{Otel: config.OtelConfig{
		ExporterEndpoint: "HTTPS://127.0.0.1:1",
		ServiceName:      "memory-server-test",
		SamplingRate:     1,
	}}, log)
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	if strings.Contains(logs.String(), "has no http(s) scheme") {
		t.Fatalf("warned about a valid uppercase scheme; logs were:\n%s", logs.String())
	}
	if out.SDKProvider == nil {
		t.Fatal("NewTracerProvider returned no SDK provider for an uppercase scheme")
	}
	if err := out.SDKProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestIsHTTPScheme(t *testing.T) {
	for _, tc := range []struct {
		scheme string
		want   bool
	}{
		{"http", true}, {"https", true}, {"HTTPS", true}, {"Http", true},
		{"tempo", false}, {"", false}, {"h2c", false},
	} {
		if got := isHTTPScheme(tc.scheme); got != tc.want {
			t.Errorf("isHTTPScheme(%q) = %v, want %v", tc.scheme, got, tc.want)
		}
	}
}
