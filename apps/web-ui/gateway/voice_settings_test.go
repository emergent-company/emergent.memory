package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestResolveVoiceSettingsDefaults asserts that with no stored settings the
// resolved config is the env defaults, Enabled defaults to true, and
// STTProvider defaults to deepgram.
func TestResolveVoiceSettingsDefaults(t *testing.T) {
	cfg := Config{
		TTSProvider:          "cartesia",
		CartesiaModel:        "sonic-3.5",
		CartesiaVoice:        "9626c31c-bec5-4cca-baa8-f8ba9e84c8bc",
		DeepgramModel:        "nova-3",
		DeepgramLang:         "multi",
		LiveKitURL:           "ws://localhost:7880",
		LiveKitPublicURL:     "wss://gw.example.com",
		ExitKeywords:         "stop,goodbye,end",
		GoodbyeText:          "Goodbye.",
		UserAwayTimeout:      "20",
		EndpointMinDelay:     "0.4",
		EndpointMaxDelay:     "3",
		AllowInterruptions:   true,
		PreemptiveGeneration: false,
		CartesiaAPIKey:       "ck",
		DeepgramAPIKey:       "dk",
		LiveKitAPISecret:     "ls",
	}
	v := resolveVoiceSettings(context.Background(), cfg, &fakeMemory{})
	if !v.Enabled {
		t.Error("enabled should default to true")
	}
	if v.STTProvider != defaultSTTProvider {
		t.Errorf("STTProvider = %q, want %q", v.STTProvider, defaultSTTProvider)
	}
	if v.TTSProvider != "cartesia" || v.TTSModel != "sonic-3.5" || v.TTSVoice != "9626c31c-bec5-4cca-baa8-f8ba9e84c8bc" {
		t.Errorf("tts defaults wrong: %+v", v)
	}
	if v.STTModel != "nova-3" || v.STTLanguage != "multi" {
		t.Errorf("stt defaults wrong: %+v", v)
	}
	if v.LiveKitURL != "ws://localhost:7880" || v.LiveKitPublicURL != "wss://gw.example.com" {
		t.Errorf("livekit defaults wrong: %+v", v)
	}
	if v.ExitKeywords != "stop,goodbye,end" || v.GoodbyeText != "Goodbye." {
		t.Errorf("turn-tuning defaults wrong: %+v", v)
	}
	if v.AwayTimeout != "20" || v.EndpointMinDelay != "0.4" || v.EndpointMaxDelay != "3" {
		t.Errorf("delay defaults wrong: %+v", v)
	}
	if !v.AllowInterruptions || v.PreemptiveGeneration {
		t.Errorf("toggle defaults wrong: %+v", v)
	}
	if !v.CartesiaKeySet || !v.DeepgramKeySet || !v.LiveKitSecretSet {
		t.Errorf("secret flags should reflect env: %+v", v)
	}
}

// TestResolveVoiceSettingsOverlay asserts that stored project settings win
// over env defaults for the overlaid keys (strings when non-empty, bools
// always), while unset keys fall back to env defaults.
func TestResolveVoiceSettingsOverlay(t *testing.T) {
	f := &fakeMemory{settings: map[string]map[string]map[string]any{
		"voice": {
			"enabled":             {"value": false},
			"tts_provider":        {"value": "client"},
			"livekit_public_url":  {"value": "wss://x"},
			"allow_interruptions": {"value": false},
			"stt_model":           {"value": "nova-2"},
			"goodbye_text":        {"value": ""}, // empty stored string is ignored
		},
	}}
	cfg := Config{
		TTSProvider:          "cartesia",
		CartesiaModel:        "sonic-3.5",
		DeepgramModel:        "nova-3",
		LiveKitURL:           "ws://internal:7880",
		AllowInterruptions:   true,
		PreemptiveGeneration: true,
	}
	v := resolveVoiceSettings(context.Background(), cfg, f)
	if v.Enabled {
		t.Error("stored enabled=false should win")
	}
	if v.TTSProvider != "client" {
		t.Errorf("stored tts_provider should win, got %q", v.TTSProvider)
	}
	if v.LiveKitPublicURL != "wss://x" {
		t.Errorf("stored livekit_public_url should win, got %q", v.LiveKitPublicURL)
	}
	if v.AllowInterruptions {
		t.Error("stored allow_interruptions=false should win")
	}
	if v.STTModel != "nova-2" {
		t.Errorf("stored stt_model should win, got %q", v.STTModel)
	}
	if v.TTSModel != "sonic-3.5" {
		t.Errorf("unset field should fall back to env, got %q", v.TTSModel)
	}
	if !v.PreemptiveGeneration {
		t.Error("unset bool should fall back to env default true")
	}
}

