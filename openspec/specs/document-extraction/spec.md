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
