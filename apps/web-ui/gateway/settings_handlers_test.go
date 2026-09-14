package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestUIProjectSettingsRoute exercises GET /settings: the project record and
// the remember/dedup settings render; absent settings fall back to "not set".
func TestUIProjectSettingsRoute(t *testing.T) {
	f := &fakeMemory{
		project: &Project{
			ID:                 "p1",
			Name:               "Home",
			ProjectInfo:        "family",
			ChatPromptTemplate: "hi",
			AutoExtractObjects: boolPtr(true),
			BudgetUSD:          floatPtr(25.5),
		},
		settings: map[string]map[string]map[string]any{
			"remember_config": {"agent_name": {"name": "memory"}},
			"entity_create":   {"similarity_threshold": {"value": 0.8}},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/settings", s.uiProjectSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Project Settings", "Project info", "Remember &amp; dedup",
		`value="Home"`, "family", "hi",
		`name="auto_extract_objects"`,
		`value="memory"`, `value="0.8"`,
		`hx-post="/settings/project/name"`,
		`hx-post="/settings/remember/agent"`, `hx-post="/settings/remember/dedup"`,
		`hx-post="/settings/editor"`, `name="editor_agent"`,
		`href="/settings/assistant"`, `href="/settings/overrides"`, `href="/settings/providers"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings route missing %q", want)
		}
	}
	// the checked toggle renders a bare `checked` attribute
	if !strings.Contains(body, `name="auto_extract_objects" class="toggle" checked`) {
		t.Error("auto-extract toggle should render checked")
	}
}

// TestUIProjectSettingsRouteAbsent exercises the "not set" states: no stored
// settings render clear empty/not-set states.
func TestUIProjectSettingsRouteAbsent(t *testing.T) {
	f := &fakeMemory{project: &Project{ID: "p1", Name: "Home"}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/settings", s.uiProjectSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Budget not set.", "not set"} {
		if !strings.Contains(body, want) {
			t.Errorf("absent settings route missing %q", want)
		}
	}
}

// TestUIProjectSettingsRouteError asserts the whole-page error state when the
// project fetch fails (memory unreachable), without crashing the rest.
func TestUIProjectSettingsRouteError(t *testing.T) {
	f := &fakeMemory{projectErr: errTest}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/settings", s.uiProjectSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Project settings unavailable") {
		t.Error("load-error state missing")
	}
}

// TestUIProjectSettingsRouteBestEffort asserts that a failed overrides or
// settings read still renders the page (best-effort reads, per design).
func TestUIProjectSettingsRouteBestEffort(t *testing.T) {
	f := &fakeMemory{
		project:     &Project{ID: "p1", Name: "Home"},
		overrideErr: errTest,
		settingErr:  errTest,
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/settings", s.uiProjectSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="Home"`) || strings.Contains(body, "Project settings unavailable") {
		t.Error("page should still render with best-effort reads degraded")
	}
}

// TestUIProjectVoiceSettingsRoute exercises GET /settings/voice: the Voice
// sub-page renders its sub-nav and the inline auto-save panel (no whole-form
// Save button).
func TestUIProjectVoiceSettingsRoute(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/settings/voice", s.uiProjectVoiceSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/voice", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Voice",
		`hx-post="/settings/voice/enabled"`, `hx-swap="none"`,
		`hx-post="/settings/voice/group/tts"`,
		`hx-post="/settings/voice/group/endpoint_delays"`,
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("voice route missing %q", want)
		}
	}
	for _, gone := range []string{`action="/settings/voice"`, "Save voice", `name="voice_enabled"`, `-feedback`} {
		if strings.Contains(body, gone) {
			t.Errorf("voice route must not contain %q", gone)
		}
	}
}

// TestUIProjectDeviceSettingsRoute exercises GET /settings/devices: the
// Devices sub-page renders its sub-nav, the iOS setup panel, and the devices
// list empty state.
func TestUIProjectDeviceSettingsRoute(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/settings/devices", s.uiProjectDeviceSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/devices", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Devices", "iOS setup", "No devices registered yet.",
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("devices route missing %q", want)
		}
	}
}

