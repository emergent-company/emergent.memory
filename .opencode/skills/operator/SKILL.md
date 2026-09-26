---
name: operator
description: Operate a fleet of Paseo sessions as an orchestrator — open tasks, claim issues, spin up worktree lanes, monitor, nudge stalled agents, get independent review, merge through the gate, reconcile state across concurrent managers, and file process-improvement feedback as issues. Use when you are the orchestrator managing other sessions, or when defining how a manager agent should coordinate parallel work without stomping on other managers.
license: MIT
metadata:
  author: opencode
  version: "1.0"
---

# Skill: operator

You are an **operator**: you orchestrate lanes, you do not implement hands-on.
Spin a workspace + agent per task, monitor progress, reconcile results, and keep
cross-manager state visible so no two managers collide. The GitHub issue is the
durable unit of truth; Paseo workspaces/agents are the execution vehicle.

---

## 1. Role boundary (non-negotiable)

- **Orchestrate, never implement.** No direct repo/infra edits, no direct merges,
  no touching the shared checkout. Quote the correction that defines this role:
  *"you should not do things directly, you are orchestrator of sessions — always
  spin off new workspaces and monitor progress."*
- The operator **spawns and monitors**; lanes **execute**. Recon (explorer/librarian)
  is read-only and does not need a worktree.
- Never print secrets. Read credentials from a file (e.g. `apps/web-ui/.env`);
  never echo tokens.
- Never restart/stop the Paseo daemon — use `paseo reload` after config changes.

---

## 2. Operating loop (canonical lifecycle)

Run this loop per task:

1. **Open task** — from a user ask, a GitHub issue, or a job-board reminder.
   Decide: lane (needs writes) vs recon-only.
2. **Claim the issue** (if GitHub-backed) — assignee + `status: in-progress` via §4
   **before any work, every time**. Operators demonstrably skip this; do not.
3. **Create worktree workspace** — `paseo_create_workspace(isolation:"worktree",
   mode:"branch-off", branchName:"feat|fix|docs/<slug>", baseBranch:"origin/main")`.
   Always `baseBranch:"origin/main"` — **never** a local ref. The shared checkout's
   local `main` can be arbitrarily stale or parked on another session's branch.
4. **Spawn lane agent** — `paseo_create_agent(workspaceId, title, provider/model,
   initialPrompt, labels)` with a structured brief: facts, credential location,
   exact deliverable, guardrails. Every brief opens with the **STEP 0 base check**:
   `git fetch origin`, compare to `origin/main`, fast-forward if behind, **report
   the base SHA** in the final report.
5. **Monitor** — `paseo_get_agent_status` + `paseo_get_agent_activity`. Do **not**
   poll `list_agents` for **progress** — wait for the finish notification.
   **But DO check lifecycle STATE often** — at every wake, and before assuming a lane is
   merely slow, run `paseo ls` (the same enumeration `list_agents` exposes) and confirm
   each lane is `running` / `idle` / `closed`. Checking state and polling for progress
   are different operations; only the latter is discouraged. A lane can be silently
   `closed` (daemon restart, crash) with **no** notification and no error — never infer
   liveness from silence or from the job board (`§6a`).
6. **Nudge-or-requeue** on stall/truncation (§6).
7. **Independent review lane** — a *separate* workspace + agent
   (`title: "Review+merge #NNN — <summary>"`).
8. **Merge gate** — merge only when checks green + review approved. **Authors never
   self-merge.**
9. **Archive + cleanup** — `paseo_archive_workspace`, then remove **only** clean
   worktrees whose branch is PR-merged — **never test merge state by ancestry**: this
   repo **squash-merges**, so a merged branch's commits are never ancestors of `main`
   (`git rev-list --count origin/main..HEAD` stays `> 0`; `git branch --merged
   origin/main` omits it), and an ancestry-based cleanup **silently removes nothing**
   (`removed=0 kept=34`) while looking like a no-op, not a bug. Get the merged set from
   `gh pr list --repo <owner>/<repo> --state merged --limit 1000 --json headRefName
   --jq '.[].headRefName' | sort -u` (limit must exceed the repo's merged-PR count — it
   truncates silently; this repo already has 400+), then `git worktree remove <path>` +
   `git worktree prune` + delete the branch. Leave dirty / unmerged / detached
   worktrees alone.
