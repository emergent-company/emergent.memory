package email

import (
	"context"
	"log/slog"
)

// settingsSender resolves the effective config on every send, so a superadmin
// save takes effect without restarting the server.
type settingsSender struct {
	settings *SettingsService
	log      *slog.Logger
}

func NewSettingsSender(log *slog.Logger, settings *SettingsService) Sender {
	return &settingsSender{settings: settings, log: log}
}

func (s *settingsSender) Send(ctx context.Context, opts SendOptions) (*SendResult, error) {
	cfg, err := s.settings.Current(ctx)
	if err != nil {
		return &SendResult{Success: false, Error: "email settings unavailable"}, err
	}
	return NewSender(s.log, &cfg).Send(ctx, opts)
}

// Enabled implements the optional check the worker uses before dequeueing.
func (s *settingsSender) Enabled(ctx context.Context) bool {
	return s.settings.Enabled(ctx)
}
