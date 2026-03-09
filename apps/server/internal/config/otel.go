package config

import "strings"

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
func (c OtelConfig) InternalTempoQueryURL() string {
	return strings.Replace(c.ExporterEndpoint, ":4318", ":3200", 1)
}
