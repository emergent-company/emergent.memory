## Purpose

Gives the signed-in web-console user a topbar account menu and the ability to be signed into multiple Memory accounts at once, switching between them without re-authenticating.

## ADDED Requirements

### Requirement: Topbar account menu

The app shell SHALL show the signed-in user's avatar in the topbar, and selecting it SHALL open an account menu.

#### Scenario: Signed-in user sees the avatar

- **WHEN** a signed-in user loads any page in the app shell
- **THEN** their avatar is shown in the topbar, and selecting it opens the account menu

#### Scenario: No session shows no account menu

- **WHEN** the gateway runs in dev or API-key mode with no signed-in identity
- **THEN** no account menu is rendered in the topbar

### Requirement: Show the active account

The account menu SHALL identify which account is currently active.

#### Scenario: Active account marked

- **WHEN** the account menu is opened
- **THEN** the active account is shown and visually marked as the current one

### Requirement: List other signed-in accounts

The account menu SHALL list the other Memory accounts the user is concurrently signed into.

#### Scenario: Multiple accounts listed

- **WHEN** the user is signed into more than one account
- **THEN** the menu lists the other accounts alongside the active one

#### Scenario: Single account

- **WHEN** the user is signed into exactly one account
- **THEN** the menu shows only that active account and no other-account list

### Requirement: Add another account

The account menu SHALL offer an "Add another account" action that signs the user into an additional Memory account without signing out the current one.

#### Scenario: Add succeeds

- **WHEN** the user completes sign-in for a different account
- **THEN** that account is added, becomes the active account, and the previous account remains signed in and listed

#### Scenario: Re-adding an existing account

- **WHEN** the user signs in to an account that is already signed in
- **THEN** no duplicate entry is created and the active account switches to it

### Requirement: Switch account

The user SHALL be able to switch the active account without re-authenticating.

#### Scenario: Switch succeeds

- **WHEN** the user selects another signed-in account in the menu
- **THEN** the active identity changes to that account and the app serves the request under the new account's session

### Requirement: Sign out of the current account

The "Log out" action SHALL end the current account's session without ending other accounts' sessions.

#### Scenario: Sign out one account

- **WHEN** the user chooses "Log out" while multiple accounts are signed in
- **THEN** the current account's session ends and the other accounts remain signed in

### Requirement: Stable account identity

Accounts SHALL be distinguished by a stable identity so the same Memory account is never listed twice.

#### Scenario: No duplicate accounts

- **WHEN** the user is signed into a set of accounts
- **THEN** each account appears at most once, keyed by a stable identifier that survives token refresh
