## 1. OpenSpec artifacts

- [x] 1.1 Author `proposal.md`, `tasks.md`, and the `object-creation` + `object-browser` delta specs describing the multi-line long-text field, its live character count, and post-swap autogrow. Verify: `openspec validate object-long-text-fields --strict` passes.

## 2. Server-rendered field markup

- [x] 2.1 Add `charCountLabel` / `groupDigits` in `objects.go` (code-point count, comma-grouped). Verify: `TestCharCountLabel` passes.
- [x] 2.2 Add the `longTextarea` templ helper (auto-growing, `min-h-20 max-h-96 resize-y` textarea + `data-testid="char-count"` counter) and route `propertyInput`'s string path and the `objectPropertiesCard` fallback through it. Verify: `TestPropertyInput`, `TestObjectPropertiesCardLongTextFallback` pass.

## 3. Client behavior

- [x] 3.1 Extend `app.js`: counter init + update on input, `autogrowAll`/`charCountAll` on `htmx:after:swap`, and resize-y preservation across auto-grow. Verify: manual check on a spare port (or rely on render tests if the supervised dev service owns the default port).

## 4. Tests

- [x] 4.1 Extend `objects_test.go` table-driven render assertions: counter present for every multi-line string case (with initial count), absent for date/number/integer/array/object/enum/single-line-input/boolean; fallback textarea in the detail card counts exactly once. Verify: `go test ./...` passes.

## 5. Build, test, lint

- [x] 5.1 `templ generate`, `go build ./...`, `go test ./...`, `task lint` from `apps/web-ui/gateway` all pass (0 issues).

## 6. Commit, push, PR

- [x] 6.1 Stage only the changed files, commit `feat(web-ui): …`, push the branch, and open a PR against `main` (author does not merge).
