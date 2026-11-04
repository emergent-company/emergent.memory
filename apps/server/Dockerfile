# Dockerfile for Go Backend API — LOCAL DEV ONLY
# ⚠️  CI/CD uses deploy/self-hosted/Dockerfile.server instead.
# This file is kept for quick local builds (no CLI, no migration binary).
#
# Build context: REPOSITORY ROOT (.) - workspace-aware build
# Usage: docker build -f apps/server/Dockerfile .
#
# Multi-stage build:
# - Builder stage: Compiles Go binary with all dependencies
# - Runtime stage: Minimal distroless image with just the binary
#
# The resulting image is ~18MB (vs ~500MB for Node.js)

# Build arguments for version information
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

# Build stage
FROM golang:1.26-alpine AS builder

# Re-declare ARGs after FROM to use them in this stage
ARG VERSION
ARG GIT_COMMIT
ARG BUILD_TIME

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go.mod and go.sum first for dependency caching
COPY apps/server/go.mod apps/server/go.sum ./
# The go.mod replace directive points at ./pkg/sdk, so its module files must be
# present before the dependency graph can be resolved.
COPY apps/server/pkg/sdk/go.mod apps/server/pkg/sdk/go.sum ./pkg/sdk/

# Download dependencies (cached if go.mod/go.sum unchanged)
RUN go mod download

# Copy source code. `go mod download` above runs in -mod=mod, so it would
# silently write any hash the committed go.sum lacks; this COPY then restores
# the committed file, and the builds below run in the default -mod=readonly.
# go.sum is now complete (#2083), so a second download after this COPY is not
# needed. If go.sum ever drifts again, the first build here fails first.
COPY apps/server/ ./

# Install swag at the version pinned in go.mod, so the generated spec always
# matches the compiled swaggo/swag library.
RUN go install "github.com/swaggo/swag/cmd/swag@$(go list -m -f '{{.Version}}' github.com/swaggo/swag)"

# Remove symlink/directory and create real directory for swagger docs
RUN rm -rf docs && mkdir -p docs/swagger

# Generate OpenAPI/Swagger documentation
RUN /go/bin/swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal

# Build the binary
# CGO_ENABLED=0 for static binary (no libc dependency)
# -ldflags="-s -w" strips debug info for smaller binary
# -ldflags injects version info at build time
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w \
      -X github.com/emergent-company/emergent.memory/internal/version.Version=${VERSION} \
      -X github.com/emergent-company/emergent.memory/internal/version.GitCommit=${GIT_COMMIT} \
      -X github.com/emergent-company/emergent.memory/internal/version.BuildTime=${BUILD_TIME}" \
    -o /server \
    ./cmd/server

# Runtime stage - Alpine for minimal size + health check support
FROM alpine:3.19

RUN apk add --no-cache ca-certificates curl tzdata

COPY --from=builder /server /server
COPY apps/server/migrations/*.sql /migrations/
COPY --from=builder /build/docs/swagger /docs/swagger

RUN addgroup -g 65532 -S nonroot && \
    adduser -u 65532 -S nonroot -G nonroot

EXPOSE 3002

USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3002/health || exit 1

ENTRYPOINT ["/server"]
