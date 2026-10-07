package mcpregistry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestCollectRegistryRequiredConfig(t *testing.T) {
	t.Run("github-shaped secret header counts", func(t *testing.T) {
		got := collectRegistryRequiredConfig(RegistryRemote{
			Type: "streamable-http",
			URL:  "https://api.githubcopilot.com/mcp/",
			Headers: []RegistryHeader{
				{Name: "Authorization", IsSecret: true}, // no isRequired
			},
		})
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
		}
		k := got[0]
		if k.Name != "Authorization" || k.Kind != "header" || !k.IsSecret || !k.IsRequired {
			t.Fatalf("key = %+v, want Authorization/header/secret/required", k)
		}
	})

	t.Run("env var case", func(t *testing.T) {
		got := collectRegistryRequiredConfig(RegistryRemote{
			Type: "streamable-http",
			URL:  "https://8.8.8.8/mcp",
			EnvironmentVariables: []RegistryEnvVar{
				{Name: "TOKEN", IsRequired: true},
			},
		})
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
		}
		k := got[0]
		if k.Name != "TOKEN" || k.Kind != "env" || !k.IsRequired || k.IsSecret {
			t.Fatalf("key = %+v, want TOKEN/env/required/not-secret", k)
		}
	})

	t.Run("empty remote yields empty", func(t *testing.T) {
		if got := collectRegistryRequiredConfig(RegistryRemote{}); len(got) != 0 {
			t.Fatalf("got %+v, want empty", got)
		}
	})

	t.Run("headers then env, each sorted by name", func(t *testing.T) {
		got := collectRegistryRequiredConfig(RegistryRemote{
			Headers: []RegistryHeader{
				{Name: "Z-Header", IsSecret: true},
				{Name: "A-Header", IsRequired: true},
			},
			EnvironmentVariables: []RegistryEnvVar{
				{Name: "Z_ENV", IsSecret: true},
				{Name: "A_ENV", IsRequired: true},
			},
		})
		wantNames := []string{"A-Header", "Z-Header", "A_ENV", "Z_ENV"}
		if len(got) != len(wantNames) {
			t.Fatalf("len = %d, want %d (%+v)", len(got), len(wantNames), got)
		}
		for i, n := range wantNames {
			if got[i].Name != n {
				t.Fatalf("got[%d].Name = %q, want %q (full: %+v)", i, got[i].Name, n, got)
			}
		}
	})
}

func TestMissingRequiredConfigKeys(t *testing.T) {
	secret := EncryptedSecret{Ciphertext: []byte("ct"), Nonce: []byte("nonce")}

	t.Run("blank header missing", func(t *testing.T) {
		s := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "Authorization", Kind: "header"}},
			Headers:        map[string]any{"Authorization": ""},
		}
		assertMissingKeys(t, missingRequiredConfigKeys(s), []string{"Authorization"})
	})

	t.Run("plaintext header filled not missing", func(t *testing.T) {
		s := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "Authorization", Kind: "header"}},
			Headers:        map[string]any{"Authorization": "Bearer x"},
		}
		assertMissingKeys(t, missingRequiredConfigKeys(s), nil)
	})

	t.Run("stored secret header not missing", func(t *testing.T) {
		s := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "Authorization", Kind: "header"}},
			SecretHeaders:  map[string]EncryptedSecret{"Authorization": secret},
		}
		assertMissingKeys(t, missingRequiredConfigKeys(s), nil)
	})

	t.Run("blank env missing", func(t *testing.T) {
		s := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "TOKEN", Kind: "env"}},
			Env:            map[string]any{"TOKEN": ""},
		}
		assertMissingKeys(t, missingRequiredConfigKeys(s), []string{"TOKEN"})
	})

	t.Run("stored secret env not missing", func(t *testing.T) {
		s := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "TOKEN", Kind: "env"}},
			SecretEnv:      map[string]EncryptedSecret{"TOKEN": secret},
		}
		assertMissingKeys(t, missingRequiredConfigKeys(s), nil)
	})

	t.Run("nil value renders blank", func(t *testing.T) {
		s := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "TOKEN", Kind: "env"}},
			Env:            map[string]any{"TOKEN": nil},
		}
		assertMissingKeys(t, missingRequiredConfigKeys(s), []string{"TOKEN"})
	})

	t.Run("non-string value renders non-blank", func(t *testing.T) {
		s := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "TOKEN", Kind: "env"}},
			Env:            map[string]any{"TOKEN": 0},
		}
		assertMissingKeys(t, missingRequiredConfigKeys(s), nil)
	})

	t.Run("nil maps and nil server are safe", func(t *testing.T) {
		assertMissingKeys(t, missingRequiredConfigKeys(nil), nil)
		assertMissingKeys(t, missingRequiredConfigKeys(&MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "Authorization", Kind: "header"}},
		}), []string{"Authorization"})
	})

	t.Run("deduped and sorted", func(t *testing.T) {
		s := &MCPServer{
			RequiredConfig: []RequiredConfigKey{
				{Name: "Z", Kind: "header"},
				{Name: "A", Kind: "header"},
				{Name: "Z", Kind: "header"},
			},
		}
		assertMissingKeys(t, missingRequiredConfigKeys(s), []string{"A", "Z"})
	})
}

