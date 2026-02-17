package devtools

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Handler serves developer tools endpoints
type Handler struct {
	log         *slog.Logger
	cfg         *config.Config
	coverageDir string
	docsDir     string
}

// NewHandler creates a new devtools handler
func NewHandler(log *slog.Logger, cfg *config.Config) *Handler {
	// Determine base directory (relative to server-go)
	baseDir := "."
	if _, err := os.Stat("apps/server-go"); err == nil {
		baseDir = "apps/server-go"
	}

	return &Handler{
		log:         log.With(logger.Scope("devtools")),
		cfg:         cfg,
		coverageDir: filepath.Join(baseDir, "coverage"),
		docsDir:     filepath.Join(baseDir, "docs", "swagger"),
	}
}

// ServeCoverage serves the coverage index page
// @Summary      Serve test coverage report
// @Description  Returns the HTML coverage report index page (or helpful message if not generated)
// @Tags         devtools
// @Produce      html
// @Success      200 {string} string "Coverage report HTML"
// @Router       /coverage [get]
func (h *Handler) ServeCoverage(c echo.Context) error {
	indexPath := filepath.Join(h.coverageDir, "index.html")

	// Check if coverage report exists
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		return h.serveCoverageNotFound(c)
	}

	return c.File(indexPath)
}

// ServeCoverageFiles serves coverage report files (CSS, JS, etc.)
// @Summary      Serve coverage report assets
// @Description  Serves static files for coverage report (CSS, JS, etc.) with directory traversal protection
// @Tags         devtools
// @Produce      octet-stream
// @Param        filepath path string true "File path within coverage directory (use * for wildcard in actual route)"
// @Success      200 {file} file "Coverage asset file"
// @Failure      400 {string} string "Invalid path"
// @Failure      403 {string} string "Access denied (directory traversal attempt)"
// @Failure      404 {string} string "File not found"
// @Router       /coverage/{filepath} [get]
func (h *Handler) ServeCoverageFiles(c echo.Context) error {
	// Get the requested file path
	requestPath := c.Param("*")
	if requestPath == "" {
		return h.ServeCoverage(c)
	}

	filePath := filepath.Join(h.coverageDir, requestPath)

	// Security: prevent directory traversal
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid path")
	}
	absDir, _ := filepath.Abs(h.coverageDir)
	if !strings.HasPrefix(absPath, absDir) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "File not found")
	}

	return c.File(filePath)
}

