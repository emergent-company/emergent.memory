package email

import (
	"bytes"
	"mime"
	"net/mail"
	"strings"
	"testing"
)

// TestBuildMessageRejectsCRLFHeaderInjection proves that a CR/LF sequence in a
// caller-influenced header value (subject, To address, or display name) cannot
// inject additional headers into the built message. With the fix in place the
// builder fails closed (returns an error) on such input; without it, the raw
// CR/LF is written verbatim and a "Bcc:" header is injected.
func TestBuildMessageRejectsCRLFHeaderInjection(t *testing.T) {
	cfg := &Config{FromName: "Memory", FromEmail: "noreply@example.com"}
	payload := "attacker@example.com"

	tests := []struct {
		name string
		opts SendOptions
	}{
		{
			name: "subject injection",
			opts: SendOptions{To: "victim@example.com", Subject: "Hello\r\nBcc: " + payload},
		},
		{
			name: "to address injection",
			opts: SendOptions{To: "victim@example.com\r\nBcc: " + payload, Subject: "Hello"},
		},
		{
			name: "display name injection",
			opts: SendOptions{To: "victim@example.com", ToName: "Friend\r\nBcc: " + payload, Subject: "Hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, _, err := buildMessage(cfg, tt.opts)
			if err != nil {
				// Fail-closed: a message must not be built from a header value
				// containing CR/LF.
				return
			}

			raw := string(msg)
			m, perr := mail.ReadMessage(bytes.NewReader(msg))
			if perr != nil {
				t.Fatalf("mail.ReadMessage: %v", perr)
			}
			if got := m.Header.Get("Bcc"); got != "" {
				t.Errorf("injected Bcc header present: %q", got)
			}
			if strings.Contains(raw, payload) {
				t.Errorf("injected address %q present in message:\n%s", payload, raw)
			}
		})
	}
}

// TestBuildMessageEncodesNonASCIISubject proves a legitimate non-ASCII subject
// is emitted as an RFC 2047 encoded-word (so no raw bytes leak into the header
// block) and that a standard decoder round-trips it back to the original.
func TestBuildMessageEncodesNonASCIISubject(t *testing.T) {
	cfg := &Config{FromName: "Memory", FromEmail: "noreply@example.com"}
	const subject = "Café ☕ 東京 — welcome"

	msg, _, err := buildMessage(cfg, SendOptions{
		To:      "invitee@example.com",
		Subject: subject,
		Text:    "hi",
		HTML:    "<p>hi</p>",
	})
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	m, err := mail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		t.Fatalf("mail.ReadMessage: %v", err)
	}

	rawSubject := m.Header.Get("Subject")
	if !strings.HasPrefix(rawSubject, "=?utf-8?") {
		t.Errorf("Subject not encoded as encoded-word, got %q", rawSubject)
	}
	if strings.ContainsAny(rawSubject, "\r\n") {
		t.Errorf("Subject contains raw CR/LF: %q", rawSubject)
	}

	dec := new(mime.WordDecoder)
	decoded, err := dec.DecodeHeader(rawSubject)
	if err != nil {
		t.Fatalf("decode subject: %v", err)
	}
	if decoded != subject {
		t.Errorf("subject round-trip = %q, want %q", decoded, subject)
	}
}
