package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/asr"
)

func TestNewTranscriberSelection(t *testing.T) {
	openaiDef := &ProviderDefinition{
		Type:             ProviderOpenAI,
		TranscriptionAPI: TranscriptionAPIOpenAITranscriptions,
		DefaultBaseURLs:  map[ModelType]string{ModelTypeTranscription: "https://api.openai.com/v1"},
	}
	unsupportedDef := &ProviderDefinition{Type: ProviderDeepSeek}
	cred := &ResolvedCredential{Provider: ProviderOpenAI, APIKey: "sk-test"}

	t.Run("supported vendor returns adapter", func(t *testing.T) {
		tr, err := NewTranscriber(openaiDef, cred, "whisper-1")
		if err != nil {
			t.Fatalf("NewTranscriber: unexpected error: %v", err)
		}
		if tr == nil {
			t.Fatal("NewTranscriber returned nil transcriber")
		}
	})

	t.Run("unsupported vendor returns error", func(t *testing.T) {
		if _, err := NewTranscriber(unsupportedDef, cred, ""); err == nil {
			t.Fatal("expected error for vendor with no TranscriptionAPI")
		}
	})

	t.Run("nil definition returns error", func(t *testing.T) {
		if _, err := NewTranscriber(nil, cred, ""); err == nil {
			t.Fatal("expected error for nil definition")
		}
	})

	t.Run("nil credential returns error", func(t *testing.T) {
		if _, err := NewTranscriber(openaiDef, nil, ""); err == nil {
			t.Fatal("expected error for nil credential")
		}
	})
}

// TestNewTranscriberRequiresBaseURL guards the credential-leak hazard: a vendor
// that declares transcription but has no default transcription base URL (e.g.
// the generic OpenAI-compatible relay) must be rejected when the credential
// carries no BaseURL, instead of silently inheriting the adapter's built-in
// OpenAI endpoint (which would send the relay's key to OpenAI).
func TestNewTranscriberRequiresBaseURL(t *testing.T) {
	def := &ProviderDefinition{
		Type:             ProviderGeneric,
		TranscriptionAPI: TranscriptionAPIOpenAITranscriptions,
		// Deliberately no DefaultBaseURLs[ModelTypeTranscription].
	}

	if _, err := NewTranscriber(def, &ResolvedCredential{Provider: ProviderGeneric, APIKey: "k"}, ""); err == nil {
		t.Fatal("expected error when neither credential nor definition supplies a base URL")
	}

	// An explicit credential base URL satisfies the requirement.
	tr, err := NewTranscriber(def, &ResolvedCredential{Provider: ProviderGeneric, APIKey: "k", BaseURL: "https://relay.example/v1"}, "")
	if err != nil {
		t.Fatalf("NewTranscriber with credential BaseURL: %v", err)
	}
	if tr == nil {
		t.Fatal("NewTranscriber returned nil transcriber")
	}
}

func TestRegistryNewTranscriberFor(t *testing.T) {
	r := NewRegistry()
	cred := &ResolvedCredential{Provider: ProviderOpenAI, APIKey: "sk-test"}

	tr, err := r.NewTranscriberFor(ProviderOpenAI, cred, "whisper-1")
	if err != nil {
		t.Fatalf("NewTranscriberFor(openai): %v", err)
	}
	if tr == nil {
		t.Fatal("NewTranscriberFor(openai) returned nil")
	}

	if _, err := r.NewTranscriberFor(ProviderDeepSeek, cred, ""); err == nil {
		t.Fatal("expected error for a vendor that does not support transcription")
	}
}

// TestNewTranscriberUsesTranscriptionDefaultBaseURL proves the adapter falls
// back to the definition's DefaultBaseURLs[ModelTypeTranscription] when the
// credential carries no BaseURL, and that it hits the OpenAI-compatible
// /audio/transcriptions path.
func TestNewTranscriberUsesTranscriptionDefaultBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	def := &ProviderDefinition{
		Type:             ProviderOpenAI,
		TranscriptionAPI: TranscriptionAPIOpenAITranscriptions,
		DefaultBaseURLs:  map[ModelType]string{ModelTypeTranscription: srv.URL + "/v1"},
	}
	cred := &ResolvedCredential{Provider: ProviderOpenAI, APIKey: "sk-test"} // BaseURL deliberately empty

	tr, err := NewTranscriber(def, cred, "whisper-1")
	if err != nil {
		t.Fatalf("NewTranscriber: %v", err)
	}
	res, err := tr.Transcribe(context.Background(), []byte("audio-bytes"), asr.Options{Filename: "a.mp3"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "ok" {
		t.Errorf("Text = %q, want %q", res.Text, "ok")
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Errorf("request path = %q, want %q", gotPath, "/v1/audio/transcriptions")
	}
}

// TestNewTranscriberForwardsCredentialBaseURLAndHeaders proves the
// credential's BaseURL and ExtraHeaders plumbing reaches the adapter.
func TestNewTranscriberForwardsCredentialBaseURLAndHeaders(t *testing.T) {
	var gotExtra string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotExtra = req.Header.Get("X-Extra")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	def := &ProviderDefinition{
		Type:             ProviderOpenAI,
		TranscriptionAPI: TranscriptionAPIOpenAITranscriptions,
		DefaultBaseURLs:  map[ModelType]string{ModelTypeTranscription: "https://ignored.example/v1"},
	}
	cred := &ResolvedCredential{
		Provider:     ProviderOpenAI,
		APIKey:       "sk-test",
		BaseURL:      srv.URL,
		ExtraHeaders: map[string]string{"X-Extra": "yes"},
	}

	tr, err := NewTranscriber(def, cred, "whisper-1")
	if err != nil {
		t.Fatalf("NewTranscriber: %v", err)
	}
	if _, err := tr.Transcribe(context.Background(), []byte("audio"), asr.Options{Filename: "a.mp3"}); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotExtra != "yes" {
		t.Errorf("X-Extra header = %q, want %q (cred.BaseURL/ExtraHeaders not plumbed)", gotExtra, "yes")
	}
}
