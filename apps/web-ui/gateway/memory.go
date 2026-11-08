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
	"net"
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

// MemoryClient is a thin HTTP client for the Memory REST API.
// It holds no process-global credential: every request's bearer token and
// project scope are resolved from the request's session context.
type MemoryClient struct {
	baseURL    string
	projectID  string
	http       *http.Client // non-streaming requests — short timeout
	streamHTTP *http.Client // streaming (SSE) requests — no timeout; caller ctx governs lifetime
	// shareRefSecret is the HMAC key for the X-End-User-Ref-Sig header on
	// public share calls. Set once at construction from SHARE_REF_SECRET.
	shareRefSecret string
}

func NewMemoryClient(baseURL, projectID string) *MemoryClient {
	return &MemoryClient{
		baseURL:    baseURL,
		projectID:  projectID,
		http:       &http.Client{Timeout: 60 * time.Second},
		streamHTTP: &http.Client{}, // no timeout — SSE streams stay open for the run
	}
}

// tokenFor resolves the bearer token for one request: the session credential
// when a session context is attached, else a session-less token (a scoped
// programmatic credential such as the GitHub webhook trigger token). There is
// no process-global fallback: a session-less caller without an explicit token
// sends an empty bearer and the upstream rejects it with 401, surfacing the
// mistake loudly instead of silently acting as a shared service account.
func (m *MemoryClient) tokenFor(ctx context.Context) string {
	if sc, ok := sessionContextFrom(ctx); ok {
		return sc.Token
	}
	return sessionlessTokenFrom(ctx)
}

