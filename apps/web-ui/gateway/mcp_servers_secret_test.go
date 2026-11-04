package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- secret env/header rows ---
//
// Memory stores secret values write-only: responses carry the keys in
// secretEnvKeys/secretHeadersKeys but omit the values from env/headers.
// Requests list the keys and send the (possibly blank) values inline — a
// listed key with a blank value keeps the stored secret on update.

// mcpFormContext builds an echo context over a urlencoded body so the
// form-parsing helpers can be exercised without a route.
func mcpFormContext(t *testing.T, form url.Values) echo.Context {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	return e.NewContext(req, httptest.NewRecorder())
}

// TestMCPServerFormSecretHeadersToServer asserts the always-submitted secret
// select keeps index alignment for mixed rows, here plain-then-secret: row 0
// must stay Plain and row 1 must be the secret, named in secretHeadersKeys.
// (Regression: with the old checkbox, row 0's unchecked box was omitted and
// the lone "1" shifted onto row 0, marking the wrong header secret.)
func TestMCPServerFormSecretHeadersToServer(t *testing.T) {
	form := url.Values{"name": {"gh"}, "type": {"http"}, "url": {"https://mcp.example.com"}}
	form.Add("headers.key", "X-Plain")
	form.Add("headers.value", "plain")
	form.Add("headers.key", "Authorization")
	form.Add("headers.value", "Bearer tok")
	form.Add("headers.secret", "")  // row 0: Plain
	form.Add("headers.secret", "1") // row 1: Secret

	d := mcpServerFormDataFromRequest(mcpFormContext(t, form))
	if len(d.Headers) != 2 {
		t.Fatalf("headers rows = %+v, want 2", d.Headers)
	}
	if d.Headers[0].Secret || d.Headers[0].Key != "X-Plain" {
		t.Errorf("row 0 = %+v, want X-Plain non-secret", d.Headers[0])
	}
	if !d.Headers[1].Secret || d.Headers[1].Key != "Authorization" {
		t.Errorf("row 1 = %+v, want Authorization marked secret", d.Headers[1])
	}

	s := d.toServer()
	if s.Headers["Authorization"] != "Bearer tok" || s.Headers["X-Plain"] != "plain" {
		t.Errorf("headers = %v, want both values sent inline", s.Headers)
	}
	if got := s.SecretHeadersKeys; len(got) != 1 || got[0] != "Authorization" {
		t.Errorf("secretHeadersKeys = %v, want [Authorization]", got)
	}
	if len(s.SecretEnvKeys) != 0 {
		t.Errorf("secretEnvKeys = %v, want none for an http server", s.SecretEnvKeys)
	}
}

// TestMCPServerFormSecretHeadersToServerSecretFirst is the mirror mixed-order
// case: secret-then-plain. Row 0 is the secret, row 1 is Plain, and toServer
// must still map only Authorization to secretHeadersKeys.
func TestMCPServerFormSecretHeadersToServerSecretFirst(t *testing.T) {
	form := url.Values{"name": {"gh"}, "type": {"http"}, "url": {"https://mcp.example.com"}}
	form.Add("headers.key", "Authorization")
	form.Add("headers.value", "Bearer tok")
	form.Add("headers.key", "X-Plain")
	form.Add("headers.value", "plain")
	form.Add("headers.secret", "1") // row 0: Secret
	form.Add("headers.secret", "")  // row 1: Plain

	d := mcpServerFormDataFromRequest(mcpFormContext(t, form))
	if len(d.Headers) != 2 {
		t.Fatalf("headers rows = %+v, want 2", d.Headers)
	}
	if !d.Headers[0].Secret || d.Headers[0].Key != "Authorization" {
		t.Errorf("row 0 = %+v, want Authorization marked secret", d.Headers[0])
	}
	if d.Headers[1].Secret || d.Headers[1].Key != "X-Plain" {
		t.Errorf("row 1 = %+v, want X-Plain non-secret", d.Headers[1])
	}

	s := d.toServer()
	if got := s.SecretHeadersKeys; len(got) != 1 || got[0] != "Authorization" {
		t.Errorf("secretHeadersKeys = %v, want [Authorization]", got)
	}
	if len(s.SecretEnvKeys) != 0 {
		t.Errorf("secretEnvKeys = %v, want none for an http server", s.SecretEnvKeys)
	}
}

