package extraction

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/provider"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestVendorTranscriberFromEnvSelection is the selection contract test: only a
// registered vendor that declares a TranscriptionAPI yields an adapter; an
// unset ASR_PROVIDER, an unknown vendor, and a registered non-transcription
// vendor all leave the in-house Whisper path in place (nil).
func TestVendorTranscriberFromEnvSelection(t *testing.T) {
	registry := provider.NewRegistry()
	log := testLogger()

	t.Run("unset provider leaves whisper default", func(t *testing.T) {
		t.Setenv(envASRProvider, "")
		if got := vendorTranscriberFromEnv(registry, log); got != nil {
			t.Fatalf("expected nil transcriber when ASR_PROVIDER unset, got %+v", got)
		}
	})

	t.Run("unknown vendor yields nil", func(t *testing.T) {
		t.Setenv(envASRProvider, "not-a-real-vendor")
		if got := vendorTranscriberFromEnv(registry, log); got != nil {
			t.Fatalf("expected nil transcriber for unknown vendor, got %+v", got)
		}
	})

	t.Run("registered non-transcription vendor yields nil", func(t *testing.T) {
		t.Setenv(envASRProvider, string(provider.ProviderDeepSeek))
		if got := vendorTranscriberFromEnv(registry, log); got != nil {
			t.Fatalf("expected nil transcriber for vendor without TranscriptionAPI, got %+v", got)
		}
	})

	t.Run("openai selects the openai-compatible adapter", func(t *testing.T) {
		t.Setenv(envASRProvider, string(provider.ProviderOpenAI))
		t.Setenv(envASRModel, "whisper-1")
		t.Setenv(envASRAPIKey, "sk-test")
		t.Setenv(envASRLanguage, "en")
		t.Setenv(envASRResponseFormat, "json")

		got := vendorTranscriberFromEnv(registry, log)
		if got == nil {
			t.Fatal("expected a configured vendor transcriber for openai")
		}
		if got.transcriber == nil {
			t.Fatal("vendorASR.transcriber is nil")
		}
		if got.provider != string(provider.ProviderOpenAI) {
			t.Errorf("provider = %q, want %q", got.provider, provider.ProviderOpenAI)
		}
		if got.language != "en" {
			t.Errorf("language = %q, want %q", got.language, "en")
		}
		if got.responseFormat != "json" {
			t.Errorf("responseFormat = %q, want %q", got.responseFormat, "json")
		}
	})

	t.Run("generic openai-compatible vendor is selectable", func(t *testing.T) {
		t.Setenv(envASRProvider, string(provider.ProviderGeneric))
		t.Setenv(envASRBaseURL, "https://api.groq.com/openai/v1")
		if got := vendorTranscriberFromEnv(registry, log); got == nil {
			t.Fatal("expected generic provider to select openai-compatible transcription")
		}
	})

	t.Run("generic vendor without base URL falls back to whisper", func(t *testing.T) {
		// The generic vendor has no default transcription base URL; without an
		// explicit ASR_BASE_URL the adapter must not be built at all (instead of
		// silently posting to OpenAI with the configured key).
		t.Setenv(envASRProvider, string(provider.ProviderGeneric))
		t.Setenv(envASRAPIKey, "relay-key")
		t.Setenv(envASRBaseURL, "")
		if got := vendorTranscriberFromEnv(registry, log); got != nil {
			t.Fatalf("expected nil transcriber when generic vendor has no base URL, got %+v", got)
		}
	})
}

// TestDefaultAudioPathIsWhisper proves the default (no ASR_PROVIDER) path is
// unchanged: a worker built without a vendor transcriber has a nil ASR adapter
// and the vendor entry point refuses to run, so processJob falls through to the
// in-house Whisper client.
func TestDefaultAudioPathIsWhisper(t *testing.T) {
	w := &DocumentParsingWorker{}
	if w.asr != nil {
		t.Fatalf("default worker must not have a vendor ASR adapter, got %+v", w.asr)
	}
	if _, err := w.extractWithVendorASR(context.Background(), "key", "a.mp3", "audio/mpeg", ""); err == nil {
		t.Fatal("extractWithVendorASR must error when no vendor ASR is configured")
	}
}