// TestUIProjectAssistantSettingsRoute exercises GET /settings/assistant: the
// Assistant sub-page renders its sub-nav and the assistant agent dropdown.
func TestUIProjectAssistantSettingsRoute(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/settings/assistant", s.uiProjectAssistantSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/assistant", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Assistant", `hx-post="/settings/assistant"`, `name="assistant_agent"`, `hx-swap="none"`,
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("assistant route missing %q", want)
		}
	}
	for _, gone := range []string{`action="/settings/assistant"`, "Save assistant"} {
		if strings.Contains(body, gone) {
			t.Errorf("assistant route must not contain %q", gone)
		}
	}
}

// TestUIProjectSettingsAssistantFullLoad: saving the assistant agent forces a
// full page load (HX-Redirect for HTMX, 303 for plain POSTs) because the
// selection gates shell chrome — the topbar "Assistant" button and the sidepanel
// both render outside #main-content. A no-swap toast would leave the button
// hidden until a manual refresh.
func TestUIProjectSettingsAssistantFullLoad(t *testing.T) {
	t.Run("htmx full load", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: Config{}, memory: f}
		e := echo.New()
		e.POST("/settings/assistant", s.uiProjectSettingsAssistant)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/assistant", strings.NewReader("assistant_agent=a1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/settings/assistant?updated=1" {
			t.Fatalf("HTMX assistant save should force a full reload, got %d HX-Redirect %q", rec.Code, rec.Header().Get("HX-Redirect"))
		}
		if f.lastSettingCat != settingsAssistantCategory || f.lastSettingKey != settingsAssistantKey {
			t.Errorf("assistant setting write = %q/%q, want %q/%q", f.lastSettingCat, f.lastSettingKey, settingsAssistantCategory, settingsAssistantKey)
		}
	})

	t.Run("plain post redirect", func(t *testing.T) {
		f := &fakeMemory{}
		s := &Server{cfg: Config{}, memory: f}
		e := echo.New()
		e.POST("/settings/assistant", s.uiProjectSettingsAssistant)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/assistant", strings.NewReader("assistant_agent="))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/assistant?updated=1" {
			t.Fatalf("plain assistant save should 303, got %d %q", rec.Code, rec.Header().Get("Location"))
		}
		if f.lastDeletedCat != settingsAssistantCategory || f.lastDeletedKey != settingsAssistantKey {
			t.Errorf("assistant setting delete = %q/%q, want %q/%q", f.lastDeletedCat, f.lastDeletedKey, settingsAssistantCategory, settingsAssistantKey)
		}
	})
}

// TestUIProjectOverridesSettingsRoute exercises GET /settings/overrides: the
// Overrides sub-page renders its sub-nav and the override list/form.
func TestUIProjectOverridesSettingsRoute(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/settings/overrides", s.uiProjectOverridesSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/overrides", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Agent overrides", `action="/settings/overrides"`, `name="agentName"`,
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overrides route missing %q", want)
		}
	}
}

