## ADDED Requirements

### Requirement: OpenAI-compatible provider configuration via environment variables
The system SHALL support configuring an OpenAI-compatible LLM endpoint using three environment variables: `OPENAI_BASE_URL` (the base URL of the compatible API, e.g. `http://localhost:11434/v1`), `OPENAI_API_KEY` (the API key, which MAY be a placeholder such as `local` for keyless local servers), and `LLM_MODEL` (the model name to request from the endpoint, e.g. `kvasir`, `llama3`, `mistral`).

#### Scenario: Server starts with OpenAI-compatible env vars set
- **WHEN** `OPENAI_BASE_URL`, `OPENAI_API_KEY`, and `LLM_MODEL` are all set in the environment
- **THEN** the server SHALL initialize successfully and `LLMConfig.IsEnabled()` SHALL return true

#### Scenario: Server starts with only OPENAI_BASE_URL set (no API key)
- **WHEN** `OPENAI_BASE_URL` is set but `OPENAI_API_KEY` is empty
- **THEN** the server SHALL initialize successfully, sending requests without an Authorization header

#### Scenario: Server starts with only OPENAI_BASE_URL set (no model)
- **WHEN** `OPENAI_BASE_URL` is set but `LLM_MODEL` is empty
- **THEN** the server SHALL return an error when attempting to create a model: "model name is required"

### Requirement: OpenAI-compatible provider takes priority over Google backends
When `OPENAI_BASE_URL` is configured, the system SHALL use the OpenAI-compatible provider for all LLM calls, regardless of whether Google AI or Vertex AI credentials are also present.

#### Scenario: Both OPENAI_BASE_URL and GOOGLE_API_KEY are set
- **WHEN** both `OPENAI_BASE_URL` and `GOOGLE_API_KEY` are set in the environment
- **THEN** the system SHALL route all agent LLM calls to the OpenAI-compatible endpoint
- **THEN** the Google AI credentials SHALL be ignored for generative model calls

#### Scenario: Both OPENAI_BASE_URL and GCP credentials are set
- **WHEN** both `OPENAI_BASE_URL` and `GCP_PROJECT_ID`+`VERTEX_AI_LOCATION` are set
- **THEN** the system SHALL route all agent LLM calls to the OpenAI-compatible endpoint

### Requirement: OpenAI Chat Completions wire protocol
The system SHALL communicate with the configured endpoint using the OpenAI Chat Completions API format (`POST /v1/chat/completions`), sending a JSON body with `model`, `messages`, and `max_tokens` fields, and parsing the response's `choices[0].message.content` as the model output.

#### Scenario: Successful generation request
- **WHEN** an agent calls `ModelFactory.CreateModel` and the OpenAI-compatible provider is configured
- **THEN** the system SHALL POST to `{OPENAI_BASE_URL}/chat/completions` with the correct JSON body
- **THEN** the system SHALL set the `Authorization: Bearer {OPENAI_API_KEY}` header when `OPENAI_API_KEY` is non-empty
- **THEN** the system SHALL return the content from `choices[0].message.content` as the LLM response

#### Scenario: Endpoint returns an error status
- **WHEN** the OpenAI-compatible endpoint returns a non-2xx HTTP status
- **THEN** the system SHALL return an error containing the HTTP status code and response body

#### Scenario: Endpoint is unreachable
- **WHEN** the OpenAI-compatible endpoint is not reachable (connection refused, timeout)
- **THEN** the system SHALL return a descriptive error wrapping the underlying network error

### Requirement: ADK message role mapping
The system SHALL map ADK message roles to OpenAI roles when constructing Chat Completions requests: ADK `user` → OpenAI `user`, ADK `model` → OpenAI `assistant`, ADK `system` → OpenAI `system`. Multi-part text messages SHALL be concatenated into a single `content` string.

#### Scenario: Multi-turn conversation with system prompt
- **WHEN** an agent sends a request with system, user, and model messages
- **THEN** the Chat Completions request SHALL contain messages with roles `system`, `user`, and `assistant` respectively

#### Scenario: Multi-part text message
- **WHEN** an ADK message contains multiple text parts
- **THEN** the parts SHALL be concatenated with newlines into a single `content` string

### Requirement: JSON mode for structured extraction
The system SHALL include `response_format: {"type": "json_object"}` in Chat Completions requests when the ADK `GenerateContentConfig` specifies `ResponseMIMEType: "application/json"`, to improve structured output reliability on models that support JSON mode.

#### Scenario: Extraction request with JSON response type
- **WHEN** an agent calls `CreateModelWithName` with a config that has `ResponseMIMEType: "application/json"`
- **THEN** the Chat Completions request SHALL include `"response_format": {"type": "json_object"}`

#### Scenario: Regular generation request without JSON mode
- **WHEN** an agent calls `CreateModelWithName` without specifying `ResponseMIMEType`
- **THEN** the Chat Completions request SHALL NOT include a `response_format` field

### Requirement: LLM_MODEL env var for model name
The system SHALL read the `LLM_MODEL` environment variable as the model name when `OPENAI_BASE_URL` is configured. This variable SHALL override the `VERTEX_AI_MODEL` default for OpenAI-compatible backends only.

#### Scenario: LLM_MODEL is set with OPENAI_BASE_URL
- **WHEN** `LLM_MODEL=kvasir` and `OPENAI_BASE_URL=http://localhost:8001/v1` are both set
- **THEN** all Chat Completions requests SHALL use `"model": "kvasir"`

#### Scenario: LLM_MODEL is not set, OPENAI_BASE_URL is set
- **WHEN** `OPENAI_BASE_URL` is set but `LLM_MODEL` is empty
- **THEN** `ModelFactory.CreateModel()` SHALL return an error: "model name is required"

### Requirement: openai-compatible ProviderType registration
The system SHALL register `openai-compatible` as a known `ProviderType` constant in `domain/provider/entity.go` and in the provider registry, with credential fields `base_url` (required, non-secret) and `api_key` (optional, secret). This enables consistent logging, tracing, and future DB-stored credential support.

#### Scenario: Provider type appears in registry
- **WHEN** the provider registry is queried for supported providers
- **THEN** `openai-compatible` SHALL appear as a registered provider type with its credential field definitions

#### Scenario: Usage tracking logs provider type
- **WHEN** an LLM call is made via the OpenAI-compatible adapter
- **THEN** debug logs SHALL include `provider=openai-compatible` and the model name
