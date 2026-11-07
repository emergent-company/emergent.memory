package chat

import (
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestShouldReuseToolSession pins the gate that keeps the stateless-tool dedupe
// out of the paths it must not touch: in-run calls (#2266 parent linkage),
// explicit session continuations, and generic /query clients (no tool kind).
func TestShouldReuseToolSession(t *testing.T) {
	cases := []struct {
		name           string
		parentRunID    string
		conversationID string
		spawnSource    string
		want           bool
	}{
		{
			name:        "stateless search-knowledge call reuses its tool session",
			spawnSource: "search-knowledge",
			want:        true,
		},
		{
			name:        "in-run call keeps the per-call parent-linked row",
			parentRunID: "11111111-1111-1111-1111-111111111111",
			spawnSource: "search-knowledge",
			want:        false,
		},
		{
			name:           "explicit session continuation wins",
			conversationID: "22222222-2222-2222-2222-222222222222",
			spawnSource:    "search-knowledge",
			want:           false,
		},
		{
			name: "generic query client (web UI) keeps per-call behaviour",
			want: false,
		},
		{
			name:        "blank tool kind is not a tool kind",
			spawnSource: "   ",
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldReuseToolSession(tc.parentRunID, tc.conversationID, tc.spawnSource); got != tc.want {
				t.Fatalf("shouldReuseToolSession(%q, %q, %q) = %v, want %v",
					tc.parentRunID, tc.conversationID, tc.spawnSource, got, tc.want)
			}
		})
	}
}

// TestToolSessionIdentity pins the strongest-identity rule: the resolved user
// id wins, and only an ownerless credential falls back to its API token id.
func TestToolSessionIdentity(t *testing.T) {
	const userID = "00000000-0000-0000-0000-000000000001"
	const tokenID = "00000000-0000-0000-0000-0000000000aa"

	if got := toolSessionIdentity(nil); got != "" {
		t.Fatalf("toolSessionIdentity(nil) = %q, want empty", got)
	}
	if got := toolSessionIdentity(&auth.AuthUser{ID: userID, APITokenID: tokenID}); got != userID {
		t.Fatalf("user id must win over API token id: got %q, want %q", got, userID)
	}
	if got := toolSessionIdentity(&auth.AuthUser{APITokenID: tokenID}); got != tokenID {
		t.Fatalf("ownerless credential must fall back to token id: got %q, want %q", got, tokenID)
	}
}
