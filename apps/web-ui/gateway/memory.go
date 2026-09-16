package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

// maxMemoryResponseBytes caps a memory response body read into memory at 64 MiB.
// A misbehaving or malicious backend must not be able to exhaust the gateway's
// address space by streaming an unbounded body.
const maxMemoryResponseBytes = 64 << 20

// MemoryClient is a thin HTTP client for the Emergent Memory REST API.
// It holds the server-side memory credentials (never exposed to clients).
type MemoryClient struct {
	baseURL    string
	token      string
	projectID  string
	http       *http.Client // non-streaming requests — short timeout
	streamHTTP *http.Client // streaming (SSE) requests — no timeout; caller ctx governs lifetime

}

func NewMemoryClient(baseURL, token, projectID string) *MemoryClient {
	return &MemoryClient{
		baseURL:    baseURL,
		token:      token,
		projectID:  projectID,
		http:       &http.Client{Timeout: 60 * time.Second},
		streamHTTP: &http.Client{}, // no timeout — SSE streams stay open for the run
	}
}

// tokenFor resolves the bearer token for one request: the session's token when
// a session context is attached, else the static server-side token (the
// API-key / supervisor path).
func (m *MemoryClient) tokenFor(ctx context.Context) string {
	if sc, ok := sessionContextFrom(ctx); ok && sc.Token != "" {
		return sc.Token
	}
	return m.token
}

// projectIDFor resolves the active project for one request: the session's
// active project when a session context is attached, else the static
// server-side project id.
func (m *MemoryClient) projectIDFor(ctx context.Context) string {
	if sc, ok := sessionContextFrom(ctx); ok && sc.ProjectID != "" {
		return sc.ProjectID
	}
	return m.projectID
}

