# Dockerfile for Go Backend API
# Build context: REPOSITORY ROOT (.) - workspace-aware build
# Usage: docker build -f apps/server-go/Dockerfile .
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
FROM golang:1.24-alpine AS builder

# Re-declare ARGs after FROM to use them in this stage
ARG VERSION
ARG GIT_COMMIT
ARG BUILD_TIME

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go.mod and go.sum first for dependency caching
COPY apps/server-go/go.mod apps/server-go/go.sum ./

# Download dependencies (cached if go.mod/go.sum unchanged)
RUN go mod download

# Copy source code
COPY apps/server-go/ ./

# Install swag for OpenAPI spec generation
RUN go install github.com/swaggo/swag/cmd/swag@latest

# Remove symlink and create real directory for swagger docs
RUN rm -f docs && mkdir -p docs/swagger

# Generate OpenAPI/Swagger documentation
RUN /go/bin/swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal

# Build the binary
# CGO_ENABLED=0 for static binary (no libc dependency)
# -ldflags="-s -w" strips debug info for smaller binary
# -ldflags injects version info at build time
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w \
      -X github.com/emergent/emergent-core/internal/version.Version=${VERSION} \
      -X github.com/emergent/emergent-core/internal/version.GitCommit=${GIT_COMMIT} \
      -X github.com/emergent/emergent-core/internal/version.BuildTime=${BUILD_TIME}" \
    -o /server \
    ./cmd/server

# Runtime stage - Alpine for minimal size + health check support
FROM alpine:3.19

RUN apk add --no-cache ca-certificates curl tzdata

COPY --from=builder /server /server
COPY apps/server-go/migrations/*.sql /migrations/
COPY --from=builder /build/docs/swagger /docs/swagger

RUN addgroup -g 65532 -S nonroot && \
    adduser -u 65532 -S nonroot -G nonroot

EXPOSE 3002

USER nonroot:nonroot

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3002/health || exit 1

ENTRYPOINT ["/server"]
