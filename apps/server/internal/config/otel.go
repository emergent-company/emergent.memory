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

// ResolvedTempoURL returns the Tempo query URL to advertise to clients.
// Uses OTEL_TEMPO_URL if set, otherwise derives it from OTEL_EXPORTER_OTLP_ENDPOINT.
func (c OtelConfig) ResolvedTempoURL() string {
	if c.TempoURL != "" {
		return c.TempoURL
	}
	return strings.Replace(c.ExporterEndpoint, ":4318", ":3200", 1)
}
