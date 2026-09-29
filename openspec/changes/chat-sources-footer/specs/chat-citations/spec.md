# chat-citations Specification

(Delta for the `chat-sources-footer` change. Only the requirements this change
touches are restated; all other requirements in the capability are unchanged.)

## MODIFIED Requirements

### Requirement: Sources are surfaced to the user

The web chat SHALL render an answer's citations as a collapsed disclosure INSIDE
the assistant's `.chat-bubble`, after the rendered markdown, so the sources read
as agent-side content and do not occupy a separate message row or distort the
bubble. The disclosure SHALL be closed on mount, and its summary SHALL show the
number of sources as "N sources" (singular "1 source" for one). Each source
SHALL render as one compact, linkable row in the spirit of the entity row —
showing the source's name and its human type label — and SHALL link to the
citation's validated object `url` when one exists. The human type label SHALL be
the compiled schema type's `label` when known, and SHALL fall back to a
humanized form of the raw type name (`LegalParagraph` → "Legal paragraph") when
no label is available. The block SHALL be produced without interpreting answer
text as markup: names and type labels SHALL be set as text, never as HTML. The
same source-row renderer and label resolution SHALL be used by every surface
that shows citations (live turn, history replay, side panel, and the A2UI
`sources` card). No block SHALL be rendered when there are no citations.

#### Scenario: Sources block renders

- **WHEN** an answer has citations
- **THEN** the citations SHALL render as a collapsed disclosure inside the
  assistant `.chat-bubble`, after the markdown, and SHALL NOT add a separate
  `chat chat-start` message row for them
- **AND** each entry SHALL link to its object page when the citation resolves
  to a safe object url/id

#### Scenario: Disclosure is collapsed by default

- **WHEN** an answer with citations finishes rendering
- **THEN** the sources disclosure SHALL be closed, and opening it SHALL reveal
  the source rows

#### Scenario: Summary count is singular-aware

- **WHEN** an answer carries one citation
- **THEN** the disclosure summary SHALL read "1 source"; for any other count it
  SHALL read "N sources"

#### Scenario: Human type label replaces the raw type

- **WHEN** a citation's type has the compiled label "Legal paragraph"
- **THEN** the source row SHALL show "Legal paragraph", not the raw type name

#### Scenario: Unknown type falls back to a humanized name

- **WHEN** a citation's type is absent from the embedded type map
- **THEN** the source row SHALL show a humanized form of the raw type name
  (e.g. `LegalParagraph` → "Legal paragraph")

#### Scenario: Every citation surface shares the renderer

- **WHEN** citations arrive on the live stream, in a reloaded transcript, in the
  side panel, or as an A2UI `sources` card
- **THEN** each SHALL render the same source-row markup with the same label
  resolution

#### Scenario: Link safety is preserved

- **WHEN** a citation's `url` is not exactly `/objects/<uuid>` and its `id` is
  not a UUID
- **THEN** the source row SHALL render its name as plain text with no anchor

#### Scenario: No citations, no block

- **WHEN** an answer has no citations
- **THEN** no sources disclosure SHALL be rendered

### Requirement: A2UI reference components

The A2UI catalog SHALL add a `sources` component (carrying an `items` list). The
web renderer SHALL render each `sources` item with the same compact source-row
language as the in-bubble sources footer — the item's name and its human type
label, linked to `/objects/<id>` when the item resolves to a safe object url/id
— and SHALL show the item count in the card header. The `entity` component SHALL
render its `id` prop as a link to `/objects/<id>` when present. Both additions
SHALL be additive and SHALL NOT change the envelope or existing components.

#### Scenario: Sources card renders links

- **WHEN** an agent emits a `sources` component whose items carry ids
- **THEN** the client SHALL render each item as a compact source row, linked to
  its object page when the id/url is a safe object reference

#### Scenario: Entity card links its object

- **WHEN** an `entity` component carries an `id` prop
- **THEN** the client SHALL render a link to `/objects/<id>`

#### Scenario: Older clients degrade

- **WHEN** a client without a `sources` renderer receives a `sources` component
- **THEN** it SHALL fall back to the existing text/summary render and SHALL NOT
  error

## ADDED Requirements

### Requirement: Human type labels are available to the client renderer

The web shell SHALL embed a machine-readable map from compiled type name to its
display label (and optional icon/color), covering object and relationship types,
so the client can render human type labels synchronously. When the map is absent
or a type is missing from it, the renderer SHALL degrade to a humanized type
name rather than showing the raw machine name or failing.

#### Scenario: Map is embedded

- **WHEN** the shell renders a page
- **THEN** it SHALL include a JSON map of compiled type name → label (and
  optional icon/color)

#### Scenario: Missing map degrades

- **WHEN** the type map is absent or a type is not present in it
- **THEN** the renderer SHALL show a humanized type name and SHALL NOT error
