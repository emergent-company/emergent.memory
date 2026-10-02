## 1. Email tracking (server)

- [x] 1.1 Enable open/click tracking on outbound mail in `MailgunSender.Send` (`apps/server/domain/email/mailgun.go`) via `message.SetTrackingOptions(&mailgun.TrackingOptions{Tracking: true, TrackingOpens: true, TrackingClicks: "yes"})`
- [x] 1.2 Map `opened` → `DeliveryStatusOpened` and `clicked` → `DeliveryStatusClicked` in `deliveryStatusForEvent` (`apps/server/domain/email/delivery_store.go`)
- [x] 1.3 Unit test: `deliveryStatusForEvent` maps delivered/opened/clicked/failed(permanent|temporary)/bounced/rejected/dropped/complained/unsubscribed and leaves unmapped events unmapped
- [x] 1.4 Unit test: the built Mailgun message carries the tracking parameters (`o:tracking`, `o:tracking-opens`, `o:tracking-clicks`)

## 2. Invite delivery state (server)

- [x] 2.1 Add `DeliveryStatus *string` + `DeliveryStatusAt *time.Time` to `SentInvite` (`apps/server/domain/invites/entity.go`)
- [x] 2.2 `ListByProject` (`apps/server/domain/invites/service.go`) selects the invite's most recent email job delivery state (`source_type='invite' AND source_id=i.id`) — LATERAL/most-recent per invite, no N+1
- [x] 2.3 Unit/integration test: an invite whose email job has `delivery_status='opened'` is returned with `deliveryStatus="opened"`; an invite with no job returns null

## 3. Resend invitation (server)

- [x] 3.1 Extract the invite-email enqueue block in `Create` into a reusable helper (`enqueueInviteEmail`) (`apps/server/domain/invites/service.go`)
- [x] 3.2 Add `Service.Resend(ctx, inviteID)`: load invite; require `status='pending'` (else HTTP 404, matching revoke's non-pending guard); keep the token; extend `expires_at` to now+7d; re-enqueue via the helper; return the invite
- [x] 3.3 Add `Resend` handler `POST /api/invites/:id/resend` with the same org-admin authority check as `Delete` (`apps/server/domain/invites/handler.go`, `module.go`), plus swagger annotation
- [x] 3.4 Unit tests: resend re-enqueues a `project-invitation` job and extends expiry; resend of accepted/revoked/declined/unknown invite returns 404; non-org-admin returns 403
- [x] 3.5 SDK: add `deliveryStatus`/`deliveryStatusAt` to `SentInvite` and a `Resend(ctx, inviteID)` method (`apps/server/pkg/sdk/invitations/client.go`)
- [x] 3.6 Server-enforced resend idempotency guard: `Resend` is a no-op (HTTP 200, existing invitation, no enqueue) when the invitation already has an invite-scoped `kb.email_jobs` row created within `inviteResendGuardWindow` (60s), keyed per invitation (`hasRecentInviteEmailJob`, `apps/server/domain/invites/service.go`)
- [x] 3.7 Tests: a repeat resend within the guard window does not enqueue a second job; a resend past the window does enqueue; the guard is per-invite (`apps/server/domain/invites/resend_test.go`)
- [x] 3.8 Make the guard atomic (issue #1328): `Resend` runs the check-then-enqueue in one transaction holding a per-invite `pg_advisory_xact_lock`, and enqueues via `JobsService.EnqueueTx` on that transaction, so N concurrent resends for the same invite enqueue exactly one job; concurrency test (`TestResendConcurrentEnqueuesExactlyOne`, `apps/server/domain/invites/resend_test.go`)

## 4. Web UI (gateway)

- [x] 4.1 Add `DeliveryStatus *string` + `DeliveryStatusAt *time.Time` to `SentInviteDto` (`apps/web-ui/gateway/memory_orgs.go`) and add `ResendInvite(ctx, inviteID)` to the backend interface + `MemoryClient` (`POST /api/invites/{id}/resend`)
- [x] 4.2 Add `uiResendInvite` handler + `POST /invites/:id/resend` route; redirect back to the current members surface (do not hardcode `/members` like revoke does)
- [x] 4.3 `memberInviteRow` (`apps/web-ui/gateway/org_members_ui.templ`): render an email delivery badge (Sent / Delivered / Opened / Clicked / Bounced / Complained) alongside the "Not responded" badge, and a Resend button (with confirmation) for pending invites
- [x] 4.4 Gateway unit tests: resend route forwards and redirects; the row renders the delivery badge and resend control
- [x] 4.5 `templ generate` so generated `*_templ.go` matches the source

## 5. Verification

- [x] 5.1 `go build ./...` and `go test ./...` in `apps/server`
- [x] 5.2 `templ generate` + `go build ./...` + `go test ./...` in `apps/web-ui/gateway`
- [x] 5.3 `task lint` (server + web-ui)
- [ ] 5.4 Manual check on the dev server: invite list shows delivery state; Resend re-sends and extends expiry
- [x] 5.5 `openspec validate invite-email-tracking-and-resend`
- [x] 5.6 Playwright e2e spec added (`invite-resend-ui.spec.ts`); live run against this branch's gateway on the dev members page verified the UI half (delivery badge + resend control render, confirm dialog, PRG to `/members?resent=1`). The resend API round-trip cannot pass until the server half is deployed: the gateway's `POST /api/invites/:id/resend` call returns 404 from the currently-deployed dev API, and `deploy-dev.yml` deploys the default branch only (its sole input is `target`). Re-run the spec after this PR is deployed to dev.
