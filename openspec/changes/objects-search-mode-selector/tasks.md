## 1. Default mode

- [x] 1.1 In `objects.go`, default an absent `mode` to `unified` in `uiObjects` (search branch) — verify a unit test asserts unified is used when `mode` is omitted
- [x] 1.2 Keep the partial handler's mode passthrough consistent with the new default — verify `go test ./...`

## 2. Split dropdown button

- [x] 2.1 Rewrite `objectsSearchForm` in `objects.templ` as a split button: primary submit labelled "<Mode> Search" plus a chevron dropdown of the three modes, each option with a label and a short description — verify a render test asserts the labels and descriptions
- [x] 2.2 Preserve the active type/branch filters (hidden inputs) and the query input — verify the existing filter/query assertions still pass

## 3. Verification

- [x] 3.1 Update `objects_test.go` render assertions for the new control — verify `go test ./...` passes in `gateway/`
- [x] 3.2 `templ generate` produces no diff; `go build ./...`, `go vet ./...`, and `task lint` pass
- [ ] 3.3 Manual browser check on `/objects`: default shows Unified Search, chevron opens the dropdown with descriptions, choosing Hybrid runs `mode=hybrid`
