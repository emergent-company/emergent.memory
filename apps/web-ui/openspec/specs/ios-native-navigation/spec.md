# ios-native-navigation Specification

## Purpose
Provides a two-level native navigation shell for the iOS app: a first-level agent picker with Settings, and a per-agent second level exposing Conversation, Sessions, and Agent settings.
## Requirements
### Requirement: Two-level navigation hierarchy

The app SHALL present a two-level navigation hierarchy: a first level containing the agent picker and Settings, and a second level, reached by choosing an agent, containing Conversation, Sessions, and Agent settings for that agent.

#### Scenario: First level shown

- **WHEN** the app launches
- **THEN** the app shows the first level: the agent picker and a Settings entry point

#### Scenario: Choosing an agent opens the second level

- **WHEN** the user chooses an agent from the first level
- **THEN** the app navigates to that agent's second level

### Requirement: Agent picker as main screen

The agent picker SHALL be the main screen, presenting each agent as a large panel or button.

#### Scenario: Agents as large panels

- **WHEN** one or more agents exist
- **THEN** the main screen shows each agent as a large, tappable panel

#### Scenario: No agents

- **WHEN** no agents exist
- **THEN** the main screen shows a call-to-action to add the first agent instead of an empty picker

### Requirement: Per-agent second level

The second level SHALL expose three destinations for the chosen agent: Conversation (start/stop), Sessions, and Agent settings.

#### Scenario: Second-level destinations

- **WHEN** the user is on an agent's second level
- **THEN** the app offers Conversation, Sessions, and Agent settings as selectable destinations

#### Scenario: Return to first level

- **WHEN** the user navigates back from the second level
- **THEN** the app returns to the first-level agent picker with prior state preserved

### Requirement: Voice flow preserved

The Conversation destination SHALL host the existing voice connect flow (connect, interact, end session) working exactly as before.

#### Scenario: Connect from Conversation

- **WHEN** the user starts a conversation from the Conversation destination
- **THEN** the app enters the connected state and runs the existing voice interaction

#### Scenario: Session ends

- **WHEN** an active session ends
- **THEN** the app returns to the Conversation destination in the idle state

#### Scenario: Navigate mid-session

- **WHEN** the user navigates away from Conversation while a session is active
- **THEN** the session continues and the user can return to the live Conversation view

