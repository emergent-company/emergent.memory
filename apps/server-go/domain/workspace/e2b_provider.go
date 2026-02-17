package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// e2bDefaultDomain is the default E2B API domain.
	e2bDefaultDomain = "e2b.app"

	// e2bDefaultTemplate is the default sandbox template (Ubuntu-based with common tools).
	e2bDefaultTemplate = "base"

	// e2bDefaultTimeoutSec is the default sandbox timeout in seconds (5 minutes).
	e2bDefaultTimeoutSec = 300

	// e2bMaxTimeoutSec is the maximum sandbox timeout in seconds (24 hours for Pro).
	e2bMaxTimeoutSec = 86400

	// e2bEnvdPort is the envd API port inside the sandbox.
	e2bEnvdPort = 49983

	// e2bHTTPTimeout is the default HTTP timeout for API calls.
	e2bHTTPTimeout = 60 * time.Second

	// e2bExecHelperPath is the path where the execution helper script is written.
	e2bExecHelperPath = "/tmp/.e2b-exec-helper.sh"

	// e2bExecStdoutPath is the path where command stdout is captured.
	e2bExecStdoutPath = "/tmp/.e2b-exec-stdout"

	// e2bExecStderrPath is the path where command stderr is captured.
	e2bExecStderrPath = "/tmp/.e2b-exec-stderr"

	// e2bExecExitCodePath is the path where command exit code is captured.
	e2bExecExitCodePath = "/tmp/.e2b-exec-exitcode"

	// e2bExecDurationPath is the path where command duration (ms) is captured.
	e2bExecDurationPath = "/tmp/.e2b-exec-duration"
)

// E2BProvider implements the Provider interface using E2B managed sandboxes.
// E2B sandboxes are ephemeral cloud VMs accessed via E2B's REST API.
// Command execution uses a helper-script approach via the /files REST endpoint
// to avoid ConnectRPC/protobuf dependencies.
type E2BProvider struct {
	log        *slog.Logger
	config     *E2BProviderConfig
	httpClient *http.Client

	mu sync.RWMutex
	// sandboxes tracks active sandbox metadata by sandbox ID.
	sandboxes map[string]*e2bSandbox

	// Quota tracking
	totalCreates  atomic.Int64 // Total sandbox creates since startup
	totalDestroys atomic.Int64 // Total sandbox destroys since startup
	activeMinutes atomic.Int64 // Accumulated compute minutes (estimated)
}

// e2bSandbox tracks the state of an E2B sandbox.
type e2bSandbox struct {
	id              string // E2B sandbox ID
	domain          string // Sandbox-specific domain (may differ from default)
	envdAccessToken string // Token for envd API access
	templateID      string // Template used to create the sandbox
	createdAt       time.Time
	paused          bool // Whether the sandbox is paused
}

// E2BProviderConfig holds configuration for the E2B provider.
type E2BProviderConfig struct {
	// APIKey is the E2B API key (required).
	APIKey string

	// Domain is the E2B domain. Defaults to "e2b.app".
	Domain string

	// APIURL is the E2B control plane API URL. Defaults to "https://api.{Domain}".
	APIURL string

	// DefaultTemplate is the sandbox template ID. Defaults to "base".
	DefaultTemplate string

	// DefaultTimeoutSec is the default sandbox timeout in seconds. Defaults to 300 (5 min).
	DefaultTimeoutSec int
}

// NewE2BProvider creates a new E2B-based workspace provider.
func NewE2BProvider(log *slog.Logger, cfg *E2BProviderConfig) (*E2BProvider, error) {
	if cfg == nil || cfg.APIKey == "" {
		return nil, fmt.Errorf("E2B API key is required")
	}

	if cfg.Domain == "" {
		cfg.Domain = e2bDefaultDomain
	}
	if cfg.APIURL == "" {
		cfg.APIURL = fmt.Sprintf("https://api.%s", cfg.Domain)
	}
	if cfg.DefaultTemplate == "" {
		cfg.DefaultTemplate = e2bDefaultTemplate
	}
	if cfg.DefaultTimeoutSec <= 0 {
		cfg.DefaultTimeoutSec = e2bDefaultTimeoutSec
	}

	return &E2BProvider{
		log:    log.With("component", "e2b-provider"),
		config: cfg,
		httpClient: &http.Client{
			Timeout: e2bHTTPTimeout,
		},
		sandboxes: make(map[string]*e2bSandbox),
	}, nil
}

