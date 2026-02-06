package testutil

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTempConfig(t *testing.T) {
	content := "test: value\nfoo: bar"

	path := CreateTempConfig(t, content)

	// Verify file exists
	_, err := os.Stat(path)
	require.NoError(t, err, "temp config file should exist")

	// Verify content
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(data), "content should match")

	// Verify it's a temp file
	assert.Contains(t, path, os.TempDir(), "should be in temp directory")
}

func TestCreateTempConfig_Cleanup(t *testing.T) {
	var path string

	// Run in subtest to trigger cleanup
	t.Run("create", func(t *testing.T) {
		path = CreateTempConfig(t, "cleanup test")
		_, err := os.Stat(path)
		require.NoError(t, err, "file should exist during test")
	})

	// After subtest completes, cleanup should have run
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "file should be cleaned up after test")
}

func TestSetEnv(t *testing.T) {
	key := "TEST_ENV_VAR_12345"
	value := "test_value"
	original := os.Getenv(key)

	SetEnv(t, key, value)

	// Verify environment variable is set
	assert.Equal(t, value, os.Getenv(key), "env var should be set")

	// Manually trigger cleanup (in real test, t.Cleanup handles this)
	defer func() {
		if original == "" {
			os.Unsetenv(key)
		} else {
			os.Setenv(key, original)
		}
	}()
}

func TestSetEnv_RestoresOriginal(t *testing.T) {
	key := "TEST_ENV_RESTORE_12345"
	original := "original_value"

	// Set initial value
	os.Setenv(key, original)
	defer os.Unsetenv(key)

	var capturedValue string

	// Run in subtest
	t.Run("set", func(t *testing.T) {
		SetEnv(t, key, "new_value")
		capturedValue = os.Getenv(key)
		assert.Equal(t, "new_value", capturedValue)
	})

	// After subtest, original should be restored
	assert.Equal(t, original, os.Getenv(key), "original value should be restored")
}

func TestCaptureOutput(t *testing.T) {
	capture := CaptureOutput()
	defer capture.Restore()

	// Write to stdout and stderr
	os.Stdout.WriteString("stdout message\n")
	os.Stderr.WriteString("stderr message\n")

	stdout, stderr, err := capture.Read()
	require.NoError(t, err)

	assert.Contains(t, stdout, "stdout message", "should capture stdout")
	assert.Contains(t, stderr, "stderr message", "should capture stderr")
}

func TestCaptureOutput_Restore(t *testing.T) {
	originalStdout := os.Stdout
	originalStderr := os.Stderr

	capture := CaptureOutput()

	// Verify stdout/stderr are replaced
	assert.NotEqual(t, originalStdout, os.Stdout, "stdout should be replaced")
	assert.NotEqual(t, originalStderr, os.Stderr, "stderr should be replaced")

	capture.Restore()

	// Verify they're restored
	assert.Equal(t, originalStdout, os.Stdout, "stdout should be restored")
	assert.Equal(t, originalStderr, os.Stderr, "stderr should be restored")
}

func TestCreateTestConfig(t *testing.T) {
	serverURL := "http://localhost:3000"
	email := "test@example.com"

	path := CreateTestConfig(serverURL, email)

	// Verify file exists
	_, err := os.Stat(path)
	require.NoError(t, err)

	// Verify content
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, serverURL, "should contain server URL")
	assert.Contains(t, content, email, "should contain email")
	assert.Contains(t, content, "server_url:", "should have YAML format")
}

func TestWithConfigFile(t *testing.T) {
	content := "test_key: test_value\nfoo: bar"

	path := WithConfigFile(t, content)

	// Verify file exists
	_, err := os.Stat(path)
	require.NoError(t, err)

	// Verify env var is set
	configPath := os.Getenv("EMERGENT_CONFIG")
	assert.Equal(t, path, configPath, "EMERGENT_CONFIG should point to config file")

	// Verify content
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}
