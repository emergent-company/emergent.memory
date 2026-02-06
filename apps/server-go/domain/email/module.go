package email

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent/emergent-core/internal/config"
)

// Module provides email functionality including job queue and worker
var Module = fx.Module("email",
	fx.Provide(
		NewConfig,
		NewJobsService,
		NewTemplateServiceFromConfig,
		NewSender, // Uses Mailgun when configured, otherwise no-op
		NewWorker,
	),
	fx.Invoke(RegisterWorkerLifecycle),
)

// NewTemplateServiceFromConfig creates a template service with the default template directory
func NewTemplateServiceFromConfig(log *slog.Logger) *TemplateService {
	// Default template directory relative to the server binary
	// In production, templates are typically at ./templates/email
	// In development, they might be at apps/server/templates/email
	templateDir := os.Getenv("EMAIL_TEMPLATE_DIR")
	if templateDir == "" {
		// Try common paths
		candidates := []string{
			"templates/email",
			"../server/templates/email",
			"../../apps/server/templates/email",
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				templateDir = candidate
				break
			}
		}
	}

	if templateDir == "" {
		templateDir = "templates/email"
	}

	absPath, _ := filepath.Abs(templateDir)
	log.Info("initializing email template service", slog.String("template_dir", absPath))

	return NewTemplateService(templateDir, log)
}

// NewSender creates the appropriate email sender based on configuration.
// Uses Mailgun when configured, otherwise falls back to no-op sender.
func NewSender(log *slog.Logger, cfg *Config) Sender {
	if cfg.IsConfigured() && cfg.Enabled {
		mailgunSender := NewMailgunSender(cfg, log)
		if mailgunSender != nil {
			log.Info("using Mailgun sender",
				slog.String("domain", cfg.MailgunDomain),
				slog.String("from", cfg.FromEmail))
			return mailgunSender
		}
	}

	log.Info("using no-op email sender (Mailgun not configured or email disabled)")
	return &noOpSender{log: log}
}

// noOpSender is a no-op email sender for development/testing
type noOpSender struct {
	log *slog.Logger
}

func (s *noOpSender) Send(ctx context.Context, opts SendOptions) (*SendResult, error) {
	s.log.Info("email send (no-op)",
		slog.String("to", opts.To),
		slog.String("subject", opts.Subject))
	
	return &SendResult{
		Success:   true,
		MessageID: "noop-" + opts.To,
	}, nil
}

// RegisterWorkerLifecycle registers the email worker with fx lifecycle
func RegisterWorkerLifecycle(lc fx.Lifecycle, worker *Worker, cfg *Config) {
	if !cfg.Enabled {
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return worker.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx)
		},
	})
}

// Params for NewJobsService constructor
type JobsServiceParams struct {
	fx.In
	DB  *bun.DB
	Log *slog.Logger
	Cfg *config.Config
}