// TestUIProjectProvidersSettingsRoute exercises GET /settings/providers: the
// Providers sub-page renders its sub-nav and the provider rate panel.
func TestUIProjectProvidersSettingsRoute(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/settings/providers", s.uiProjectProvidersSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/providers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Providers",
		`href="/settings"`, `href="/settings/assistant"`, `href="/settings/overrides"`,
		`href="/settings/providers"`, `href="/settings/voice"`, `href="/settings/devices"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("providers route missing %q", want)
		}
	}
}

func TestUIProjectSettingsProjectFieldName(t *testing.T) {
	f := &fakeMemory{project: &Project{ID: "p1", Name: "Old"}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/settings/project/:field", s.uiProjectSettingsProjectField)

	rec := voicePost(e, "/settings/project/name", "name=Home")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("name save should trigger a success toast, got %q", hdr)
	}
	if f.updatedProject == nil || f.updatedProject.Name != "Home" {
		t.Errorf("UpdateProject name = %v, want Home", f.updatedProject)
	}
}

func TestUIProjectSettingsProjectFieldToggle(t *testing.T) {
	f := &fakeMemory{project: &Project{ID: "p1", Name: "Home"}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/project/:field", s.uiProjectSettingsProjectField)

	rec := voicePost(e, "/settings/project/auto_extract_objects", "auto_extract_objects=on")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if f.updatedProject == nil || f.updatedProject.AutoExtractObjects == nil || !*f.updatedProject.AutoExtractObjects {
		t.Error("auto_extract_objects should be set true")
	}
	if f.updatedProject.Name != "" {
		t.Errorf("name should not be touched by a toggle save, got %q", f.updatedProject.Name)
	}
}

func TestUIProjectSettingsProjectFieldEmptyName(t *testing.T) {
	f := &fakeMemory{project: &Project{ID: "p1", Name: "Old"}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/project/:field", s.uiProjectSettingsProjectField)

	rec := voicePost(e, "/settings/project/name", "name=")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "project name is required") {
		t.Errorf("empty name should trigger an error toast, got %q", hdr)
	}
	if f.updatedProject != nil {
		t.Error("UpdateProject must not be called for an empty name")
	}
}

func TestUIProjectSettingsProjectFieldError(t *testing.T) {
	f := &fakeMemory{project: &Project{ID: "p1", Name: "Old"}, updateErr: errTest}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/project/:field", s.uiProjectSettingsProjectField)

	rec := voicePost(e, "/settings/project/name", "name=Home")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "backend unreachable") {
		t.Errorf("write failure should trigger an error toast, got %q", hdr)
	}
}

func TestUIProjectSettingsOverride(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/settings/overrides", s.uiProjectSettingsOverride)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/overrides", strings.NewReader("agentName=memory&systemPrompt=be+terse&model=gpt-4o&tools=web_search,+memory_lookup&maxSteps=10"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/overrides?updated=1" {
		t.Errorf("redirect = %q, want /settings/overrides?updated=1", loc)
	}
	if f.overrideAgent != "memory" {
		t.Errorf("agent name = %q, want memory", f.overrideAgent)
	}
	in := f.overrideInput
	if in == nil {
		t.Fatal("SetAgentOverride not called")
	}
	if in.SystemPrompt == nil || *in.SystemPrompt != "be terse" {
		t.Errorf("systemPrompt = %v", in.SystemPrompt)
	}
	if in.Model == nil || in.Model.Name != "gpt-4o" {
		t.Errorf("model = %v", in.Model)
	}
	if len(in.Tools) != 2 || in.Tools[0] != "web_search" || in.Tools[1] != "memory_lookup" {
		t.Errorf("tools = %v", in.Tools)
	}
	if in.MaxSteps == nil || *in.MaxSteps != 10 {
		t.Errorf("maxSteps = %v", in.MaxSteps)
	}
}

func TestUIProjectSettingsOverrideEmptyName(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/settings/overrides", s.uiProjectSettingsOverride)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/overrides", strings.NewReader("agentName=&systemPrompt=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/overrides?err=agent+name+is+required" {
		t.Errorf("empty agent name should redirect with the error, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if f.overrideInput != nil {
		t.Error("SetAgentOverride must not be called for an empty agent name")
	}
}

func TestUIProjectSettingsOverrideError(t *testing.T) {
	f := &fakeMemory{overrideErr: errTest}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/settings/overrides", s.uiProjectSettingsOverride)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/overrides", strings.NewReader("agentName=memory"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/overrides?err=backend+unreachable" {
		t.Errorf("override write failure should redirect with the error, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIProjectSettingsOverrideDelete(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.POST("/settings/overrides/:agentName/delete", s.uiProjectSettingsOverrideDelete)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/overrides/diane/delete", nil)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/overrides?updated=1" {
		t.Errorf("redirect = %q, want /settings/overrides?updated=1", loc)
	}
	if f.overrideAgent != "diane" {
		t.Errorf("deleted agent = %q, want diane", f.overrideAgent)
	}
}

func TestUIProjectSettingsRememberAgent(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/remember/:field", s.uiProjectSettingsRememberField)

	rec := voicePost(e, "/settings/remember/agent", "remember_agent=memory")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("agent save should trigger a success toast, got %q", hdr)
	}
	if f.lastSettingCat != "remember_config" || f.lastSettingKey != "agent_name" {
		t.Errorf("write = %s/%s, want remember_config/agent_name", f.lastSettingCat, f.lastSettingKey)
	}
	if v, ok := f.lastSettingValue["name"].(string); !ok || v != "memory" {
		t.Errorf("remember value = %v, want memory", f.lastSettingValue)
	}
}

func TestUIProjectSettingsRememberAgentClear(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/remember/:field", s.uiProjectSettingsRememberField)

	rec := voicePost(e, "/settings/remember/agent", "remember_agent=")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if f.lastDeletedCat != "remember_config" || f.lastDeletedKey != "agent_name" {
		t.Errorf("clear should delete remember_config/agent_name, got %s/%s", f.lastDeletedCat, f.lastDeletedKey)
	}
}

func TestUIProjectSettingsRememberDedup(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/remember/:field", s.uiProjectSettingsRememberField)

	rec := voicePost(e, "/settings/remember/dedup", "dedup_threshold=0.8")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if f.lastSettingCat != "entity_create" || f.lastSettingKey != "similarity_threshold" {
		t.Errorf("write = %s/%s, want entity_create/similarity_threshold", f.lastSettingCat, f.lastSettingKey)
	}
	if v, ok := f.lastSettingValue["value"].(float64); !ok || v != 0.8 {
		t.Errorf("dedup value = %v, want 0.8", f.lastSettingValue["value"])
	}
}

func TestUIProjectSettingsRememberInvalidDedup(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/remember/:field", s.uiProjectSettingsRememberField)

	for _, v := range []string{"1.5", "-0.1", "abc"} {
		rec := voicePost(e, "/settings/remember/dedup", "dedup_threshold="+v)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, want 200", rec.Code)
		}
		if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "dedup threshold must be a number") {
			t.Errorf("dedup %q should trigger an error toast, got %q", v, hdr)
		}
	}
	// validation happens before any write: nothing persisted
	if f.lastSettingCat != "" || len(f.settingWrites) != 0 {
		t.Error("invalid dedup must not write anything")
	}
}

func TestUIProjectSettingsRememberError(t *testing.T) {
	f := &fakeMemory{settingErr: errTest}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/remember/:field", s.uiProjectSettingsRememberField)

	rec := voicePost(e, "/settings/remember/agent", "remember_agent=memory")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "backend unreachable") {
		t.Errorf("write failure should trigger an error toast, got %q", hdr)
	}
}

// TestUIProjectSettingsEditor covers a valid editor-agent inline save (HTMX →
// POST /settings/editor): the value stores as {"agentId": id} under
// editor/agent_id and surfaces a success toast.
func TestUIProjectSettingsEditor(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/editor", s.uiProjectSettingsEditor)

	rec := voicePost(e, "/settings/editor", "editor_agent=a1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("agent save should trigger a success toast, got %q", hdr)
	}
	if f.lastSettingCat != "editor" || f.lastSettingKey != "agent_id" {
		t.Errorf("write = %s/%s, want editor/agent_id", f.lastSettingCat, f.lastSettingKey)
	}
	if v, ok := f.lastSettingValue["agentId"].(string); !ok || v != "a1" {
		t.Errorf("editor value = %v, want agentId a1", f.lastSettingValue)
	}
}

// TestUIProjectSettingsEditorClear covers clearing the editor agent: an empty
// value deletes the stored editor/agent_id setting.
func TestUIProjectSettingsEditorClear(t *testing.T) {
	f := &fakeMemory{settings: map[string]map[string]map[string]any{
		"editor": {"agent_id": {"agentId": "a1"}},
	}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/editor", s.uiProjectSettingsEditor)

	rec := voicePost(e, "/settings/editor", "editor_agent=")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) {
		t.Errorf("clear should trigger a success toast, got %q", hdr)
	}
	if f.lastDeletedCat != "editor" || f.lastDeletedKey != "agent_id" {
		t.Errorf("clear should delete editor/agent_id, got %s/%s", f.lastDeletedCat, f.lastDeletedKey)
	}
}

// TestUIProjectSettingsEditorError covers a rejected editor-agent write
// returning an error toast.
func TestUIProjectSettingsEditorError(t *testing.T) {
	f := &fakeMemory{settingErr: errTest}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/editor", s.uiProjectSettingsEditor)

	rec := voicePost(e, "/settings/editor", "editor_agent=a1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "backend unreachable") {
		t.Errorf("write failure should trigger an error toast, got %q", hdr)
	}
}

// TestUIProjectSettingsEditorRender exercises the editor-agent selector on the
// General settings page: the dropdown renders (empty option = default agent)
// and a stored value pre-selects its agent.
func TestUIProjectSettingsEditorRender(t *testing.T) {
	f := &fakeMemory{
		project: &Project{ID: "p1", Name: "Home"},
		agents:  []AgentDefinitionSummary{{ID: "a9", Name: "editor-x", Enabled: true}},
		settings: map[string]map[string]map[string]any{
			"editor": {"agent_id": {"agentId": "a9"}},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/settings", s.uiProjectSettings)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Editor agent",
		`hx-post="/settings/editor"`, `name="editor_agent"`, `hx-swap="none"`,
		"None — use default agent",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor selector missing %q", want)
		}
	}
	if !strings.Contains(body, `value="a9" selected`) {
		t.Error("stored editor agent should render selected")
	}
}

// voicePost issues a form-encoded POST to a voice inline-save endpoint and
// returns the recorder.
func voicePost(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

// TestUIVoiceFieldStringSave covers a valid string-field inline save.
func TestUIVoiceFieldStringSave(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)

	rec := voicePost(e, "/settings/voice/exit_keywords", "exit_keywords=stop,+goodbye")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := f.settings["voice"]["exit_keywords"]["value"]; got != "stop, goodbye" {
		t.Errorf("stored = %v, want %q", got, "stop, goodbye")
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"success"`) || !strings.Contains(hdr, `"message":"Saved"`) {
		t.Errorf("success should trigger a success toast, got HX-Trigger %q", hdr)
	}
}

