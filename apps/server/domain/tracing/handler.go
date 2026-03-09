package tracing

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// Handler proxies Tempo query API requests so clients never talk to Tempo directly.
type Handler struct {
	tempoBaseURL string
	client       *http.Client
}

// NewHandler creates a tracing handler. When tracing is disabled the handler
// still registers routes but returns 503 for all requests.
func NewHandler(cfg *config.Config) *Handler {
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
	}
}

// Search proxies GET /api/search to Tempo with all query params forwarded.
// Corresponds to Tempo's trace search API.
func (h *Handler) Search(c echo.Context) error {
	if h.tempoBaseURL == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "tracing not enabled")
	}
	return h.proxy(c, "/api/search", c.QueryParams())
}

// GetTrace proxies GET /api/traces/:id to Tempo.
func (h *Handler) GetTrace(c echo.Context) error {
	if h.tempoBaseURL == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "tracing not enabled")
	}
	return h.proxy(c, "/api/traces/"+c.Param("id"), nil)
}

// proxy forwards the request to Tempo and streams the response back.
func (h *Handler) proxy(c echo.Context, path string, params url.Values) error {
	target := h.tempoBaseURL + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build tempo request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("tempo unreachable: %s", err))
	}
	defer resp.Body.Close()

	c.Response().Header().Set(echo.HeaderContentType, resp.Header.Get(echo.HeaderContentType))
	c.Response().WriteHeader(resp.StatusCode)
	_, err = io.Copy(c.Response(), resp.Body)
	return err
}