// TestMCPServerFormSecretEnvToServer covers the stdio envelope: secret env rows
// name keys in secretEnvKeys and no secret behavior leaks when nothing is
// checked (backwards-compatible nil slice + plaintext-only map).
func TestMCPServerFormSecretEnvToServer(t *testing.T) {
	form := url.Values{"name": {"files"}, "type": {"stdio"}, "command": {"npx"}, "enabled": {"on"}}
	form.Add("env.key", "API_TOKEN")
	form.Add("env.value", "s3cr3t")
	form.Add("env.secret", "1")
	form.Add("env.key", "LOG_LEVEL")
	form.Add("env.value", "debug")
	form.Add("env.secret", "") // always submitted: row 1 Plain

	d := mcpServerFormDataFromRequest(mcpFormContext(t, form))
	s := d.toServer()
	if s.Env["API_TOKEN"] != "s3cr3t" || s.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("env = %v, want both values sent inline", s.Env)
	}
	if got := s.SecretEnvKeys; len(got) != 1 || got[0] != "API_TOKEN" {
		t.Errorf("secretEnvKeys = %v, want [API_TOKEN]", got)
	}
	if len(s.SecretHeadersKeys) != 0 {
		t.Errorf("secretHeadersKeys = %v, want none for a stdio server", s.SecretHeadersKeys)
	}

	// nothing secret: no keys listed, plain maps preserved
	plain := url.Values{"name": {"files"}, "type": {"stdio"}, "command": {"npx"}}
	plain.Add("env.key", "LOG_LEVEL")
	plain.Add("env.value", "debug")
	plain.Add("env.secret", "")
	pd := mcpServerFormDataFromRequest(mcpFormContext(t, plain))
	ps := pd.toServer()
	if len(ps.SecretEnvKeys) != 0 || len(ps.Env) != 1 || ps.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("plain draft = %+v, want no secret keys and the env value intact", ps)
	}
}

// TestMCPServerFormSecretEnvMixedOrder is the stdio regression for index
// alignment: row 0 plain + row 1 secret must map only the second key to
// secretEnvKeys, and the reverse order must map only the first.
func TestMCPServerFormSecretEnvMixedOrder(t *testing.T) {
	// row 0 plain, row 1 secret
	form := url.Values{"name": {"files"}, "type": {"stdio"}, "command": {"npx"}}
	form.Add("env.key", "LOG_LEVEL")
	form.Add("env.value", "debug")
	form.Add("env.key", "API_TOKEN")
	form.Add("env.value", "s3cr3t")
	form.Add("env.secret", "")
	form.Add("env.secret", "1")

	d := mcpServerFormDataFromRequest(mcpFormContext(t, form))
	if len(d.Env) != 2 {
		t.Fatalf("env rows = %+v, want 2", d.Env)
	}
	if d.Env[0].Secret || d.Env[0].Key != "LOG_LEVEL" {
		t.Errorf("row 0 = %+v, want LOG_LEVEL non-secret", d.Env[0])
	}
	if !d.Env[1].Secret || d.Env[1].Key != "API_TOKEN" {
		t.Errorf("row 1 = %+v, want API_TOKEN marked secret", d.Env[1])
	}
	if got := d.toServer().SecretEnvKeys; len(got) != 1 || got[0] != "API_TOKEN" {
		t.Errorf("secretEnvKeys = %v, want [API_TOKEN]", got)
	}

	// row 0 secret, row 1 plain
	reverse := url.Values{"name": {"files"}, "type": {"stdio"}, "command": {"npx"}}
	reverse.Add("env.key", "API_TOKEN")
	reverse.Add("env.value", "s3cr3t")
	reverse.Add("env.key", "LOG_LEVEL")
	reverse.Add("env.value", "debug")
	reverse.Add("env.secret", "true") // case-insensitive truthy
	reverse.Add("env.secret", "")

	rd := mcpServerFormDataFromRequest(mcpFormContext(t, reverse))
	if len(rd.Env) != 2 {
		t.Fatalf("env rows = %+v, want 2", rd.Env)
	}
	if !rd.Env[0].Secret || rd.Env[0].Key != "API_TOKEN" {
		t.Errorf("reverse row 0 = %+v, want API_TOKEN marked secret", rd.Env[0])
	}
	if rd.Env[1].Secret || rd.Env[1].Key != "LOG_LEVEL" {
		t.Errorf("reverse row 1 = %+v, want LOG_LEVEL non-secret", rd.Env[1])
	}
	if got := rd.toServer().SecretEnvKeys; len(got) != 1 || got[0] != "API_TOKEN" {
		t.Errorf("reverse secretEnvKeys = %v, want [API_TOKEN]", got)
	}
}

