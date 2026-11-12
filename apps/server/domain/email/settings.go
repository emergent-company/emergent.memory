package email

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/encryption"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// InstanceEmailSettings is the singleton row in core.instance_email_settings.
// The SMTP password is kept inside SettingsEncrypted, never in a returned DTO.
type InstanceEmailSettings struct {
	bun.BaseModel `bun:"table:core.instance_email_settings,alias:ies"`

	ID                bool      `bun:"id,pk"`
	Enabled           bool      `bun:"enabled,notnull"`
	Provider          string    `bun:"provider,notnull"`
	FromEmail         string    `bun:"from_email,notnull"`
	FromName          string    `bun:"from_name,notnull"`
	SMTPHost          string    `bun:"smtp_host,notnull"`
	SMTPPort          int       `bun:"smtp_port,notnull"`
	SMTPUsername      string    `bun:"smtp_username,notnull"`
	SMTPTLS           string    `bun:"smtp_tls,notnull"`
	SettingsEncrypted string    `bun:"settings_encrypted,notnull"`
	UpdatedAt         time.Time `bun:"updated_at,notnull"`
	UpdatedBy         *string   `bun:"updated_by"`
}

// SettingsView is the non-secret view returned by the superadmin API.
type SettingsView struct {
	Enabled      bool      `json:"enabled"`
	Provider     string    `json:"provider"`
	FromEmail    string    `json:"fromEmail"`
	FromName     string    `json:"fromName"`
	SMTPHost     string    `json:"smtpHost"`
	SMTPPort     int       `json:"smtpPort"`
	SMTPUsername string    `json:"smtpUsername"`
	SMTPTLS      string    `json:"smtpTls"`
	PasswordSet  bool      `json:"passwordSet"`
	Source       string    `json:"source"` // database | environment
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

// SettingsInput is the write payload. SMTPPassword is optional on update:
// empty means keep the stored secret.
type SettingsInput struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	FromEmail    string `json:"fromEmail"`
	FromName     string `json:"fromName"`
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPUsername string `json:"smtpUsername"`
	SMTPPassword string `json:"smtpPassword"`
	SMTPTLS      string `json:"smtpTls"`
}

// SettingsService owns instance email configuration and its encryption.
type SettingsService struct {
	db  *bun.DB
	enc *encryption.Service
	env Config
	log *slog.Logger
}

func NewSettingsService(db *bun.DB, enc *encryption.Service, cfg *Config, log *slog.Logger) *SettingsService {
	return &SettingsService{db: db, enc: enc, env: *cfg, log: log.With(logger.Scope("email.settings"))}
}

func (s *SettingsService) row(ctx context.Context) (*InstanceEmailSettings, error) {
	var row InstanceEmailSettings
	err := s.db.NewSelect().Model(&row).Where("id = true").Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *SettingsService) secret(ctx context.Context, row *InstanceEmailSettings) (string, error) {
	if row == nil || row.SettingsEncrypted == "" {
		return "", nil
	}
	values, err := s.enc.Decrypt(ctx, row.SettingsEncrypted)
	if err != nil {
		return "", err
	}
	v, _ := values["smtp_password"].(string)
	return v, nil
}

// Current returns the effective config: the DB row when present, else the
// environment. It is read on every send / worker tick, so a save takes effect
// without a restart.
func (s *SettingsService) Current(ctx context.Context) (Config, error) {
	row, err := s.row(ctx)
	if err != nil {
		return Config{}, err
	}
	if row == nil {
		return s.env, nil
	}
	cfg := s.env
	cfg.Enabled = row.Enabled
	cfg.FromEmail = row.FromEmail
	cfg.FromName = row.FromName
	if row.Enabled {
		cfg.Transport = "smtp"
		cfg.SMTPHost = row.SMTPHost
		cfg.SMTPPort = row.SMTPPort
		cfg.SMTPUsername = row.SMTPUsername
		cfg.SMTPTLS = row.SMTPTLS
		password, err := s.secret(ctx, row)
		if err != nil {
			return Config{}, err
		}
		cfg.SMTPPassword = password
	}
	return cfg, nil
}

// Enabled reports whether the effective configuration can send email.
func (s *SettingsService) Enabled(ctx context.Context) bool {
	cfg, err := s.Current(ctx)
	if err != nil {
		s.log.Warn("email settings read failed; treating email as disabled", slog.String("error", err.Error()))
		return false
	}
	return cfg.Enabled
}

