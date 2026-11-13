package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

type platformEmailData struct {
	Settings *instanceEmailSettings
	LoadErr  error
	FlashMsg string
	FlashErr error
	TestMsg  string
	TestErr  error
}

func (s *Server) platformEmailSuperadmin(c echo.Context) (*superadminMe, error) {
	me, err := s.memory.GetSuperadminMe(c.Request().Context())
	if err != nil {
		return nil, err
	}
	if me == nil || !me.IsSuperadmin || me.Role != "superadmin_full" {
		return nil, echo.ErrNotFound
	}
	return me, nil
}

// uiPlatformEmailSettings renders the superadmin-only instance email page.
func (s *Server) uiPlatformEmailSettings(c echo.Context) error {
	if _, err := s.platformEmailSuperadmin(c); err != nil {
		return err
	}
	ctx := c.Request().Context()
	settings, err := s.memory.GetInstanceEmailSettings(ctx)
	data := platformEmailData{Settings: settings, LoadErr: err}
	if err == nil {
		if c.QueryParam("updated") != "" {
			data.FlashMsg = "Email settings saved."
		}
		if c.QueryParam("tested") != "" {
			data.TestMsg = "Test email sent."
		}
		if raw := c.QueryParam("err"); raw != "" {
			data.FlashErr = &platformEmailFlashError{message: raw}
		}
		if raw := c.QueryParam("testerr"); raw != "" {
			data.TestErr = &platformEmailFlashError{message: raw}
		}
	}
	return s.page(c, pageTitle("Platform email"), PlatformEmailSettingsPage(data))
}

type platformEmailFlashError struct{ message string }

func (e *platformEmailFlashError) Error() string { return e.message }

// uiPlatformEmailSettingsSave handles the settings form (PRG).
func (s *Server) uiPlatformEmailSettingsSave(c echo.Context) error {
	if _, err := s.platformEmailSuperadmin(c); err != nil {
		return err
	}
	port, _ := strconv.Atoi(c.FormValue("smtpPort"))
	if port == 0 {
		port = 587
	}
	in := instanceEmailSettingsInput{
		Enabled:      c.FormValue("enabled") == "on" || c.FormValue("enabled") == "true",
		Provider:     strings.TrimSpace(c.FormValue("provider")),
		FromEmail:    strings.TrimSpace(c.FormValue("fromEmail")),
		FromName:     strings.TrimSpace(c.FormValue("fromName")),
		SMTPHost:     strings.TrimSpace(c.FormValue("smtpHost")),
		SMTPPort:     port,
		SMTPUsername: strings.TrimSpace(c.FormValue("smtpUsername")),
		SMTPPassword: c.FormValue("smtpPassword"),
		SMTPTLS:      strings.TrimSpace(c.FormValue("smtpTls")),
	}
	if _, err := s.memory.UpdateInstanceEmailSettings(c.Request().Context(), in); err != nil {
		return c.Redirect(http.StatusSeeOther, "/settings/platform/email?err="+url.QueryEscape(userFacingError(err).Error()))
	}
	return c.Redirect(http.StatusSeeOther, "/settings/platform/email?updated=1")
}

// uiPlatformEmailSettingsTest sends a test message with the saved settings.
func (s *Server) uiPlatformEmailSettingsTest(c echo.Context) error {
	if _, err := s.platformEmailSuperadmin(c); err != nil {
		return err
	}
	to := strings.TrimSpace(c.FormValue("to"))
	err := s.memory.TestInstanceEmail(c.Request().Context(), to)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/settings/platform/email?testerr="+url.QueryEscape(userFacingError(err).Error()))
	}
	return c.Redirect(http.StatusSeeOther, "/settings/platform/email?tested=1")
}