// TestUIVoiceFieldNumericSave covers a valid numeric-field inline save.
func TestUIVoiceFieldNumericSave(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)

	rec := voicePost(e, "/settings/voice/away_timeout", "away_timeout=20")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := f.settings["voice"]["away_timeout"]["value"]; got != "20" {
		t.Errorf("stored = %v, want %q", got, "20")
	}
}

// TestUIVoiceFieldInvalidNumeric covers a non-numeric delay/timeout: the
// handler returns an inline error and persists nothing.
func TestUIVoiceFieldInvalidNumeric(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)

	for _, v := range []string{"abc", "-1"} {
		rec := voicePost(e, "/settings/voice/endpoint_min_delay", "endpoint_min_delay="+v)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, want 200", rec.Code)
		}
		hdr := rec.Header().Get("HX-Trigger")
		if !strings.Contains(hdr, `"kind":"error"`) || !strings.Contains(hdr, "endpoint min delay must be a non-negative number") {
			t.Errorf("invalid %q should trigger an error toast, got HX-Trigger %q", v, hdr)
		}
	}
	if f.settings["voice"] != nil {
		t.Errorf("invalid numeric must not persist anything, got %v", f.settings["voice"])
	}
}

// TestUIVoiceFieldEmptyDeletes covers clearing a field: an empty value deletes
// the stored row so it falls back to the env default.
func TestUIVoiceFieldEmptyDeletes(t *testing.T) {
	f := &fakeMemory{settings: map[string]map[string]map[string]any{
		"voice": {"away_timeout": {"value": "20"}},
	}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)

	rec := voicePost(e, "/settings/voice/away_timeout", "away_timeout=")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if f.lastDeletedCat != "voice" || f.lastDeletedKey != "away_timeout" {
		t.Errorf("expected delete of voice/away_timeout, got %s/%s", f.lastDeletedCat, f.lastDeletedKey)
	}
}

