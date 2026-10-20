package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestService(key []byte) *Service {
	cfg := &config.Config{}
	if key != nil {
		cfg.LLMProvider.EncryptionKey = hex.EncodeToString(key)
	}
	return NewService(nil, cfg, discardLog(), NewDefaultRegistry())
}

func TestListProviders_IncludesGoogleDrive(t *testing.T) {
	svc := newTestService(nil)
	providers := svc.ListProviders(context.Background())
	p, ok := findProvider(providers, ProviderTypeGoogleDrive)
	if !ok {
		t.Fatalf("ListProviders() = %v, want a %q entry", providers, ProviderTypeGoogleDrive)
	}
	if !p.Available {
		t.Error("google_drive provider must be marked available")
	}
	if p.SourceType != SourceTypeDrive {
		t.Errorf("provider sourceType = %q, want %q", p.SourceType, SourceTypeDrive)
	}
}

// findProvider returns the catalog entry with the given type and whether it was
// present. The registry ships multiple providers (google_drive + github), so
// tests locate entries by type rather than by count/index.
func findProvider(providers []Provider, typ string) (Provider, bool) {
	for _, p := range providers {
		if p.Type == typ {
			return p, true
		}
	}
	return Provider{}, false
}

func TestGetProviderSchema_Shape(t *testing.T) {
	svc := newTestService(nil)
	schema, err := svc.GetProviderSchema(context.Background(), ProviderTypeGoogleDrive)
	if err != nil {
		t.Fatalf("GetProviderSchema() error = %v", err)
	}
	if schema.Type != ProviderTypeGoogleDrive {
		t.Errorf("schema type = %q, want %q", schema.Type, ProviderTypeGoogleDrive)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "folder_id" {
		t.Errorf("schema.Required = %v, want [folder_id]", schema.Required)
	}
	for _, key := range []string{"folder_id", "shared_drive_id", "mime_types", "recursive"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("schema.Properties missing %q", key)
		}
	}
}

func TestGetProviderSchema_UnknownProviderNotFound(t *testing.T) {
	svc := newTestService(nil)
	if _, err := svc.GetProviderSchema(context.Background(), "imap"); err == nil {
		t.Fatal("GetProviderSchema(unknown) = nil error, want not-found")
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name    string
		config  map[string]any
		wantErr bool
	}{
		{"valid minimal", map[string]any{"folder_id": "abc"}, false},
		{"missing folder_id", map[string]any{"mime_types": []any{"application/pdf"}}, true},
		{"empty folder_id", map[string]any{"folder_id": "  "}, true},
		{"valid mime_types", map[string]any{"folder_id": "a", "mime_types": []any{"application/pdf", "text/plain"}}, false},
		{"empty mime_types entry", map[string]any{"folder_id": "a", "mime_types": []any{"application/pdf", ""}}, true},
		{"non-string mime_types entry", map[string]any{"folder_id": "a", "mime_types": []any{42}}, true},
		{"valid string slice mime_types", map[string]any{"folder_id": "a", "mime_types": []string{"application/pdf"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newGoogleDriveProvider().ValidateConfig(tc.config)
			if tc.wantErr && err == nil {
				t.Fatal("validateConfig() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateConfig() error = %v", err)
			}
		})
	}
}

func TestEncryptDecryptCredentials_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	svc := newTestService(key)

	creds := Credentials{"client_id": "cid", "client_secret": "secret", "refresh_token": "rt"}
	blob, err := svc.encryptCredentials(creds)
	if err != nil {
		t.Fatalf("encryptCredentials() error = %v", err)
	}
	got, err := svc.decryptCredentials(blob)
	if err != nil {
		t.Fatalf("decryptCredentials() error = %v", err)
	}
	if !reflect.DeepEqual(got, creds) {
		t.Errorf("round-trip creds = %+v, want %+v", got, creds)
	}
}

func TestEncryptCredentials_NoKey(t *testing.T) {
	svc := newTestService(nil)
	if _, err := svc.encryptCredentials(Credentials{"refresh_token": "x"}); err == nil {
		t.Fatal("encryptCredentials() with no key = nil, want error")
	}
}

