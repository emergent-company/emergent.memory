## ADDED Requirements

### Requirement: Transcription API is a registry dimension

The vendor registry SHALL describe speech-to-text (transcription) as an
orthogonal dimension on each vendor definition, separate from the chat wire
protocol and from rerank. A `ProviderDefinition` SHALL carry a
`TranscriptionAPI` value naming the vendor's transcription wire shape, and the
registry SHALL expose a `TranscriptionAPIFor` lookup. An empty `TranscriptionAPI`
SHALL mean the vendor does not support transcription.

#### Scenario: Vendor declares transcription support

- **WHEN** a vendor supports transcription
- **THEN** its registry definition declares a non-empty `TranscriptionAPI`, and `TranscriptionAPIFor` returns it

#### Scenario: Vendor without transcription support

- **WHEN** a vendor does not support transcription
- **THEN** its `TranscriptionAPI` is empty and `TranscriptionAPIFor` returns the empty value

#### Scenario: Transcription shape is independent of chat protocol and rerank

- **WHEN** a vendor speaks one chat protocol, no rerank shape, and an OpenAI-compatible transcription shape
- **THEN** `Protocol`, `RerankAPI`, and `TranscriptionAPI` are declared independently and none constrains the others

### Requirement: Transcription adapters are selected from the registry definition

The system SHALL select a transcription adapter from the vendor's registry
definition (`TranscriptionAPI`) rather than from a hardcoded per-vendor switch,
and SHALL reuse the existing credential plumbing — the bearer credential, the
default transcription base URL, per-vendor extra fields, and extra headers —
when constructing the adapter. A vendor whose `TranscriptionAPI` is empty SHALL
be rejected with an error.

#### Scenario: Vendor maps to its adapter

- **WHEN** a transcriber is built for a vendor declaring a `TranscriptionAPI`
- **THEN** the adapter matching that `TranscriptionAPI` is used and the request hits that vendor's wire path

#### Scenario: Unsupported vendor rejected

- **WHEN** a transcriber is requested for a vendor with an empty `TranscriptionAPI`
- **THEN** construction fails with an actionable error

### Requirement: OpenAI-compatible transcription adapter

The system SHALL provide an OpenAI-compatible transcription adapter that issues
a `multipart/form-data` `POST` to `{base}/audio/transcriptions` with the audio
as the `file` field and the model as the `model` field, sending optional
`language`, `response_format`, and `prompt` fields only when set, authenticating
with `Authorization: Bearer <key>`, and applying any configured extra headers.
It SHALL parse the `json` and `verbose_json` responses (`text`, `language`,
`duration`) and return plain text for `text`/`srt`/`vtt`. An empty audio payload
SHALL be rejected before any request is issued.

#### Scenario: Transcription request carries the documented fields

- **WHEN** an OpenAI-compatible transcription is requested with a language hint
- **THEN** the request is a multipart POST to `/audio/transcriptions` carrying `file` and `model` plus the `language` field

#### Scenario: Optional fields are omitted when unset

- **WHEN** no language, response format, or prompt is supplied
- **THEN** those form fields are absent from the request

#### Scenario: JSON response is decoded

- **WHEN** the vendor returns `{"text":"...","language":"en","duration":1.5}`
- **THEN** the adapter returns the text, language, and duration

#### Scenario: Empty audio is rejected

- **WHEN** the audio payload is empty
- **THEN** the adapter returns an error without issuing an HTTP request
