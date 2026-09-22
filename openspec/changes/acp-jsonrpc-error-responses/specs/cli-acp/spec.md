## ADDED Requirements

### Requirement: Malformed message handling

The CLI SHALL answer any inbound line it cannot decode into a valid JSON-RPC Request with a JSON-RPC error response, rather than silently dropping it. It SHALL return `-32700` Parse error for malformed JSON and `-32600` Invalid Request for structurally valid JSON that is not a valid Request object (bad or missing `jsonrpc`, missing or non-string `method`, or a non-object value). The response SHALL echo the request `id` when one was recoverable from the input and `id: null` otherwise. The CLI SHALL still log the failure to stderr for operator visibility.

#### Scenario: Malformed JSON

- **WHEN** the client sends a line that is not valid JSON
- **THEN** the CLI writes a `-32700` Parse error response with `id: null` and logs the failure to stderr

#### Scenario: Missing method

- **WHEN** the client sends a structurally valid JSON object that carries an `id` but no `method`
- **THEN** the CLI writes a `-32600` Invalid Request response echoing that `id`

#### Scenario: Unsupported version

- **WHEN** the client sends a request whose `jsonrpc` field is not `"2.0"`
- **THEN** the CLI writes a `-32600` Invalid Request response echoing the request `id`

#### Scenario: Non-Request JSON value

- **WHEN** the client sends valid JSON that is not an object (for example a string or an array)
- **THEN** the CLI writes a `-32600` Invalid Request response with `id: null`

#### Scenario: Loop continues after a bad line

- **WHEN** a malformed line is followed by a well-formed request
- **THEN** the CLI answers the bad line with the appropriate error frame and still serves the following request
