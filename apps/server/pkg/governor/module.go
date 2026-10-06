package governor

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/syshealth"
)

// Module provides the resource governor as an fx dependency so both the graph
// (HTTP admission middleware) and extraction (background workers) domains can
// inject it. It depends on syshealth.Monitor, which is provided by the
// extraction module.
var Module = fx.Module("governor",
	fx.Provide(ProvideGovernor),
)

// ProvideGovernor builds a Governor from the application config, health
// monitor, and logger.
func ProvideGovernor(cfg *config.Config, monitor syshealth.Monitor, log *slog.Logger) *Governor {
	return New(monitor, configFromApp(cfg.Governor), log)
}

// configFromApp maps the env-driven application config onto the governor's
// runtime config.
func configFromApp(c config.GovernorConfig) *Config {
	return &Config{
		Enabled:                 c.Enabled,
		ThrottleIOWaitPercent:   c.ThrottleIOWaitPercent,
		ShedIOWaitPercent:       c.ShedIOWaitPercent,
		WarningCPULoadFactor:    c.WarningCPULoadFactor,
		ShedCPULoadFactor:       c.ShedCPULoadFactor,
		SustainedSamples:        c.SustainedSamples,
		RecoverSamples:          c.RecoverSamples,
		MaxInflightPerProject:   c.MaxInflightPerProject,
		MaxInflightGlobal:       c.MaxInflightGlobal,
		RatePerMinutePerProject: c.RatePerMinutePerProject,
		RateBurst:               c.RateBurst,
		RetryAfterSeconds:       c.RetryAfterSeconds,
	}
}
