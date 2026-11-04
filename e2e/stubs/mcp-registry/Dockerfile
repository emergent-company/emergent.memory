# Stub MCP registry — hermetic impersonation of registry.modelcontextprotocol.io
# for the e2e harness. Multi-stage: build a static Go binary, run in minimal alpine.
#
# Build context: e2e/stubs/mcp-registry

FROM golang:1.25-alpine AS builder

WORKDIR /src

# Copy module + source and build a static, CGO-free binary.
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mcp-registry-stub .

FROM alpine:3.21

# busybox wget is used by the compose healthcheck
# (wget -qO- http://localhost:8080/healthz); ca-certificates keeps the base tidy.
RUN apk --no-cache add ca-certificates

COPY --from=builder /out/mcp-registry-stub /usr/local/bin/mcp-registry-stub

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/mcp-registry-stub"]
