package mcpregistry

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// testEncryptor builds a real AES-256 encryptor from a fresh random key.
func testEncryptor(t *testing.T) *crypto.Encryptor {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	enc, err := crypto.NewEncryptor(key)
	require.NoError(t, err)
	return enc
}

func encryptForTest(t *testing.T, enc *crypto.Encryptor, plaintext string) EncryptedSecret {
	t.Helper()
	ct, nonce, err := enc.Encrypt([]byte(plaintext))
	require.NoError(t, err)
	return EncryptedSecret{Ciphertext: ct, Nonce: nonce}
}

// TestSplitSecrets_CreateEncryptsAndStrips covers create semantics: secret keys
// are removed from the plaintext map and stored encrypted.
func TestSplitSecrets_CreateEncryptsAndStrips(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)

	values := map[string]any{
		"PLAIN":  "visible",
		"SECRET": "s3cr3t-pem",
	}

	plain, secrets, err := splitSecrets(enc, values, []string{"SECRET"}, nil, "env")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"PLAIN": "visible"}, plain)
	_, leaked := plain["SECRET"]
	assert.False(t, leaked, "secret key must be stripped from plaintext")

	sec, ok := secrets["SECRET"]
	require.True(t, ok, "secret must be encrypted")
	assert.NotEmpty(t, sec.Ciphertext)
	assert.NotEmpty(t, sec.Nonce)

	got, err := enc.Decrypt(sec.Ciphertext, sec.Nonce)
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t-pem", string(got))
}

// TestToDTO_ExposesOnlySecretKeys ensures the response DTO never includes
// secret values — only the key names, deterministically sorted.
func TestToDTO_ExposesOnlySecretKeys(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)

	server := &MCPServer{
		ID:   "srv-1",
		Name: "gh",
		Env: map[string]any{
			"PLAIN_ENV": "1",
		},
		Headers: map[string]any{
			"X-Plain": "1",
		},
		SecretEnv: map[string]EncryptedSecret{
			"Z_KEY": encryptForTest(t, enc, "z-secret"),
			"A_KEY": encryptForTest(t, enc, "a-secret"),
		},
		SecretHeaders: map[string]EncryptedSecret{
			"Authorization": encryptForTest(t, enc, "Bearer tok"),
		},
	}

	dto := server.ToDTO()

	assert.Equal(t, map[string]any{"PLAIN_ENV": "1"}, dto.Env)
	assert.Equal(t, []string{"A_KEY", "Z_KEY"}, dto.SecretEnvKeys)
	assert.Equal(t, []string{"Authorization"}, dto.SecretHeadersKeys)
	assert.Equal(t, map[string]any{"X-Plain": "1"}, dto.Headers)

	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(raw), "z-secret"), "secret env value leaked into JSON: %s", raw)
	assert.False(t, strings.Contains(string(raw), "a-secret"), "secret env value leaked into JSON: %s", raw)
	assert.False(t, strings.Contains(string(raw), "Bearer tok"), "secret header value leaked into JSON: %s", raw)
	assert.Contains(t, string(raw), "secretEnvKeys")
	assert.Contains(t, string(raw), "A_KEY")
}

// TestSplitSecrets_UpdateEmptyKeepsPrior verifies that listing a key with an
// empty value retains the previously stored ciphertext (partial update).
func TestSplitSecrets_UpdateEmptyKeepsPrior(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)
	prior := encryptForTest(t, enc, "old-secret")

	plain, secrets, err := splitSecrets(enc, map[string]any{"SECRET": ""}, []string{"SECRET"},
		map[string]EncryptedSecret{"SECRET": prior}, "env")
	require.NoError(t, err)
	assert.Empty(t, plain)
	require.Contains(t, secrets, "SECRET")
	assert.True(t, bytes.Equal(prior.Ciphertext, secrets["SECRET"].Ciphertext))
	assert.True(t, bytes.Equal(prior.Nonce, secrets["SECRET"].Nonce))

	got, err := enc.Decrypt(secrets["SECRET"].Ciphertext, secrets["SECRET"].Nonce)
	require.NoError(t, err)
	assert.Equal(t, "old-secret", string(got))
}

