package config

import "testing"

// The Tempo query URL is derived from the OTLP *ingest* endpoint, and the trace
// handler appends /api/search to it itself. Keeping the endpoint's path produced
// /v1/traces/api/search, so the panel was empty while spans were ingested fine
// (#2340). The port substitution must survive that, and must not invent a port
// the operator did not configure.
func TestInternalTempoQueryURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{name: "disabled", endpoint: "", want: ""},
		{name: "base URL swaps the ingest port", endpoint: "http://tempo:4318", want: "http://tempo:3200"},
		{name: "trailing slash", endpoint: "http://tempo:4318/", want: "http://tempo:3200"},
		{name: "full signal URL drops its path", endpoint: "http://tempo:4318/v1/traces", want: "http://tempo:3200"},
		{name: "proxy prefix drops its path", endpoint: "https://tempo.example.com/otlp", want: "https://tempo.example.com"},
		{name: "a port that is not 4318 is kept", endpoint: "http://tempo.example.com:8080/otlp", want: "http://tempo.example.com:8080"},
		{name: "query string is dropped", endpoint: "http://tempo:4318?tenant=x", want: "http://tempo:3200"},
		{name: "scheme-less host keeps the historical substitution", endpoint: "tempo:4318", want: "tempo:3200"},
		// 4317 is the gRPC ingest port: the exporter is OTLP/HTTP, so the endpoint
		// is a misconfiguration, but leaving the port alone derived a query URL
		// that cannot answer (#2360).
		{name: "the gRPC ingest port also swaps", endpoint: "http://tempo:4317", want: "http://tempo:3200"},
		{name: "scheme-less gRPC port swaps too", endpoint: "tempo:4317", want: "tempo:3200"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := (OtelConfig{ExporterEndpoint: tc.endpoint}).InternalTempoQueryURL()
			if got != tc.want {
				t.Fatalf("InternalTempoQueryURL(%q) = %q, want %q", tc.endpoint, got, tc.want)
			}
		})
	}
}

// OTEL_TEMPO_URL was documented as this override since it was added, and nothing
// read it — so the only way to point the trace tools at a Tempo behind a proxy
// prefix did not exist (#2360). It wins over the derived value, and its trailing
// slash is trimmed because the callers append "/api/search" themselves.
func TestInternalTempoQueryURLPrefersConfiguredURL(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		endpoint   string
		want       string
	}{
		{name: "configured wins over the derived value", configured: "https://tempo.example.com/otlp-query", endpoint: "http://tempo:4318", want: "https://tempo.example.com/otlp-query"},
		{name: "trailing slash is trimmed", configured: "https://tempo.example.com/otlp-query/", endpoint: "http://tempo:4318", want: "https://tempo.example.com/otlp-query"},
		{name: "configured stands alone without an exporter endpoint", configured: "http://localhost:3200", endpoint: "", want: "http://localhost:3200"},
		{name: "unset falls back to the derived value", configured: "", endpoint: "http://tempo:4318", want: "http://tempo:3200"},
		{name: "only a slash is not a configured URL", configured: "/", endpoint: "http://tempo:4318", want: "http://tempo:3200"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := (OtelConfig{ExporterEndpoint: tc.endpoint, TempoURL: tc.configured}).InternalTempoQueryURL()
			if got != tc.want {
				t.Fatalf("InternalTempoQueryURL(configured %q, endpoint %q) = %q, want %q",
					tc.configured, tc.endpoint, got, tc.want)
			}
		})
	}
}
