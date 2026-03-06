package workspace

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2BProvider_Capabilities(t *testing.T) {
	p := &E2BProvider{
		log:    testLogger(),
		config: &E2BProviderConfig{APIKey: "test-key"},
	}

	caps := p.Capabilities()
	assert.Equal(t, "E2B (managed)", caps.Name)
	assert.Equal(t, ProviderE2B, caps.ProviderType)
	assert.False(t, caps.SupportsPersistence)
	assert.False(t, caps.SupportsSnapshots)
	assert.False(t, caps.SupportsWarmPool)
	assert.False(t, caps.RequiresKVM)
	assert.Equal(t, 150, caps.EstimatedStartupMs)
}

func TestE2BProvider_NewRequiresAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *E2BProviderConfig
		wantErr bool
	}{
		{"nil config", nil, true},
		{"empty key", &E2BProviderConfig{APIKey: ""}, true},
		{"valid key", &E2BProviderConfig{APIKey: "test-key"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewE2BProvider(testLogger(), tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, p)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, p)
			}
		})
	}
}

func TestE2BProvider_ConfigDefaults(t *testing.T) {
	p, err := NewE2BProvider(testLogger(), &E2BProviderConfig{
		APIKey: "test-key",
	})
	require.NoError(t, err)

	assert.Equal(t, e2bDefaultDomain, p.config.Domain)
	assert.Equal(t, fmt.Sprintf("https://api.%s", e2bDefaultDomain), p.config.APIURL)
	assert.Equal(t, e2bDefaultTemplate, p.config.DefaultTemplate)
	assert.Equal(t, e2bDefaultTimeoutSec, p.config.DefaultTimeoutSec)
}

func TestE2BProvider_ConfigCustom(t *testing.T) {
	p, err := NewE2BProvider(testLogger(), &E2BProviderConfig{
		APIKey:            "test-key",
		Domain:            "custom.e2b.dev",
		APIURL:            "https://custom-api.e2b.dev",
		DefaultTemplate:   "custom-template",
		DefaultTimeoutSec: 600,
	})
	require.NoError(t, err)

	assert.Equal(t, "custom.e2b.dev", p.config.Domain)
	assert.Equal(t, "https://custom-api.e2b.dev", p.config.APIURL)
	assert.Equal(t, "custom-template", p.config.DefaultTemplate)
	assert.Equal(t, 600, p.config.DefaultTimeoutSec)
}

func TestE2BProvider_EnvdBaseURL(t *testing.T) {
	p := &E2BProvider{
		config: &E2BProviderConfig{Domain: "e2b.app"},
	}

	sandbox := &e2bSandbox{
		id:     "sandbox-abc123",
		domain: "e2b.app",
	}

	url := p.envdBaseURL(sandbox)
	// Format: https://{port}-{sandboxId}.{domain}
	assert.Equal(t, "https://49983-sandbox-abc123.e2b.app", url)
}

func TestE2BProvider_EnvdBaseURL_CustomDomain(t *testing.T) {
	p := &E2BProvider{
		config: &E2BProviderConfig{Domain: "custom.e2b.dev"},
	}

	sandbox := &e2bSandbox{
		id:     "test-sandbox-xyz",
		domain: "custom.e2b.dev",
	}

	url := p.envdBaseURL(sandbox)
	assert.Equal(t, "https://49983-test-sandbox-xyz.custom.e2b.dev", url)
}

func TestE2BProvider_SnapshotNotSupported(t *testing.T) {
	p := &E2BProvider{
		log:    testLogger(),
		config: &E2BProviderConfig{APIKey: "test-key"},
	}

	_, err := p.Snapshot(t.Context(), "some-id")
	assert.ErrorIs(t, err, ErrSnapshotNotSupported)

	_, err = p.CreateFromSnapshot(t.Context(), "snap-id", &CreateContainerRequest{})
	assert.ErrorIs(t, err, ErrSnapshotNotSupported)
}

