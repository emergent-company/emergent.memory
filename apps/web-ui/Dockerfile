# Memory — Go gateway (supervisor + web UI) + Python voice bridge, one container.
# Connects to external memory / LiveKit / LiteLLM / Deepgram / Cartesia.
#
# templ-generated Go files AND the compiled Tailwind CSS (webui/static/css/app.css)
# are gitignored, so the builder stage must run `templ generate` and the Tailwind
# build before `go build`. go-daisy is a published module
# (github.com/emergent-company/go-daisy), so `go mod vendor` resolves it and
# supplies the component source + custom.css the Tailwind build @sources/@imports.

# ---- Stage 1: build the Go binary ----
FROM golang:1.26 AS build
# Node 22 (Tailwind v4 CLI requires Node >= 20; Debian bookworm's apt node is 18).
# xz-utils provides `xz`, required by `tar -xJ` to extract the Node tarball
# (the golang base image does not ship it).
# These come before the module manifest so a go.mod change does not invalidate
# the Node install layer.
RUN apt-get update \
 && apt-get install -y --no-install-recommends xz-utils \
 && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://nodejs.org/dist/v22.14.0/node-v22.14.0-linux-x64.tar.xz \
      | tar -xJ -C /usr/local --strip-components=1
WORKDIR /src
# Manifest + lock first so `npm ci` is a cacheable layer independent of source.
COPY gateway/package.json gateway/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
# The templ CLI version is derived from gateway/go.mod instead of a duplicated
# literal, so the image cannot drift from the version CI and the gateway build
# resolve against (#2314). Copying the manifest before the install makes this
# layer depend on go.mod/go.sum; that is the correct trade — a dependency change
# should rebuild the tool.
COPY gateway/go.mod gateway/go.sum ./
RUN go install "github.com/a-h/templ/cmd/templ@$(go list -m -f '{{.Version}}' github.com/a-h/templ)"
# Build-time version metadata, injected into the binary via -X ldflags by CI
# (.github/workflows/build-publish.yml). Defaults keep manual/local builds
# working. The gateway package lives at the module root of the built binary
# (go build . here), so its package is `main`: -X main.version/-X main.commit.
ARG VERSION=dev
ARG GIT_SHA=unknown
COPY gateway/ ./
# Order matters: templ generate (CSS @sources generated *_templ.go) -> vendor
# (CSS @sources/@imports go-daisy) -> Tailwind compile -> go build (embeds
# webui/static via go:embed, which FAILS if app.css is missing).
RUN templ generate ./... \
 && GOWORK=off go mod vendor \
 && node_modules/.bin/tailwindcss -i webui/css/app.css -o webui/static/css/app.css --minify \
 && CGO_ENABLED=0 go build \
    -ldflags "-X main.version=${VERSION} -X main.commit=${GIT_SHA}" \
    -o /out/memory .

# ---- Stage 2: runtime (Python bridge + Go binary) ----
FROM python:3.14-slim AS runtime
WORKDIR /app

# The bridge's Python dependency set comes from pyproject.toml + uv.lock — the
# same two files Dependabot audits. `--locked` asserts the lock is up to date
# with pyproject.toml and fails the build otherwise; it never re-resolves or
# silently installs from a stale lock (`--frozen` would use the lock without
# checking pyproject at all). `--no-install-project` leaves the bridge package
# itself to the COPY below (it is imported from WORKDIR, not installed). uv is
# pinned so the resolver is reproducible too.
COPY --from=ghcr.io/astral-sh/uv:0.13.6 /uv /uvx /bin/
COPY pyproject.toml uv.lock ./
RUN uv sync --locked --no-dev --no-install-project --no-cache

# Go binary
COPY --from=build /out/memory /usr/local/bin/memory

# Python voice bridge
COPY memory_bridge/ ./memory_bridge/

# Use the uv-managed interpreter explicitly: it is the one that has the locked
# dependency set. PATH keeps the venv first for any tool the supervisor runs.
ENV PATH="/app/.venv/bin:$PATH" \
    BRIDGE_BIN=/app/.venv/bin/python \
    BRIDGE_ARGS="-m memory_bridge start" \
    BRIDGE_WORKDIR=/app

EXPOSE 8080
CMD ["memory"]
