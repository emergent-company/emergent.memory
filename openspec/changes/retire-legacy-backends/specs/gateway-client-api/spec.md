## Purpose

The Go gateway serves the client-facing endpoints — QR onboarding, session-log browsing, and the read-only memory proxy — as the single origin iOS and the web UI talk to.

## ADDED Requirements

### Requirement: QR onboarding payload

The gateway SHALL serve a QR-config payload carrying the LiveKit server URL, the token endpoint, the control-plane base URL, the shared API key, and the default agent and room.

#### Scenario: Fetch QR config

- **WHEN** a client requests the QR config endpoint
- **THEN** the gateway returns the server URL, token endpoint, API base URL, API key, default agent, and default room

### Requirement: Session-log API on the gateway

The gateway SHALL expose the session-log API — a list of sessions (most recent first) and a per-session timeline with turns and tool calls — sourced from Memory conversations and history.

#### Scenario: List sessions

- **WHEN** a client lists sessions
- **THEN** the gateway returns the recorded sessions ordered by recency

#### Scenario: Session timeline

- **WHEN** a client requests a session's timeline
- **THEN** the gateway returns the session's turns and tool-call records in chronological order

### Requirement: Memory proxy on the gateway

The gateway SHALL proxy read-only memory access: report per-agent memory capability and search an agent's memories, returning an empty result for memory-less agents.

#### Scenario: Capability lookup

- **WHEN** a client queries memory capability for an agent
- **THEN** the gateway reports whether the agent references the memory MCP server

#### Scenario: Memory search

- **WHEN** a client searches memories for a memory-enabled agent
- **THEN** the gateway returns the matching memories with content, category, and confidence

### Requirement: Authenticated access

The gateway SHALL require the shared `X-API-Key` trust boundary on these client endpoints and MUST NOT expose sessions or memories to unauthenticated callers.

#### Scenario: Unauthenticated request

- **WHEN** a client calls a client-facing endpoint without a valid API key
- **THEN** the gateway returns an unauthorized response and no data
