package tracing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Handler proxies Tempo query API requests so clients never talk to Tempo directly.
type Handler struct {
	tempoBaseURL string
	client       *http.Client
	// db is used for the superadmin_full resolution on the instance-wide
	// aggregate path (no project context). Nil when the module is wired without
	// a database (e.g. some test servers), in which case the aggregate path
	// fails closed.
	db bun.IDB
}

// NewHandler creates a tracing handler. When tracing is disabled the handler
// still registers routes but returns 503 for all requests.
func NewHandler(cfg *config.Config, db bun.IDB) *Handler {
	tempoBase := ""
	if cfg.Otel.Enabled() {
		// Derive the internal Tempo query URL from the exporter endpoint:
		// replace the ingest port (4318) with the query port (3200), and use
		// the service hostname (tempo) that is reachable inside the Docker network.
		tempoBase = cfg.Otel.InternalTempoQueryURL()
	}
	return &Handler{
		tempoBaseURL: tempoBase,
		client:       &http.Client{},
		db:           db,
	}
}

// Search proxies GET /api/search to Tempo with all query params forwarded.
// Corresponds to Tempo's trace search API.
//
// The effective project is resolved server-side (RequireProjectTokenScope +
// RequireProjectMember on the group already validated membership) and the query
// is FORCED to that project: a client-supplied project_id or TraceQL q can never
// widen the result set beyond the caller's own project (issue #994 mechanism
// 1/5). With no project context, the instance-wide aggregate is refused unless
// the caller holds superadmin_full.
//
// @Summary      Search traces
// @Description  Proxies Tempo's trace search API, scoped to the caller's project. Returns 503 when tracing is not enabled.
// @Tags         tracing
// @Accept       json
// @Produce      json
// @Security     bearerAuth
// @Param        limit         query string false "Maximum number of traces to return"
// @Param        service_name  query string false "Filter by service name"
// @Param        tags          query string false "Filter by tags (key=value, comma-separated)"
// @Param        min_duration  query string false "Minimum trace duration (e.g. '100ms', '1s')"
// @Param        start         query string false "Start time for the search window (RFC3339)"
// @Param        end           query string false "End time for the search window (RFC3339)"
// @Param        q             query string false "TraceQL query (project predicate is enforced server-side)"
// @Success      200 {object} map[string]any "Trace search results (Tempo passthrough)"
// @Failure      401 {object} map[string]any "Unauthorized"
// @Failure      403 {object} map[string]any "Insufficient permissions"
// @Failure      503 {object} map[string]any "Tracing not enabled"
// @Router       /api/traces [get]
// @Router       /api/traces/search [get]
func (h *Handler) Search(c echo.Context) error {
	if h.tempoBaseURL == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "tracing not enabled")
	}

	user := auth.MustGetUser(c)
	params := c.QueryParams()

	projectID := user.ProjectID
	if projectID == "" {
		// No project context → instance-wide aggregate. superadmin_full only
		// (a scope is not sufficient; see issue #994 mechanism 1/5).
		isSuperadmin, err := auth.IsSuperadminFull(c.Request().Context(), h.db)
		if err != nil {
			return apperror.NewInternal("failed to resolve superadmin status", err)
		}
		if !isSuperadmin {
			return apperror.NewForbidden("project context required; superadmin privilege required for instance-wide trace search")
		}
		// Superadmin: aggregate across all tenants, honouring any explicit TraceQL.
		return h.proxy(c, "/api/search", params)
	}

	// Project-scoped: force the project predicate into the query. The client's
	// q (if any) is ANDed against the server's project filter, and any
	// client-supplied project_id is discarded — it is not an authority.
	params.Set("q", scopeTraceQL(params.Get("q"), projectID))
	params.Del("project_id")
	return h.proxy(c, "/api/search", params)
}

