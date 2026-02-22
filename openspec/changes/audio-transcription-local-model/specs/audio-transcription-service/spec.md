## ADDED Requirements

### Requirement: Whisper Docker Service Availability

The system SHALL provide a Whisper transcription service as a Docker container that is available for audio parsing operations.

#### Scenario: Service starts and accepts requests

- **GIVEN** the Docker Compose stack is starting
- **WHEN** the Whisper container initializes
- **THEN** the service is ready to accept transcription requests within 60 seconds
- **AND** the `POST /asr` endpoint responds with HTTP 200 for a valid audio file

#### Scenario: Service recovers from restart

- **GIVEN** the Whisper container is restarted
- **WHEN** the container comes back online and the model is reloaded
- **THEN** the service resumes processing without data loss
- **AND** pending audio jobs can be retried by the document parsing worker

#### Scenario: Service disabled by configuration

- **GIVEN** `WHISPER_ENABLED` is set to `false`
- **WHEN** a document parsing job for an audio file is picked up by the worker
- **THEN** the job is marked as failed with message "whisper transcription service is disabled"
- **AND** an ERROR log is written with the job ID and audio MIME type
- **AND** no request is made to the Whisper service URL

### Requirement: Audio Transcription via HTTP API

The system SHALL transcribe audio files to plaintext by sending files to the Whisper service via HTTP multipart form upload.

#### Scenario: Transcribe MP3 audio file

- **GIVEN** an MP3 audio file (`audio/mpeg`) is uploaded for processing
- **WHEN** the file is sent to the Whisper service via `POST /asr?output=txt&task=transcribe`
- **THEN** the service returns HTTP 200 with the transcript as plain text in the response body
- **AND** processing completes within 600 seconds for files up to 500 MB

#### Scenario: Transcribe WAV audio file

- **GIVEN** a WAV audio file (`audio/wav` or `audio/x-wav`) is uploaded for processing
- **WHEN** the file is sent to the Whisper service
- **THEN** the service returns a plaintext transcript
- **AND** the transcript contains the spoken words from the recording

#### Scenario: Transcribe M4A audio file

- **GIVEN** an M4A audio file (`audio/mp4`) is uploaded for processing
- **WHEN** the file is sent to the Whisper service
- **THEN** the service returns a plaintext transcript

#### Scenario: Transcribe OGG audio file

- **GIVEN** an OGG audio file (`audio/ogg`) is uploaded for processing
- **WHEN** the file is sent to the Whisper service
- **THEN** the service returns a plaintext transcript

#### Scenario: Transcribe FLAC audio file

- **GIVEN** a FLAC audio file (`audio/flac`) is uploaded for processing
- **WHEN** the file is sent to the Whisper service
- **THEN** the service returns a plaintext transcript

#### Scenario: Transcribe OPUS audio file

- **GIVEN** an OPUS audio file (`audio/opus`) is uploaded for processing
- **WHEN** the file is sent to the Whisper service
- **THEN** the service returns a plaintext transcript

#### Scenario: Transcribe AAC audio file

- **GIVEN** an AAC audio file (`audio/aac`) is uploaded for processing
- **WHEN** the file is sent to the Whisper service
- **THEN** the service returns a plaintext transcript

#### Scenario: Transcribe WebM audio file

- **GIVEN** a WebM audio file (`audio/webm`) is uploaded for processing
- **WHEN** the file is sent to the Whisper service
- **THEN** the service returns a plaintext transcript

#### Scenario: Empty audio file handling

- **GIVEN** an audio file that contains no audio data or is corrupted
- **WHEN** the file is sent to the Whisper service
- **THEN** the job is marked as failed with a descriptive error message
- **AND** the failure is logged with the storage key and MIME type

#### Scenario: Timeout handling for large files

- **GIVEN** a large audio file is being transcribed
- **WHEN** processing exceeds the configured timeout (`WHISPER_SERVICE_TIMEOUT`, default 600 seconds)
- **THEN** the request is cancelled and the job is marked as failed
- **AND** an ERROR log is written with the duration and file size

### Requirement: Go Whisper Client

The server SHALL provide a `pkg/whisper` client package that encapsulates HTTP communication with the Whisper Docker service.

