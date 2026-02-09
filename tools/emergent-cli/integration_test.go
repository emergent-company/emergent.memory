package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/auth"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_ConfigManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := &config.Config{
		ServerURL: "https://zitadel.emergent.mcj-one.eyedea.dev",
		Email:     "test@emergent-company.ai",
		OrgID:     "test-org",
		ProjectID: "test-project",
		Debug:     true,
	}

	err := config.Save(cfg, configPath)
	require.NoError(t, err, "should save config")

	loadedCfg, err := config.Load(configPath)
	require.NoError(t, err, "should load config")

	assert.Equal(t, cfg.ServerURL, loadedCfg.ServerURL)
	assert.Equal(t, cfg.Email, loadedCfg.Email)
	assert.Equal(t, cfg.OrgID, loadedCfg.OrgID)
	assert.Equal(t, cfg.ProjectID, loadedCfg.ProjectID)
	assert.Equal(t, cfg.Debug, loadedCfg.Debug)
}

func TestIntegration_OIDCDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	issuerURL := "https://zitadel.emergent.mcj-one.eyedea.dev"

	oidcConfig, err := auth.DiscoverOIDC(issuerURL)
	if err != nil {
		// Skip on network/infrastructure errors (502, 503, connection refused, etc.)
		t.Skipf("Skipping - OIDC discovery unavailable: %v", err)
	}
	require.NotNil(t, oidcConfig)

	assert.Equal(t, issuerURL, oidcConfig.Issuer)
	assert.Contains(t, oidcConfig.DeviceAuthorizationEndpoint, "/device_authorization")
	assert.Contains(t, oidcConfig.TokenEndpoint, "/token")
	assert.Contains(t, oidcConfig.UserinfoEndpoint, "/userinfo")

	t.Logf("OIDC Discovery successful:")
	t.Logf("  Issuer: %s", oidcConfig.Issuer)
	t.Logf("  Device Auth Endpoint: %s", oidcConfig.DeviceAuthorizationEndpoint)
	t.Logf("  Token Endpoint: %s", oidcConfig.TokenEndpoint)
	t.Logf("  Userinfo Endpoint: %s", oidcConfig.UserinfoEndpoint)
}

func TestIntegration_DeviceCodeRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Skip("Skipping - requires registered OAuth client in Zitadel")

	issuerURL := "https://zitadel.emergent.mcj-one.eyedea.dev"

	oidcConfig, err := auth.DiscoverOIDC(issuerURL)
	require.NoError(t, err, "should discover OIDC configuration")

	clientID := "emergent-cli"
	scopes := []string{"openid", "profile", "email"}

	deviceResp, err := auth.RequestDeviceCode(oidcConfig, clientID, scopes)
	require.NoError(t, err, "should request device code")
	require.NotNil(t, deviceResp)

	assert.NotEmpty(t, deviceResp.DeviceCode, "device code should not be empty")
	assert.NotEmpty(t, deviceResp.UserCode, "user code should not be empty")
	assert.NotEmpty(t, deviceResp.VerificationURI, "verification URI should not be empty")
	assert.Greater(t, deviceResp.ExpiresIn, 0, "expires_in should be positive")
	assert.Greater(t, deviceResp.Interval, 0, "interval should be positive")

	t.Logf("Device Code Request successful:")
	t.Logf("  User Code: %s", deviceResp.UserCode)
	t.Logf("  Verification URI: %s", deviceResp.VerificationURI)
	t.Logf("  Expires In: %d seconds", deviceResp.ExpiresIn)
	t.Logf("  Polling Interval: %d seconds", deviceResp.Interval)

	if deviceResp.VerificationURIComplete != "" {
		t.Logf("  Complete URI: %s", deviceResp.VerificationURIComplete)
	}
}

