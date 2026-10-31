package config

import (
	"net"
	"net/url"
	"strings"
)

// OtelConfig holds OpenTelemetry configuration.
// Tracing is disabled when ExporterEndpoint is empty.
type OtelConfig struct {
	// ExporterEndpoint is the OTLP HTTP endpoint (e.g. http://localhost:4318).
	// Leave empty to disable tracing entirely (no-op provider, zero overhead).
	ExporterEndpoint string  `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:""`
	ServiceName      string  `env:"OTEL_SERVICE_NAME"            envDefault:"memory-server"`
	SamplingRate     float64 `env:"OTEL_SAMPLING_RATE"           envDefault:"1.0"`
	// TempoURL is the externally-reachable Tempo query URL (e.g. http://localhost:3200).
	// If unset, derived from ExporterEndpoint by substituting port 4318 → 3200.
	TempoURL string `env:"OTEL_TEMPO_URL" envDefault:""`
}

// Enabled returns true when an OTLP endpoint is configured.
func (c OtelConfig) Enabled() bool {
	return c.ExporterEndpoint != ""
}

// InternalTempoQueryURL returns the Tempo query URL reachable from within the
// server process (e.g. inside the Docker network). Derived from the exporter
// endpoint by substituting the ingest port (4318) for the query port (3200).
// This is used by the server-side Tempo proxy — never exposed to clients.
//
// The path is dropped: the endpoint may be the full signal URL the SDK
// documents (http://host:4318/v1/traces), and the handler appends /api/search
// itself — keeping the path produced /v1/traces/api/search and an empty panel
// while spans were ingested fine (#2340). A port other than 4318 is kept as-is:
// it is not ours to guess, and a proxied endpoint may not carry one at all.
func (c OtelConfig) InternalTempoQueryURL() string {
	if c.ExporterEndpoint == "" {
		return ""
	}
	u, err := url.Parse(c.ExporterEndpoint)
	if err != nil || u.Host == "" {
		// A scheme-less value such as `tempo:4318` parses with an empty host;
		// keep the historical substitution for it rather than returning nothing.
		return strings.Replace(c.ExporterEndpoint, ":4318", ":3200", 1)
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	if u.Port() == "4318" {
		u.Host = net.JoinHostPort(u.Hostname(), "3200")
	}
	return u.String()
}