func assertMissingKeys(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("missing = %v, want %v", got, want)
		}
	}
}

func TestBuildTransientServerDraftSecretReachesTransient(t *testing.T) {
	cmd := "echo"
	dto := &CreateMCPServerDTO{
		Name:    "gh",
		Type:    ServerTypeHTTP,
		URL:     ptr("https://8.8.8.8/mcp"),
		Headers: map[string]any{"Authorization": "secret-value"},
	}
	transient := buildTransientServer(dto)

	if transient.Headers["Authorization"] != "secret-value" {
		t.Fatalf("draft secret header value did not reach transient: %v", transient.Headers)
	}
	if len(transient.SecretHeaders) != 0 || len(transient.SecretEnv) != 0 {
		t.Fatalf("transient secret maps must be empty (no encryption): env=%v headers=%v",
			transient.SecretEnv, transient.SecretHeaders)
	}
	if transient.Name != "gh" {
		t.Fatalf("name = %q, want gh", transient.Name)
	}
	_ = cmd // referenced for clarity; transient.Command may be nil here
}

func TestBuildTransientServerNameFallback(t *testing.T) {
	cmd := "echo"
	transient := buildTransientServer(&CreateMCPServerDTO{Type: ServerTypeStdio, Command: &cmd})
	if transient.Name != "test" {
		t.Fatalf("name = %q, want test", transient.Name)
	}
	if transient.Target != TargetServer {
		t.Fatalf("target = %q, want server default", transient.Target)
	}
}

func TestBackfillTransientSecretsEditMode(t *testing.T) {
	storedCipher := EncryptedSecret{Ciphertext: []byte("cipher"), Nonce: []byte("nonce")}

	t.Run("blank draft header backfilled from stored secret", func(t *testing.T) {
		stored := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "Authorization", Kind: "header", IsSecret: true}},
			SecretHeaders:  map[string]EncryptedSecret{"Authorization": storedCipher},
		}
		transient := buildTransientServer(&CreateMCPServerDTO{
			Type:    ServerTypeHTTP,
			URL:     ptr("https://8.8.8.8/mcp"),
			Headers: map[string]any{"Authorization": ""},
		})

		backfillTransientSecrets(transient, stored)

		sec, ok := transient.SecretHeaders["Authorization"]
		if !ok {
			t.Fatalf("stored secret not backfilled; headers=%v", transient.SecretHeaders)
		}
		if string(sec.Ciphertext) != "cipher" {
			t.Fatalf("ciphertext not copied verbatim: %v", sec.Ciphertext)
		}
	})

	t.Run("draft value wins over stored secret", func(t *testing.T) {
		stored := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "Authorization", Kind: "header", IsSecret: true}},
			SecretHeaders:  map[string]EncryptedSecret{"Authorization": storedCipher},
		}
		transient := buildTransientServer(&CreateMCPServerDTO{
			Type:    ServerTypeHTTP,
			URL:     ptr("https://8.8.8.8/mcp"),
			Headers: map[string]any{"Authorization": "draft-token"},
		})

		backfillTransientSecrets(transient, stored)

		if _, ok := transient.SecretHeaders["Authorization"]; ok {
			t.Fatalf("draft value must win; secret should not be backfilled")
		}
		if transient.Headers["Authorization"] != "draft-token" {
			t.Fatalf("draft header was clobbered: %v", transient.Headers)
		}
	})

	t.Run("env backfill", func(t *testing.T) {
		stored := &MCPServer{
			RequiredConfig: []RequiredConfigKey{{Name: "TOKEN", Kind: "env", IsSecret: true}},
			SecretEnv:      map[string]EncryptedSecret{"TOKEN": storedCipher},
		}
		transient := buildTransientServer(&CreateMCPServerDTO{
			Type:    ServerTypeStdio,
			Command: ptr("echo"),
		})

		backfillTransientSecrets(transient, stored)

		if _, ok := transient.SecretEnv["TOKEN"]; !ok {
			t.Fatalf("stored env secret not backfilled; env=%v", transient.SecretEnv)
		}
	})
}

func TestTestServerConfigDeviceTargetRejected(t *testing.T) {
	svc, err := NewService(nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	cmd := "echo"
	_, err = svc.TestServerConfig(context.Background(), "p1", &CreateMCPServerDTO{
		Type:    ServerTypeStdio,
		Command: &cmd,
		Target:  TargetDevice,
	}, nil)
	if !errors.Is(err, ErrDeviceServerNotOutbound) {
		t.Fatalf("err = %v, want ErrDeviceServerNotOutbound", err)
	}
}

func TestTestServerConfigTransportValidation(t *testing.T) {
	svc, err := NewService(nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = svc.TestServerConfig(context.Background(), "p1", &CreateMCPServerDTO{
		Type: ServerTypeStdio, // no command
	}, nil)
	if err == nil {
		t.Fatal("expected command-required error")
	}

	_, err = svc.TestServerConfig(context.Background(), "p1", &CreateMCPServerDTO{
		Type: ServerTypeHTTP, // no url
	}, nil)
	if err == nil {
		t.Fatal("expected url-required error")
	}
}

func ptr(s string) *string { return &s }
