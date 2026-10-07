## MODIFIED Requirements

### Requirement: Signed-out state prompts re-authentication

When the app has an account but the connector CLI has no valid session (an
expired or cleared session), the app SHALL show a signed-out state with a clear
Sign in action instead of a raw "Couldn't load projects" error. Detecting the
failure SHALL prefer the connector CLI's own session status (`auth status`),
falling back to the failure text (`not signed in`, `401`, `invalid_token`). A
successful sign-in SHALL import that session into the connector CLI via
`auth import` and then load the user's projects. Non-auth failures SHALL still
show the error state. When a project is connected and the failure is an
auth/session failure, the app SHALL keep the connected intent and SHALL NOT stop
a running engine whose config still binds that project; only a non-auth failure
tears down the connection and stops the engine.

#### Scenario: Auth failure shows the signed-out state

- **WHEN** loading projects fails because the connector CLI reports no valid
  session (or the failure is a `not signed in`, `401`, or `invalid_token`
  error)
- **THEN** the Project & Account page and the project switcher show a
  "Signed out" prompt with a Sign in action (and a Try again action), not a raw
  "Couldn't load projects" error

#### Scenario: Sign-in imports the session and loads projects

- **WHEN** the user signs in from the signed-out prompt
- **THEN** the app imports the session into the connector CLI via
  `auth import` and loads the user's projects for the signed-in environment

#### Scenario: Non-auth failure still shows the error state

- **WHEN** loading projects fails for a reason that is not an auth/session
  failure
- **THEN** the app shows the "Couldn't load projects" error state with the
  failure message, as before

#### Scenario: Session expires while a project is connected

- **WHEN** a project is connected and the connector CLI's session expires or is
  cleared, so a config re-materialisation (e.g. `projects use` during the launch
  scope-settle) fails with an auth/session error
- **THEN** the app keeps the connected intent and leaves the running engine
  running (its long-lived project token and on-disk config still bind that
  project), and surfaces the signed-out prompt for management actions — it does
  not tear down a healthy engine
