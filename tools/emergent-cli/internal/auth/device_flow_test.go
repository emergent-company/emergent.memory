package auth

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestDeviceCode_Success(t *testing.T) {
	deviceResponse := map[string]interface{}{
		"device_code":               "GmRhmhcxhwAzkoEqiMEg_DnyEysNkuNhszIySk9eS",
		"user_code":                 "WDJB-MJHT",
		"verification_uri":          "https://auth.example.com/device",
		"verification_uri_complete": "https://auth.example.com/device?user_code=WDJB-MJHT",
		"expires_in":                1800,
		"interval":                  5,
	}

	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/device_authorization": testutil.WithJSONResponse(200, deviceResponse),
	})
	defer server.Close()

	config := &OIDCConfig{
		Issuer:                      server.URL,
		DeviceAuthorizationEndpoint: server.URL + "/oauth/device_authorization",
		TokenEndpoint:               server.URL + "/oauth/token",
		UserinfoEndpoint:            server.URL + "/oauth/userinfo",
	}

	resp, err := RequestDeviceCode(config, "test-client-id", []string{"openid", "profile", "email"})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "GmRhmhcxhwAzkoEqiMEg_DnyEysNkuNhszIySk9eS", resp.DeviceCode)
	assert.Equal(t, "WDJB-MJHT", resp.UserCode)
	assert.Equal(t, "https://auth.example.com/device", resp.VerificationURI)
	assert.Equal(t, "https://auth.example.com/device?user_code=WDJB-MJHT", resp.VerificationURIComplete)
	assert.Equal(t, 1800, resp.ExpiresIn)
	assert.Equal(t, 5, resp.Interval)
}

func TestRequestDeviceCode_InvalidClientID(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/device_authorization": testutil.WithJSONResponse(400, map[string]interface{}{
			"error":             "invalid_client",
			"error_description": "Client authentication failed",
		}),
	})
	defer server.Close()

	config := &OIDCConfig{
		DeviceAuthorizationEndpoint: server.URL + "/oauth/device_authorization",
	}

	resp, err := RequestDeviceCode(config, "invalid-client", []string{"openid"})
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "400")
}

func TestOpenBrowser_ValidURL(t *testing.T) {
	t.Skip("Skipping browser open test - requires manual verification")

	err := OpenBrowser("https://example.com")
	assert.NoError(t, err)
}

func TestOpenBrowser_InvalidURL(t *testing.T) {
	err := OpenBrowser("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty URL")
}

func TestOpenBrowser_Fallback(t *testing.T) {
	err := OpenBrowser("https://auth.example.com/device?user_code=TEST")
	if err != nil {
		assert.Contains(t, err.Error(), "please open")
	}
}

func TestPollForToken_Success(t *testing.T) {
	var callCount atomic.Int32

	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/token": func(w http.ResponseWriter, r *http.Request) {
			count := callCount.Add(1)

			if count < 3 {
				testutil.WithJSONResponse(400, map[string]interface{}{
					"error": "authorization_pending",
				})(w, r)
				return
			}

			testutil.WithJSONResponse(200, map[string]interface{}{
				"access_token":  "ya29.a0AfH6SMBx...",
				"refresh_token": "1//0gKN3...",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})(w, r)
		},
	})
	defer server.Close()

	config := &OIDCConfig{
		TokenEndpoint: server.URL + "/oauth/token",
	}

	token, err := PollForToken(config, "test-device-code", "test-client-id", 1, 10)
	require.NoError(t, err)
	require.NotNil(t, token)

	assert.Equal(t, "ya29.a0AfH6SMBx...", token.AccessToken)
	assert.Equal(t, "1//0gKN3...", token.RefreshToken)
	assert.Equal(t, 3600, token.ExpiresIn)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.True(t, callCount.Load() >= 3)
}

func TestPollForToken_SlowDown(t *testing.T) {
	var callCount atomic.Int32

	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/token": func(w http.ResponseWriter, r *http.Request) {
			count := callCount.Add(1)

			if count == 1 {
				testutil.WithJSONResponse(400, map[string]interface{}{
					"error": "slow_down",
				})(w, r)
				return
			}

			testutil.WithJSONResponse(200, map[string]interface{}{
				"access_token": "token123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})(w, r)
		},
	})
	defer server.Close()

	config := &OIDCConfig{
		TokenEndpoint: server.URL + "/oauth/token",
	}

	start := time.Now()
	token, err := PollForToken(config, "test-device-code", "test-client-id", 1, 10)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, token)
	assert.True(t, elapsed.Seconds() >= 2, "should wait longer after slow_down")
}

