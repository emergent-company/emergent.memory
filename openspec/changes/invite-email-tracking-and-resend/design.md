## Context

- `kb.email_jobs` already carries `source_type`/`source_id` (invite emails set them) and `delivery_status`/`delivery_status_at`/`delivery_status_synced_at`. `idx_email_jobs_source` supports lookup by source. No schema change is needed.
- The Mailgun webhook (`POST /api/webhooks/mailgun`) is the only ingest path; a `MailgunSender.GetEventsForMessage` poller exists but has no callers.
- Invites are created by `invites.Service.Create`, which enqueues a `project-invitation` job (non-fatal on failure) and stores a 7-day expiry. `Create` refuses a duplicate pending invite with HTTP 400.
- The members UI already renders one row per pending invite and has a Revoke action (org-admin gated).

## Goals / Non-Goals

**Goals**
- Make per-invite email delivery state visible to org admins (delivered / opened / clicked / bounced / complained).
- Give admins a resend action that reuses the existing invite rather than forcing revoke-and-re-invite.
- Make open/click events actually fire (enable tracking at send) and be reflected (map them).

**Non-Goals**
- A delivery-history timeline (only the latest state is shown).
- Reviving the Logs-API poller — webhook ingest remains the single path.
- Changing the accept flow or invite authorization model.
- Surfacing delivery state for MCP invite emails (those jobs set no source and are out of scope).

## Decisions

- **Resend semantics: keep the token, extend expiry.** Resend re-enqueues the email for the same invite row with the same token and resets `expires_at` to now+7 days. Rotating the token would break a link already delivered and in an inbox. Only `status='pending'` invites can be resent (accepted/declined/revoked return HTTP 404, matching revoke's guard); this also recovers invites whose expiry has lapsed while still `pending`.
- **Resend authz mirrors revoke.** Requiring `org_admin` (or active `superadmin_full`) over the invitation's organization keeps resend an org-tier write; reusing the existing check avoids a second authority model.
- **Delivery state is the invite's most recent email job.** `ListByProject` resolves it in the same query (a lateral/most-recent join on `source_type='invite' AND source_id=<invite.id>`), so there is no N+1 and no cross-invite ambiguity.
- **Tracking is enabled per message, not (only) per domain.** `SetTrackingOptions` on the outbound message makes the behaviour explicit in code and independent of domain configuration; per-message `o:tracking*` overrides domain-level settings.
- **`opened` is not a reliable signal; `clicked` is better.** Mailgun (and recipients' privacy proxies such as Apple Mail Privacy Protection) prefetch tracking pixels, inflating opens. The UI must present delivery/click as the meaningful signals and not imply that "not opened" means "not seen". Copy must not overstate precision.

## Risks / Trade-offs

- **Click tracking rewrites links** through a Mailgun redirect domain. The accept URL still resolves, but the URL in the captured email differs; the existing `add-invite-email-e2e` assertion matches the token, not the host, so it remains valid.
- **Domain-level CNAME/tracking must be configured** for `opened`/`clicked` to fire in a real environment; enabling the send option alone is not sufficient operationally. This is deployment configuration, not code.
- **Bounces/complaints are the strong signals** and already worked for non-invite mail; invites simply were not surfaced.
- Extending expiry on resend changes invite lifetime semantics slightly (repeated resends can keep an invite alive indefinitely). Accepted as the intended "keep trying to reach them" behaviour; revoke remains the way to kill an invite.
