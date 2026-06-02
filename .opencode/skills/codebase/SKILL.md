---
name: codebase
description: Use the `codebase` CLI to populate, audit, and explore the Memory knowledge graph for a codebase. Use when the user wants to onboard a project, sync routes, check API quality, analyze domain structure, manage graph objects, manage constitution rules, track competitors and market trends, or install skills.
metadata:
  author: emergent
  version: "1.2"
---

Operate the `codebase` CLI — a unified tool for syncing codebase structure into the Memory knowledge graph and querying it.

## Rules

- **Project config is auto-discovered** — the CLI walks up from cwd to find `.codebase.yml` containing `project_id`. Only pass `--project-id` to override.
- **Auth is auto-discovered** — reads `~/.memory/config.yaml`, `.env.local`, or `MEMORY_API_KEY`. No manual auth needed.
- **Dry-run first for destructive ops** — `sync routes --dry-run`, `fix stale --dry-run` before committing changes.
- **`--format json`** for machine-readable output; default is `table`.
- **`--all`** on `graph list` to paginate through all results (default limit is 50).

---

## Setup

```bash
# Install the CLI (from repo root)
task codebase:install   # → /usr/local/bin/codebase

# Verify project binding
cat .codebase.yml       # should have project_id and server
codebase graph list --type Domain   # smoke test
```

`.codebase.yml` format:
```yaml
project_id: <uuid>
server: https://memory.emergent-company.ai   # optional, defaults to localhost
```

---

## When to use which command

| User says… | Run… |
|---|---|
| "set up the graph", "onboard this project", "build the graph" | `codebase onboard` |
| "what rules do we have", "show me the constitution" | `codebase constitution rules` |
| "add a rule that X", "encode this constraint" | `codebase constitution add-rule` |
| "check naming conventions", "run the rules" | `codebase constitution check --type <Type>` |
| "sync the routes", "update endpoints" | `codebase sync routes` |
| "audit the API", "check endpoint quality" | `codebase check api` |
| "show domain structure", "what does this domain do" | `codebase analyze tree --domain <name>` |
| "show the competitive landscape", "who are our competitors" | `codebase analyze competitive` |
| "drill into a competitor", "what does X offer" | `codebase analyze competitive --competitor <key>` |
| "add a competitor", "track this product" | `codebase create competitor <name>` |
| "add a feature gap", "we're missing X vs competitors" | `codebase create featuregap <name>` |
| "add a strategic initiative", "we should respond to X" | `codebase create strategicinitiative <name>` |
| "add a market trend" | `codebase create markettrend <name>` |
| "link an initiative to a scenario" | `codebase graph relate --from init-<slug> --to scn-<slug> --type drives` |
| "I want to make a batch of graph changes safely" | create a branch first via `memory graph branches create` |
| "preview changes before they hit main" | write to branch, then `memory graph branches merge main --source <id>` (dry run) |
| "merge my branch into main" | `memory graph branches merge main --source <id> --execute` |

---

## Constitution Rule Detection

The agent actively looks for opportunities to encode constraints into the constitution — both
when the **user states them** and when the **agent discovers them** during analysis.

---

### Trigger 1 — User-stated rules

Recognize these signal patterns in user messages:

| Pattern | Examples |
|---|---|
| Prohibition | "never do X", "don't use X", "X is forbidden", "we shouldn't X" |
| Mandate | "always do X", "every Y must have Z", "X is required", "we should always" |
| Naming convention | "keys should start with X", "files must be named X", "use the prefix X" |
| Structural constraint | "every domain needs X", "handlers must return X", "services should have Y" |
| Quality gate | "endpoints must have X", "make sure all Y have Z" |

---

### Trigger 2 — Agent-discovered patterns

While running `constitution check`, `analyze`, `check api`, or reading source files, the agent
looks for systemic findings that aren't yet encoded as rules:

**Fire when:**
- `constitution check` or `check api` surfaces **5+ violations of the same type** with no existing rule covering it
- A consistent pattern is observed across **3+ domains or files** during analysis
- A gap exists: an existing rule covers case A but the symmetric case B has no rule

