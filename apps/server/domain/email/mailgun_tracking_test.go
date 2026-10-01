package email

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestMailgunSender_SendEnablesTracking proves that a sent message carries the
// per-message tracking options to the wire (o:tracking-opens and
// o:tracking-clicks), so Mailgun reports delivery events back to the webhook.
// It uses the same MAILGUN_API_BASE override mechanism as the rest of the suite
// to point the SDK at a hermetic stub.
func TestMailgunSender_SendEnablesTracking(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"Queued. Thank you.","id":"<stub@example.com>"}`))
	}))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := &Config{
		Enabled:        true,
		MailgunDomain:  "mg.example.com",
		MailgunAPIKey:  "key-abc123",
		MailgunAPIBase: srv.URL + "/v3",
		FromEmail:      "noreply@example.com",
		FromName:       "Test",
	}

	sender := NewMailgunSender(cfg, log)
	if sender == nil {
		t.Fatal("NewMailgunSender returned nil for a configured sender")
	}

	res, err := sender.Send(context.Background(), SendOptions{
		To:      "invitee@example.com",
		ToName:  "Invitee",
		Subject: "You've been invited",
		Text:    "Join the project",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !res.Success {
		t.Fatalf("Send not successful: %+v", res)
	}

	if !strings.Contains(gotBody, "o:tracking-opens") {
		t.Errorf("request body must contain o:tracking-opens, got:\n%s", gotBody)
	}
	if !strings.Contains(gotBody, "o:tracking-clicks") {
		t.Errorf("request body must contain o:tracking-clicks, got:\n%s", gotBody)
	}
}
