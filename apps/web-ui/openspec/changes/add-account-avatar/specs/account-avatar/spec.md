## Purpose

Defines the signed-in account's avatar: how it is resolved across manual override, Zitadel social picture, and an initials fallback, and how the user uploads and removes their avatar override.

## ADDED Requirements

### Requirement: Avatar resolution precedence

The system SHALL resolve a signed-in account's avatar using a fixed precedence: a manual override avatar when present, otherwise the Zitadel `picture` claim, otherwise an initials placeholder.

#### Scenario: Override wins over social avatar
- **WHEN** the account has both a manual override avatar and a Zitadel `picture`
- **THEN** the override avatar is displayed

#### Scenario: Social avatar when no override
- **WHEN** the account has no override but has a Zitadel `picture`
- **THEN** the Zitadel `picture` is displayed

#### Scenario: Initials when neither exists
- **WHEN** the account has neither an override nor a Zitadel `picture`
- **THEN** an initials placeholder derived from the account's display name is displayed

### Requirement: Upload an avatar override

The system SHALL allow the signed-in user to upload an image that replaces their avatar override.

#### Scenario: Successful upload
- **WHEN** the user uploads a valid image as their avatar
- **THEN** the image is stored and becomes the override avatar, replacing any prior override

#### Scenario: Invalid file type
- **WHEN** the user uploads a file that is not a supported image type (or is an SVG)
- **THEN** the upload is rejected with an error and the existing avatar is unchanged

#### Scenario: Oversized file
- **WHEN** the user uploads an image exceeding the size limit
- **THEN** the upload is rejected with an error and the existing avatar is unchanged

### Requirement: Remove the avatar override

The system SHALL allow the signed-in user to remove their override avatar, reverting resolution to the Zitadel `picture` or initials fallback.

#### Scenario: Revert to social avatar
- **WHEN** the user removes their override and a Zitadel `picture` exists
- **THEN** the Zitadel `picture` is displayed again

#### Scenario: Revert to initials
- **WHEN** the user removes their override and no Zitadel `picture` exists
- **THEN** the initials placeholder is displayed

### Requirement: Avatar displayed in account surfaces

The system SHALL display the resolved avatar in the signed-in user's account menu and profile page.

#### Scenario: Account menu shows avatar
- **WHEN** the signed-in user opens the account menu
- **THEN** the resolved avatar is shown as the menu trigger

#### Scenario: Profile page shows avatar
- **WHEN** the signed-in user views their profile page
- **THEN** the resolved avatar is shown in the profile header

### Requirement: Social avatar read from the correct OIDC source

The system SHALL read the Zitadel `picture` from the OIDC userinfo endpoint (not the ID token) at sign-in.

#### Scenario: Sign-in populates social avatar
- **WHEN** a user signs in and Zitadel exposes a `picture` on the userinfo endpoint
- **THEN** the session carries that picture as the social avatar