// TestUIVoiceFieldBoolSave covers a toggle inline save.
func TestUIVoiceFieldBoolSave(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)

	rec := voicePost(e, "/settings/voice/enabled", "enabled=on")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := f.settings["voice"]["enabled"]["value"]; got != true {
		t.Errorf("stored = %v, want true", got)
	}
}

// TestUIVoiceFieldUnknownKey covers an unknown key returning an inline error.
func TestUIVoiceFieldUnknownKey(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)

	rec := voicePost(e, "/settings/voice/bogus", "bogus=x")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "Unknown voice setting") {
		t.Errorf("unknown key should trigger an error toast, got HX-Trigger %q", hdr)
	}
	if len(f.settingWrites) != 0 {
		t.Error("unknown key must not write anything")
	}
}

// TestUIVoiceFieldWriteError covers a rejected write returning an inline error.
func TestUIVoiceFieldWriteError(t *testing.T) {
	f := &fakeMemory{settingErr: errTest}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)

	rec := voicePost(e, "/settings/voice/goodbye_text", "goodbye_text=bye")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "backend unreachable") {
		t.Errorf("write failure should trigger an error toast, got HX-Trigger %q", hdr)
	}
}

// TestUIVoiceGroupEndpointDelays covers a valid endpoint-delays group save.
func TestUIVoiceGroupEndpointDelays(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/group/:group", s.uiProjectVoiceGroup)

	rec := voicePost(e, "/settings/voice/group/endpoint_delays", "endpoint_min_delay=0.4&endpoint_max_delay=3")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := f.settings["voice"]["endpoint_min_delay"]["value"]; got != "0.4" {
		t.Errorf("min = %v, want 0.4", got)
	}
	if got := f.settings["voice"]["endpoint_max_delay"]["value"]; got != "3" {
		t.Errorf("max = %v, want 3", got)
	}
}