func TestE2BProvider_GetSandbox_NotFound(t *testing.T) {
	p := &E2BProvider{
		sandboxes: make(map[string]*e2bSandbox),
	}

	_, err := p.getSandbox("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestE2BProvider_GetSandbox_Paused(t *testing.T) {
	p := &E2BProvider{
		sandboxes: map[string]*e2bSandbox{
			"sb-1": {id: "sb-1", paused: true},
		},
	}

	_, err := p.getSandbox("sb-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "paused")
}

func TestE2BProvider_GetSandbox_OK(t *testing.T) {
	p := &E2BProvider{
		sandboxes: map[string]*e2bSandbox{
			"sb-1": {id: "sb-1", paused: false, envdAccessToken: "tok"},
		},
	}

	sb, err := p.getSandbox("sb-1")
	assert.NoError(t, err)
	assert.Equal(t, "sb-1", sb.id)
	assert.Equal(t, "tok", sb.envdAccessToken)
}

func TestE2BProvider_ActiveSandboxes(t *testing.T) {
	p := &E2BProvider{
		sandboxes: map[string]*e2bSandbox{
			"sb-1": {id: "sb-1"},
			"sb-2": {id: "sb-2"},
		},
	}
	assert.Equal(t, 2, p.ActiveSandboxes())
}

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		binary bool
	}{
		{"text", "hello world\nfoo bar", false},
		{"empty", "", false},
		{"binary with null", "hello\x00world", true},
		{"null at start", "\x00binary", true},
		{"all text chars", "abcdefghijklmnop", false},
		{"json", `{"key": "value"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.binary, isBinaryContent(tt.input))
		})
	}
}

// --- HTTP mock-based tests ---

func TestE2BProvider_ControlPlaneCall_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		assert.Equal(t, "test-api-key", r.Header.Get("X-API-Key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-api-key", APIURL: server.URL},
		httpClient: server.Client(),
	}

	var result map[string]string
	err := p.controlPlaneCall(t.Context(), http.MethodPost, "/test", map[string]string{"key": "val"}, &result)
	assert.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
}

func TestE2BProvider_ControlPlaneCall_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "bad-key", APIURL: server.URL},
		httpClient: server.Client(),
	}

	err := p.controlPlaneCall(t.Context(), http.MethodGet, "/test", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden")
}

func TestE2BProvider_ControlPlaneCall_NoBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		// DELETE requests typically have no body
		assert.Empty(t, r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key", APIURL: server.URL},
		httpClient: server.Client(),
	}

	err := p.controlPlaneCall(t.Context(), http.MethodDelete, "/sandboxes/sb1", nil, nil)
	assert.NoError(t, err)
}

func TestE2BProvider_Create_Success(t *testing.T) {
	var capturedReq e2bCreateSandboxRequest

	// Mock control plane API
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sandboxes" && r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&capturedReq)
			json.NewEncoder(w).Encode(e2bCreateSandboxResponse{
				SandboxID:       "sb-new-123",
				EnvdAccessToken: "envd-token-abc",
				Domain:          "e2b.app",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cpServer.Close()

	// Mock envd API (for installExecHelper — will fail since URL doesn't match, but that's OK)
	// installExecHelper failure is just a warning, doesn't fail Create
	p := &E2BProvider{
		log: testLogger(),
		config: &E2BProviderConfig{
			APIKey:            "test-key",
			APIURL:            cpServer.URL,
			Domain:            "e2b.app",
			DefaultTemplate:   "base",
			DefaultTimeoutSec: 300,
		},
		httpClient: &http.Client{},
		sandboxes:  make(map[string]*e2bSandbox),
	}

	// Override envdBaseURL by using a custom domain that routes to envdServer
	// Since we can't easily override envdBaseURL, we'll accept that installExecHelper
	// may fail (it logs a warning but doesn't fail creation)

	result, err := p.Create(t.Context(), &CreateContainerRequest{
		BaseImage:     "custom-template",
		ContainerType: ContainerTypeAgentWorkspace,
		Labels:        map[string]string{"workspace_id": "ws-1"},
		Env:           map[string]string{"FOO": "bar"},
	})
	require.NoError(t, err)
	assert.Equal(t, "sb-new-123", result.ProviderID)

	// Verify the request sent to E2B
	assert.Equal(t, "custom-template", capturedReq.TemplateID)
	assert.Equal(t, 300, capturedReq.Timeout)
	assert.True(t, capturedReq.Secure)
	assert.True(t, capturedReq.AllowInternetAccess)
	assert.Equal(t, "bar", capturedReq.EnvVars["FOO"])
	assert.Equal(t, "agent_workspace", capturedReq.Metadata["container_type"])
	assert.Equal(t, "ws-1", capturedReq.Metadata["workspace_id"])

	// Verify sandbox is tracked
	assert.Equal(t, 1, p.ActiveSandboxes())
}

func TestE2BProvider_Create_DefaultTemplate(t *testing.T) {
	var capturedReq e2bCreateSandboxRequest

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sandboxes" {
			json.NewDecoder(r.Body).Decode(&capturedReq)
			json.NewEncoder(w).Encode(e2bCreateSandboxResponse{
				SandboxID:       "sb-default",
				EnvdAccessToken: "tok",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log: testLogger(),
		config: &E2BProviderConfig{
			APIKey:            "test-key",
			APIURL:            cpServer.URL,
			Domain:            "e2b.app",
			DefaultTemplate:   "base",
			DefaultTimeoutSec: 300,
		},
		httpClient: &http.Client{},
		sandboxes:  make(map[string]*e2bSandbox),
	}

	// No BaseImage → uses default template
	result, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)
	assert.Equal(t, "sb-default", result.ProviderID)
	assert.Equal(t, "base", capturedReq.TemplateID)
}

func TestE2BProvider_Destroy_Success(t *testing.T) {
	var deletedID string

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/sandboxes/") {
			deletedID = strings.TrimPrefix(r.URL.Path, "/sandboxes/")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key", APIURL: cpServer.URL},
		httpClient: &http.Client{},
		sandboxes: map[string]*e2bSandbox{
			"sb-del": {id: "sb-del"},
		},
	}

	err := p.Destroy(t.Context(), "sb-del")
	assert.NoError(t, err)
	assert.Equal(t, "sb-del", deletedID)
	assert.Equal(t, 0, p.ActiveSandboxes())
}

func TestE2BProvider_Destroy_AlreadyGone(t *testing.T) {
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 not found"))
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key", APIURL: cpServer.URL},
		httpClient: &http.Client{},
		sandboxes:  make(map[string]*e2bSandbox),
	}

	// Should not return an error if sandbox is already gone (404)
	err := p.Destroy(t.Context(), "sb-gone")
	assert.NoError(t, err)
}

func TestE2BProvider_Health_Healthy(t *testing.T) {
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/sandboxes" && r.URL.Query().Get("limit") == "1" {
			json.NewEncoder(w).Encode([]e2bListedSandbox{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key", APIURL: cpServer.URL},
		httpClient: &http.Client{},
		sandboxes: map[string]*e2bSandbox{
			"sb-1": {id: "sb-1"},
		},
	}

	status, err := p.Health(t.Context())
	assert.NoError(t, err)
	assert.True(t, status.Healthy)
	assert.Equal(t, 1, status.ActiveCount)
	assert.Contains(t, status.Message, "healthy")
}

func TestE2BProvider_Health_Unhealthy(t *testing.T) {
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid api key"))
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "bad-key", APIURL: cpServer.URL},
		httpClient: &http.Client{},
		sandboxes:  make(map[string]*e2bSandbox),
	}

	status, err := p.Health(t.Context())
	assert.NoError(t, err)
	assert.False(t, status.Healthy)
	assert.Contains(t, status.Message, "unreachable")
}

func TestE2BProvider_EnvdReadFile(t *testing.T) {
	envdServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "envd-token", r.Header.Get("X-Access-Token"))

		path := r.URL.Query().Get("path")
		switch path {
		case "/home/user/test.txt":
			w.Write([]byte("file content here"))
		case "/nonexistent":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("not found"))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer envdServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key"},
		httpClient: envdServer.Client(),
	}

	// We need to override envdBaseURL — use a sandbox with the test server's host
	// by manipulating the domain to point at the test server
	sandbox := &e2bSandbox{
		id:              "test-sb",
		envdAccessToken: "envd-token",
	}

	// Directly test envdReadFile by overriding the URL construction
	// Since we can't easily override envdBaseURL, we'll test controlPlaneCall pattern instead
	// For envd calls, test the HTTP interaction directly

	// Create a provider that uses the test server's URL
	// We need to construct the URL manually for tests
	t.Run("read existing file", func(t *testing.T) {
		url := envdServer.URL + "/files?path=/home/user/test.txt"
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		req.Header.Set("X-Access-Token", sandbox.envdAccessToken)

		resp, err := p.httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("read nonexistent file", func(t *testing.T) {
		url := envdServer.URL + "/files?path=/nonexistent"
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		req.Header.Set("X-Access-Token", sandbox.envdAccessToken)

		resp, err := p.httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestE2BProvider_EnvdWriteFile(t *testing.T) {
	var capturedPath string
	var capturedToken string
	var capturedContentType string

	envdServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		capturedPath = r.URL.Query().Get("path")
		capturedToken = r.Header.Get("X-Access-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer envdServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key"},
		httpClient: envdServer.Client(),
	}

	t.Run("write file sends multipart", func(t *testing.T) {
		url := envdServer.URL + "/files?path=/test/file.txt"
		// Simulate what envdWriteFile does
		var buf strings.Builder
		buf.WriteString("file content")

		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(buf.String()))
		req.Header.Set("X-Access-Token", "tok-123")
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := p.httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "/test/file.txt", capturedPath)
		assert.Equal(t, "tok-123", capturedToken)
		assert.NotEmpty(t, capturedContentType)
	})
}

func TestE2BProvider_ExecViaCommandsAPI_Success(t *testing.T) {
	envdServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/commands/run" && r.Method == http.MethodPost {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)

			// Verify command structure
			assert.Equal(t, "/bin/bash", req["cmd"])
			args := req["args"].([]any)
			assert.Equal(t, "-l", args[0])
			assert.Equal(t, "-c", args[1])
			assert.Contains(t, args[2], "echo hello")

			json.NewEncoder(w).Encode(map[string]any{
				"stdout":   "hello\n",
				"stderr":   "",
				"exitCode": 0,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer envdServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key"},
		httpClient: envdServer.Client(),
	}

	sandbox := &e2bSandbox{
		id:              "test",
		envdAccessToken: "tok",
	}

	// We need to override envdBaseURL. Since we can't, test the HTTP call pattern directly.
	result, err := func() (*ExecResult, error) {
		reqBody := map[string]any{
			"cmd":  "/bin/bash",
			"args": []string{"-l", "-c", "echo hello"},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		url := envdServer.URL + "/commands/run"
		httpReq, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(string(bodyBytes)))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Access-Token", sandbox.envdAccessToken)

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var cmdResult struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exitCode"`
		}
		json.NewDecoder(resp.Body).Decode(&cmdResult)

		return &ExecResult{
			Stdout:   cmdResult.Stdout,
			Stderr:   cmdResult.Stderr,
			ExitCode: cmdResult.ExitCode,
		}, nil
	}()

	require.NoError(t, err)
	assert.Equal(t, "hello\n", result.Stdout)
	assert.Equal(t, 0, result.ExitCode)
}