**Don't fire when:**
- A single one-off violation (likely an exception, not a systemic issue)
- The pattern is already covered by an existing rule — check first with `codebase constitution rules --format json`
- During routine `graph get` / `graph list` / `graph tree` operations with no pattern analysis

---

### The flow — detect → suggest → add

**Step 1 — Detect.** Note the signal. Finish the current task first unless the detection is the
primary finding.

**Step 2 — Suggest.** Present the pre-formulated rule inline — don't ask a yes/no first:

> "I noticed [observation]. There's no rule covering this yet. I'd add:
>
> **`rule-<category>-<slug>`** — *"[statement]"*
> _(category: X · applies to: Y · check: auto/scan/prop)_
>
> Should I add it to the constitution?"

**Step 3 — Add.** If the user confirms, run `codebase constitution add-rule` with the
pre-formulated fields. Confirm with the key and ID returned.

If the user says no, drop it — don't ask again for the same pattern in this session.

---

### Category mapping

| Statement type | Category |
|---|---|
| Key naming, file naming, struct naming | `naming` |
| HTTP endpoints, auth, responses, Swagger annotations | `api` |
| Error handling, service patterns, fx module structure | `service` |
| DB queries, ORM patterns, schema-qualified names | `db` |
| Scenario / step / context structure | `scenario` |
| Auth, scopes, public endpoints, access control | `security` |
| Pagination, timeouts, unbounded queries, streaming | `performance` |

---

### Field derivation guide

| Field | How to derive it |
|---|---|
| `--key` | `rule-<category>-<slug>` where slug is 2-4 words kebab-cased from the statement |
| `--name` | ≤6 words, imperative: "Handlers must use apperror" |
| `--statement` | Full constraint in one sentence. Specific: name the pattern, the violation, the required alternative |
| `--category` | From the mapping table above |
| `--applies-to` | The graph type the rule checks: `APIEndpoint`, `Service`, `Domain`, `SourceFile` |
| `--auto-check` | Use when the rule is a **key naming convention** — provide a Go regex |
| `--prop-check` | Use when the rule checks a **graph object property** — JSON spec: `{"field":"method","nonempty":true}` |
| `--scan-pattern` | Use when the rule checks **source file content** — provide a ripgrep regex |
| `--scan-target` | Glob to restrict scan: `apps/server/domain/*/handler.go` |
| `--how-to-verify` | One sentence for the AI: what to look for, what's wrong, what the fix is |
| `--audit-type` | Add `security` or `performance` if the rule belongs to a targeted audit |

Pick **one** check mode (`--auto-check`, `--prop-check`, or `--scan-pattern`) — the first one set wins.
If none apply cleanly, omit all three — the rule becomes a `review` mode rule (AI verifies manually).

---

### Worked example 1 — User-stated

**User says:** "We should never return raw errors from a handler, always wrap with apperror."

**Agent detects:** prohibition + mandate on error handling → category `api`, applies to `APIEndpoint`.

**Agent suggests:**

> I'd add this to the constitution:
>
> **`rule-api-apperror-handlers`** — *"HTTP handlers must return errors using apperror.New\*, never raw echo.NewHTTPError or plain http status codes."*
> _(category: api · applies to: APIEndpoint · check: scan)_
>
> Should I add it?

**User confirms → Agent runs:**

```bash
codebase constitution add-rule \
  --key rule-api-apperror-handlers \
  --name "Handlers must use apperror for errors" \
  --statement "HTTP handlers must return errors using apperror.New*, never raw echo.NewHTTPError or plain http status codes." \
  --category api \
  --applies-to APIEndpoint \
  --scan-pattern "echo\.NewHTTPError|c\.JSON\(http\.Status(Bad|Unauthorized|Not|Internal)" \
  --scan-target "apps/server/domain/*/handler.go" \
  --how-to-verify "Search handler.go files for echo.NewHTTPError or c.JSON with 4xx/5xx codes. These must use apperror instead."
```

---

### Worked example 2 — Agent-discovered

**Agent runs** `codebase constitution check --type APIEndpoint` and finds 14 handlers returning
`echo.NewHTTPError` with no rule covering it.

**Agent suggests (inline with the check output):**

