package email

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aymerick/raymond"
	"github.com/emergent-company/emergent/pkg/logger"
)

// TemplateService handles email template rendering using Handlebars.
//
// Templates are loaded from the templates/email directory with the structure:
// - layouts/*.hbs - Base layouts that wrap content
// - partials/*.hbs - Reusable template parts (buttons, footers, etc.)
// - *.hbs - Main email templates
//
// For MJML templates, this service expects pre-compiled HTML versions.
// In production, MJML templates should be pre-compiled during the build process.
type TemplateService struct {
	templateDir   string
	log           *slog.Logger
	isDevelopment bool

	// Caches
	templateCache map[string]*raymond.Template
	layoutCache   map[string]*raymond.Template
	mu            sync.RWMutex
}

// TemplateRenderResult contains the rendered email content
type TemplateRenderResult struct {
	HTML string
	Text string
}

// TemplateContext is the data passed to templates
type TemplateContext map[string]interface{}

// NewTemplateService creates a new template service
func NewTemplateService(templateDir string, log *slog.Logger) *TemplateService {
	isDev := os.Getenv("NODE_ENV") != "production" && os.Getenv("GO_ENV") != "production"
	
	ts := &TemplateService{
		templateDir:   templateDir,
		log:           log.With(logger.Scope("email.template")),
		isDevelopment: isDev,
		templateCache: make(map[string]*raymond.Template),
		layoutCache:   make(map[string]*raymond.Template),
	}

	// Register partials
	ts.registerPartials()

	// Preload templates in production
	if !isDev {
		ts.preloadTemplates()
	}

	return ts
}

// registerPartials loads and registers all partials from the partials directory
func (ts *TemplateService) registerPartials() {
	partialsDir := filepath.Join(ts.templateDir, "partials")

	if _, err := os.Stat(partialsDir); os.IsNotExist(err) {
		ts.log.Debug("partials directory not found", slog.String("path", partialsDir))
		return
	}

	entries, err := os.ReadDir(partialsDir)
	if err != nil {
		ts.log.Warn("failed to read partials directory", slog.String("error", err.Error()))
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Support both .hbs and .mjml.hbs extensions
		if strings.HasSuffix(name, ".hbs") {
			partialName := strings.TrimSuffix(strings.TrimSuffix(name, ".hbs"), ".mjml")
			content, err := os.ReadFile(filepath.Join(partialsDir, name))
			if err != nil {
				ts.log.Warn("failed to read partial", slog.String("name", name), slog.String("error", err.Error()))
				continue
			}
			raymond.RegisterPartial(partialName, string(content))
			ts.log.Debug("registered partial", slog.String("name", partialName))
		}
	}
}

// preloadTemplates loads all templates into cache (production optimization)
func (ts *TemplateService) preloadTemplates() {
	if _, err := os.Stat(ts.templateDir); os.IsNotExist(err) {
		ts.log.Warn("template directory not found", slog.String("path", ts.templateDir))
		return
	}

	// Load layouts
	layoutsDir := filepath.Join(ts.templateDir, "layouts")
	if entries, err := os.ReadDir(layoutsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".hbs") {
				name := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".hbs"), ".mjml")
				ts.loadLayout(name)
			}
		}
	}

	// Load main templates
	entries, err := os.ReadDir(ts.templateDir)
	if err != nil {
		ts.log.Warn("failed to read template directory", slog.String("error", err.Error()))
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".hbs") {
			name := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".hbs"), ".mjml")
			ts.loadTemplate(name)
		}
	}

	ts.log.Info("preloaded email templates",
		slog.Int("templates", len(ts.templateCache)),
		slog.Int("layouts", len(ts.layoutCache)))
}