// View returns the non-secret current state for the superadmin page.
func (s *SettingsService) View(ctx context.Context) (SettingsView, error) {
	row, err := s.row(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	if row == nil {
		return SettingsView{
			Enabled:      s.env.Enabled,
			Provider:     s.env.Transport,
			FromEmail:    s.env.FromEmail,
			FromName:     s.env.FromName,
			SMTPHost:     s.env.SMTPHost,
			SMTPPort:     s.env.SMTPPort,
			SMTPUsername: s.env.SMTPUsername,
			SMTPTLS:      s.env.SMTPTLS,
			Source:       "environment",
		}, nil
	}
	password, err := s.secret(ctx, row)
	if err != nil {
		return SettingsView{}, err
	}
	return SettingsView{
		Enabled:      row.Enabled,
		Provider:     row.Provider,
		FromEmail:    row.FromEmail,
		FromName:     row.FromName,
		SMTPHost:     row.SMTPHost,
		SMTPPort:     row.SMTPPort,
		SMTPUsername: row.SMTPUsername,
		SMTPTLS:      row.SMTPTLS,
		PasswordSet:  password != "",
		Source:       "database",
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// Validate checks a write payload against the stored state without persisting it.
func (s *SettingsService) Validate(ctx context.Context, in SettingsInput) error {
	existing, err := s.row(ctx)
	if err != nil {
		return err
	}
	stored, err := s.secret(ctx, existing)
	if err != nil {
		return err
	}
	return in.validate(stored != "")
}

func (in SettingsInput) validate(hasStoredPassword bool) error {
	if in.Provider != "resend" && in.Provider != "mailgun" && in.Provider != "gmail" && in.Provider != "custom" {
		return fmt.Errorf("unsupported email provider")
	}
	if _, err := mail.ParseAddress(in.FromEmail); err != nil {
		return fmt.Errorf("invalid from address")
	}
	if in.SMTPPort < 1 || in.SMTPPort > 65535 {
		return fmt.Errorf("SMTP port must be between 1 and 65535")
	}
	switch in.SMTPTLS {
	case "none", "starttls", "tls":
	default:
		return fmt.Errorf("SMTP TLS must be none, starttls or tls")
	}
	if in.Enabled {
		if strings.TrimSpace(in.SMTPHost) == "" {
			return fmt.Errorf("SMTP host is required")
		}
		if in.SMTPPassword == "" && !hasStoredPassword {
			return fmt.Errorf("SMTP password is required")
		}
	}
	return nil
}

// Save persists the setting and returns the effective config.
func (s *SettingsService) Save(ctx context.Context, in SettingsInput, updatedBy string) (Config, error) {
	existing, err := s.row(ctx)
	if err != nil {
		return Config{}, err
	}
	storedPassword, err := s.secret(ctx, existing)
	if err != nil {
		return Config{}, err
	}
	if err := in.validate(storedPassword != ""); err != nil {
		return Config{}, err
	}
	password := in.SMTPPassword
	if password == "" {
		password = storedPassword
	}
	values := map[string]any{}
	if password != "" {
		values["smtp_password"] = password
	}
	encrypted, err := s.enc.Encrypt(ctx, values)
	if err != nil {
		return Config{}, fmt.Errorf("encrypt email settings: %w", err)
	}
	var by any
	if updatedBy != "" {
		by = updatedBy
	}
	_, err = s.db.NewRaw(`
INSERT INTO core.instance_email_settings
  (id, enabled, provider, from_email, from_name, smtp_host, smtp_port,
   smtp_username, smtp_tls, settings_encrypted, updated_at, updated_by)
VALUES (true, ?, ?, ?, ?, ?, ?, ?, ?, ?, now(), ?)
ON CONFLICT (id) DO UPDATE SET
  enabled = EXCLUDED.enabled,
  provider = EXCLUDED.provider,
  from_email = EXCLUDED.from_email,
  from_name = EXCLUDED.from_name,
  smtp_host = EXCLUDED.smtp_host,
  smtp_port = EXCLUDED.smtp_port,
  smtp_username = EXCLUDED.smtp_username,
  smtp_tls = EXCLUDED.smtp_tls,
  settings_encrypted = EXCLUDED.settings_encrypted,
  updated_at = now(),
  updated_by = EXCLUDED.updated_by`,
		in.Enabled, in.Provider, in.FromEmail, in.FromName, in.SMTPHost, in.SMTPPort,
		in.SMTPUsername, in.SMTPTLS, encrypted, by).Exec(ctx)
	if err != nil {
		return Config{}, err
	}
	return s.Current(ctx)
}
