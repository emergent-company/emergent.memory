## 1. Implementation

- [x] 1.1 In `executeQueryKnowledge`, replace the `client.Do` deadline branch: on `queryCtx.Err() != nil`, return an explicit tool error naming the configured budget derived from `queryKnowledgeTimeout`, or a propagated cancellation error.
- [x] 1.2 Replace the SSE-loop budget-exhaustion handling (`truncated = true` + success) with the same failure helper; keep the post-loop `queryCtx.Err()` check on the same helper.
- [x] 1.3 Remove the stale `"query timed out after 60s"` literal.
- [x] 1.4 Keep the successful path returning `{answer, truncated}` unchanged (`truncated: false`), so genuine partial-success semantics are untouched.

## 2. Tests

- [x] 2.1 Add a fail-first test: an expiring/expired deadline makes the call fail with an error containing the budget derived from `queryKnowledgeTimeout` (RED before the fix).
- [x] 2.2 Add a test: a cancelled context returns an error satisfying `errors.Is(err, context.Canceled)` and is not reported as a timeout (RED before the fix).
- [x] 2.3 Keep/extend the normal-success test proving `answer` and `truncated: false` are unchanged.

## 3. Verification

- [x] 3.1 `go build ./...` from `apps/server`.
- [x] 3.2 `task lint`.
- [x] 3.3 Fail-first tests observable RED then GREEN.
- [x] 3.4 `gofmt -l` clean on changed Go files.
- [x] 3.5 `openspec validate --all --strict` passes.
