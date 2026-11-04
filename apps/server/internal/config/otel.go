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
	// TempoURL is the Tempo query URL (e.g. http://localhost:3200). When set it is
	// used as given, apart from a trailing slash — that is the deployment this
	// field exists for, a Tempo behind a proxy prefix or one whose query API is
	// not on the ingest host. When unset, it is derived from ExporterEndpoint.
	TempoURL string `env:"OTEL_TEMPO_URL" envDefault:""`
}

// Enabled returns true when an OTLP endpoint is configured.
func (c OtelConfig) Enabled() bool {
	return c.ExporterEndpoint != ""
}

// InternalTempoQueryURL returns the Tempo query URL the server's own tracing
// consumers use (the Tempo proxy, the agent and MCP trace tools). An explicit
// OTEL_TEMPO_URL wins; otherwise it is derived from the exporter endpoint by
// substituting the ingest port (4318, or 4317 for a gRPC endpoint) for the query
// port (3200).
//
// OTEL_TEMPO_URL was documented as this override since it was added but nothing
// read it, so a proxied Tempo had no way to keep its prefix once the derived
// path started being dropped (#2360).
//
// The path is dropped: the endpoint may be the full signal URL the SDK
// documents (http://host:4318/v1/traces), and the handler appends /api/search
// itself — keeping the path produced /v1/traces/api/search and an empty panel
// while spans were ingested fine (#2340). The two OTLP ingest ports (4318 for
// HTTP, 4317 for gRPC) map to 3200; any other port is kept as-is, since it is not
// ours to guess and a proxied endpoint may not carry one at all.
func (c OtelConfig) InternalTempoQueryURL() string {
	// Trimmed so the callers' own "/api/search" join does not produce "//".
	if configured := strings.TrimRight(c.TempoURL, "/"); configured != "" {
		return configured
	}
	if c.ExporterEndpoint == "" {
		return ""
	}
	u, err := url.Parse(c.ExporterEndpoint)
	if err != nil || u.Host == "" {
		// A scheme-less value such as `tempo:4318` parses with an empty host;
		// keep the historical substitution for it rather than returning nothing.
		return strings.NewReplacer(":4318", ":3200", ":4317", ":3200").Replace(c.ExporterEndpoint)
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	// 4317 is the gRPC ingest port: the exporter here is OTLP/HTTP, so an endpoint
	// on it is a misconfiguration, but it still names the Tempo host, and leaving
	// it alone derived `http://host:4317/api/search`, a URL that cannot answer
	// (#2360).
	switch u.Port() {
	case "4318", "4317":
		u.Host = net.JoinHostPort(u.Hostname(), "3200")
	}
	return u.String()
}