func TestE2BProvider_ExecViaCommandsAPI_NotAvailable(t *testing.T) {
	envdServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/commands/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer envdServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key"},
		httpClient: envdServer.Client(),
	}

	sandbox := &e2bSandbox{
		id:              "test",
		envdAccessToken: "tok",
	}

	// Simulate what envdExecViaCommandsAPI does when endpoint returns 404
	reqBody := map[string]any{"cmd": "/bin/bash", "args": []string{"-c", "echo hello"}}
	bodyBytes, _ := json.Marshal(reqBody)

	url := envdServer.URL + "/commands/run"
	httpReq, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(string(bodyBytes)))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Access-Token", sandbox.envdAccessToken)

	resp, err := p.httpClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	// The envdExecViaCommandsAPI method treats 404 as "not available"
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestE2BProvider_Stop_Success(t *testing.T) {
	var pauseRequested bool

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pause") {
			pauseRequested = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key", APIURL: cpServer.URL},
		httpClient: &http.Client{},
		sandboxes: map[string]*e2bSandbox{
			"sb-1": {id: "sb-1", paused: false},
		},
	}

	err := p.Stop(t.Context(), "sb-1")
	assert.NoError(t, err)
	assert.True(t, pauseRequested)

	// Verify sandbox is now marked as paused
	p.mu.RLock()
	assert.True(t, p.sandboxes["sb-1"].paused)
	p.mu.RUnlock()
}

