# document-extraction Specification

## Purpose
Defines how the server extracts document text through the external xberg service: configuration and enablement, the extraction response envelope, supported OCR backends, health reporting of the xberg component, and pinning of the deployment image.

## Requirements

### Requirement: Server extracts documents via the xberg service
The server SHALL extract document text through the xberg HTTP service, configured via `XBERG_ENABLED`, `XBERG_SERVICE_URL`, `XBERG_SERVICE_TIMEOUT`, and `XBERG_MAX_FILE_SIZE_MB`. When `XBERG_ENABLED` is false, extraction SHALL be refused with a service-unavailable error.

#### Scenario: Enabled extraction calls xberg
- **WHEN** a binary document is submitted and `XBERG_ENABLED` is true
- **THEN** the server POSTs the file to `<XBERG_SERVICE_URL>/extract` as multipart `files` and returns the first extracted result

#### Scenario: Disabled extraction is refused
- **WHEN** extraction is attempted while `XBERG_ENABLED` is false
- **THEN** the server returns a service-unavailable error and does not call the service

#### Scenario: Plain text bypasses xberg
- **WHEN** the file is a plain-text MIME type or a known plain-text extension
- **THEN** the server does not route it to xberg

### Requirement: Server parses the xberg extraction envelope
The server SHALL decode the xberg `/extract` response envelope (`{"results": [...], "errors": [...], "summary": {...}}`) rather than a bare JSON array, and SHALL return the first entry of `results` on success.

#### Scenario: Successful envelope yields content
- **WHEN** xberg returns a non-empty `results` array
- **THEN** the server returns `results[0]` including its `content`, `metadata`, `tables`, and `images`

#### Scenario: Errors envelope is surfaced
- **WHEN** xberg returns an empty `results` array alongside one or more `errors`
- **THEN** the server returns an error whose message and detail summarise the per-input errors, preserving the HTTP status code

#### Scenario: Empty response without errors fails
- **WHEN** xberg returns an empty `results` array and no `errors`
- **THEN** the server returns an error stating that xberg returned empty results

### Requirement: Server supports xberg OCR backends
The server SHALL advertise OCR backends `tesseract`, `paddleocr`, `sceptre`, and `vlm`. The removed `easyocr` backend SHALL NOT be offered.

#### Scenario: EasyOCR is not an accepted backend
- **WHEN** OCR backend options are enumerated
- **THEN** `easyocr` is absent and the supported set is `tesseract`, `paddleocr`, `sceptre`, `vlm`

### Requirement: Health reports the xberg component
The health endpoint SHALL report the document extraction dependency under the component name `xberg`, deriving its state from the xberg `/health` probe and honouring the disabled state.

#### Scenario: Disabled reports healthy
- **WHEN** `XBERG_ENABLED` is false
- **THEN** the `xberg` health component reports `healthy` with a `disabled` message

#### Scenario: Unreachable reports unhealthy
- **WHEN** the xberg service cannot be reached
- **THEN** the `xberg` health component reports `unhealthy` with the failure message

### Requirement: Deployment pins the xberg image
The CLI installer and the self-hosted compose files SHALL pin the extraction image `ghcr.io/xberg-io/xberg:1.3.0` consistently, exposing it as the `xberg` compose service (`memory-xberg` container) and configuring the server with `XBERG_SERVICE_URL`/`XBERG_ENABLED`.

#### Scenario: Static copies match the CLI constant
- **WHEN** the image referenced by the CLI `XbergImage` constant is compared with the self-hosted compose and install-online copies
- **THEN** all copies reference `ghcr.io/xberg-io/xberg:1.3.0`

#### Scenario: Rendered compose names the xberg service
- **WHEN** the installer renders the compose template
- **THEN** it contains an `xberg:` service with container `memory-xberg`, `RUST_LOG` from `XBERG_LOG_LEVEL`, and the server's `XBERG_SERVICE_URL` pointing at `http://xberg:8000`

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
