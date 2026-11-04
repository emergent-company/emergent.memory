# Stub Gmail API — hermetic impersonation of Gmail v1 (thread listing, thread
# fetch, labels, profile) plus the OAuth2 refresh grant, for the e2e harness.
# Multi-stage: build a static Go binary, run in a minimal alpine image.
#
# Build context: e2e/stubs/gmail

FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gmail-stub .

FROM alpine:3.21

# busybox wget is used by the compose healthcheck
# (wget -qO- http://localhost:8080/healthz); ca-certificates keeps the base tidy.
RUN apk --no-cache add ca-certificates

COPY --from=builder /out/gmail-stub /usr/local/bin/gmail-stub

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/gmail-stub"]
