package sources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// revokedGrantErr mirrors what golang.org/x/oauth2 returns when Google rejects a
// stored refresh token: a *oauth2.RetrieveError carrying invalid_grant.
func revokedGrantErr() error {
	return &oauth2.RetrieveError{
		ErrorCode:        "invalid_grant",
		ErrorDescription: "Token has been expired or revoked.",
	}
}

// TestDrivePickerSession_GrantRevokedMapsToReconnect: a revoked or expired grant
// is a user-actionable state, not a backend failure. It must surface as a
// validation error naming the remediation so the picker modal shows "reconnect"
// instead of a generic "temporarily unavailable" page.
func TestDrivePickerSession_GrantRevokedMapsToReconnect(t *testing.T) {
	p := driveProviderWithTransport(&fakeDriveTransport{tokenErr: revokedGrantErr()})
	picker := p.(ResourcePicker)
	_, err := picker.PickerSession(context.Background(), Credentials{"refresh_token": "rt"}, nil, PickerConfig{
		DeveloperKey: "dev-key",
		AppID:        "12345",
		Strategy:     string(ScopeStrategyDriveReadonly),
	})
	if err == nil {
		t.Fatal("PickerSession(revoked grant) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "reconnect the source") {
		t.Errorf("PickerSession(revoked grant) error = %q, want the reconnect remediation", err)
	}
	if strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("PickerSession(revoked grant) leaked the raw provider error: %q", err)
	}
}

// TestDrivePickerSession_TransientTokenErrorStaysInternal: only an auth
// rejection maps to "reconnect". A 5xx or a timeout from the token endpoint is
// retryable and must keep the internal-error path.
func TestDrivePickerSession_TransientTokenErrorStaysInternal(t *testing.T) {
	transient := &oauth2.RetrieveError{ErrorCode: "backend_error", Response: nil}
	p := driveProviderWithTransport(&fakeDriveTransport{tokenErr: transient})
	picker := p.(ResourcePicker)
	_, err := picker.PickerSession(context.Background(), Credentials{"refresh_token": "rt"}, nil, PickerConfig{
		DeveloperKey: "dev-key",
		AppID:        "12345",
		Strategy:     string(ScopeStrategyDriveReadonly),
	})
	if err == nil {
		t.Fatal("PickerSession(transient) = nil, want an error")
	}
	if strings.Contains(err.Error(), "reconnect the source") {
		t.Errorf("PickerSession(transient) error = %q, want the internal path, not a reconnect prompt", err)
	}
}

// TestDriveTestConnection_GrantRevoked asserts the connector rewrites a revoked
// grant into the reconnect message without the apperror code prefix.
func TestDriveTestConnection_GrantRevoked(t *testing.T) {
	connector := &googleDriveConnector{client: &fakeDriveTransport{testConnErr: revokedGrantErr()}}
	err := connector.TestConnection(context.Background(), nil)
	if err == nil {
		t.Fatal("TestConnection(revoked grant) = nil, want an error")
	}
	if err.Error() != driveReconnectRequired {
		t.Errorf("TestConnection(revoked grant) = %q, want %q", err.Error(), driveReconnectRequired)
	}
}

// TestDriveTestConnection_TransientStaysRaw: a non-auth failure keeps the raw
// provider error so operators can diagnose it.
func TestDriveTestConnection_TransientStaysRaw(t *testing.T) {
	raw := errors.New("dial tcp: connection refused")
	connector := &googleDriveConnector{client: &fakeDriveTransport{testConnErr: raw}}
	if err := connector.TestConnection(context.Background(), nil); !errors.Is(err, raw) {
		t.Errorf("TestConnection(transient) = %v, want the raw error", err)
	}
}

// TestResolveResourceTargets_GrantRevokedMapsToReconnect: saving picked folders
// after the grant died must not read as "folder not accessible".
func TestResolveResourceTargets_GrantRevokedMapsToReconnect(t *testing.T) {
	p := driveProviderWithTransport(&fakeDriveTransport{getErr: revokedGrantErr()})
	resolver := p.(ResourceTargetResolver)
	_, err := resolver.ResolveResourceTargets(context.Background(), Credentials{"refresh_token": "rt"}, []string{"folder-1"})
	if err == nil {
		t.Fatal("ResolveResourceTargets(revoked grant) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "reconnect the source") {
		t.Errorf("ResolveResourceTargets(revoked grant) error = %q, want the reconnect remediation", err)
	}
}

// assertReconnectMessage pins the copy and the no-leak rule. The sync path
// reports the error verbatim in the job history, so the message must be exactly
// the remediation — not "validation: <copy>", and never the raw provider error.
func assertReconnectMessage(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want the reconnect error")
	}
	if !errors.Is(err, errDriveGrantRejected) {
		t.Errorf("error = %v, want errDriveGrantRejected", err)
	}
	if err.Error() != driveReconnectRequired {
		t.Errorf("error = %q, want %q", err.Error(), driveReconnectRequired)
	}
	if strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("error leaked the raw provider error: %q", err)
	}
}

// TestDrivePickerSession_GrantStatusIsUnprocessable pins the status the picker
// surface returns. apperror.NewValidation is 422, not 400: the UI treats 4xx as
// actionable copy and 5xx as an outage.
func TestDrivePickerSession_GrantStatusIsUnprocessable(t *testing.T) {
	p := driveProviderWithTransport(&fakeDriveTransport{tokenErr: revokedGrantErr()})
	picker := p.(ResourcePicker)
	_, err := picker.PickerSession(context.Background(), Credentials{"refresh_token": "rt"}, nil, PickerConfig{
		DeveloperKey: "dev-key",
		AppID:        "12345",
		Strategy:     string(ScopeStrategyDriveReadonly),
	})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want an *apperror.Error", err)
	}
	if appErr.HTTPStatus != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", appErr.HTTPStatus, http.StatusUnprocessableEntity)
	}
	if appErr.Code != "validation_error" {
		t.Errorf("code = %q, want validation_error", appErr.Code)
	}
}

