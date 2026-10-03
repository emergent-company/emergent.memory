## Why

The `invite-email-tracking-and-resend` change surfaces each invitation's **latest**
delivery state as a single badge, but operators cannot see the history: when the
invitation was first sent, every resend, and the individual Mailgun delivery
events (delivered, opened, bounced, complained) with their bounce/complaint
reason. The data already exists — one `kb.email_jobs` row per send
(`source_type='invite'`, `source_id=<invite.id>`) and `kb.email_logs` Mailgun
events reconciled onto those jobs — but `ListByProject` resolves only the most
recent job's delivery status and the UI has no read path to the rest.

Result: an admin looking at a "Sent" invitation cannot tell how many emails were
sent, when, or why a bounce happened. Issue #1390 asks for exactly this: a log
of the invitations, shown on hover of the sent-invitation row.

## What Changes

- `ListByProject` returns a per-invitation `deliveryLog`: every invite-scoped
  email job (initial send + resends), newest first, each with its processing
  status, latest delivery status, failure error, and the Mailgun delivery events
  recorded against it (type, timestamp, and the reason Mailgun supplied).
- The project members list renders that log in the existing popover component on
  a hover/focus trigger on the invitation row (issue #1390) — no hand-rolled
  tooltip, no change to the existing lifecycle/delivery badges or row actions.
- The log is loaded in batched queries for the whole project, not per invitation.

## Capabilities

### Modified Capabilities

- `project-invitations`: the project invitations list carries a per-invitation
  email delivery log in addition to the latest delivery state.
- `web-invite-delivery`: the sent-invitations list exposes the delivery log on
  hover/focus.

## Impact

- `apps/server/domain/invites/entity.go` — `InviteDeliveryEvent` /
  `InviteDeliveryLogEvent`, `SentInvite.deliveryLog`.
- `apps/server/domain/invites/service.go` — `ListByProject` attaches the batched
  delivery log; `inviteDeliveryLogs` helper.
- `apps/server/pkg/sdk/invitations/client.go` — mirror the log on the SDK
  `SentInvite`.
- `apps/web-ui/gateway/memory_orgs.go` — delivery-log DTOs on `SentInviteDto`.
- `apps/web-ui/gateway/org_members_ui.templ` / `org_members_ui.go` — popover log
  in `memberInviteRow` plus presentation helpers.
- No database migration: `kb.email_jobs` / `kb.email_logs` and their indexes
  already exist.
