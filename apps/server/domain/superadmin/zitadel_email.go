package superadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/internal/config"
)

var errZitadelEmailUnavailable = errors.New("zitadel admin credentials are not configured")

// ZitadelEmailClient configures the instance SMTP provider through the Zitadel
// Admin API. It is nil when ZITADEL_ADMIN_PAT is absent.
type ZitadelEmailClient struct {
	baseURL string
	pat     string
	http    *http.Client
}

func NewZitadelEmailClient(cfg *config.Config) *ZitadelEmailClient {
	if cfg == nil || cfg.Zitadel.AdminPAT == "" || cfg.Zitadel.Domain == "" {
		return nil
	}
	scheme := "https"
	if cfg.Zitadel.Insecure {
		scheme = "http"
	}
	return &ZitadelEmailClient{
		baseURL: scheme + "://" + strings.TrimRight(cfg.Zitadel.Domain, "/"),
		pat:     cfg.Zitadel.AdminPAT,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *ZitadelEmailClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("zitadel %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

// ConfigureSMTP creates or updates the instance SMTP provider and activates it.
func (c *ZitadelEmailClient) ConfigureSMTP(ctx context.Context, in email.SettingsInput) error {
	if c == nil {
		return errZitadelEmailUnavailable
	}
	tls := in.SMTPTLS != "none"
	body := map[string]any{
		"description":   "Memory deployment",
		"senderAddress": in.FromEmail,
		"senderName":    in.FromName,
		"tls":           tls,
		"host":          fmt.Sprintf("%s:%d", in.SMTPHost, in.SMTPPort),
		"user":          in.SMTPUsername,
		"plain":         map[string]string{"password": in.SMTPPassword},
	}
	var search struct {
		Result []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Type        string `json:"type"`
		} `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, "/admin/v1/email/_search", map[string]any{}, &search); err != nil {
		return err
	}
	id := ""
	for _, p := range search.Result {
		if p.Description == "Memory deployment" || strings.EqualFold(p.Type, "SMTP") {
			id = p.ID
			break
		}
	}
	if id != "" {
		if err := c.do(ctx, http.MethodPut, "/admin/v1/email/smtp/"+id, body, nil); err != nil {
			return err
		}
	} else {
		var created struct {
			ID string `json:"id"`
		}
		if err := c.do(ctx, http.MethodPost, "/admin/v1/email/smtp", body, &created); err != nil {
			return err
		}
		id = created.ID
		if id == "" {
			return fmt.Errorf("zitadel returned no email provider id")
		}
	}
	return c.do(ctx, http.MethodPost, "/admin/v1/email/"+id+"/_activate", map[string]any{}, nil)
}

// DeactivateSMTP deactivates the Memory-provided SMTP configuration if one exists.
func (c *ZitadelEmailClient) DeactivateSMTP(ctx context.Context) error {
	if c == nil {
		return errZitadelEmailUnavailable
	}
	var search struct {
		Result []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Type        string `json:"type"`
		} `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, "/admin/v1/email/_search", map[string]any{}, &search); err != nil {
		return err
	}
	for _, p := range search.Result {
		if p.Description == "Memory deployment" || strings.EqualFold(p.Type, "SMTP") {
			return c.do(ctx, http.MethodPost, "/admin/v1/email/"+p.ID+"/_deactivate", map[string]any{}, nil)
		}
	}
	return nil
}
