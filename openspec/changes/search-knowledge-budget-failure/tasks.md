## 1. Implementation

- [ ] 1.1 In `executeQueryKnowledge`, replace the `client.Do` deadline branch: on `queryCtx.Err() != nil`, return an explicit tool error naming the configured budget derived from `queryKnowledgeTimeout`, or a propagated cancellation error.
- [ ] 1.2 Replace the SSE-loop budget-exhaustion handling (`truncated = true` + success) with the same failure helper; keep the post-loop `queryCtx.Err()` check on the same helper.
- [ ] 1.3 Remove the stale `"query timed out after 60s"` literal.
- [ ] 1.4 Keep the successful path returning `{answer, truncated}` unchanged (`truncated: false`), so genuine partial-success semantics are untouched.

## 2. Tests

- [ ] 2.1 Add a fail-first test: an expiring parent deadline yields `ok: false` and an `error` containing the budget derived from `queryKnowledgeTimeout` (RED before the fix).
- [ ] 2.2 Add a test: a cancelled context returns an error satisfying `errors.Is(err, context.Canceled)` and is not reported as a timeout (RED before the fix).
- [ ] 2.3 Keep/extend the normal-success test proving `answer` and `truncated: false` are unchanged.

## 3. Verification

- [ ] 3.1 `go build ./...` from `apps/server`.
- [ ] 3.2 `task lint`.
- [ ] 3.3 Fail-first tests observable RED then GREEN.
- [ ] 3.4 `gofmt -l` clean on changed Go files.
- [ ] 3.5 `openspec validate --all --strict` passes.
