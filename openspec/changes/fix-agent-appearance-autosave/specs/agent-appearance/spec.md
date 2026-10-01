## MODIFIED Requirements

### Requirement: Edit appearance in the Web UI

The Web UI agent editor (settings form and create/edit modal) SHALL use the same pickers
object types use — `ui.IconPicker` and `ui.ColorPicker`, backed by
`supportedIconPickerOptions()` and `schemaColorPresets`. The icon catalog SHALL include
`lucide--bot`.

The General settings form auto-saves (it has no Save button), so a change made through
either picker SHALL be persisted by that auto-save regardless of how the value was
entered: typing into the colour text field, choosing a preset swatch, using the native
colour swatch, or clearing. A save whose request body does not carry the `icon`/`color`
fields SHALL preserve the stored appearance rather than clear it.

#### Scenario: Pick icon and color

- **WHEN** the owner opens the agent editor and selects an icon and color
- **THEN** the selection is saved to `ui_config` and rendered on the agent

#### Scenario: Clear appearance to defaults

- **WHEN** the owner clears the icon and color
- **THEN** `ui_config` becomes `{}` and the agent renders the neutral `lucide--bot` tile

#### Scenario: Preset colour persists through the settings auto-save

- **WHEN** the owner picks a colour from a preset swatch on the General settings form
- **THEN** the debounced auto-save persists it to `ui_config`, and it survives a reload

#### Scenario: Partial General save preserves the stored appearance

- **WHEN** a General save request omits the appearance fields
- **THEN** the stored `ui_config` is left unchanged
