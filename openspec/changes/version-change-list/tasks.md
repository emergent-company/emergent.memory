<!-- openspec:archive-hold: capability web-object-preview-history is shared with the still-unarchived board-ux-improvements, whose base history requirements must land first; do not archive version-change-list alone -->
# Tasks

## 1. Gateway client decodes the per-version change_summary

- [x] 1.1 Add `ChangeSummary map[string]any` (`json:"change_summary"`) and `Assignee string` (`json:"assignee"`) to the gateway `GraphObject` (`memory_graph.go`) so the server's per-version data survives decoding.
- [x] 1.2 Unit: pin the decode in `TestGetObjectHistoryClient` — assert `change_summary.added/removed/updated` survive and that a version with no summary decodes to nil (`object_preview_test.go`).

## 2. Change extraction and rendering

- [x] 2.1 Add `objectPreviewChanges` + `objectPreviewChangesFromSummary` + `objectPreviewChangesDerived` + value/equality/scalar helpers in `object_preview.go` — recorded summary wins; derived fallback for a nil summary; muted note when nothing derivable.
- [x] 2.2 Render each history entry as a collapsed native `<details>/<summary>` (keyboard operable, no JS) with a change list body (`object_preview.templ`).
- [x] 2.3 Unit: added/removed/updated rendering, merge provenance, derived status/assignee/property fallback, the no-changes note, the disclosure contract, and tolerance of unexpected `change_summary` shapes (`object_preview_test.go`).

## 3. E2E

- [x] 3.1 Hermetic DOM-level Playwright spec asserting an entry expands on click and on Enter to reveal its change list (`specs/js/object-preview-history-changes.spec.ts`).

## 4. Verification

- [x] 4.1 `templ generate` + `go generate ./...` + `task css` + `go build ./...` in `apps/web-ui/gateway`.
- [x] 4.2 `go test ./... -short` green in `apps/web-ui/gateway`; `golangci-lint run ./...` clean.
- [x] 4.3 `openspec validate version-change-list` passes with no archive-refusal advisory.
- [x] 4.4 `bash apps/server/scripts/lint-ratchet.sh` green from the worktree root.
