package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// withVoiceSupport sets the package-level capability for a test and restores
// the previous value when the test finishes.
func withVoiceSupport(t *testing.T, supported bool) {
	t.Helper()
	prev := voiceSupported.Load()
	setVoiceSupported(supported)
	t.Cleanup(func() { setVoiceSupported(prev) })
}

// TestSettingsSubNavVoiceCapability pins the rail behaviour: the Voice entry is
// present when the deployment supports voice and absent when it does not.
func TestSettingsSubNavVoiceCapability(t *testing.T) {
	t.Run("supported", func(t *testing.T) {
		withVoiceSupport(t, true)
		html := renderHTML(t, settingsSubNav("general", false))
		if !strings.Contains(html, `href="/settings/voice"`) || !strings.Contains(html, "Voice") {
			t.Error("Voice section should render when voice is supported")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		withVoiceSupport(t, false)
		html := renderHTML(t, settingsSubNav("general", false))
		for _, gone := range []string{`href="/settings/voice"`, ">Voice<"} {
			if strings.Contains(html, gone) {
				t.Errorf("voice-disabled rail must not contain %q", gone)
			}
		}
		// The rest of the rail stays intact.
		if !strings.Contains(html, `href="/settings/providers"`) {
			t.Error("voice-disabled rail lost unrelated sections")
		}
	})
}

// TestVoiceDisabledRoutesFailClosed asserts a VOICE_SUPPORTED=false deployment
// exposes no voice page, token, binding or device toggle.
func TestVoiceDisabledRoutesFailClosed(t *testing.T) {
	s := &Server{cfg: Config{VoiceDisabled: true}, memory: nil}
	e := echo.New()
	e.GET("/settings/voice", s.uiProjectVoiceSettings)
	e.POST("/settings/voice", s.uiProjectSettingsVoice)
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)
	e.POST("/settings/voice/group/:group", s.uiProjectVoiceGroup)
	e.POST("/api/token", s.mintToken)
	e.GET("/internal/voice-binding", s.voiceBindingHandler)
	e.GET("/api/project/config", s.projectConfig)

	cases := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/settings/voice", http.StatusNotFound},
		{http.MethodPost, "/settings/voice", http.StatusNotFound},
		{http.MethodPost, "/settings/voice/enabled", http.StatusNotFound},
		{http.MethodPost, "/settings/voice/group/tts", http.StatusNotFound},
		{http.MethodPost, "/api/token", http.StatusNotFound},
		{http.MethodGet, "/internal/voice-binding?room=r", http.StatusNotFound},
		{http.MethodGet, "/api/project/config", http.StatusOK},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d; body=%s", tc.method, tc.path, rec.Code, tc.want, rec.Body.String())
		}
		if tc.path == "/api/project/config" {
			var got map[string]bool
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode project config: %v", err)
			}
			if len(got) != 1 || got["voiceEnabled"] {
				t.Errorf("project config = %v, want exactly voiceEnabled=false", got)
			}
		}
	}
}

// TestVoiceDisabledOverridesStoredSetting pins that a stored enabled=true does
// not resurrect voice on a deployment without an SFU.
func TestVoiceDisabledOverridesStoredSetting(t *testing.T) {
	f := &fakeMemory{settings: map[string]map[string]map[string]any{
		"voice": {"enabled": {"value": true}},
	}}
	s := &Server{cfg: Config{VoiceDisabled: true}, memory: f}
	if s.voiceEnabled(context.Background()) {
		t.Error("voice must stay disabled when VOICE_SUPPORTED=false")
	}
}

// TestConfigVoiceSupportedEnv covers the env mapping: only an explicit false
// disables voice, preserving the zero-value/test default.
func TestConfigVoiceSupportedEnv(t *testing.T) {
	t.Run("default enabled", func(t *testing.T) {
		t.Setenv("VOICE_SUPPORTED", "")
		if LoadConfig().VoiceDisabled {
			t.Error("unset VOICE_SUPPORTED must keep voice enabled")
		}
	})
	t.Run("explicit false disables", func(t *testing.T) {
		t.Setenv("VOICE_SUPPORTED", "false")
		if !LoadConfig().VoiceDisabled {
			t.Error("VOICE_SUPPORTED=false must disable voice")
		}
	})
}
