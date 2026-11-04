package extraction

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/asr"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Environment variables that opt the audio branch of the document pipeline into
// a vendor speech-to-text service. The in-house Whisper client is the DEFAULT
// and is used byte-for-byte unchanged unless ASR_PROVIDER names a registered
// vendor whose registry definition declares a TranscriptionAPI. This mirrors
// the search rerank opt-in (SEARCH_RERANK_*) so both vendor dimensions are
// selected the same way.
//
// ASR_PROVIDER selects the vendor by its registry type (e.g. "openai", or
// "generic" for any OpenAI-compatible relay such as Groq). ASR_MODEL selects the
// transcription model; an empty model lets the adapter apply its own default
// (openai-transcriptions -> "whisper-1"). ASR_API_KEY / ASR_BASE_URL reuse the
// provider credential plumbing; ASR_LANGUAGE and ASR_RESPONSE_FORMAT are
// optional request hints.
const (
	envASRProvider       = "ASR_PROVIDER"
	envASRModel          = "ASR_MODEL"
	envASRAPIKey         = "ASR_API_KEY"
	envASRBaseURL        = "ASR_BASE_URL"
	envASRLanguage       = "ASR_LANGUAGE"
	envASRResponseFormat = "ASR_RESPONSE_FORMAT"
)

// vendorASR bundles a configured vendor transcription adapter with the
// per-request hints resolved from the environment.
type vendorASR struct {
	transcriber    asr.Transcriber
	provider       string
	language       string
	responseFormat string
}

// vendorTranscriberFromEnv builds the opt-in vendor speech-to-text adapter from
// environment config, resolving the vendor through the registry (its
// TranscriptionAPI selects the wire shape) and reusing the provider credential
// plumbing. It returns nil when unconfigured or when construction fails;
// failures are logged and never fatal, so the default Whisper path stays in
// place.
func vendorTranscriberFromEnv(registry *provider.Registry, log *slog.Logger) *vendorASR {
	name := strings.TrimSpace(os.Getenv(envASRProvider))
	if name == "" {
		return nil
	}

	pt := provider.ProviderType(name)
	def, err := registry.Get(pt)
	if err != nil || def.TranscriptionAPI == "" {
		log.Warn("vendor ASR disabled: vendor does not support transcription", slog.String("provider", name))
		return nil
	}

	cred := &provider.ResolvedCredential{
		Provider: pt,
		APIKey:   os.Getenv(envASRAPIKey),
		BaseURL:  os.Getenv(envASRBaseURL),
		Extra:    map[string]string{},
	}

	tr, err := provider.NewTranscriber(def, cred, os.Getenv(envASRModel))
	if err != nil {
		log.Warn("vendor ASR disabled: failed to build transcriber", logger.Error(err))
		return nil
	}

	log.Info("vendor ASR enabled",
		slog.String("provider", name),
		slog.String("model", os.Getenv(envASRModel)),
	)

	return &vendorASR{
		transcriber:    tr,
		provider:       name,
		language:       os.Getenv(envASRLanguage),
		responseFormat: os.Getenv(envASRResponseFormat),
	}
}

// extractWithVendorASR downloads an audio file and transcribes it with the
// configured vendor adapter. initialPrompt optionally seeds the decoder (the
// same per-project whisper_initial_prompt reused by the Whisper path) and is
// only sent by adapters that support a prompt field.
func (w *DocumentParsingWorker) extractWithVendorASR(ctx context.Context, storageKey, filename, mimeType, initialPrompt string) (string, error) {
	if w.asr == nil || w.asr.transcriber == nil {
		return "", fmt.Errorf("vendor ASR transcription service is not configured")
	}

	content, err := w.downloadFile(ctx, storageKey)
	if err != nil {
		return "", fmt.Errorf("download file: %w", err)
	}

	res, err := w.asr.transcriber.Transcribe(ctx, content, asr.Options{
		Filename:       filename,
		MIMEType:       mimeType,
		Language:       w.asr.language,
		ResponseFormat: w.asr.responseFormat,
		Prompt:         initialPrompt,
	})
	if err != nil {
		return "", fmt.Errorf("vendor ASR transcription: %w", err)
	}

	return res.Text, nil
}
