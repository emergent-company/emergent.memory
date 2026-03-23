#!/usr/bin/env python3
"""
API Coverage Checker — verifies that the Python SDK covers the server's REST API.

Parses all `routes.go` files under `apps/server/domain/` to extract registered
HTTP endpoints, then parses all SDK `.py` files under `sdk/python/emergent/` to
extract the endpoints they call.  Compares the two sets and reports:

  - Endpoints the SDK covers
  - Endpoints the server exposes but the SDK does NOT cover (gaps)
  - SDK calls that don't match any known server route (stale/wrong)

Endpoints can be intentionally excluded from coverage checks (admin-only,
internal, webhooks, etc.) by adding them to the EXCLUDED set below.

Usage:
    python3 sdk/python/scripts/check_api_coverage.py

Exit codes:
    0  — all non-excluded endpoints are covered
    1  — coverage gaps found
"""
from __future__ import annotations

import os
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, List, Set, Tuple

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

REPO_ROOT = Path(__file__).resolve().parents[3]  # sdk/python/scripts -> repo root
SERVER_DOMAIN = REPO_ROOT / "apps" / "server" / "domain"
SDK_SRC = REPO_ROOT / "sdk" / "python" / "emergent"

# Endpoints intentionally excluded from SDK coverage.
# These are admin-only, internal, infrastructure, or webhook endpoints that
# a public SDK shouldn't expose.  Add new exclusions here with a comment.
EXCLUDED: Set[str] = {
    # Health / debug / metrics (infrastructure, not user-facing)
    "GET /health",
    "GET /healthz",
    "GET /ready",
    "GET /debug",
    "GET /api/health",
    "GET /api/diagnostics",
    "GET /api/metrics/jobs",
    "GET /api/metrics/scheduler",

    # Auth info (internal)
    "GET /api/auth/issuer",
    "GET /api/auth/me",

    # Superadmin (privileged, not for SDK users)
    "GET /api/superadmin/me",
    "GET /api/superadmin/users",
    "DELETE /api/superadmin/users/:param",
    "GET /api/superadmin/organizations",
    "DELETE /api/superadmin/organizations/:param",
    "GET /api/superadmin/projects",
    "DELETE /api/superadmin/projects/:param",
    "GET /api/superadmin/email-jobs",
    "GET /api/superadmin/email-jobs/:param/preview-json",
    "GET /api/superadmin/embedding-jobs",
    "POST /api/superadmin/embedding-jobs/delete",
    "POST /api/superadmin/embedding-jobs/cleanup-orphans",
    "POST /api/superadmin/embedding-jobs/reset-dead-letter",
    "GET /api/superadmin/extraction-jobs",
    "POST /api/superadmin/extraction-jobs/delete",
    "POST /api/superadmin/extraction-jobs/cancel",
    "GET /api/superadmin/document-parsing-jobs",
    "POST /api/superadmin/document-parsing-jobs/delete",
    "POST /api/superadmin/document-parsing-jobs/retry",
    "GET /api/superadmin/sync-jobs",
    "GET /api/superadmin/sync-jobs/:param/logs",
    "POST /api/superadmin/sync-jobs/delete",
    "POST /api/superadmin/sync-jobs/cancel",
    "POST /api/superadmin/service-tokens",

    # Docs site (static content API)
    "GET /api/docs",
    "GET /api/docs/categories",
    "GET /api/docs/:param",

    # GitHub app (OAuth callback flow, not SDK-driven)
    "GET /api/v1/settings/github",
    "POST /api/v1/settings/github/connect",
    "GET /api/v1/settings/github/callback",
    "DELETE /api/v1/settings/github",
    "POST /api/v1/settings/github/cli",
    "POST /api/v1/settings/github/webhook",

    # Webhooks (inbound, server receives these)
    "POST /api/webhooks/agents/:param",

    # SSE transport (long-lived connection, not REST)
    "GET /api/mcp/sse/:param",
    "POST /api/mcp/sse/:param/message",

    # User profile (session-bound, not typical SDK use)
    "GET /api/user/profile",
    "PUT /api/user/profile",

    # User search (admin UI)
    "GET /api/users/search",

    # User activity tracking (UI-driven)
    "POST /api/user-activity/record",
    "GET /api/user-activity/recent",
    "GET /api/user-activity/recent/:param",
    "DELETE /api/user-activity/recent",
    "DELETE /api/user-activity/recent/:param/:param",

    # Notifications (UI-driven)
    "GET /api/notifications/stats",
    "GET /api/notifications/counts",
    "GET /api/notifications",
    "PATCH /api/notifications/:param/read",
    "DELETE /api/notifications/:param/dismiss",
    "POST /api/notifications/mark-all-read",

    # Legacy upload endpoint (duplicate)
    "POST /api/document-parsing-jobs/upload",

    # Agent sessions v1 (internal)
    "GET /api/v1/agent/sessions/:param",

    # Global runs v1 (internal)
    "GET /api/v1/runs/:param",

    # -----------------------------------------------------------------
    # Admin / infrastructure domains — not exposed in public SDK
    # -----------------------------------------------------------------

    # Backups (admin — backup/restore operations)
    "GET /api/v1/backups",
    "GET /api/v1/backups/:param",
    "POST /api/v1/backups",
    "POST /api/v1/backups/:param/restore",
    "DELETE /api/v1/backups/:param",
    "GET /api/v1/backups/:param/download",

    # Chunks (internal extraction artifacts)
    "GET /api/chunks",
    "GET /api/chunks/:param",
    "PATCH /api/chunks/:param",
    "DELETE /api/chunks/:param",

    # Data sources (integration infra, configured via Integrations)
    "GET /api/datasources",
    "GET /api/datasources/:param",
    "POST /api/datasources",
    "PUT /api/datasources/:param",
    "DELETE /api/datasources/:param",
    "POST /api/datasources/:param/sync",

    # Discovery jobs (internal extraction pipeline)
    "GET /api/discovery-jobs",
    "GET /api/discovery-jobs/:param",
    "POST /api/discovery-jobs",
    "POST /api/discovery-jobs/:param/cancel",

    # Embedding policies (admin graph config)
    "GET /api/graph/embedding-policies",
    "GET /api/graph/embedding-policies/:param",
    "POST /api/graph/embedding-policies",
    "PATCH /api/graph/embedding-policies/:param",
    "DELETE /api/graph/embedding-policies/:param",

    # Integrations (admin connectors — GitHub, Notion, etc.)
    "GET /api/integrations",
    "GET /api/integrations/:param",
    "GET /api/integrations/:param/public",
    "GET /api/integrations/available",
    "POST /api/integrations",
    "PUT /api/integrations/:param",
    "DELETE /api/integrations/:param",
    "POST /api/integrations/:param/sync",
    "POST /api/integrations/:param/test",

    # Journal (graph notes — internal UI feature)
    "GET /api/graph/journal",
    "POST /api/graph/journal/notes",

    # MCP registry (admin — server-level MCP management)
    "GET /api/admin/mcp-servers",
    "GET /api/admin/mcp-servers/:param",
    "POST /api/admin/mcp-servers",
    "PATCH /api/admin/mcp-servers/:param",
    "DELETE /api/admin/mcp-servers/:param",
    "GET /api/admin/mcp-servers/:param/tools",
    "PATCH /api/admin/mcp-servers/:param/tools/:param",
    "POST /api/admin/mcp-servers/:param/inspect",
    "POST /api/admin/mcp-servers/:param/sync",
    "GET /api/admin/mcp-registry/search",
    "GET /api/admin/mcp-registry/servers/:param",
    "POST /api/admin/mcp-registry/install",
    "GET /api/admin/builtin-tools",
    "PATCH /api/admin/builtin-tools/:param",

    # Monitoring (admin — extraction job monitoring dashboard)
    "GET /api/monitoring/extraction-jobs",
    "GET /api/monitoring/extraction-jobs/:param",
    "GET /api/monitoring/extraction-jobs/:param/logs",
    "GET /api/monitoring/extraction-jobs/:param/llm-calls",

    # Org tool settings (admin)
    "GET /api/admin/orgs/:param/tool-settings",
    "PUT /api/admin/orgs/:param/tool-settings/:param",
    "DELETE /api/admin/orgs/:param/tool-settings/:param",

    # Provider management (admin — LLM provider credentials & usage)
    "GET /api/v1/organizations/:param/providers",
    "GET /api/v1/organizations/:param/providers/:param",
    "PUT /api/v1/organizations/:param/providers/:param",
    "DELETE /api/v1/organizations/:param/providers/:param",
    "GET /api/v1/organizations/:param/project-providers",
    "GET /api/v1/organizations/:param/usage",
    "GET /api/v1/organizations/:param/usage/by-project",
    "GET /api/v1/organizations/:param/usage/timeseries",
    "GET /api/v1/projects/:param/providers/:param",
    "PUT /api/v1/projects/:param/providers/:param",
    "DELETE /api/v1/projects/:param/providers/:param",
    "GET /api/v1/projects/:param/usage",
    "GET /api/v1/projects/:param/usage/timeseries",
    "GET /api/v1/providers/:param/models",
    "POST /api/v1/providers/:param/test",

    # Sandbox (v1 agent sandboxes — admin/infra)
    "GET /api/v1/agent/sandboxes",
    "GET /api/v1/agent/sandboxes/:param",
    "POST /api/v1/agent/sandboxes",
    "DELETE /api/v1/agent/sandboxes/:param",
    "GET /api/v1/agent/sandboxes/providers",
    "POST /api/v1/agent/sandboxes/:param/attach",
    "POST /api/v1/agent/sandboxes/:param/bash",
    "POST /api/v1/agent/sandboxes/:param/detach",
    "POST /api/v1/agent/sandboxes/:param/edit",
    "POST /api/v1/agent/sandboxes/:param/git",
    "POST /api/v1/agent/sandboxes/:param/glob",
    "POST /api/v1/agent/sandboxes/:param/grep",
    "POST /api/v1/agent/sandboxes/:param/read",
    "POST /api/v1/agent/sandboxes/:param/resume",
    "POST /api/v1/agent/sandboxes/:param/snapshot",
    "POST /api/v1/agent/sandboxes/:param/stop",
    "POST /api/v1/agent/sandboxes/:param/write",
    "POST /api/v1/agent/sandboxes/from-snapshot",

    # Sandbox images (admin)
    "GET /api/admin/sandbox-images",
    "GET /api/admin/sandbox-images/:param",
    "POST /api/admin/sandbox-images",
    "DELETE /api/admin/sandbox-images/:param",

    # Schema registry (admin — type registry management)
    "GET /api/schema-registry/projects/:param",
    "GET /api/schema-registry/projects/:param/stats",
    "GET /api/schema-registry/projects/:param/types/:param",
    "POST /api/schema-registry/projects/:param/types",
    "PUT /api/schema-registry/projects/:param/types/:param",
    "DELETE /api/schema-registry/projects/:param/types/:param",

    # Schemas (expanded — global/pack management, admin)
    "GET /api/schemas/:param",
    "POST /api/schemas",
    "PUT /api/schemas/:param",
    "DELETE /api/schemas/:param",
    "GET /api/schemas/projects/:param/available",
    "GET /api/schemas/projects/:param/installed",
    "GET /api/schemas/projects/:param/compiled-types",
    "GET /api/schemas/projects/:param/history",
    "POST /api/schemas/projects/:param/assign",
    "PATCH /api/schemas/projects/:param/assignments/:param",
    "DELETE /api/schemas/projects/:param/assignments/:param",
    "POST /api/schemas/projects/:param/migrate",
    "POST /api/schemas/projects/:param/migrate/preview",
    "POST /api/schemas/projects/:param/migrate/execute",
    "POST /api/schemas/projects/:param/migrate/commit",
    "POST /api/schemas/projects/:param/migrate/rollback",
    "GET /api/schemas/projects/:param/migration-jobs/:param",

    # Skills — global and org-level (admin; SDK covers project-level only)
    "GET /api/skills",
    "GET /api/skills/:param",
    "POST /api/skills",
    "PATCH /api/skills/:param",
    "DELETE /api/skills/:param",
    "GET /api/orgs/:param/skills",
    "POST /api/orgs/:param/skills",
    "PATCH /api/orgs/:param/skills/:param",
    "DELETE /api/orgs/:param/skills/:param",

    # Tracing (admin — trace search and detail)
    "GET /api/traces",
    "GET /api/traces/:param",
    "GET /api/traces/search",

    # Project members (admin UI)
    "GET /api/projects/:param/members",
    "DELETE /api/projects/:param/members/:param",

    # Document batch upload (bulk infra)
    "POST /api/documents/upload/batch",

    # Backups (admin — org/project backup & restore)
    "GET /api/v1/organizations/:param/backups",
    "GET /api/v1/organizations/:param/backups/:param",
    "GET /api/v1/organizations/:param/backups/:param/download",
    "DELETE /api/v1/organizations/:param/backups/:param",
    "POST /api/v1/projects/:param/backups",
    "POST /api/v1/projects/:param/restore",
    "GET /api/v1/projects/:param/restores/:param",

    # Chunking / chunks (internal extraction pipeline)
    "POST /api/documents/:param/recreate-chunks",
    "DELETE /api/chunks",
    "DELETE /api/chunks/by-document/:param",
    "DELETE /api/chunks/by-documents",

    # Data source integrations (admin — connector management)
    "GET /api/data-source-integrations",
    "GET /api/data-source-integrations/:param",
    "POST /api/data-source-integrations",
    "PATCH /api/data-source-integrations/:param",
    "DELETE /api/data-source-integrations/:param",
    "POST /api/data-source-integrations/:param/sync",
    "POST /api/data-source-integrations/:param/test-connection",
    "POST /api/data-source-integrations/test-config",
    "GET /api/data-source-integrations/:param/sync-jobs",
    "GET /api/data-source-integrations/:param/sync-jobs/:param",
    "GET /api/data-source-integrations/:param/sync-jobs/latest",
    "POST /api/data-source-integrations/:param/sync-jobs/:param/cancel",
    "GET /api/data-source-integrations/providers",
    "GET /api/data-source-integrations/providers/:param/schema",
    "GET /api/data-source-integrations/source-types",

    # Discovery jobs (internal extraction pipeline)
    "DELETE /api/discovery-jobs/:param",
    "GET /api/discovery-jobs/projects/:param",
    "POST /api/discovery-jobs/:param/finalize",
    "POST /api/discovery-jobs/projects/:param/start",

    # Agent definition overrides/sandbox (advanced admin config)
    "GET /api/projects/:param/agent-definitions/overrides",
    "GET /api/projects/:param/agent-definitions/overrides/:param",
    "PUT /api/projects/:param/agent-definitions/overrides/:param",
    "DELETE /api/projects/:param/agent-definitions/overrides/:param",
    "GET /api/projects/:param/agent-definitions/:param/sandbox-config",
    "PUT /api/projects/:param/agent-definitions/:param/sandbox-config",
}

