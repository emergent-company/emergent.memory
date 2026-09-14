## Purpose

Defines a single consistent rule for the browser page title shown across every web page in the control plane, so the tab title is deterministic regardless of which page or state is rendered.

## ADDED Requirements

### Requirement: Consistent title format
Every web page SHALL render a browser `<title>` that ends with the brand "Memory", separated from the page-specific label by an em dash (`—`) with surrounding spaces.

#### Scenario: List page title
- **WHEN** the user opens a list page such as Agents, Chat, or Documents
- **THEN** the tab title is the page label followed by "— Memory" (for example "Agents — Memory")

#### Scenario: Detail page title
- **WHEN** the user opens a detail page for a named entity such as an agent, document, or member
- **THEN** the tab title is the entity name followed by "— Memory" (for example "Ada — Memory")

### Requirement: Sectioned detail title
A detail page that belongs to a section SHALL include the section as its own segment between the entity and the brand, joined by the same em-dash separator.

#### Scenario: Entity section title
- **WHEN** the user opens a section of a named entity, such as an agent's settings or memories
- **THEN** the tab title is the entity name, the section label, then "Memory", each separated by " — " (for example "Ada — Settings — Memory")

### Requirement: Blank segment handling
Title construction SHALL ignore empty or whitespace-only segments so no title renders consecutive separators or a bare brand.

#### Scenario: Empty entity label
- **WHEN** a page provides no meaningful entity name
- **THEN** the title still renders a stable generic label followed by "— Memory", with no leading or doubled separators

### Requirement: Login title consistency
The sign-in page SHALL render its title in the same format as every other page, ending with "— Memory".

#### Scenario: Sign-in title
- **WHEN** the user opens the sign-in page
- **THEN** the tab title is "Sign in — Memory"

### Requirement: Single brand source
The brand string used in the title SHALL be the same value used for the app's standalone-web-app title and navigation brand label.

#### Scenario: Brand consistency
- **WHEN** the app renders a title, its standalone-web-app meta title, or its navigation brand label
- **THEN** all three use the identical brand value ("Memory")
