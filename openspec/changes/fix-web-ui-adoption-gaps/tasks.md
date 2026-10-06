# Tasks — fix-web-ui-adoption-gaps

Run commands from `apps/web-ui/gateway` unless stated otherwise. `*_templ.go` is generated and gitignored — never stage it.

## 1 — Detector signature correctness

- [x] 1.1 In `cmd/componentgraph/signatures.go`, remove `modal-action` as a tracked marker from `Dialog` (the caller owns the action row). It is also removed from `SecretRevealModal`: the component's own action row lives in the `components/` package (never scanned), and authored action rows sit inside an adopted dialog's children, so a raw dialog is still caught by the `modal`/`modal-box`/`modal-backdrop`/`dialog` markers.
- [x] 1.2 Add `ToggleInput` as an owner of the `toggle` marker, alongside `ToggleField`.
- [x] 1.3 Update `cmd/componentgraph/adoption_test.go` expectations for both changes.
- [x] 1.4 Regenerate `devgallery_adoption_gen.go`; confirm the three `modal-action` findings are gone.

## 2 — Buttons (27 findings → `ui.Button`)

- [x] 2.1 `<button>` sites: `agent.templ:377`, `blueprints.templ:731`, `chat.templ:166`, `chat_dock.templ:44/45/46/81/82`, `inbox.templ:208/226`, `project_settings.templ:908`, `schema.templ:438/519/544/547/581`, `share_manage.templ:591/607` — map variant/size/shape, keep `data-*`/`aria-*`/`disabled` and app classes (`dock-*`, `text-error`, `btn-square`) in `Attrs`/`ExtraClass`.
- [x] 2.2 `<a>` sites: `agent_mcp_endpoint.templ:434`, `documents.templ:198/250`, `inbox.templ:73/306`, `notifications_bell.templ:26`, `schema.templ:466`, `usage.templ:63` — `Type: ui.ButtonTypeLink` + `Href`, conditional variants preserved.
- [x] 2.3 join-group `<label>`: `objects.templ:317` — `Type: ui.ButtonTypeLabel` + `ExtraClass: "join-item"`, active variant conditional.
- [x] 2.4 Verify: `templ generate -f <files>` then `go build ./...`.

## 3 — Cards (14 findings → shared card)

- [x] 3.1 `approvals.templ:40/65/131`, `objects.templ:407/452/576`, `share_manage.templ:224` — render through `ui.CardRaw`, preserving card class, body density classes, and every `data-testid`/id.
- [x] 3.2 Verify: `templ generate -f <files>` then `go build ./...`.

## 4 — Badges and toggles (7 + 4 findings)

- [x] 4.1 Badges: `chat.templ:574`, `inbox.templ:79/166/169/369`, `notifications_bell.templ` (n/a) — render through `ui.Badge`, preserving variant/size/icon. (`inbox.templ` tab-count badge `:106` and `notifications_bell.templ:34` indicator badge could not be reproduced exactly — allowlisted in §5.)
- [x] 4.2 Toggles: `inbox.templ:361`, `project_settings.templ:1586`, `share_manage.templ:551/566` — render through `form.ToggleInput`, passing the existing class string (`toggle-sm`, `toggle-warning`, `mt-0.5`).
- [x] 4.3 Verify: `templ generate -f <files>` then `go build ./...`.

## 5 — Accepted local markup (9 findings)

- [x] 5.1 `devgallery_adoption.go`: add `adoptionAllowlist` entries with reasons — `runs.templ: {badge}` (link-styled badges; `ui.Badge` has no href), `chat.templ: {alert}` and `share_page.templ: {alert}` (rich children; `ui.Alert` has no children slot), `share_manage.templ: {card, card-border}` (form card), `documents.templ: {card-border}` (border-only row), `inbox.templ: {badge}` and `notifications_bell.templ: {badge}` (tab-count direct text / indicator-positioned badge that `ui.Badge` cannot reproduce).
- [x] 5.2 File upstream go-daisy issues: (a) `ui.Badge` href/link support — emergent-company/go-daisy#24, (b) `ui.Alert` body/children slot — emergent-company/go-daisy#25, (c) `ui.Badge` direct-text/indicator variant — emergent-company/go-daisy#26; referenced in the PR body.

## 6 — Verification

- [x] 6.1 `task generate`, `go build ./...`, `go test ./...` — all green from `apps/web-ui/gateway`.
- [x] 6.2 Regenerated findings contain only the allowlisted 9 sites; gallery `adoptionGapSection` renders the empty state (`TestAdoptionSectionEmptyState`, `TestUIComponentGalleryRendersAdoption`).
- [x] 6.3 `task lint` from `apps/web-ui`; all web-ui jobs clean (repo-wide run additionally hits a pre-existing, unrelated `connector.linux` date-bomb test).
- [x] 6.4 Playwright gallery e2e (`task e2e:gallery`) — 3 passed, including the adoption-gaps spec now exercising the zero-findings branch.
