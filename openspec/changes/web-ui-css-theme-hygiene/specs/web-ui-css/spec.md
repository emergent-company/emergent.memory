## ADDED Requirements

### Requirement: Runtime-injected styles are not a second stylesheet

The gateway SHALL NOT maintain two divergent sources for the same component styles. Any styles a script injects into the document SHALL be reduced to a minimal, documented subset that an automated check keeps in sync with the compiled stylesheet. Removing the injected block entirely is not an option for the badge rules, whose retention is separately required by the badge-sync contract; that contract stays authoritative for them.

#### Scenario: No undocumented duplicated rule sets

- **WHEN** the styles injected by `webui/static/js/chat-components.js` are compared with the compiled stylesheet
- **THEN** no rule is declared in both places except the documented, tested subset, and that subset matches the compiled source

#### Scenario: Injected subset stays in sync

- **WHEN** the unit/style tests run
- **THEN** a test fails if the injected subset and the compiled stylesheet disagree, and the badge rules retained by the badge-sync contract are covered by that test rather than removed

### Requirement: Colours derive from theme tokens

Surfaces and foregrounds in the gateway's own CSS SHALL derive from the daisyUI theme tokens, so the app's palette has a single source of truth.

#### Scenario: No hardcoded page background

- **WHEN** the page background and its no-flash guard are declared
- **THEN** the value comes from a theme token and is declared in exactly one place

#### Scenario: App-owned component colours are tokens

- **WHEN** the app's own component classes declare a colour
- **THEN** the colour derives from a theme token rather than a literal value, apart from documented decorative gradients

#### Scenario: Rendered-markdown and hairline literals are reconciled

- **WHEN** the app's own CSS sets a colour for rendered markdown, a hairline border, or a surface
- **THEN** it derives from a theme token, apart from documented decorative translucency

### Requirement: Radius derives from theme radius variables

The gateway SHALL express corner radius through the theme's radius variables: the theme-driven utilities (`rounded-box`, `rounded-field`, `rounded-selector`) in markup, and `var(--radius-box|field|selector)` in the gateway's own CSS. The gateway SHALL NOT use Tailwind's literal radius scale (`rounded`, `rounded-sm`, `rounded-md`, `rounded-lg`, `rounded-xl`, `rounded-2xl`) on controls or containers, because those resolve from Tailwind's own radius namespace and do not follow the theme.

While library-authored markup still contains literal radius that the gateway cannot change, the gateway SHALL bridge it by aliasing Tailwind's radius namespace to the semantic tokens in `@theme inline`, and SHALL remove the alias once the upstream literals are converted.

#### Scenario: A theme radius change propagates to controls and containers

- **WHEN** `--radius-field` or `--radius-box` is changed in the theme block and the CSS is rebuilt
- **THEN** buttons, fields, cards, panels, dialogs, and badges pick up the new radius without any template change

#### Scenario: No literal radius on controls or containers

- **WHEN** the gateway templates and the gateway's own CSS are searched for Tailwind's literal radius scale
- **THEN** no occurrence sets the radius of a button, field, card, panel, dialog, or badge

#### Scenario: App CSS reads the theme variables

- **WHEN** the gateway's own CSS sets a border radius
- **THEN** it reads `var(--radius-box)`, `var(--radius-field)`, or `var(--radius-selector)` rather than a length literal

#### Scenario: The alias bridge is temporary and visible

- **WHEN** an alias is present because library markup still uses literal radius
- **THEN** it is declared in one place, documented as a bridge, and removed when the upstream literals are converted

### Requirement: Component padding and density have one central source

daisyUI exposes no theme token for component padding, so the gateway SHALL declare its component density in one documented block of its own CSS, and components SHALL NOT set their own padding on a daisyUI component root (`card-body`, `btn`, `input`, `select`, `textarea`, `menu`, `modal-box`). The intent is that adjusting density is one edit rather than a sweep; the assertions below are the two invariants that make that intent checkable.

#### Scenario: No padding utility on a daisyUI root outside the block

- **WHEN** the gateway templates are searched for padding utilities applied alongside a daisyUI component root class
- **THEN** no occurrence overrides that component's own padding, so the documented block is the only place density is set

#### Scenario: The one place is identifiable

- **WHEN** the gateway's own CSS is searched for the component-density declarations
- **THEN** they all live in the single documented block, which is identifiable by its accompanying note

#### Scenario: The lever is documented as non-token

- **WHEN** the density block is read
- **THEN** it states that daisyUI provides no padding token, that the block uses component-internal variables and layered overrides, and that it must be re-checked on a daisyUI upgrade

### Requirement: The page background has one source

The page background and its no-flash guard SHALL derive from the theme's background token, and the
theme-coloured surface that browser chrome takes its cue from SHALL derive from the theme's surface token.
Static metadata that cannot read CSS variables SHALL be co-located with the declaration it mirrors and
documented as manually kept in sync: the web manifest's background colour mirrors the page background
token, and the web manifest's theme colour and the `theme-color` meta tags mirror the surface token.

#### Scenario: One declaration drives the page background

- **WHEN** the page background is changed
- **THEN** the change is made in one place and no other rule or inline guard paints a different value

#### Scenario: Background and theme colour are not conflated

- **WHEN** the page background token and the surface token are compared with the static metadata
- **THEN** the manifest background mirrors the background token, and the manifest theme colour and the `theme-color` meta tags mirror the surface token, so neither pair mixes the two

#### Scenario: Static metadata is co-located and marked

- **WHEN** the `theme-color` meta tags or the web manifest colour fields are read
- **THEN** they sit next to the declaration they mirror and are annotated as literals that must be updated together

### Requirement: The CSS build input matches the pinned go-daisy version

The vendored go-daisy source that the CSS build scans and imports SHALL correspond to the version pinned
in the gateway module graph. Because that tree is a generated, untracked build input rather than a
committed artifact, its freshness SHALL NOT depend on a developer remembering to regenerate it.

#### Scenario: Regeneration is keyed to the pin

- **WHEN** the version pinned in `go.mod` changes, or no vendored tree exists
- **THEN** the vendored go-daisy tree is regenerated for that version before the CSS build scans it, rather than being reused because the directory already exists

#### Scenario: A stale tree cannot silently compile

- **WHEN** a vendored go-daisy tree is present and disagrees with the `go.mod` pin
- **THEN** the check fails rather than compiling the stale library source

#### Scenario: Vendored build succeeds when the tree is present

- **WHEN** a vendored tree exists at the pinned version and `go build -mod=vendor ./...` is run for the gateway
- **THEN** it succeeds