// TestDriveEnumerate_GrantRevokedMapsToReconnect: the sync path builds its client
// from the same stored credential as the interactive surfaces, so a dead grant
// must reach the sync job as the reconnect action, not an internal error.
func TestDriveEnumerate_GrantRevokedMapsToReconnect(t *testing.T) {
	connector := &googleDriveConnector{client: &fakeDriveTransport{listErr: revokedGrantErr()}}
	_, err := connector.Enumerate(context.Background(), map[string]any{"folder_id": "folder-1"})
	assertReconnectMessage(t, err)
}

// TestDriveFetch_GrantRevokedMapsToReconnect covers both fetch transports: a
// native document exported as text, and a binary file downloaded.
func TestDriveFetch_GrantRevokedMapsToReconnect(t *testing.T) {
	t.Run("export", func(t *testing.T) {
		connector := &googleDriveConnector{client: &fakeDriveTransport{exportErr: revokedGrantErr()}}
		_, _, err := connector.Fetch(context.Background(), SourceItem{
			ID:       "doc-1",
			MimeType: "application/vnd.google-apps.document",
		})
		assertReconnectMessage(t, err)
	})
	t.Run("download", func(t *testing.T) {
		connector := &googleDriveConnector{client: &fakeDriveTransport{downloadErr: revokedGrantErr()}}
		_, _, err := connector.Fetch(context.Background(), SourceItem{ID: "file-1", MimeType: "application/pdf"})
		assertReconnectMessage(t, err)
	})
}

// TestDriveEnumerate_NonGrantErrorStaysRaw: a 4xx that is not a dead grant (a
// misconfigured client, say) must not tell the user to reconnect — reconnecting
// would fail identically. It stays retryable/diagnosable.
func TestDriveEnumerate_NonGrantErrorStaysRaw(t *testing.T) {
	raw := &oauth2.RetrieveError{ErrorCode: "invalid_client", ErrorDescription: "Unauthorized"}
	connector := &googleDriveConnector{client: &fakeDriveTransport{listErr: raw}}
	_, err := connector.Enumerate(context.Background(), map[string]any{"folder_id": "folder-1"})
	if !errors.Is(err, raw) {
		t.Fatalf("error = %v, want the raw provider error", err)
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		t.Errorf("non-grant error was mapped to %v", appErr)
	}
}

// TestDriveGrant_MatchesBodyOnlyRejection: Google sometimes omits the parsed
// error code and only puts invalid_grant in the response body; the body fallback
// must still be recognised (pinned because it is easy to drop in a refactor).
func TestDriveGrant_MatchesBodyOnlyRejection(t *testing.T) {
	err := &oauth2.RetrieveError{Body: []byte(`{"error":"invalid_grant"}`)}
	if !isDriveGrantRejected(err) {
		t.Fatal("isDriveGrantRejected(body-only invalid_grant) = false, want true")
	}
	if isDriveGrantRejected(&oauth2.RetrieveError{Body: []byte(`{"error":"invalid_client"}`)}) {
		t.Fatal("isDriveGrantRejected(invalid_client) = true, want false")
	}
}

// TestConnectionFailureMessage_WordingByCredentialContext: the connector cannot
// tell a saved source from a config the user is still typing; the caller can, and
// "reconnect the source" is nonsense on the connect form where no source exists.
func TestConnectionFailureMessage_WordingByCredentialContext(t *testing.T) {
	other := errors.New("dial tcp: connection refused")
	cases := []struct {
		name string
		cc   credentialContext
		err  error
		want string
	}{
		{"stored grant", storedCredentials, errDriveGrantRejected, driveReconnectRequired},
		{"unsaved grant", unsavedCredentials, errDriveGrantRejected, driveUnsavedGrantRequired},
		{"unsaved grant, wrapped", unsavedCredentials, fmt.Errorf("test connection: %w", errDriveGrantRejected), driveUnsavedGrantRequired},
		{"unsaved transient", unsavedCredentials, other, other.Error()},
		{"stored transient", storedCredentials, other, other.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectionFailureMessage(tc.cc, tc.err); got != tc.want {
				t.Errorf("connectionFailureMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestService_TestConfigRewordsUnsavedGrant is the wiring check for
// connectionFailureMessage: the connect form tests credentials that are not saved
// on any source yet, so it must not tell the user to "reconnect the source".
func TestService_TestConfigRewordsUnsavedGrant(t *testing.T) {
	s := &Service{providers: NewRegistry(
		driveProviderWithTransport(&fakeDriveTransport{testConnErr: revokedGrantErr()}),
	)}
	resp, err := s.TestConfig(context.Background(), &TestConfigRequest{
		ProviderType: "google_drive",
		Config:       map[string]any{"folder_id": "f1", "refresh_token": "rt"},
	})
	if err != nil {
		t.Fatalf("TestConfig() error = %v, want a response", err)
	}
	if resp.Success {
		t.Fatal("TestConfig(revoked grant) reported success")
	}
	if resp.Message != driveUnsavedGrantRequired {
		t.Errorf("TestConfig message = %q, want %q", resp.Message, driveUnsavedGrantRequired)
	}
}
