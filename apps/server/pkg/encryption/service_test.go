package encryption

import (
	"context"
	"testing"
)

func TestNullService(t *testing.T) {
	t.Run("NewNullService", func(t *testing.T) {
		svc := NewNullService()
		if svc == nil {
			t.Error("NewNullService() returned nil")
		}
	})

	t.Run("IsConfigured returns false", func(t *testing.T) {
		svc := NewNullService()
		if svc.IsConfigured() {
			t.Error("IsConfigured() = true, want false for NullService")
		}
	})
}

func TestNullService_Encrypt(t *testing.T) {
	ctx := context.Background()
	svc := NewNullService()

	tests := []struct {
		name     string
		settings map[string]interface{}
		wantErr  bool
	}{
		{
			name:     "empty settings",
			settings: map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "simple string value",
			settings: map[string]interface{}{"key": "value"},
			wantErr:  false,
		},
		{
			name: "complex nested settings",
			settings: map[string]interface{}{
				"apiKey":  "secret123",
				"timeout": 30,
				"nested": map[string]interface{}{
					"enabled": true,
					"items":   []string{"a", "b"},
				},
			},
			wantErr: false,
		},
		{
			name:     "nil values",
			settings: map[string]interface{}{"key": nil},
			wantErr:  false,
		},
		{
			name:     "unmarshallable value (channel)",
			settings: map[string]interface{}{"ch": make(chan int)},
			wantErr:  true,
		},
		{
			name:     "unmarshallable value (func)",
			settings: map[string]interface{}{"fn": func() {}},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.Encrypt(ctx, tt.settings)
			if (err != nil) != tt.wantErr {
				t.Errorf("Encrypt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == "" && len(tt.settings) > 0 {
				t.Error("Encrypt() returned empty string for non-empty settings")
			}
		})
	}
}

func TestNullService_Decrypt(t *testing.T) {
	ctx := context.Background()
	svc := NewNullService()

	tests := []struct {
		name      string
		data      string
		wantEmpty bool
	}{
		{
			name:      "empty string",
			data:      "",
			wantEmpty: true,
		},
		{
			name:      "valid JSON object",
			data:      `{"key":"value"}`,
			wantEmpty: false,
		},
		{
			name:      "valid empty JSON object",
			data:      `{}`,
			wantEmpty: true,
		},
		{
			name:      "invalid JSON returns empty",
			data:      "not json",
			wantEmpty: true,
		},
		{
			name:      "JSON array returns empty",
			data:      `["a","b"]`,
			wantEmpty: true, // json.Unmarshal into map[string]interface{} fails for arrays
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.Decrypt(ctx, tt.data)
			if err != nil {
				t.Errorf("Decrypt() unexpected error = %v", err)
				return
			}
			if tt.wantEmpty && len(result) != 0 {
				t.Errorf("Decrypt() = %v, want empty map", result)
			}
			if !tt.wantEmpty && len(result) == 0 {
				t.Error("Decrypt() returned empty map, want non-empty")
			}
		})
	}
}

func TestNullService_RoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := NewNullService()

	original := map[string]interface{}{
		"apiKey":  "secret123",
		"timeout": float64(30), // JSON numbers are float64
		"enabled": true,
	}

	encrypted, err := svc.Encrypt(ctx, original)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	decrypted, err := svc.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	// Verify values match
	for k, v := range original {
		if decrypted[k] != v {
			t.Errorf("Round-trip mismatch for key %q: got %v, want %v", k, decrypted[k], v)
		}
	}
}

func TestNullService_ImplementsDecrypter(t *testing.T) {
	// Verify NullService implements Decrypter interface
	var _ Decrypter = (*NullService)(nil)
	var _ Decrypter = NewNullService()
}

func TestService_IsConfigured(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{
			name: "empty key",
			key:  "",
			want: false,
		},
		{
			name: "short key (16 chars)",
			key:  "1234567890123456",
			want: false,
		},
		{
			name: "short key (31 chars)",
			key:  "1234567890123456789012345678901",
			want: false,
		},
		{
			name: "exact minimum (32 chars)",
			key:  "12345678901234567890123456789012",
			want: true,
		},
		{
			name: "long key (64 chars)",
			key:  "1234567890123456789012345678901234567890123456789012345678901234",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{key: tt.key}
			if got := svc.IsConfigured(); got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrKeyNotConfigured(t *testing.T) {
	if ErrKeyNotConfigured == nil {
		t.Error("ErrKeyNotConfigured is nil")
	}
	if ErrKeyNotConfigured.Error() != "encryption key not configured" {
		t.Errorf("ErrKeyNotConfigured.Error() = %q", ErrKeyNotConfigured.Error())
	}
}

func TestErrDecryptionFailed(t *testing.T) {
	if ErrDecryptionFailed == nil {
		t.Error("ErrDecryptionFailed is nil")
	}
	if ErrDecryptionFailed.Error() != "failed to decrypt data" {
		t.Errorf("ErrDecryptionFailed.Error() = %q", ErrDecryptionFailed.Error())
	}
}

func TestModule(t *testing.T) {
	// Module() should return the NewService function
	m := Module()
	if m == nil {
		t.Error("Module() returned nil")
	}
	// Verify it's a function (the NewService constructor)
	// We can't easily type-assert it but we can verify it's not nil
}