# ---------------------------------------------------------------------------
# Server route extraction
# ---------------------------------------------------------------------------

@dataclass
class ServerRoute:
    method: str
    path: str
    domain: str
    handler: str


def _resolve_groups(routes_file: Path) -> Dict[str, str]:
    """Parse Go source to build a map of variable name -> prefix path."""
    text = routes_file.read_text()
    groups: Dict[str, str] = {}

    # Match: varName := something.Group("prefix")
    # We need to resolve chained groups like:
    #   g := e.Group("/api/graph")
    #   objects := g.Group("/objects")
    group_re = re.compile(
        r'(\w+)\s*:?=\s*(\w+)\.Group\(\s*"([^"]*)"\s*\)'
    )

    for match in group_re.finditer(text):
        var_name = match.group(1)
        parent_var = match.group(2)
        suffix = match.group(3)

        parent_prefix = groups.get(parent_var, "")
        groups[var_name] = parent_prefix + suffix

    return groups


def _extract_routes(routes_file: Path, domain: str) -> List[ServerRoute]:
    """Extract all registered routes from a single routes.go file."""
    text = routes_file.read_text()
    groups = _resolve_groups(routes_file)
    routes: List[ServerRoute] = []

    # Match: varName.METHOD("path", handler)
    route_re = re.compile(
        r'(\w+)\.(GET|POST|PUT|PATCH|DELETE)\(\s*"([^"]*)"'
        r'\s*,\s*\w+\.(\w+)'
    )

    for match in route_re.finditer(text):
        var_name = match.group(1)
        method = match.group(2)
        suffix = match.group(3)
        handler = match.group(4)

        prefix = groups.get(var_name, "")
        full_path = prefix + suffix
        routes.append(ServerRoute(method=method, path=full_path, domain=domain, handler=handler))

    # Also match direct e.METHOD calls (no group):
    # e.GET("/health", h.Health)
    direct_re = re.compile(
        r'\be\.(GET|POST|PUT|PATCH|DELETE)\(\s*"([^"]*)"'
        r'\s*,\s*\w+\.(\w+)'
    )
    for match in direct_re.finditer(text):
        method = match.group(1)
        path = match.group(2)
        handler = match.group(3)
        if not any(r.path == path and r.method == method for r in routes):
            routes.append(ServerRoute(method=method, path=path, domain=domain, handler=handler))

    return routes


