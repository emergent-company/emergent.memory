## ADDED Requirements

### Requirement: Bare relationship references are linkified

The web chat renderer SHALL linkify a bare relationship reference — a
`#relationship-<relationship-id>` fragment appearing in an answer or thinking
block, outside an existing object-link target and outside inline/fenced code —
when, and only when, the turn's citations contain a relationship citation whose
`id` is that full identifier and whose `url` is a canonical `/objects/<uuid>`
page. The emitted link's target SHALL be
`/objects/<source-object-id>#relationship-<relationship-id>`, so it opens the
source object's preview with its relationship context. A reference that is not a
full identifier (for example a truncated form), a reference with no matching
relationship citation, and text carrying no relationship citations SHALL be left
unchanged.

#### Scenario: Bare bracketed reference becomes a link

- **WHEN** the text contains `[#relationship-<rel>]` and the turn's citations contain a relationship citation for `<rel>` whose `url` is `/objects/<src>`
- **THEN** the rendered output SHALL contain an anchor to `/objects/<src>#relationship-<rel>`

#### Scenario: Bare unbracketed reference becomes a link

- **WHEN** the text contains `#relationship-<rel>` (no surrounding brackets) and the turn's citations contain a relationship citation for `<rel>`
- **THEN** the rendered output SHALL contain an anchor to `/objects/<src>#relationship-<rel>`

#### Scenario: Truncated reference is left as text

- **WHEN** the text contains a truncated reference such as `#relationship-2abaf19c...`
- **THEN** it SHALL render as plain text with no anchor

#### Scenario: Reference without a citation is left as text

- **WHEN** the text contains `#relationship-<rel>` and the turn's citations contain no relationship citation for `<rel>`
- **THEN** it SHALL render as plain text with no anchor

#### Scenario: Existing link target is not rewritten

- **WHEN** the text already contains a markdown link target `/objects/<ref>#relationship-<rel>`
- **THEN** the renderer SHALL NOT wrap that fragment again

#### Scenario: Code is untouched

- **WHEN** a `#relationship-<rel>` fragment appears inside an inline code span or a fenced code block
- **THEN** it SHALL be rendered verbatim as code

#### Scenario: No relationship citations leaves text unchanged

- **WHEN** the turn carries no relationship citations
- **THEN** the rendered output SHALL be identical to the output before this capability
