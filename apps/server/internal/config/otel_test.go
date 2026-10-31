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