// sessionHeaders returns the X-Project-ID / X-Org-ID headers for a request
// carrying a session context (design D6): a Zitadel (user) access token is not
// project-bound, so Memory needs the headers to scope every call. The API-key
// path has no session context and returns nothing — that token is already
// project-bound (the document/chunk routes set X-Project-ID explicitly).
func sessionHeaders(ctx context.Context) map[string]string {
	h := map[string]string{}
	if sc, ok := sessionContextFrom(ctx); ok && sc.ProjectID != "" {
		h["X-Project-ID"] = sc.ProjectID
		if sc.OrgID != "" {
			h["X-Org-ID"] = sc.OrgID
		}
	}
	return h
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// memoryHTTPError is a non-2xx response from the memory backend. It carries the
// HTTP status so callers can classify without parsing the message text.
type memoryHTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e *memoryHTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("memory %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("memory %d: %s", e.Status, e.Message)
}

// memoryAttemptError wraps a memory request failure with the number of HTTP
// attempts made. Error/Unwrap delegate to the underlying error, so status
// classification (memoryStatus, isMemoryStatus, isMemoryNotFound) and
// errors.Is/errors.As keep working unchanged.
type memoryAttemptError struct {
	err      error
	attempts int
}

func (e *memoryAttemptError) Error() string { return e.err.Error() }

func (e *memoryAttemptError) Unwrap() error { return e.err }

// maxMemoryAttempts is the total number of attempts (initial + retries) for an
// idempotent request.
const maxMemoryAttempts = 3

// retryBackoff holds the base delay before retry i (0-based), before jitter.
var retryBackoff = [...]time.Duration{200 * time.Millisecond, 600 * time.Millisecond}

// retryableMemoryStatus reports whether a memory response status is a transient
// upstream failure worth retrying for an idempotent request.
func retryableMemoryStatus(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// retryBackoffDelay returns the delay before the next attempt: the configured
// base for the attempt just made plus up to 100ms of jitter, so concurrent
// clients do not retry in lockstep.
func retryBackoffDelay(attempt int) time.Duration {
	base := retryBackoff[min(attempt, len(retryBackoff))-1]
	return base + rand.N(100*time.Millisecond)
}

// retryAfterDelay parses a Retry-After header value as delta-seconds, capped at
// 5s. It returns 0 when the header is absent or not a valid delta-seconds value
// (e.g. an HTTP-date), so the caller falls back to its own backoff.
func retryAfterDelay(header string) time.Duration {
	if header == "" {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || secs < 0 {
		return 0
	}
	return min(time.Duration(secs)*time.Second, 5*time.Second)
}

// waitRetry sleeps for d, reporting false when ctx is cancelled first so the
// caller aborts immediately.
func waitRetry(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// parseMemoryError converts a non-2xx memory response body into a clean
// error. It extracts the {"error":{"code":...,"message":...}} shape when
// present, else the {"error":"..."} string shape, else the raw body.
func parseMemoryError(status int, raw []byte) error {
	var obj struct {
		Error apiError `json:"error"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Error.Code != "" {
		return &memoryHTTPError{Status: status, Code: obj.Error.Code, Message: obj.Error.Message}
	}
	var str struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &str) == nil && str.Error != "" {
		return &memoryHTTPError{Status: status, Message: str.Error}
	}
	return &memoryHTTPError{Status: status, Message: string(raw)}
}

// memoryStatus returns the HTTP status carried by a memory backend error, or 0
// when err is not a memory HTTP error.
func memoryStatus(err error) int {
	var he *memoryHTTPError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

// isMemoryStatus reports whether err is a memory backend error with the given
// HTTP status.
func isMemoryStatus(err error, status int) bool { return memoryStatus(err) == status }

// truncateString caps s at n bytes for log output, appending an ellipsis when
// truncated. Memory response bodies are small, but a defensive cap keeps a
// misbehaving backend from flooding the gateway log.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// successEnvelope wraps the {success,data} shape used by agent-definition routes.
type successEnvelope[T any] struct {
	Success bool      `json:"success"`
	Data    T         `json:"data"`
	Error   *apiError `json:"error"`
}

func (m *MemoryClient) do(ctx context.Context, method, path string, body any, out any) error {
	return m.doH(ctx, method, path, body, nil, out)
}

// doH is do with extra request headers (e.g. the Accept header the MCP
// endpoint needs).
//
// Transient upstream failures (502/503/504) are retried up to maxMemoryAttempts
// times for idempotent requests only (GET/HEAD without a body); non-idempotent
// requests are never replayed. The returned error is wrapped in
// memoryAttemptError so callers can report how many attempts were made.
func (m *MemoryClient) doH(ctx context.Context, method, path string, body any, hdrs map[string]string, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	// Only requests without a body may be replayed: re-sending a
	// POST/PUT/PATCH/DELETE could duplicate a side effect, and a consumed
	// reader cannot be safely reused.
	retryable := (method == http.MethodGet || method == http.MethodHead) && body == nil

	var (
		status   int
		raw      []byte
		lastErr  error
		attempts int
	)
	for attempt := range maxMemoryAttempts {
		attempts = attempt + 1
		var retryAfter time.Duration
		status, raw, retryAfter, lastErr = m.doOnce(ctx, method, path, rd, body != nil, hdrs)
		if lastErr != nil {
			break
		}
		if !retryable || attempts == maxMemoryAttempts || !retryableMemoryStatus(status) {
			break
		}
		// Honour an upstream Retry-After (valid delta-seconds, capped at 5s)
		// over the default exponential backoff with jitter. Abort immediately
		// when the caller's context is done.
		delay := retryAfter
		if delay == 0 {
			delay = retryBackoffDelay(attempts)
		}
		if !waitRetry(ctx, delay) {
			break
		}
	}
	attemptErr := func(err error) error { return &memoryAttemptError{err: err, attempts: attempts} }
	if lastErr != nil {
		return attemptErr(lastErr)
	}
	if status >= 400 {
		err := parseMemoryError(status, raw)
		// A 404 "not found" is a normal, expected outcome for single-resource
		// reads in this app — absent project settings, deleted conversations or
		// blueprints, unknown sessions. Handlers map those to "not set"/empty
		// states via isMemoryNotFound, so they are not real errors and must not
		// be reported to Sentry as such.
		if status != http.StatusNotFound {
			// Log the failure locally (method/path/status + the backend's
			// response body — never the request body, which carries secrets
			// like API keys) so real causes surface even when Sentry is
			// unconfigured.
			log.Printf("memory API error: %s %s -> %d: %s", method, path, status, truncateString(string(raw), 2048))
			captureMemoryError(method, path, status, attemptErr(err))
		}
		return attemptErr(err)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// doOnce performs one HTTP attempt and returns the response status, body and
// any Retry-After delay. The response body is always closed before returning,
// so retries never leak connections.
func (m *MemoryClient) doOnce(ctx context.Context, method, path string, rd io.Reader, hasBody bool, hdrs map[string]string) (status int, raw []byte, retryAfter time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, rd)
	if err != nil {
		return 0, nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+m.tokenFor(ctx))
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	for k, v := range sessionHeaders(ctx) {
		req.Header.Set(k, v)
	}
	span := sentry.StartSpan(ctx, "http.client", sentry.WithDescription(method+" "+path))
	resp, err := m.http.Do(req)
	span.Finish()
	if err != nil {
		return 0, nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxMemoryResponseBytes+1))
	if err != nil {
		return resp.StatusCode, nil, 0, err
	}
	if len(raw) > maxMemoryResponseBytes {
		return resp.StatusCode, nil, 0, fmt.Errorf("memory response exceeds %d bytes", maxMemoryResponseBytes)
	}
	return resp.StatusCode, raw, retryAfterDelay(resp.Header.Get("Retry-After")), nil
}

// --- agent definitions ---

type AgentDefinitionSummary struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	FlowType   string   `json:"flowType"`
	Visibility string   `json:"visibility"`
	ToolCount  int      `json:"toolCount"`
	Skills     []string `json:"skills"`
	IsDefault  bool     `json:"isDefault"`
	Enabled    bool     `json:"enabled"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
	// EffectiveModel is the model this agent would run with (explicit override
	// wins, else the project's resolved generative default). Present on the
	// list endpoint from memory ≥ the effective-model deploy; empty means
	// unconfigured or an older memory version.
	EffectiveModel string `json:"effectiveModel,omitempty"`
}

// UnmarshalJSON defaults Enabled to true when the `enabled` key is absent, so
// the gateway treats agents as enabled against a Memory that predates the
// `enabled` column, and never disables an agent via a partial payload that
// omits the field.
func (a *AgentDefinitionSummary) UnmarshalJSON(data []byte) error {
	a.Enabled = true
	type alias AgentDefinitionSummary
	return json.Unmarshal(data, (*alias)(a))
}

// UnmarshalJSON defaults Enabled to true when the `enabled` key is absent.
func (a *AgentDefinition) UnmarshalJSON(data []byte) error {
	a.Enabled = true
	type alias AgentDefinition
	return json.Unmarshal(data, (*alias)(a))
}

type ModelConfig struct {
	Name        string  `json:"name,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"maxTokens,omitempty"`
}

// Delegation is a gateway-only field that maps onto memory's A2A gating:
// when enabled it adds the spawn_agents/list_available_agents tools and an
// spawnPolicy.allow allowlist to Config. It is never serialized to memory.
type Delegation struct {
	Enabled bool     `json:"enabled"`
	Targets []string `json:"targets"`
}

// ToolPolicy is the per-tool approval policy on an agent definition: Confirm
// requires human approval before the tool executes; Disabled hard-blocks it.
type ToolPolicy struct {
	Confirm  bool   `json:"confirm"`
	Message  string `json:"message,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type AgentDefinition struct {
	ID           string       `json:"id"`
	ProjectID    string       `json:"projectId"`
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	SystemPrompt string       `json:"systemPrompt,omitempty"`
	Model        *ModelConfig `json:"model,omitempty"`
	// EffectiveModel is the resolved generative model memory will use when this
	// definition runs (explicit model wins, else the project's resolved
	// default). Present on GET and on the list endpoint from memory ≥ the
	// effective-model deploy; empty means "not provided" (older memory).
	EffectiveModel    string                `json:"effectiveModel,omitempty"`
	Tools             []string              `json:"tools"`
	BannedTools       []string              `json:"bannedTools,omitempty"`
	ToolPolicies      map[string]ToolPolicy `json:"toolPolicies,omitempty"`
	DefaultToolPolicy string                `json:"defaultToolPolicy,omitempty"`
	Skills            []string              `json:"skills"`
	Config            map[string]any        `json:"config"`
	Delegation        *Delegation           `json:"delegation,omitempty"`
	FlowType          string                `json:"flowType,omitempty"`
	Visibility        string                `json:"visibility,omitempty"`
	DispatchMode      string                `json:"dispatchMode,omitempty"`
	Enabled           bool                  `json:"enabled"`
	ToolCount         int                   `json:"toolCount,omitempty"`
	CreatedAt         string                `json:"createdAt,omitempty"`
	UpdatedAt         string                `json:"updatedAt,omitempty"`
}

// applyDelegation maps the gateway-only Delegation field onto memory's A2A
// gating: the spawn_agents/list_available_agents tools and Config's
// spawnPolicy.allow allowlist. The Delegation field is always cleared so it
// is not serialized to memory.
func applyDelegation(def *AgentDefinition) error {
	if def.Delegation == nil {
		return nil
	}
	if def.Delegation.Enabled {
		if len(def.Delegation.Targets) == 0 {
			return fmt.Errorf("delegation.targets must not be empty when delegation is enabled")
		}
		def.Tools = appendUnique(def.Tools, "spawn_agents", "list_available_agents")
		if def.Config == nil {
			def.Config = map[string]any{}
		}
		def.Config["spawnPolicy"] = map[string]any{"allow": def.Delegation.Targets}
	} else {
		def.Tools = removeItems(def.Tools, "spawn_agents", "list_available_agents")
		if def.Tools == nil {
			def.Tools = []string{}
		}
		if def.Config == nil {
			def.Config = map[string]any{}
		}
		delete(def.Config, "spawnPolicy")
	}
	def.Delegation = nil
	return nil
}

// deriveDelegation reconstructs the gateway-only Delegation field from the
// state applyDelegation persisted to memory (the spawn_agents tool and the
// Config.spawnPolicy.allow allowlist), so GET responses round-trip the field
// the create/update path accepts.
func deriveDelegation(def *AgentDefinition) {
	if def == nil {
		return
	}
	enabled := false
	for _, t := range def.Tools {
		if t == "spawn_agents" {
			enabled = true
			break
		}
	}
	var targets []string
	if def.Config != nil {
		if sp, ok := def.Config["spawnPolicy"].(map[string]any); ok {
			switch allow := sp["allow"].(type) {
			case []any:
				for _, a := range allow {
					if s, ok := a.(string); ok {
						targets = append(targets, s)
					}
				}
			case []string:
				targets = append(targets, allow...)
			}
		}
	}
	if enabled || len(targets) > 0 {
		def.Delegation = &Delegation{Enabled: enabled, Targets: targets}
	}
}

// appendUnique appends items to list, skipping any already present.
func appendUnique(list []string, items ...string) []string {
	for _, item := range items {
		found := false
		for _, existing := range list {
			if existing == item {
				found = true
				break
			}
		}
		if !found {
			list = append(list, item)
		}
	}
	return list
}

// removeItems returns list without any occurrences of items.
func removeItems(list []string, items ...string) []string {
	out := list[:0]
	for _, existing := range list {
		keep := true
		for _, item := range items {
			if existing == item {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, existing)
		}
	}
	return out
}
