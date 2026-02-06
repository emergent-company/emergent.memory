package testutil

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMockServer(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"/health": WithJSONResponse(200, map[string]string{"status": "ok"}),
		"/users":  WithJSONResponse(200, []string{"user1", "user2"}),
	}

	server := NewMockServer(handlers)
	defer server.Close()

	// Test /health endpoint
	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Test /users endpoint
	resp2, err := http.Get(server.URL + "/users")
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.Equal(t, 200, resp2.StatusCode)

	// Test non-existent endpoint returns 404
	resp3, err := http.Get(server.URL + "/not-found")
	require.NoError(t, err)
	defer resp3.Body.Close()

	assert.Equal(t, 404, resp3.StatusCode)
}

func TestMockServerHandlers(t *testing.T) {
	called := false
	customHandler := func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(201)
		w.Write([]byte("custom response"))
	}

	handlers := map[string]http.HandlerFunc{
		"/custom": customHandler,
	}

	server := NewMockServer(handlers)
	defer server.Close()

	resp, err := http.Get(server.URL + "/custom")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.True(t, called, "custom handler should be called")
	assert.Equal(t, 201, resp.StatusCode)
}

func TestMockServerClose(t *testing.T) {
	server := NewMockServer(map[string]http.HandlerFunc{})

	// Verify server is running
	resp, err := http.Get(server.URL + "/")
	require.NoError(t, err)
	resp.Body.Close()

	// Close server
	server.Close()

	// Verify server is no longer accessible
	_, err = http.Get(server.URL + "/")
	assert.Error(t, err, "should not be able to connect after close")
}

func TestWithJSONResponse(t *testing.T) {
	data := map[string]interface{}{
		"name":  "test",
		"count": 42,
	}

	handler := WithJSONResponse(200, data)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"name":"test"`)
	assert.Contains(t, rec.Body.String(), `"count":42`)
}

func TestWithDelayedResponse(t *testing.T) {
	baseHandler := WithJSONResponse(200, map[string]string{"status": "ok"})
	delayedHandler := WithDelayedResponse(50*time.Millisecond, baseHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	delayedHandler(rec, req)
	duration := time.Since(start)

	assert.GreaterOrEqual(t, duration, 50*time.Millisecond, "should delay at least 50ms")
	assert.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}
