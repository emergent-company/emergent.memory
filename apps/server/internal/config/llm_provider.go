package config

// LLMProviderConfig holds configuration for multi-tenant LLM provider management
type LLMProviderConfig struct {
	// EncryptionKey is the AES-256 encryption key (64-char hex string = 32 bytes)
	// Used to encrypt/decrypt provider credentials at rest.
	// Required when credential storage features are used.
	EncryptionKey string `env:"LLM_ENCRYPTION_KEY" envDefault:""`
}

// IsEncryptionConfigured returns true if the encryption key is set
func (c *LLMProviderConfig) IsEncryptionConfigured() bool {
	return c.EncryptionKey != ""
}
