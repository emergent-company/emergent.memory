package provider

import (
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/asr"
)

// transcriptionAdapterFor maps the registry's TranscriptionAPI dimension onto
// the concrete pkg/asr adapter identifier. Keeping the mapping here (rather
// than in the registry) means the registry stays free of adapter dependencies
// while still being the single source of truth for vendor -> wire-shape
// selection.
func transcriptionAdapterFor(api TranscriptionAPI) (asr.API, bool) {
	switch api {
	case TranscriptionAPIOpenAITranscriptions:
		return asr.APIOpenAITranscriptions, true
	default:
		return "", false
	}
}

// NewTranscriber builds a vendor speech-to-text adapter for a registry
// definition using resolved credential material. It reuses the same auth +
// extra-config plumbing as the chat/embedding/rerank paths:
//
//   - the bearer credential comes from cred.APIKey;
//   - header-declared extra fields come from cred.ExtraHeaders;
//   - a nil/empty cred.BaseURL falls back to the definition's transcription
//     default base URL (DefaultBaseURLs[ModelTypeTranscription]);
//   - when neither is set, construction fails: a vendor with no base URL must
//     not silently inherit the adapter's built-in default (which would send a
//     relay/vendor key to that default host).
//
// model selects the transcription model; an empty model lets the vendor adapter
// apply its own default.
func NewTranscriber(def *ProviderDefinition, cred *ResolvedCredential, model string) (asr.Transcriber, error) {
	if def == nil {
		return nil, fmt.Errorf("transcription: nil provider definition")
	}
	api, ok := transcriptionAdapterFor(def.TranscriptionAPI)
	if !ok {
		return nil, fmt.Errorf("transcription: provider %s does not support transcription", def.Type)
	}
	if cred == nil {
		return nil, fmt.Errorf("transcription: nil credential for provider %s", def.Type)
	}

	baseURL := cred.BaseURL
	if baseURL == "" {
		baseURL = def.DefaultBaseURLs[ModelTypeTranscription]
	}
	if baseURL == "" {
		return nil, fmt.Errorf("transcription: provider %s requires a base URL", def.Type)
	}

	return asr.New(asr.Config{
		API:     api,
		BaseURL: baseURL,
		APIKey:  cred.APIKey,
		Model:   model,
		Headers: cred.ExtraHeaders,
	})
}

// NewTranscriberFor resolves the vendor definition for pt from the registry and
// builds a transcription adapter for it.
func (r *Registry) NewTranscriberFor(pt ProviderType, cred *ResolvedCredential, model string) (asr.Transcriber, error) {
	def, err := r.Get(pt)
	if err != nil {
		return nil, err
	}
	return NewTranscriber(def, cred, model)
}