func TestStripCredentialKeys_RemovesSecrets(t *testing.T) {
	in := map[string]any{
		"folder_id":     "f",
		"client_id":     "cid",
		"client_secret": "cs",
		"refresh_token": "rt",
		"access_token":  "at",
		"recursive":     true,
	}
	out := stripCredentialKeys(in, credentialKeyNames)
	for _, k := range []string{"client_id", "client_secret", "refresh_token", "access_token"} {
		if _, ok := out[k]; ok {
			t.Errorf("stripCredentialKeys() left secret key %q", k)
		}
	}
	if out["folder_id"] != "f" || out["recursive"] != true {
		t.Errorf("stripCredentialKeys() dropped non-secret keys: %v", out)
	}
}

func TestToIntegration_NoSecrets(t *testing.T) {
	id := uuid.New()
	projectID := uuid.New()
	interval := 30
	src := &Source{
		ID:                   id,
		ProjectID:            projectID,
		Provider:             ProviderTypeGoogleDrive,
		DisplayName:          "My Drive",
		Config:               map[string]any{"folder_id": "f", "refresh_token": "rt", "access_token": "at"},
		EncryptedCredentials: []byte("ciphertext"),
		SyncPolicy:           "manual",
		SyncIntervalMinutes:  &interval,
		Status:               "active",
	}
	it := newTestService(nil).toIntegration(src)
	if it.ID != id.String() {
		t.Errorf("integration ID = %q, want %q", it.ID, id.String())
	}
	if it.ProviderType != ProviderTypeGoogleDrive {
		t.Errorf("integration ProviderType = %q", it.ProviderType)
	}
	if it.SourceType != SourceTypeDrive {
		t.Errorf("integration SourceType = %q, want %q", it.SourceType, SourceTypeDrive)
	}
	if it.SyncMode != "manual" {
		t.Errorf("integration SyncMode = %q, want manual", it.SyncMode)
	}
	if it.Status != "active" {
		t.Errorf("integration Status = %q, want active", it.Status)
	}
	// Config carries the non-secret values (folder_id), credential keys stripped.
	if it.Config["folder_id"] != "f" {
		t.Errorf("integration Config = %+v, want folder_id preserved", it.Config)
	}
	for _, k := range credentialKeyNames {
		if _, ok := it.Config[k]; ok {
			t.Errorf("integration Config leaks credential key %q: %+v", k, it.Config)
		}
	}
	// The integration shape must never carry credential material.
	b, _ := json.Marshal(it)
	if strings.Contains(string(b), "client_secret") || strings.Contains(string(b), "refresh_token") || strings.Contains(string(b), "access_token") || strings.Contains(string(b), "ciphertext") {
		t.Errorf("integration JSON leaks credential material: %s", b)
	}
}

func TestCreate_RejectsWrongProvider(t *testing.T) {
	svc := newTestService(make([]byte, 32))
	_, err := svc.Create(context.Background(), uuid.New(), &CreateIntegrationRequest{
		ProviderType: "imap",
		Config:       map[string]any{"folder_id": "f", "refresh_token": "rt"},
	})
	if err == nil {
		t.Fatal("Create() with wrong provider = nil, want error")
	}
}

func TestCreate_RejectsMissingRefreshToken(t *testing.T) {
	svc := newTestService(make([]byte, 32))
	_, err := svc.Create(context.Background(), uuid.New(), &CreateIntegrationRequest{
		ProviderType: ProviderTypeGoogleDrive,
		Config:       map[string]any{"folder_id": "f"},
	})
	if err == nil {
		t.Fatal("Create() with no refresh_token = nil, want error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("Create() error type = %T, want *apperror.Error", err)
	}
}

// TestCreate_RejectsMissingFolderID asserts Create enforces the folder_id
// contract when AllowIncompleteConfig is unset (the credentials connect path).
func TestCreate_RejectsMissingFolderID(t *testing.T) {
	svc := newTestService(make([]byte, 32))
	_, err := svc.Create(context.Background(), uuid.New(), &CreateIntegrationRequest{
		ProviderType: ProviderTypeGoogleDrive,
		Config:       map[string]any{"refresh_token": "rt"},
	})
	if err == nil {
		t.Fatal("Create() with no folder_id = nil, want error")
	}
	if !strings.Contains(err.Error(), "folder_id") {
		t.Errorf("Create() error = %q, want folder_id validation error", err)
	}
}
