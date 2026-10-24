// mock-memory.mjs — STATEFUL mock of the Emergent Memory API for e2e tests.
// The gateway points at this (MEMORY_URL=http://localhost:5301) so tests are
// deterministic and never touch production memory.
import http from "node:http";

const PORT = Number(process.env.MOCK_PORT || 5301);

// The gateway origin. `run-e2e.sh` boots the gateway on `MEMORY_PORT` (default
// 8097), so OAuth metadata this mock returns must name the same origin — a
// hardcoded 8097 would point a non-default-port run's authorize/redirect flows
// at the wrong server.
const GATEWAY_ORIGIN = `http://localhost:${process.env.MEMORY_PORT || 8097}`;

const agents = [
  { id: "agent-1", name: "memory", flowType: "single", visibility: "project", toolCount: 3, isDefault: false, createdAt: "2026-08-25T08:18:01Z", updatedAt: "2026-08-25T08:18:01Z" },
  { id: "agent-2", name: "diane", flowType: "single", visibility: "project", toolCount: 6, isDefault: false, createdAt: "2026-08-25T08:18:02Z", updatedAt: "2026-08-25T08:18:02Z" },
];

const models = [
  { id: "m1", provider: "deepseek", modelName: "deepseek-v4-flash", modelType: "generative", displayName: "DeepSeek V4 Flash" },
  { id: "m2", provider: "google", modelName: "gemini-2.5-flash", modelType: "generative", displayName: "Gemini 2.5 Flash" },
];

// In-memory conversation state (mutated by chat POSTs).
const conversations = [
  { id: "conv-1", title: "Test conversation", agentDefinitionId: "agent-2", projectId: "p1", createdAt: "2026-08-25T09:00:00Z", updatedAt: "2026-08-25T09:00:05Z" },
];
const messages = {
  "conv-1": [
    { role: "user", content: "Hello" },
    { role: "agent", content: "Hi there!" },
  ],
};
let convCounter = 2;

// --- public agent-share state ------------------------------------------------
//
// Public share keys: well-known terminal keys map to deterministic exchange
// errors; VALID_KEYS (plus any non-revoked owner-created link key) resolve to
// the sanitized public config. Sessions are keyed by the X-End-User-Ref header
// the gateway sends (one anonymous visitor = one ref), so a second browser
// context never sees the first visitor's sessions.
const VALID_KEYS = new Set(["valid-key-123"]);

const TERMINAL_KEYS = {
  "revoked-key":    { status: 410, code: "share_link_revoked", message: "share link revoked" },
  "expired-key":    { status: 410, code: "share_link_expired", message: "share link expired" },
  "budget-key":     { status: 429, code: "share_budget_exceeded", message: "share budget exceeded" },
  "rate-limit-key": { status: 429, code: "too_many_requests", message: "rate limited" },
};

// endUserRef -> [session]; a session is the upstream ShareSession DTO shape
// (id/title/isArchived/lastActivityAt/createdAt). Questions live in a separate
// map so session JSON stays clean.
const sessionsByRef = {};
const questionsBySession = {};
// Grounded citations shared by the live `citations` frame and the timeline
// assistant message so Sources renders identically on both paths.
const CITATIONS = [
  { kind: "object", id: "11111111-1111-4111-8111-111111111111", type: "Note", label: "A note", url: "/objects/11111111-1111-4111-8111-111111111111" },
];

// Full-timeline transcripts for the seeded sessions (all item kinds: run
// markers, messages, tool calls, reasoning, citations), served by
// GET /api/share/agent/sessions/:id in the admin history shape (`items` +
// `pending_approvals`). seed-1 is rich (tool chip + thinking + Sources + a code
// block); seed-2 is plain; seed-3 carries a pending ask_user question so the
// inline question card renders on load.
const timelineBySession = {
  "seed-1": [
    { kind: "run_start", run_id: "run-seed-1", step_number: 0, created_at: "2026-08-25T09:00:00Z", run_status: "completed", run_model: "deepseek-v4-flash" },
    { kind: "message", run_id: "run-seed-1", step_number: 1, created_at: "2026-08-25T09:00:01Z", role: "user", content: { text: "What can you do?" } },
    { kind: "tool_call", run_id: "run-seed-1", step_number: 2, created_at: "2026-08-25T09:00:02Z", id: "tc-seed-1", tool_name: "web_search", tool_input: { query: "capabilities" }, tool_output: { results: [{ title: "Memory capabilities" }] }, tool_status: "completed", duration_ms: 120 },
    { kind: "message", run_id: "run-seed-1", step_number: 3, created_at: "2026-08-25T09:00:03Z", role: "assistant", content: { text: "I can help with tasks.\n\n```js\nconsole.log('hi')\n```", reasoning: "The visitor asked what I can do." }, citations: CITATIONS },
    { kind: "run_end", run_id: "run-seed-1", step_number: 0, created_at: "2026-08-25T09:00:03Z", run_status: "completed", completed_at: "2026-08-25T09:00:04Z" },
  ],
  "seed-2": [
    { kind: "run_start", run_id: "run-seed-2", step_number: 0, created_at: "2026-08-24T09:00:00Z", run_status: "completed", run_model: "deepseek-v4-flash" },
    { kind: "message", run_id: "run-seed-2", step_number: 1, created_at: "2026-08-24T09:00:01Z", role: "user", content: { text: "Set a reminder." } },
    { kind: "message", run_id: "run-seed-2", step_number: 2, created_at: "2026-08-24T09:00:02Z", role: "assistant", content: { text: "Reminder set." } },
    { kind: "run_end", run_id: "run-seed-2", step_number: 0, created_at: "2026-08-24T09:00:02Z", run_status: "completed", completed_at: "2026-08-24T09:00:03Z" },
  ],
  "seed-3": [
    { kind: "run_start", run_id: "run-seed-3", step_number: 0, created_at: "2026-08-23T09:00:00Z", run_status: "input-required", run_model: "deepseek-v4-flash" },
    { kind: "message", run_id: "run-seed-3", step_number: 1, created_at: "2026-08-23T09:00:01Z", role: "user", content: { text: "Ask me something." } },
    { kind: "tool_call", run_id: "run-seed-3", step_number: 2, created_at: "2026-08-23T09:00:02Z", id: "tc-seed-3", tool_name: "ask_user", tool_input: { question: "Which option?", interaction_type: "buttons", options: [{ label: "Alpha", value: "alpha" }, { label: "Beta", value: "beta" }] }, tool_output: { question_id: "q-seed-3" }, tool_status: "awaiting_confirmation" },
    { kind: "run_end", run_id: "run-seed-3", step_number: 0, created_at: "2026-08-23T09:00:02Z", run_status: "input-required" },
  ],
};

// recordTurn appends one full run to a session's timeline (the turn the mock
// just streamed), so a post-turn transcript refresh returns the same items the
// live stream rendered. wantQuestion turns record no web_search tool call (the
// ask_user question renders live via the `question` frame).
function recordTurn(sid, msg, wantQuestion) {
  if (!timelineBySession[sid]) timelineBySession[sid] = [];
  const t = timelineBySession[sid];
  const n = t.filter((x) => x.kind === "run_start").length + 1;
  const runId = `run-${sid}-${n}`;
  const at = nowISO();
  t.push({ kind: "run_start", run_id: runId, step_number: 0, created_at: at, run_status: "completed", run_model: "deepseek-v4-flash" });
  t.push({ kind: "message", run_id: runId, step_number: 1, created_at: at, role: "user", content: { text: msg } });
  if (wantQuestion) {
    t.push({ kind: "tool_call", run_id: runId, step_number: 2, created_at: at, id: `tc-${sid}-${n}`, tool_name: "ask_user", tool_input: { question: "Which option do you want?", interaction_type: "buttons", options: [{ label: "Alpha", value: "alpha" }, { label: "Beta", value: "beta" }] }, tool_output: { question_id: `q-live-${sid}` }, tool_status: "awaiting_confirmation" });
  } else {
    t.push({ kind: "tool_call", run_id: runId, step_number: 2, created_at: at, id: `tc-${sid}-${n}`, tool_name: "web_search", tool_input: { query: "latest news" }, tool_output: { results: [{ title: "News item" }] }, tool_status: "completed", duration_ms: 120 });
  }
  t.push({ kind: "message", run_id: runId, step_number: 3, created_at: at, role: "assistant", content: { text: "**Hello** from share!", reasoning: "The visitor asked a question; I searched and answered." }, citations: CITATIONS });
  t.push({ kind: "run_end", run_id: runId, step_number: 0, created_at: at, run_status: "completed", completed_at: at });
}

