# web-ui-component-conventions Specification

## Purpose
The gateway's UI component conventions: how components are layered, when a component earns its place, how
they are composed, how their props and variants are named, who owns layout and the htmx swap contract, and
what component tests may assert. These rules are generic to the gateway UI; the concrete single-sourcing
requirements for individual surfaces live in `web-ui-components`.

## Requirements

### Requirement: Components are layered and imports flow one way

Gateway UI code SHALL be organised in layers with imports flowing downward only:

- **L0** go-daisy primitives.
- **L1** domain adapters that map domain vocabulary to presentation intent and add no markup beyond that mapping.
- **L2** app composites that render markup only — no domain vocabulary, no route knowledge, no configuration reads.
- **L3** page-local helpers, private to one page and promoted to L2 when a second page needs them.
- **L4** pages, which compose L0–L3.

An L2 component SHALL delegate to L0 for primitives rather than re-implementing markup the primitive already provides.

#### Scenario: A composite delegates instead of re-implementing

- **WHEN** a composite component needs a primitive defined at L0
- **THEN** it renders the L0 component rather than reproducing its markup

#### Scenario: Domain knowledge stays out of the composite layer

- **WHEN** a component needs to know what a domain status means
- **THEN** that mapping lives in an L1 adapter, and the L2 component receives presentation intent only

#### Scenario: A page-local helper is promoted on reuse

- **WHEN** a helper used by one page becomes needed by a second page
- **THEN** it moves to the shared composite layer rather than being copied

### Requirement: A component earns its place

A component SHALL be created only when at least one of these holds: it is used by two or more call sites,
it encapsulates an invariant (identifiers, `data-testid`s, `aria-*`, htmx wiring, or class-merge order),
or its markup or branching is non-trivial. Otherwise the markup SHALL stay inline at its call site.

A component SHALL NOT accumulate per-caller conditionals: when the varying parts cannot be expressed as
props without internal branching on the caller, the abstraction SHALL be reverted by re-inlining it into
its callers and re-extracted later.

#### Scenario: Single-use markup stays inline

- **WHEN** a piece of markup is used once and encapsulates no invariant
- **THEN** it does not become a shared component

#### Scenario: An abstraction that branches on its callers is reverted

- **WHEN** a shared component needs an internal conditional for each new caller in order to work
- **THEN** the component is re-inlined into its callers rather than extended with another conditional

#### Scenario: An existing implementation is reused rather than duplicated

- **WHEN** a component for a surface already exists at any layer
- **THEN** the new call site uses it instead of adding a further implementation of the same surface

### Requirement: Variants use one axis vocabulary aligned with daisyUI

Component variant props SHALL use four axes named `Tone`, `Style`, `Size`, and `State`, aligned with the
daisyUI class families they map to: tone for semantic colour, style for fill treatment, size for scale,
and state for interaction state. A structural difference in a component's shape SHALL be modelled as its
own variant axis and SHALL NOT be expressed as a tone.

#### Scenario: Semantic colour uses Tone

- **WHEN** a component expresses semantic colour
- **THEN** it takes a `Tone` value from the aligned set (primary, secondary, accent, neutral, info, success, warning, error)

#### Scenario: Structural variation is not a tone

- **WHEN** a component has two different content shapes, such as a compact list item and a hero block
- **THEN** that difference is a dedicated variant axis, not a tone value

### Requirement: Mutually exclusive presentation states are one enum, not booleans

Presentation states that cannot hold simultaneously SHALL be modelled as a single enum value. Boolean props
SHALL be reserved for genuinely independent two-state flags such as disabled, loading, or selected.

#### Scenario: Mutually exclusive states cannot be combined illegally

- **WHEN** a component has several mutually exclusive presentation modes
- **THEN** its API accepts one mode value, so an invalid combination cannot be expressed

#### Scenario: Genuine flags remain boolean

- **WHEN** a property is independently true or false, such as loading or disabled
- **THEN** it is modelled as a boolean, not folded into an enum

### Requirement: Colours come from theme tokens, not CSS strings

A component SHALL express colour through a tone token. Passing a colour as an arbitrary CSS class string
in a prop SHALL be limited to genuinely data-driven accents defined outside the component.

#### Scenario: Tone replaces inline colour classes

- **WHEN** a component needs a semantic colour
- **THEN** it derives the classes from its tone value rather than accepting pre-built colour classes

#### Scenario: Data-driven accent is the only exception

- **WHEN** a colour is supplied by data rather than by the design system
- **THEN** a raw value is accepted, and this is documented as the exception

### Requirement: Components do not own external layout

A component SHALL own its internal padding and rhythm. It SHALL NOT own its own external margin, maximum
width, or grid placement, and SHALL NOT accept the caller's external layout as a prop. Callers SHALL own
the space between components.

#### Scenario: Spacing between components belongs to the caller

- **WHEN** two components are stacked on a page
- **THEN** the spacing between them is applied by the caller or a shared rhythm utility, not baked into either component

#### Scenario: Internal padding is a density choice