> The check found 14 handlers using `echo.NewHTTPError` directly — this pattern has no rule yet. I'd add:
>
> **`rule-api-apperror-responses`** — *"HTTP handlers must use apperror.New\*, never echo.NewHTTPError or raw c.JSON with status codes."*
> _(category: api · applies to: APIEndpoint · check: scan · audit-type: security)_
>
> Should I add it to the constitution?

**User confirms → Agent runs `add-rule` as above, then re-runs `constitution check` to confirm the new rule appears.**

---

## Command Reference

### onboard — first-time graph population (AI-oriented)

```bash
codebase onboard                  # full population sequence + constitution + skill install
codebase onboard --dry-run        # preview without writing
codebase onboard --repo /path     # specify repo root
```

Runs in sequence: sync routes → sync middleware → sync files → seed exposes → create constitution → install skill.
Prints a structured report of what was created. No interactive prompts — designed for AI agents.

### constitution — coding rules and constraints

```bash
# List rules
codebase constitution rules
codebase constitution rules --category naming
codebase constitution rules --format json

# Create constitution with starter rules (called by onboard automatically)
codebase constitution create

# Add a rule (AI infers rules from codebase analysis)
codebase constitution add-rule \
  --key rule-api-pagination \
  --name "List endpoints must support pagination" \
  --statement "Every GET endpoint returning a list must accept limit and cursor query params." \
  --category api \
  --applies-to APIEndpoint

# Add a naming rule with auto-check regex
codebase constitution add-rule \
  --key rule-naming-service-key \
  --name "Service key prefix" \
  --statement "Every Service key must start with svc-" \
  --category naming \
  --applies-to Service \
  --auto-check "^svc-[a-z][a-z0-9-]+$"

# Run checks
codebase constitution check --type APIEndpoint
codebase constitution check --type APIEndpoint --category naming
codebase constitution check --type Service --domain agents
codebase constitution check --type APIEndpoint --format json
```

Rule categories: `naming` | `api` | `service` | `db` | `scenario` | `security` | `performance`
Key convention: `rule-<category>-<slug>`

### sync — populate graph from source

```bash
codebase sync routes              # upsert APIEndpoint objects from route files
codebase sync routes --dry-run    # preview changes without writing
codebase sync middleware          # wire Middleware→APIEndpoint relationships
codebase sync files               # sync SourceFile objects with disk
codebase sync files --sync        # actually create/delete (default is read-only)
codebase sync files --orphans     # show only files in graph but not on disk
```

### check — read-only quality audits

```bash
codebase check api                          # audit APIEndpoint completeness
codebase check api --domain agents          # filter to one domain
codebase check api --checks no_path,orphan  # run specific checks only

codebase check coverage                     # test coverage gaps by domain
codebase check coverage --sort coverage     # sort by coverage %
codebase check coverage --min-coverage 80   # only show below 80%
codebase check coverage --fail-on-risk      # exit 2 if high-risk untested domains

codebase check complexity --top 10          # top 10 most complex domains
codebase check complexity --recommendations # include refactor suggestions
codebase check complexity --domain sandbox  # filter to one domain

codebase check logic                        # graph consistency checks
codebase check logic --domain agents        # filter to one domain
codebase check logic --verbose              # include passing checks
```

### analyze — structural analysis

```bash
codebase analyze tree                       # Domain→Service→Endpoint map
codebase analyze tree --domain agents       # filter to one domain
codebase analyze tree --format markdown     # markdown output

codebase analyze uml                        # PlantUML class diagram (all entities)
codebase analyze uml --domain agents        # filter to one domain
codebase analyze uml --format mermaid       # Mermaid output instead

codebase analyze scenarios                  # Scenario→Context→Action map
codebase analyze scenarios --domain agents  # filter to one domain

codebase analyze contexts                   # Context→Action map
codebase analyze contexts --type screen     # filter by context_type
codebase analyze contexts --show-empty      # include contexts with no actions

codebase analyze competitive                            # full matrix: competitors, gaps, initiatives, trends
codebase analyze competitive --competitor comp-hermes   # drill-down for one competitor
codebase analyze competitive --format json              # machine-readable output
```

### graph — CRUD on graph objects

