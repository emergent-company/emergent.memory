# Build stage
FROM golang:1.24-alpine AS builder

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/emergent-company/emergent.memory/tools/cli/internal/cmd.Version=${VERSION} -X github.com/emergent-company/emergent.memory/tools/cli/internal/cmd.Commit=${GIT_COMMIT} -X github.com/emergent-company/emergent.memory/tools/cli/internal/cmd.BuildDate=${BUILD_TIME}" \
    -o memory \
    ./cmd

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /build/memory /usr/local/bin/

ENTRYPOINT ["memory"]
CMD ["--help"]