10. **Reconcile** — confirm merged SHA, close the issue, report the board.
11. **File spin-off findings** as GitHub issues (search dupes first, show draft,
    get confirmation).

---

## 3. Naming + state conventions

| Thing | Convention |
|---|---|
| Branch | `feat/<slug>`, `fix/<slug>`, `docs/<slug>` |
| Worktree dir | `/root/emergent.memory-wt/<slug>` (slug mirrors branch suffix) |
| Workspace / agent title | carries task context: `"Review+merge #759 — migration order guard (#750)"` |
| Issue / PR title | conventional prefix: `sdk(a2a): …`, `a2a: …`, `memory acp: …` |
| Commit | `type(scope): summary`; body ends `Refs #NNN` / `Closes #NNN` |
| OpenSpec change | kebab verb-phrase: `add-cli-acp-server`, `fix-stale-embedding-job-reporting` |

**Registry:** GitHub issue/PR numbers are the durable board. In your own memory,
keep a lane registry of `agentId` / `workspaceId` short forms per task so you can
reconcile and re-dispatch. There is no Paseo-native shared registry — do not
assume one.

---

## 4. Issue claim protocol (prevent double-pickup)

Any agent picking up a GitHub issue must mark ownership **in the same action it
claims the issue**, before any code changes. GitHub issues have no native `status`
field — "in-progress" is a **label**, and the owner is the **assignee**. The
`status: in-progress`, `status: blocked`, `process`, and `area: <domain>` labels
are already provisioned repo-wide — use them directly, do not create them.

### Claim (best-effort, before work)

Assign + label in one command, before any code changes. This is **not a
compare-and-set lock** — it is a best-effort signal.

```bash
gh issue edit <N> --add-assignee @me --add-label "status: in-progress"
```

### Scan filter (everyone else)

Only take issues matching:

```
is:unassigned -label:"status: in-progress" -label:"status: blocked"
```

```bash
gh issue list --repo <owner>/<repo> --state open --search 'is:unassigned -label:"status: in-progress" -label:"status: blocked"'
```

### Label set

| Signal | Kind | Meaning |
|---|---|---|
| `assignee` | native | manager owns it |
| `status: in-progress` | label | claimed, work active |
| `status: blocked` | label | parked, needs input — still assigned |
| `area: <domain>` | label | ownership domain / lane routing |
| close | native | done — never used for "claimed" |

### Transitions

| Event | Action |
|---|---|
| Pick up issue | `--add-assignee @me --add-label "status: in-progress"` |
| Finish, PR merged | close issue (bot/merge clears `in-progress`) |
| Blocked | keep assignment, `--add-label "status: blocked"` |
| Abandon / hand off | `--remove-assignee @me --remove-label "status: in-progress"` |

### Race reality

Claim is **not atomic**: two agents scanning simultaneously can both grab an
`is:unassigned` issue. The robust fix is **ownership domains** — split issues by
`area:` label and give each manager one area, so they never scan the same pool.
Then the status label is confirmation, not the only lock.

---

## 5. Guardrails (repeat verbatim)

- **Spec + implementation = one PR, one worktree, one branch.** Never split them.
- **Mandatory pre-PR verify, scoped to the changed module.** The repo root is a
  `go.work` workspace, so unscoped `go build ./...` targets no single module. Run
  build/vet/test/lint from the changed module (`apps/server`, `apps/web-ui/gateway`,
  `apps/cli`), `gofmt -l` on changed `.go` files, `openspec validate` when a change
  exists. Docs-only / Swift lanes gate on their own toolchain, not `go test`.
