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

# Runtime stage - using distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

# Copy timezone data for time.LoadLocation
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /server /server

# Copy migrations (for Goose)
COPY apps/server-go/migrations/*.sql /migrations/

# Expose port
EXPOSE 3002

# Run as non-root user (distroless nonroot has uid 65532)
USER nonroot:nonroot

# Start application
ENTRYPOINT ["/server"]