// projectIDFor resolves the active project for one request: the session's
// active project when a session context is attached, else the static
// server-side project id (API-key / programmatic callers only). A session with
// no active project yields "" — the static id is a deployment default, never a
// per-session fallback, so a session can never silently act against a project
// the caller is not a member of (#1998).
func (m *MemoryClient) projectIDFor(ctx context.Context) string {
	if sc, ok := sessionContextFrom(ctx); ok {
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

// retryBackoff holds one base delay per retry, in order. Its length MUST equal
// maxMemoryAttempts-1; the compile-time assertion below fails to build if
// maxMemoryAttempts grows without a matching entry, so a new retry can never
// silently reuse the last delay or index out of bounds.
var retryBackoff = [2]time.Duration{200 * time.Millisecond, 600 * time.Millisecond}

const _ = uint(len(retryBackoff) - (maxMemoryAttempts - 1))

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

// isTimeout reports whether err is a client-side timeout: the transport
// deadline fired (http.Client.Timeout) or the request context was cancelled.
// Such an error means the upstream is slow, not transiently unavailable, so the
// retry loop must fail fast rather than replay a request that will only time
// out again (issue #1096).
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// retryBackoffDelay returns the delay before the next attempt: the configured
// base for the attempt just made plus up to 100ms of jitter, so concurrent
// clients do not retry in lockstep.
func retryBackoffDelay(attempt int) time.Duration {
	// attempt is 1-based; clamp both ends so a bad caller can never panic.
	base := retryBackoff[min(max(attempt, 1), len(retryBackoff))-1]
	// math/rand/v2's rand.N is auto-seeded and safe for concurrent use. It is
	// used for jitter only (not security-sensitive): do NOT swap it for a
	// seeded math/rand source or wrap it in a mutex.
	return base + rand.N(100*time.Millisecond)
}

// retryAfterDelay parses a Retry-After header in either RFC 7231 form: a
// non-negative delta-seconds value, or an HTTP-date (parsed with
// http.ParseTime). The resulting delay is capped at 5s. It returns 0 when the
// header is absent, unparseable, or already in the past, so the caller falls
// back to its own backoff.
func retryAfterDelay(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs < 0 {
			return 0
		}
		return min(time.Duration(secs)*time.Second, 5*time.Second)
	}
	if t, err := http.ParseTime(header); err == nil {
		if d := time.Until(t); d > 0 {
			return min(d, 5*time.Second)
		}
	}
	return 0
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

// memoryHTTPErrorMessage returns the backend's message for a memory HTTP error,
// without the internal "memory <status> <code>:" prefix. It returns "" for any
// other error (transport failure, locally-authored error), so callers fall back
// to a generic message. Use it on surfaces that want to echo the backend's
// actionable validation copy to the user rather than a redacted generic line.
func memoryHTTPErrorMessage(err error) string {
	var he *memoryHTTPError
	if errors.As(err, &he) {
		return he.Message
	}
	return ""
}

// isMemoryStatus reports whether err is a memory backend error with the given
// HTTP status.
func isMemoryStatus(err error, status int) bool { return memoryStatus(err) == status }

// userFacingMemoryError returns a safe, non-technical message for a memory
// request failure. It never returns err.Error(): transport failures carry
// *url.Error strings such as "dial tcp 172.17.0.1:8080: connect: connection
// refused" and memoryHTTPError renders a "memory <status> <code>:" prefix —
// both leak internal addresses and backend detail to end users.
func userFacingMemoryError(err error) string {
	if err == nil {
		return ""
	}
	// A 5xx response (502/503/504 and any other server error) is an upstream
	// outage, not a user mistake: report "temporarily unavailable" without any
	// backend detail.
	if status := memoryStatus(err); status >= http.StatusInternalServerError {
		return memoryUnavailableMessage
	}
	// A transport-level failure or client-side timeout: connection
	// refused/reset, TLS handshake, DNS, or an exceeded deadline. net.Error
	// covers *url.Error (which implements Timeout/Temporary);
	// context.DeadlineExceeded is checked explicitly because it is not a
	// net.Error.
	if errors.Is(err, context.DeadlineExceeded) {
		return memoryUnavailableMessage
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return memoryUnavailableMessage
	}
	return genericErrorMessage
}

// memoryUnavailableMessage is the user-facing copy for a backend outage: a
// non-technical "your data is safe" message that never exposes hosts, ports,
// URLs, or the raw `memory <status> <code>:` prefix.
const memoryUnavailableMessage = "Memory is temporarily unavailable. Your data is safe — retrying automatically."

// genericErrorMessage is the fallback user-facing copy for any failure that is
// neither a 5xx nor a transport/timeout error.
const genericErrorMessage = "Something went wrong."

// userFacingError wraps a memory backend failure in its sanitized,
// non-technical message (see userFacingMemoryError) for rendering through a
// pageError surface. Unavailable tenancy pages (org/project loads) render the
// error directly, so a raw *url.Error or memoryHTTPError must never reach them.
func userFacingError(err error) error {
	return errors.New(userFacingMemoryError(err))
}

// redactBackendError sanitizes a memory backend or transport failure, while
// returning a locally-authored, already-safe error unchanged. Surfaces that mix
// their own validation/sentinel copy with backend failures use it so only the
// latter is redacted.
func redactBackendError(err error) error {
	if err == nil {
		return nil
	}
	if memoryStatus(err) != 0 || errors.Is(err, context.DeadlineExceeded) {
		return userFacingError(err)
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return userFacingError(err)
	}
	return err
}

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

// memoryRetryPolicy controls how doWithPolicy replays a failed request. A zero
// value disables all retries: the request is attempted exactly once.
type memoryRetryPolicy struct {
	// retryTransientStatus retries 502/503/504 responses on idempotent
	// (GET/HEAD, no body) requests.
	retryTransientStatus bool
	// retryTransport retries transport-level failures (connection reset, EOF,
	// TLS handshake) on idempotent requests.
	retryTransport bool
}

// memoryRetryDefault is the policy do/doH use: transient statuses and transport
// errors are retried for idempotent requests, preserving the historical
// behavior for every existing caller.
var memoryRetryDefault = memoryRetryPolicy{retryTransientStatus: true, retryTransport: true}

// memoryRetryNone disables every retry: one attempt, no replay. It is for
// idempotent reads whose upstream is slow or hanging — the official MCP
// registry — where a 502 is not a transient blip and replaying it only
// multiplies page latency (maxMemoryAttempts × the upstream timeout).
var memoryRetryNone memoryRetryPolicy

func (m *MemoryClient) do(ctx context.Context, method, path string, body any, out any) error {
	return m.doH(ctx, method, path, body, nil, out)
}

// doH is do with extra request headers (e.g. the Accept header the MCP
// endpoint needs). It retries transient failures under the default policy; see
// doWithPolicy.
func (m *MemoryClient) doH(ctx context.Context, method, path string, body any, hdrs map[string]string, out any) error {
	_, err := m.doWithPolicy(ctx, method, path, body, hdrs, out, memoryRetryDefault)
	return err
}

// doHResp is doH that also returns the final response headers, so a caller can
// read transport-level hints (e.g. X-Resources-Truncated) without disturbing the
// shared retry machinery. The headers come from the last attempt.
func (m *MemoryClient) doHResp(ctx context.Context, method, path string, body any, hdrs map[string]string, out any) (http.Header, error) {
	return m.doWithPolicy(ctx, method, path, body, hdrs, out, memoryRetryDefault)
}

// doNoRetry issues a request exactly once, without the generic 502/503/504
// retry and without transport-error retry. It is for idempotent reads whose
// upstream is slow or hanging — the official MCP registry — where a 502 is not
// a transient blip worth replaying and a retry only multiplies page latency.
func (m *MemoryClient) doNoRetry(ctx context.Context, method, path string, body any, out any) error {
	_, err := m.doWithPolicy(ctx, method, path, body, nil, out, memoryRetryNone)
	return err
}

// doWithPolicy performs one or more attempts of a memory request and unmarshals
// a 2xx response into out.
//
// Transient upstream failures — 502/503/504 responses and transport errors
// (connection reset, EOF, TLS handshake) — are retried up to maxMemoryAttempts
// times for idempotent requests only (GET/HEAD without a body); non-idempotent
// requests are never replayed. The policy gates which of those two failure
// classes are retried; memoryRetryNone retries neither, so a request is made
// exactly once. The returned error is wrapped in memoryAttemptError so callers
// can report how many attempts were made.
func (m *MemoryClient) doWithPolicy(ctx context.Context, method, path string, body any, hdrs map[string]string, out any, policy memoryRetryPolicy) (http.Header, error) {
	// Marshal the body once; a fresh reader is rebuilt per attempt in doOnce so
	// a *bytes.Reader consumed by an earlier attempt can never be replayed
	// empty.
	var rdBuf []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdBuf = b
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
		hdr      http.Header
	)
	for attempt := range maxMemoryAttempts {
		attempts = attempt + 1
		var retryAfter time.Duration
		status, raw, retryAfter, hdr, lastErr = m.doOnce(ctx, method, path, rdBuf, body != nil, hdrs)
		if lastErr != nil {
			// A transport error is transient: retry it for an idempotent
			// request on the same terms as a 5xx status, but fail fast for
			// non-idempotent requests and abort immediately on cancellation.
			//
			// A client-side timeout is NOT transient — it means the upstream is
			// slow, and replaying the same 60s request only multiplies the
			// outage (3×60s ≈ 180s held a page behind one slow /similar call,
			// issue #1096). Timeout errors therefore never retry.
			if isTimeout(lastErr) || !retryable || !policy.retryTransport || attempts == maxMemoryAttempts || ctx.Err() != nil {
				break
			}
			if !waitRetry(ctx, retryBackoffDelay(attempts)) {
				break
			}
			continue
		}
		if !retryable || !policy.retryTransientStatus || attempts == maxMemoryAttempts || !retryableMemoryStatus(status) {
			break
		}
		// Honour an upstream Retry-After (delta-seconds or HTTP-date, capped at
		// 5s) over the default exponential backoff with jitter. Abort
		// immediately when the caller's context is done.
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
		return hdr, attemptErr(lastErr)
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
			if shouldCaptureMemoryError(ctx, status) {
				captureMemoryError(method, path, status, attemptErr(err))
			}
		}
		return hdr, attemptErr(err)
	}
	if out == nil {
		return hdr, nil
	}
	return hdr, json.Unmarshal(raw, out)
}