def get_all_server_routes() -> List[ServerRoute]:
    """Scan all domain routes.go files and return every registered route."""
    all_routes: List[ServerRoute] = []
    for routes_file in sorted(SERVER_DOMAIN.glob("*/routes.go")):
        domain = routes_file.parent.name
        all_routes.extend(_extract_routes(routes_file, domain))
    return all_routes


# ---------------------------------------------------------------------------
# SDK endpoint extraction
# ---------------------------------------------------------------------------

@dataclass
class SDKEndpoint:
    method: str
    path_pattern: str  # normalized (param placeholders replaced)
    source_file: str
    line: int


def _find_project_path_return(text: str) -> str:
    """
    Parse a Python file to find its _project_path() return value.

    Different SDK sub-clients define _project_path() with different return
    values.  Most return ``/api/projects/:param`` but api_tokens.py returns
    ``/api/projects/:param/tokens``.  We parse the actual return statement
    to get the correct expansion for each file.

    Returns the normalized path, or the default '/api/projects/:param' if
    not found.
    """
    # Look for: return f"/api/projects/{quote(pid, safe='')}/extra"
    m = re.search(
        r'def _project_path\b.*?return\s+f"([^"]*)"',
        text, re.DOTALL
    )
    if m:
        raw = m.group(1)
        # Replace {anything} with :param
        return re.sub(r'\{[^}]+\}', ':param', raw)
    return "/api/projects/:param"