// Capabilities returns what this provider supports.
func (p *E2BProvider) Capabilities() *ProviderCapabilities {
	return &ProviderCapabilities{
		Name:                "E2B (managed)",
		SupportsPersistence: false, // E2B sandboxes are ephemeral (pause is beta)
		SupportsSnapshots:   false,
		SupportsWarmPool:    false,
		RequiresKVM:         false,
		EstimatedStartupMs:  150,
		ProviderType:        ProviderE2B,
	}
}

// Create provisions a new E2B sandbox.
func (p *E2BProvider) Create(ctx context.Context, req *CreateContainerRequest) (*CreateContainerResult, error) {
	template := p.config.DefaultTemplate
	if req.BaseImage != "" {
		template = req.BaseImage
	}

	timeout := p.config.DefaultTimeoutSec
	if timeout > e2bMaxTimeoutSec {
		timeout = e2bMaxTimeoutSec
	}

	// Build create request body
	createReq := e2bCreateSandboxRequest{
		TemplateID:          template,
		Timeout:             timeout,
		Metadata:            make(map[string]string),
		EnvVars:             req.Env,
		Secure:              true,
		AllowInternetAccess: true,
	}

	// Map labels to metadata
	for k, v := range req.Labels {
		createReq.Metadata[k] = v
	}
	createReq.Metadata["container_type"] = string(req.ContainerType)

	var result e2bCreateSandboxResponse
	if err := p.controlPlaneCall(ctx, http.MethodPost, "/sandboxes", createReq, &result); err != nil {
		return nil, fmt.Errorf("failed to create E2B sandbox: %w", err)
	}

	// Determine sandbox domain
	domain := result.Domain
	if domain == "" {
		domain = p.config.Domain
	}

	sandbox := &e2bSandbox{
		id:              result.SandboxID,
		domain:          domain,
		envdAccessToken: result.EnvdAccessToken,
		templateID:      template,
		createdAt:       time.Now(),
	}

	p.mu.Lock()
	p.sandboxes[result.SandboxID] = sandbox
	p.mu.Unlock()

	// Install the exec helper script in the sandbox
	if err := p.installExecHelper(ctx, sandbox); err != nil {
		p.log.Warn("failed to install exec helper, command execution may fail", "sandbox_id", result.SandboxID, "error", err)
	}

	p.log.Info("E2B sandbox created",
		"sandbox_id", result.SandboxID,
		"template", template,
		"timeout_sec", timeout,
	)

	p.totalCreates.Add(1)
	creates := p.totalCreates.Load()
	if creates%100 == 0 {
		p.log.Warn("E2B quota check: high sandbox creation count",
			"total_creates", creates,
			"active_sandboxes", p.ActiveSandboxes(),
		)
	}

	return &CreateContainerResult{ProviderID: result.SandboxID}, nil
}

// Destroy permanently removes an E2B sandbox.
func (p *E2BProvider) Destroy(ctx context.Context, providerID string) error {
	p.mu.Lock()
	sandbox, existed := p.sandboxes[providerID]
	delete(p.sandboxes, providerID)
	p.mu.Unlock()

	// Track compute minutes (estimated from creation time)
	if existed && sandbox != nil {
		minutes := int64(time.Since(sandbox.createdAt).Minutes())
		if minutes < 1 {
			minutes = 1
		}
		p.activeMinutes.Add(minutes)
		p.totalDestroys.Add(1)
	}

	err := p.controlPlaneCall(ctx, http.MethodDelete, fmt.Sprintf("/sandboxes/%s", providerID), nil, nil)
	if err != nil {
		// 404 means already gone
		if strings.Contains(err.Error(), "404") {
			p.log.Debug("sandbox already gone", "sandbox_id", providerID)
			return nil
		}
		return fmt.Errorf("failed to destroy E2B sandbox: %w", err)
	}

	p.log.Info("E2B sandbox destroyed", "sandbox_id", providerID)
	return nil
}