```bash
# List
codebase graph list --type APIEndpoint --all
codebase graph list --type Domain
codebase graph list --filter domain=agents

# Get
codebase graph get ep-agents-listagents     # by key
codebase graph get <uuid>                   # by ID

# Tree (dependency view)
codebase graph tree ep-agents-listagents    # outbound relationships
codebase graph tree ep-agents-listagents --depth 3

# Create / update
codebase graph create --type Domain --key domain-foo --props '{"name":"Foo"}'
codebase graph update --key domain-foo --props '{"description":"updated"}'

# Relate / unrelate
codebase graph relate --from ep-foo --to svc-foo --type calls
codebase graph unrelate --from ep-foo --to svc-foo --type calls

# Delete / rename
codebase graph delete ep-old-key
codebase graph rename old-key new-key --dry-run

# Prune (delete isolated objects)
codebase graph prune --dry-run
codebase graph prune

# Batch (JSON array of ops)
echo '[{"op":"create","type":"Domain","key":"domain-x","props":{"name":"X"}}]' | codebase graph batch
codebase graph batch --file ops.json

# Query — natural language / hybrid search (FTS + vector)
codebase graph query "MCP proxy support"
codebase graph query "authentication scenarios" --type Scenario
codebase graph query "open source competitors" --type Competitor --limit 10
codebase graph query "login flow" --mode fts --format json
# Use this whenever a user asks to "find", "search", or "what objects relate to X"
```

### seed — write seed objects

```bash
codebase seed entities --glob "apps/server/domain/**/*.go"   # seed entities from Go files
codebase seed exposes                                         # wire Service→exposes→APIEndpoint
```

### fix — repair graph state

```bash
codebase fix stale --dry-run    # preview stale endpoint removal
codebase fix stale              # delete APIEndpoints with no matching code route

codebase fix rewire --from old-ctx-key --map "slug1=ctx-new-1,slug2=ctx-new-2"
```

### create / key — type-specific object creation

Standard codebase types: `context`, `uicomponent`, `helper`, `action`, `apiendpoint`, `sourcefile`, `domain`, `scenario`, `step`, `actor`

**Competitive landscape types:**

```bash
# Add a competitor
codebase create competitor "Hermes" \
  --category personal-agent --status active --open-source \
  --repo-url https://github.com/example/hermes \
  --tech-stack "Go, MCP" --target-audience "developers"

# Add a feature gap
codebase create featuregap "MCP Proxy" \
  --impact critical --effort medium --in-progress

# Add a strategic initiative (links back to competitive pressure)
codebase create strategicinitiative "Build MCP Support" \
  --priority high --status planned \
  --competitive-driver "Multiple competitors already ship MCP proxy" \
  --owner "platform-team"

# Add a market trend
codebase create markettrend "MCP Standardization" \
  --impact-level high \
  --impact-on-diane "Diane must support MCP to stay relevant as a personal agent platform" \
  --source "Anthropic MCP spec, Nov 2024"

# Add a comparison point (Diane vs competitor on a feature)
codebase create comparisonpoint "mcp-proxy" \
  --competitor comp-hermes --feature mcp-proxy \
  --assessment weaker \
  --reasoning "Hermes ships native MCP proxy; Diane has none" \
  --priority high

# Add competitor feature
codebase create competitorfeature "MCP Proxy" \
  --competitor comp-hermes --capability-area connectivity --core --maturity-level stable

# Add pricing model for a competitor
codebase create pricingmodel "hermes-pricing" \
  --competitor comp-hermes --model-type freemium --price-range "$0-20/mo" --currency USD

# Add an integration a competitor supports
codebase create integration "GitHub" \
  --competitor comp-hermes --type native --maturity-level stable

# Link initiative → scenario (cross-pack traceability)
codebase graph relate \
  --from init-build-mcp-support \
  --to scn-implement-mcp-proxy \
  --type drives

# Link competitor → feature gap (competitor exposes this gap)
codebase graph relate \
  --from comp-hermes \
  --to gap-mcp-proxy \
  --type exposes_gap

# Link initiative → competitor (responding to competitive pressure)
codebase graph relate \
  --from init-build-mcp-support \
  --to comp-hermes \
  --type responds_to

# Dry-run key generation (no graph write)
codebase key competitor "Hermes"              # → comp-hermes
codebase key featuregap "MCP Proxy"           # → gap-mcp-proxy
codebase key strategicinitiative "Build MCP"  # → init-build-mcp
```