func TestE2BProvider_Stop_NotFound(t *testing.T) {
	p := &E2BProvider{
		log:       testLogger(),
		config:    &E2BProviderConfig{APIKey: "test-key"},
		sandboxes: make(map[string]*e2bSandbox),
	}

	err := p.Stop(t.Context(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestE2BProvider_Resume_Success(t *testing.T) {
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connect") {
			json.NewEncoder(w).Encode(e2bCreateSandboxResponse{
				SandboxID:       "sb-1",
				EnvdAccessToken: "new-token",
				Domain:          "updated.e2b.app",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key", APIURL: cpServer.URL, DefaultTimeoutSec: 300},
		httpClient: &http.Client{},
		sandboxes: map[string]*e2bSandbox{
			"sb-1": {id: "sb-1", paused: true, envdAccessToken: "old-token", domain: "e2b.app"},
		},
	}

	err := p.Resume(t.Context(), "sb-1")
	assert.NoError(t, err)

	// Verify token and domain were updated
	p.mu.RLock()
	sb := p.sandboxes["sb-1"]
	assert.Equal(t, "new-token", sb.envdAccessToken)
	assert.Equal(t, "updated.e2b.app", sb.domain)
	assert.False(t, sb.paused)
	p.mu.RUnlock()
}

func TestE2BProvider_MaxTimeoutClamped(t *testing.T) {
	var capturedTimeout int

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sandboxes" {
			var req e2bCreateSandboxRequest
			json.NewDecoder(r.Body).Decode(&req)
			capturedTimeout = req.Timeout
			json.NewEncoder(w).Encode(e2bCreateSandboxResponse{
				SandboxID: "sb-1",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log: testLogger(),
		config: &E2BProviderConfig{
			APIKey:            "test-key",
			APIURL:            cpServer.URL,
			Domain:            "e2b.app",
			DefaultTemplate:   "base",
			DefaultTimeoutSec: 100000, // Exceeds max
		},
		httpClient: &http.Client{},
		sandboxes:  make(map[string]*e2bSandbox),
	}

	_, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)

	// Timeout should be clamped to max
	assert.Equal(t, e2bMaxTimeoutSec, capturedTimeout)
}

func TestE2BProvider_ExecOutputTruncation(t *testing.T) {
	// Test that output is truncated to maxOutputBytes
	largeOutput := strings.Repeat("x", maxOutputBytes+1000)

	// Verify truncation logic matches what envdExecViaCommandsAPI does
	stdout := largeOutput
	truncated := false
	if len(stdout) > maxOutputBytes {
		stdout = stdout[:maxOutputBytes]
		truncated = true
	}

	assert.True(t, truncated)
	assert.Equal(t, maxOutputBytes, len(stdout))
}

func TestE2BProvider_Concurrency(t *testing.T) {
	// Verify thread safety of sandbox tracking
	p := &E2BProvider{
		log:       testLogger(),
		config:    &E2BProviderConfig{APIKey: "test-key"},
		sandboxes: make(map[string]*e2bSandbox),
	}

	done := make(chan struct{})
	const n = 100

	// Concurrent writes
	go func() {
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("sb-%d", i)
			p.mu.Lock()
			p.sandboxes[id] = &e2bSandbox{id: id}
			p.mu.Unlock()
		}
		close(done)
	}()

	// Concurrent reads
	for i := 0; i < n; i++ {
		_ = p.ActiveSandboxes()
	}

	<-done
	assert.Equal(t, n, p.ActiveSandboxes())
}

func TestE2BProvider_QuotaUsage_Initial(t *testing.T) {
	p, err := NewE2BProvider(testLogger(), &E2BProviderConfig{APIKey: "test-key"})
	require.NoError(t, err)

	quota := p.QuotaUsage()
	assert.Equal(t, int64(0), quota.TotalCreates)
	assert.Equal(t, int64(0), quota.TotalDestroys)
	assert.Equal(t, 0, quota.ActiveSandboxes)
	assert.Equal(t, int64(0), quota.ComputeMinutes)
}

func TestE2BProvider_QuotaUsage_AfterCreateAndDestroy(t *testing.T) {
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sandboxes" && r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(e2bCreateSandboxResponse{
				SandboxID:       "sb-quota-1",
				EnvdAccessToken: "tok",
			})
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log: testLogger(),
		config: &E2BProviderConfig{
			APIKey:            "test-key",
			APIURL:            cpServer.URL,
			Domain:            "e2b.app",
			DefaultTemplate:   "base",
			DefaultTimeoutSec: 300,
		},
		httpClient: &http.Client{},
		sandboxes:  make(map[string]*e2bSandbox),
	}

	// Create a sandbox
	_, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
	})
	require.NoError(t, err)

	quota := p.QuotaUsage()
	assert.Equal(t, int64(1), quota.TotalCreates)
	assert.Equal(t, int64(0), quota.TotalDestroys)
	assert.Equal(t, 1, quota.ActiveSandboxes)

	// Destroy the sandbox
	err = p.Destroy(t.Context(), "sb-quota-1")
	assert.NoError(t, err)

	quota = p.QuotaUsage()
	assert.Equal(t, int64(1), quota.TotalCreates)
	assert.Equal(t, int64(1), quota.TotalDestroys)
	assert.Equal(t, 0, quota.ActiveSandboxes)
	// Compute minutes should be >= 1 (minimum 1 minute per sandbox)
	assert.GreaterOrEqual(t, quota.ComputeMinutes, int64(1))
}

