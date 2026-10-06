package mcphost

import (
	"strings"
	"testing"
)

func TestValidateHeaders(t *testing.T) {
	valid := []map[string]string{
		{"Authorization": "Bearer abc123"},
		{"X-Custom": "some value with spaces"},
		{},
	}
	for _, h := range valid {
		if err := validateHeaders(h); err != nil {
			t.Errorf("validateHeaders(%v) = %v, want nil", h, err)
		}
	}

	bad := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{name: "empty header name", headers: map[string]string{"": "x"}, want: "illegal header name"},
		{name: "colon in name", headers: map[string]string{"Bad:Name": "x"}, want: "illegal header name"},
		{name: "space in name", headers: map[string]string{"Bad Name": "x"}, want: "illegal header name"},
		{name: "CR in name", headers: map[string]string{"Bad\rName": "x"}, want: "illegal header name"},
		{name: "LF in value", headers: map[string]string{"X": "a\nb"}, want: "illegal header value"},
		{name: "CR in value", headers: map[string]string{"X": "a\rb"}, want: "illegal header value"},
		{name: "CRLF in value", headers: map[string]string{"X": "a\r\nb"}, want: "illegal header value"},
		{name: "NUL in value", headers: map[string]string{"X": "a\x00b"}, want: "illegal header value"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHeaders(tc.headers)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("validateHeaders(%v) = %v, want containing %q", tc.headers, err, tc.want)
			}
		})
	}
}

func TestValidateEnv(t *testing.T) {
	valid := []map[string]string{
		{"GITHUB_TOKEN": "secret"},
		{"MCP_SERVER_URL": "https://example.test"},
		{},
	}
	for _, e := range valid {
		if err := validateEnv(e); err != nil {
			t.Errorf("validateEnv(%v) = %v, want nil", e, err)
		}
	}

	bad := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "empty key", env: map[string]string{"": "x"}, want: "illegal env key"},
		{name: "equals in key", env: map[string]string{"A=B": "x"}, want: "illegal env key"},
		{name: "space in key", env: map[string]string{"A B": "x"}, want: "illegal env key"},
		{name: "CR in key", env: map[string]string{"A\rB": "x"}, want: "illegal env key"},
		{name: "LF in key", env: map[string]string{"A\nB": "x"}, want: "illegal env key"},
		{name: "LF in value", env: map[string]string{"K": "a\nb"}, want: "illegal env value"},
		{name: "CR in value", env: map[string]string{"K": "a\rb"}, want: "illegal env value"},
		{name: "CRLF in value", env: map[string]string{"K": "a\r\nb"}, want: "illegal env value"},
		{name: "NUL in value", env: map[string]string{"K": "a\x00b"}, want: "illegal env value"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEnv(tc.env)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("validateEnv(%v) = %v, want containing %q", tc.env, err, tc.want)
			}
		})
	}
}

func TestServerConfigValidateRejectsIllegalHeaderEnv(t *testing.T) {
	cases := []struct {
		name string
		cfg  ServerConfig
		want string
	}{
		{
			name: "illegal header name",
			cfg:  ServerConfig{Name: "x", Transport: TransportHTTP, URL: "http://x", Headers: map[string]string{"Bad\rName": "v"}},
			want: "illegal header name",
		},
		{
			name: "illegal header value",
			cfg:  ServerConfig{Name: "x", Transport: TransportHTTP, URL: "http://x", Headers: map[string]string{"X": "v\nv"}},
			want: "illegal header value",
		},
		{
			name: "illegal env key",
			cfg:  ServerConfig{Name: "x", Transport: TransportStdio, Command: "c", Env: map[string]string{"K\rL": "v"}},
			want: "illegal env key",
		},
		{
			name: "credential value with newline",
			cfg:  ServerConfig{Name: "x", Transport: TransportStdio, Command: "c", Env: map[string]string{"TOKEN": "a\nb"}},
			want: "illegal env value",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateServersRejectsIllegalHeaderEnv(t *testing.T) {
	servers := []ServerConfig{
		{Name: "good", Transport: TransportStdio, Command: "c"},
		{Name: "bad", Transport: TransportHTTP, URL: "http://x", Headers: map[string]string{"X": "a\r\nb"}},
	}
	err := ValidateServers(servers)
	if err == nil || !strings.Contains(err.Error(), "illegal header value") {
		t.Errorf("ValidateServers() = %v, want illegal header value error", err)
	}
}
