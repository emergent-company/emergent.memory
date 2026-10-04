## Why

On a board-enabled object type (e.g. `Task`), the object create/edit forms render
the built-in `status` field as a plain text input. Users can therefore type any
string, including values the board-enabled type forbids, and the picker gives no
hint of the allowed work statuses. The schema already exposes the type's board
configuration (`boardEnabled` / `allowedStatuses`) through the compiled-types
view, so the form can constrain the field without any backend change.

## What Changes

- Render the built-in `status` field in the object create and edit forms as a
  native `<select>`: the type's declared `allowedStatuses` when present,
  otherwise the canonical board order. This covers every type that reaches the
  form, including a type with no board config (the reported `Task` shape), whose
  objects still render on the board via the canonical-lane fallback.
- Mark the object's current status as the selected option; preserve a stored
  value that is outside the option set as an extra selected option so saving
  never drops data; render a blank pick option when the current value is empty.
- The free-form text input is retained only when no options can be resolved —
  the edit form when the compiled types fail to load (a create form with no type
  selected renders no status field).
- No backend/API change: the handlers already read the submitted `status` value.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `object-driven-agent-work`: the object create/edit forms render the built-in
  status field as a select of the type's declared work statuses, or the canonical
  board order when it declares none — for every type presented on the form,
  including a type with no board config.

## Impact

- **Gateway templates**: `apps/web-ui/gateway/objects.templ` — new
  `objectStatusField` helper; `ObjectCreatePage`, `ObjectDetailPage`, and
  `objectPropertiesCard` take a `statusOptions []string` parameter.
- **Gateway Go**: `apps/web-ui/gateway/objects.go` — new `objectStatusOptions`
  helper; create/detail handlers populate the parameter from the compiled type.
- **Tests**: `apps/web-ui/gateway/objects_test.go` render + handler tests.
- **Data/API**: none.