func TestPollForToken_AccessDenied(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/token": testutil.WithJSONResponse(400, map[string]interface{}{
			"error":             "access_denied",
			"error_description": "User denied authorization",
		}),
	})
	defer server.Close()

	config := &OIDCConfig{
		TokenEndpoint: server.URL + "/oauth/token",
	}

	token, err := PollForToken(config, "test-device-code", "test-client-id", 1, 10)
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "access_denied")
}

func TestPollForToken_ExpiredToken(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/token": testutil.WithJSONResponse(400, map[string]interface{}{
			"error": "expired_token",
		}),
	})
	defer server.Close()

	config := &OIDCConfig{
		TokenEndpoint: server.URL + "/oauth/token",
	}

	token, err := PollForToken(config, "test-device-code", "test-client-id", 1, 10)
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "expired")
}

func TestPollForToken_Timeout(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/token": testutil.WithJSONResponse(400, map[string]interface{}{
			"error": "authorization_pending",
		}),
	})
	defer server.Close()

	config := &OIDCConfig{
		TokenEndpoint: server.URL + "/oauth/token",
	}

	token, err := PollForToken(config, "test-device-code", "test-client-id", 1, 2)
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "timeout")
}

func TestRefreshToken_Success(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/token": testutil.WithJSONResponse(200, map[string]interface{}{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
			"token_type":    "Bearer",
		}),
	})
	defer server.Close()

	config := &OIDCConfig{
		TokenEndpoint: server.URL + "/oauth/token",
	}

	token, err := RefreshToken(config, "old-refresh-token", "test-client-id")
	require.NoError(t, err)
	require.NotNil(t, token)

	assert.Equal(t, "new-access-token", token.AccessToken)
	assert.Equal(t, "new-refresh-token", token.RefreshToken)
	assert.Equal(t, 3600, token.ExpiresIn)
	assert.Equal(t, "Bearer", token.TokenType)
}

func TestRefreshToken_InvalidToken(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/token": testutil.WithJSONResponse(400, map[string]interface{}{
			"error":             "invalid_grant",
			"error_description": "Refresh token expired or revoked",
		}),
	})
	defer server.Close()

	config := &OIDCConfig{
		TokenEndpoint: server.URL + "/oauth/token",
	}

	token, err := RefreshToken(config, "invalid-refresh-token", "test-client-id")
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "invalid_grant")
}

func TestRefreshToken_NetworkError(t *testing.T) {
	config := &OIDCConfig{
		TokenEndpoint: "http://invalid.test.local.nonexistent/token",
	}

	token, err := RefreshToken(config, "some-token", "test-client-id")
	assert.Error(t, err)
	assert.Nil(t, token)
}

func TestGetUserInfo_Success(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/userinfo": testutil.WithJSONResponse(200, map[string]interface{}{
			"sub":   "user-123",
			"email": "user@example.com",
			"name":  "Test User",
		}),
	})
	defer server.Close()

	config := &OIDCConfig{
		UserinfoEndpoint: server.URL + "/oauth/userinfo",
	}

	userInfo, err := GetUserInfo(config, "valid-access-token")
	require.NoError(t, err)
	require.NotNil(t, userInfo)

	assert.Equal(t, "user-123", userInfo.Sub)
	assert.Equal(t, "user@example.com", userInfo.Email)
	assert.Equal(t, "Test User", userInfo.Name)
}

func TestGetUserInfo_InvalidToken(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/oauth/userinfo": testutil.WithJSONResponse(401, map[string]interface{}{
			"error":             "invalid_token",
			"error_description": "Token is invalid or expired",
		}),
	})
	defer server.Close()

	config := &OIDCConfig{
		UserinfoEndpoint: server.URL + "/oauth/userinfo",
	}

	userInfo, err := GetUserInfo(config, "invalid-token")
	assert.Error(t, err)
	assert.Nil(t, userInfo)
	assert.Contains(t, err.Error(), "401")
}