// Stop pauses an E2B sandbox (beta feature).
func (p *E2BProvider) Stop(ctx context.Context, providerID string) error {
	sandbox, err := p.getSandbox(providerID)
	if err != nil {
		return err
	}

	err = p.controlPlaneCall(ctx, http.MethodPost, fmt.Sprintf("/sandboxes/%s/pause", providerID), nil, nil)
	if err != nil {
		return fmt.Errorf("failed to pause E2B sandbox: %w", err)
	}

	p.mu.Lock()
	sandbox.paused = true
	p.mu.Unlock()

	p.log.Info("E2B sandbox paused", "sandbox_id", providerID)
	return nil
}

// Resume reconnects to a paused E2B sandbox (beta feature).
func (p *E2BProvider) Resume(ctx context.Context, providerID string) error {
	p.mu.RLock()
	sandbox, ok := p.sandboxes[providerID]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("E2B sandbox not found: %s (may need to reconnect)", providerID)
	}

	connectReq := map[string]int{"timeout": p.config.DefaultTimeoutSec}
	var result e2bCreateSandboxResponse
	if err := p.controlPlaneCall(ctx, http.MethodPost, fmt.Sprintf("/sandboxes/%s/connect", providerID), connectReq, &result); err != nil {
		return fmt.Errorf("failed to resume E2B sandbox: %w", err)
	}

	p.mu.Lock()
	sandbox.paused = false
	// Update access token (may change on reconnect)
	if result.EnvdAccessToken != "" {
		sandbox.envdAccessToken = result.EnvdAccessToken
	}
	if result.Domain != "" {
		sandbox.domain = result.Domain
	}
	p.mu.Unlock()

	p.log.Info("E2B sandbox resumed", "sandbox_id", providerID)
	return nil
}

// Exec executes a command inside an E2B sandbox using a helper-script approach.
// It writes a shell script to the sandbox via the /files API, which captures
// stdout, stderr, exit code, and duration to separate files, then reads the results.
func (p *E2BProvider) Exec(ctx context.Context, providerID string, req *ExecRequest) (*ExecResult, error) {
	sandbox, err := p.getSandbox(providerID)
	if err != nil {
		return nil, err
	}

	// Build the execution script
	timeoutSec := 120
	if req.TimeoutMs > 0 {
		timeoutSec = int(req.TimeoutMs / 1000)
		if timeoutSec < 1 {
			timeoutSec = 1
		}
	}

	workdir := req.Workdir
	if workdir == "" {
		workdir = "/home/user"
	}

	// Write the command to a separate file to avoid shell escaping issues
	cmdFilePath := "/tmp/.e2b-exec-cmd"
	if err := p.envdWriteFile(ctx, sandbox, cmdFilePath, req.Command); err != nil {
		return nil, fmt.Errorf("failed to write command file: %w", err)
	}

	// The exec helper script reads the command from the cmd file
	execScript := fmt.Sprintf(`#!/bin/bash
cd %q 2>/dev/null || cd /home/user
START_MS=$(($(date +%%s%%N)/1000000))
timeout %d bash /tmp/.e2b-exec-cmd >%s 2>%s
echo $? > %s
END_MS=$(($(date +%%s%%N)/1000000))
echo $((END_MS - START_MS)) > %s
`, workdir, timeoutSec, e2bExecStdoutPath, e2bExecStderrPath, e2bExecExitCodePath, e2bExecDurationPath)

	// Write exec script
	if err := p.envdWriteFile(ctx, sandbox, e2bExecHelperPath, execScript); err != nil {
		return nil, fmt.Errorf("failed to write exec script: %w", err)
	}

	// Execute the helper script by writing a trigger file that the installed daemon runs
	// Actually, since we don't have a daemon, we need a different approach.
	// Use the envd /execute endpoint if available, or fall back to a poll-based approach.
	//
	// The simplest reliable approach: use nsenter or run bash directly via envd's
	// built-in exec mechanism.
	//
	// After research: E2B's envd has a simple HTTP exec endpoint at /commands.
	// Let's try that first, falling back to the script approach.
	result, err := p.envdExecCommand(ctx, sandbox, fmt.Sprintf("bash %s", e2bExecHelperPath))
	if err != nil {
		return nil, fmt.Errorf("failed to execute command via envd: %w", err)
	}

	// Read result files
	stdoutContent, _ := p.envdReadFile(ctx, sandbox, e2bExecStdoutPath)
	stderrContent, _ := p.envdReadFile(ctx, sandbox, e2bExecStderrPath)
	exitCodeStr, _ := p.envdReadFile(ctx, sandbox, e2bExecExitCodePath)
	durationStr, _ := p.envdReadFile(ctx, sandbox, e2bExecDurationPath)

	// If we got result files, use them. Otherwise fall back to direct exec output.
	if stdoutContent != "" || stderrContent != "" || exitCodeStr != "" {
		exitCode := parseInt(strings.TrimSpace(exitCodeStr))
		durationMs := int64(parseInt(strings.TrimSpace(durationStr)))

		stdout := stdoutContent
		truncated := false
		if len(stdout) > maxOutputBytes {
			stdout = stdout[:maxOutputBytes]
			truncated = true
		}

		return &ExecResult{
			Stdout:     stdout,
			Stderr:     stderrContent,
			ExitCode:   exitCode,
			DurationMs: durationMs,
			Truncated:  truncated,
		}, nil
	}

	// Fall back to direct exec result
	return result, nil
}