def _normalize_sdk_path(raw: str, project_path_expansion: str) -> str:
    """
    Convert SDK path strings to normalized patterns comparable with server routes.

    SDK paths use f-strings with variables; server routes use :param.
    We first expand _project_path() calls using the file-specific expansion,
    then replace remaining {anything} with :param.
    """
    # Expand {self._project_path(...)} calls with file-specific value
    result = re.sub(
        r'\{self\._project_path\([^)]*\)\}',
        project_path_expansion,
        raw,
    )
    # Replace remaining f-string expressions like {quote(object_id, safe='')}
    result = re.sub(r'\{[^}]+\}', ':param', result)
    # Strip trailing slashes for consistent comparison
    result = result.rstrip('/')
    return result


def _normalize_server_path(path: str) -> str:
    """Normalize server :paramName placeholders to :param for comparison."""
    return re.sub(r':[a-zA-Z]+', ':param', path)


def _preprocess_sdk_source(text: str) -> str:
    """
    Preprocess Python source to simplify regex matching.

    Handles:
    1. Multi-line calls: joins ``self._post(\\n  f"..."`` into one line
    2. Python implicit string concatenation: joins ``f"part1"\\n  f"part2"``
       into a single string ``f"part1part2"``
    3. ``self._http.request(\\n  "METHOD",...`` patterns
    """
    lines = text.split('\n')
    result: List[str] = []
    i = 0
    while i < len(lines):
        line = lines[i]
        stripped = line.rstrip()

        # Phase 1: join self._method(\n  f"..." or self._base + f"..." onto one line
        if i + 1 < len(lines):
            next_stripped = lines[i + 1].strip()
            next_is_string = (next_stripped.startswith('f"') or next_stripped.startswith('"'))
            next_is_base_string = next_stripped.startswith('self._base')
            # self._method( at end, next line starts with f" or " or self._base
            if (re.search(r'self\.(_get|_post|_patch|_put|_delete|_stream)\(\s*$', stripped)
                    and (next_is_string or next_is_base_string)):
                stripped = stripped + next_stripped
                i += 1  # consumed next line, will check for more below
            # self._http.method( at end, next has self._base + f"..."
            elif (re.search(r'self\._http\.(get|post|patch|put|delete)\(\s*$', stripped)
                    and (next_is_string or next_is_base_string)):
                stripped = stripped + next_stripped
                i += 1
            # self._http.request( at end, next has "METHOD" or self._base
            elif (re.search(r'self\._http\.request\(\s*$', stripped)
                    and (next_stripped.startswith('"') or next_is_base_string)):
                stripped = stripped + next_stripped
                i += 1
                # After joining "METHOD", check if next line has self._base
                if i + 1 < len(lines):
                    next2 = lines[i + 1].strip()
                    if next2.startswith('self._base'):
                        stripped = stripped + " " + next2
                        i += 1

        # Phase 2: join implicit Python string concatenation
        # Lines like:  f"part1"  (possibly with trailing whitespace)
        # followed by: f"part2"  (next f-string)
        # This handles patterns like:
        #   self._post(f"/api/projects/{...}/agents/{...}"
        #              f"/runs/{...}/cancel")
        while i + 1 < len(lines):
            next_stripped = lines[i + 1].strip()
            # Check if current line ends with a string and next line continues
            # Pattern: current ends with "<close-quote> and next starts with f" or "
            if (stripped.rstrip().rstrip(',').rstrip().endswith('"')
                    and (next_stripped.startswith('f"') or next_stripped.startswith('"'))):
                # Concatenate the string contents: remove trailing " and leading [f]"
                # e.g.: ..."/agents/{id}" + f"/runs/{rid}/cancel" →
                #        ..."/agents/{id}/runs/{rid}/cancel"
                # Find the last " on current line (before optional comma)
                cur = stripped.rstrip()
                has_comma = cur.endswith(',')
                if has_comma:
                    cur = cur[:-1].rstrip()
                if cur.endswith('"'):
                    # Remove the trailing quote
                    cur = cur[:-1]
                    # Remove the leading f" or " from next
                    nxt = next_stripped
                    if nxt.startswith('f"'):
                        nxt = nxt[2:]
                    elif nxt.startswith('"'):
                        nxt = nxt[1:]
                    # Join them
                    stripped = cur + nxt
                    if has_comma and not stripped.rstrip().endswith(','):
                        stripped = stripped  # comma was between strings, drop it
                    i += 1
                    continue
            break

        result.append(stripped)
        i += 1

    return '\n'.join(result)


