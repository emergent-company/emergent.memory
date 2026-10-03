## 1. Server — delivery-log read path (invites domain)

- [x] 1.1 Add `InviteDeliveryEvent` / `InviteDeliveryLogEvent` and `SentInvite.deliveryLog` (`apps/server/domain/invites/entity.go`)
- [x] 1.2 `ListByProject` attaches each invitation's full send history via the batched `inviteDeliveryLogs` helper — jobs newest first, then Mailgun events oldest first; no per-invitation round trip (`apps/server/domain/invites/service.go`)
- [x] 1.3 Integration test: multiple sends are returned newest first with their delivery events and bounce `reason`, an invitation with no sends returns an empty log, and another project's sends do not leak (`apps/server/domain/invites/delivery_log_test.go`)

## 2. SDK parity

- [x] 2.1 Mirror `InviteDeliveryEvent` / `InviteDeliveryLogEvent` and `SentInvite.deliveryLog` (`apps/server/pkg/sdk/invitations/client.go`)

## 3. Web UI — hover/focus delivery log (gateway)

- [x] 3.1 Add the delivery-log DTOs to `SentInviteDto` (`apps/web-ui/gateway/memory_orgs.go`)
- [x] 3.2 Render the log inside the go-daisy popover on a hover/focus trigger on the invitation row; preserve the lifecycle/delivery badges and row actions (`apps/web-ui/gateway/org_members_ui.templ`)
- [x] 3.3 Presentation helpers for send state, event label, and count (`apps/web-ui/gateway/org_members_ui.go`)
- [x] 3.4 Load the popover runtime on the members surface (`ui.PopoverScript()`)
- [x] 3.5 Render test: the row carries a hover popover whose content shows each send's state/date/bounce detail, newest first, with a neutral empty state (`apps/web-ui/gateway/org_members_ui_test.go`)
- [x] 3.6 `templ generate ./...` + `go generate ./...` so generated templ and the component graph match the source

## 4. Verification

- [x] 4.1 `go build ./...` and `go test -count=1 ./domain/invites/... ./domain/email/...` in `apps/server`
- [x] 4.2 `templ generate` + `go build ./...` + `go test ./...` in `apps/web-ui/gateway`
- [x] 4.3 js-dom gate (`npx playwright test --config=js-dom.config.ts`) and `golangci-lint run ./...`
- [ ] 4.4 Manual check on the dev server: hovering a sent-invitation row shows its delivery log