// doOnce performs one HTTP attempt and returns the response status, body and
// any Retry-After delay. The response body is always closed before returning,
// so retries never leak connections.
//
// body is the already-marshalled request payload (nil when the request has
// none); a fresh bytes.Reader is built here for every call, so each retry
// attempt sends the complete payload rather than a consumed one.
func (m *MemoryClient) doOnce(ctx context.Context, method, path string, body []byte, hasBody bool, hdrs map[string]string) (status int, raw []byte, retryAfter time.Duration, hdr http.Header, err error) {
	var rd io.Reader
	if hasBody {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, rd)
	if err != nil {
		return 0, nil, 0, nil, err
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
		return 0, nil, 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxMemoryResponseBytes+1))
	if err != nil {
		return resp.StatusCode, nil, 0, resp.Header, err
	}
	if len(raw) > maxMemoryResponseBytes {
		return resp.StatusCode, nil, 0, resp.Header, fmt.Errorf("memory response exceeds %d bytes", maxMemoryResponseBytes)
	}
	return resp.StatusCode, raw, retryAfterDelay(resp.Header.Get("Retry-After")), resp.Header, nil
}

// --- agent definitions ---

// AgentWorkStatusConfig mirrors the server's agent workConfig.status: the object
// status values that mean each work-item lifecycle phase. An empty value
// inherits the platform default for that phase.
type AgentWorkStatusConfig struct {
	Ready      string `json:"ready"`
	InProgress string `json:"inProgress"`
	Review     string `json:"review"`
	Revision   string `json:"revision"`
	Blocked    string `json:"blocked"`
	Done       string `json:"done"`
}

