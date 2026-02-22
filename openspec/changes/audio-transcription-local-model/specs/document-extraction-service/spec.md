## MODIFIED Requirements

### Requirement: Document Parsing Worker Integration

The DocumentParsingWorker SHALL route documents to the appropriate extraction handler based on file type: audio files to the Whisper transcription service, binary documents to Kreuzberg, and plain text files to direct storage reading.

#### Scenario: Plain text file direct storage

- **GIVEN** a parsing job for a .txt, .md, or .csv file
- **WHEN** the worker processes the job
- **THEN** the file content is read directly as UTF-8
- **AND** no call is made to the Kreuzberg or Whisper service
- **AND** the job metadata records `"extractionMethod": "plain_text"`

#### Scenario: Audio file Whisper routing

- **GIVEN** a parsing job for an audio file (mp3, wav, m4a, ogg, flac, aac, opus, webm audio)
- **WHEN** the worker processes the job
- **AND** `WHISPER_ENABLED` is `true`
- **THEN** the file is sent to the Whisper service for transcription
- **AND** the transcript text is stored in `kb.documents.content`
- **AND** the job metadata records `"extractionMethod": "whisper"`

#### Scenario: Complex document Kreuzberg routing

- **GIVEN** a parsing job for a PDF, DOCX, or image file
- **WHEN** the worker processes the job
- **THEN** the file is sent to Kreuzberg for extraction
- **AND** the extracted text is stored in `kb.documents`
- **AND** the job metadata records `"extractionMethod": "kreuzberg"`

#### Scenario: Kreuzberg disabled fallback

- **GIVEN** `KREUZBERG_ENABLED` is set to `false`
- **AND** a parsing job for a complex document is created
- **WHEN** the worker processes the job
- **THEN** the job fails with error "Document parsing service not available"
- **AND** the job status is set to `"failed"`

#### Scenario: Whisper disabled fallback

- **GIVEN** `WHISPER_ENABLED` is set to `false`
- **AND** a parsing job for an audio file is created
- **WHEN** the worker processes the job
- **THEN** the job fails with error "whisper transcription service is disabled"
- **AND** the job status is set to `"failed"`

#### Scenario: Audio MIME type detection by extension fallback

- **GIVEN** an audio file is uploaded with an ambiguous or missing MIME type
- **AND** the filename has a known audio extension (.mp3, .wav, .m4a, .ogg, .flac, .aac, .opus, .webm)
- **WHEN** the worker evaluates the routing for the job
- **THEN** the file is classified as audio and routed to the Whisper service

#### Scenario: Retry on transient failure

- **GIVEN** a parsing job fails due to a Kreuzberg or Whisper timeout
- **WHEN** `retry_count` is less than `max_retries` (default: 3)
- **THEN** the job is re-queued with incremented `retry_count`
- **AND** the next attempt uses exponential backoff
