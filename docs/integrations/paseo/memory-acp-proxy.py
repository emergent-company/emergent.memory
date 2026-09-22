#!/usr/bin/env python3
"""Temporary verification shim for SDK issue #761.

`apps/server/pkg/sdk/a2a` (and therefore `memory acp`) never sends the project
selector, so every A2A call fails with "project context is required". This
forward proxy injects `X-Project-ID` and relays the SSE body unbuffered, which
lets the full ACP -> A2A -> streaming path be exercised until the SDK fix lands.

The upstream target comes from $MEMORY_ACP_PROXY_TARGET (falling back to
MEMORY_ACP_PROXY_TARGET in the env file). It is required: the proxy refuses to
start without it so credentials are never forwarded to an unintended server.
The project id comes from $MEMORY_PROJECT_ID, else from the env file.

Usage: python3 memory-acp-proxy.py [port]   (default 18095, binds 127.0.0.1)
Env:   MEMORY_ACP_PROXY_TARGET (required, e.g. https://api.dev.emergent-company.ai)
       MEMORY_PROJECT_ID       (required, UUID)
       MEMORY_ACP_ENV_FILE     (optional, default ~/.memory/memory-acp.env)
"""
import http.client
import http.server
import os
import sys
import urllib.parse

ENV_FILE = os.path.expanduser(os.environ.get("MEMORY_ACP_ENV_FILE", "~/.memory/memory-acp.env"))
PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18095
HOP = {"host", "x-project-id", "content-length", "connection", "transfer-encoding"}


def env_file_value(key):
    try:
        with open(ENV_FILE) as fh:
            for line in fh:
                line = line.strip()
                if line.startswith(key + "="):
                    return line.split("=", 1)[1].strip().strip('"').strip("'")
    except OSError:
        pass
    return None


def lookup(key):
    return os.environ.get(key) or env_file_value(key)


def resolve_target():
    raw = lookup("MEMORY_ACP_PROXY_TARGET")
    if not raw:
        sys.exit(
            "memory-acp-proxy: MEMORY_ACP_PROXY_TARGET is not set and not found in "
            f"{ENV_FILE}. Set it to the real Memory server base URL, e.g. "
            "https://api.dev.emergent-company.ai — refusing to guess so credentials "
            "are never forwarded to an unintended server."
        )
    parsed = urllib.parse.urlsplit(raw if "://" in raw else "https://" + raw)
    if not parsed.hostname:
        sys.exit(f"memory-acp-proxy: MEMORY_ACP_PROXY_TARGET is not a valid URL: {raw!r}")
    scheme = parsed.scheme or "https"
    port = parsed.port or (443 if scheme == "https" else 80)
    authority = parsed.hostname if port in (80, 443) else f"{parsed.hostname}:{port}"
    return scheme, parsed.hostname, port, authority


SCHEME, TARGET_HOST, TARGET_PORT, TARGET_AUTHORITY = resolve_target()


def project_id():
    pid = lookup("MEMORY_PROJECT_ID")
    if pid:
        return pid
    sys.exit(
        "memory-acp-proxy: MEMORY_PROJECT_ID not set and not found in " + ENV_FILE
    )


PROJECT_ID = project_id()


def connect():
    if SCHEME == "https":
        return http.client.HTTPSConnection(TARGET_HOST, TARGET_PORT, timeout=600)
    return http.client.HTTPConnection(TARGET_HOST, TARGET_PORT, timeout=600)


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"
    server_version = "memory-acp-proxy/1.0"

    def log_message(self, fmt, *args):
        sys.stderr.write("memory-acp-proxy: " + (fmt % args) + "\n")
        sys.stderr.flush()

    def _proxy(self, method):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else None
        headers = {k: v for k, v in self.headers.items() if k.lower() not in HOP}
        headers["X-Project-ID"] = PROJECT_ID
        headers["Host"] = TARGET_AUTHORITY
        conn = connect()
        try:
            conn.request(method, self.path, body=body, headers=headers)
            resp = conn.getresponse()
        except Exception as exc:
            self.send_response(502)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(f"memory-acp-proxy upstream error: {exc}\n".encode())
            return
        self.send_response(resp.status)
        for k, v in resp.getheaders():
            if k.lower() in HOP:
                continue
            self.send_header(k, v)
        self.end_headers()
        while True:
            chunk = resp.read(4096)
            if not chunk:
                break
            try:
                self.wfile.write(chunk)
                self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                break
        conn.close()

    do_GET = lambda self: self._proxy("GET")  # noqa: E731
    do_POST = lambda self: self._proxy("POST")  # noqa: E731


class Server(http.server.ThreadingHTTPServer):
    daemon_threads = True


if __name__ == "__main__":
    srv = Server(("127.0.0.1", PORT), Handler)
    sys.stderr.write(
        f"memory-acp-proxy: 127.0.0.1:{PORT} -> {SCHEME}://{TARGET_AUTHORITY} "
        "(injecting X-Project-ID)\n"
    )
    sys.stderr.flush()
    srv.serve_forever()