// TestResolveVoiceSettingsReadError asserts a failed settings read degrades
// to the env default (best-effort) instead of erroring.
func TestResolveVoiceSettingsReadError(t *testing.T) {
	f := &fakeMemory{settingErr: errTest}
	v := resolveVoiceSettings(context.Background(), Config{TTSProvider: "cartesia"}, f)
	if !v.Enabled {
		t.Error("read error should keep the default enabled=true")
	}
	if v.TTSProvider != "cartesia" {
		t.Errorf("read error should keep the env default, got %q", v.TTSProvider)
	}
}

// TestPersistVoiceSettings asserts booleans are always written explicitly,
// non-empty strings are stored, empty strings clear via delete, and secrets
// are never written.
func TestPersistVoiceSettings(t *testing.T) {
	f := &fakeMemory{}
	v := voiceSettings{
		Enabled:              false,
		TTSProvider:          "cartesia",
		TTSModel:             "",
		TTSVoice:             "9626c31c-bec5-4cca-baa8-f8ba9e84c8bc",
		STTProvider:          "deepgram",
		STTModel:             "nova-3",
		STTLanguage:          "multi",
		LiveKitURL:           "ws://localhost:7880",
		LiveKitPublicURL:     "",
		ExitKeywords:         "stop,end",
		GoodbyeText:          "",
		AwayTimeout:          "20",
		EndpointMinDelay:     "0.4",
		EndpointMaxDelay:     "3",
		AllowInterruptions:   true,
		PreemptiveGeneration: false,
	}
	if err := persistVoiceSettings(context.Background(), f, v); err != nil {
		t.Fatal(err)
	}
	// bools written explicitly, including false
	for _, kv := range []struct {
		key  string
		want bool
	}{
		{voiceKeyEnabled, false},
		{voiceKeyAllowInterruptions, true},
		{voiceKeyPreemptiveGeneration, false},
	} {
		got, ok := f.settings["voice"][kv.key]["value"].(bool)
		if !ok || got != kv.want {
			t.Errorf("%s = %v (type %T), want %v", kv.key, f.settings["voice"][kv.key]["value"], f.settings["voice"][kv.key]["value"], kv.want)
		}
	}
	// non-empty strings stored
	for _, kv := range []struct{ key, want string }{
		{voiceKeyTTSProvider, "cartesia"},
		{voiceKeyTTSVoice, "9626c31c-bec5-4cca-baa8-f8ba9e84c8bc"},
		{voiceKeySTTProvider, "deepgram"},
		{voiceKeySTTModel, "nova-3"},
		{voiceKeySTTLanguage, "multi"},
		{voiceKeyLiveKitURL, "ws://localhost:7880"},
		{voiceKeyExitKeywords, "stop,end"},
		{voiceKeyAwayTimeout, "20"},
		{voiceKeyEndpointMinDelay, "0.4"},
		{voiceKeyEndpointMaxDelay, "3"},
	} {
		got, ok := f.settings["voice"][kv.key]["value"].(string)
		if !ok || got != kv.want {
			t.Errorf("%s stored = %v, want %q", kv.key, f.settings["voice"][kv.key]["value"], kv.want)
		}
	}
	// empty strings cleared via delete
	for _, key := range []string{voiceKeyTTSModel, voiceKeyLiveKitPublicURL, voiceKeyGoodbyeText} {
		if _, ok := f.settings["voice"][key]; ok {
			t.Errorf("empty %s should be deleted, still present", key)
		}
	}
	// the last delete is goodbye_text (second-to-last string in write order)
	if f.lastDeletedCat != voiceCategory || f.lastDeletedKey != voiceKeyGoodbyeText {
		t.Errorf("last delete = %s/%s, want %s/%s", f.lastDeletedCat, f.lastDeletedKey, voiceCategory, voiceKeyGoodbyeText)
	}
	// secrets never written
	secretKeys := map[string]bool{"cartesia_api_key": true, "deepgram_api_key": true, "livekit_api_secret": true}
	for _, w := range f.settingWrites {
		if w.Category != voiceCategory {
			t.Errorf("write outside voice category: %s/%s", w.Category, w.Key)
		}
		if secretKeys[w.Key] {
			t.Errorf("secret key written: %s", w.Key)
		}
	}
	if len(f.settingWrites) != 13 {
		t.Errorf("want 13 setting writes (3 bools + 10 strings), got %d", len(f.settingWrites))
	}
}

