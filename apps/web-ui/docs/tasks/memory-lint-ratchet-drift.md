# Pay down the emergent.memory lint-ratchet drift (origin/main is red)

**Status:** done
**Created:** 2026-09-10
**Resolved:** 2026-09-10 (origin/main `4f6b4a4c` — all ratchets green again)
**Source:** —

## Resolution

Transient: the drift was resolved on `origin/main` by a later merge. Verified at
`/root/emergent.memory-wt/...` base `4f6b4a4c`: `bash scripts/lint-ratchet.sh` reports all
"ratchet ok" (auth 13/13, setters 3/3, apperror 1199/1199, response types 6/6). No paydown
needed. Keep the no-raise rule; re-open only if drift reappears.

## What

`origin/main` in **/root/emergent.memory** fails `apps/server/scripts/lint-ratchet.sh`, so every
memory PR's `Lint` job fails even when `golangci-lint` reports `0 issues`. Observed counts vs the
script's baselines:

```
RATCHET FAIL: auth guards grew from 13 to 14
RATCHET FAIL: cross-domain setters grew from 3 to 4
RATCHET FAIL: apperror Style A grew from 1199 to 1204
ratchet ok: response type dupes = 6 (baseline 6)
```

The drift came from recently merged PRs (project grace-period deletion and others) that grew the
counts without paying them down. The script's rule is explicit: **never raise a baseline**.

## Why

The red ratchet forces `--admin` merges on otherwise-green PRs (bypassing the CI gate) and masks
genuine new debt. It should return to a green baseline.

## How

Pay down to ≤ baseline, without changing behavior — share package-level helpers so repeated
inline chains collapse, as the orgs package already does (`notFoundOrg` / `dbErr` /
`errInvalidBody`):

- **auth guards** 14→13: fold one inline `if user == nil` guard into the codebase-standard helper
  for the same domain.
- **cross-domain setters** 4→3: remove one `func (s *Service) SetXxx` cross-domain wiring in favour
  of constructor/field injection already used elsewhere.
- **apperror Style A** 1204→1199: collapse five `.WithMessage`/`.WithInternal` call sites into
  shared constructors/helpers (count once per helper, not per call).

Do NOT edit `scripts/lint-ratchet.sh` or its baseline constants.

## Verify

```
cd /root/emergent.memory/apps/server
bash scripts/lint-ratchet.sh     # all "ratchet ok"
go build ./... && go vet ./...   # unchanged
```

## Notes

- File: `apps/server/scripts/lint-ratchet.sh` (baselines `BASELINE_AUTH_GUARDS`,
  `BASELINE_SETTERS`, `BASELINE_APPERROR_STYLEA`, `BASELINE_RESPONSE_TYPES`).
- Blocks the `Lint` check on PR #422 (merged with `--admin`); fix independently.