// pendingApprovalsOf maps the seeded pending questions to the admin
// pending_approvals shape ({ questionId, tool, input }).
function pendingApprovalsOf(sid) {
  return (questionsBySession[sid] || []).map((q) => ({ questionId: q.id, tool: q.tool, input: q.input }));
}
// Per-token count of GET /api/share/agent calls, so a "revoke-later" key can
// succeed on the first call (exchange) then fail on the next (config rehydrate).
const configCalls = {};
let sessionCounter = 1;

// Owner share links (project p1, agent agent-1).
const shareLinks = [];
let linkCounter = 1;

const nowISO = () => new Date().toISOString();

function ensureSessions(ref) {
  if (!sessionsByRef[ref]) {
    sessionsByRef[ref] = [
      { id: "seed-1", title: "Welcome chat", isArchived: false, lastActivityAt: nowISO(), createdAt: nowISO() },
      { id: "seed-2", title: "Archived chat", isArchived: true, lastActivityAt: nowISO(), createdAt: nowISO() },
      { id: "seed-3", title: "Question chat", isArchived: false, lastActivityAt: nowISO(), createdAt: nowISO() },
    ];
  }
  return sessionsByRef[ref];
}

function filterSessions(list, filter) {
  if (!filter || filter === "all") return list;
  if (filter === "archived") return list.filter((s) => s.isArchived);
  return list.filter((s) => !s.isArchived); // "active" (and the client default)
}

function defaultShareConfig() {
  return {
    link_expiry_days: 30,
    budget_window_seconds: 0,
    budget_max_messages: 0,
    budget_max_tokens: 0,
    budget_max_cost_usd: 0,
    max_active_sessions_per_user: 0,
    max_concurrent_runs: 1,
    max_approvals_per_session: 0,
    approval_timeout_seconds: 0,
    max_message_chars: 4000,
    retention_days: 30,
    require_email: false,
    show_session_list: true,
    sandbox_enabled: false,
    allow_end_user_approvals: true,
    tool_allowlist: [],
  };
}

function publicConfig() {
  return {
    linkId: "link-1",
    agentName: "Memory",
    agentDescription: "A helpful assistant.",
    icon: "database",
    color: "#2563EB",
    model: "deepseek-v4-flash",
    requireEmail: false,
    showSessionList: true,
    maxMessageChars: 4000,
    allowEndUserApprovals: true,
    sandboxEnabled: false,
    retentionDays: 30,
    budgetMaxMessages: 0,
    budgetMaxTokens: 0,
    budgetMaxCostUSD: 0,
    maxActiveSessionsPerUser: 0,
    maxConcurrentRuns: 1,
  };
}

// publicConfigForLink maps a snake_case link config onto the sanitized camelCase
// public config the exchange serves. Identity fields mirror publicConfig();
// owner-tunable fields (limits, budgets, toggles) are derived from the stored
// config so the exchange faithfully reflects what the owner configured.
function publicConfigForLink(linkConfig) {
  const cfg = linkConfig || defaultShareConfig();
  const out = {
    linkId: "link-1",
    agentName: "Memory",
    agentDescription: "A helpful assistant.",
    icon: "database",
    color: "#2563EB",
    model: "deepseek-v4-flash",
    requireEmail: !!cfg.require_email,
    showSessionList: !!cfg.show_session_list,
    maxMessageChars: cfg.max_message_chars,
    allowEndUserApprovals: !!cfg.allow_end_user_approvals,
    sandboxEnabled: !!cfg.sandbox_enabled,
    retentionDays: cfg.retention_days,
    budgetMaxMessages: cfg.budget_max_messages,
    budgetMaxTokens: cfg.budget_max_tokens,
    budgetMaxCostUSD: cfg.budget_max_cost_usd,
    maxActiveSessionsPerUser: cfg.max_active_sessions_per_user,
    maxConcurrentRuns: cfg.max_concurrent_runs,
  };
  if (cfg.welcome_message) out.welcomeMessage = cfg.welcome_message;
  return out;
}

function shareLinkObj(l) {
  return {
    id: l.id,
    projectId: "p1",
    agentDefinitionId: l.agentId,
    label: l.label,
    config: l.config,
    apiTokenPrefix: "sk_share",
    token: l.token || undefined, // only present at create/rotate
    createdAt: l.createdAt,
    updatedAt: l.updatedAt,
    lastUsedAt: l.lastUsedAt ?? undefined,
    revokedAt: l.revokedAt ?? undefined,
    expiresAt: l.expiresAt ?? undefined,
  };
}

// --- data-source integrations state (Data Source Integrations API) -----------
//
// Stateful store backing the data-sources UI. Sync behavior is keyed off the
// source's folder_id so a spec can deterministically drive an in-progress job
// (`folder-slow`), a failed job (`folder-fail`), or a clean completed sync
// (anything else). Credentials are write-only: they are stored on the source so
// connection tests can vary, but are never returned in a read response,
// mirroring the server contract.

const dsProviders = [
  {
    type: "google_drive",
    name: "Google Drive",
    description: "Sync documents from a Google Drive folder.",
    sourceType: "drive",
    vendor: "google",
    vendorName: "Google",
    available: true,
    authMethods: ["oauth"],
    strategies: ["drive.file", "drive.readonly"],
  },
  {
    type: "github",
    name: "GitHub",
    description: "Sync documentation from a GitHub repository.",
    sourceType: "repo",
    vendor: "github",
    vendorName: "GitHub",
    available: true,
    authMethods: ["oauth", "credentials"],
  },
  {
    type: "web",
    name: "Web",
    description: "Crawl a website and sync its pages into your knowledge base.",
    sourceType: "web",
    vendor: "web",
    vendorName: "Web",
    available: true,
    authMethods: ["config"],
  },
  {
    type: "trale",
    name: "Trale",
    description: "Sync meeting transcripts and summaries from Trale into your knowledge base.",
    sourceType: "meeting",
    vendor: "trale",
    vendorName: "Trale",
    available: true,
    selfRegistering: true,
    authMethods: ["oauth"],
  },
];

// Per-provider config schemas (the connect form is schema-driven). The mock
// mirrors each provider's UI contract: google_drive is OAuth-only and declares
// no credential fields (its form must never render a pasted-credential
// surface), while github carries its repo/ref contract plus the write-only
// token. The create endpoint still requires a Drive refresh_token, like the
// server's ExtractCredentials for legacy/API clients.
const dsSchemas = {
  google_drive: {
    type: "google_drive",
    properties: {
      folders: {
        type: "array",
        "x-ui-widget": "folders",
        description: "Folders to sync; each entry is {folder_id, name, drive_id}.",
        items: {
          type: "object",
          properties: {
            folder_id: { type: "string" },
            name: { type: "string" },
            drive_id: { type: "string" },
          },
          required: ["folder_id"],
        },
      },
      mime_types: { type: "array", items: { type: "string" } },
      recursive: { type: "boolean" },
    },
  },
  github: {
    type: "github",
    properties: {
      repo: { type: "string", "x-ui-widget": "resource", description: "The GitHub repository to sync from (owner/name)." },
      ref: { type: "string", "x-ui-widget": "ref", description: "The branch, tag, or commit SHA to sync from." },
      paths: { type: "array" },
      extensions: { type: "array" },
      access_token: { type: "string", writeOnly: true, label: "Personal access token" },
    },
    required: ["repo", "access_token"],
  },
  web: {
    type: "web",
    properties: {
      start_urls: { type: "array", items: { type: "string" }, description: "One or more absolute http(s) URLs to start from (max 50)." },
      include_globs: { type: "array", items: { type: "string" }, description: "Optional URL path globs to keep (e.g. /docs/**). Empty keeps every in-scope page." },
      exclude_globs: { type: "array", items: { type: "string" }, description: "Optional URL path globs to skip (e.g. /admin/**)." },
      max_pages: { type: "integer", description: "Maximum pages to crawl. Default 200 (the v1 ceiling)." },
      max_depth: { type: "integer", description: "Maximum link depth to follow. Default 3; 0 ingests only the configured start URLs (no link following)." },
    },
    required: ["start_urls"],
  },
  trale: {
    type: "trale",
    properties: {
      tags: { type: "array", items: { type: "string" }, "x-ui-widget": "resource", description: "Trale tag slugs to sync (ANY match)." },
      name_pattern: { type: "string", description: "Optional Go RE2 regular expression matched against the meeting title; non-matching meetings are skipped." },
      include_transcript: { type: "boolean", description: "Include the speaker-tagged transcript in the imported document. Default false (summary only)." },
      backfill_days: { type: "integer", description: "How many days back to sync. Default 90." },
    },
    required: ["tags"],
  },
};

