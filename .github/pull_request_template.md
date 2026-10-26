## Summary

Describe the change. What feature/bug does this address?

## Type of Change

- [ ] Feature
- [ ] Bug Fix
- [ ] Refactor / Cleanup
- [ ] Docs / Benchmark / Tooling
- [ ] Other (specify)

## Graph Checklist

If you touched graph logic or tests:

- [ ] Updated relevant test helpers in `apps/server/tests/`
- [ ] Verified head selection semantics (DISTINCT ON then outer tombstone filter) preserved
- [ ] Multiplicity logic unchanged OR new tests added

## Tests

- [ ] Unit tests added / updated
- [ ] All affected test suites passing locally (`go test ./<pkg>/`; `task -d apps/web-ui e2e:test` for UI changes)
- [ ] (Optional) Ran the live-LLM e2e suite (`npx playwright test --config=playwright.config.ts --project=chat`) — not PR-gated; it runs on a schedule and on manual dispatch

## Docs

- [ ] Added / updated documentation where appropriate

## Risk & Rollback Plan

Briefly describe potential risk and how to rollback if needed.

## Additional Notes

Anything else reviewers should know.