// TestUIVoiceGroupEndpointDelaysInvalid covers min > max: the handler rejects
// and persists nothing (no partial write).
func TestUIVoiceGroupEndpointDelaysInvalid(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/group/:group", s.uiProjectVoiceGroup)

	rec := voicePost(e, "/settings/voice/group/endpoint_delays", "endpoint_min_delay=5&endpoint_max_delay=3")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, `"kind":"error"`) || !strings.Contains(hdr, "cannot exceed") {
		t.Errorf("min > max should trigger an error toast, got HX-Trigger %q", hdr)
	}
	if f.settings["voice"] != nil {
		t.Errorf("min > max must not persist anything, got %v", f.settings["voice"])
	}
}

// TestUIVoiceGroupTTSClearsModelVoice covers a provider change to a
// model/voice-less provider clearing the coupled fields in the same save.
func TestUIVoiceGroupTTSClearsModelVoice(t *testing.T) {
	f := &fakeMemory{settings: map[string]map[string]map[string]any{
		"voice": {"tts_model": {"value": "sonic-3.5"}, "tts_voice": {"value": "abc"}},
	}}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/group/:group", s.uiProjectVoiceGroup)

	rec := voicePost(e, "/settings/voice/group/tts", "tts_provider=client&tts_model=sonic-3.5&tts_voice=abc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if got := f.settings["voice"]["tts_provider"]["value"]; got != "client" {
		t.Errorf("provider = %v, want client", got)
	}
	if _, ok := f.settings["voice"]["tts_model"]; ok {
		t.Error("tts_model should be cleared when provider is client")
	}
	if _, ok := f.settings["voice"]["tts_voice"]; ok {
		t.Error("tts_voice should be cleared when provider is client")
	}
}

// TestUIVoiceGroupUnknown covers an unknown group returning an inline error.
func TestUIVoiceGroupUnknown(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice/group/:group", s.uiProjectVoiceGroup)

	rec := voicePost(e, "/settings/voice/group/bogus", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if hdr := rec.Header().Get("HX-Trigger"); !strings.Contains(hdr, "Unknown voice setting group") {
		t.Errorf("unknown group should trigger an error toast, got HX-Trigger %q", hdr)
	}
}

// helpers for pointer-typed fake data
func strPtr(s string) *string     { return &s }
func intPtr(n int) *int           { return &n }
func floatPtr(f float64) *float64 { return &f }
