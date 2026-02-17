package githubapp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Crypto Tests ---

func generateTestKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return hex.EncodeToString(key)
}

func TestNewCrypto_ValidKey(t *testing.T) {
	key := generateTestKey(t)
	crypto, err := NewCrypto(key)
	require.NoError(t, err)
	assert.True(t, crypto.IsConfigured())
}

func TestNewCrypto_EmptyKey(t *testing.T) {
	crypto, err := NewCrypto("")
	require.NoError(t, err)
	assert.False(t, crypto.IsConfigured())
}

func TestNewCrypto_InvalidHex(t *testing.T) {
	_, err := NewCrypto("not-hex-at-all")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hex-encoded")
}

func TestNewCrypto_WrongKeyLength(t *testing.T) {
	shortKey := hex.EncodeToString(make([]byte, 16)) // 16 bytes instead of 32
	_, err := NewCrypto(shortKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "256 bits")
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	key := generateTestKey(t)
	crypto, err := NewCrypto(key)
	require.NoError(t, err)

	plaintext := []byte("This is a PEM private key content for testing")

	ciphertext, err := crypto.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)
	assert.True(t, len(ciphertext) > len(plaintext)) // ciphertext includes nonce + auth tag

	decrypted, err := crypto.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptDecryptString_Roundtrip(t *testing.T) {
	key := generateTestKey(t)
	crypto, err := NewCrypto(key)
	require.NoError(t, err)

	original := "-----BEGIN RSA PRIVATE KEY-----\nMIIBogIBAAJBALRiMLAHudeSA/...\n-----END RSA PRIVATE KEY-----"

	ciphertext, err := crypto.EncryptString(original)
	require.NoError(t, err)

	decrypted, err := crypto.DecryptString(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted)
}

func TestEncrypt_DifferentCiphertexts(t *testing.T) {
	key := generateTestKey(t)
	crypto, err := NewCrypto(key)
	require.NoError(t, err)

	plaintext := []byte("same content")

	ct1, err := crypto.Encrypt(plaintext)
	require.NoError(t, err)

	ct2, err := crypto.Encrypt(plaintext)
	require.NoError(t, err)

	// Same plaintext should produce different ciphertexts (different nonces)
	assert.NotEqual(t, ct1, ct2)
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := generateTestKey(t)
	key2 := generateTestKey(t)

	crypto1, err := NewCrypto(key1)
	require.NoError(t, err)

	crypto2, err := NewCrypto(key2)
	require.NoError(t, err)

	ciphertext, err := crypto1.Encrypt([]byte("secret data"))
	require.NoError(t, err)

	_, err = crypto2.Decrypt(ciphertext)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decryption failed")
}

func TestDecrypt_TooShort(t *testing.T) {
	key := generateTestKey(t)
	crypto, err := NewCrypto(key)
	require.NoError(t, err)

	_, err = crypto.Decrypt([]byte("short"))
	assert.Error(t, err)
}

func TestEncrypt_UnconfiguredCrypto(t *testing.T) {
	crypto, err := NewCrypto("")
	require.NoError(t, err)

	_, err = crypto.Encrypt([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encryption key not configured")
}

func TestDecrypt_UnconfiguredCrypto(t *testing.T) {
	crypto, err := NewCrypto("")
	require.NoError(t, err)

	_, err = crypto.Decrypt([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encryption key not configured")
}

func TestEncrypt_EmptyPlaintext(t *testing.T) {
	key := generateTestKey(t)
	crypto, err := NewCrypto(key)
	require.NoError(t, err)

	ciphertext, err := crypto.Encrypt([]byte{})
	require.NoError(t, err)

	decrypted, err := crypto.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Empty(t, decrypted)
}

// --- Entity Tests ---

func TestGitHubAppConfig_IsInstalled(t *testing.T) {
	tests := []struct {
		name           string
		installationID *int64
		expected       bool
	}{
		{"nil installation", nil, false},
		{"zero installation", ptrInt64(0), false},
		{"valid installation", ptrInt64(12345), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &GitHubAppConfig{InstallationID: tt.installationID}
			assert.Equal(t, tt.expected, config.IsInstalled())
		})
	}
}

// --- Bot Identity Tests ---

func TestBotCommitIdentity(t *testing.T) {
	name, email := BotCommitIdentity(12345)
	assert.Equal(t, "emergent-app[bot]", name)
	assert.Equal(t, "12345+emergent-app[bot]@users.noreply.github.com", email)
}

func TestDefaultCommitIdentity(t *testing.T) {
	name, email := DefaultCommitIdentity()
	assert.Equal(t, "Emergent Agent", name)
	assert.Equal(t, "agent@emergent.local", email)
}

// --- Manifest URL Tests ---

func TestGenerateManifestURL(t *testing.T) {
	svc := &Service{}

	url, err := svc.GenerateManifestURL("https://example.com/callback")
	require.NoError(t, err)
	assert.Contains(t, url, "https://github.com/settings/apps/new?manifest=")
	assert.Contains(t, url, "Emergent")
	assert.Contains(t, url, "contents")
}

// --- Token Cache Tests ---

func TestTokenCacheDuration(t *testing.T) {
	assert.Equal(t, 55*60, int(tokenCacheDuration.Seconds()),
		"token cache duration should be 55 minutes")
}

// --- Helpers ---

func ptrInt64(v int64) *int64 {
	return &v
}

// --- Webhook Signature Verification Tests ---

func computeHMACSHA256(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyHMACSignature_Valid(t *testing.T) {
	secret := []byte("webhook-secret-123")
	body := []byte(`{"action":"created","installation":{"id":1}}`)
	signature := computeHMACSHA256(secret, body)

	err := verifyHMACSignature(secret, signature, body)
	assert.NoError(t, err)
}

func TestVerifyHMACSignature_InvalidSignature(t *testing.T) {
	secret := []byte("webhook-secret-123")
	body := []byte(`{"action":"created"}`)

	err := verifyHMACSignature(secret, "sha256=deadbeef", body)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature")
}

func TestVerifyHMACSignature_WrongSecret(t *testing.T) {
	secret1 := []byte("correct-secret")
	secret2 := []byte("wrong-secret")
	body := []byte(`{"action":"created"}`)
	signature := computeHMACSHA256(secret1, body)

	err := verifyHMACSignature(secret2, signature, body)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature")
}

func TestVerifyHMACSignature_TamperedBody(t *testing.T) {
	secret := []byte("webhook-secret-123")
	body := []byte(`{"action":"created"}`)
	signature := computeHMACSHA256(secret, body)

	tamperedBody := []byte(`{"action":"deleted"}`)
	err := verifyHMACSignature(secret, signature, tamperedBody)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature")
}

func TestVerifyHMACSignature_EmptyBody(t *testing.T) {
	secret := []byte("webhook-secret-123")
	body := []byte{}
	signature := computeHMACSHA256(secret, body)

	err := verifyHMACSignature(secret, signature, body)
	assert.NoError(t, err)
}

func TestVerifyHMACSignature_MissingPrefix(t *testing.T) {
	secret := []byte("webhook-secret-123")
	body := []byte(`test`)

	// Compute valid HMAC but strip "sha256=" prefix
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	rawSig := hex.EncodeToString(mac.Sum(nil))

	err := verifyHMACSignature(secret, rawSig, body)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature")
}
