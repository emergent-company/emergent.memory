## ADDED Requirements

### Requirement: Voice session applies the active project's settings

The running voice session SHALL apply the voice settings (text-to-speech provider, voice, language, speech-to-text provider, and the remaining voice options) of the session's active project, resolving them from that project's agent definition rather than a single static supervisor-injected environment.

#### Scenario: Project-specific voice settings applied

- **WHEN** a signed-in user whose active project has specific voice settings starts a voice call
- **THEN** the bridge worker uses that project's provider, voice, language, and voice options

#### Scenario: Different projects apply different settings

- **WHEN** two users in different projects with different voice settings each start a voice call
- **THEN** each call is served with its own project's voice settings