// loadTemplate loads a template from disk and caches it
func (ts *TemplateService) loadTemplate(name string) (*raymond.Template, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Check cache first (in production)
	if !ts.isDevelopment {
		if tmpl, ok := ts.templateCache[name]; ok {
			return tmpl, nil
		}
	}

	// Try different file extensions
	var content []byte
	var err error
	
	// Try .html.hbs first (pre-compiled MJML)
	filePath := filepath.Join(ts.templateDir, name+".html.hbs")
	if _, statErr := os.Stat(filePath); statErr == nil {
		content, err = os.ReadFile(filePath)
	} else {
		// Try .mjml.hbs (raw MJML - will need compilation)
		filePath = filepath.Join(ts.templateDir, name+".mjml.hbs")
		if _, statErr := os.Stat(filePath); statErr == nil {
			content, err = os.ReadFile(filePath)
		} else {
			// Try .hbs
			filePath = filepath.Join(ts.templateDir, name+".hbs")
			content, err = os.ReadFile(filePath)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("template not found: %s", name)
	}

	tmpl, err := raymond.Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	// Cache in production
	if !ts.isDevelopment {
		ts.templateCache[name] = tmpl
	}

	return tmpl, nil
}

// loadLayout loads a layout template
func (ts *TemplateService) loadLayout(name string) (*raymond.Template, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Check cache first (in production)
	if !ts.isDevelopment {
		if tmpl, ok := ts.layoutCache[name]; ok {
			return tmpl, nil
		}
	}

	// Try different file extensions
	var content []byte
	var err error

	layoutsDir := filepath.Join(ts.templateDir, "layouts")
	
	// Try .html.hbs first
	filePath := filepath.Join(layoutsDir, name+".html.hbs")
	if _, statErr := os.Stat(filePath); statErr == nil {
		content, err = os.ReadFile(filePath)
	} else {
		// Try .mjml.hbs
		filePath = filepath.Join(layoutsDir, name+".mjml.hbs")
		if _, statErr := os.Stat(filePath); statErr == nil {
			content, err = os.ReadFile(filePath)
		} else {
			// Try .hbs
			filePath = filepath.Join(layoutsDir, name+".hbs")
			content, err = os.ReadFile(filePath)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("layout not found: %s", name)
	}

	tmpl, err := raymond.Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse layout %s: %w", name, err)
	}

	// Cache in production
	if !ts.isDevelopment {
		ts.layoutCache[name] = tmpl
	}

	return tmpl, nil
}

// Render renders an email template with the given context
func (ts *TemplateService) Render(templateName string, context TemplateContext, layoutName string) (*TemplateRenderResult, error) {
	// Reload partials in development
	if ts.isDevelopment {
		ts.registerPartials()
	}

	// Load and render the main template
	tmpl, err := ts.loadTemplate(templateName)
	if err != nil {
		return nil, err
	}

	content, err := tmpl.Exec(context)
	if err != nil {
		return nil, fmt.Errorf("failed to render template %s: %w", templateName, err)
	}

	// Apply layout if specified
	if layoutName != "" {
		layout, err := ts.loadLayout(layoutName)
		if err != nil {
			ts.log.Debug("layout not found, using template directly",
				slog.String("layout", layoutName))
		} else {
			// Create context with content for layout
			layoutCtx := make(TemplateContext)
			for k, v := range context {
				layoutCtx[k] = v
			}
			layoutCtx["content"] = raymond.SafeString(content)

			content, err = layout.Exec(layoutCtx)
			if err != nil {
				return nil, fmt.Errorf("failed to render layout %s: %w", layoutName, err)
			}
		}
	}

	return &TemplateRenderResult{
		HTML: content,
		Text: ts.generatePlainText(context),
	}, nil
}

// RenderFromContent renders a template from raw content string
func (ts *TemplateService) RenderFromContent(content string, context TemplateContext, layoutName string) (*TemplateRenderResult, error) {
	tmpl, err := raymond.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse content: %w", err)
	}

	rendered, err := tmpl.Exec(context)
	if err != nil {
		return nil, fmt.Errorf("failed to render content: %w", err)
	}

	// Apply layout if specified
	if layoutName != "" {
		layout, err := ts.loadLayout(layoutName)
		if err != nil {
			ts.log.Debug("layout not found, using content directly",
				slog.String("layout", layoutName))
		} else {
			layoutCtx := make(TemplateContext)
			for k, v := range context {
				layoutCtx[k] = v
			}
			layoutCtx["content"] = raymond.SafeString(rendered)

			rendered, err = layout.Exec(layoutCtx)
			if err != nil {
				return nil, fmt.Errorf("failed to render layout %s: %w", layoutName, err)
			}
		}
	}

	return &TemplateRenderResult{
		HTML: rendered,
		Text: ts.generatePlainText(context),
	}, nil
}