// TestMCPServerFormDataFromServerSecretRows asserts write-only keys materialize
// as blank-valued Secret editor rows alongside the non-secret values.
func TestMCPServerFormDataFromServerSecretRows(t *testing.T) {
	s := MCPServer{
		ID: "srv-1", Name: "gh", Type: "http",
		Headers:           map[string]string{"X-Plain": "shown"},
		SecretHeadersKeys: []string{"Authorization"},
		Env:               map[string]string{"LOG_LEVEL": "debug"},
		SecretEnvKeys:     []string{"API_TOKEN"},
	}
	d := mcpServerFormDataFromServer(s)

	plain := findKVRow(d.Headers, "X-Plain")
	if plain == nil || plain.Value != "shown" || plain.Secret {
		t.Errorf("non-secret header row = %+v, want shown non-secret", plain)
	}
	secret := findKVRow(d.Headers, "Authorization")
	if secret == nil || !secret.Secret || secret.Value != "" {
		t.Errorf("secret header row = %+v, want blank Secret row", secret)
	}

	envSecret := findKVRow(d.Env, "API_TOKEN")
	if envSecret == nil || !envSecret.Secret || envSecret.Value != "" {
		t.Errorf("secret env row = %+v, want blank Secret row", envSecret)
	}
	envPlain := findKVRow(d.Env, "LOG_LEVEL")
	if envPlain == nil || envPlain.Secret || envPlain.Value != "debug" {
		t.Errorf("non-secret env row = %+v, want shown non-secret", envPlain)
	}
}

// TestUIMCPServersUpdatePreservesSecretKey asserts the update handler forwards
// secretHeadersKeys and a blank value for a write-only row.
func TestUIMCPServersUpdatePreservesSecretKey(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	body := "name=github-mcp&type=http&url=https%3A%2F%2Fupdated.example.com&" +
		"headers.key=Authorization&headers.value=&headers.secret=1&enabled=on"
	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/update", body)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update = %d, want 303 (body: %s)", rec.Code, rec.Body.String())
	}
	srv := f.find("srv-http")
	if srv == nil {
		t.Fatal("server missing after update")
	}
	if got := srv.SecretHeadersKeys; len(got) != 1 || got[0] != "Authorization" {
		t.Errorf("secretHeadersKeys after update = %v, want [Authorization]", got)
	}
	if v, ok := srv.Headers["Authorization"]; !ok || v != "" {
		t.Errorf("Authorization header = %q (present %v), want blank placeholder that keeps the stored secret", v, ok)
	}
}

// TestUIMCPServersTogglePreservesSecretKey asserts the full-server toggle PATCH
// re-names every write-only key with a blank value (memory treats that as
// "keep the stored secret") and does not drop it.
func TestUIMCPServersTogglePreservesSecretKey(t *testing.T) {
	servers := mcpTestRegistryFixture()
	servers[0].Headers = map[string]string{"X-Plain": "shown"} // memory omits secret values
	servers[0].SecretHeadersKeys = []string{"Authorization"}
	f := newMCPTestBackend(servers)
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/toggle", "enabled=false")
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	srv := f.find("srv-http")
	if srv == nil {
		t.Fatal("server missing after toggle")
	}
	if srv.Enabled {
		t.Error("toggle must persist the disabled flip")
	}
	if got := srv.SecretHeadersKeys; len(got) != 1 || got[0] != "Authorization" {
		t.Errorf("secretHeadersKeys after toggle = %v, want [Authorization]", got)
	}
	if v, ok := srv.Headers["Authorization"]; !ok || v != "" {
		t.Errorf("Authorization header = %q (present %v), want blank placeholder", v, ok)
	}
	if srv.Headers["X-Plain"] != "shown" {
		t.Errorf("X-Plain = %q, want the non-secret value preserved", srv.Headers["X-Plain"])
	}
}

// TestRenderMCPServerEditPageSecretRows asserts a write-only env key renders a
// Secret-selected select with a blank value and the keep-stored hint.
func TestRenderMCPServerEditPageSecretRows(t *testing.T) {
	d := mcpServerFormDataFromServer(MCPServer{
		ID: "srv-1", Name: "files", Type: "stdio", Command: "npx",
		Env:           map[string]string{"LOG_LEVEL": "debug"},
		SecretEnvKeys: []string{"API_TOKEN"},
	})
	html := renderHTML(t, MCPServerEditPage(d))
	for _, want := range []string{
		`name="env.secret"`,
		`name="env.value" value=""`,
		">Plain<",
		">Secret<",
		"Leave blank to keep the stored value.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
	if strings.Contains(html, `type="checkbox" name="env.secret"`) {
		t.Error("env.secret must no longer render as a checkbox")
	}
}

// findKVRow returns the row with the given key, or nil.
func findKVRow(rows []mcpKVField, key string) *mcpKVField {
	if i := slices.IndexFunc(rows, func(r mcpKVField) bool { return r.Key == key }); i >= 0 {
		return &rows[i]
	}
	return nil
}