#### Scenario: Successful transcription call

- **GIVEN** the Whisper client is initialised with `WHISPER_ENABLED=true` and a valid `WHISPER_SERVICE_URL`
- **AND** the Whisper service is running
- **WHEN** `Transcribe(ctx, audioBytes, filename, mimeType)` is called
- **THEN** the method returns the plaintext transcript string
- **AND** the audio file is sent as multipart form data with field name `audio_file`
- **AND** the request includes query parameters `output=txt` and `task=transcribe`

#### Scenario: Language hint applied when configured

- **GIVEN** `WHISPER_LANGUAGE` is set to a non-empty value (e.g., `"en"`)
- **WHEN** `Transcribe()` is called
- **THEN** the request includes query parameter `language=<value>`

#### Scenario: Language auto-detected when not configured

- **GIVEN** `WHISPER_LANGUAGE` is empty
- **WHEN** `Transcribe()` is called
- **THEN** no `language` query parameter is sent
- **AND** the Whisper service performs automatic language detection

#### Scenario: Connection error handling

- **GIVEN** the Whisper service is unavailable
- **WHEN** `Transcribe()` is called
- **THEN** the method returns a wrapped error including the service URL
- **AND** the caller (document parsing worker) marks the job as failed

#### Scenario: Non-200 response handling

- **GIVEN** the Whisper service returns a non-200 HTTP status
- **WHEN** `Transcribe()` is called
- **THEN** the method returns an error including the status code and response body

### Requirement: Whisper Configuration via Environment Variables

The system SHALL support configuration of the Whisper service via environment variables.

#### Scenario: Default configuration values

- **GIVEN** no Whisper environment variables are set
- **WHEN** the server starts
- **THEN** the following defaults are applied:
  - `WHISPER_ENABLED`: `false`
  - `WHISPER_SERVICE_URL`: `http://localhost:9000`
  - `WHISPER_SERVICE_TIMEOUT`: `600000` (10 minutes in milliseconds)
  - `WHISPER_MODEL`: `base`
  - `WHISPER_LANGUAGE`: `""` (auto-detect)
  - `WHISPER_MAX_FILE_SIZE_MB`: `500`

#### Scenario: Custom model selection

- **GIVEN** `WHISPER_MODEL` is set to `medium`
- **WHEN** the Whisper Docker container starts
- **THEN** the medium Whisper model is loaded at startup
- **AND** transcription uses the medium model for improved accuracy

#### Scenario: Custom service URL

- **GIVEN** `WHISPER_SERVICE_URL` is set to a custom value
- **WHEN** the Whisper client makes transcription requests
- **THEN** requests are sent to the configured URL

#### Scenario: Audio file exceeds size limit

- **GIVEN** `WHISPER_MAX_FILE_SIZE_MB` is set to `500`
- **WHEN** an audio file larger than 500 MB is uploaded
- **THEN** the parsing job is marked as failed with message "audio file exceeds maximum size"
- **AND** no request is sent to the Whisper service

### Requirement: Transcript Stored in Knowledge Base

The system SHALL store audio transcripts in `kb.documents.content` and process them through the full downstream pipeline.

#### Scenario: Transcript fed into chunking pipeline

- **GIVEN** an audio file is successfully transcribed
- **WHEN** the document parsing worker receives the transcript text
- **THEN** the transcript is written to `kb.documents.content`
- **AND** `ChunkingService.RecreateChunks()` is called with the transcript
- **AND** chunks are created, embedded, and graph-extracted identically to text documents

#### Scenario: Extraction method recorded in job metadata

- **GIVEN** an audio file is transcribed via the Whisper client
- **WHEN** the document parsing job is marked as completed
- **THEN** the job metadata includes `"extractionMethod": "whisper"`
- **AND** the job metadata includes `"processingTimeMs"` with the elapsed time

#### Scenario: Failed transcription does not corrupt document

- **GIVEN** a transcription attempt fails after the document record exists
- **WHEN** the job is marked as failed
- **THEN** `kb.documents.content` is NOT updated (remains null or previous value)
- **AND** the document status is set to `"failed"`
- **AND** the job can be retried up to the maximum retry count
