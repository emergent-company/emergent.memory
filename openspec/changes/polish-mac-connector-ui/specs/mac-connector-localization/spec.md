## Purpose

Ensures all user-facing text in the mac connector app is localizable, uses semantic text styles so it scales and does not clip longer translations, and lays out correctly for right-to-left locales.

## ADDED Requirements

### Requirement: User-facing strings are localizable

All user-facing text SHALL be localizable. Literal text in views SHALL use SwiftUI's localizable initializers backed by a String Catalog, and user-facing strings carried by non-view types SHALL be `LocalizedStringResource` rather than `String`. Localizable text SHALL NOT be constructed by transforming case at runtime or by joining fragments at runtime.

#### Scenario: A string catalog exists and is wired in

- **WHEN** the app bundle is built
- **THEN** a String Catalog is compiled into the app and view literals resolve through it

#### Scenario: No runtime case transform on user text

- **WHEN** the app's user-facing text is inspected
- **THEN** no display string is produced by applying a runtime case transform (such as uppercasing) to a base string

#### Scenario: List values are formatted, not joined

- **WHEN** a comma-separated list of user-facing values is displayed
- **THEN** it is produced with the platform list formatting API rather than by joining strings with a separator

### Requirement: Text follows system text styles

User-visible text SHALL use semantic text styles (or otherwise scale with the system text-size setting) rather than fixed point sizes, so it scales for accessibility and does not clip when a translation is longer than the source string.

#### Scenario: Text scales with the system text size

- **WHEN** the system text size is increased
- **THEN** the app's text scales instead of staying at a fixed point size

#### Scenario: Longer translations do not clip

- **WHEN** a user-facing string is replaced by a longer translation in a fixed-format layout (such as the dashboard stat tiles)
- **THEN** the text wraps, scales, or otherwise remains fully readable rather than being clipped

### Requirement: Layout supports right-to-left locales

Directional layout, alignment, and spacing SHALL use leading/trailing semantics rather than left/right, so the interface mirrors correctly in right-to-left locales.

#### Scenario: Right-to-left layout mirrors

- **WHEN** the app runs under a right-to-left locale
- **THEN** controls and aligned content mirror correctly with no hard-coded left/right placement
