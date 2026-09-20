package sdk

import (
	"net/http"
	"net/url"
	"testing"
)

func TestCredentialSafeRedirectPolicyStripsHeadersCrossHost(t *testing.T) {
	policy := credentialSafeRedirectPolicy()

	orig, err := url.Parse("https://api.example.com/backups/1/download")
	if err != nil {
		t.Fatal(err)
	}
	via := []*http.Request{{URL: orig}}

	next, err := url.Parse("https://storage.example.com/signed-url")
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: next, Header: http.Header{}}
	req.Header.Set("X-API-Key", "secret")
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Org-ID", "org")
	req.Header.Set("X-Project-ID", "proj")

	if err := policy(req, via); err != nil {
		t.Fatalf("policy() error = %v", err)
	}

	for _, h := range []string{"X-API-Key", "Authorization", "X-Org-ID", "X-Project-ID"} {
		if got := req.Header.Get(h); got != "" {
			t.Errorf("expected %s to be stripped on cross-host redirect, got %q", h, got)
		}
	}
}

func TestCredentialSafeRedirectPolicyKeepsHeadersSameHost(t *testing.T) {
	policy := credentialSafeRedirectPolicy()

	orig, err := url.Parse("https://api.example.com/backups/1/download")
	if err != nil {
		t.Fatal(err)
	}
	via := []*http.Request{{URL: orig}}

	next, err := url.Parse("https://api.example.com/signed-url")
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: next, Header: http.Header{}}
	req.Header.Set("X-API-Key", "secret")

	if err := policy(req, via); err != nil {
		t.Fatalf("policy() error = %v", err)
	}
	if got := req.Header.Get("X-API-Key"); got != "secret" {
		t.Errorf("expected X-API-Key preserved on same-host redirect, got %q", got)
	}
}