// TestSplitSecrets_UpdateNewValueReencrypts verifies a new value replaces the
// prior ciphertext.
func TestSplitSecrets_UpdateNewValueReencrypts(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)
	prior := encryptForTest(t, enc, "old-secret")

	_, secrets, err := splitSecrets(enc, map[string]any{"SECRET": "new-secret"}, []string{"SECRET"},
		map[string]EncryptedSecret{"SECRET": prior}, "env")
	require.NoError(t, err)
	require.Contains(t, secrets, "SECRET")
	assert.False(t, bytes.Equal(prior.Ciphertext, secrets["SECRET"].Ciphertext), "ciphertext should change")

	got, err := enc.Decrypt(secrets["SECRET"].Ciphertext, secrets["SECRET"].Nonce)
	require.NoError(t, err)
	assert.Equal(t, "new-secret", string(got))
}

// TestSplitSecrets_RemovesUnlisted verifies keys no longer listed are dropped
// from the encrypted map.
func TestSplitSecrets_RemovesUnlisted(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)
	prior := encryptForTest(t, enc, "old-secret")

	plain, secrets, err := splitSecrets(enc, map[string]any{"OTHER": "plain"}, nil,
		map[string]EncryptedSecret{"SECRET": prior}, "env")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"OTHER": "plain"}, plain)
	assert.Empty(t, secrets)
}

// TestDecryptHelpers_RoundTrip verifies the proxy merges decrypted secrets into
// env/headers used for spawning and connecting.
func TestDecryptHelpers_RoundTrip(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)
	pm := &ProxyManager{encryptor: enc, log: slog.Default()}

	server := &MCPServer{
		Env: map[string]any{"PLAIN": "p"},
		SecretEnv: map[string]EncryptedSecret{
			"TOKEN": encryptForTest(t, enc, "s3cr3t"),
		},
		Headers: map[string]any{"X-Plain": "p"},
		SecretHeaders: map[string]EncryptedSecret{
			"Authorization": encryptForTest(t, enc, "Bearer abc"),
		},
	}

	env, err := pm.decryptEnv(server)
	require.NoError(t, err)
	assert.Contains(t, env, "PLAIN=p")
	assert.Contains(t, env, "TOKEN=s3cr3t")

	headers, err := pm.decryptHeaders(server)
	require.NoError(t, err)
	assert.Equal(t, "p", headers["X-Plain"])
	assert.Equal(t, "Bearer abc", headers["Authorization"])
}

// TestSplitSecrets_NilEncryptor verifies a nil encryptor errors only when a
// secret value actually needs encrypting, and is a no-op otherwise.
func TestSplitSecrets_NilEncryptor(t *testing.T) {
	t.Parallel()

	_, _, err := splitSecrets(nil, map[string]any{"SECRET": "value"}, []string{"SECRET"}, nil, "env")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM_ENCRYPTION_KEY")

	// No secret keys: no error.
	plain, secrets, err := splitSecrets(nil, map[string]any{"A": "b"}, nil, nil, "env")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"A": "b"}, plain)
	assert.Empty(t, secrets)

	// Secret key listed but empty on create: nothing to encrypt, no error.
	plain, secrets, err = splitSecrets(nil, map[string]any{"SECRET": ""}, []string{"SECRET"}, nil, "env")
	require.NoError(t, err)
	assert.Empty(t, plain)
	assert.Empty(t, secrets)
}

// TestDecryptHelpers_NilEncryptorErrors verifies a stored secret cannot be read
// without an encryptor.
func TestDecryptHelpers_NilEncryptorErrors(t *testing.T) {
	t.Parallel()
	enc := testEncryptor(t)
	pm := &ProxyManager{encryptor: nil, log: slog.Default()}

	server := &MCPServer{
		SecretEnv: map[string]EncryptedSecret{"TOKEN": encryptForTest(t, enc, "x")},
	}
	_, err := pm.decryptEnv(server)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM_ENCRYPTION_KEY")

	// With no stored secrets the nil encryptor is a no-op.
	env, err := pm.decryptEnv(&MCPServer{Env: map[string]any{"A": "b"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"A=b"}, env)
}
