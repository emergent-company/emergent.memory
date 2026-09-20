// mock-memory.mjs — STATEFUL mock of the Emergent Memory API for e2e tests.
// The gateway points at this (MEMORY_URL=http://localhost:5301) so tests are
// deterministic and never touch production memory.
import http from "node:http";

const PORT = Number(process.env.MOCK_PORT || 5301);

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
// Deterministic transcript for the seeded sessions, served by
// GET /api/share/agent/sessions/:id (the gateway now relays messages so
// resuming a session renders its history).
const messagesBySession = {
  "seed-1": [
    { role: "user", content: "What can you do?" },
    { role: "assistant", content: "I can help with tasks." },
  ],
  "seed-2": [
    { role: "user", content: "Set a reminder." },
    { role: "assistant", content: "Reminder set." },
  ],
};
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
    ];
  }
  return sessionsByRef[ref];
}

function filterSessions(list, filter) {
  if (!filter || filter === "all") return list;
  if (filter === "archived") return list.filter((s) => s.isArchived);
  return list.filter((s) => !s.isArchived); // "active" (and the client default)
}

function publicConfig() {
  return {
    linkId: "link-1",
    agentName: "Memory",
    agentDescription: "A helpful assistant.",
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

function shareLinkObj(l) {
  return {
    id: l.id,
    projectId: "p1",
    agentDefinitionId: l.agentId,
    label: l.label,
    config: {
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
    },
    apiTokenPrefix: "sk_share",
    token: l.token || undefined, // only present at create/rotate
    createdAt: l.createdAt,
    updatedAt: l.updatedAt,
    lastUsedAt: l.lastUsedAt ?? undefined,
    revokedAt: l.revokedAt ?? undefined,
    expiresAt: l.expiresAt ?? undefined,
  };
}

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
    const created = shareLinks.find((l) => l.token === key && !l.revokedAt);
    if (VALID_KEYS.has(key) || created) {
      return json(res, publicConfig());
    }
    res.statusCode = 401;
    return json(res, { error: { code: "share_link_not_found", message: "share link not found" } });
  }

  if (path === "/api/share/agent/stream" && req.method === "POST") {
    return readBody(req, (payload) => {
      const ref = payload.endUserRef || endUserRefOf(req);
      const sid = String(payload.sessionId || "");
      const msg = String(payload.message || "");
      const list = ensureSessions(ref);
      const sess = list.find((s) => s.id === sid);
      if (sess && !sess.title) sess.title = msg.slice(0, 60) || "Untitled chat";
      sess.lastActivityAt = nowISO();

      res.setHeader("Content-Type", "text/event-stream");
      res.setHeader("Cache-Control", "no-cache");
      res.write(`data: ${JSON.stringify({ type: "token", token: "Hello" })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "token", token: " from share!" })}\n\n`);
      res.write(`data: ${JSON.stringify({ type: "done" })}\n\n`);
      res.end();
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
        { id: `q-${id}`, runId: "run-1", question: "Approve this action?", proposal: { kind: "web_search", query: "latest news" }, status: "pending" },
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

  if (path === "/api/share/agent/questions" && req.method === "GET") {
    const sid = url.searchParams.get("sessionId") || "";
    return json(res, questionsBySession[sid] || []);
  }

  const shareSessionMatch = path.match(/^\/api\/share\/agent\/sessions\/([^/]+)$/);
  if (shareSessionMatch && req.method === "GET") {
    const ref = endUserRefOf(req);
    const sess = ensureSessions(ref).find((s) => s.id === shareSessionMatch[1]);
    // Detail shape (ShareSessionDetail): list DTO + transcript messages.
    const detail = sess || { id: shareSessionMatch[1], title: "", isArchived: false, createdAt: nowISO() };
    return json(res, {
      id: detail.id,
      title: detail.title,
      isArchived: detail.isArchived,
      messages: messagesBySession[detail.id] || [],
      createdAt: detail.createdAt,
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
      const link = {
        id: `sl-${linkCounter}`,
        agentId: ownerLinksMatch[1],
        label,
        token: `sh_key_${linkCounter}`,
        createdAt: nowISO(),
        updatedAt: nowISO(),
        lastUsedAt: null,
        revokedAt: null,
        expiresAt: null,
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
      acp_session_id: `acp-${cid}`,
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

  res.statusCode = 404;
  json(res, { error: { code: "not_found", message: "not found" } });
});

server.listen(PORT, () => console.log(`mock memory on :${PORT}`));