// generatePlainText creates a plain text version from context
func (ts *TemplateService) generatePlainText(context TemplateContext) string {
	// If context has a plainText field, use it
	if plainText, ok := context["plainText"].(string); ok && plainText != "" {
		return plainText
	}

	// Simple text generation from common fields
	var parts []string

	if title, ok := context["title"].(string); ok && title != "" {
		parts = append(parts, title, "")
	}

	if previewText, ok := context["previewText"].(string); ok && previewText != "" {
		parts = append(parts, previewText, "")
	}

	if message, ok := context["message"].(string); ok && message != "" {
		parts = append(parts, message, "")
	}

	if ctaUrl, ok := context["ctaUrl"].(string); ok && ctaUrl != "" {
		parts = append(parts, fmt.Sprintf("Link: %s", ctaUrl), "")
	}

	if dashboardUrl, ok := context["dashboardUrl"].(string); ok && dashboardUrl != "" {
		parts = append(parts, fmt.Sprintf("Dashboard: %s", dashboardUrl), "")
	}

	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}

	return ""
}

// HasTemplate checks if a template exists
func (ts *TemplateService) HasTemplate(name string) bool {
	// Try different extensions
	extensions := []string{".html.hbs", ".mjml.hbs", ".hbs"}
	for _, ext := range extensions {
		filePath := filepath.Join(ts.templateDir, name+ext)
		if _, err := os.Stat(filePath); err == nil {
			return true
		}
	}
	return false
}

// ListTemplates returns all available template names
func (ts *TemplateService) ListTemplates() []string {
	if _, err := os.Stat(ts.templateDir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(ts.templateDir)
	if err != nil {
		return nil
	}

	var templates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".hbs") {
			// Remove all extensions
			templateName := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, ".hbs"), ".mjml"), ".html")
			templates = append(templates, templateName)
		}
	}

	return templates
}

// ClearCache clears the template cache (useful for development)
func (ts *TemplateService) ClearCache() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.templateCache = make(map[string]*raymond.Template)
	ts.layoutCache = make(map[string]*raymond.Template)
}

// EmbeddedTemplateService is a variant that uses embedded templates
// This is useful for including templates in the binary
type EmbeddedTemplateService struct {
	*TemplateService
	fs embed.FS
}

// NewEmbeddedTemplateService creates a template service from embedded files
func NewEmbeddedTemplateService(fs embed.FS, basePath string, log *slog.Logger) *EmbeddedTemplateService {
	// For embedded templates, we always preload
	ets := &EmbeddedTemplateService{
		TemplateService: &TemplateService{
			templateDir:   basePath,
			log:           log.With(logger.Scope("email.template")),
			isDevelopment: false,
			templateCache: make(map[string]*raymond.Template),
			layoutCache:   make(map[string]*raymond.Template),
		},
		fs: fs,
	}

	// Load embedded templates
	ets.loadEmbeddedTemplates()

	return ets
}

// loadEmbeddedTemplates loads templates from the embedded filesystem
func (ets *EmbeddedTemplateService) loadEmbeddedTemplates() {
	// Register partials
	partialsDir := filepath.Join(ets.templateDir, "partials")
	if entries, err := ets.fs.ReadDir(partialsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".hbs") {
				name := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".hbs"), ".mjml")
				if content, err := ets.fs.ReadFile(filepath.Join(partialsDir, entry.Name())); err == nil {
					raymond.RegisterPartial(name, string(content))
				}
			}
		}
	}

	// Load layouts
	layoutsDir := filepath.Join(ets.templateDir, "layouts")
	if entries, err := ets.fs.ReadDir(layoutsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".hbs") {
				name := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".hbs"), ".mjml")
				if content, err := ets.fs.ReadFile(filepath.Join(layoutsDir, entry.Name())); err == nil {
					if tmpl, err := raymond.Parse(string(content)); err == nil {
						ets.layoutCache[name] = tmpl
					}
				}
			}
		}
	}

	// Load main templates
	if entries, err := ets.fs.ReadDir(ets.templateDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasSuffix(entry.Name(), ".hbs") {
				name := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".hbs"), ".mjml")
				if content, err := ets.fs.ReadFile(filepath.Join(ets.templateDir, entry.Name())); err == nil {
					if tmpl, err := raymond.Parse(string(content)); err == nil {
						ets.templateCache[name] = tmpl
					}
				}
			}
		}
	}

	ets.log.Info("loaded embedded email templates",
		slog.Int("templates", len(ets.templateCache)),
		slog.Int("layouts", len(ets.layoutCache)))
}