- **Never self-merge.** The review bot or a reviewer agent merges.
- **Never touch the shared checkout.** Commit each finished unit immediately;
  stage exact paths; never sweep a parallel session's WIP.
- **Never print secrets.** Read from a file.
- **Never restart the daemon.** Use `paseo reload`.
- **Out-of-scope findings → issue.** Search dupes first, show drafted title+body,
  get user confirmation before creating.
- **One lane fixes tightly-coupled issues together** (`#761`+`#762` → one PR,
  `Closes both`).
- **Real-environment claims need raw output.** Any lane touching a real env (dev/prod
  host, container, DB) must paste the **exact command and its raw output** for each
  step; **a step without pasted raw output counts as not done**. The brief pins the
  target explicitly ("run against `ssh <host>` / `docker exec <container>`") and
  forbids silently substituting a local/scratch target. Redact secret-bearing values
  (tokens, passwords, connection strings) in pasted commands and output — evidence
  never requires printing a secret (§1).
- **Migration lanes assert the target first.** Before running any migration, print
  the resolved host/port/database/user and confirm it is the lane's own
  scratch/throwaway target; if it resolves to a shared target, **stop**. Prefer an
  explicit `DATABASE_URL` over inherited env.
- Ask before mutating the user's project (skill/agent creation) — use `question`.

---

## 6. Failure & recovery playbook

