# Stub website — hermetic stand-in for a small crawlable site, for the e2e
# harness. Multi-stage: build a static Go binary, run in a minimal alpine image.
#
# Build context: e2e/stubs/web

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/web-stub .

FROM alpine:3.21

# busybox wget is used by the compose healthcheck
# (wget -qO- http://localhost:8080/healthz); ca-certificates keeps the base tidy.
RUN apk --no-cache add ca-certificates

COPY --from=builder /out/web-stub /usr/local/bin/web-stub

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/web-stub"]
