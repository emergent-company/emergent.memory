package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doRequest(h http.Handler, method, target string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != nil {
		r = body
	}
	req := httptest.NewRequest(method, target, r)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// multipartSend builds the multipart/form-data body the Mailgun SDK emits.
func multipartSend(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func TestHealthz(t *testing.T) {
	h := newHandler()
	rec := doRequest(h, http.MethodGet, "/healthz", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestCaptureMessage(t *testing.T) {
	h := newHandler()
	body, ct := multipartSend(t, map[string]string{
		"from":    `"Memory" <noreply@example.com>`,
		"to":      "invitee@example.com",
		"subject": "You have been invited",
		"text":    "accept at /invites/accept?token=abc",
		"html":    `<a href="/invites/accept?token=abc">accept</a>`,
	})

	req := httptest.NewRequest(http.MethodPost, "/v3/test.example.com/messages", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Basic YXBpOmtleQ==")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] == "" {
		t.Error("response missing id")
	}
	if resp["message"] != "Queued. Thank you." {
		t.Errorf("message = %q, want %q", resp["message"], "Queued. Thank you.")
	}

	// Read back via /captured.
	rec = doRequest(h, http.MethodGet, "/captured", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("captured: expected 200, got %d", rec.Code)
	}
	var captured []CapturedMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &captured); err != nil {
		t.Fatalf("decode captured: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("captured %d messages, want 1", len(captured))
	}
	m := captured[0]
	if m.Path != "/v3/test.example.com/messages" {
		t.Errorf("path = %q", m.Path)
	}
	if m.Domain != "test.example.com" {
		t.Errorf("domain = %q, want %q", m.Domain, "test.example.com")
	}
	if m.From != `"Memory" <noreply@example.com>` {
		t.Errorf("from = %q", m.From)
	}
	if len(m.ToAddresses) != 1 || m.ToAddresses[0] != "invitee@example.com" {
		t.Errorf("to_addresses = %v", m.ToAddresses)
	}
	if m.Subject != "You have been invited" {
		t.Errorf("subject = %q", m.Subject)
	}
	if !m.AuthPresent {
		t.Error("expected auth_present = true")
	}
}

func TestCapturedEmptyIsArray(t *testing.T) {
	h := newHandler()
	rec := doRequest(h, http.MethodGet, "/captured", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var captured []CapturedMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &captured); err != nil {
		t.Fatalf("decode captured: %v", err)
	}
	if captured == nil {
		t.Error("decoded captured is nil, want empty array")
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	h := newHandler()
	rec := doRequest(h, http.MethodGet, "/nope", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestDomainFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/v3/test.example.com/messages", "test.example.com"},
		{"/test.example.com/messages", "test.example.com"},
		{"/v3/mg.example.com/messages", "mg.example.com"},
		{"/v3/messages", ""},
		{"/messages", ""},
	}
	for _, tt := range tests {
		if got := domainFromPath(tt.path); got != tt.want {
			t.Errorf("domainFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
