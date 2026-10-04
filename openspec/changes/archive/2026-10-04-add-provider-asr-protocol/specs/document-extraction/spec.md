## ADDED Requirements

### Requirement: Audio transcription defaults to the in-house Whisper client

The document parsing worker's audio branch SHALL transcribe audio with the
in-house Whisper client by default. When no vendor transcription provider is
configured, the Whisper path SHALL be used unchanged, and a newly created worker
SHALL hold no vendor transcription adapter.

#### Scenario: No vendor ASR configured uses Whisper

- **WHEN** `ASR_PROVIDER` is unset
- **THEN** the audio branch uses the existing Whisper client and no vendor adapter is constructed

#### Scenario: Unusable vendor configuration falls back to Whisper

- **WHEN** `ASR_PROVIDER` names an unknown vendor or a registered vendor that does not declare a `TranscriptionAPI`
- **THEN** no vendor adapter is constructed, the failure is logged, and the Whisper default remains in effect

### Requirement: Vendor speech-to-text is an opt-in alternative

The document parsing worker SHALL support an opt-in vendor speech-to-text
adapter selected through the provider registry. When `ASR_PROVIDER` names a
registered vendor that declares a `TranscriptionAPI`, the audio branch SHALL
transcribe with that adapter instead of Whisper, and SHALL record the active
path in the parsing job metadata (`asr:<vendor>` versus `whisper`).

#### Scenario: Configured vendor transcribes audio

- **WHEN** `ASR_PROVIDER` names a transcription-capable vendor
- **THEN** the audio branch downloads the file, transcribes it through the registry-selected adapter, and records `asr:<vendor>` as the extraction method

#### Scenario: Vendor path reuses the per-project prompt

- **WHEN** the project carries a `whisper_initial_prompt` and the vendor adapter is active
- **THEN** that prompt is passed to the adapter's optional prompt field

#### Scenario: No migration is required

- **WHEN** vendor transcription is selected
- **THEN** selection is driven entirely by opt-in environment configuration and existing provider credential plumbing, and persists nothing new
