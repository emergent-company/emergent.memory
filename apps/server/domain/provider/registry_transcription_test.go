package provider

import (
	"encoding/json"
	"testing"
)

func TestTranscriptionAPIForVendors(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		pt   ProviderType
		want TranscriptionAPI
	}{
		{ProviderOpenAI, TranscriptionAPIOpenAITranscriptions},
		{ProviderGeneric, TranscriptionAPIOpenAITranscriptions},
		{ProviderDeepSeek, ""},
		{ProviderCohere, ""},
		{ProviderAnthropic, ""},
	}
	for _, tt := range tests {
		if got := r.TranscriptionAPIFor(tt.pt); got != tt.want {
			t.Errorf("TranscriptionAPIFor(%q) = %q, want %q", tt.pt, got, tt.want)
		}
	}
}

func TestTranscriptionVendorHasTranscriptionModelType(t *testing.T) {
	r := NewRegistry()

	transcriptionVendors := []ProviderType{ProviderOpenAI, ProviderGeneric}
	for _, pt := range transcriptionVendors {
		def, err := r.Get(pt)
		if err != nil {
			t.Fatalf("Get(%q): %v", pt, err)
		}

		hasModelType := false
		for _, mt := range def.ModelTypes {
			if mt == ModelTypeTranscription {
				hasModelType = true
				break
			}
		}
		if !hasModelType {
			t.Errorf("vendor %q does not declare ModelTypeTranscription in ModelTypes", pt)
		}

		if _, ok := def.DefaultBaseURLs[ModelTypeTranscription]; !ok {
			t.Errorf("vendor %q has no DefaultBaseURLs[ModelTypeTranscription] entry", pt)
		}
	}

	// OpenAI is first-party: its transcription endpoint must be concrete.
	openai, err := r.Get(ProviderOpenAI)
	if err != nil {
		t.Fatalf("Get(openai): %v", err)
	}
	if url := openai.DefaultBaseURLs[ModelTypeTranscription]; url == "" {
		t.Errorf("openai has empty DefaultBaseURLs[ModelTypeTranscription]")
	}

	// Non-transcription vendors must not declare the model type.
	for _, pt := range []ProviderType{ProviderDeepSeek, ProviderCohere} {
		def, err := r.Get(pt)
		if err != nil {
			t.Fatalf("Get(%q): %v", pt, err)
		}
		for _, mt := range def.ModelTypes {
			if mt == ModelTypeTranscription {
				t.Errorf("vendor %q declares ModelTypeTranscription but has no TranscriptionAPI", pt)
			}
		}
	}
}

func TestDefinitionTranscriptionAPIJSON(t *testing.T) {
	def := &ProviderDefinition{
		Type:             ProviderOpenAI,
		TranscriptionAPI: TranscriptionAPIOpenAITranscriptions,
	}

	b, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundTripped map[string]any
	if err := json.Unmarshal(b, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got, ok := roundTripped["transcriptionApi"]; !ok || got != string(TranscriptionAPIOpenAITranscriptions) {
		t.Errorf("expected transcriptionApi key with value %q, got %v", TranscriptionAPIOpenAITranscriptions, roundTripped["transcriptionApi"])
	}

	// Empty TranscriptionAPI must be omitted.
	empty := &ProviderDefinition{Type: ProviderDeepSeek}
	b2, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	var m2 map[string]any
	if err := json.Unmarshal(b2, &m2); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if _, ok := m2["transcriptionApi"]; ok {
		t.Errorf("expected transcriptionApi to be omitted when empty, got %v", m2)
	}
}
