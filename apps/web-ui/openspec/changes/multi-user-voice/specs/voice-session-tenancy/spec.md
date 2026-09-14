## Purpose

Binds each voice (LiveKit) session to a signed-in user's session and active project, so a voice call operates in that user's project with per-session credentials and agent resolution instead of a single shared owner/project.

## ADDED Requirements

### Requirement: Voice session binds to the signed-in user's active project

The gateway SHALL resolve, for a session-authenticated voice call, the user's active project and org, and SHALL carry that binding into the bridge worker so the call operates in that project rather than a shared static project.

#### Scenario: Session voice call uses the active project

- **WHEN** a signed-in user starts a voice call
- **THEN** the bridge worker serves the call against the user's active project

#### Scenario: API-key path unchanged

- **WHEN** a programmatic client with a valid `X-API-Key` and no session starts a voice call
- **THEN** the call operates with the server's static project credentials as before

### Requirement: Per-session project-scoped credentials

The gateway SHALL supply a short-lived, project-scoped Memory credential for each voice session, and MUST NOT place the user's long-lived session token in the LiveKit room-dispatch metadata or worker environment.

#### Scenario: Project-scoped credential issued

- **WHEN** a signed-in user starts a voice call
- **THEN** the bridge worker receives a short-lived credential scoped to the user's active project

#### Scenario: User session token not exposed to the worker

- **WHEN** a voice call is set up
- **THEN** the user's session token is not placed in the room-dispatch metadata or the worker process environment

### Requirement: Agent definition resolved per project

The gateway SHALL resolve the requested agent name to the agent-definition id within the user's active project, and the bridge worker SHALL drive that definition.

#### Scenario: Same agent name across projects

- **WHEN** two users in different projects dispatch the same agent name
- **THEN** each call is served by the agent definition in its own project

### Requirement: Reject a voice call without a resolvable project or agent

The gateway SHALL reject a session-authenticated voice call it cannot bind to a project or agent, instead of dispatching to a worker bound to the wrong project.

#### Scenario: No active project

- **WHEN** a session-authenticated caller has no active project
- **THEN** the token endpoint returns an error and no LiveKit token is issued

#### Scenario: Unknown agent in the active project

- **WHEN** a session-authenticated caller requests an agent name that does not exist in their active project
- **THEN** the token endpoint rejects the request and no voice call is dispatched
