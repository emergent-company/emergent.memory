package adk

import (
	"net/http/httptest"
	"testing"
)

// TestApplyAuth covers each auth style by asserting the header set on a
// request created via httptest.
func TestApplyAuth(t *testing.T) {
	cases := []struct {
		name    string
		style   AuthStyle
		apiKey  string
		header  string
		wantVal string
		wantErr bool
	}{
		{name: "bearer", style: AuthBearer, apiKey: "sk-bearer", header: "Authorization", wantVal: "Bearer sk-bearer"},
		{name: "api-key", style: AuthAPIKeyHeader, apiKey: "sk-azure", header: "api-key", wantVal: "sk-azure"},
		{name: "x-api-key", style: AuthXAPIKey, apiKey: "sk-ant", header: "x-api-key", wantVal: "sk-ant"},
		{name: "x-goog-api-key", style: AuthGoogleAPIKey, apiKey: "sk-goog", header: "x-goog-api-key", wantVal: "sk-goog"},
		{name: "none", style: AuthNone, apiKey: "sk-ignored", header: "", wantVal: ""},
		{name: "signed", style: AuthSigned, apiKey: "sk-sign", header: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://example.com/messages", nil)
			err := ApplyAuth(req, tc.style, tc.apiKey)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyAuth() error = %v", err)
			}
			got := req.Header.Get(tc.header)
			if got != tc.wantVal {
				t.Errorf("%s header = %q, want %q", tc.header, got, tc.wantVal)
			}
		})
	}
}

// TestApplyAuth_EmptyStyleDefaultsToBearer verifies an empty style defaults to
// bearer when an API key is present.
func TestApplyAuth_EmptyStyleDefaultsToBearer(t *testing.T) {
	req := httptest.NewRequest("POST", "http://example.com/messages", nil)
	if err := ApplyAuth(req, "", "sk-default"); err != nil {
		t.Fatalf("ApplyAuth() error = %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-default" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer sk-default")
	}
}

// TestApplyAuth_EmptyStyleNoKey verifies an empty style with an empty key is a
// no-op (no headers set).
func TestApplyAuth_EmptyStyleNoKey(t *testing.T) {
	req := httptest.NewRequest("POST", "http://example.com/messages", nil)
	if err := ApplyAuth(req, "", ""); err != nil {
		t.Fatalf("ApplyAuth() error = %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization header = %q, want empty", got)
	}
}

// TestApplyAuth_BearerEmptyKey verifies bearer style with an empty key sets no
// header.
func TestApplyAuth_BearerEmptyKey(t *testing.T) {
	req := httptest.NewRequest("POST", "http://example.com/messages", nil)
	if err := ApplyAuth(req, AuthBearer, ""); err != nil {
		t.Fatalf("ApplyAuth() error = %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization header = %q, want empty", got)
	}
}