// serveCoverageNotFound shows a helpful message when coverage isn't generated
func (h *Handler) serveCoverageNotFound(c echo.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Coverage Report - Not Generated</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 40px; max-width: 800px; margin: 0 auto; }
        h1 { color: #333; }
        code { background: #f4f4f4; padding: 2px 8px; border-radius: 4px; }
        pre { background: #1e1e1e; color: #d4d4d4; padding: 16px; border-radius: 8px; overflow-x: auto; }
        .command { color: #9cdcfe; }
        .comment { color: #6a9955; }
    </style>
</head>
<body>
    <h1>📊 Coverage Report Not Found</h1>
    <p>The coverage report has not been generated yet. Run the following commands to generate it:</p>
    
    <h3>Generate Coverage Report</h3>
    <pre><code><span class="comment"># From apps/server-go directory:</span>
<span class="command">make test-coverage-html</span>

<span class="comment"># Or manually:</span>
<span class="command">go test -coverprofile=coverage/coverage.out ./...</span>
<span class="command">go tool cover -html=coverage/coverage.out -o coverage/index.html</span>
</code></pre>

    <h3>Generate E2E Test Coverage</h3>
    <pre><code><span class="comment"># Run E2E tests with coverage:</span>
<span class="command">./scripts/run-e2e-tests.sh --coverage</span>
</code></pre>

    <p>After generating the report, refresh this page.</p>
    
    <p><a href="/docs">→ View API Documentation</a></p>
</body>
</html>`
	return c.HTML(http.StatusOK, html)
}

// ServeDocsIndex serves the Swagger UI index page
// @Summary      Serve Swagger UI
// @Description  Returns the Swagger UI HTML page for browsing API documentation
// @Tags         devtools
// @Produce      html
// @Success      200 {string} string "Swagger UI HTML"
// @Router       /docs [get]
func (h *Handler) ServeDocsIndex(c echo.Context) error {
	return h.serveSwaggerUI(c)
}

// ServeDocs serves Swagger UI assets
// @Summary      Serve API documentation assets
// @Description  Serves Swagger specification files (swagger.json, swagger.yaml)
// @Tags         devtools
// @Produce      json
// @Param        filepath path string true "File path (swagger.json or swagger.yaml) - use * for wildcard in actual route"
// @Success      200 {file} file "Swagger spec file"
// @Failure      404 {string} string "File not found"
// @Router       /docs/{filepath} [get]
func (h *Handler) ServeDocs(c echo.Context) error {
	requestPath := c.Param("*")

	// Serve index.html for empty path (shouldn't happen with new routes but keep as fallback)
	if requestPath == "" || requestPath == "/" {
		return h.serveSwaggerUI(c)
	}

	// Try to serve from docs directory
	filePath := filepath.Join(h.docsDir, requestPath)
	if _, err := os.Stat(filePath); err == nil {
		return c.File(filePath)
	}

	return echo.NewHTTPError(http.StatusNotFound, "File not found")
}

// serveSwaggerUI serves the Swagger UI HTML
func (h *Handler) serveSwaggerUI(c echo.Context) error {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Emergent API Documentation</title>
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui.css">
    <style>
        html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
        *, *:before, *:after { box-sizing: inherit; }
        body { margin: 0; background: #fafafa; }
        .swagger-ui .topbar { display: none; }
        .swagger-ui .info { margin: 30px 0; }
        .swagger-ui .info .title { font-size: 36px; }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-bundle.js"></script>
    <script src="https://unpkg.com/swagger-ui-dist@5.9.0/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            // Determine the base path from current URL (handles /docs and /api/docs)
            const basePath = window.location.pathname.replace(/\/docs\/?$/, '');
            const specUrl = basePath ? basePath + '/openapi.json' : '/openapi.json';
            
            window.ui = SwaggerUIBundle({
                url: specUrl,
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout",
                validatorUrl: null,
                persistAuthorization: true
            });
        };
    </script>
</body>
</html>`
	return c.HTML(http.StatusOK, html)
}

// ServeOpenAPISpec serves the OpenAPI JSON specification
// @Summary      Serve OpenAPI specification
// @Description  Returns the generated OpenAPI/Swagger JSON spec (or minimal spec if not generated)
// @Tags         devtools
// @Produce      json
// @Success      200 {object} map[string]interface{} "OpenAPI 3.0 specification"
// @Router       /openapi.json [get]
func (h *Handler) ServeOpenAPISpec(c echo.Context) error {
	// Try to serve generated spec from docs directory
	specPath := filepath.Join(h.docsDir, "swagger.json")
	if _, err := os.Stat(specPath); err == nil {
		return c.File(specPath)
	}

	// Return a minimal spec if none exists
	return h.serveMinimalSpec(c)
}

// serveMinimalSpec returns a minimal OpenAPI spec when none is generated
func (h *Handler) serveMinimalSpec(c echo.Context) error {
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Emergent API",
			"description": "Emergent Knowledge Base API - Go Server\n\nOpenAPI spec not yet generated. Run `make swagger` to generate the full spec from annotations.",
			"version":     "1.0.0",
			"contact": map[string]any{
				"name": "Emergent Team",
			},
		},
		"servers": []map[string]any{
			{"url": "/", "description": "Current server"},
		},
		"paths": map[string]any{
			"/health": map[string]any{
				"get": map[string]any{
					"summary":     "Health check",
					"description": "Returns server health status",
					"tags":        []string{"Health"},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Server is healthy",
						},
					},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
		},
		"security": []map[string]any{
			{"bearerAuth": []string{}},
		},
		"tags": []map[string]any{
			{"name": "Health", "description": "Health check endpoints"},
			{"name": "Documents", "description": "Document management"},
			{"name": "Chunks", "description": "Document chunk management"},
			{"name": "Graph", "description": "Knowledge graph operations"},
			{"name": "Search", "description": "Search operations"},
			{"name": "Chat", "description": "Chat conversations"},
			{"name": "MCP", "description": "Model Context Protocol"},
		},
	}
	return c.JSON(http.StatusOK, spec)
}

// EnsureCoverageDir creates the coverage directory if it doesn't exist
func (h *Handler) EnsureCoverageDir() error {
	return os.MkdirAll(h.coverageDir, 0755)
}

// EnsureDocsDir creates the docs directory if it doesn't exist
func (h *Handler) EnsureDocsDir() error {
	return os.MkdirAll(h.docsDir, 0755)
}
