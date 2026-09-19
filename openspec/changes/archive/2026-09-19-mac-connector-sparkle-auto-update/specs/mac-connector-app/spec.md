## ADDED Requirements

### Requirement: The bundled connector engine is a pinned, non-self-updating snapshot

The app SHALL bundle a connector engine binary whose version is determined by the app release, and that binary SHALL be replaced only by installing a new version of the app. When the engine is executed from inside the app bundle, an engine-initiated self-upgrade SHALL be refused with a message stating that the engine is managed by the app, and the bundle SHALL remain unmodified. The app SHALL report the bundled engine's version together with the app version.

#### Scenario: Engine self-upgrade attempted inside the bundle

- **WHEN** the bundled engine is asked to upgrade itself while running from inside the app bundle
- **THEN** it refuses, reports that it is managed by the app, and does not modify any file in the bundle

#### Scenario: Engine self-upgrade from a standalone install

- **WHEN** the engine binary lives outside any app bundle and is asked to upgrade itself
- **THEN** it upgrades in place as before

#### Scenario: Engine version tracks the app release

- **WHEN** a new version of the app is installed
- **THEN** the bundled engine reports the version built for that app release

#### Scenario: Versions are reported

- **WHEN** the user inspects the app's version information
- **THEN** both the app version and the bundled engine version are shown
