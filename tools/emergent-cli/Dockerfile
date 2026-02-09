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
    -ldflags="-s -w -X github.com/emergent-company/emergent/tools/emergent-cli/internal/cmd.Version=${VERSION} -X github.com/emergent-company/emergent/tools/emergent-cli/internal/cmd.Commit=${GIT_COMMIT} -X github.com/emergent-company/emergent/tools/emergent-cli/internal/cmd.BuildDate=${BUILD_TIME}" \
    -o emergent-cli \
    ./cmd

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /build/emergent-cli /usr/local/bin/

ENTRYPOINT ["emergent-cli"]
CMD ["--help"]
