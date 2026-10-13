## ADDED Requirements

### Requirement: Question and approval cards render through a single component

The gateway SHALL define the pending-question card and the pending-approval card once, as components
in `apps/web-ui/gateway/components`, and every server-rendered consumer SHALL render those components
rather than its own markup. The chat's client-side renderer SHALL mirror the same structure — the
same `data-testid` set and class skeleton — and a test SHALL fail when the two diverge.

The component SHALL present a question's options as selectable rows carrying the interaction's
indicator role (`radio` for a single choice, `checkbox` for a multi-select), the option's label and
its optional description, and it SHALL render the answered state as a non-interactive card with the
chosen answer reflected and its controls disabled. An approval card SHALL present the tool, its
redacted argument summary, the decision, and the approve / reject / cancel controls.

#### Scenario: One definition serves every server-rendered surface

- **WHEN** the gateway templates are searched for a second implementation of a question card or an approval card
- **THEN** none remains, and the human-input surface and the chat's no-JS dock both render the shared components

#### Scenario: A question's options render as described rows

- **WHEN** a question with options is rendered
- **THEN** each option is a row carrying its indicator role, label, and description when present

#### Scenario: An answered question renders without controls

- **WHEN** a question that has already been answered is rendered
- **THEN** the card shows the chosen answer and offers no interactive controls

#### Scenario: The chat renderer cannot drift from the component

- **WHEN** the client-side chat renderer's emitted testids or class skeleton diverge from the component's
- **THEN** the contract test fails
