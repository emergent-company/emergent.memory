package sources

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCredentialPermissions(t *testing.T) {
	got, err := normalizeCredentialPermissions(map[string]string{"contents": "write", "issues": "read"})
	require.NoError(t, err)
	require.Equal(t, "write", got["contents"])
	require.Equal(t, "read", got["issues"])

	got, err = normalizeCredentialPermissions(nil)
	require.NoError(t, err)
	require.Equal(t, "read", got["contents"])

	_, err = normalizeCredentialPermissions(map[string]string{"admin": "write"})
	require.ErrorContains(t, err, "not allowed")

	_, err = normalizeCredentialPermissions(map[string]string{"contents": "admin"})
	require.ErrorContains(t, err, "read or write")
}
