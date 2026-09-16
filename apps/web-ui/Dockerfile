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
# templ version pinned to gateway/go.mod (a-h/templ v0.3.1020)
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1020
# Node 22 (Tailwind v4 CLI requires Node >= 20; Debian bookworm's apt node is 18).
# xz-utils provides `xz`, required by `tar -xJ` to extract the Node tarball
# (the golang base image does not ship it).
RUN apt-get update \
 && apt-get install -y --no-install-recommends xz-utils \
 && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://nodejs.org/dist/v22.14.0/node-v22.14.0-linux-x64.tar.xz \
      | tar -xJ -C /usr/local --strip-components=1
WORKDIR /src
# Build-time version metadata, injected into the binary via -X ldflags by CI
# (.github/workflows/build-publish.yml). Defaults keep manual/local builds
# working. The gateway package lives at the module root of the built binary
# (go build . here), so its package is `main`: -X main.version/-X main.commit.
ARG VERSION=dev
ARG GIT_SHA=unknown
# Manifest + lock first so `npm ci` is a cacheable layer independent of source.
COPY gateway/package.json gateway/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
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

# Go binary
COPY --from=build /out/memory /usr/local/bin/memory

# Python voice bridge
COPY memory_bridge/ ./memory_bridge/
RUN pip install --no-cache-dir \
    livekit-agents>=1.6.0 \
    livekit-plugins-deepgram>=1.6.0 \
    livekit-plugins-cartesia>=1.6.0 \
    livekit-plugins-silero>=1.6.0 \
    livekit-plugins-turn-detector>=1.6.0 \
    httpx>=0.27.0 \
    python-dotenv>=1.0.0

ENV BRIDGE_BIN=python \
    BRIDGE_ARGS="-m memory_bridge" \
    BRIDGE_WORKDIR=/app

EXPOSE 8080
CMD ["memory"]