**Key prefixes for competitive types:**

| Type | Prefix | Example |
|---|---|---|
| Competitor | `comp-` | `comp-hermes` |
| CompetitorFeature | `feat-<comp>-` | `feat-hermes-mcp-proxy` |
| FeatureGap | `gap-` | `gap-mcp-proxy` |
| StrategicInitiative | `init-` | `init-build-mcp-support` |
| MarketTrend | `trend-` | `trend-mcp-standardization` |
| CapabilityMatrix | `matrix-` | `matrix-q2-2026` |
| ComparisonPoint | `cmp-<comp>-` | `cmp-hermes-mcp-proxy` |
| PricingModel | `price-<comp>` | `price-hermes` |
| Integration | `intg-<comp>-` | `intg-hermes-github` |

> **Key generation is idempotent** — the CLI strips known prefixes before prepending them.
> `codebase key competitor "Hermes"` and `codebase key competitor "comp-hermes"` both produce `comp-hermes`.
> Never manually add the prefix when passing a name — pass the human name and let the CLI generate the key.

### branch — graph branch operations

Branches are isolated workspaces. Use them whenever you are making a batch of changes to the graph that should be reviewed before hitting main — e.g. a competitive landscape import, a large sync, a schema refactor, or experimental planning.

**Branches are created via the `memory` CLI, not `codebase`:**

```bash
# 1. Create a branch
BRANCH_ID=$(memory graph branches create --name "competitive/q2-2026" --output json \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
echo $BRANCH_ID   # save this — use it for all writes below

# 2. All codebase writes go to the branch via --branch
codebase create competitor "Hermes" --branch "$BRANCH_ID" --category personal-agent --status active
codebase create featuregap "MCP Proxy" --branch "$BRANCH_ID" --impact critical
codebase graph relate --from comp-hermes --to gap-mcp-proxy --type exposes_gap --branch "$BRANCH_ID"
codebase sync routes --branch "$BRANCH_ID"

# 3. Inspect what's on the branch
codebase graph list --type Competitor --branch "$BRANCH_ID" --all
codebase graph tree comp-hermes --branch "$BRANCH_ID"

# 4. Verify branch contents (codebase verify)
codebase branch verify --branch "$BRANCH_ID"

# 5. Preview merge — dry run first, always
memory graph branches merge main --source "$BRANCH_ID"

# 6. Inspect conflicts (if any)
memory graph branches merge main --source "$BRANCH_ID" --output json

# 7. Execute merge when clean
memory graph branches merge main --source "$BRANCH_ID" --execute

# 8. Delete branch after merge
memory graph branches delete "$BRANCH_ID"
```

**When to use a branch:**

| Situation | Branch? |
|---|---|
| Adding/updating a single graph object | No — write directly to main |
| Importing a full competitor + features + gaps | Yes |
| Running `codebase sync routes` on a large codebase | Yes — preview before committing |
| Experimental planning or "what if" analysis | Yes |
| Bulk `codebase graph batch` operations | Yes |

**Branch naming conventions:**

| Purpose | Name pattern |
|---|---|
| Competitive landscape import | `competitive/<period>` e.g. `competitive/q2-2026` |
| Route sync preview | `sync/routes-<date>` |
| Scenario planning | `plan/<scenario-slug>` |
| Feature exploration | `feature/<slug>` |

**Merge conflict rules:**
- Conflicts occur when an object changed on **both** the branch and main since the branch was created
- `--execute` is blocked if any conflicts exist
- Resolve by updating either the branch object or the main object to match, then re-run dry run

```bash
# List all branches
memory graph branches list

# Get branch details
memory graph branches get "$BRANCH_ID"

# Fork an existing branch (for branching off a branch)
memory graph branches fork "$BRANCH_ID" --name "sub-branch"
```

### skills — manage agent skills

```bash
codebase skills install --list          # list bundled skills
codebase skills install                 # install all bundled skills
codebase skills install codebase        # install only the codebase skill
codebase skills install --force         # overwrite existing
codebase skills install --dir /path     # custom destination
```

---

## Common Workflows

### Bootstrap a graph from scratch (scenarios-first)