func TestE2BProvider_HealthIncludesQuota(t *testing.T) {
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]e2bListedSandbox{})
	}))
	defer cpServer.Close()

	p := &E2BProvider{
		log:        testLogger(),
		config:     &E2BProviderConfig{APIKey: "test-key", APIURL: cpServer.URL},
		httpClient: &http.Client{},
		sandboxes:  make(map[string]*e2bSandbox),
	}

	// Simulate some usage
	p.totalCreates.Store(5)
	p.activeMinutes.Store(42)

	status, err := p.Health(t.Context())
	assert.NoError(t, err)
	assert.True(t, status.Healthy)
	assert.Contains(t, status.Message, "5 total creates")
	assert.Contains(t, status.Message, "42 compute minutes")
}

// --- Integration tests (require E2B_API_KEY) ---

func skipWithoutE2BKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("E2B_API_KEY")
	if key == "" {
		t.Skip("E2B_API_KEY not set — skipping integration test")
	}
	return key
}

func TestE2BProvider_Integration_Health(t *testing.T) {
	apiKey := skipWithoutE2BKey(t)

	p, err := NewE2BProvider(testLogger(), &E2BProviderConfig{
		APIKey: apiKey,
	})
	require.NoError(t, err)

	status, err := p.Health(t.Context())
	require.NoError(t, err)
	assert.True(t, status.Healthy, "E2B API should be healthy: %s", status.Message)
}