const dsSourceTypes = [{ sourceType: "drive", documentCount: 3 }];

// Per-scope OAuth-client state for the credentials pages. Secrets are never
// stored here (the write-only contract is asserted by the specs).
const dsOAuthState = {
  org: new Map(),
  project: new Map(),
};

function dsStoreOAuthClient(payload, providerType) {
  return {
    providerType,
    clientId: String(payload.clientId || ""),
    scopes: Array.isArray(payload.scopes) ? payload.scopes : [],
    strategy: String(payload.strategy || ""),
    config: payload.config || {},
    configured: true,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  };
}

// dsStoredOAuthClient renders a stored client in the redacted DTO shape. The
// deployment callback/origin values are always present so the console guide
// renders on the editor pages.
function dsStoredOAuthClient(stored) {
  return {
    ...stored,
    redirectUri: `${GATEWAY_ORIGIN}/api/data-source-integrations/oauth/${stored.providerType}/callback`,
    javascriptOrigin: GATEWAY_ORIGIN,
  };
}

// dsOAuthClientView renders one provider's client, defaulting to unconfigured.
function dsOAuthClientView(stored, providerType) {
  if (stored) return dsStoredOAuthClient(stored);
  return {
    providerType,
    clientId: "",
    configured: false,
    redirectUri: `${GATEWAY_ORIGIN}/api/data-source-integrations/oauth/${providerType}/callback`,
    javascriptOrigin: GATEWAY_ORIGIN,
  };
}

// Seeded with the source the OAuth bounce-back lands on (`connected=s1`), so the
// OAuth callback → detail flow resolves. Specs that create their own sources are
// unaffected — they filter the list by name.
// dsFolderIDs reads the selected folder ids from a source config, canonical
// `folders` first and the legacy single-target keys as the fallback. The sync
// outcome specs key off these ids.
function dsFolderIDs(config) {
  const folders = Array.isArray(config && config.folders) ? config.folders : [];
  const ids = folders
    .map((f) => String((f && f.folder_id) || ""))
    .filter(Boolean);
  if (ids.length) return ids;
  const legacy = String((config && config.folder_id) || "");
  return legacy ? [legacy] : [];
}

// The folder catalog the picker save resolves names/drive ids from.
const dsMockFolders = {
  "folder-ok": { name: "Reports" },
  "folder-slow": { name: "Slow folder" },
  "folder-fail": { name: "Failing folder" },
  "folder-picked": { name: "Picked Folder", drive_id: "drive-1" },
  "folder-two": { name: "Second Folder" },
};

let dsSources = [
  {
    row: {
      id: "s1",
      projectId: "p1",
      name: "Team Drive",
      providerType: "google_drive",
      sourceType: "drive",
      syncMode: "manual",
      syncIntervalMinutes: null,
      lastSyncedAt: null,
      nextSyncAt: null,
      status: "active",
      errorCount: 0,
      createdAt: "2026-09-01T00:00:00Z",
      updatedAt: "2026-09-01T00:00:00Z",
    },
    config: {},
  },
]; // [{ row, config }] — the full config (incl. credentials) is server-side only; dsIntegration returns a stripped copy.
let dsJobsBySource = {}; // sourceId -> [job], newest first.
let dsCounter = 1;
let dsJobCounter = 1;

// dsIntegration projects a stored source: the row plus its non-secret config
// (credential keys stripped). Credentials never leave here.
//
// Write-only credential keys, stripped from every read response. This is the
// union of the legacy/OAuth client keys and Trale's extra write-only set — the
// server's traleCredentialKeyNames is access_token/refresh_token/expires_at/
// client_id/token_endpoint/resource/scope (apps/server/domain/sources/
// trale_provider.go); client_secret covers the OAuth-client editor surface. Any
// provider whose stored config can carry a secret must list that key here or it
// leaks on read.
const dsCredentialKeys = [
  "client_id",
  "client_secret",
  "refresh_token",
  "access_token",
  "expires_at",
  "token_endpoint",
  "resource",
  "scope",
];
const dsIntegration = (src) => {
  const config = {};
  for (const [k, v] of Object.entries(src.config || {})) {
    if (!dsCredentialKeys.includes(k)) config[k] = v;
  }
  return { ...src.row, config };
};

// re2Incompatibility approximates the Go RE2 restriction set the server applies
// when it compiles `name_pattern` (regexp.Compile — see apps/server/domain/
// sources/trale_connector.go). JavaScript's RegExp accepts Perl constructs RE2
// rejects, so a pattern that passes a JS-only check could still fail on the real
// server. Reject the two divergences a UI could plausibly submit — lookaround
// and backreferences — while leaving named groups alone: Go 1.22+ RE2 accepts
// both `(?<name>…)` and `(?P<name>…)` (and JS already rejects `(?P<name>…)` on
// its own). Returns a reason string, or null when RE2 would accept the pattern.
function re2Incompatibility(pattern) {
  // Lookahead/lookbehind: `(?=`, `(?!`, `(?<=`, `(?<!` — RE2 has none of these.
  if (/\(\?(?:[=!]|<[=!])/.test(pattern)) {
    return "lookahead/lookbehind are not supported by RE2";
  }
  // Backreferences `\1`…`\9`: a single digit after a backslash is a JS
  // backreference but RE2 rejects it as an "invalid escape sequence". Exclude
  // `\NN`/`\NNN` octal (valid in both) by requiring no second OCTAL digit — a
  // trailing 8/9 is not an octal digit, so `\18`/`\79` are rejected like Go
  // does, while `\128`/`\178` (three octal digits) are left alone.
  if (/\\(?:[89]|[1-7](?![0-7]))/.test(pattern)) {
    return "backreferences are not supported by RE2";
  }
  return null;
}

// dsNewSyncJob prepends a job to a source's history.
function dsNewSyncJob(sourceID, status, extra) {
  const job = {
    id: `dsjob-${dsJobCounter++}`,
    integrationId: sourceID,
    status,
    totalItems: 0,
    processedItems: 0,
    successfulItems: 0,
    failedItems: 0,
    skippedItems: 0,
    triggerType: "manual",
    retryCount: 0,
    maxRetries: 3,
    createdAt: nowISO(),
    ...extra,
  };
  dsJobsBySource[sourceID] = [job, ...(dsJobsBySource[sourceID] || [])];
  return job;
}

// --- documents + version history (document-versioning) -----------------------
//
// Two seeded documents exercise the version-history surface: `doc-report` has
// two active versions (a stable document re-synced with new content), and
// `doc-removed` shows the deleted_from_source terminal state.

const mockDocuments = {
  "doc-report": {
    id: "doc-report",
    filename: "report.md",
    mimeType: "text/markdown",
    sourceType: "external",
    chunks: 3,
    embeddedChunks: 3,
    totalChars: 120,
    extractionStatus: "completed",
    processingStatus: "completed",
    objectsCreated: 2,
    relationshipsCreated: 1,
    sourceId: "ds-1",
    sourceExternalId: "file-report",
    sourceRevisionId: "rev-2",
    sourceStatus: "active",
    sourceSyncedAt: "2026-09-02T10:00:00Z",
    createdAt: "2026-09-01T10:00:00Z",
    updatedAt: "2026-09-02T10:00:00Z",
  },
  "doc-removed": {
    id: "doc-removed",
    filename: "removed-notes.md",
    mimeType: "text/markdown",
    sourceType: "external",
    chunks: 2,
    extractionStatus: "completed",
    processingStatus: "completed",
    sourceId: "ds-2",
    sourceExternalId: "file-removed",
    sourceRevisionId: "rev-2",
    sourceStatus: "deleted_from_source",
    sourceDeletedAt: "2026-09-03T10:00:00Z",
    createdAt: "2026-09-01T10:00:00Z",
    updatedAt: "2026-09-03T10:00:00Z",
  },
};

const mockDocumentVersions = {
  "doc-report": [
    { id: "vd-1", canonicalId: "canon-report", version: 1, sourceStatus: "active", sourceRevisionId: "rev-1", createdAt: "2026-09-01T10:00:00Z" },
    { id: "vd-2", canonicalId: "canon-report", version: 2, supersedesId: "vd-1", sourceStatus: "active", sourceRevisionId: "rev-2", createdAt: "2026-09-02T10:00:00Z" },
  ],
  "doc-removed": [
    { id: "vr-1", canonicalId: "canon-removed", version: 1, sourceStatus: "active", sourceRevisionId: "rev-1", createdAt: "2026-09-01T10:00:00Z" },
    { id: "vr-2", canonicalId: "canon-removed", version: 2, supersedesId: "vr-1", sourceStatus: "deleted_from_source", sourceRevisionId: "rev-2", sourceDeletedAt: "2026-09-03T10:00:00Z", createdAt: "2026-09-03T10:00:00Z" },
  ],
};

const json = (res, obj) => {
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(obj));
};

