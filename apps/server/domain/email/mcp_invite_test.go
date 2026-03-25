package email

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTemplateDir returns the absolute path to the actual email templates directory,
// walking up from the test file's location.
func realTemplateDir(t *testing.T) string {
	t.Helper()
	// __file__ is in apps/server/domain/email/ — templates are at apps/server/templates/email/
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Walk up to apps/server, then down to templates/email
	dir := filepath.Join(wd, "..", "..", "templates", "email")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("template dir not found at %s: %v", abs, err)
	}
	return abs
}

// TestNewTemplateServiceFromConfig_StoresAbsolutePath verifies that the module
// stores an absolute path in the TemplateService, not the relative candidate
// string. This is the regression test for the bug where HasTemplate() would
// fail when the process working directory differed from the startup directory.
func TestNewTemplateServiceFromConfig_StoresAbsolutePath(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewTemplateServiceFromConfig(log)

	if !filepath.IsAbs(svc.templateDir) {
		t.Errorf("templateDir is not absolute: %q — HasTemplate() will fail when CWD changes", svc.templateDir)
	}
}

// TestHasTemplate_MCPInviteExists verifies that the mcp-invite template is
// discoverable by HasTemplate() when given the real template directory.
func TestHasTemplate_MCPInviteExists(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewTemplateService(realTemplateDir(t), log)

	if !svc.HasTemplate("mcp-invite") {
		t.Error("HasTemplate(\"mcp-invite\") = false — template file is missing or not found")
	}
}

// TestHasTemplate_RelativePathFails documents the original bug: a relative
// templateDir causes HasTemplate() to return false when CWD differs.
func TestHasTemplate_RelativePathFails(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Deliberately use a relative path (the old buggy behaviour)
	svc := NewTemplateService("apps/server/templates/email", log)

	// Change to a directory where the relative path won't resolve
	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	_ = os.Chdir(os.TempDir())

	if svc.HasTemplate("mcp-invite") {
		t.Log("HasTemplate returned true even with relative path from wrong CWD — behaviour changed")
	} else {
		t.Log("confirmed: relative path causes HasTemplate to return false when CWD differs (original bug)")
	}
	// This test is documentary — it doesn't fail either way, it just shows the contrast.
}

// TestMCPInviteTemplate_Renders verifies that the mcp-invite.hbs template
// renders without error and produces HTML containing the expected fields.
func TestMCPInviteTemplate_Renders(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewTemplateService(realTemplateDir(t), log)

	ctx := TemplateContext{
		"senderName":  "Alice",
		"projectName": "Acme Knowledge Base",
		"projectId":   "proj-123",
		"mcpUrl":      "https://api.example.com/api/mcp",
		"apiKey":      "emt_testtoken123",
		"installUrl":  "claude://install-mcp?url=https://api.example.com/api/mcp&name=memory",
		"snippets": map[string]interface{}{
			"claudeDesktop": `{"mcpServers":{"memory":{"url":"https://api.example.com/api/mcp"}}}`,
			"claudeCode":    `{"mcpServers":{"memory":{"type":"http","url":"https://api.example.com/api/mcp"}}}`,
			"cursor":        `{"mcpServers":{"memory":{"url":"https://api.example.com/api/mcp"}}}`,
			"cloudCode":     `{"mcpServers":{"memory":{"httpUrl":"https://api.example.com/api/mcp"}}}`,
		},
	}

	result, err := svc.Render("mcp-invite", ctx, "default")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if result == nil {
		t.Fatal("Render() returned nil result")
	}

	html := result.HTML

	checks := []struct {
		desc    string
		contain string
	}{
		{"sender name", "Alice"},
		{"project name", "Acme Knowledge Base"},
		{"api key", "emt_testtoken123"},
		{"mcp url", "https://api.example.com/api/mcp"},
		{"install deep link", "claude://install-mcp"},
		{"claude desktop section", "Claude Desktop"},
		{"claude code section", "Claude Code"},
		{"cursor section", "Cursor"},
		{"cloud code section", "Cloud Code"},
		{"not a fallback", "emergent.memory"},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.contain) {
			t.Errorf("rendered HTML missing %s: expected to contain %q", c.desc, c.contain)
		}
	}
}

// TestMCPInviteTemplate_NotFallback verifies that when the mcp-invite template
// is found, the worker does NOT produce the generic fallback content.
func TestMCPInviteTemplate_NotFallback(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewTemplateService(realTemplateDir(t), log)

	ctx := TemplateContext{
		"senderName":  "Bob",
		"projectName": "Test Project",
		"projectId":   "proj-456",
		"mcpUrl":      "https://api.example.com/api/mcp",
		"apiKey":      "emt_abc",
		"installUrl":  "claude://install-mcp?url=https://api.example.com/api/mcp&name=memory",
		"snippets":    map[string]interface{}{},
	}

	result, err := svc.Render("mcp-invite", ctx, "default")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	fallbackMarker := "This email was sent by Memory"
	if strings.Contains(result.HTML, fallbackMarker) {
		t.Errorf("rendered HTML contains fallback content %q — template was not used", fallbackMarker)
	}
}

// TestMCPInviteTemplate_HasTemplateAndRenders verifies the full path the worker
// takes: HasTemplate() returns true, then Render() produces non-fallback HTML.
// This is the critical regression test — if either step fails the worker falls
// back to "Hello, This email was sent by Memory."
func TestMCPInviteTemplate_HasTemplateAndRenders(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewTemplateService(realTemplateDir(t), log)

	// Step 1: worker checks HasTemplate before rendering
	if !svc.HasTemplate("mcp-invite") {
		t.Fatal("HasTemplate(\"mcp-invite\") = false — worker would use fallback")
	}

	// Step 2: worker calls Render
	ctx := TemplateContext{
		"senderName":  "Carol",
		"projectName": "My Project",
		"projectId":   "proj-789",
		"mcpUrl":      "https://api.example.com/api/mcp",
		"apiKey":      "emt_xyz",
		"installUrl":  "claude://install-mcp?url=https://api.example.com/api/mcp&name=memory",
		"snippets": map[string]interface{}{
			"claudeDesktop": `{}`,
			"claudeCode":    `{}`,
			"cursor":        `{}`,
			"cloudCode":     `{}`,
		},
	}

	result, err := svc.Render("mcp-invite", ctx, "default")
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}

	if strings.Contains(result.HTML, "This email was sent by Memory") {
		t.Error("rendered HTML contains fallback content — template was not used")
	}
	if !strings.Contains(result.HTML, "Carol") {
		t.Error("rendered HTML missing sender name")
	}
	if !strings.Contains(result.HTML, "My Project") {
		t.Error("rendered HTML missing project name")
	}
}