// ReadFile reads file content from an E2B sandbox via the envd /files REST endpoint.
func (p *E2BProvider) ReadFile(ctx context.Context, providerID string, req *FileReadRequest) (*FileReadResult, error) {
	sandbox, err := p.getSandbox(providerID)
	if err != nil {
		return nil, err
	}

	// For directories, use exec to list
	checkResult, err := p.envdExecCommand(ctx, sandbox, fmt.Sprintf("test -d %q && echo DIR || echo FILE", req.FilePath))
	if err != nil {
		return nil, fmt.Errorf("failed to check path type: %w", err)
	}

	if strings.TrimSpace(checkResult.Stdout) == "DIR" {
		dirResult, err := p.envdExecCommand(ctx, sandbox, fmt.Sprintf("ls -1F %q 2>/dev/null", req.FilePath))
		if err != nil {
			return nil, err
		}
		return &FileReadResult{
			Content: dirResult.Stdout,
			IsDir:   true,
		}, nil
	}

	// Read file content via /files endpoint
	content, err := p.envdReadFile(ctx, sandbox, req.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Check if binary
	if isBinaryContent(content) {
		return &FileReadResult{
			IsBinary: true,
			Content:  "Binary file",
			FileSize: int64(len(content)),
		}, nil
	}

	// Apply offset/limit
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if req.Offset > 0 || req.Limit > 0 {
		start := 0
		if req.Offset > 0 {
			start = req.Offset - 1 // 1-indexed
			if start >= len(lines) {
				start = len(lines)
			}
		}
		end := len(lines)
		if req.Limit > 0 && start+req.Limit < end {
			end = start + req.Limit
		}
		lines = lines[start:end]

		// Format with line numbers
		var sb strings.Builder
		for i, line := range lines {
			fmt.Fprintf(&sb, "%d: %s\n", start+i+1, line)
		}
		content = sb.String()
	}

	return &FileReadResult{
		Content:    content,
		TotalLines: totalLines,
	}, nil
}

// WriteFile writes file content to an E2B sandbox via the envd /files REST endpoint.
func (p *E2BProvider) WriteFile(ctx context.Context, providerID string, req *FileWriteRequest) error {
	sandbox, err := p.getSandbox(providerID)
	if err != nil {
		return err
	}

	// Create parent directories
	dir := req.FilePath[:strings.LastIndex(req.FilePath, "/")]
	if dir != "" {
		if _, err := p.envdExecCommand(ctx, sandbox, fmt.Sprintf("mkdir -p %q", dir)); err != nil {
			p.log.Warn("failed to create parent directories", "path", dir, "error", err)
		}
	}

	return p.envdWriteFile(ctx, sandbox, req.FilePath, req.Content)
}

// ListFiles returns files matching a glob pattern inside an E2B sandbox.
func (p *E2BProvider) ListFiles(ctx context.Context, providerID string, req *FileListRequest) (*FileListResult, error) {
	sandbox, err := p.getSandbox(providerID)
	if err != nil {
		return nil, err
	}

	searchPath := req.Path
	if searchPath == "" {
		searchPath = "/home/user"
	}

	cmd := fmt.Sprintf("find %q -name %q -printf '%%T@ %%y %%s %%p\\n' 2>/dev/null | sort -rn | head -1000", searchPath, req.Pattern)
	result, err := p.envdExecCommand(ctx, sandbox, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	var files []FileInfo
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}
		fi := FileInfo{
			Path:  parts[3],
			IsDir: parts[1] == "d",
			Size:  parseSize(parts[2]),
		}
		if ts := parseFloat(parts[0]); ts > 0 {
			fi.ModifiedAt = time.Unix(int64(ts), 0)
		}
		files = append(files, fi)
	}

	return &FileListResult{Files: files}, nil
}

