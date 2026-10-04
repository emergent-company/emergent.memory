## ADDED Requirements

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