def get_all_sdk_endpoints() -> List[SDKEndpoint]:
    """Scan SDK .py files and extract all HTTP calls."""
    endpoints: List[SDKEndpoint] = []

    method_map = {
        '_get': 'GET',
        '_post': 'POST',
        '_patch': 'PATCH',
        '_put': 'PUT',
        '_delete': 'DELETE',
        '_stream': 'POST',  # _stream is always POST
    }

    for py_file in sorted(SDK_SRC.glob("*.py")):
        if py_file.name.startswith('__'):
            continue
        raw_text = py_file.read_text()
        project_path_expansion = _find_project_path_return(raw_text)
        text = _preprocess_sdk_source(raw_text)
        lines = text.split('\n')

        for line_no, line in enumerate(lines, 1):
            # ---------------------------------------------------------
            # 1. Match self._get/self._post etc. (BaseClient helpers)
            #    Handles both f-strings and plain strings, including
            #    bare self._project_path() calls as the path argument.
            # ---------------------------------------------------------
            for method_name, http_method in method_map.items():
                # Pattern A: self._method(f"..." or self._method("..."
                pattern_str = re.compile(
                    rf'self\.{re.escape(method_name)}\(\s*f?"([^"]*)"'
                )
                for m in pattern_str.finditer(line):
                    raw_path = m.group(1)
                    norm = _normalize_sdk_path(raw_path, project_path_expansion)
                    endpoints.append(SDKEndpoint(
                        method=http_method,
                        path_pattern=norm,
                        source_file=py_file.name,
                        line=line_no,
                    ))

                # Pattern B: self._method(self._project_path(...), ...
                # (bare function call as path, not inside f-string)
                pattern_bare = re.compile(
                    rf'self\.{re.escape(method_name)}\(\s*self\._project_path\([^)]*\)\s*[,)]'
                )
                if pattern_bare.search(line):
                    # The path IS the project_path expansion itself
                    endpoints.append(SDKEndpoint(
                        method=http_method,
                        path_pattern=project_path_expansion,
                        source_file=py_file.name,
                        line=line_no,
                    ))

            # ---------------------------------------------------------
            # 2. Match self._http.get/post/etc. (direct httpx calls)
            #    Pattern: self._http.get(self._base + f"...", ...)
            #    or:      self._http.get(self._base + "...", ...)
            # ---------------------------------------------------------
            direct_http_re = re.compile(
                r'self\._http\.(get|post|patch|put|delete)\(\s*'
                r'self\._base\s*\+\s*f?"([^"]*)"'
            )
            for m in direct_http_re.finditer(line):
                http_method = m.group(1).upper()
                raw_path = m.group(2)
                norm = _normalize_sdk_path(raw_path, project_path_expansion)
                endpoints.append(SDKEndpoint(
                    method=http_method,
                    path_pattern=norm,
                    source_file=py_file.name,
                    line=line_no,
                ))

            # ---------------------------------------------------------
            # 3. Match self._http.request("METHOD", self._base + ...)
            # ---------------------------------------------------------
            req_pattern = re.compile(
                r'self\._http\.request\(\s*"(GET|POST|PUT|PATCH|DELETE)"'
                r'\s*,\s*self\._base\s*\+\s*f?"([^"]*)"'
            )
            for m in req_pattern.finditer(line):
                http_method = m.group(1)
                raw_path = m.group(2)
                norm = _normalize_sdk_path(raw_path, project_path_expansion)
                endpoints.append(SDKEndpoint(
                    method=http_method,
                    path_pattern=norm,
                    source_file=py_file.name,
                    line=line_no,
                ))

    return endpoints