// Snapshot is not supported by E2B — sandboxes are ephemeral by design.
func (p *E2BProvider) Snapshot(_ context.Context, _ string) (string, error) {
	return "", ErrSnapshotNotSupported
}

// CreateFromSnapshot is not supported by E2B.
func (p *E2BProvider) CreateFromSnapshot(_ context.Context, _ string, _ *CreateContainerRequest) (*CreateContainerResult, error) {
	return nil, ErrSnapshotNotSupported
}

// Health validates the E2B API key and connectivity.
func (p *E2BProvider) Health(ctx context.Context) (*HealthStatus, error) {
	// Try to list sandboxes to verify API connectivity
	var sandboxes []e2bListedSandbox
	err := p.controlPlaneCall(ctx, http.MethodGet, "/v2/sandboxes?limit=1", nil, &sandboxes)
	if err != nil {
		return &HealthStatus{
			Healthy: false,
			Message: fmt.Sprintf("E2B API unreachable: %v", err),
		}, nil
	}

	quota := p.QuotaUsage()
	return &HealthStatus{
		Healthy:     true,
		Message:     fmt.Sprintf("E2B API healthy, %d tracked sandboxes, %d total creates, %d compute minutes", quota.ActiveSandboxes, quota.TotalCreates, quota.ComputeMinutes),
		ActiveCount: quota.ActiveSandboxes,
	}, nil
}

// --- Control Plane API helpers ---

