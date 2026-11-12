package email

import "testing"

func validSettingsInput() SettingsInput {
	return SettingsInput{
		Enabled:      true,
		Provider:     "resend",
		FromEmail:    "noreply@example.com",
		FromName:     "Memory",
		SMTPHost:     "smtp.resend.com",
		SMTPPort:     587,
		SMTPUsername: "resend",
		SMTPPassword: "secret",
		SMTPTLS:      "starttls",
	}
}

func TestSettingsInputValidate(t *testing.T) {
	in := validSettingsInput()
	if err := in.validate(false); err != nil {
		t.Fatal(err)
	}
	in.SMTPPassword = ""
	if err := in.validate(false); err == nil {
		t.Fatal("missing password must fail without a stored value")
	}
	if err := in.validate(true); err != nil {
		t.Fatalf("stored password should satisfy the requirement: %v", err)
	}
	in = validSettingsInput()
	in.Provider = "unknown"
	if err := in.validate(false); err == nil {
		t.Fatal("unknown provider must fail")
	}
	in = validSettingsInput()
	in.FromEmail = "not-an-address"
	if err := in.validate(false); err == nil {
		t.Fatal("invalid from address must fail")
	}
	in = validSettingsInput()
	in.SMTPPort = 0
	if err := in.validate(false); err == nil {
		t.Fatal("invalid port must fail")
	}
	in = validSettingsInput()
	in.SMTPTLS = "plain"
	if err := in.validate(false); err == nil {
		t.Fatal("invalid TLS mode must fail")
	}
	in = validSettingsInput()
	in.Enabled = false
	in.SMTPHost = ""
	in.SMTPPassword = ""
	if err := in.validate(false); err != nil {
		t.Fatalf("disabled settings should not require SMTP credentials: %v", err)
	}
}