const bearerOf = (req) => {
  const h = req.headers["authorization"] || "";
  return h.startsWith("Bearer ") ? h.slice(7) : "";
};

const endUserRefOf = (req) => req.headers["x-end-user-ref"] || "";

function readBody(req, cb) {
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    let payload = {};
    try {
      payload = JSON.parse(body);
    } catch {
      /* ignore */
    }
    cb(payload);
  });
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://localhost:${PORT}`);
  const path = url.pathname;

  // --- public agent-share (anonymous, bearer key + X-End-User-Ref) -----------

  if (path === "/api/share/agent" && req.method === "GET") {
    const key = bearerOf(req);
    const terminal = TERMINAL_KEYS[key];
    if (terminal) {
      res.statusCode = terminal.status;
      return json(res, { error: { code: terminal.code, message: terminal.message } });
    }
    // A key that passes exchange but is revoked afterwards, so a cookie-gated
    // config rehydrate (GET /share/api/config) reveals the revoked terminal.
    if (key === "revoke-later-key") {
      configCalls[key] = (configCalls[key] || 0) + 1;
      if (configCalls[key] === 1) return json(res, publicConfig());
      res.statusCode = 410;
      return json(res, { error: { code: "share_link_revoked", message: "share link revoked" } });
    }
    // A well-known key whose link has the session list disabled, so the
    // flash-elimination test can drive a deterministic showSessionList:false
    // exchange without the owner-page round trip.
    if (key === "no-rail-key") {
      return json(res, { ...publicConfig(), showSessionList: false });
    }
    // A well-known key whose link has end-user approvals disabled, so the
    // read-only card test can drive a deterministic allowEndUserApprovals:false
    // exchange without an owner-page round trip.
    if (key === "no-approvals-key") {
      return json(res, { ...publicConfig(), allowEndUserApprovals: false });
    }
    const created = shareLinks.find((l) => l.token === key && !l.revokedAt);
    if (VALID_KEYS.has(key)) {
      return json(res, publicConfig());
    }
    if (created) {
      return json(res, publicConfigForLink(created.config));
    }
    res.statusCode = 401;
    return json(res, { error: { code: "share_link_not_found", message: "share link not found" } });
  }

  if (path === "/api/share/agent/stream" && req.method === "POST") {
    return readBody(req, (payload) => {
      const ref = payload.endUserRef || endUserRefOf(req);
      const sid = String(payload.sessionId || "");
      const msg = String(payload.message || "");
      const wantQuestion = /question/i.test(msg);
      const list = ensureSessions(ref);
      const sess = list.find((s) => s.id === sid);
      if (sess && !sess.title) sess.title = msg.slice(0, 60) || "Untitled chat";
      if (sess) sess.lastActivityAt = nowISO();

      res.setHeader("Content-Type", "text/event-stream");
      res.setHeader("Cache-Control", "no-cache");
      // Full shared-admin engine frame set: reasoning/thinking, tool progress,
      // token deltas, grounded citations, then either an approval request or an
      // ask_user question, then done. The gateway still injects `meta` + `html`
      // snapshot frames upstream of the client.
      res.write(`data: ${JSON.stringify({ type: "thinking", id: "th-1", role: "assistant", text: "The visitor asked a question; I searched and answered.", done: true })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "mcp_tool", tool: "web_search", id: "tc-live", status: "running" })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "mcp_tool", tool: "web_search", id: "tc-live", status: "completed", result: { results: [{ title: "News item" }] } })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "token", token: "**Hello**" })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "token", token: " from share!" })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "citations", citations: CITATIONS })}\n\n`);
      if (wantQuestion) {
        // Real wire for a live pause: the agent emits an ask_user MCP tool call
        // (start carries the question, completed carries the question_id). The
        // gateway translates it into the client `question` frame exactly as it
        // does for /api/chat, and suppresses the raw tool frames.
        res.write(`data: ${JSON.stringify({ type: "mcp_tool", tool: "ask_user", id: "tc-live-q", status: "started", result: { question: "Which option do you want?", interaction_type: "buttons", options: [{ label: "Alpha", value: "alpha" }, { label: "Beta", value: "beta" }] } })}\n\n`);
        res.write(`data: ${JSON.stringify({ type: "mcp_tool", tool: "ask_user", id: "tc-live-q", status: "completed", result: { question_id: `q-live-${sid}`, status: "pausing" } })}\n\n`);
      } else {
        res.write(`data: ${JSON.stringify({ type: "approval", questionId: `q-live-${sid}`, tool: "web_search", input: { query: "latest news" } })}\n\n`);
      }
      res.write(`data: ${JSON.stringify({ type: "done" })}\n\n`);
      res.end();

      // Record the streamed turn into the timeline so a post-turn transcript
      // refresh is consistent with what the live stream rendered.
      recordTurn(sid, msg, wantQuestion);
    });
  }

  if (path === "/api/share/agent/sessions" && req.method === "GET") {
    const ref = endUserRefOf(req);
    const filter = url.searchParams.get("filter") || "";
    return json(res, filterSessions(ensureSessions(ref), filter));
  }

  if (path === "/api/share/agent/sessions" && req.method === "POST") {
    return readBody(req, (payload) => {
      const ref = payload.endUserRef || endUserRefOf(req);
      const list = ensureSessions(ref);
      const id = `sess-${sessionCounter++}`;
      const sess = { id, title: payload.title || "", isArchived: false, lastActivityAt: nowISO(), createdAt: nowISO() };
      list.push(sess);
      // Every POST-created session carries one pending approval so the end-user
      // approval flow is deterministic (seeded sessions carry none).
      questionsBySession[id] = [
        { id: `q-${id}`, runId: "run-1", question: "Approve this action?", tool: "web_search", input: { query: "latest news" }, status: "pending" },
      ];
      res.statusCode = 201;
      return json(res, sess);
    });
  }

  const archiveMatch = path.match(/^\/api\/share\/agent\/sessions\/([^/]+)\/archive$/);
  if (archiveMatch && req.method === "POST") {
    const ref = endUserRefOf(req);
    const sess = ensureSessions(ref).find((s) => s.id === archiveMatch[1]);
    if (sess) sess.isArchived = true;
    return json(res, { status: "archived" });
  }

  const unarchiveMatch = path.match(/^\/api\/share\/agent\/sessions\/([^/]+)\/unarchive$/);
  if (unarchiveMatch && req.method === "POST") {
    const ref = endUserRefOf(req);
    const sess = ensureSessions(ref).find((s) => s.id === unarchiveMatch[1]);
    if (sess) sess.isArchived = false;
    return json(res, { status: "unarchived" });
  }

  const deleteSessionMatch = path.match(/^\/api\/share\/agent\/sessions\/([^/]+)$/);
  if (deleteSessionMatch && req.method === "DELETE") {
    const ref = endUserRefOf(req);
    const id = deleteSessionMatch[1];
    sessionsByRef[ref] = (ensureSessions(ref) || []).filter((s) => s.id !== id);
    delete questionsBySession[id];
    delete timelineBySession[id];
    res.statusCode = 204;
    return res.end();
  }

  const approveMatch = path.match(/^\/api\/share\/agent\/sessions\/([^/]+)\/approvals\/([^/]+)$/);
  if (approveMatch && req.method === "POST") {
    return readBody(req, () => {
      const sid = approveMatch[1];
      const qid = approveMatch[2];
      const qs = questionsBySession[sid];
      if (qs) questionsBySession[sid] = qs.filter((q) => q.id !== qid);
      return json(res, { ok: true });
    });
  }

  const cancelMatch = path.match(/^\/api\/share\/agent\/sessions\/([^/]+)\/approvals\/([^/]+)\/cancel$/);
  if (cancelMatch && req.method === "POST") {
    const sid = cancelMatch[1];
    const qid = cancelMatch[2];
    const qs = questionsBySession[sid];
    if (qs) questionsBySession[sid] = qs.filter((q) => q.id !== qid);
    return json(res, { ok: true });
  }

  const shareSessionMatch = path.match(/^\/api\/share\/agent\/sessions\/([^/]+)$/);
  if (shareSessionMatch && req.method === "GET") {
    const ref = endUserRefOf(req);
    const sess = ensureSessions(ref).find((s) => s.id === shareSessionMatch[1]);
    // Detail shape (admin history shape): list DTO + full-timeline `items` +
    // `pending_approvals`, replacing the old transcript `messages`.
    const detail = sess || { id: shareSessionMatch[1], title: "", isArchived: false, createdAt: nowISO() };
    return json(res, {
      id: detail.id,
      title: detail.title,
      isArchived: detail.isArchived,
      createdAt: detail.createdAt,
      items: timelineBySession[detail.id] || [],
      pending_approvals: pendingApprovalsOf(detail.id),
    });
  }

  // --- owner share-link management (project p1, agent agent-1) ---------------

  if (path === "/api/projects/p1/agent-definitions" && req.method === "GET") {
    return json(res, { success: true, data: agents });
  }

  const agentDetailMatch = path.match(/^\/api\/projects\/p1\/agent-definitions\/([^/]+)$/);
  if (agentDetailMatch && req.method === "GET") {
    return json(res, {
      success: true,
      data: {
        id: "agent-1",
        projectId: "p1",
        name: "Memory",
        description: "A helpful assistant.",
        tools: ["web_search", "memory_write"],
        skills: [],
        config: {},
        flowType: "single",
        visibility: "project",
        enabled: true,
        createdAt: "2026-08-25T08:18:01Z",
        updatedAt: "2026-08-25T08:18:01Z",
      },
    });
  }

  const ownerLinksMatch = path.match(/^\/api\/projects\/p1\/agent-definitions\/([^/]+)\/share-links$/);
  if (ownerLinksMatch && req.method === "GET") {
    return json(res, [...shareLinks].sort((a, b) => (a.createdAt < b.createdAt ? 1 : -1)).map(shareLinkObj));
  }
  if (ownerLinksMatch && req.method === "POST") {
    return readBody(req, (payload) => {
      const label = String(payload.label || "Untitled link");
      const config = { ...defaultShareConfig(), ...(payload.config || {}) };
      const link = {
        id: `sl-${linkCounter}`,
        agentId: ownerLinksMatch[1],
        label,
        token: `sh_key_${linkCounter}`,
        config,
        createdAt: nowISO(),
        updatedAt: nowISO(),
        lastUsedAt: null,
        revokedAt: null,
        expiresAt: config.link_expiry_days > 0 ? new Date(Date.now() + config.link_expiry_days * 86400e3).toISOString() : null,
      };
      linkCounter += 1;
      shareLinks.push(link);
      res.statusCode = 201;
      return json(res, shareLinkObj(link));
    });
  }

  const revealMatch = path.match(/^\/api\/projects\/p1\/share-links\/([^/]+)\/reveal$/);
  if (revealMatch && req.method === "GET") {
    const link = shareLinks.find((l) => l.id === revealMatch[1]);
    if (!link) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "not found" } });
    }
    return json(res, { key: link.token });
  }

  const rotateMatch = path.match(/^\/api\/projects\/p1\/share-links\/([^/]+)\/rotate$/);
  if (rotateMatch && req.method === "POST") {
    const link = shareLinks.find((l) => l.id === rotateMatch[1]);
    if (!link) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "not found" } });
    }
    link.token = `sh_key_${linkCounter++}`;
    link.updatedAt = nowISO();
    return json(res, shareLinkObj(link));
  }

  const revokeMatch = path.match(/^\/api\/projects\/p1\/share-links\/([^/]+)$/);
  if (revokeMatch && req.method === "DELETE") {
    const link = shareLinks.find((l) => l.id === revokeMatch[1]);
    if (link) {
      link.revokedAt = nowISO();
      link.updatedAt = nowISO();
    }
    return json(res, { status: "revoked" });
  }

  // --- shell tenancy (best-effort, keeps the owner page shell quiet) ---------

  if (path === "/api/projects" && req.method === "GET") {
    return json(res, [{ id: "p1", name: "E2E Main Project", orgId: "o1" }]);
  }
  if (path === "/api/orgs" && req.method === "GET") {
    return json(res, [{ id: "o1", name: "E2E Main" }]);
  }
  if (path === "/api/v1/projects/p1/providers" && req.method === "GET") {
    return json(res, []);
  }
  if (path === "/api/projects/p1/skills" && req.method === "GET") {
    return json(res, { skills: [] });
  }

  // --- data-source integrations (provider-generic OAuth connect) ------------
  //
  // Mirrors the provider-generic data-source connect surface: the provider
  // catalog (with OAuth + credentials auth methods and org-selectable scope
  // strategies), per-provider config schemas, a seeded source for the OAuth
  // round trip, and the org-scoped OAuth-client config (write-only secret).

  // Org access tree the shell + org-admin gating read (GET /api/user/orgs-and-projects).
  if (path === "/api/user/orgs-and-projects" && req.method === "GET") {
    return json(res, [
      { id: "org-1", name: "E2E Org", role: "org_admin", projects: [{ id: "p1", name: "E2E Main", orgId: "org-1", role: "project_admin" }] },
    ]);
  }

  // Compiled schema types (best-effort shell call; empty is fine).
  if (path === "/api/schemas/projects/p1/compiled-types" && req.method === "GET") {
    return json(res, { objectTypes: [], relationshipTypes: [] });
  }

  // Org record (GET /api/orgs/:id), used by the org OAuth-clients page header.
  if (path === "/api/orgs/org-1" && req.method === "GET") {
    return json(res, { id: "org-1", name: "E2E Org" });
  }

  // OAuth authorize: the gateway's UI flow (StartDSOAuth) issues a GET here and
  // follows the returned authorizeUrl; the JSON API handler is POST. Both are
  // served the same bounce-back URL so the browser lands on the gateway's
  // /data-sources/oauth/callback, which redirects to the connected source.
  const dsAuthorizeMatch = path.match(/^\/api\/data-source-integrations\/providers\/([^/]+)\/oauth\/authorize$/);
  if (dsAuthorizeMatch && (req.method === "GET" || req.method === "POST")) {
    return json(res, { authorizeUrl: `${GATEWAY_ORIGIN}/data-sources/oauth/callback?connected=s1` });
  }

  // --- org OAuth-client config (org-admin only; write-only secret) -----------
  //
  // State is in-memory so the credentials table reflects a save made during a
  // spec run; secrets are never stored or echoed.

  const dsOrgClientsList = path.match(/^\/api\/orgs\/([^/]+)\/data-source-integrations\/oauth-clients$/);
  if (dsOrgClientsList && req.method === "GET") {
    return json(res, [...dsOAuthState.org.values()].map((c) => dsStoredOAuthClient(c)));
  }

  const dsOrgClientMatch = path.match(/^\/api\/orgs\/([^/]+)\/data-source-integrations\/oauth-clients\/([^/]+)$/);
  if (dsOrgClientMatch && req.method === "GET") {
    return json(res, dsOAuthClientView(dsOAuthState.org.get(dsOrgClientMatch[2]), dsOrgClientMatch[2]));
  }
  if (dsOrgClientMatch && req.method === "PUT") {
    return readBody(req, (payload) => {
      // Echo the saved client WITHOUT the secret (write-only, never returned).
      const stored = dsStoreOAuthClient(payload, dsOrgClientMatch[2]);
      dsOAuthState.org.set(dsOrgClientMatch[2], stored);
      return json(res, dsStoredOAuthClient(stored));
    });
  }
  if (dsOrgClientMatch && req.method === "DELETE") {
    dsOAuthState.org.delete(dsOrgClientMatch[2]);
    res.statusCode = 204;
    return res.end();
  }

  // --- project OAuth-client config (project-admin only; write-only secret) ---

  const dsProjectClientsList = path.match(/^\/api\/projects\/([^/]+)\/data-source-integrations\/oauth-clients$/);
  if (dsProjectClientsList && req.method === "GET") {
    return json(res, [...dsOAuthState.project.values()].map((c) => dsStoredOAuthClient(c)));
  }

  const dsProjectClientMatch = path.match(/^\/api\/projects\/([^/]+)\/data-source-integrations\/oauth-clients\/([^/]+)$/);
  if (dsProjectClientMatch && req.method === "GET") {
    return json(res, dsOAuthClientView(dsOAuthState.project.get(dsProjectClientMatch[2]), dsProjectClientMatch[2]));
  }
  if (dsProjectClientMatch && req.method === "PUT") {
    return readBody(req, (payload) => {
      const stored = dsStoreOAuthClient(payload, dsProjectClientMatch[2]);
      dsOAuthState.project.set(dsProjectClientMatch[2], stored);
      return json(res, dsStoredOAuthClient(stored));
    });
  }
  if (dsProjectClientMatch && req.method === "DELETE") {
    dsOAuthState.project.delete(dsProjectClientMatch[2]);
    res.statusCode = 204;
    return res.end();
  }

  // Active project record (GET /api/projects/:id), used by the project settings
  // pages (e.g. the project OAuth-clients page header).
  const projectRecordMatch = path.match(/^\/api\/projects\/([^/]+)$/);
  if (projectRecordMatch && req.method === "GET") {
    return json(res, { id: projectRecordMatch[1], name: "E2E Main", orgId: "org-1" });
  }

  // --- legacy chat surface (kept for the existing smoke path) ----------------

  if (path === "/api/v1/models" && req.method === "GET") {
    return json(res, models);
  }
  if (path === "/api/chat/conversations" && req.method === "GET") {
    return json(res, { conversations: [...conversations].reverse(), total: conversations.length });
  }
  if (path === "/api/chat/stream" && req.method === "POST") {
    let body = "";
    req.on("data", (c) => (body += c));
    return req.on("end", () => {
      let payload = {};
      try { payload = JSON.parse(body); } catch { /* ignore */ }
      const cid = payload.conversationId || `conv-${convCounter++}`;
      const userMsg = String(payload.message || "");

      if (!conversations.some((c) => c.id === cid)) {
        conversations.push({
          id: cid,
          title: userMsg.slice(0, 60) || "New chat",
          agentDefinitionId: payload.agentDefinitionId || "agent-2",
          projectId: "p1",
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        });
        messages[cid] = [];
      }
      messages[cid].push({ role: "user", content: userMsg });
      messages[cid].push({ role: "agent", content: "Hello from mock!" });

      res.setHeader("Content-Type", "text/event-stream");
      res.write(`data: ${JSON.stringify({ type: "meta", conversationId: cid })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "token", token: "Hello" })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "token", token: " from mock!" })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "done" })}\n\n`);
      res.end();
    });
  }

  const histMatch = path.match(/^\/api\/chat\/([^/]+)\/history$/);
  if (histMatch && req.method === "GET") {
    const cid = histMatch[1];
    const msgs = messages[cid] || [];
    return json(res, {
      session_id: `sess-${cid}`,
      conversation_id: cid,
      items: msgs.map((m) => ({
        kind: "message",
        run_id: "r1",
        role: m.role === "agent" ? "diane" : m.role,
        content: { text: m.content },
        created_at: new Date().toISOString(),
      })),
    });
  }

  const detailMatch = path.match(/^\/api\/chat\/([^/]+)$/);
  if (detailMatch && req.method === "GET") {
    const cid = detailMatch[1];
    const conv = conversations.find((c) => c.id === cid) || { id: cid, title: "Conversation" };
    const msgs = messages[cid] || [];
    return json(res, {
      ...conv,
      messages: msgs.map((m, i) => ({ id: `${cid}-${i}`, conversationId: cid, role: m.role, content: m.content })),
    });
  }

  // --- data-source integrations (Data Source Integrations API) --------------

  if (path === "/api/data-source-integrations/providers" && req.method === "GET") {
    return json(res, dsProviders);
  }

  // Member-safe credential descriptions: configured flags + non-secret client
  // ids + the resolved scope. Secrets never appear here.
  if (path === "/api/data-source-integrations/credentials" && req.method === "GET") {
    const items = dsProviders.map((p) => {
      const project = dsOAuthState.project.get(p.type);
      const org = dsOAuthState.org.get(p.type);
      const resolvedScope = project ? "project" : org ? "organization" : "none";
      return {
        providerType: p.type,
        project: { configured: !!project, clientId: project ? project.clientId : "" },
        organization: { configured: !!org, clientId: org ? org.clientId : "" },
        deployment: { configured: false },
        resolvedScope,
      };
    });
    return json(res, { items });
  }

  const dsSchemaMatch = path.match(/^\/api\/data-source-integrations\/providers\/([^/]+)\/schema$/);
  if (dsSchemaMatch && req.method === "GET") {
    const schema = dsSchemas[dsSchemaMatch[1]];
    if (!schema) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "unknown provider" } });
    }
    return json(res, schema);
  }

  if (path === "/api/data-source-integrations/source-types" && req.method === "GET") {
    return json(res, dsSourceTypes);
  }

  if (path === "/api/data-source-integrations/test-config" && req.method === "POST") {
    return readBody(req, (payload) => {
      const config = payload.config || {};
      if (payload.providerType === "github") {
        if (!String(config.access_token || "")) {
          return json(res, { success: false, message: "GitHub token is required." });
        }
        return json(res, { success: true, message: "Connected to GitHub." });
      }
      // Web is config-only: the start URL list is the whole scope, no credential.
      if (payload.providerType === "web") {
        const urls = Array.isArray(config.start_urls) ? config.start_urls : [];
        if (urls.length === 0) {
          return json(res, { success: false, message: "config.start_urls is required" });
        }
        if (urls.some((u) => /fail/i.test(String(u)))) {
          return json(res, { success: false, message: "Start URL is not reachable." });
        }
        return json(res, { success: true, message: "Connected to Web." });
      }
      // ValidateConfig then ExtractCredentials, like the server: at least one
      // folder target is required before the stored credential.
      const folders = dsFolderIDs(config);
      if (!folders.length) {
        return json(res, { success: false, message: "config.folders must name at least one folder" });
      }
      if (!String(config.refresh_token || "")) {
        return json(res, { success: false, message: "config.refresh_token is required" });
      }
      if (folders.includes("folder-fail")) {
        return json(res, { success: false, message: "Drive folder not found." });
      }
      return json(res, { success: true, message: "Connected to Google Drive." });
    });
  }

  // Credential-scoped listing for the connect form's repo picker. The token is
  // in the body and is never stored; only identifiers come back.
  if (path === "/api/data-source-integrations/resources" && req.method === "POST") {
    return readBody(req, (payload) => {
      if (payload.providerType === "trale") {
        // Trale's resource listing is the tag picker backing the required
        // `tags` scope (mirrors ListResources on trale_provider.go).
        return json(res, [
          { id: "standup", name: "Standup", type: "tag" },
          { id: "planning", name: "Planning", type: "tag" },
          { id: "one-on-one", name: "One-on-one", type: "tag" },
        ]);
      }
      if (payload.providerType !== "github") {
        return json(res, []);
      }
      return json(res, [
        { id: "acme/private-docs", name: "acme/private-docs", type: "repo" },
        { id: "acme/public-docs", name: "acme/public-docs", type: "repo" },
      ]);
    });
  }

  // Credential-scoped ref listing for the connect form's ref picker.
  if (path === "/api/data-source-integrations/refs" && req.method === "POST") {
    return readBody(req, (payload) => {
      const repo = String((payload.config || {}).repo || "");
      if (repo !== "acme/private-docs") {
        res.statusCode = 422;
        return json(res, {
          error: { code: "validation_error", message: `repository ${repo || "(none)"} not found` },
        });
      }
      return json(res, [
        { id: "main", name: "main", type: "branch" },
        { id: "release/1.x", name: "release/1.x", type: "branch" },
        { id: "v1.0.0", name: "v1.0.0", type: "tag" },
      ]);
    });
  }

  if (path === "/api/data-source-integrations" && req.method === "GET") {
    return json(res, dsSources.map(dsIntegration));
  }

  if (path === "/api/data-source-integrations" && req.method === "POST") {
    return readBody(req, (payload) => {
      const config = payload.config || {};
      // Mirror the server's provider validation: the gateway no longer applies
      // Drive-only rules, so an incomplete config must still fail here.
      if (payload.providerType === "github") {
        if (!String(config.repo || "")) {
          res.statusCode = 422;
          return json(res, { error: { code: "validation_error", message: "config.repo is required" } });
        }
        if (!String(config.access_token || "")) {
          res.statusCode = 422;
          return json(res, { error: { code: "validation_error", message: "config.access_token is required" } });
        }
      } else if (payload.providerType === "web") {
        // Config-only: the start URL list is the only required scope.
        const urls = Array.isArray(config.start_urls) ? config.start_urls : [];
        if (urls.length === 0) {
          res.statusCode = 422;
          return json(res, { error: { code: "validation_error", message: "config.start_urls is required" } });
        }
      } else if (payload.providerType === "trale") {
        // Mirror the server's validateTraleConfig (trale_connector.go): tags is
        // the required scope (choose at least one slug), name_pattern must
        // compile, and backfill_days is an optional integer in [0, 3650]. The
        // message strings match the server copy so the spec asserts real text.
        const tags = Array.isArray(config.tags) ? config.tags : [];
        if (tags.length === 0) {
          res.statusCode = 422;
          return json(res, { error: { code: "validation_error", message: "config.tags is required: choose at least one Trale tag to sync" } });
        }
        if (tags.some((t) => typeof t !== "string" || t.trim() === "")) {
          res.statusCode = 422;
          return json(res, { error: { code: "validation_error", message: "config.tags must be non-empty slugs" } });
        }
        if (typeof config.name_pattern === "string" && config.name_pattern.trim() !== "") {
          try {
            new RegExp(config.name_pattern);
          } catch (err) {
            res.statusCode = 422;
            return json(res, { error: { code: "validation_error", message: "config.name_pattern is not a valid regular expression: " + err.message } });
          }
          // JS compiled it, but the server validates with RE2 (regexp.Compile),
          // which is stricter. Reject the RE2-unsupported constructs so the mock
          // is never more permissive than the real server.
          const re2 = re2Incompatibility(config.name_pattern);
          if (re2) {
            res.statusCode = 422;
            return json(res, { error: { code: "validation_error", message: "config.name_pattern is not a valid regular expression: " + re2 } });
          }
        }
        if (config.backfill_days !== undefined && config.backfill_days !== null) {
          const raw = config.backfill_days;
          if (typeof raw === "string" && raw.trim() === "") {
            // A blank value means "use the default", matching the server.
          } else {
            const n = typeof raw === "number" ? raw : Number(raw);
            if (typeof raw !== "number" && !Number.isInteger(n)) {
              res.statusCode = 422;
              return json(res, { error: { code: "validation_error", message: "config.backfill_days must be an integer" } });
            }
            if (n < 0 || n > 3650) {
              res.statusCode = 422;
              return json(res, { error: { code: "validation_error", message: "config.backfill_days must be between 0 and 3650" } });
            }
          }
        }
      } else {
        if (!dsFolderIDs(config).length && payload.allowIncompleteConfig !== true) {
          res.statusCode = 422;
          return json(res, { error: { code: "validation_error", message: "config.folders must name at least one folder" } });
        }
        if (!String(config.refresh_token || "")) {
          res.statusCode = 422;
          return json(res, { error: { code: "validation_error", message: "config.refresh_token is required" } });
        }
      }
      const now = nowISO();
      const row = {
        id: `ds-${dsCounter++}`,
        projectId: "p1",
        name: String(payload.name || "Data source"),
        providerType: payload.providerType || "google_drive",
        sourceType: payload.sourceType || (payload.providerType === "github" ? "repo" : payload.providerType === "web" ? "web" : "drive"),
        syncMode: payload.syncMode || "manual",
        syncIntervalMinutes: payload.syncIntervalMinutes,
        lastSyncedAt: null,
        nextSyncAt: null,
        status: "active",
        errorCount: 0,
        createdAt: now,
        updatedAt: now,
      };
      const src = { row, config: payload.config || {} };
      dsSources.push(src);
      res.statusCode = 201;
      return json(res, dsIntegration(src));
    });
  }

  const dsJobsLatestMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)\/sync-jobs\/latest$/);
  if (dsJobsLatestMatch && req.method === "GET") {
    const jobs = dsJobsBySource[dsJobsLatestMatch[1]] || [];
    return json(res, jobs[0] || null);
  }

  const dsCancelMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)\/sync-jobs\/([^/]+)\/cancel$/);
  if (dsCancelMatch && req.method === "POST") {
    const jobs = dsJobsBySource[dsCancelMatch[1]] || [];
    const job = jobs.find((j) => j.id === dsCancelMatch[2]);
    if (job) {
      job.status = "cancelled";
      job.currentPhase = "Cancelled";
      job.completedAt = nowISO();
    }
    return json(res, { success: true, message: "Sync cancelled." });
  }

  const dsJobMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)\/sync-jobs\/([^/]+)$/);
  if (dsJobMatch && req.method === "GET") {
    const jobs = dsJobsBySource[dsJobMatch[1]] || [];
    const job = jobs.find((j) => j.id === dsJobMatch[2]);
    if (!job) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "job not found" } });
    }
    return json(res, job);
  }

  const dsJobsListMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)\/sync-jobs$/);
  if (dsJobsListMatch && req.method === "GET") {
    const jobs = dsJobsBySource[dsJobsListMatch[1]] || [];
    return json(res, { items: jobs, total: jobs.length });
  }

  const dsSyncMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)\/sync$/);
  if (dsSyncMatch && req.method === "POST") {
    const src = dsSources.find((s) => s.row.id === dsSyncMatch[1]);
    if (!src) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "source not found" } });
    }
    let job;
    if (src.row.providerType === "web") {
      // Web sync outcomes key off the start URL list, not a folder id.
      const urls = Array.isArray(src.config.start_urls) ? src.config.start_urls : [];
      const scope = urls.join(" ");
      if (/slow/i.test(scope)) {
        job = dsNewSyncJob(src.row.id, "processing", {
          totalItems: 10,
          processedItems: 3,
          successfulItems: 3,
          currentPhase: "Crawling",
          statusMessage: "Crawling pages from the start URL.",
        });
      } else if (/fail/i.test(scope)) {
        job = dsNewSyncJob(src.row.id, "failed", {
          totalItems: 1,
          processedItems: 1,
          failedItems: 1,
          errorMessage: "Start URL is not reachable.",
          completedAt: nowISO(),
        });
      } else {
        job = dsNewSyncJob(src.row.id, "completed", {
          totalItems: 3,
          processedItems: 3,
          successfulItems: 3,
          currentPhase: "Done",
          completedAt: nowISO(),
        });
        src.row.lastSyncedAt = nowISO();
        src.row.updatedAt = nowISO();
      }
      return json(res, { success: true, message: "Sync started.", jobId: job.id });
    }
    if (src.row.providerType === "trale") {
      // Trale has no folder scope: a source with valid tags always completes.
      job = dsNewSyncJob(src.row.id, "completed", {
        totalItems: 2,
        processedItems: 2,
        successfulItems: 2,
        currentPhase: "Done",
        completedAt: nowISO(),
      });
      src.row.lastSyncedAt = nowISO();
      src.row.updatedAt = nowISO();
      return json(res, { success: true, message: "Sync started.", jobId: job.id });
    }
    const folders = dsFolderIDs(src.config);
    if (folders.includes("folder-slow")) {
      job = dsNewSyncJob(src.row.id, "processing", {
        totalItems: 10,
        processedItems: 3,
        successfulItems: 3,
        currentPhase: "Downloading",
        statusMessage: "Scanning the configured folder.",
      });
    } else if (folders.includes("folder-fail")) {
      job = dsNewSyncJob(src.row.id, "failed", {
        totalItems: 1,
        processedItems: 1,
        failedItems: 1,
        errorMessage: "Drive returned 403 for the configured folder.",
        completedAt: nowISO(),
      });
    } else {
      job = dsNewSyncJob(src.row.id, "completed", {
        totalItems: 3,
        processedItems: 3,
        successfulItems: 3,
        currentPhase: "Done",
        completedAt: nowISO(),
      });
      src.row.lastSyncedAt = nowISO();
      src.row.updatedAt = nowISO();
    }
    return json(res, { success: true, message: "Sync started.", jobId: job.id });
  }

  // Short-lived Google Picker bootstrap. Mirrors the server: the token is minted
  // from the source's own credentials and a drive.file grant cannot browse
  // folders. The hermetic specs stub window.google/gapi, so this only supplies
  // the session payload.
  const dsPickerMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)\/picker-session$/);
  if (dsPickerMatch && req.method === "GET") {
    const src = dsSources.find((s) => s.row.id === dsPickerMatch[1]);
    if (!src) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "source not found" } });
    }
    if (String(src.config.oauth_granted_strategy || "") === "drive.file") {
      res.statusCode = 422;
      return json(res, {
        error: {
          code: "validation_error",
          message:
            "this source was authorized with per-file access (drive.file), so the folder picker cannot browse its folders; reconnect the source with read-only Drive access",
        },
      });
    }
    return json(res, {
      accessToken: "mock-picker-token",
      developerKey: "mock-picker-key",
      appId: "123456789",
      origin: GATEWAY_ORIGIN,
      expiresAt: new Date(Date.now() + 3600e3).toISOString(),
    });
  }

  // Folder-picker save. Only ids travel from the browser; names and shared-drive
  // ids are resolved against the folder catalog, like the server resolves them
  // from Drive.
  const dsTargetsMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)\/sync-targets$/);
  if (dsTargetsMatch && req.method === "PUT") {
    return readBody(req, (payload) => {
      const src = dsSources.find((s) => s.row.id === dsTargetsMatch[1]);
      if (!src) {
        res.statusCode = 404;
        return json(res, { error: { code: "not_found", message: "source not found" } });
      }
      const ids = Array.isArray(payload.folderIds)
        ? payload.folderIds.map((id) => String(id || "").trim()).filter(Boolean)
        : [];
      if (!ids.length) {
        res.statusCode = 422;
        return json(res, { error: { code: "validation_error", message: "select at least one folder" } });
      }
      // Mirrors ResolveResourceTargets: the bound is checked before any lookup,
      // and an id the source cannot read fails the whole save.
      if (ids.length > 50) {
        res.statusCode = 422;
        return json(res, { error: { code: "validation_error", message: "select at most 50 folders" } });
      }
      const unknown = ids.find((id) => !dsMockFolders[id]);
      if (unknown) {
        res.statusCode = 422;
        // Mirrors ResolveResourceTargets (#2162): when *none* of the picked ids
        // resolves the cause is the account the Picker ran in, not the folder.
        const allUnknown = ids.every((id) => !dsMockFolders[id]);
        return json(res, {
          error: {
            code: "validation_error",
            message: allUnknown
              ? "none of the selected folders can be seen by the account that connected this source; if you picked them while signed into a different Google account, pick again with the source's account (or share those folders with it)"
              : "one of the selected folders is not accessible: google drive: folder \"" + unknown + "\" not found",
          },
        });
      }
      const folders = ids.map((id) => {
        const known = dsMockFolders[id];
        const entry = { folder_id: id, name: known.name };
        if (known.drive_id) entry.drive_id = known.drive_id;
        return entry;
      });
      src.config = { ...src.config, folders };
      delete src.config.folder_id;
      delete src.config.shared_drive_id;
      src.row.updatedAt = nowISO();
      return json(res, dsIntegration(src));
    });
  }

  const dsTestConnMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)\/test-connection$/);
  if (dsTestConnMatch && req.method === "POST") {
    const src = dsSources.find((s) => s.row.id === dsTestConnMatch[1]);
    if (!src) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "source not found" } });
    }
    if (src.row.providerType === "web") {
      const urls = Array.isArray(src.config.start_urls) ? src.config.start_urls : [];
      if (/fail/i.test(urls.join(" "))) {
        return json(res, { success: false, message: "Start URL is not reachable." });
      }
      return json(res, { success: true, message: "Connection OK." });
    }
    if (dsFolderIDs(src.config).includes("folder-fail")) {
      return json(res, { success: false, message: "Drive folder not found." });
    }
    return json(res, { success: true, message: "Connection OK." });
  }

  const dsItemMatch = path.match(/^\/api\/data-source-integrations\/([^/]+)$/);
  if (dsItemMatch && req.method === "GET") {
    const src = dsSources.find((s) => s.row.id === dsItemMatch[1]);
    if (!src) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "source not found" } });
    }
    return json(res, dsIntegration(src));
  }

  if (dsItemMatch && req.method === "PATCH") {
    return readBody(req, (payload) => {
      const src = dsSources.find((s) => s.row.id === dsItemMatch[1]);
      if (!src) {
        res.statusCode = 404;
        return json(res, { error: { code: "not_found", message: "source not found" } });
      }
      if (payload.name) src.row.name = payload.name;
      if (payload.syncMode) src.row.syncMode = payload.syncMode;
      if (payload.syncIntervalMinutes != null) src.row.syncIntervalMinutes = payload.syncIntervalMinutes;
      if (payload.config) src.config = { ...src.config, ...payload.config };
      src.row.updatedAt = nowISO();
      return json(res, dsIntegration(src));
    });
  }

  if (dsItemMatch && req.method === "DELETE") {
    dsSources = dsSources.filter((s) => s.row.id !== dsItemMatch[1]);
    delete dsJobsBySource[dsItemMatch[1]];
    res.statusCode = 204;
    return res.end();
  }

  // --- documents + version history ------------------------------------------

  const docVersionsMatch = path.match(/^\/api\/documents\/([^/]+)\/versions$/);
  if (docVersionsMatch && req.method === "GET") {
    return json(res, { versions: mockDocumentVersions[docVersionsMatch[1]] || [] });
  }

  const docMatch = path.match(/^\/api\/documents\/([^/]+)$/);
  if (docMatch && req.method === "GET") {
    const doc = mockDocuments[docMatch[1]];
    if (!doc) {
      res.statusCode = 404;
      return json(res, { error: { code: "not_found", message: "document not found" } });
    }
    return json(res, doc);
  }

  // --- MCP servers (settings page, specs/mcp/) --------------------------------
  //
  // The MCP servers settings page needs one outbound http server whose inspect
  // answers auth_required + unavailable, so the disabled-Connect branch in
  // gateway/mcp_servers.templ renderInspect is exercised deterministically. The
  // gateway proxies memory's /api/admin/mcp-servers namespace (list + inspect),
  // so the mock serves both shapes here.
  if (path === "/api/admin/mcp-servers" && req.method === "GET") {
    return json(res, {
      success: true,
      data: [
        {
          id: "mcp-unavailable",
          name: "unavailable-oauth-server",
          type: "http",
          url: "http://localhost:9999",
          enabled: true,
          toolCount: 0,
        },
      ],
    });
  }

  const mcpInspectMatch = path.match(/^\/api\/admin\/mcp-servers\/([^/]+)\/inspect$/);
  if (mcpInspectMatch && req.method === "POST") {
    return json(res, {
      success: true,
      data: {
        serverId: mcpInspectMatch[1],
        serverName: "unavailable-oauth-server",
        serverType: "http",
        status: "auth_required",
        latencyMs: 0,
        auth: {
          unavailable: true,
          remedy: "configure a token header for this server instead (e.g. Authorization: Bearer <token>)",
        },
        tools: [],
        prompts: [],
        resources: [],
        resourceTemplates: [],
      },
    });
  }

  res.statusCode = 404;
  json(res, { error: { code: "not_found", message: "not found" } });
});

server.listen(PORT, () => console.log(`mock memory on :${PORT}`));