func TestIntegration_CredentialsStorage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := t.TempDir()
	credsPath := filepath.Join(tempDir, ".emergent", "credentials.json")

	expiresAt := time.Now().Add(1 * time.Hour)
	creds := &auth.Credentials{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    expiresAt,
		UserEmail:    "test@emergent-company.ai",
		IssuerURL:    "https://zitadel.emergent.mcj-one.eyedea.dev",
	}

	err := auth.Save(creds, credsPath)
	require.NoError(t, err, "should save credentials")

	info, err := os.Stat(credsPath)
	require.NoError(t, err, "credentials file should exist")
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "should have 0600 permissions")

	dirInfo, err := os.Stat(filepath.Dir(credsPath))
	require.NoError(t, err, "credentials directory should exist")
	assert.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm(), "directory should have 0700 permissions")

	loadedCreds, err := auth.Load(credsPath)
	require.NoError(t, err, "should load credentials")

	assert.Equal(t, creds.AccessToken, loadedCreds.AccessToken)
	assert.Equal(t, creds.RefreshToken, loadedCreds.RefreshToken)
	assert.Equal(t, creds.UserEmail, loadedCreds.UserEmail)
	assert.Equal(t, creds.IssuerURL, loadedCreds.IssuerURL)
	assert.WithinDuration(t, creds.ExpiresAt, loadedCreds.ExpiresAt, 1*time.Second)

	assert.False(t, loadedCreds.IsExpired(), "credentials should not be expired")

	expiredCreds := &auth.Credentials{
		AccessToken: "expired",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	assert.True(t, expiredCreds.IsExpired(), "old credentials should be expired")

	t.Logf("Credentials storage test successful:")
	t.Logf("  File: %s", credsPath)
	t.Logf("  Permissions: %o", info.Mode().Perm())
	t.Logf("  Directory Permissions: %o", dirInfo.Mode().Perm())
}

func TestIntegration_ConfigFileDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "custom-config.yaml")

	cfg := &config.Config{
		ServerURL: "https://test.example.com",
	}
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	originalEnv := os.Getenv("EMERGENT_CONFIG")
	defer func() {
		if originalEnv != "" {
			os.Setenv("EMERGENT_CONFIG", originalEnv)
		} else {
			os.Unsetenv("EMERGENT_CONFIG")
		}
	}()

	os.Setenv("EMERGENT_CONFIG", configPath)

	discoveredPath := config.DiscoverPath("")
	assert.Equal(t, configPath, discoveredPath, "should discover config from env var")

	flagPath := filepath.Join(tempDir, "flag-config.yaml")
	flagCfg := &config.Config{
		ServerURL: "https://flag.example.com",
	}
	err = config.Save(flagCfg, flagPath)
	require.NoError(t, err)

	discoveredPath = config.DiscoverPath(flagPath)
	assert.Equal(t, flagPath, discoveredPath, "flag should take precedence over env")
}

func TestIntegration_EnvironmentOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := &config.Config{
		ServerURL: "https://file.example.com",
		Email:     "file@example.com",
	}
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	t.Setenv("EMERGENT_SERVER_URL", "https://env.example.com")
	t.Setenv("EMERGENT_EMAIL", "env@example.com")

	loadedCfg, err := config.LoadWithEnv(configPath)
	require.NoError(t, err)

	assert.Equal(t, "https://env.example.com", loadedCfg.ServerURL, "env should override file")
	assert.Equal(t, "env@example.com", loadedCfg.Email, "env should override file")
}

func TestIntegration_JSONSerialization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	creds := &auth.Credentials{
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		UserEmail:    "test@example.com",
		IssuerURL:    "https://auth.example.com",
	}

	jsonData, err := json.MarshalIndent(creds, "", "  ")
	require.NoError(t, err)

	var decoded auth.Credentials
	err = json.Unmarshal(jsonData, &decoded)
	require.NoError(t, err)

	assert.Equal(t, creds.AccessToken, decoded.AccessToken)
	assert.Equal(t, creds.RefreshToken, decoded.RefreshToken)
	assert.Equal(t, creds.UserEmail, decoded.UserEmail)
	assert.Equal(t, creds.IssuerURL, decoded.IssuerURL)
}
