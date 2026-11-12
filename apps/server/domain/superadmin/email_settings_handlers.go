package superadmin

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// This file intentionally has no imports from the web-ui; it exposes the
// superadmin-only email settings API and keeps all secrets write-only.

// GetEmailSettings returns the non-secret instance email settings.
func (h *Handler) GetEmailSettings(c echo.Context) error {
	view, err := h.emailSettings.View(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to read email settings", err)
	}
	return c.JSON(http.StatusOK, view)
}

type updateEmailSettingsRequest = email.SettingsInput

// UpdateEmailSettings validates, applies to Zitadel, and persists the settings.
func (h *Handler) UpdateEmailSettings(c echo.Context) error {
	user := auth.MustGetUser(c).ID
	var in updateEmailSettingsRequest
	if err := c.Bind(&in); err != nil {
		return apperror.NewBadRequest("invalid email settings payload")
	}
	if err := h.emailSettings.Validate(c.Request().Context(), in); err != nil {
		return apperror.NewBadRequest(err.Error())
	}
	if in.Enabled {
		if h.zitadelEmail == nil {
			return apperror.NewServiceUnavailable("Zitadel admin credentials are not configured", nil)
		}
		// A blank password means "keep the stored one"; Zitadel needs the
		// actual value, so resolve it before configuring the provider.
		if in.SMTPPassword == "" {
			cfg, err := h.emailSettings.Current(c.Request().Context())
			if err != nil {
				return apperror.NewInternal("failed to read email settings", err)
			}
			in.SMTPPassword = cfg.SMTPPassword
		}
		if err := h.zitadelEmail.ConfigureSMTP(c.Request().Context(), in); err != nil {
			return apperror.New(http.StatusBadGateway, "zitadel_smtp_failed", "failed to configure Zitadel SMTP").WithInternal(err)
		}
	}
	view, err := h.emailSettings.Save(c.Request().Context(), in, user)
	if err != nil {
		return apperror.NewBadRequest(err.Error())
	}
	return c.JSON(http.StatusOK, view)
}

type testEmailRequest struct {
	To string `json:"to"`
}

// TestEmailSettings sends a test message using the effective configuration.
func (h *Handler) TestEmailSettings(c echo.Context) error {
	var in testEmailRequest
	if err := c.Bind(&in); err != nil || in.To == "" {
		return apperror.NewBadRequest("to is required")
	}
	cfg, err := h.emailSettings.Current(c.Request().Context())
	if err != nil {
		return apperror.NewInternal("failed to read email settings", err)
	}
	if !cfg.Enabled {
		return apperror.NewBadRequest("email is disabled")
	}
	result, err := email.NewSender(h.log, &cfg).Send(c.Request().Context(), email.SendOptions{
		To:      in.To,
		Subject: "Memory email test",
		Text:    "This is a test message from your Memory deployment.",
	})
	if err != nil || !result.Success {
		if err == nil {
			err = echo.NewHTTPError(http.StatusBadGateway, result.Error)
		}
		return apperror.New(http.StatusBadGateway, "email_test_failed", "failed to send test email").WithInternal(err)
	}
	return c.JSON(http.StatusOK, map[string]any{"sent": true})
}