// AgentRetryPolicy mirrors the server's agent workConfig.retryPolicy: the
// backoff schedule for queued work runs.
type AgentRetryPolicy struct {
	MaxAttempts        int     `json:"maxAttempts"`
	InitialIntervalMS  int     `json:"initialIntervalMs"`
	BackoffCoefficient float64 `json:"backoffCoefficient"`
	MaxIntervalMS      int     `json:"maxIntervalMs"`
}

// AgentWorkContract mirrors the server's agent workConfig.workContract: the
// deliverables an agent must produce before it may complete a work item. A zero
// value means "no validation".
type AgentWorkContract struct {
	RequireArtifacts         bool     `json:"requireArtifacts"`
	RequiredDeliverableTypes []string `json:"requiredDeliverableTypes,omitempty"`
}

// AgentWorkTrigger is one pickup rule row (object type + status + prompt).
type AgentWorkTrigger struct {
	ObjectType string `json:"objectType"`
	Status     string `json:"status"`
	Prompt     string `json:"prompt,omitempty"`
}

// AgentTransition is one agent-permitted status edge.
type AgentTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// AgentObjectRule is the per-object-type work rule.
type AgentObjectRule struct {
	ObjectType  string                 `json:"objectType"`
	Status      *AgentWorkStatusConfig `json:"status,omitempty"`
	CanCreate   *bool                  `json:"canCreate,omitempty"`
	CanUpdate   *bool                  `json:"canUpdate,omitempty"`
	CanDelete   *bool                  `json:"canDelete,omitempty"`
	Fields      []string               `json:"fields,omitempty"`
	Triggers    []AgentWorkTrigger     `json:"triggers,omitempty"`
	Transitions []AgentTransition      `json:"transitions,omitempty"`
	// ClearAssigneeOnComplete nulls the assignee when the item completes.
	ClearAssigneeOnComplete bool `json:"clearAssigneeOnComplete,omitempty"`
}

// AgentWorkConfig mirrors the server's full agent workConfig. The board consumes
// only the status phase map (boardStatusMapFromDefinitions); the Work settings
// form reads and writes the whole shape.
type AgentWorkConfig struct {
	Status         AgentWorkStatusConfig `json:"status"`
	RequiresReview bool                  `json:"requiresReview"`
	FailureLimit   int                   `json:"failureLimit"`
	RevisionLimit  int                   `json:"revisionLimit"`
	RetryPolicy    AgentRetryPolicy      `json:"retryPolicy"`
	WorkContract   AgentWorkContract     `json:"workContract"`
	// TriggerStatuses lists the status values that, when an object transitions
	// into one of them, wake this agent. Defaults to "ready" when unset. Legacy:
	// used only when ObjectRules declares no triggers.
	TriggerStatuses []string `json:"triggerStatuses,omitempty"`
	// ObjectRules carries the per-type rules (phase map, write actions, fields,
	// triggers, transitions).
	ObjectRules []AgentObjectRule `json:"objectRules,omitempty"`
}

type AgentDefinitionSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Description is the agent's stored short description, shown under its name
	// in the chat pane header. Absent/empty means no description.
	Description string   `json:"description,omitempty"`
	Visibility  string   `json:"visibility"`
	ToolCount   int      `json:"toolCount"`
	Skills      []string `json:"skills"`
	IsDefault   bool     `json:"isDefault"`
	Enabled     bool     `json:"enabled"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
	// EffectiveModel is the model this agent would run with (explicit override
	// wins, else the project's resolved generative default). Present on the
	// list endpoint from memory ≥ the effective-model deploy; empty means
	// unconfigured or an older memory version.
	EffectiveModel string `json:"effectiveModel,omitempty"`
	// UIConfig is the agent's user-picked appearance blob
	// ({"icon":"<kebab-lucide-name>","color":"<CSS color>"}); absent/{} means no
	// appearance. Parsed by agentUIOf; see agent_ui.go.
	UIConfig json.RawMessage `json:"uiConfig,omitempty"`
	// WorkConfig carries the agent's object-driven work status map (read-only).
	// The board derives its work-path transitions from it so custom-mapped
	// boards expose drag/drawer actions; nil/zero means "use the defaults".
	WorkConfig *AgentWorkConfig `json:"workConfig,omitempty"`
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

// AgentSkillCapture mirrors memory's skills.AgentSkillCapture
// (kb.agent_definitions.skill_capture): the per-agent override of the project's
// automatic skill-capture policy. Mode is a pointer so "unset" (inherit the
// project value) is distinguishable from an explicit mode; memory only lets an
// override narrow the project policy, never widen it. AutoApply is round-tripped
// untouched — the UI does not render it, but dropping it would clear an
// API-set opt-out on the next save (memory replaces the whole override). A new
// server-side field must be added here too, or the next Skills save drops it;
// the project policy avoids that class of bug by staying a raw map, which is
// affordable there because it carries many knobs.
type AgentSkillCapture struct {
	Mode      *string `json:"mode,omitempty"`
	AutoApply *bool   `json:"autoApply,omitempty"`
}

// ToolGroup is the server-computed capability group on an agent definition
// (read-only: the gateway renders it, never derives it). Tools is the group's
// FULL membership — the project catalog for the group unioned with the agent's
// allowed and banned tools — so a fully disabled group still carries every
// member. Enabled reports whether at least one member is currently allowed and
// not banned. Policy is the stored group policy as "" (inherit), "allow",
// "ask", or "deny".
type ToolGroup struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Policy      string   `json:"policy,omitempty"`
	Enabled     bool     `json:"enabled"`
	Tools       []string `json:"tools"`
}

type AgentDefinition struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	// Description is deliberately NOT omitempty: the General settings form
	// clears it by submitting an empty value, and memory's PATCH treats an
	// absent key as "leave unchanged" while an explicit "" clears. Omitting it
	// would silently keep the old description.
	Description  string       `json:"description"`
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
	// ToolGroups is the server-computed capability taxonomy for the Tools
	// picker. Read-only: it is populated on GET, and sanitizeAgentWrite clears
	// it before a create/update is serialized (group policies live under
	// "@group:<id>" keys in ToolPolicies). Omitted when an older memory does not
	// send it.
	ToolGroups []ToolGroup `json:"toolGroups,omitempty"`
	Skills     []string    `json:"skills"`
	// SkillCapture is the per-agent override of the project's automatic
	// skill-capture policy. The override may only narrow the project value
	// ("off"/"suggest"); memory resolves the effective mode, which this field
	// never widens. Nil inherits; the Skills form sends the field on every save
	// so "inherit" clears a stored override by sending {}.
	SkillCapture *AgentSkillCapture `json:"skillCapture,omitempty"`
	Config       map[string]any     `json:"config"`
	Delegation   *Delegation        `json:"delegation,omitempty"`
	Visibility   string             `json:"visibility,omitempty"`
	DispatchMode string             `json:"dispatchMode,omitempty"`
	Enabled      bool               `json:"enabled"`
	ToolCount    int                `json:"toolCount,omitempty"`
	CreatedAt    string             `json:"createdAt,omitempty"`
	UpdatedAt    string             `json:"updatedAt,omitempty"`
	// UIConfig is the agent's user-picked appearance blob
	// ({"icon":"<kebab-lucide-name>","color":"<CSS color>"}); absent/{} means no
	// appearance. Parsed by agentUIOf; see agent_ui.go.
	UIConfig json.RawMessage `json:"uiConfig,omitempty"`
	// WorkConfig is the object-driven work configuration. The Work settings
	// form reads and writes it; every other section save round-trips the stored
	// value unchanged (the server PATCH re-applies it losslessly). nil means
	// the agent does not opt into object-driven work.
	WorkConfig *AgentWorkConfig `json:"workConfig,omitempty"`
	// SourceBlueprintID is the ID of the blueprint that created this definition
	// (read-only). Non-nil marks a blueprint-derived agent, which the Work form
	// warns about before saving a divergent edit.
	SourceBlueprintID *string `json:"sourceBlueprintId,omitempty"`
}

// sanitizeAgentWrite clears server-computed, read-only fields from an agent
// definition before it is sent to memory. ToolGroups is computed on read from
// the stored policies + catalog; the gateway must never depend on it
// round-tripping, so the write path drops it rather than relying on the server
// to ignore an unknown field. WorkConfig is intentionally preserved: it is a
// first-class persisted field the Work settings form writes, and every other
// section save round-trips the stored value unchanged.
func sanitizeAgentWrite(def *AgentDefinition) {
	if def != nil {
		def.ToolGroups = nil
	}
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
		def.Tools = appendUnique(def.Tools, "spawn_agents", "list_available_agents")
		if def.Config == nil {
			def.Config = map[string]any{}
		}
		if len(def.Delegation.Targets) == 0 {
			// No targets means the open policy: every agent is spawnable. The
			// server treats an absent Config["spawnPolicy"] as open (see
			// apps/server/domain/agents/coordination_tools.go), so deleting the
			// key keeps deriveDelegation→applyDelegation round-trips idempotent.
			delete(def.Config, "spawnPolicy")
		} else {
			def.Config["spawnPolicy"] = map[string]any{"allow": def.Delegation.Targets}
		}
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
