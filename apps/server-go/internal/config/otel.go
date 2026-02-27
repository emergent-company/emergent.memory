package config

// OtelConfig holds OpenTelemetry configuration.
// Tracing is disabled when ExporterEndpoint is empty.
type OtelConfig struct {
	// ExporterEndpoint is the OTLP HTTP endpoint (e.g. http://localhost:4318).
	// Leave empty to disable tracing entirely (no-op provider, zero overhead).
	ExporterEndpoint string  `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:""`
	ServiceName      string  `env:"OTEL_SERVICE_NAME"            envDefault:"emergent-server"`
	SamplingRate     float64 `env:"OTEL_SAMPLING_RATE"           envDefault:"1.0"`
}

// Enabled returns true when an OTLP endpoint is configured.
func (c OtelConfig) Enabled() bool {
	return c.ExporterEndpoint != ""
}
