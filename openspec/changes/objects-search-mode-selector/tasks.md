## 1. Default mode

- [x] 1.1 In `objects.go`, default an absent `mode` to `unified` in `uiObjects` (search branch) — verify a unit test asserts unified is used when `mode` is omitted
- [x] 1.2 Keep the partial handler's mode passthrough consistent with the new default — verify `go test ./...`
- [x] 1.3 Normalize `mode` once in `uiObjects` against the known set (`unified|hybrid|fulltext`, unknown/absent → unified) so the label, hidden/submit value, and dispatch agree — verify a unit test covers an unknown value

## 2. Split dropdown button

- [x] 2.1 Rewrite `objectsSearchForm` in `objects.templ` as a split button: primary submit labelled "<Mode> Search" plus a chevron dropdown of the three modes, each option with a label and a short description — verify a render test asserts the labels and descriptions
- [x] 2.2 Preserve the active type/branch filters (hidden inputs) and the query input — verify the existing filter/query assertions still pass
- [x] 2.3 Make mode selection a plain form submit (`name="mode" value=…`) so it works with JavaScript disabled; drop the misleading static `aria-expanded` on the CSS-only dropdown trigger
- [x] 2.4 Move the selected-state ARIA onto the menu item anchor (`role="menuitemradio"` + `aria-checked`) so it is actually announced

## 3. Verification

- [x] 3.1 Update `objects_test.go` render assertions for the new control — verify `go test ./...` passes in `gateway/`
- [x] 3.2 `templ generate` produces no diff; `go build ./...`, `go vet ./...`, and `task lint` pass
- [ ] 3.3 Manual browser check on `/objects`: default shows Unified Search, chevron opens the dropdown with descriptions, choosing Hybrid runs `mode=hybrid` (the JS path this checked was removed; covered by unit tests + the no-JS submit contract)
