## REMOVED Requirements

### Requirement: Google Drive connects by pasting a refresh token

The gateway SHALL no longer offer a paste-refresh-token path for Google Drive. Creating or updating a
Drive source by supplying an OAuth2 refresh token directly in the connect form is removed; Drive
connects proceed exclusively through the OAuth connect flow. A pre-existing Drive source whose stored
credential is a refresh token is unaffected and keeps syncing.

#### Scenario: Paste-refresh-token form is gone

- **WHEN** a signed-in user opens the Drive connect flow
- **THEN** the gateway offers only the OAuth connect action and presents no refresh-token field

#### Scenario: Existing pasted sources are unaffected

- **WHEN** a Drive source was created before this change with a stored refresh token
- **THEN** the gateway lists it and its syncs continue without re-authorization
