package superadmin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/email"
)

func TestZitadelEmailConfigureCreatesAndActivates(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer pat" {
			t.Errorf("missing auth header")
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/admin/v1/email/_search":
			_, _ = w.Write([]byte(`{"result":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/admin/v1/email/smtp":
			_, _ = w.Write([]byte(`{"id":"smtp-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/admin/v1/email/smtp-1/_activate":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := &ZitadelEmailClient{baseURL: ts.URL, pat: "pat", http: ts.Client()}
	err := c.ConfigureSMTP(context.Background(), email.SettingsInput{
		Enabled: true, FromEmail: "noreply@example.com", FromName: "Memory",
		SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: "user",
		SMTPPassword: "pass", SMTPTLS: "starttls",
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"POST /admin/v1/email/_search", "POST /admin/v1/email/smtp", "POST /admin/v1/email/smtp-1/_activate"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestZitadelEmailConfigureUpdatesExisting(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/admin/v1/email/_search":
			_, _ = w.Write([]byte(`{"result":[{"id":"smtp-1","description":"Memory deployment","type":"SMTP"}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/admin/v1/email/smtp/smtp-1":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["host"] != "smtp.example.com:587" {
				t.Errorf("host = %v", body["host"])
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/admin/v1/email/smtp-1/_activate":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := &ZitadelEmailClient{baseURL: ts.URL, pat: "pat", http: ts.Client()}
	if err := c.ConfigureSMTP(context.Background(), email.SettingsInput{
		Enabled: true, FromEmail: "noreply@example.com", SMTPHost: "smtp.example.com",
		SMTPPort: 587, SMTPUsername: "user", SMTPPassword: "pass", SMTPTLS: "starttls",
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 3 || methods[1] != "PUT /admin/v1/email/smtp/smtp-1" {
		t.Fatalf("methods = %v", methods)
	}
}