// GetTrace proxies GET /api/traces/:id to Tempo.
//
// Tempo's trace-by-id endpoint accepts no TraceQL filter, so ownership is
// verified server-side: a project-scoped caller may only read a trace whose
// memory.project.id / emergent.project.id matches the caller's authorized
// project. A foreign or unknown project is masked as 404 (no existence oracle,
// issue #994 mechanism 5). With no project context, the instance-wide read is
// refused unless the caller holds superadmin_full.
//
// @Summary      Get trace by ID
// @Description  Proxies Tempo's trace retrieval API, verifying the trace belongs to the caller's project. Returns 503 when tracing is not enabled.
// @Tags         tracing
// @Accept       json
// @Produce      json
// @Security     bearerAuth
// @Param        id path string true "Trace ID"
// @Param        format        query string false "Response format: 'structured' returns a normalized span list instead of raw OTLP"
// @Success      200 {object} map[string]any "Full span tree (Tempo passthrough)"
// @Failure      401 {object} map[string]any "Unauthorized"
// @Failure      403 {object} map[string]any "Insufficient permissions"
// @Failure      404 {object} map[string]any "Trace not found"
// @Failure      503 {object} map[string]any "Tracing not enabled"
// @Router       /api/traces/{id} [get]
func (h *Handler) GetTrace(c echo.Context) error {
	if h.tempoBaseURL == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "tracing not enabled")
	}

	user := auth.MustGetUser(c)
	traceID := c.Param("id")

	projectID := user.ProjectID
	if projectID == "" {
		isSuperadmin, err := auth.IsSuperadminFull(c.Request().Context(), h.db)
		if err != nil {
			return apperror.NewInternal("failed to resolve superadmin status", err)
		}
		if !isSuperadmin {
			return apperror.NewForbidden("project context required; superadmin privilege required for instance-wide trace access")
		}
	}

	// Fetch the trace buffered so ownership can be verified before any span /
	// prompt / payload bytes are returned.
	path := "/api/traces/" + traceID
	resp, err := h.tempoGet(c, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read tempo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return echo.NewHTTPError(resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if projectID != "" {
		traceProject := extractTraceProject(body)
		if traceProject == "" || traceProject != projectID {
			// Foreign or un-attributed trace: mask as 404 so a foreign trace id
			// cannot be distinguished from a nonexistent one.
			return apperror.NewNotFound("trace", traceID)
		}
	}

	if c.QueryParam("format") == "structured" {
		var otlpResp otlpTraceResponse
		if err := json.Unmarshal(body, &otlpResp); err != nil {
			return fmt.Errorf("decode tempo response: %w", err)
		}
		return c.JSON(http.StatusOK, toStructuredTrace(&otlpResp))
	}

	c.Response().Header().Set(echo.HeaderContentType, resp.Header.Get(echo.HeaderContentType))
	c.Response().WriteHeader(http.StatusOK)
	_, err = c.Response().Write(body)
	return err
}

// extractTraceProject parses an OTLP trace JSON payload and returns the owning
// project id (memory.project.id, falling back to emergent.project.id), or "" if
// the trace carries no project attribute.
func extractTraceProject(body []byte) string {
	var resp otlpTraceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	var spans []otlpSpan
	for _, b := range resp.Batches {
		for _, ss := range b.ScopeSpans {
			spans = append(spans, ss.Spans...)
		}
	}
	if p := firstAttrValue(spans, "memory.project.id"); p != "" {
		return p
	}
	return firstAttrValue(spans, "emergent.project.id")
}

// scopeTraceQL forces a TraceQL query to be scoped to the given project by
// ANDing a project predicate into the query's condition block. When q is empty
// it produces a project-only query. The caller's q is treated as untrusted
// input (a client may craft a foreign project predicate), so the server's
// predicate always wraps it.
func scopeTraceQL(q, projectID string) string {
	predicate := fmt.Sprintf(`.memory.project.id = "%s" || .emergent.project.id = "%s"`, projectID, projectID)
	q = strings.TrimSpace(q)
	if q == "" {
		return "{ " + predicate + " }"
	}
	// q is "{ conditions } [| pipeline]". AND the predicate into the condition
	// block. The first "}" closes the condition block (TraceQL pipelines and
	// select() clauses use parentheses, never bare braces).
	if strings.HasPrefix(q, "{") {
		if end := strings.Index(q, "}"); end > 0 {
			rest := q[end+1:]
			inner := strings.TrimSpace(q[1:end])
			if inner == "" {
				return "{ " + predicate + " }" + rest
			}
			return "{ " + predicate + " && (" + inner + ") }" + rest
		}
	}
	// No leading brace: treat the whole string as bare conditions and AND them.
	return "{ " + predicate + " && (" + q + ") }"
}

// tempoGet performs a GET against Tempo and returns the raw response. It
// mirrors proxy's request-building and error handling. The caller must close
// the returned response body.
func (h *Handler) tempoGet(c echo.Context, path string) (*http.Response, error) {
	target := h.tempoBaseURL + path

	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build tempo request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("tempo unreachable: %s", err))
	}
	return resp, nil
}

// proxy forwards the request to Tempo and streams the response back.
func (h *Handler) proxy(c echo.Context, path string, params url.Values) error {
	target := path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	resp, err := h.tempoGet(c, target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	c.Response().Header().Set(echo.HeaderContentType, resp.Header.Get(echo.HeaderContentType))
	c.Response().WriteHeader(resp.StatusCode)
	_, err = io.Copy(c.Response(), resp.Body)
	return err
}
