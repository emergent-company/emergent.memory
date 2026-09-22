#!/usr/bin/env python3
"""Temporary verification shim for SDK issue #761.

`apps/server/pkg/sdk/a2a` (and therefore `memory acp`) never sends the project
selector, so every A2A call fails with "project context is required". This
forward proxy injects `X-Project-ID` and relays the SSE body unbuffered, which
lets the full ACP -> A2A -> streaming path be exercised until the SDK fix lands.

Project id is taken from $MEMORY_PROJECT_ID, else from ~/.memory/memory-acp.env.

Usage: python3 memory-acp-proxy.py [port]   (default 18095, binds 127.0.0.1)
"""
import http.client
import http.server
import os
import sys

TARGET_HOST = os.environ.get("MEMORY_ACP_PROXY_TARGET", "api.dev.emergent-company.ai")
TARGET_PORT = int(os.environ.get("MEMORY_ACP_PROXY_TARGET_PORT", "443"))
PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18095
HOP = {"host", "x-project-id", "content-length", "connection", "transfer-encoding"}


def project_id():
    pid = os.environ.get("MEMORY_PROJECT_ID")
    if pid:
        return pid
    env_file = os.path.expanduser("~/.memory/memory-acp.env")
    try:
        with open(env_file) as fh:
            for line in fh:
                line = line.strip()
                if line.startswith("MEMORY_PROJECT_ID="):
                    return line.split("=", 1)[1].strip()
    except OSError:
        pass
    sys.exit("memory-acp-proxy: MEMORY_PROJECT_ID not set and not found in ~/.memory/memory-acp.env")


PROJECT_ID = project_id()


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
        headers["Host"] = TARGET_HOST
        conn = http.client.HTTPSConnection(TARGET_HOST, TARGET_PORT, timeout=600)
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
        f"memory-acp-proxy: 127.0.0.1:{PORT} -> https://{TARGET_HOST} (injecting X-Project-ID)\n"
    )
    sys.stderr.flush()
    srv.serve_forever()