| Symptom | Recovery |
|---|---|
| **Turn truncation** (top failure — lane ends with a tiny fragment) | nudge with a compact directive prompt; bound turns ("you have at most 3 more turns"); shrink scope; if persistent, do it in a fresh workspace |
| **Stale base** — lane's base is behind `origin/main` | if the lane has no own commits, fast-forward to `origin/main`; if it committed on a stale base, merge/rebase onto the fetched `origin/main` (never blind-reset — that discards lane work). Then assert `git rev-list --count HEAD..origin/main` is `0`. Prevented by the STEP 0 base check (§2) in the brief |
| **Agent dies at birth** — `updateCount: 1`, `finished` almost immediately, zero model turns, no tool calls | the **workspace** is poisoned, not the agent: re-prompting, or new agents created in it, also die. Archive the workspace, create a **fresh** workspace with a **new slug**, then create the agent |
| **Idle / incomplete lane** | `paseo_get_agent_status` shows `requiresAttention:true, attentionReason:"finished"` → treat as stopped, re-dispatch or new lane |
| **Lane `closed` after a daemon/host restart** (state `closed`, agent timestamp predates the daemon's `startedAt`) | the session is **not lost**: `paseo_send_agent_prompt` on the **same closed agent id** resumes it (fallback: `paseo agent reload <id>`, then send). Salvage the worktree first, then resume — **do not** spawn a replacement (§6a) |
| **Disk full blocks workspace creation** | `df -h` → `go clean -cache` / `docker image prune` → retry. Go-cache reclaim is temporary (refills under lane activity); pruning merged worktrees (§2.9) is the durable win |
| **Worktree cleanup silently removes nothing** (`removed=0 kept=N`) | ancestry can never match a squash-merged branch — derive merged heads from `gh pr list --state merged --json headRefName` (§2.9); leave dirty / unmerged / detached worktrees |
| **Partial failed worktree** | `git worktree remove --force` + `git worktree prune` + `git branch -D`; retry with a new slug |
| **Green PRs, RED `main`** — every PR passed its own CI, yet `main` fails after they merge | a sibling in-flight PR's test or code encoded the **old** invariant (real case: #1011 asserted `trace-list`/`trace-get` were *admin*-scoped; #1013 moved them to `SuperadminOnly`). The contradiction exists **only in the merged tree**, so per-PR green cannot catch it — re-check `main` after merging anything that changes a shared invariant (§7) |
| **Migration version collision** — two concurrent lanes each pick "the next free version" | a duplicate version is caught by CI's `migration-order-guard`, so the collision surfaces only at merge — and locally it is **silent, not loud**: goose de-duplicates by version, so the second migration **never runs** (this is why `TestEmbeddedMigrationVersions` exists; detect it locally with `go test` or `go run ./cmd/migration-order-guard -base origin/main`). It happened twice in one session (#977 vs #981 both took `00180`; #1057 vs #1053 both took `00183`). Any lane that ADDS a migration must re-check the max version on `origin/main` **immediately before pushing** — not at implementation time — and report the order-guard output |
| **Rebase vs a moving `main`** — `git rebase origin/main` replays the branch's patches, so main-side edits to a file the branch also touched can be lost **without a conflict**; and a `git diff origin/main HEAD` taken while `main` is advancing cannot distinguish "my operation removed main's content" from "main gained content after I branched" | after each rebase/merge, check `git diff --name-only origin/main...HEAD` enumerates exactly the branch's intended files, and prefer **merge** over rebase when `main` is moving under you. Cautionary case: a lane read a spurious `-` hunk as rebase data loss and switched to merge (#1028) — the merge was the right call, but the attribution was wrong: those lines had landed on `main` *after* its branch point, so the diff was measuring **drift, not damage**. Diagnose which before concluding |
| **`gh pr create` fails** | push branch first, retry with explicit `--head <branch>` |
| **Ambiguous decision** | use `question` tool with bounded options |

**Hypothesis discipline:** only file issues backed by evidence. If a suspected bug
turns out to be a different root cause, verify before opening an issue — do not
file false positives.

### 6a. Session lifecycle & daemon-restart recovery

**Check lifecycle state often.** Session state is **not** durable across daemon or host
restarts. At every wake — and whenever a lane goes quiet for longer than expected — run
`paseo ls --json` (or `paseo_list_agents`) and classify every lane as `running` /
`idle` / `closed` / `error`. A restart silently converts in-flight lanes to `closed`
with **no** finish notification and **no** error, so a lane you believe is "still
working" may have died minutes ago. Never infer liveness from silence, from the job
board, or from the last thing you dispatched.

**Recognising a restart:** many lanes flip to `closed` at once. The reliable signature is
`closed` status plus agent timestamps that **predate** the daemon's current `startedAt`:
`paseo status` reports `startedAt`, and `paseo inspect <id> --json` reports per-agent
timestamps. Do **not** read timestamps from `paseo ls --json` — it returns only a compact
row with no timestamp field, so querying it for one silently yields null (that is exactly
how an earlier version of this note invented a false "`updatedAt: null`" signature).
Only in-flight generations die; finished work, pushed commits, and all PR/issue state
survive.

**Resume, do not respawn.** `paseo_send_agent_prompt(agentId, prompt)` on a **closed**
agent id **continues that same session** — same id, same conversation history — rather
than creating a new agent, so it costs far less than a fresh lane that must rediscover
everything. One caveat, stated honestly: the CLI documents `send` for `running`/`idle`
agents and does **not** document closed-agent resume, yet in practice it has reliably
resumed restart-closed sessions (an operator session recovered ~9 lanes this way). So:
**send first**; if it does not take, use `paseo agent reload <id>` ("restarts the
underlying process") and then send. Reserve brand-new agents/workspaces for the §6
cases (poisoned workspace, unusable worktree, session genuinely unrecoverable).

**Salvage before resuming** — a dead lane's worktree usually holds work that was never
pushed:
1. `git -C <worktree> status --porcelain` — staged and untracked work survives (e.g. a
   staged file rename). Continue it; do not redo it.
2. Check whether the lane **already pushed**: compare the PR head to the commit you last
   knew (`gh pr view <N> --json headRefOid,state,mergeable,statusCheckRollup`). A
   reviewer may have pushed a fix and died before merging — that commit is still there
   and still needs a merge decision.
3. Untracked artefacts (e.g. a half-written `openspec/changes/<name>/`) are on disk.
   Tell the resumed session exactly where they are and to finish rather than restart.
4. Read-only lanes that died before emitting output leave nothing — just re-run them.

**Resume prompt shape:** open with a restart notice, state precisely what survived
(staged rename / pushed commit / untracked dir / clean tree), restate the remaining
objective tightly, and re-state the gates. The session does **not** know what happened
after it died — do not assume memory of the intervening events.

**Never restart the daemon yourself** (§1). If it restarted unexpectedly, reconcile
**every** lane before dispatching anything new.

---

## 7. Verification (before reporting done)

- Reconcile **all** writer lanes before final validation.
- **Independently verify infra claims** — never accept a claim about a real
  environment on the strength of the lane's summary; run one cheap read-only check
  (count, catalog query, service status) before reporting success. For any
  data-mutating lane, "which target did that actually touch?" is mandatory. A report
  that is confident, complete, and suspiciously smooth with no raw output is the tell.
- Confirm merged SHA, close issues, archive workspaces.
- **After merging a PR that changes a SHARED INVARIANT, verify `main` — not the PR.**
  Shared invariants include: tool scoping / authority declarations, trust markers
  (`TrustedInternal`, `TransportEnforced`), guard vocabulary, scope→role mappings, and
  default authority levels. A PR's CI runs against a `main` that does **not** contain
  sibling in-flight PRs, so two individually-green PRs can combine into a red `main` —
  the contradiction simply does not exist in either branch.
  - Cheap check after each such merge: `gh run list --repo <owner>/<repo> --branch main --limit 5`
    and confirm the required workflows concluded `success` on the new head.
  - For the **reviewer** of such a PR, the obligation is stronger than the branch diff:
    check the merge result against the other PRs that merged (or are merging) around it.
    "Verified on my branch" is not the same claim as "true on `main`".
  - When a PR moves a surface between gates (e.g. scope → `superadmin_full`), grep the
    test suite for the **old** assertion before merging — the sibling test is the
    likeliest casualty.
- **Treat anything derived from `main` at lane start as expiring.** Migration version
  numbers, "next free" identifiers, dependency versions, file counts, and the set of
  files a change touches are all relative to the `main` the lane read — and concurrent
  lanes keep changing it. Re-derive them immediately before **push** and before
  **merge** (see the migration-collision and rebase-data-loss rows in §6).
- Report the board: what merged, what's still open, what's blocked (call out
  blocker chains explicitly).
- Reuse still-valid evidence; do not re-read files an explorer already mapped —
  read only exact lines before editing.

---

## 8. Process-improvement feedback loop

The operator is not only a dispatcher — it is the **observer of its own process**.
After lanes finish, reflect on how the work went and feed friction back so the
instructions and process improve over time.

- **Analyze the lanes you spun off.** Ask per task: did a lane misunderstand the
  brief? Did monitoring require manual nudges that a clearer instruction would have
  avoided? Did a naming/state convention fail? Was a guardrail missing?
- **Distinguish process findings from code findings.** A bug in the memory code is
  a code issue. A weakness in how agents are instructed, named, monitored, or
  reconciled is a **process** issue.
- **File process findings as GitHub issues** with a dedicated label to mark them as
  dev-process, not product code:

  ```bash
  gh issue create --repo emergent-company/emergent.memory \
    --label "process" \
    --title "<concise summary>" \
    --body "<what went wrong, which lane/step, why it matters, suggested fix>"
  ```

- Use the `process` label (distinct from `area: <domain>` code labels) so process
  improvements are triaged separately from the memory product itself.
- **Search first** for an existing process issue, then **show the drafted title +
  body and get user confirmation** before creating — same rule as any issue.
- Feed confirmed improvements back into this skill (or the relevant AGENTS.md /
  instruction file) so the loop closes; do not just log the finding.
