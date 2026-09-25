package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"log/slog"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// SMTPSender sends emails via a plain SMTP server using only the Go standard
// library. It is the transport used by e2e tests to capture real email in a
// Mailpit box (EMAIL_TRANSPORT=smtp).
type SMTPSender struct {
	cfg *Config
	log *slog.Logger
}

// NewSMTPSender creates a new SMTP email sender.
func NewSMTPSender(cfg *Config, log *slog.Logger) *SMTPSender {
	return &SMTPSender{
		cfg: cfg,
		log: log.With(logger.Scope("email.smtp")),
	}
}

// Send sends an email via SMTP.
func (s *SMTPSender) Send(ctx context.Context, opts SendOptions) (*SendResult, error) {
	if !s.cfg.Enabled {
		s.log.Warn("email sending is disabled (EMAIL_ENABLED=false)")
		return &SendResult{
			Success: false,
			Error:   "Email sending is disabled",
		}, nil
	}

	if err := s.validate(); err != nil {
		s.log.Error("email configuration invalid", slog.String("error", err.Error()))
		return &SendResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	msg, messageID, err := buildMessage(s.cfg, opts)
	if err != nil {
		s.log.Error("failed to build email message", slog.String("error", err.Error()))
		return &SendResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	fromEmail := s.cfg.FromEmail
	if fromEmail == "" {
		fromEmail = "noreply@example.com"
	}

	s.log.Debug("sending email",
		slog.String("to", opts.To),
		slog.String("subject", opts.Subject))

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := s.deliver(sendCtx, fromEmail, opts.To, msg); err != nil {
		s.log.Error("failed to send email",
			slog.String("to", opts.To),
			slog.String("error", err.Error()))
		return &SendResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.log.Info("email sent successfully",
		slog.String("to", opts.To),
		slog.String("message_id", messageID))

	return &SendResult{
		Success:   true,
		MessageID: messageID,
	}, nil
}

// validate checks that the SMTP configuration is valid.
func (s *SMTPSender) validate() error {
	if s.cfg.SMTPHost == "" {
		return fmt.Errorf("SMTP_HOST is required")
	}
	if !validSMTPTLS(s.cfg.SMTPTLS) {
		return fmt.Errorf("SMTP_TLS has invalid value %q (must be one of none, starttls, tls)", s.cfg.SMTPTLS)
	}
	return nil
}

// validSMTPTLS reports whether tlsMode is a recognised SMTP_TLS value. An empty
// value is treated as "none" (the historical default); any other unrecognised
// string is rejected so a typo cannot silently downgrade credentials to plaintext.
func validSMTPTLS(tlsMode string) bool {
	switch tlsMode {
	case "", "none", "starttls", "tls":
		return true
	default:
		return false
	}
}

// deliver connects to the SMTP server and delivers a single message. The dial
// honors ctx so a cancelled send aborts promptly.
func (s *SMTPSender) deliver(ctx context.Context, fromEmail, to string, msg []byte) error {
	host := s.cfg.SMTPHost
	addr := net.JoinHostPort(host, strconv.Itoa(s.cfg.SMTPPort))

	client, err := s.connect(ctx, host, addr)
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	defer client.Close()

	if s.cfg.SMTPUsername != "" {
		auth := smtp.PlainAuth("", s.cfg.SMTPUsername, s.cfg.SMTPPassword, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(fromEmail); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}

	return client.Quit()
}

// connect establishes an SMTP session according to the configured TLS mode.
func (s *SMTPSender) connect(ctx context.Context, host, addr string) (*smtp.Client, error) {
	dialer := &net.Dialer{}

	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	// The dial above honours ctx, but the net/smtp exchange (greeting, Auth,
	// Mail, Rcpt, Data, Quit) does not — a host that accepts the connection and
	// then goes silent would stall the worker goroutine indefinitely. Bound the
	// entire exchange to the caller's deadline so every read/write fails once it
	// is reached.
	if deadline, ok := ctx.Deadline(); ok {
		if err := rawConn.SetDeadline(deadline); err != nil {
			rawConn.Close()
			return nil, err
		}
	}

	switch s.cfg.SMTPTLS {
	case "tls":
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: host})
		return smtp.NewClient(tlsConn, host)
	case "starttls":
		client, err := smtp.NewClient(rawConn, host)
		if err != nil {
			return nil, err
		}
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			client.Close()
			return nil, err
		}
		return client, nil
	case "", "none":
		return smtp.NewClient(rawConn, host)
	default:
		rawConn.Close()
		return nil, fmt.Errorf("unsupported SMTP_TLS mode %q", s.cfg.SMTPTLS)
	}
}

// buildMessage builds a multipart/alternative MIME message and returns the raw
// bytes plus the generated Message-ID. It is exposed for unit testing.
//
// The From/To/Subject header values are validated and encoded before being
// written: addresses are parsed with net/mail (which rejects CR/LF) and the
// subject is Q-encoded. Any caller-influenced value containing CR/LF fails the
// build closed rather than being written raw into the header block.
func buildMessage(cfg *Config, opts SendOptions) ([]byte, string, error) {
	fromEmail := cfg.FromEmail
	if fromEmail == "" {
		fromEmail = "noreply@example.com"
	}

	from, err := formatAddress(cfg.FromName, fromEmail)
	if err != nil {
		return nil, "", fmt.Errorf("invalid from address: %w", err)
	}

	to, err := formatAddress(opts.ToName, opts.To)
	if err != nil {
		return nil, "", fmt.Errorf("invalid to address: %w", err)
	}

	subject, err := encodeSubject(opts.Subject)
	if err != nil {
		return nil, "", err
	}

	host := "localhost"
	if i := strings.LastIndexByte(fromEmail, '@'); i >= 0 && i+1 < len(fromEmail) {
		host = fromEmail[i+1:]
	}
	messageID := generateMessageID(host)

	boundary := fmt.Sprintf("memory-%x", randomHex(16))

	var buf bytes.Buffer

	writeHeader := func(name, value string) {
		buf.WriteString(name)
		buf.WriteString(": ")
		buf.WriteString(value)
		buf.WriteString("\r\n")
	}

	writeHeader("From", from)
	writeHeader("To", to)
	writeHeader("Subject", subject)
	writeHeader("Message-ID", messageID)
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	writeHeader("MIME-Version", "1.0")
	writeHeader("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	buf.WriteString("\r\n")

	writePart := func(contentType, body string) {
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: " + contentType + "; charset=utf-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("\r\n")
		q := quotedprintable.NewWriter(&buf)
		_, _ = q.Write([]byte(body))
		_ = q.Close()
		buf.WriteString("\r\n")
	}

	writePart("text/plain", opts.Text)
	writePart("text/html", opts.HTML)

	buf.WriteString("--" + boundary + "--\r\n")

	return buf.Bytes(), messageID, nil
}

// generateMessageID produces a unique Message-ID like "<hex>@<host>".
func generateMessageID(host string) string {
	return fmt.Sprintf("<%x@%s>", randomHex(16), host)
}

// randomHex returns n random bytes encoded as lowercase hex. On rand failure it
// falls back to a time-derived value so message IDs are always unique.
func randomHex(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fall back to time-based bytes; crypto/rand failures are extremely
		// rare but we must never produce a duplicate or panic.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (8 * (i % 8)))
		}
		return b
	}
	return b
}
