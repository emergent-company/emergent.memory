## Purpose

The server supports the OpenAI provider (and OpenAI-compatible endpoints via its optional base-URL override) configured via environment variables. The provider is selected by the model name's `openai/` prefix, speaks the Chat Completions wire protocol with ADK role mapping and JSON mode for structured extraction, honors `OPENAI_MODEL`, and registers the `openai` provider type.

## Requirements

### Requirement: OpenAI-compatible provider configuration via environment variables
The system SHALL support configuring an OpenAI LLM endpoint using three environment variables: `OPENAI_BASE_URL` (optional; the base URL of the OpenAI-compatible API, defaulting to `https://api.openai.com/v1`), `OPENAI_API_KEY` (the API key, required on the env-var path), and `OPENAI_MODEL` (the model name to request, e.g. `openai/gpt-4o`; it MUST carry the `openai/` provider prefix).

#### Scenario: Server starts with OpenAI-compatible env vars set
- **WHEN** `OPENAI_API_KEY` and `OPENAI_MODEL` are set in the environment
- **THEN** the server SHALL initialize successfully and `LLMConfig.IsEnabled()` SHALL return true

#### Scenario: OPENAI_API_KEY is unset
- **WHEN** `OPENAI_API_KEY` is empty and a model is requested with an `openai/` prefix
- **THEN** `ModelFactory.CreateModelWithName` SHALL return an error: "OPENAI_API_KEY is not set"

#### Scenario: OPENAI_MODEL is unset
- **WHEN** `OPENAI_API_KEY` is set but `OPENAI_MODEL` is empty and the caller supplies no bare model portion
- **THEN** `ModelFactory.CreateModelWithName` SHALL return an error naming `OPENAI_MODEL`

### Requirement: OpenAI Chat Completions wire protocol
The system SHALL communicate with the configured endpoint using the OpenAI Chat Completions API format (`POST {baseURL}/chat/completions`), sending a JSON body with `model`, `messages`, and `max_tokens` fields, and parsing the response's `choices[0].message.content` as the model output.

#### Scenario: Successful generation request
- **WHEN** an agent requests an `openai/`-prefixed model and the OpenAI provider is configured
- **THEN** the system SHALL POST to `{baseURL}/chat/completions` with the correct JSON body
- **THEN** the system SHALL set the `Authorization: Bearer {OPENAI_API_KEY}` header when `OPENAI_API_KEY` is non-empty
- **THEN** the system SHALL return the content from `choices[0].message.content` as the LLM response

#### Scenario: Endpoint returns an error status
- **WHEN** the OpenAI endpoint returns a non-2xx HTTP status
- **THEN** the system SHALL return an error containing the HTTP status code and response body

#### Scenario: Endpoint is unreachable
- **WHEN** the OpenAI endpoint is not reachable (connection refused, timeout)
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

### Requirement: OpenAI provider selected by model-name prefix
A model name SHALL carry a provider prefix in the form `provider/model-name`. When the prefix is `openai`, the system SHALL construct an OpenAI-protocol model regardless of whether Google AI or Vertex AI credentials are also present; Google/Vertex credentials SHALL only be consulted when the prefix selects them (`google` or `google-vertex`).

#### Scenario: openai prefix routes to OpenAI
- **WHEN** a model name `openai/gpt-4o` is requested
- **THEN** the system SHALL create an OpenAI-protocol model, ignoring `GOOGLE_API_KEY` and `GCP_PROJECT_ID` even when set

#### Scenario: Model name without a provider prefix is rejected
- **WHEN** `CreateModelWithName` receives a model name with no `/` prefix
- **THEN** the system SHALL return an error stating the model name must include a provider prefix

### Requirement: OPENAI_MODEL env var for model name
The system SHALL read the `OPENAI_MODEL` environment variable as the fallback model name when an `openai/`-prefixed model is requested without a bare model portion. `OPENAI_MODEL` SHALL carry the `openai/` provider prefix, which the system strips when constructing the request.

#### Scenario: OPENAI_MODEL is set
- **WHEN** `OPENAI_MODEL=openai/kvasir` and `OPENAI_API_KEY` are set
- **THEN** all Chat Completions requests SHALL use `"model": "kvasir"`

#### Scenario: OPENAI_MODEL is not set
- **WHEN** `OPENAI_API_KEY` is set but `OPENAI_MODEL` is empty and no bare model is supplied
- **THEN** `ModelFactory.CreateModelWithName` SHALL return an error naming `OPENAI_MODEL`