// TestUIProjectSettingsVoice exercises POST /settings/voice: a valid save
// persists the fields and redirects to ?updated=1; an emptied field triggers
// a delete.
func TestUIProjectSettingsVoice(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice", s.uiProjectSettingsVoice)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/voice", strings.NewReader(
		"voice_enabled=on&tts_provider=cartesia&tts_model=sonic-3.5&tts_voice=v1&stt_provider=deepgram&stt_model=nova-3&stt_language=multi&livekit_url=ws://lk:7880&livekit_public_url=&exit_keywords=stop&goodbye_text=&away_timeout=20&endpoint_min_delay=0.4&endpoint_max_delay=3&allow_interruptions=on",
	))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/voice?updated=1" {
		t.Errorf("redirect = %q, want /settings/voice?updated=1", loc)
	}
	if v, ok := f.settings["voice"]["enabled"]["value"].(bool); !ok || !v {
		t.Errorf("enabled = %v, want true (voice_enabled=on)", f.settings["voice"]["enabled"]["value"])
	}
	if v, ok := f.settings["voice"]["tts_model"]["value"].(string); !ok || v != "sonic-3.5" {
		t.Errorf("tts_model = %v, want sonic-3.5", f.settings["voice"]["tts_model"]["value"])
	}
	if v, ok := f.settings["voice"]["livekit_url"]["value"].(string); !ok || v != "ws://lk:7880" {
		t.Errorf("livekit_url = %v, want ws://lk:7880", f.settings["voice"]["livekit_url"]["value"])
	}
	// empty fields are cleared via delete, not stored
	if _, ok := f.settings["voice"]["livekit_public_url"]; ok {
		t.Error("empty livekit_public_url should be deleted")
	}
	if _, ok := f.settings["voice"]["goodbye_text"]; ok {
		t.Error("empty goodbye_text should be deleted")
	}
	// absent checkbox = explicit false
	if v, ok := f.settings["voice"]["preemptive_generation"]["value"].(bool); !ok || v {
		t.Errorf("preemptive_generation = %v, want false (not sent)", f.settings["voice"]["preemptive_generation"]["value"])
	}
}

// TestUIProjectSettingsVoiceInvalidDelay asserts a non-numeric or negative
// delay redirects with an error and persists nothing.
func TestUIProjectSettingsVoiceInvalidDelay(t *testing.T) {
	for _, tc := range []struct{ body, wantErr string }{
		{"voice_enabled=on&away_timeout=abc", "away+timeout+must+be+a+non-negative+number"},
		{"endpoint_min_delay=-1", "endpoint+min+delay+must+be+a+non-negative+number"},
		{"endpoint_max_delay=abc", "endpoint+max+delay+must+be+a+non-negative+number"},
	} {
		f := &fakeMemory{}
		s := &Server{cfg: Config{}, memory: f}
		e := echo.New()
		e.POST("/settings/voice", s.uiProjectSettingsVoice)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/voice", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/voice?err="+tc.wantErr {
			t.Errorf("invalid %q should redirect with the error, got %d %q", tc.body, rec.Code, rec.Header().Get("Location"))
		}
		if len(f.settingWrites) != 0 || f.lastDeletedCat != "" {
			t.Errorf("invalid %q must not persist anything, wrote %d deleted %s/%s", tc.body, len(f.settingWrites), f.lastDeletedCat, f.lastDeletedKey)
		}
	}
}

// TestUIProjectSettingsVoiceError asserts a persist failure redirects with
// the error (PRG) instead of crashing.
func TestUIProjectSettingsVoiceError(t *testing.T) {
	f := &fakeMemory{settingErr: errTest}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.POST("/settings/voice", s.uiProjectSettingsVoice)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/voice", strings.NewReader("voice_enabled=on&away_timeout=20"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/voice?err=backend+unreachable" {
		t.Errorf("persist failure should redirect with the error, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// TestVoiceEnabled asserts the Server.voiceEnabled helper resolves the
// project's stored enabled flag (defaulting to on).
func TestVoiceEnabled(t *testing.T) {
	s := &Server{cfg: Config{}, memory: &fakeMemory{}}
	if !s.voiceEnabled(context.Background()) {
		t.Error("no stored setting should default to enabled")
	}
	disabled := &fakeMemory{settings: map[string]map[string]map[string]any{
		"voice": {"enabled": {"value": false}},
	}}
	s = &Server{cfg: Config{}, memory: disabled}
	if s.voiceEnabled(context.Background()) {
		t.Error("stored enabled=false should report disabled")
	}
}
