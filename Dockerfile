# =============================================================================
# Stage 1: builder
# Full Go toolchain. Compiles the binary and runs tests.
# =============================================================================
FROM golang:1.24-alpine AS builder

# Install git (required by go mod for some dependencies) and ca-certificates
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy dependency files first so Docker can cache the module download layer.
# This layer is only invalidated when go.mod or go.sum change — not on every
# source code change.
COPY go.mod go.sum* ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary.
# -ldflags="-s -w" strips debug symbols → smaller binary
# CGO_ENABLED=0    → fully static binary, no libc dependency
# -o /app/go-redis → output path
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /app/go-redis \
    ./cmd/server

# =============================================================================
# Stage 2: runtime
# Minimal image. Only contains the compiled binary and TLS certificates.
# =============================================================================
FROM scratch AS runtime

# Copy TLS certs (needed if the server ever makes outbound HTTPS calls)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled binary from the builder stage
COPY --from=builder /app/go-redis /go-redis

# Redis default port
EXPOSE 6379

# Run the server
ENTRYPOINT ["/go-redis"]
