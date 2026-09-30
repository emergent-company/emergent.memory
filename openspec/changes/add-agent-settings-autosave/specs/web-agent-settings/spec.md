## ADDED Requirements

### Requirement: Debounced auto-save on the agent General settings form

The agent General settings form SHALL auto-save its fields as the user edits,
without an explicit submit step. Text fields SHALL save after a short debounce
(≈700 ms) once typing stops, not on every keystroke; selects, the icon/colour
pickers, and the Visibility listbox SHALL save once their value changes. A save
SHALL be sent to a JSON endpoint that applies the same General section applier
and answers `200 {"ok":true}` on success or `422 {"ok":false,"error":…}` on
validation failure, so errors surface inline without a page navigation. An
unchanged value SHALL NOT post, and the form SHALL keep its PRG POST route as a
no-JavaScript fallback. The General form SHALL NOT render the shared explicit
save button; every other settings subpage SHALL keep it.

#### Scenario: Typing saves once after the debounce

- **WHEN** the owner edits the name, description, or system prompt
- **THEN** one save is sent after typing stops — not one per keystroke — and a status indicator reports saving then saved

#### Scenario: Non-text changes save promptly

- **WHEN** the owner changes the language select, the icon/colour picker, or the visibility listbox
- **THEN** the change is saved without waiting for the text debounce

#### Scenario: Unchanged value does not post

- **WHEN** a field is re-entered with the value it already holds
- **THEN** no save request is sent

#### Scenario: Validation failure surfaces inline and preserves the edit

- **WHEN** the server rejects a save (for example an empty required name)
- **THEN** the status indicator shows the error with a retry affordance and the edited values remain in the form

#### Scenario: Pending edit is not lost on in-app navigation

- **WHEN** the owner edits a text field and navigates to another settings subpage before the debounce elapses
- **THEN** the pending change is flushed before the navigation

#### Scenario: Pending debounce is cancelled on unload

- **WHEN** the page is unloaded before the debounce elapses
- **THEN** the pending timer is cancelled rather than firing against a torn-down document
