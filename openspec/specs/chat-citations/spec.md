# chat-citations Specification

## Purpose
Defines grounded citations for knowledge-base-backed chat answers: how the server
derives references to graph objects and relationships from data a run actually
retrieved, validates them, transports them on the live stream and the session
timeline, and how the web client renders them as validated inline links and a
Sources block (with an A2UI reference component).

## Requirements

### Requirement: Citations are grounded in retrieved data

The server SHALL derive an answer's citations from the identifiers the run's
tools actually returned and the answer actually referenced. It SHALL collect
candidate object and relationship references by walking the run's tool-call
outputs, and SHALL include a citation only when the answer references that
identifier **and** the identifier appears among the candidates. An identifier
referenced by the answer but absent from the candidates SHALL NOT become a
citation.

#### Scenario: Retrieved and referenced object is cited

- **WHEN** a run's `search-hybrid` output contains object id `X` and the answer links to `/objects/X`
- **THEN** the answer's citations SHALL include object `X` with its type and label

#### Scenario: Hallucinated identifier is not cited

- **WHEN** the answer links to `/objects/Y` and no tool output of the run contains object id `Y`
- **THEN** `Y` SHALL NOT appear in the citations

#### Scenario: Retrieved but unreferenced object is not cited

- **WHEN** a tool output of the run contains object `Z` and the answer never references `Z`
- **THEN** `Z` SHALL NOT appear in the citations

#### Scenario: Derivation covers every graph tool

- **WHEN** a run retrieves objects through `search-hybrid`, `entity-query`, `graph-traverse`, `search-similar`, or `entity-edges-get`, or relationships through `relationship-list`
- **THEN** the ids in those outputs SHALL be candidates for citation

### Requirement: Answer reference format

An answer SHALL reference an object with a markdown link whose target is
`/objects/<canonical-object-id>`, and a relationship with a markdown link whose
target is `/objects/<source-object-id>` carrying a `#relationship-<relationship-id>`
fragment. The identifiers SHALL be the canonical identifiers returned by the
tools.

#### Scenario: Object reference

- **WHEN** the answer contains `[Acme Corp](/objects/<id>)` and the run retrieved `<id>`
- **THEN** the citations SHALL include an object citation for `<id>` labelled `Acme Corp`

#### Scenario: Relationship reference

- **WHEN** the answer contains `[A —works_at→ B](/objects/<src>#relationship-<rel>)` and the run retrieved relationship `<rel>`
- **THEN** the citations SHALL include a relationship citation for `<rel>` with its type, source `A`, and target `B`

### Requirement: Key-based references are resolved

An answer MAY reference an object by its `key` (for example `lov/2005-06-17-90`)
instead of its canonical identifier. When a `/objects/<ref>` reference matches
the `key` of a retrieved object rather than its `id`, the server SHALL resolve it
to that object's canonical id and a citation SHALL be produced for that object.
The rendered link SHALL target `/objects/<canonical-id>` so it resolves, not the
key. A reference that matches neither a retrieved id nor a retrieved key SHALL be
treated as unvalidated.

#### Scenario: Key reference resolves to the object

- **WHEN** the answer links `/objects/lov/2005-06-17-90` and a retrieved object of the run carries that `key`
- **THEN** the citations SHALL include that object and the rendered link SHALL target `/objects/<canonical-id>`

#### Scenario: Unknown key is unvalidated

- **WHEN** the answer links a key that no retrieved object of the run carries
- **THEN** it SHALL NOT be cited and the link SHALL follow the neutralization rule

### Requirement: Citation payload shape

Each citation SHALL carry `kind` (`object` or `relationship`), `id`, `type`,
`label`, and `url`. An object citation's `url` SHALL be `/objects/<id>`; a
relationship citation's `url` SHALL resolve to the object page of its source. A
citation MAY additionally carry `key` — the object's human key — when the
reference was made by key, so a renderer can keep and re-target such a link.

#### Scenario: Shape is stable across transports

- **WHEN** a citation reaches the client on the live stream or in a reloaded transcript
- **THEN** it SHALL carry `kind`, `id`, `type`, `label`, and `url`

### Requirement: Citations are transported on live turns and history

The chat SSE stream SHALL carry a turn's citations in a dedicated terminal
`citations` event emitted before `done`, and the session timeline and run-history
responses SHALL carry the same citations on assistant message items. The live
turn and a reloaded transcript SHALL therefore render the same citations.

#### Scenario: Live turn carries citations

- **WHEN** a run produces citations and finishes
- **THEN** the client SHALL receive a `citations` event before `done` carrying them

#### Scenario: Reloaded transcript carries citations

- **WHEN** a conversation's history is loaded for an answer that has citations
- **THEN** that answer's timeline item SHALL carry the same citations

#### Scenario: Turn without citations is unchanged

- **WHEN** a run produces no citations
- **THEN** the stream and timeline SHALL behave exactly as before this capability

### Requirement: Unvalidated object links are neutralized

A link to `/objects/<id>` whose `id` is not a validated citation SHALL render as
its label text without a hyperlink. A `#relationship-<rel>` fragment whose `rel`
is not a validated citation SHALL be dropped. A link whose target is a validated
citation SHALL render as a working link.

#### Scenario: Unknown object link is demoted

- **WHEN** an answer links to `/objects/<id>` but `<id>` is not a citation
- **THEN** the rendered answer SHALL show the link label as plain text and SHALL NOT emit an anchor for that id

#### Scenario: Valid citation link is preserved

- **WHEN** an answer links to `/objects/<id>` and `<id>` is a citation
- **THEN** the rendered answer SHALL contain a link to that object page

#### Scenario: Unknown relationship fragment is dropped

- **WHEN** a link carries `#relationship-<rel>` and `<rel>` is not a citation
- **THEN** the fragment SHALL be removed and the remaining object link SHALL follow the object-link rule

#### Scenario: Key-based link is kept and re-targeted

- **WHEN** an answer links `/objects/<key>` and `<key>` is the key of a citation
- **THEN** the link SHALL be kept with its target rewritten to `/objects/<canonical-id>`

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

### Requirement: Agents are instructed to cite retrieved sources

An agent that answers from the knowledge base SHALL be instructed to cite the
objects and relationships it uses, using the exact identifiers from tool results
and the reference format above, and to never invent an identifier.

#### Scenario: Instruction is present

- **WHEN** a knowledge-base-backed agent's instruction is assembled
- **THEN** it SHALL include the citation instruction and the reference format

### Requirement: Human type labels are available to the client renderer

The web shell SHALL embed a machine-readable map from compiled type name to its
display label (and optional color), covering object and relationship types,
so the client can render human type labels synchronously. When the map is absent
or a type is missing from it, the renderer SHALL degrade to a humanized type
name rather than showing the raw machine name or failing.

#### Scenario: Map is embedded

- **WHEN** the shell renders a page
- **THEN** it SHALL include a JSON map of compiled type name → label (and
  optional color)

#### Scenario: Missing map degrades

- **WHEN** the type map is absent or a type is not present in it
- **THEN** the renderer SHALL show a humanized type name and SHALL NOT error