This is the recommended approach for starting a fresh graph on a new or existing project.
The strategy: define the **intended behaviour** (scenarios) first, validate them, then use
`branch verify` to determine what structural components are needed, and build out from there.

#### Phase 1 — Project setup

```bash
# Bind the project (one-time)
cat .codebase.yml           # confirm project_id + server are set
codebase graph list --type Domain   # smoke test — should succeed (empty is fine)
```

If `.codebase.yml` doesn't exist:
```yaml
# .codebase.yml
project_id: <your-memory-project-id>
server: https://memory.emergent-company.ai
```

#### Phase 2 — Create scenarios on a branch

Work on a branch so nothing hits main until you're satisfied.

```bash
# Create a planning branch
BRANCH_ID=$(memory graph branches create --name "plan/initial-scenarios" --output json \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
echo "branch: $BRANCH_ID"

# Create scenarios — one per user-facing capability
codebase create scenario "User registers an account" --branch "$BRANCH_ID" \
  --given "User is on the registration page" \
  --when "User fills in email and password and submits" \
  --then "Account is created and user is redirected to dashboard"

codebase create scenario "User lists their agents" --branch "$BRANCH_ID" \
  --given "User is authenticated" \
  --when "User navigates to the agents page" \
  --then "A paginated list of the user's agents is displayed"

# Add steps to each scenario
codebase create step "Open registration page" \
  --scenario scn-user-registers-an-account --order 1 --branch "$BRANCH_ID"

codebase create step "Submit registration form" \
  --scenario scn-user-registers-an-account --order 2 --branch "$BRANCH_ID"

codebase create step "See confirmation" \
  --scenario scn-user-registers-an-account --order 3 --branch "$BRANCH_ID"
```

#### Phase 3 — Validate scenarios

Check that scenarios are structurally sound before building on top of them.

```bash
# Inspect scenario structure — look for missing steps, contexts, actions
codebase analyze scenarios --branch "$BRANCH_ID" --show-empty

# Check logical consistency
codebase check logic --branch "$BRANCH_ID" --verbose

# Red flags to fix before continuing:
#   - Scenarios with 0 steps
#   - Steps with no context (occurs_in missing)
#   - Steps with no action (has_action missing) except the final step
```

