---
description: Analyze logs for anomalies, errors, duplicate operations, and suggest fixes with a todo plan
agent: diagnostics
---

# /analyze-logs

Perform a comprehensive analysis of all application and infrastructure logs from the last 3 hours, identifying issues and creating an actionable fix plan.

$ARGUMENTS

## User Arguments (Optional)

Interpret arguments as follows:

- **Time range**: "last hour", "last 30m", "last 6h" → adjust analysis window (default: 3h)
- **Severity filter**: "errors only", "critical" → focus on specific severity
- **Service filter**: "server", "admin", "postgres", "zitadel" → focus on specific logs

---

## Analysis Process

### Phase 1: Infrastructure Health Check

1. Use `workspace_get_status` to get overall health of services and dependencies
2. Note any unhealthy or degraded components for priority investigation

### Phase 2: Application Log Analysis

Collect logs from the last 3 hours (or user-specified range). Use `lines=500` for comprehensive coverage.

1. **Error Extraction**

   - Use `logs_get_errors` with `lines=100` to get recent errors
   - Look for: `ERROR`, `FATAL`, `Exception`, `Error:`, `failed`, `crash`

2. **Server Logs**

   - Use `logs_tail_server_logs` with `lines=500`
   - Look for: slow operations (>1000ms), repeated patterns, warnings

3. **HTTP Traffic**

   - Use `logs_tail_http_logs` with `lines=300`
   - Look for: 4xx/5xx responses, slow endpoints, repeated identical requests

4. **Admin/Frontend Logs**

   - Use `logs_tail_admin_logs` with `lines=200`
   - Look for: build errors, client-side exceptions, React errors

5. **Debug Logs** (if issues unclear)
   - Use `logs_tail_debug_logs` with `lines=200`
   - Look for: verbose warnings, edge case handling

### Phase 3: Infrastructure Container Logs

Check Docker container logs for infrastructure issues:

1. **Postgres**

   - Use `workspace_docker_logs` with `container=postgres`, `since=3h`, `lines=200`
   - Look for: connection errors, slow queries, deadlocks, disk space

2. **Zitadel (Auth)**

   - Use `workspace_docker_logs` with `container=zitadel`, `since=3h`, `lines=200`
   - Look for: auth failures, token errors, OIDC issues

3. **Redis** (if applicable)
   - Use `workspace_docker_logs` with `container=redis`, `since=3h`, `lines=100`
   - Look for: memory warnings, connection issues

### Phase 4: Pattern Detection

While analyzing, specifically look for:

| Category        | Patterns                                                                  |
| --------------- | ------------------------------------------------------------------------- |
| **Errors**      | `ERROR`, `FATAL`, `Exception`, `ECONNREFUSED`, `ETIMEDOUT`, `ENOENT`      |
| **Duplicates**  | Identical messages within 1 min, repeated API calls, same error recurring |
| **Performance** | `>1000ms`, `slow`, `timeout`, `latency`, `took \d+ms`                     |
| **Security**    | `401`, `403`, `unauthorized`, `invalid token`, `authentication failed`    |
| **Resources**   | `memory`, `OOM`, `disk`, `quota`, `limit exceeded`                        |

### Severity Classification

- **🔴 Critical**: `FATAL`, unhandled exceptions, service unavailable, data loss risk, security breaches
- **🟠 Warning**: `ERROR` level, timeouts, repeated failures, auth issues, performance degradation
- **🔵 Info**: Slow operations, unusual patterns, potential optimizations

---

## Output Format

Present findings as a **summary report** (not full log dumps):

```
## 📊 Log Analysis Report

**Analysis Period:** [start time] to [end time] (3 hours)
**Generated:** [current timestamp]

---

### 🔴 Critical Issues (X found)

#### 1. [Issue Title]
- **Source:** [log file or container name]
- **Count:** [X occurrences in analysis period]
- **First/Last:** [first timestamp] / [last timestamp]
- **Summary:** [1-2 sentence description of the issue]
- **Sample:** `[single truncated log line as example]`

[Repeat for each critical issue, max 5 detailed]

---

### 🟠 Warnings & Anomalies (X found)

| Issue | Source | Count | Summary |
|-------|--------|-------|---------|
| [title] | [source] | [N] | [brief description] |

[Table format for brevity, max 10 rows]

---

### 🔵 Performance Issues (X found)

| Operation/Endpoint | Avg Time | Max Time | Count | Source |
|--------------------|----------|----------|-------|--------|
| [name] | [Xms] | [Xms] | [N] | [log] |

[Table format, max 10 rows]

---

### 🟡 Duplicate/Wasteful Operations (X found)

| Pattern | Count | Window | Potential Impact |
|---------|-------|--------|------------------|
| [description] | [N times] | [X min] | [e.g., "unnecessary API load"] |

[Table format, max 5 rows]

---

### 🟢 Infrastructure Health Summary

| Component | Status | Issues |
|-----------|--------|--------|
| Server | ✅ Healthy / ⚠️ Degraded / ❌ Down | [brief or "None"] |
| Admin | ✅ / ⚠️ / ❌ | [brief or "None"] |
| Postgres | ✅ / ⚠️ / ❌ | [brief or "None"] |
| Zitadel | ✅ / ⚠️ / ❌ | [brief or "None"] |
| Langfuse | ✅ / ⚠️ / ❌ | [brief or "None"] |
| Redis | ✅ / ⚠️ / ❌ | [brief or "None"] |

---

### ✅ Suggested Fixes

| # | Issue | Suggested Fix | Priority | Complexity |
|---|-------|---------------|----------|------------|
| 1 | [issue summary] | [specific actionable fix] | High | Low |
| 2 | [issue summary] | [specific actionable fix] | High | Medium |
| 3 | [issue summary] | [specific actionable fix] | Medium | Low |

[Prioritized list of actionable fixes]
```

---

## Final Step: Create Fix Plan

After presenting the report, use the `TodoWrite` tool to create a prioritized task list for fixing the identified issues.

**Todo List Structure:**

- Group by priority (High → Medium → Low)
- Each todo should be actionable and specific
- Include the source/location of the issue
- Mark all as `pending` status

**Example:**

```
todos:
  - id: "fix-1"
    content: "[High] Fix database connection pool exhaustion in server logs - add connection timeout"
    status: "pending"
    priority: "high"
  - id: "fix-2"
    content: "[High] Investigate repeated 401 errors from Zitadel - check token refresh logic"
    status: "pending"
    priority: "high"
  - id: "fix-3"
    content: "[Medium] Optimize slow /api/objects endpoint (avg 2.3s) - add database index"
    status: "pending"
    priority: "medium"
  - id: "fix-4"
    content: "[Low] Remove duplicate API calls in admin client - implement request deduplication"
    status: "pending"
    priority: "low"
```

Inform the user that the fix plan has been created and they can proceed with implementation.

---

## Notes

- This is a **read-only analysis** - no files are modified
- Focus on **actionable insights**, not raw log dumps
- When in doubt about severity, classify as Warning (🟠)
- If no issues found, report a clean bill of health
- If user asks to "dig deeper" into a specific issue, use `search_logs` or `tail_log` for that specific pattern/file
