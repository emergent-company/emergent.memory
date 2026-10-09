package main

import (
	"net/url"
	"testing"
)

// Tests for the credential-guidance surface: the Authorization scheme+token
// assembly, the registry card's required-credential preview, and the
// provider-docs link derivation.

func TestMCPAuthHeaderValue(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
		token  string
		want   string
	}{
		{"bearer prefixes a bare token", "Bearer", "ghp_x", "Bearer ghp_x"},
		{"no double prefix when already bearer", "Bearer", "Bearer ghp_x", "Bearer ghp_x"},
		{"prefix match is case-insensitive", "Bearer", "bearer ghp_x", "bearer ghp_x"},
		{"raw scheme leaves the token untouched", "", "ghp_x", "ghp_x"},
		{"custom scheme prefixes", "Token", "abc", "Token abc"},
		{"empty token stays empty (keep stored)", "Bearer", "", ""},
		{"whitespace is trimmed", "Bearer", "  ghp_x  ", "Bearer ghp_x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpAuthHeaderValue(tc.scheme, tc.token); got != tc.want {
				t.Errorf("mcpAuthHeaderValue(%q, %q) = %q, want %q", tc.scheme, tc.token, got, tc.want)
			}
		})
	}
}

func TestMCPAuthSchemeFromHint(t *testing.T) {
	tests := []struct {
		hint string
		want string
	}{
		{"Bearer {TOKEN}", "Bearer"},
		{"Token {TOKEN}", "Token"},
		{"{TOKEN}", "Bearer"},
		{"", "Bearer"},
	}
	for _, tc := range tests {
		if got := mcpAuthSchemeFromHint(tc.hint); got != tc.want {
			t.Errorf("mcpAuthSchemeFromHint(%q) = %q, want %q", tc.hint, got, tc.want)
		}
	}
}

// TestMCPKVFieldsFromFormAssemblesAuthHeader asserts the auth row's scheme +
// token fields are assembled into the Authorization header value on submit,
// with the row marked secret.
func TestMCPKVFieldsFromFormAssemblesAuthHeader(t *testing.T) {
	form := url.Values{
		"headers.key":    {"Authorization"},
		"headers.value":  {"ghp_secret"},
		"headers.secret": {"1"},
		"headers.auth":   {"1"},
		"headers.scheme": {"Bearer"},
	}
	rows := mcpKVFieldsFromForm(mcpFormContext(t, form), "headers")
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	r := rows[0]
	if r.Value != "Bearer ghp_secret" {
		t.Errorf("value = %q, want assembled \"Bearer ghp_secret\"", r.Value)
	}
	if !r.Auth || !r.Secret || r.AuthScheme != "Bearer" {
		t.Errorf("row = %+v, want Auth+Secret with Bearer scheme", r)
	}
}

// TestMCPKVFieldsFromFormPlainRowKeepsAlignment asserts a plain row still parses
// correctly when auth hidden fields are present (empty) — the regression risk of
// the always-posted .auth/.scheme fields.
func TestMCPKVFieldsFromFormPlainRowKeepsAlignment(t *testing.T) {
	form := url.Values{
		"headers.key":    {"X-Trace", "Authorization"},
		"headers.value":  {"abc", "ghp_secret"},
		"headers.secret": {"", "1"},
		"headers.auth":   {"", "1"},
		"headers.scheme": {"", "Bearer"},
	}
	rows := mcpKVFieldsFromForm(mcpFormContext(t, form), "headers")
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2", rows)
	}
	if rows[0].Auth || rows[0].Value != "abc" {
		t.Errorf("row 0 = %+v, want a plain X-Trace row", rows[0])
	}
	if !rows[1].Auth || rows[1].Value != "Bearer ghp_secret" {
		t.Errorf("row 1 = %+v, want the auth Authorization row", rows[1])
	}
}

func TestMCPCredentialDocs(t *testing.T) {
	label, href := mcpCredentialDocs("https://github.com/github/github-mcp-server")
	if label != "Create a GitHub token" || href != "https://github.com/settings/tokens" {
		t.Errorf("github docs = (%q, %q)", label, href)
	}
	label, href = mcpCredentialDocs("https://gitlab.com/group/project")
	if label != "View provider docs" || href != "https://gitlab.com/group/project" {
		t.Errorf("generic docs = (%q, %q)", label, href)
	}
	if label, href = mcpCredentialDocs(""); label != "" || href != "" {
		t.Errorf("empty repo url should yield no link, got (%q, %q)", label, href)
	}
}

// TestMCPRegistryRequiredPreview asserts the card preview lists the selected
// remote's required-or-secret credentials with their descriptions, in headers
// then env order, and skips optional ones.
func TestMCPRegistryRequiredPreview(t *testing.T) {
	entry := MCPRegistryServer{
		HasRemotes: true,
		Remotes: []MCPRegistryRemote{{
			Type: "streamable-http",
			URL:  "https://api.githubcopilot.com/mcp/",
			Headers: []MCPRegistryHeader{
				{Name: "Authorization", IsSecret: true, Description: "Bearer token (PAT or App token)"},
				{Name: "X-Optional", IsRequired: false, IsSecret: false},
			},
			EnvVars: []MCPRegistryEnvVar{
				{Name: "GITHUB_TOKEN", IsRequired: true, Description: "A PAT"},
			},
		}},
	}
	preview := mcpRegistryRequiredPreview(entry)
	if len(preview) != 2 {
		t.Fatalf("preview = %+v, want 2 (Authorization, GITHUB_TOKEN)", preview)
	}
	if preview[0].Name != "Authorization" || preview[0].Description != "Bearer token (PAT or App token)" {
		t.Errorf("preview[0] = %+v", preview[0])
	}
	if preview[1].Name != "GITHUB_TOKEN" || preview[1].Description != "A PAT" {
		t.Errorf("preview[1] = %+v", preview[1])
	}
}
