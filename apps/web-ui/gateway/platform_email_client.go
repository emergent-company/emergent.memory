package main

import (
	"context"
	"net/http"
)

type superadminMe struct {
	IsSuperadmin bool   `json:"isSuperadmin"`
	Role         string `json:"role"`
}

// GetSuperadminMe returns nil when the caller is not a superadmin.
func (m *MemoryClient) GetSuperadminMe(ctx context.Context) (*superadminMe, error) {
	var out *superadminMe
	if err := m.do(ctx, http.MethodGet, "/api/superadmin/me", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type instanceEmailSettings struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	FromEmail    string `json:"fromEmail"`
	FromName     string `json:"fromName"`
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPUsername string `json:"smtpUsername"`
	SMTPTLS      string `json:"smtpTls"`
	PasswordSet  bool   `json:"passwordSet"`
	Source       string `json:"source"`
}

type instanceEmailSettingsInput struct {
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

func (m *MemoryClient) GetInstanceEmailSettings(ctx context.Context) (*instanceEmailSettings, error) {
	var out instanceEmailSettings
	if err := m.do(ctx, http.MethodGet, "/api/superadmin/email-settings", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) UpdateInstanceEmailSettings(ctx context.Context, in instanceEmailSettingsInput) (*instanceEmailSettings, error) {
	var out instanceEmailSettings
	if err := m.do(ctx, http.MethodPut, "/api/superadmin/email-settings", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) TestInstanceEmail(ctx context.Context, to string) error {
	return m.do(ctx, http.MethodPost, "/api/superadmin/email-settings/test", map[string]string{"to": to}, nil)
}
