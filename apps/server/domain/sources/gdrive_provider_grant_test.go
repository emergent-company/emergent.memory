package sources

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/oauth2"
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