A scenario is **valid** when:
- It has ≥ 1 step
- Every non-terminal step has a context (`occurs_in`) and an action (`has_action`)
- The terminal step has a context but no action (it's the outcome, not a trigger)

Fix any issues before moving to Phase 4:
```bash
# Add a context to a step
codebase create context "Registration Page" --branch "$BRANCH_ID" --route /register
codebase graph relate \
  --from step-user-registers-an-account-1 \
  --to ctx-registration-page \
  --type occurs_in \
  --branch "$BRANCH_ID"

# Add an action to a step
codebase create action "Submit Registration" --branch "$BRANCH_ID" \
  --type submit --domain auth
codebase graph relate \
  --from step-user-registers-an-account-2 \
  --to act-auth-submit-registration \
  --type has_action \
  --branch "$BRANCH_ID"
```

#### Phase 4 — Verify: determine what needs to be built

`branch verify` compares the branch graph against the main graph and surfaces what's
pending. Use it after Phase 3 to see the full list of objects and relationships the
scenarios require that don't exist yet.

```bash
# Dry-run: what would merge into main?
codebase branch verify --branch "$BRANCH_ID" --verbose

# Full merge diff in JSON — pipe to jq for analysis
memory graph branches merge main --source "$BRANCH_ID" --output json \
  | jq '.items[] | select(.classification == "added") | .object.type + " " + .object.key'
```

This tells you exactly which `APIEndpoint`, `Service`, `UIComponent`, `Context`, and other
objects need to be created — grounding implementation work in the validated scenario plan.

#### Phase 5 — Build out the graph

With validated scenarios as the anchor, fill in the structural layer:

```bash
# Sync routes from code (adds APIEndpoint objects)
codebase sync routes --branch "$BRANCH_ID" --dry-run
codebase sync routes --branch "$BRANCH_ID"

# Sync source files
codebase sync files --branch "$BRANCH_ID"

# Seed entities from Go source
codebase seed entities --branch "$BRANCH_ID" \
  --glob "apps/server/domain/**/*.go"

# Wire Service→exposes→APIEndpoint
codebase seed exposes --branch "$BRANCH_ID"

# Install the constitution (coding rules)
codebase constitution create --branch "$BRANCH_ID"
```

#### Phase 6 — Final validation and merge

```bash
# Re-run logic check — should be clean
codebase check logic --branch "$BRANCH_ID"

# Check API quality
codebase check api --branch "$BRANCH_ID"

# Dry-run merge
memory graph branches merge main --source "$BRANCH_ID"

# Merge when clean
memory graph branches merge main --source "$BRANCH_ID" --execute

# Clean up
memory graph branches delete "$BRANCH_ID"
```

#### Summary: fresh graph checklist

```
[ ] .codebase.yml bound to project
[ ] Planning branch created
[ ] Scenarios created with steps
[ ] Scenarios validated (analyze scenarios --show-empty, check logic)
[ ] Every non-terminal step has context + action
[ ] branch verify run — pending objects identified
[ ] Routes synced, entities seeded
[ ] check api clean
[ ] Dry-run merge passes (no conflicts)
[ ] Merged to main
[ ] Branch deleted
```

### First-time graph population

```bash
codebase sync routes --dry-run    # preview
codebase sync routes              # write APIEndpoints
codebase sync middleware          # wire Middleware rels
codebase seed exposes             # wire Service→exposes→APIEndpoint
```

### Daily audit

```bash
codebase check api                # any broken endpoints?
codebase check coverage           # coverage gaps?
codebase sync routes --dry-run    # any drift from code?
```

### Explore a domain

```bash
codebase analyze tree --domain agents
codebase check complexity --domain agents --recommendations
codebase graph list --type APIEndpoint --filter domain=agents --all
```

### Fix drift after code changes

```bash
codebase sync routes --dry-run    # see what changed
codebase sync routes              # apply updates
codebase fix stale --dry-run      # see stale objects
codebase fix stale                # remove them
```

### Graph surgery

```bash
# Find an object
codebase graph get ep-agents-listagents

# See its relationships
codebase graph tree ep-agents-listagents

# Update a property
codebase graph update --key ep-agents-listagents --props '{"description":"updated"}'

# Wire a new relationship
codebase graph relate --from ep-agents-listagents --to svc-agents-list --type calls
```

### Competitive landscape import (with branch)

```bash
# 1. Create branch
BRANCH_ID=$(memory graph branches create --name "competitive/q2-2026" --output json \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

# 2. Add competitors + data to branch
codebase create competitor "Hermes" --branch "$BRANCH_ID" --category personal-agent --status active --open-source
codebase create featuregap "MCP Proxy" --branch "$BRANCH_ID" --impact critical --effort medium
codebase create strategicinitiative "Build MCP Support" --branch "$BRANCH_ID" --priority high --status planned
codebase graph relate --from comp-hermes --to gap-mcp-proxy --type exposes_gap --branch "$BRANCH_ID"
codebase graph relate --from init-build-mcp-support --to comp-hermes --type responds_to --branch "$BRANCH_ID"

# 3. Review
codebase analyze competitive --branch "$BRANCH_ID"

# 4. Dry-run merge
memory graph branches merge main --source "$BRANCH_ID"

# 5. Execute when clean
memory graph branches merge main --source "$BRANCH_ID" --execute
memory graph branches delete "$BRANCH_ID"
```

---

## Output Formats

All commands support `--format table|json|markdown` (global flag).

```bash
codebase check coverage --format markdown   # paste into docs
codebase graph list --type Domain --format json | jq '.[] | .key'
```

---

## Flags Reference

| Flag | Scope | Description |
|---|---|---|
| `--project-id` | global | Override project ID |
| `--branch` | global | Graph branch ID (default: main) |
| `--format` | global | Output: table, json, markdown |
| `--dry-run` | sync, fix, rename, prune | Preview without writing |
| `--all` | graph list | Paginate all results |
| `--domain` | check, analyze tree/uml/scenarios | Filter to one domain |
| `--verbose` | check logic, graph list | More detail |
| `--top N` | check complexity | Show top N only |
| `--recommendations` | check complexity | Add refactor suggestions |
| `--force` | skills install | Overwrite existing files |
