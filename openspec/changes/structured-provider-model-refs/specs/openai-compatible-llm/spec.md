## Purpose

OpenAI-compatible backends are a dialect, not a single global endpoint. A project must be able to configure several OpenAI-compatible endpoints at once, each with its own credentials and base URL, and address them independently.

## ADDED Requirements

### Requirement: Multiple OpenAI-compatible project instances

The system SHALL allow a project to configure more than one OpenAI-compatible provider instance, each with a distinct slug, base URL, and API key, and SHALL resolve model references to the intended instance.

#### Scenario: Configure two OpenAI-compatible instances

- **WHEN** a project configures two OpenAI-compatible instances with distinct slugs and base URLs
- **THEN** both are stored and both can be used for model calls

#### Scenario: Route a model to a specific instance

- **WHEN** a model reference names one OpenAI-compatible instance's slug
- **THEN** the chat-completions call is sent to that instance's base URL with that instance's API key

#### Scenario: Instances are not interchangeable

- **WHEN** two instances have distinct credentials and a model is requested from one
- **THEN** the other instance's credentials and base URL are not used

## MODIFIED Requirements

### Requirement: OpenAI Chat Completions wire protocol

The system SHALL communicate with the selected OpenAI-compatible instance using the OpenAI Chat Completions API format (`POST {base_url}/chat/completions`), sending a JSON body with `model`, `messages`, and `max_tokens` fields, and parsing the response's `choices[0].message.content` as the model output. The base URL and API key SHALL come from the instance named by the model reference. The stored `base_url` SHALL be the versioned API root (e.g. `https://api.openai.com/v1`, `http://localhost:11434/v1`); the client appends `/chat/completions` and SHALL normalize trailing slashes so it never produces `/v1/v1` or drops the version path.

#### Scenario: Successful generation request

- **WHEN** an agent creates a model for an OpenAI-compatible instance and generates content
- **THEN** the system SHALL POST to that instance's `{base_url}/chat/completions` with the correct JSON body
- **THEN** the system SHALL set the `Authorization: Bearer {api_key}` header when the instance's API key is non-empty
- **THEN** the system SHALL return the content from `choices[0].message.content` as the LLM response

#### Scenario: Base URL with trailing slash

- **WHEN** an instance's stored base URL ends with `/`
- **THEN** the client joins it to produce exactly one `/chat/completions` segment (no double slash)

#### Scenario: Endpoint returns an error status

- **WHEN** the OpenAI-compatible endpoint returns a non-2xx HTTP status
- **THEN** the system SHALL return an error containing the HTTP status code and response body

#### Scenario: Endpoint is unreachable

- **WHEN** the OpenAI-compatible endpoint is not reachable (connection refused, timeout)
- **THEN** the system SHALL return a descriptive error wrapping the underlying network error
