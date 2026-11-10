package githubapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMintInstallationToken(t *testing.T) {
	key := testRSAKey(t)

	var calls int32
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "ghs_test",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	m := NewTokenMinter(srv.URL, srv.Client())
	token, err := m.MintInstallationToken(context.Background(), 42, pkcs1PEM(key), 7,
		[]string{"owner/repo"}, map[string]string{"contents": "read", "pull_requests": "write"})
	if err != nil {
		t.Fatalf("MintInstallationToken: %v", err)
	}
	if token != "ghs_test" {
		t.Fatalf("token = %q", token)
	}
	if gotPath != "/app/installations/7/access_tokens" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("authorization = %q, want Bearer", gotAuth)
	}

	jwt := strings.TrimPrefix(gotAuth, "Bearer ")
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt parts = %d", len(parts))
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Iss int64 `json:"iss"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims.Iss != 42 {
		t.Fatalf("iss = %d, want 42", claims.Iss)
	}

	if repos, ok := gotBody["repositories"].([]any); !ok || len(repos) != 1 || repos[0] != "owner/repo" {
		t.Fatalf("repositories = %#v", gotBody["repositories"])
	}
	perms, ok := gotBody["permissions"].(map[string]any)
	if !ok || perms["contents"] != "read" {
		t.Fatalf("permissions = %#v", gotBody["permissions"])
	}

	// Second call reuses the cached token.
	if _, err := m.MintInstallationToken(context.Background(), 42, pkcs1PEM(key), 7,
		[]string{"owner/repo"}, map[string]string{"contents": "read", "pull_requests": "write"}); err != nil {
		t.Fatalf("cached mint: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("mint calls = %d, want 1 (cache)", n)
	}

	// Invalidation forces a fresh mint.
	m.Invalidate(42, 7)
	if _, err := m.MintInstallationToken(context.Background(), 42, pkcs1PEM(key), 7,
		[]string{"owner/repo"}, map[string]string{"contents": "read", "pull_requests": "write"}); err != nil {
		t.Fatalf("mint after invalidate: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("mint calls = %d, want 2 after invalidate", n)
	}
}

func TestMintInstallationTokenError(t *testing.T) {
	key := testRSAKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"bad credentials"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := NewTokenMinter(srv.URL, srv.Client())
	_, err := m.MintInstallationToken(context.Background(), 42, pkcs1PEM(key), 7, nil, nil)
	if err == nil {
		t.Fatal("expected mint error")
	}
	if strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), "eyJ") {
		t.Fatalf("error leaks the jwt: %v", err)
	}
}
