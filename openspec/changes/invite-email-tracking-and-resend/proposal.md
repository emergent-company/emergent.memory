## Why

Invitation emails are already enqueued as `kb.email_jobs` rows with `source_type='invite'` and `source_id=<invite.id>`, and a Mailgun webhook reconciles delivery events onto those jobs — but the invitee-facing lifecycle is invisible and unrecoverable:

- Mailgun open/click tracking is **never enabled at send time** (`MailgunSender.Send` sets no tracking options), so `opened`/`clicked` events never fire.
- `opened` and `clicked` are **not mapped** in `deliveryStatusForEvent`, so even if they fired they would only be audited, not reflected in `delivery_status`.
- `GET /api/projects/:id/invites` and the members UI expose only the invitation lifecycle status (`pending`/`accepted`/…), never whether the email was delivered, opened, bounced, or complained about.
- There is **no resend path**: `POST /api/invites` refuses a duplicate pending invite (HTTP 400), so an admin whose invitee lost or never received the email must revoke and re-invite.

Result: operators cannot tell whether an invite was delivered or opened, and admins have no recovery action.

## What Changes

- Enable Mailgun open/click tracking on outbound mail at send time.
- Map `opened` → `opened` and `clicked` → `clicked` delivery statuses in the webhook ingest path.
- Surface per-invite email delivery state (`deliveryStatus`, `deliveryStatusAt`) on `GET /api/projects/:projectId/invites`, resolved from the invite's most recent email job (`source_type='invite'`, `source_id=invite.id`).
- Add `POST /api/invites/:id/resend`: re-enqueue the `project-invitation` email for a pending invite, keeping the same token and extending `expires_at` to 7 days from now. Gated on the caller being an `org_admin` of the invitation's organization (or an active `superadmin_full`), matching revoke.
- Web UI: show a delivery-state badge on each sent invite row and a "Resend" action next to "Revoke" for pending invites.

## Capabilities

### New Capabilities

- `web-invite-delivery`: delivery-state badge and resend action on the sent-invitations list in the Web UI.

### Modified Capabilities

- `project-invitations`: the project invitations list carries per-invite email delivery state; a new resend endpoint re-sends a pending invitation.
- `email-delivery-status`: delivery-event mapping covers `opened`/`clicked`; open/click tracking is enabled on outbound mail.

## Impact

- `apps/server/domain/email/mailgun.go` — enable tracking on the outbound message.
- `apps/server/domain/email/delivery_store.go` — map `opened`/`clicked`.
- `apps/server/domain/invites/service.go` — `ListByProject` joins email jobs for delivery state; extract the enqueue helper; new `Resend`.
- `apps/server/domain/invites/entity.go` / `handler.go` / `module.go` — delivery fields on `SentInvite`; `Resend` handler + route.
- `apps/server/pkg/sdk/invitations/client.go` — `deliveryStatus` on `SentInvite`; `Resend` method.
- `apps/web-ui/gateway/` — `SentInviteDto` delivery field, `ResendInvite` client method, `uiResendInvite` handler + route, delivery badge and resend control in `org_members_ui.templ`.
- No database migration: `kb.email_jobs.delivery_status` / `delivery_status_at` and the `idx_email_jobs_source` index already exist.
