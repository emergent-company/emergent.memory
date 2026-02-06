package auth

import (
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverOIDC_ValidIssuer(t *testing.T) {
	discoveryDoc := map[string]interface{}{
		"issuer":                        "https://auth.example.com",
		"device_authorization_endpoint": "https://auth.example.com/oauth/device_authorization",
		"token_endpoint":                "https://auth.example.com/oauth/token",
		"userinfo_endpoint":             "https://auth.example.com/oauth/userinfo",
	}

	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/.well-known/openid-configuration": testutil.WithJSONResponse(200, discoveryDoc),
	})
	defer server.Close()

	config, err := DiscoverOIDC(server.URL)
	require.NoError(t, err)
	require.NotNil(t, config)

	assert.Equal(t, "https://auth.example.com", config.Issuer)
	assert.Equal(t, "https://auth.example.com/oauth/device_authorization", config.DeviceAuthorizationEndpoint)
	assert.Equal(t, "https://auth.example.com/oauth/token", config.TokenEndpoint)
	assert.Equal(t, "https://auth.example.com/oauth/userinfo", config.UserinfoEndpoint)
}

func TestDiscoverOIDC_InvalidIssuer(t *testing.T) {
	config, err := DiscoverOIDC("http://invalid.test.local.nonexistent")
	assert.Error(t, err)
	assert.Nil(t, config)
}

func TestDiscoverOIDC_MissingRequiredFields(t *testing.T) {
	incompleteDoc := map[string]interface{}{
		"issuer": "https://auth.example.com",
	}

	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/.well-known/openid-configuration": testutil.WithJSONResponse(200, incompleteDoc),
	})
	defer server.Close()

	config, err := DiscoverOIDC(server.URL)
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "missing required field")
}
