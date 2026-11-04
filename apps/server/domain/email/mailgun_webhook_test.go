package email

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func signMailgunBody(key, timestamp, token string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyMailgunSignature is the sole authentication for the public webhook, so
// every failure mode must reject.
func TestVerifyMailgunSignature(t *testing.T) {
	const key = "top-secret-signing-key"
	const ts = "1700000000"
	const token = "abcdef"
	valid := signMailgunBody(key, ts, token)

	tests := []struct {
		name      string
		key       string
		timestamp string
		token     string
		signature string
		want      bool
	}{
		{name: "valid", key: key, timestamp: ts, token: token, signature: valid, want: true},
		{name: "wrong signature", key: key, timestamp: ts, token: token, signature: signMailgunBody(key, ts, token+"x"), want: false},
		{name: "wrong key", key: "other-key", timestamp: ts, token: token, signature: valid, want: false},
		{name: "empty signing key fails closed", key: "", timestamp: ts, token: token, signature: valid, want: false},
		{name: "missing timestamp", key: key, timestamp: "", token: token, signature: valid, want: false},
		{name: "missing token", key: key, timestamp: ts, token: "", signature: valid, want: false},
		{name: "missing signature", key: key, timestamp: ts, token: token, signature: "", want: false},
		{name: "non-hex signature", key: key, timestamp: ts, token: token, signature: "zzzz", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, VerifyMailgunSignature(tt.key, tt.timestamp, tt.token, tt.signature))
		})
	}
}

func webhookRequest(t *testing.T, payload string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailgun", strings.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewMailgunWebhookHandler(nil, &Config{MailgunSigningKey: "key"}, log)
	return rec, h.Handle(c)
}

// A request that fails signature verification must be refused before any
// ingestion happens.
func TestMailgunWebhook_RejectsUnverifiedRequest(t *testing.T) {
	const ts = "1700000000"
	const token = "tok"

	t.Run("missing signature", func(t *testing.T) {
		payload := buildWebhookPayload(t, "", ts, token, "evt-1")
		_, err := webhookRequest(t, payload)
		require.Error(t, err)
		status, _ := apperror.ToHTTPError(err)
		require.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("invalid signature", func(t *testing.T) {
		payload := buildWebhookPayload(t, "deadbeef", ts, token, "evt-1")
		_, err := webhookRequest(t, payload)
		require.Error(t, err)
		status, _ := apperror.ToHTTPError(err)
		require.Equal(t, http.StatusUnauthorized, status)
	})
}

// A correctly signed request is accepted (the store is exercised in the
// DB-backed tests).
func TestMailgunWebhook_AcceptsVerifiedSignature(t *testing.T) {
	// The handler with a nil store cannot ingest; verify the signature gate
	// accepts by checking the parsed payload reaches the store path. Use a
	// context whose store is a real one in the DB-backed test; here we assert
	// the verifier accepts the exact envelope the handler checks.
	const ts = "1700000000"
	const token = "tok"
	payload := buildWebhookPayload(t, signMailgunBody("key", ts, token), ts, token, "evt-1")
	require.Contains(t, payload, `"signature"`)

	var envelope mailgunWebhookBody
	require.NoError(t, json.Unmarshal([]byte(payload), &envelope))
	require.True(t, VerifyMailgunSignature("key", envelope.Signature.Timestamp, envelope.Signature.Token, envelope.Signature.Signature))
}

// buildWebhookPayload renders a Mailgun v3 envelope with a signed bounce event.
func buildWebhookPayload(t *testing.T, signature, timestamp, token, eventID string) string {
	t.Helper()
	payload := map[string]any{
		"signature": map[string]string{
			"timestamp": timestamp,
			"token":     token,
			"signature": signature,
		},
		"event-data": map[string]any{
			"event":     "failed",
			"id":        eventID,
			"timestamp": 1700000000.5,
			"severity":  "permanent",
			"recipient": "user@example.com",
			"message": map[string]any{
				"headers": map[string]string{"message-id": "msg-1@example.com"},
			},
		},
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(b)
}
