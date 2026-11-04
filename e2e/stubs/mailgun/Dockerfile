# Stub Mailgun API — hermetic impersonation of api.mailgun.net/v3 for the e2e
# harness. Multi-stage: build a static Go binary, run in minimal alpine.
#
# Build context: e2e/stubs/mailgun

FROM golang:1.25-alpine AS builder

WORKDIR /src

# Copy module + source and build a static, CGO-free binary.
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mailgun-stub .

FROM alpine:3.21

# busybox wget is used by the compose healthcheck
# (wget -qO- http://localhost:8080/healthz); ca-certificates keeps the base tidy.
RUN apk --no-cache add ca-certificates

COPY --from=builder /out/mailgun-stub /usr/local/bin/mailgun-stub

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/mailgun-stub"]
