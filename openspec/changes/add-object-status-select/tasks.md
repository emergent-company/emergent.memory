## 1. Implementation

- [x] Add `objectStatusOptions` helper deriving the work statuses from a compiled
      object type (declared `allowedStatuses` when present, canonical board order
      otherwise — including a type with no board config; `nil` only when no type
      is selected).
- [x] Add an `objectStatusField` templ helper rendering a native `<select>` of
      the work statuses (current value selected, off-list value preserved, blank
      pick option when empty) or a free-form text input when there are no options
      (unknown/unloaded type).
- [x] Plumb `statusOptions []string` through `ObjectCreatePage`,
      `ObjectDetailPage`, and `objectPropertiesCard`, populated in the
      `uiObjectNew`, `uiObjectCreate`, and `uiObject` handlers.

## 2. Tests

- [x] Unit-test `objectStatusOptions` (declared statuses, canonical fallback for
      board-enabled and for a type with no board config, unknown type fallback,
      `nil` when no type is selected).
- [x] Render-test the edit page select (options + current status selected +
      humanised labels), the create page select, the canonical fallback, the
      off-list value preservation, and the blank pick option.
- [x] Regression-test the reported shape: a type whose registry row declares
      neither `boardEnabled` nor `allowedStatuses` renders a `<select>` of the
      canonical statuses (not a text input), at the render and handler-wiring
      levels.
- [x] Verify: `templ generate`, `go build ./...`, `go test -count=1 ./...`,
      js-dom Playwright gate, lint-ratchet, `openspec validate --strict`.
