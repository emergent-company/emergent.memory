# Tasks — refactor-webui-adopt-page-header

Final adoption lane: migrate the last non-catalog local header component, `pageHeader`, onto go-daisy. Run commands from `apps/web-ui/gateway` unless stated otherwise. Generated `*_templ.go` are gitignored — run `templ generate`, never stage the output.

## 1 — Bump go-daisy pin

- [x] 1.1 `go get github.com/emergent-company/go-daisy@062842a29737b686ba2cbaeda169bf81bff2cd3c` + `go mod tidy` + `GOWORK=off go mod vendor`; confirm `go.mod` pins `062842a29737`. Verify: `go build ./...` green.

## 2 — pageHeader onto nav.PageHeading

- [x] 2.1 Replace the `templ pageHeader` body with a plain Go function returning `templ.Component` that reads `templ.GetChildren(ctx)` and delegates to `nav.PageHeading` with `Title`/`Kicker`/`Subtitle`/`Actions`/`Margin: "mb-6"`/`Dashboard: true`/`SubtitleFull: true`/`HideBreadcrumbs: true`/`NoTopMargin: true`/`Flat: true`.
- [x] 2.2 Confirm the `Flat` + `Dashboard` + `NoTopMargin` combination reproduces the prior `<div class="mb-6 flex flex-wrap items-end justify-between gap-4">` wrapper byte-for-byte (kicker eyebrow + `lg:text-3xl` title + `text-base-content/55 mt-1 text-sm` subtitle, actions in the right slot). Verify: `go build ./...` and the exact-markup suites (`page_width_consistency_test.go`, `refactor_exact_test.go`, `css_consolidation_test.go`, `adoption_contract_test.go`) green.

## 3 — OpenSpec

- [x] 3.1 Create `openspec/changes/refactor-webui-adopt-page-header` (proposal, tasks, delta spec on `web-ui-components`). Verify: `openspec validate` clean.

## 4 — Verification

- [x] 4.1 `task css` && `templ generate ./...` && `go build ./...` && `go test ./...` && `task lint` — all clean.
