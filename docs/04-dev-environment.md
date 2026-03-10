# Step 4 — Development Environment

---

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.21+ | Compiler and toolchain (`log/slog` requires 1.21) |
| Docker | 24+ | Containerised development and deployment |
| docker compose | v2 | Multi-container orchestration |
| make | any | Convenience targets |
| redis-cli | any | Manual testing (optional) |

---

## Local Setup (no Docker)

```bash
# 1. Clone the repo
git clone https://github.com/hiendvt/go-redis
cd go-redis

# 2. Download dependencies
make deps

# 3. Run the server
make run

# 4. In another terminal, test with redis-cli
redis-cli -p 6379 PING
```

---

## Docker Setup

### Development mode

Uses `golang:1.24-alpine` directly. Your local source directory is mounted into
the container at `/app`, so code changes on the host are immediately visible
without rebuilding the image.

```bash
make docker-dev
# equivalent to:
# docker compose --profile dev up
```

The container runs `go run ./cmd/server`. To apply a code change, restart the
container:

```bash
docker compose --profile dev restart app
```

### Production mode

Builds the two-stage Docker image. The final image is based on `scratch` and
contains only the static binary (~10 MB total).

```bash
make docker-prod
# equivalent to:
# docker compose --profile prod up --build
```

### Stop all containers

```bash
make docker-down
```

---

## Dockerfile Design

```
┌──────────────────────────────────┐
│  Stage 1: builder                │
│  Image: golang:1.24-alpine       │
│                                  │
│  1. go mod download              │
│     (cached layer)               │
│  2. copy source                  │
│  3. go build → /app/go-redis     │
└──────────────┬───────────────────┘
               │  COPY --from=builder
               ▼
┌──────────────────────────────────┐
│  Stage 2: runtime                │
│  Image: scratch (empty)          │
│                                  │
│  Only contains:                  │
│  - /go-redis  (binary)           │
│  - CA certificates               │
└──────────────────────────────────┘
```

**Why two stages?**
The Go toolchain alone is ~300 MB. A production container should not carry build
tools it will never use. Two-stage builds produce a minimal attack surface and
fast image pulls.

**Why `scratch`?**
`scratch` is an empty base image — no shell, no package manager, no OS utilities.
Since our binary is compiled with `CGO_ENABLED=0`, it is fully self-contained and
needs nothing from the OS.

**Why copy `go.mod` before source?**
Docker builds layer by layer. If `go.mod` has not changed, the `go mod download`
layer is served from cache — saving significant time on every build.

---

## Makefile Targets

```
make build        compile binary to ./bin/go-redis
make run          go run ./cmd/server (no build step)
make test         run all tests
make test-verbose run all tests with -v
make test-race    run tests with race detector
make test-cover   generate HTML coverage report → coverage.html
make fmt          gofmt -w .
make vet          go vet ./...
make lint         golangci-lint run ./...
make deps         go mod download
make tidy         go mod tidy
make docker-dev   start dev server in Docker
make docker-prod  build and start production image
make docker-down  stop and remove all containers
make clean        remove ./bin, coverage files
make help         print this list
```

---

## Verifying the Setup

Once the server is running (either locally or in Docker):

```bash
# Basic connectivity
redis-cli -p 6379 PING
# Expected: PONG

# Or with raw telnet to see RESP on the wire
telnet localhost 6379
*1\r\n$4\r\nPING\r\n
# Expected: +PONG\r\n
```