# ---------------------------------------------------------------------------
# Comparison
# ---------------------------------------------------------------------------

def _route_key(method: str, path: str) -> str:
    return f"{method} {_normalize_server_path(path)}"


def _sdk_key(method: str, path: str) -> str:
    # path_pattern is already normalized by _normalize_sdk_path
    return f"{method} {path}"


def compare(
    server_routes: List[ServerRoute],
    sdk_endpoints: List[SDKEndpoint],
) -> Tuple[Set[str], Set[str], Set[str]]:
    """
    Returns (covered, gaps, stale):
      covered — server endpoints the SDK covers
      gaps    — server endpoints with no SDK coverage
      stale   — SDK endpoints that don't match any server route
    """
    server_keys: Dict[str, ServerRoute] = {}
    for r in server_routes:
        key = _route_key(r.method, r.path)
        server_keys[key] = r

    sdk_keys: Dict[str, SDKEndpoint] = {}
    for ep in sdk_endpoints:
        key = _sdk_key(ep.method, ep.path_pattern)
        sdk_keys[key] = ep

    server_set = set(server_keys.keys())
    sdk_set = set(sdk_keys.keys())

    # Normalize the EXCLUDED set the same way server keys are normalized
    excluded_normalized = set()
    for ex in EXCLUDED:
        parts = ex.split(" ", 1)
        if len(parts) == 2:
            excluded_normalized.add(f"{parts[0]} {_normalize_server_path(parts[1])}")
        else:
            excluded_normalized.add(ex)

    covered = (server_set & sdk_set) - excluded_normalized
    gaps = server_set - sdk_set - excluded_normalized
    stale = sdk_set - server_set

    return covered, gaps, stale


# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------

def _group_by_domain(keys: Set[str], routes: List[ServerRoute]) -> Dict[str, List[str]]:
    route_map = {}
    for r in routes:
        key = _route_key(r.method, r.path)
        route_map[key] = r

    grouped: Dict[str, List[str]] = {}
    for key in sorted(keys):
        r = route_map.get(key)
        domain = r.domain if r else "unknown"
        grouped.setdefault(domain, []).append(key)
    return grouped


def report(
    server_routes: List[ServerRoute],
    covered: Set[str],
    gaps: Set[str],
    stale: Set[str],
) -> bool:
    """Print the coverage report. Returns True if no gaps found."""
    total_server = len(set(_route_key(r.method, r.path) for r in server_routes))
    # Count unique normalized excluded that actually match server routes
    server_set = set(_route_key(r.method, r.path) for r in server_routes)
    excluded_normalized = set()
    for ex in EXCLUDED:
        parts = ex.split(" ", 1)
        if len(parts) == 2:
            excluded_normalized.add(f"{parts[0]} {_normalize_server_path(parts[1])}")
        else:
            excluded_normalized.add(ex)
    total_excluded = len(excluded_normalized & server_set)
    total_coverable = total_server - total_excluded
    pct = (len(covered) / total_coverable * 100) if total_coverable else 0

    print("=" * 70)
    print("Python SDK API Coverage Report")
    print("=" * 70)
    print()
    print(f"  Server endpoints:    {total_server}")
    print(f"  Excluded:            {total_excluded}")
    print(f"  Coverable:           {total_coverable}")
    print(f"  SDK covers:          {len(covered)}")
    print(f"  Gaps:                {len(gaps)}")
    print(f"  Stale:               {len(stale)}")
    print(f"  Coverage:            {pct:.1f}%")
    print()

    if covered:
        print("-" * 70)
        print(f"COVERED ({len(covered)} endpoints)")
        print("-" * 70)
        for domain, keys in sorted(_group_by_domain(covered, server_routes).items()):
            print(f"\n  [{domain}]")
            for key in keys:
                print(f"    {key}")
        print()

    if gaps:
        print("-" * 70)
        print(f"GAPS ({len(gaps)} endpoints NOT covered by SDK)")
        print("-" * 70)
        for domain, keys in sorted(_group_by_domain(gaps, server_routes).items()):
            print(f"\n  [{domain}]")
            for key in keys:
                print(f"    {key}")
        print()

    if stale:
        print("-" * 70)
        print(f"STALE ({len(stale)} SDK calls with no matching server route)")
        print("-" * 70)
        for key in sorted(stale):
            print(f"    {key}")
        print()

    if not gaps and not stale:
        print("All coverable endpoints are covered by the SDK. No stale calls.")
    elif not gaps:
        print("All coverable endpoints are covered by the SDK.")
        if stale:
            print(f"NOTE: {len(stale)} SDK call(s) don't match any server route (may be fine).")
    else:
        print(f"WARNING: {len(gaps)} endpoint(s) missing from the SDK.")
        print("Add them to the SDK or to the EXCLUDED set in this script.")

    print()
    return len(gaps) == 0


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> None:
    if not SERVER_DOMAIN.is_dir():
        print(f"ERROR: Server domain dir not found: {SERVER_DOMAIN}", file=sys.stderr)
        sys.exit(2)
    if not SDK_SRC.is_dir():
        print(f"ERROR: SDK source dir not found: {SDK_SRC}", file=sys.stderr)
        sys.exit(2)

    server_routes = get_all_server_routes()
    sdk_endpoints = get_all_sdk_endpoints()
    covered, gaps, stale = compare(server_routes, sdk_endpoints)
    ok = report(server_routes, covered, gaps, stale)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
