## Context

The Mailgun sender is a thin wrapper over `mailgun-go/v4`. `client.SetAPIBase`
replaces the *entire* base URL, and the SDK default already includes the `/v3`
path segment (`https://api.mailgun.net/v3`); the send request is then
`POST {base}/{domain}/messages`. To point the SDK at a local stub and still hit
`/v3/{domain}/messages`, the override value must itself include `/v3`.

## Goals / Non-Goals

**Goals:**
- Deterministic, offline e2e proof that the Mailgun transport builds and sends
  the invite email with the correct accept link.
- No real Mailgun credentials or network egress in CI.
- Default stack unchanged: no-override `docker compose config` behaves as today.

**Non-Goals:**
- Replacing the SMTP/Mailpit e2e test (it stays the default).
- A real Mailgun delivery smoke test (still future work).

## Decisions

- **Precedence**: `MAILGUN_API_BASE` wins over the `MAILGUN_REGION=eu` default.
  An explicit base is an unambiguous operator intent and the only way to target
  a non-production endpoint. When unset, behaviour is byte-for-byte unchanged.
- **Override includes `/v3`**: documented in the compose var and CI job because
  the SDK appends only `/{domain}/messages`.
- **Stub accepts both `/v3/{domain}/messages` and `/{domain}/messages`** so it
  tolerates either base form, while the e2e test asserts the canonical
  `/v3/{domain}/messages` path used by the Mailgun CI configuration.
- **Skip vs. fail**: the new test skips unless `MAILGUN_STUB_URL` is set, so the
  default SMTP/Mailpit stack (where the stub is reachable but unused) stays green.

## Risks / Trade-offs

- The stub captures Authorization *presence* only, never the key value, so no
  credential-looking material is logged or persisted.
- Publishing stub port 8080 is required for host-run API tests; it is otherwise
  idle in the default stack.
