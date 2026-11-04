package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// TestIssueVerifySessionRoundTrip asserts issue→verify returns equal claims.
func TestIssueVerifySessionRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := sessionClaims{
		AccessToken:     "zitadel-access-1",
		RefreshToken:    "zitadel-refresh-1",
		ActiveProjectID: "proj-42",
		ExpiresAt:       now.Add(time.Hour).Unix(),
	}
	val, err := issueSession("secret-key", claims, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifySession("secret-key", val, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if *got != claims {
		t.Errorf("claims = %+v, want %+v", *got, claims)
	}
}

// TestIssueVerifySessionIdentityRoundTrip asserts the user-identity claims
// (name/email/picture) survive the issue→verify round trip.
func TestIssueVerifySessionIdentityRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := sessionClaims{
		AccessToken:     "at-1",
		RefreshToken:    "rt-1",
		IDToken:         "id-token-1",
		ActiveProjectID: "proj-42",
		OrgID:           "org-7",
		Name:            "Ada Lovelace",
		Email:           "ada@example.com",
		Picture:         "https://example.com/ada.png",
		ExpiresAt:       now.Add(time.Hour).Unix(),
	}
	val, err := issueSession("secret-key", claims, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifySession("secret-key", val, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if *got != claims {
		t.Errorf("claims = %+v, want %+v", *got, claims)
	}

	// Identity-less claims (no tokens either) are still rejected as empty.
	if _, err := verifySessionSignature("secret-key", val); err != nil {
		t.Errorf("verifySessionSignature: %v", err)
	}
}

// TestVerifySessionSignatureIgnoresExpiry asserts the lenient signature-only
// path decodes an expired cookie (used by the token-refresh recovery).
func TestVerifySessionSignatureIgnoresExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := sessionClaims{
		AccessToken:  "at-expired",
		RefreshToken: "rt-still-valid",
		ExpiresAt:    now.Add(-time.Hour).Unix(), // already expired
	}
	val, err := issueSession("secret-key", claims, now)
	if err != nil {
		t.Fatal(err)
	}
	// verifySession rejects the expired cookie…
	if _, err := verifySession("secret-key", val, now); err == nil {
		t.Error("verifySession: expired cookie should error")
	}
	// …but the signature-only path still recovers the claims.
	got, err := verifySessionSignature("secret-key", val)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "rt-still-valid" || got.AccessToken != "at-expired" {
		t.Errorf("claims = %+v", got)
	}
}

// TestVerifySessionTamperRejected asserts flipping a byte in either the
// payload or the signature is rejected.
func TestVerifySessionTamperRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := sessionClaims{
		AccessToken:     "tok",
		ActiveProjectID: "proj",
		ExpiresAt:       now.Add(time.Hour).Unix(),
	}
	val, err := issueSession("secret-key", claims, now)
	if err != nil {
		t.Fatal(err)
	}

	// Flip a byte in the payload (rewrite the first claim's value).
	tamperedPayload := mutateB64(val, 0)
	if _, err := verifySession("secret-key", tamperedPayload, now); err == nil {
		t.Error("tampered payload: want error, got nil")
	}

	// Flip a byte in the signature.
	tamperedSig := mutateB64(val, 1)
	if _, err := verifySession("secret-key", tamperedSig, now); err == nil {
		t.Error("tampered signature: want error, got nil")
	}
}

// TestVerifySessionWrongKeyRejected asserts a cookie signed with a different
// secret is rejected.
func TestVerifySessionWrongKeyRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := sessionClaims{AccessToken: "tok", ExpiresAt: now.Add(time.Hour).Unix()}
	val, err := issueSession("key-a", claims, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifySession("key-b", val, now); err == nil {
		t.Error("wrong key: want error, got nil")
	}
}

// TestVerifySessionExpiredRejected asserts an expired cookie is rejected.
func TestVerifySessionExpiredRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := sessionClaims{AccessToken: "tok", ExpiresAt: now.Add(time.Hour).Unix()}
	val, err := issueSession("secret-key", claims, now)
	if err != nil {
		t.Fatal(err)
	}
	// Verify exactly at expiry is rejected (ExpiresAt <= now).
	if _, err := verifySession("secret-key", val, now.Add(time.Hour)); err == nil {
		t.Error("expired session: want error, got nil")
	}
}

// TestVerifySessionMalformedRejected asserts malformed values are rejected.
func TestVerifySessionMalformedRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []string{
		"",                // empty
		"not-a-cookie",    // no dot
		"onlyone.",        // missing signature
		".missingpayload", // missing payload
		"a.b.c",           // too many parts
		"%%%.%%%",         // not base64
		base64.RawURLEncoding.EncodeToString([]byte("not json here")) + ".c2ln", // base64 but not JSON
	}
	for _, c := range cases {
		if _, err := verifySession("secret-key", c, now); err == nil {
			t.Errorf("value %q: want error, got nil", c)
		}
	}
}

// TestVerifySessionEmptyClaimsRejected asserts a validly-signed cookie with no
// claims is rejected.
func TestVerifySessionEmptyClaimsRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	val, err := issueSession("secret-key", sessionClaims{ExpiresAt: now.Add(time.Hour).Unix()}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifySession("secret-key", val, now); err == nil {
		t.Error("empty claims: want error, got nil")
	}
}

// mutateB64 flips the first byte of the first (part 0) or second (part 1)
// base64url segment and reassembles the cookie value.
func mutateB64(val string, part int) string {
	segs := strings.Split(val, ".")
	raw, err := base64.RawURLEncoding.DecodeString(segs[part])
	if err != nil {
		panic(err)
	}
	raw[0] ^= 0x01
	segs[part] = base64.RawURLEncoding.EncodeToString(raw)
	return strings.Join(segs, ".")
}
