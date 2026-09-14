# Memory — Go gateway (supervisor + web UI) + Python voice bridge, one container.
# Connects to external memory / LiveKit / LiteLLM / Deepgram / Cartesia.
#
# templ-generated Go files are gitignored, so the builder stage must run
# `templ generate` before `go build`. go-daisy is a published module
# (github.com/emergent-company/go-daisy), so `go mod download` resolves it.

# ---- Stage 1: build the Go binary ----
FROM golang:1.26 AS build
# templ version pinned to gateway/go.mod (a-h/templ v0.3.1020)
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1020
WORKDIR /src
# Build-time version metadata, injected into the binary via -X ldflags by CI
# (.github/workflows/build-publish.yml). Defaults keep manual/local builds
# working. The gateway package lives at the module root of the built binary
# (go build . here), so its package is `main`: -X main.version/-X main.commit.
ARG VERSION=dev
ARG GIT_SHA=unknown
COPY gateway/ ./
RUN templ generate ./... && CGO_ENABLED=0 go build \
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
