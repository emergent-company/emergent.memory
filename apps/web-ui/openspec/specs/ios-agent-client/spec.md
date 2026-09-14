## Purpose

A native iOS client that lets a user talk to Memory from an iPhone over the self-hosted LiveKit infrastructure, mirroring the voice-interaction behavior of the existing macOS client.

## Requirements

### Requirement: Connect to the Memory LiveKit room

The client SHALL connect to the configured self-hosted LiveKit server and join the Memory room using a server-minted access token. The client MUST obtain its token from a token endpoint (not embed LiveKit API credentials in the app binary), and MUST allow the server URL and agent name to be configured without recompiling.

#### Scenario: Successful connection

- **WHEN** the user launches the app with valid server URL, agent name, and a reachable token endpoint
- **THEN** the client obtains a token, connects to the LiveKit room, and shows a connected state

#### Scenario: Unreachable server

- **WHEN** the LiveKit server is unreachable or the token endpoint returns an error
- **THEN** the client surfaces a clear failure message and does not enter a phantom "connected" state

#### Scenario: Missing or invalid token

- **WHEN** the token endpoint returns a non-success response or a token that LiveKit rejects
- **THEN** the client reports the failure and allows the user to retry

### Requirement: Publish microphone audio

The client SHALL capture microphone audio and publish it to the room with echo cancellation enabled so the agent can hear the user and barge-in behaves correctly.

#### Scenario: Mic permission granted

- **WHEN** the user grants microphone permission and the client is connected
- **THEN** the client publishes a live mic audio track that the agent subscribes to

#### Scenario: Mic permission denied

- **WHEN** the user denies microphone permission
- **THEN** the client explains the denial and provides a path to re-enable permission in Settings

### Requirement: Render agent audio and presence

The client SHALL play audio from the agent participant (identity prefixed with `agent-`) and SHALL detect when the agent leaves the room.

#### Scenario: Agent audio playback

- **WHEN** the agent publishes audio
- **THEN** the client plays the audio through the device output

#### Scenario: Agent leaves

- **WHEN** the agent participant disconnects
- **THEN** the client ends the active session and returns to an idle/disconnected state

### Requirement: Dispatch the active agent

The client SHALL explicitly dispatch the active agent (`memory-google-rt` by default) into the joined room, matching the existing client's dispatch model.

#### Scenario: Agent dispatch on connect

- **WHEN** the client joins the room and dispatch succeeds
- **THEN** the agent worker joins the room and the session begins

#### Scenario: Dispatch failure

- **WHEN** agent dispatch fails (unknown agent, worker unavailable)
- **THEN** the client surfaces the failure and allows retry

### Requirement: Honor Memory text-stream signals

The client SHALL consume the agent's text streams to drive session state: `lk.agent.ready` (play the cue-to-speak chime), `lk.agent.events` (`user_state_changed` to `away` ends the session), and `lk.transcription` (user transcripts scanned for exit keywords).

#### Scenario: Cue to speak

- **WHEN** the agent emits `ready` on the `lk.agent.ready` topic
- **THEN** the client plays a chime signaling the user may speak

#### Scenario: User away

- **WHEN** the agent emits a `user_state_changed` event with `new_state: away` on `lk.agent.events`
- **THEN** the client ends the session

#### Scenario: Exit keyword

- **WHEN** a user transcription on `lk.transcription` contains a configured exit keyword
- **THEN** the client ends the session and returns to idle

### Requirement: Token server endpoint

The infrastructure SHALL provide an HTTP endpoint that mints a short-lived LiveKit room-join token for a requested identity and the Memory room, using server-side LiveKit credentials.

#### Scenario: Mint a token

- **WHEN** a client requests a token with a valid identity and room
- **THEN** the endpoint returns a signed JWT with `room_join` grant for that room

#### Scenario: Reject invalid requests

- **WHEN** a request is malformed, unauthorized, or names a disallowed room
- **THEN** the endpoint returns a non-success status and no token
