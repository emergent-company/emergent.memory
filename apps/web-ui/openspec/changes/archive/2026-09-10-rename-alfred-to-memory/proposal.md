## Why

Alfred is becoming the Memory web console. The name "alfred" should become "memory" so the product, binary, and deployment all match the domain it serves.

## What Changes

- Rename the binary and Go module (repo `mkucharz/alfred` → `emergent-company/memory.web-ui`, module `github.com/emergent-company/memory.web-ui`).
- Rename `ALFRED_*` env vars and LiveKit default agent/room (`alfred`).
- Rename Docker image, systemd units, and traefik labels.
- Update `docs/spec/` and `openspec/` references.
- **BREAKING**: existing `ALFRED_*` env vars and `alfred` binary names change.

## Capabilities

### New Capabilities
<!-- none — pure rename, no behavior change -->

### Modified Capabilities
<!-- none -->

## Impact

- Broad, mechanical: module path, binary, env var names, Docker/compose, deploy scripts, systemd, traefik labels, docs, and (optionally) the iOS app name.
- No behavior change; sets `skip_specs: true` (rename, not a feature).
