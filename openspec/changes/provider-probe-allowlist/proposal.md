## Why

The web-UI gateway's provider connection test (`POST /settings/providers/test`) and
inline base-url check (`POST /settings/providers/check-url`) dial the submitted URL
through an SSRF-guarded transport that rejects every non-public address
(`apps/web-ui/gateway/netguard.go`). On a self-hosted deployment whose LLM gateway is
an internal address (e.g. `http://litellm:4000/v1`, resolving to `100.113.48.6` in the
CGNAT range `100.64.0.0/10`), the test fails with
`refusing to connect to non-public address`, even though saving the provider and using
it through the memory backend — which has no such guard — works. The test therefore
contradicts reality and blocks a legitimate configuration.

## What Changes

- Add `PROVIDER_PROBE_ALLOW`: a comma-separated allowlist of hostnames and/or CIDRs the
  provider probe may dial even when they resolve to non-public addresses.
- Thread it into the probe's SSRF guard (`newSSRFSafeHTTPClient`).
- Default (unset/empty) behavior is unchanged — public-only.
- Document it in `apps/web-ui/.env.example`; the dev deployment sets it to the internal
  gateway host.
- Update the `provider-settings` capability spec.

## Capabilities

### Modified Capabilities

- `provider-settings`: connection-test egress honors an operator allowlist; default
  remains public-only.

## Impact

- `apps/web-ui/gateway/netguard.go`, `config.go`, `main.go`, `settings_providers.go`
  (+ `netguard_test.go`).
- No database, schema, or API changes. No behavior change when `PROVIDER_PROBE_ALLOW`
  is unset.
