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

const json = (res, obj) => {
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(obj));
};

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://localhost:${PORT}`);
  const path = url.pathname;

  if (path === "/api/projects/p1/agent-definitions" && req.method === "GET") {
    return json(res, { success: true, data: agents });
  }
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