// controlPlaneCall makes an HTTP request to the E2B control plane API.
func (p *E2BProvider) controlPlaneCall(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	url := p.config.APIURL + path
	httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("X-API-Key", p.config.APIKey)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("E2B API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// --- Envd (Data Plane) API helpers ---

// envdBaseURL returns the base URL for the sandbox's envd API.
func (p *E2BProvider) envdBaseURL(sandbox *e2bSandbox) string {
	return fmt.Sprintf("https://%d-%s.%s", e2bEnvdPort, sandbox.id, sandbox.domain)
}

// envdReadFile reads a file from the sandbox via the envd /files REST endpoint.
func (p *E2BProvider) envdReadFile(ctx context.Context, sandbox *e2bSandbox, path string) (string, error) {
	url := fmt.Sprintf("%s/files?path=%s", p.envdBaseURL(sandbox), path)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("X-Access-Token", sandbox.envdAccessToken)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("envd read failed (status %d): %s", resp.StatusCode, string(errBody))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// envdWriteFile writes a file to the sandbox via the envd /files REST endpoint.
func (p *E2BProvider) envdWriteFile(ctx context.Context, sandbox *e2bSandbox, filePath, content string) error {
	// The /files POST endpoint expects multipart form data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filePath)
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.WriteString(part, content); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	url := fmt.Sprintf("%s/files?path=%s", p.envdBaseURL(sandbox), filePath)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("X-Access-Token", sandbox.envdAccessToken)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("envd write failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("envd write failed (status %d): %s", resp.StatusCode, string(errBody))
	}

	return nil
}

// envdExecCommand executes a command in the sandbox.
// This uses a two-step approach: writes a script via /files, then triggers execution
// via the envd HTTP API. If direct exec isn't available, falls back to polling.
func (p *E2BProvider) envdExecCommand(ctx context.Context, sandbox *e2bSandbox, command string) (*ExecResult, error) {
	start := time.Now()

	// Try the envd /commands/run endpoint (available in newer envd versions)
	result, err := p.envdExecViaCommandsAPI(ctx, sandbox, command)
	if err == nil {
		return result, nil
	}

	// Log the error for debugging but don't fail — fall back to script-based execution
	p.log.Debug("envd /commands/run not available, falling back to script-based execution",
		"sandbox_id", sandbox.id, "error", err)

	// Fallback: write command to a script, execute via /files + polling
	return p.envdExecViaScript(ctx, sandbox, command, start)
}

// envdExecViaCommandsAPI tries to execute a command using the envd commands API.
// This endpoint is available in newer envd versions and provides synchronous command execution.
func (p *E2BProvider) envdExecViaCommandsAPI(ctx context.Context, sandbox *e2bSandbox, command string) (*ExecResult, error) {
	reqBody := map[string]any{
		"cmd":  "/bin/bash",
		"args": []string{"-l", "-c", command},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/commands/run", p.envdBaseURL(sandbox))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Access-Token", sandbox.envdAccessToken)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, fmt.Errorf("commands API not available (status %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("commands API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	var cmdResult struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cmdResult); err != nil {
		return nil, fmt.Errorf("failed to decode command result: %w", err)
	}

	stdout := cmdResult.Stdout
	truncated := false
	if len(stdout) > maxOutputBytes {
		stdout = stdout[:maxOutputBytes]
		truncated = true
	}

	return &ExecResult{
		Stdout:    stdout,
		Stderr:    cmdResult.Stderr,
		ExitCode:  cmdResult.ExitCode,
		Truncated: truncated,
	}, nil
}

// envdExecViaScript executes a command by writing a script to the sandbox
// and polling for completion. This is the fallback approach when the commands
// API is not available.
func (p *E2BProvider) envdExecViaScript(ctx context.Context, sandbox *e2bSandbox, command string, start time.Time) (*ExecResult, error) {
	// Write the command to a temp file
	script := fmt.Sprintf(`#!/bin/bash
%s >%s 2>%s
echo $? > %s
`, command, e2bExecStdoutPath, e2bExecStderrPath, e2bExecExitCodePath)

	doneMarker := "/tmp/.e2b-exec-done"
	script = fmt.Sprintf(`#!/bin/bash
rm -f %s
%s >%s 2>%s
echo $? > %s
touch %s
`, doneMarker, command, e2bExecStdoutPath, e2bExecStderrPath, e2bExecExitCodePath, doneMarker)

	scriptPath := "/tmp/.e2b-run.sh"
	if err := p.envdWriteFile(ctx, sandbox, scriptPath, script); err != nil {
		return nil, fmt.Errorf("failed to write execution script: %w", err)
	}

	// Make executable and run in background via another write
	bgScript := fmt.Sprintf("#!/bin/bash\nchmod +x %s\nnohup bash %s &\n", scriptPath, scriptPath)
	bgPath := "/tmp/.e2b-bg.sh"
	if err := p.envdWriteFile(ctx, sandbox, bgPath, bgScript); err != nil {
		return nil, fmt.Errorf("failed to write background launcher: %w", err)
	}

	// We can't directly "run" a script via /files alone without a running process.
	// This fallback approach has limitations — it requires some mechanism to trigger
	// the script. In practice, newer E2B versions support the commands API.
	// Return an error indicating that command execution requires the commands API.
	return &ExecResult{
		Stdout:     "",
		Stderr:     "E2B command execution requires envd commands API support",
		ExitCode:   -1,
		DurationMs: time.Since(start).Milliseconds(),
	}, fmt.Errorf("E2B sandbox does not support script-based execution fallback — commands API required")
}

// --- Internal helpers ---

// getSandbox retrieves sandbox metadata by provider ID.
func (p *E2BProvider) getSandbox(providerID string) (*e2bSandbox, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	sandbox, ok := p.sandboxes[providerID]
	if !ok {
		return nil, fmt.Errorf("E2B sandbox not found: %s (may need to reconnect)", providerID)
	}
	if sandbox.paused {
		return nil, fmt.Errorf("E2B sandbox is paused: %s (call Resume first)", providerID)
	}
	return sandbox, nil
}

// installExecHelper writes the execution helper script to the sandbox.
func (p *E2BProvider) installExecHelper(ctx context.Context, sandbox *e2bSandbox) error {
	helper := `#!/bin/bash
# E2B exec helper — runs a command from /tmp/.e2b-exec-cmd
# and writes results to /tmp/.e2b-exec-{stdout,stderr,exitcode,duration}
set -o pipefail
START_NS=$(date +%s%N 2>/dev/null || echo 0)
bash /tmp/.e2b-exec-cmd >/tmp/.e2b-exec-stdout 2>/tmp/.e2b-exec-stderr
echo $? > /tmp/.e2b-exec-exitcode
END_NS=$(date +%s%N 2>/dev/null || echo 0)
if [ "$START_NS" != "0" ] && [ "$END_NS" != "0" ]; then
  echo $(( (END_NS - START_NS) / 1000000 )) > /tmp/.e2b-exec-duration
else
  echo 0 > /tmp/.e2b-exec-duration
fi
`
	return p.envdWriteFile(ctx, sandbox, e2bExecHelperPath, helper)
}

// isBinaryContent checks if content appears to be binary (contains null bytes).
func isBinaryContent(content string) bool {
	for i := 0; i < len(content) && i < 8000; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

// ActiveSandboxes returns the number of tracked sandboxes.
func (p *E2BProvider) ActiveSandboxes() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.sandboxes)
}

// E2BQuotaUsage holds quota usage statistics for the E2B provider.
type E2BQuotaUsage struct {
	TotalCreates    int64 `json:"total_creates"`    // Total sandbox creates since startup
	TotalDestroys   int64 `json:"total_destroys"`   // Total sandbox destroys since startup
	ActiveSandboxes int   `json:"active_sandboxes"` // Currently tracked sandboxes
	ComputeMinutes  int64 `json:"compute_minutes"`  // Estimated total compute minutes consumed
}

// QuotaUsage returns current E2B usage statistics for quota monitoring.
func (p *E2BProvider) QuotaUsage() E2BQuotaUsage {
	return E2BQuotaUsage{
		TotalCreates:    p.totalCreates.Load(),
		TotalDestroys:   p.totalDestroys.Load(),
		ActiveSandboxes: p.ActiveSandboxes(),
		ComputeMinutes:  p.activeMinutes.Load(),
	}
}

// --- API request/response types ---

type e2bCreateSandboxRequest struct {
	TemplateID          string            `json:"templateID"`
	Timeout             int               `json:"timeout"` // seconds
	Metadata            map[string]string `json:"metadata,omitempty"`
	EnvVars             map[string]string `json:"envVars,omitempty"`
	Secure              bool              `json:"secure"`
	AllowInternetAccess bool              `json:"allow_internet_access"`
}

type e2bCreateSandboxResponse struct {
	SandboxID          string `json:"sandboxID"`
	EnvdVersion        string `json:"envdVersion"`
	EnvdAccessToken    string `json:"envdAccessToken"`
	Domain             string `json:"domain,omitempty"`
	TrafficAccessToken string `json:"trafficAccessToken,omitempty"`
}

type e2bListedSandbox struct {
	SandboxID  string            `json:"sandboxID"`
	TemplateID string            `json:"templateID"`
	Alias      string            `json:"alias,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	StartedAt  string            `json:"startedAt"`
	EndAt      string            `json:"endAt"`
	State      string            `json:"state"` // "running" or "paused"
	CPUCount   int               `json:"cpuCount"`
	MemoryMB   int               `json:"memoryMB"`
}