- **WHEN** a caller needs a component with different internal padding
- **THEN** it selects a density or size variant rather than passing a padding class

### Requirement: Components use theme-driven radius, not literal radius

A component SHALL express corner radius through the theme-driven utilities (`rounded-box`,
`rounded-field`, `rounded-selector`) and SHALL NOT use Tailwind's literal radius scale, which follows the
theme only once a bridge aliases it. `rounded-full` is exempt where a deliberate circle is intended
(avatars, status dots, pills). A component SHALL NOT override the padding of a daisyUI component root it
renders, so density stays editable in one place.

While library-authored markup still contains literal radius the gateway cannot change, a bridge aliases
Tailwind's radius namespace onto the semantic tokens; that bridge is what makes the literal radius in
library markup theme-following, and it is removed once upstream is converted.

#### Scenario: Radius follows the theme

- **WHEN** the theme's field or box radius changes
- **THEN** the component's radius changes without a template edit

#### Scenario: Literal radius is not used on controls or containers

- **WHEN** a component sets the radius of a button, field, card, panel, dialog, or badge
- **THEN** it uses a theme-driven radius utility or variable rather than a literal radius step

#### Scenario: Component padding is not overridden

- **WHEN** a component renders a daisyUI component root
- **THEN** it does not add its own padding to that root, so density remains centrally editable

### Requirement: Component content is composed, not configured

Content SHALL be supplied through children or named slots, and a prop SHALL NOT be used for content that is
structural. A component SHALL expose an attribute bag spread onto its root element, and SHALL append the
caller's class to its base classes.

#### Scenario: Structural content is a slot

- **WHEN** a caller supplies a region of a component, such as its actions or footer
- **THEN** it passes a slot rather than a configuration prop encoding that region

#### Scenario: Caller attributes reach the root

- **WHEN** a caller passes attributes to a component
- **THEN** they are spread onto the component's root element after the component's own attributes, and any caller class is appended to the base classes

### Requirement: The integration site owns the htmx swap contract

`hx-target`, `hx-swap`, `hx-trigger`, and `hx-include` SHALL be supplied by the page or integration site
through the component's attribute bag. A component SHALL own only client wiring that is meaningless without
it, such as a copy target or a dialog auto-open marker, and a component whose contract is itself a swap
region SHALL ship a default only where it also exposes an override.

#### Scenario: Swap behaviour is not hardcoded

- **WHEN** a component is used with htmx
- **THEN** the swap target and strategy come from the call site, not from the component's own markup

#### Scenario: Self-contained client wiring is allowed

- **WHEN** a control is unusable without an attribute only that component can supply
- **THEN** the component supplies it, and the attribute is documented as part of its contract

### Requirement: Component naming follows one convention

Components SHALL be named `PascalCase`, noun-first, without verb prefixes. Variant types SHALL be named
`<Component><Axis>`. Slots SHALL use the shared role names (`Leading`, `Trailing`, `Title`, `Actions`,
`Footer`, `Empty`, `Description`). Gateway-owned CSS classes SHALL follow
`memory-<block>__<element>--<modifier>`.

#### Scenario: Slot and variant names are predictable

- **WHEN** a developer uses a component's slot or variant types
- **THEN** the names follow the shared role and `<Component><Axis>` conventions rather than per-component invention

#### Scenario: App CSS is namespaced

- **WHEN** the gateway adds a CSS class of its own
- **THEN** it follows the `memory-` block/element/modifier pattern, and anything else is a daisyUI class or a Tailwind utility

### Requirement: Component tests assert the contract, not class order

Component unit tests SHALL assert the component's contract: identifiers, `data-testid` values, `aria-*`
attributes, htmx wiring, and the client markers a component is responsible for; and SHALL assert omission
where absence is part of the contract. Unit tests SHALL NOT pin class names or class order as an equality
assertion, because class composition is not part of the contract and a cosmetic change would otherwise fail
a test. Where a class-string assertion is retained for a genuinely load-bearing class, it SHALL live in
shared test data with an explicit update flag rather than as an inline whole-string equality.

A small set of class strings genuinely **are** contractual, because the class *is* the component's API for
that surface, and requirements pinning them are exempt from this policy: the tool-group disclosure base
classes (which carry the per-variant border and background tone), and the confirm-dialog icon circle. Those
remain asserted exactly; this policy governs appearance-only classes, not these.

Visual and interaction verification SHALL be performed through the component gallery and the browser test
suite rather than through byte-exact DOM assertions.

#### Scenario: Contract attributes are asserted

- **WHEN** a component's tests run
- **THEN** they verify the identifiers, ARIA attributes, htmx wiring, and client markers the component promises

#### Scenario: Cosmetic class changes do not fail tests

- **WHEN** a component's class composition changes without altering its contract
- **THEN** its unit tests still pass

#### Scenario: Visual and interaction behaviour is verified in the browser

- **WHEN** a component's appearance or interaction changes
- **THEN** the gallery or browser test suite is the place that detects it
