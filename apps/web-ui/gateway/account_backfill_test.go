package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBackfillAccountEmail exercises backfillAccountEmail against a stub
// memory profile endpoint: it fills the email from the account's OWN profile
// (fetched with the account's access token) and never overwrites a present
// email or calls out without a token.
func TestBackfillAccountEmail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer acc-token" {
			t.Errorf("Authorization = %q, want Bearer acc-token", got)
		}
		if r.URL.Path != "/api/user/profile" {
			t.Errorf("path = %q, want /api/user/profile", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"grace@example.com"}`))
	}))
	defer ts.Close()

	ctx := context.Background()

	// Missing email + token → backfilled from the account's profile.
	acc := &accountSession{Sub: "sub-a", Email: "", AccessToken: "acc-token"}
	s := &Server{cfg: Config{MemoryURL: ts.URL}}
	s.backfillAccountEmail(ctx, acc)
	if acc.Email != "grace@example.com" {
		t.Errorf("Email = %q, want backfilled grace@example.com", acc.Email)
	}

	// Email already present → untouched (no profile call).
	acc2 := &accountSession{Sub: "sub-b", Email: "known@example.com", AccessToken: "acc-token"}
	s.backfillAccountEmail(ctx, acc2)
	if acc2.Email != "known@example.com" {
		t.Errorf("Email = %q, want unchanged", acc2.Email)
	}

	// No access token → no call, stays empty.
	acc3 := &accountSession{Sub: "sub-c", Email: "", AccessToken: ""}
	s.backfillAccountEmail(ctx, acc3)
	if acc3.Email != "" {
		t.Errorf("Email = %q, want empty (no token)", acc3.Email)
	}
}