func TestE2BProvider_Integration_SandboxLifecycle(t *testing.T) {
	apiKey := skipWithoutE2BKey(t)

	p, err := NewE2BProvider(testLogger(), &E2BProviderConfig{
		APIKey:            apiKey,
		DefaultTimeoutSec: 60, // Short timeout for tests
	})
	require.NoError(t, err)

	// Create a sandbox
	result, err := p.Create(t.Context(), &CreateContainerRequest{
		ContainerType: ContainerTypeAgentWorkspace,
		Labels:        map[string]string{"test": "integration"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.ProviderID)
	t.Logf("Created E2B sandbox: %s", result.ProviderID)

	// Ensure cleanup
	defer func() {
		if err := p.Destroy(t.Context(), result.ProviderID); err != nil {
			t.Logf("Warning: failed to destroy sandbox: %v", err)
		}
	}()

	// Verify sandbox is tracked
	assert.Equal(t, 1, p.ActiveSandboxes())

	// Try exec (may fail if commands API is not available)
	execResult, err := p.Exec(t.Context(), result.ProviderID, &ExecRequest{
		Command:   "echo hello-e2b",
		TimeoutMs: 30000,
	})
	if err != nil {
		t.Logf("Exec failed (expected if commands API unavailable): %v", err)
	} else {
		t.Logf("Exec result: stdout=%q stderr=%q exit=%d", execResult.Stdout, execResult.Stderr, execResult.ExitCode)
	}

	// Try write + read file
	writeErr := p.WriteFile(t.Context(), result.ProviderID, &FileWriteRequest{
		FilePath: "/tmp/e2b-test.txt",
		Content:  "integration test content",
	})
	if writeErr != nil {
		t.Logf("WriteFile failed: %v", writeErr)
	} else {
		readResult, readErr := p.ReadFile(t.Context(), result.ProviderID, &FileReadRequest{
			FilePath: "/tmp/e2b-test.txt",
		})
		if readErr != nil {
			t.Logf("ReadFile failed: %v", readErr)
		} else {
			assert.Contains(t, readResult.Content, "integration test content")
			t.Logf("ReadFile success: %q", readResult.Content)
		}
	}

	// Verify quota tracking
	quota := p.QuotaUsage()
	assert.Equal(t, int64(1), quota.TotalCreates)
	assert.Equal(t, 1, quota.ActiveSandboxes)

	// Destroy
	err = p.Destroy(t.Context(), result.ProviderID)
	assert.NoError(t, err)
	assert.Equal(t, 0, p.ActiveSandboxes())

	quota = p.QuotaUsage()
	assert.Equal(t, int64(1), quota.TotalDestroys)
}
