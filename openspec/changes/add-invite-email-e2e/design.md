## Context

Server email is Mailgun-only today (`apps/server/domain/email/mailgun.go`), selected in `NewSender` when Mailgun is configured; otherwise a no-op sender is used. The worker (`worker.go`) renders a template and calls `Sender.Send`. The e2e compose stack (`e2e/docker-compose.yml`) has no mail service, and the sender is chosen once at startup from `EMAIL_ENABLED` + Mailgun config.

## Goals / Non-Goals

**Goals:**
- Deterministic, offline e2e verification of invite email delivery + accept-link correctness.
- Minimal, stdlib-only SMTP sender (no new Go dependencies).

**Non-Goals:**
- Replacing Mailgun in production.
- Accept-flow e2e coverage (requires a second identity) — a separate concern.
- Mailgun real-delivery smoke test (nightly) — future work.

## Decisions

- Use **Mailpit** (active, free, self-hosted) rather than MailHog (unmaintained) or Mailosaur (paid). It is an SMTP sink, which requires an SMTP transport seam — Mailpit does not speak the Mailgun HTTP API.
- Transport selection via `EMAIL_TRANSPORT` env (`mailgun` default preserves existing behavior; `smtp` opts into the new sender).
- The e2e test asserts the accept link by matching the token returned from `POST /api/invites` against the `…/invites/accept?token=…` URL in the email HTML — robust to the app host/`AppURL`.
- The test polls Mailpit's HTTP API (port 8025) with a deadline to absorb SMTP + worker-poll latency; the worker interval is lowered in the compose stack to shorten the round-trip.

## Risks / Trade-offs

- The SMTP sender is exercised end-to-end only in e2e; unit tests cover the MIME builder and sender selection.
- The Mailpit image ships no shell, so it has no in-container healthcheck; the server uses a `service_started` dependency and the test's polling absorbs any startup race.