### Requirement: OpenAI ProviderType registration
The system SHALL register `openai` as a known `ProviderType` constant (`ProviderOpenAI` in `domain/provider/entity.go`) and in the provider registry, with credential fields `api_key` (required, secret) and `base_url` (optional, non-secret). This enables consistent logging, tracing, and DB-stored credential support.

#### Scenario: Provider type appears in registry
- **WHEN** the provider registry is queried for supported providers
- **THEN** `openai` SHALL appear as a registered provider type with its credential field definitions

#### Scenario: Usage tracking logs provider type
- **WHEN** an LLM call is made via the OpenAI-protocol adapter
- **THEN** debug logs SHALL include `provider=openai` and the model name

### Requirement: OpenAI Responses protocol selection by base-URL host

The system SHALL support the OpenAI Responses wire protocol (`openai-responses`) in addition to Chat Completions (`openai-chat`). A vendor definition MAY declare a set of hostnames (`ResponsesHosts`) for which its default `openai-chat` protocol SHALL be promoted to `openai-responses`. Selection SHALL be driven by the resolved base URL host, so first-party `api.openai.com` uses Responses while any relay, proxy, or local host keeps Chat Completions. The system SHALL NOT switch a relay/proxy endpoint to Responses.

#### Scenario: First-party OpenAI selects Responses

- **WHEN** an OpenAI credential resolves to a base URL on `api.openai.com`
- **THEN** the effective protocol SHALL be `openai-responses`

#### Scenario: Relay or proxy keeps Chat Completions

- **WHEN** an OpenAI credential resolves to a base URL on a host other than the declared first-party hosts (for example a relay or `localhost`)
- **THEN** the effective protocol SHALL remain `openai-chat`

#### Scenario: Lookalike host is not treated as first-party

- **WHEN** the base URL host merely contains a first-party hostname as a substring (for example `api.openai.com.evil.example`)
- **THEN** the effective protocol SHALL remain `openai-chat`

#### Scenario: Vendors without declared hosts are unaffected

- **WHEN** a vendor definition declares no `ResponsesHosts`
- **THEN** its effective protocol SHALL be its declared default, unchanged

### Requirement: OpenAI Responses wire protocol

The system SHALL communicate with a Responses-protocol endpoint using `POST {base}/responses`. The request body SHALL carry `model`, `input` items, and — when present — `instructions`, function `tools` in the flat Responses shape, and `max_output_tokens`. The response SHALL be parsed from `output` items (assistant `message` content and `function_call` items) and `usage`, and surfaced through the same ADK `model.LLMResponse` contract as the Chat adapter.

#### Scenario: Generation request and response

- **WHEN** an agent requests a model resolved to the Responses protocol
- **THEN** the system SHALL POST to `{base}/responses` with the Responses body shape
- **THEN** the system SHALL apply the vendor's configured credential auth style
- **THEN** the system SHALL return the `output` text and function calls as ADK parts

#### Scenario: Function tool calls use the flat Responses shape

- **WHEN** the request includes tool declarations
- **THEN** each tool SHALL be sent as `{"type":"function","name":...,"description":...,"parameters":...}` without the Chat protocol's nested `function` object

#### Scenario: Endpoint returns an error status

- **WHEN** the Responses endpoint returns a non-2xx HTTP status
- **THEN** the system SHALL return an error containing the HTTP status code and response body

#### Scenario: Orphaned function calls are paired

- **WHEN** the conversation history contains a function call with no matching output
- **THEN** the system SHALL inject a synthetic `function_call_output` so the Responses conversation stays valid

### Requirement: Tools combined with a graded reasoning effort

The Responses adapter SHALL support a graded reasoning effort, sent as `{"reasoning":{"effort":...}}`. It SHALL send `tools` and the reasoning effort in the same request, because the Responses protocol permits that combination while Chat Completions rejects it for the newest OpenAI reasoning models.

#### Scenario: Tools and reasoning effort sent together

- **WHEN** a Responses model with a configured reasoning effort receives a request that declares tools
- **THEN** the request body SHALL include both the `tools` array and `reasoning.effort`

#### Scenario: Reasoning effort omitted when unset

- **WHEN** no reasoning effort is configured
- **THEN** the request body SHALL omit the `reasoning` field
