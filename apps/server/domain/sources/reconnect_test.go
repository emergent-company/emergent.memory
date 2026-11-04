package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestIsReconnectRequired pins the predicate the worker uses to decide whether a
// failed sync parks its source. It must match every connector's dead-credential
// remediation (including inside the worker's wrapped error) and nothing else: a
// transient failure parked as needs_reconnect would silently stop a healthy
// source until someone noticed.
func TestIsReconnectRequired(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "google drive rejected grant", err: errDriveGrantRejected, want: true},
		{
			name: "gmail rejected grant is a validation error",
			err:  apperror.NewValidation(gmailReconnectRequired),
			want: true,
		},
		{
			name: "trale expired token",
			err:  traleReconnectError("invalid_grant"),
			want: true,
		},
		{
			name: "wrapped by the worker's enumerate path",
			err:  fmt.Errorf("enumerate source: %w", errDriveGrantRejected),
			want: true,
		},
		{
			// A replaced OAuth client is not the user's to fix, but it is still
			// terminal: no retry can recover it, so the source parks and the error
			// row names the administrator who has to act (#2407).
			name: "drive oauth client rejected",
			err:  errDriveClientRejected,
			want: true,
		},
		{
			name: "transient token-endpoint 5xx stays retryable",
			err:  errors.New("oauth2: cannot fetch token: 503 Service Unavailable"),
			want: false,
		},
		{
			name: "token-endpoint timeout stays retryable",
			err:  errors.New(`oauth2: cannot fetch token: Post "https://oauth2.googleapis.com/token": context deadline exceeded`),
			want: false,
		},
		{
			name: "provider listing failure stays retryable",
			err:  errors.New("enumerate source: google drive: list folders: rateLimitExceeded"),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsReconnectRequired(tc.err); got != tc.want {
				t.Fatalf("IsReconnectRequired(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestReconnectRemediation_ProviderCopy asserts the refusal for a manual sync
// keeps each provider's published wording, and that the fallback still carries
// the phrase the classifier and the predicate key on.
func TestReconnectRemediation_ProviderCopy(t *testing.T) {
	if got := reconnectRemediation(ProviderTypeGoogleDrive); got != driveReconnectRequired {
		t.Errorf("google drive remediation = %q, want the provider copy", got)
	}
	if got := reconnectRemediation(ProviderTypeGmail); got != gmailReconnectRequired {
		t.Errorf("gmail remediation = %q, want the provider copy", got)
	}
	fallback := reconnectRemediation(ProviderTypeTrale)
	if !IsReconnectRequired(errors.New(fallback)) {
		t.Errorf("fallback remediation %q does not read as a reconnect prompt", fallback)
	}
}

// TestTraleRefresh_OnlyARejectionRequiresReconnect pins the Trale half of the
// predicate at the token-endpoint seam. Two things matter here. First, only the
// authorization server *answering* with a refusal (4xx) is a reconnect: a 5xx or
// a gateway error must stay retryable, or a blip parks the source until a human
// intervenes. Second, the rejection must be recognised in the envelope Trale's
// authorization server actually uses — Supabase/GoTrue reports
// `error_code`/`msg`, not the RFC 6749 `error` field — because a phone-book match
// on `invalid_grant` never fires against the real endpoint, which would leave a
// dead Trale grant looping in the sweep.
func TestTraleRefresh_OnlyARejectionRequiresReconnect(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		wantReconnect bool
	}{
		{
			name:          "supabase gotrue rejection is terminal",
			status:        http.StatusBadRequest,
			body:          `{"code":400,"error_code":"invalid_credentials","msg":"Invalid login credentials"}`,
			wantReconnect: true,
		},
		{
			name:          "rfc invalid_grant is terminal",
			status:        http.StatusBadRequest,
			body:          `{"error":"invalid_grant","error_description":"refresh token reuse detected"}`,
			wantReconnect: true,
		},
		{
			name:          "token endpoint unavailable stays retryable",
			status:        http.StatusServiceUnavailable,
			body:          `{"error":"temporarily_unavailable"}`,
			wantReconnect: false,
		},
		{
			name:          "gateway error without an oauth code stays retryable",
			status:        http.StatusBadGateway,
			body:          `<html>bad gateway</html>`,
			wantReconnect: false,
		},
		{
			// GoTrue answers a rate limit with a 4xx; the grant is fine, so
			// parking would stop a healthy source for a condition that clears.
			name:          "rate limit stays retryable",
			status:        http.StatusTooManyRequests,
			body:          `{"code":429,"error_code":"over_request_rate_limit","msg":"Request rate limit reached"}`,
			wantReconnect: false,
		},
		{
			// A server-side timeout is not a rejection either.
			name:          "request timeout stays retryable",
			status:        http.StatusRequestTimeout,
			body:          `{"error":"slow_down"}`,
			wantReconnect: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			state := &traleTokenState{
				accessToken:   "expired-access",
				refreshToken:  "refresh",
				expiresAt:     time.Now().Add(-time.Hour),
				clientID:      "client-1",
				tokenEndpoint: srv.URL,
				http:          srv.Client(),
			}
			_, err := state.validAccessToken(context.Background())
			if err == nil {
				t.Fatal("validAccessToken() = nil error, want the refresh failure")
			}
			if got := IsReconnectRequired(err); got != tc.wantReconnect {
				t.Errorf("IsReconnectRequired(%v) = %v, want %v", err, got, tc.wantReconnect)
			}
		})
	}
}

// TestTraleRefresh_GoTrueRejectionKeepsTheCopy asserts the refusal reaches the
// user with the authorization server's own reason and the reconnect instruction:
// processingproblems.Classify keys on that instruction, so a rejection whose
// message lost it would be classified as an unknown retryable failure — the exact
// regression the parking work would otherwise hide behind a green suite.
func TestTraleRefresh_GoTrueRejectionKeepsTheCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"error_code":"invalid_credentials","msg":"Invalid login credentials"}`))
	}))
	t.Cleanup(srv.Close)

	state := &traleTokenState{
		accessToken:   "expired-access",
		refreshToken:  "refresh",
		expiresAt:     time.Now().Add(-time.Hour),
		clientID:      "client-1",
		tokenEndpoint: srv.URL,
		http:          srv.Client(),
	}
	_, err := state.validAccessToken(context.Background())
	if err == nil {
		t.Fatal("validAccessToken() = nil error, want the refresh failure")
	}
	if !strings.Contains(err.Error(), "invalid_credentials") {
		t.Errorf("error = %v, want the authorization server's reason retained", err)
	}
	if !strings.Contains(err.Error(), "reconnect the source") {
		t.Errorf("error = %v, want the reconnect remediation retained", err)
	}
}

// TestTraleRefresh_TransportFailureIsNotAReconnect covers the shape that never
// reaches the endpoint: a transport error is not a rejection, so it must not park.
func TestTraleRefresh_TransportFailureIsNotAReconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	state := &traleTokenState{
		accessToken:   "expired-access",
		refreshToken:  "refresh",
		expiresAt:     time.Now().Add(-time.Hour),
		clientID:      "client-1",
		tokenEndpoint: url,
		http:          http.DefaultClient,
	}
	_, err := state.validAccessToken(context.Background())
	if err == nil {
		t.Fatal("validAccessToken() = nil error, want the transport failure")
	}
	if IsReconnectRequired(err) {
		t.Errorf("IsReconnectRequired(%v) = true, want false for a transport failure", err)
	}
}
