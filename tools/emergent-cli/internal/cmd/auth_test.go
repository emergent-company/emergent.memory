package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogout(t *testing.T) {
	tempDir := t.TempDir()

	emergentDir := filepath.Join(tempDir, ".emergent")
	err := os.MkdirAll(emergentDir, 0700)
	require.NoError(t, err)

	credsPath := filepath.Join(emergentDir, "credentials.json")
	err = os.WriteFile(credsPath, []byte(`{"access_token":"test"}`), 0600)
	require.NoError(t, err)

	require.FileExists(t, credsPath)

	t.Setenv("HOME", tempDir)
	if os.PathSeparator == '\\' {
		t.Setenv("USERPROFILE", tempDir)
	}

	cmd := newLogoutCmd()

	capture := testutil.CaptureOutput()
	err = cmd.Execute()
	require.NoError(t, err)
	stdout, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	assert.Contains(t, stdout, "Logged out successfully")

	_, err = os.Stat(credsPath)
	assert.True(t, os.IsNotExist(err), "credentials file should be deleted")
}
