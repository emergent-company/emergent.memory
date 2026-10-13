## ADDED Requirements

### Requirement: The page shell offers a full-bleed container width

`layout.ContainerSize` SHALL include a variant that removes the centred maximum
width, so a page whose content benefits from the whole viewport can opt out of
the capped container without forking the page-shell convention.

#### Scenario: A full-bleed page is declared through the shell

- **WHEN** a page renders `layout.Container` with the full size variant
- **THEN** its content is not constrained to a centred maximum width

#### Scenario: The capped variants are unchanged

- **WHEN** a page renders the medium or large container
- **THEN** its content is centred at the existing maximum width, unchanged

### Requirement: The board shares the available width instead of scrolling horizontally

The board SHALL lay its status lanes out so that they divide the available
horizontal space, and SHALL only fall back to horizontal overflow when the space
is genuinely insufficient, so a desktop viewport does not require scrolling to
see the lanes.

#### Scenario: Lanes fit a desktop viewport

- **WHEN** the board renders its status lanes at a desktop viewport
- **THEN** every lane is visible without horizontal scrolling

#### Scenario: Overflow remains the fallback

- **WHEN** the lanes cannot fit the available width (narrow viewport, or more lanes than fit at the minimum lane width)
- **THEN** the board scrolls horizontally rather than compressing lanes below a usable width

#### Scenario: The inline width utility is compiled

- **WHEN** the CSS is built
- **THEN** the width utility the full-bleed container relies on is present in the compiled stylesheet, despite being applied from a runtime value
