package main

import (
	"strings"
	"testing"
)

func TestPlatformEmailSettingsPage(t *testing.T) {
	html := renderHTML(t, PlatformEmailSettingsPage(platformEmailData{
		Settings: &instanceEmailSettings{
			Enabled: true, Provider: "resend", FromEmail: "noreply@example.com",
			FromName: "Memory", SMTPHost: "smtp.resend.com", SMTPPort: 587,
			SMTPUsername: "resend", SMTPTLS: "starttls", PasswordSet: true,
			Source: "database",
		},
	}))
	for _, want := range []string{
		"Platform email", "name=\"provider\"", "name=\"smtpHost\"",
		"name=\"smtpPassword\"", "name=\"smtpTls\"", "Send test", "password set",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(html, `value="secret"`) {
		t.Error("page must not render a stored secret")
	}
}

func TestPlatformEmailLinkPanel(t *testing.T) {
	html := renderHTML(t, platformEmailLinkPanel())
	if !strings.Contains(html, "/settings/platform/email") || !strings.Contains(html, "Instance email") {
		t.Fatalf("link panel = %s", html)
	}
}
