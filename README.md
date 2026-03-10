# go-redis

An educational Redis clone built in Go to deeply understand systems programming,
networking, storage engines, and database architecture.

> This is not a production Redis replacement. It is a well-architected, step-by-step
> implementation of the core Redis subsystems for learning purposes.

---

## Features

- TCP server on port 6379
- RESP v2 protocol — full parser and serializer
- Commands: `PING`, `SET`, `GET`, `DEL`, `EXISTS`, `KEYS`
- Thread-safe in-memory key-value store (`sync.RWMutex`)
- Append-Only File (AOF) persistence with startup replay
- Graceful shutdown (SIGTERM / SIGINT)

## Planned Extensions

- Key expiration (`TTL` / `EXPIRE`)
- RDB snapshot persistence
- Pub/Sub
- Replication (leader/follower)
- Sharded storage

---

## Quick Start

### Requirements

- Go 1.24+ **or** Docker

### Run locally

```bash
make run
```

### Run with Docker

```bash
# Development — live source mount
make docker-dev

# Production — optimised scratch image
make docker-prod
```

---

## Connect

Once the server is running, connect with `redis-cli` or `telnet`:

```bash
redis-cli -p 6379 PING
# → PONG

redis-cli -p 6379 SET hello world
# → OK

redis-cli -p 6379 GET hello
# → "world"

redis-cli -p 6379 DEL hello
# → (integer) 1

redis-cli -p 6379 KEYS "*"
# → (empty array)
```

---

## Development

```bash
make build          # compile binary to ./bin/go-redis
make run            # run with go run (no build step)
make test           # run all tests
make test-verbose   # run tests with verbose output
make test-race      # run tests with race detector
make test-cover     # generate HTML coverage report → coverage.html
make fmt            # format source files
make vet            # run go vet
make lint           # run golangci-lint (must be installed separately)
make tidy           # tidy go.mod and go.sum
make docker-dev     # start dev server in Docker
make docker-prod    # build and start production image
make docker-down    # stop and remove containers
make clean          # remove build artifacts
make help           # list all targets
```

---

## Documentation

Step-by-step design and implementation notes live in [`docs/`](docs/).

| Step | File | Content |
|------|------|---------|
| 01 | [01-project-scope.md](docs/01-project-scope.md) | What Redis is, scope, learning objectives |
| 02 | [02-architecture.md](docs/02-architecture.md) | System architecture, data flow, dependency graph |
| 03 | [03-project-structure.md](docs/03-project-structure.md) | Repository layout and dependency rules |
| 04 | [04-dev-environment.md](docs/04-dev-environment.md) | Docker, Makefile, local setup |
| 05 | [05-resp-protocol.md](docs/05-resp-protocol.md) | RESP v2 wire format, parser, serializer |
| 06 | [06-tcp-server.md](docs/06-tcp-server.md) | TCP listener, goroutine-per-connection model |
| 07 | [07-command-handler.md](docs/07-command-handler.md) | Command router, handler functions |
| 08 | [08-storage.md](docs/08-storage.md) | In-memory store, RWMutex concurrency |
| 09 | [09-persistence-aof.md](docs/09-persistence-aof.md) | AOF write path, fsync policies, replay |
| 10 | [10-request-flow.md](docs/10-request-flow.md) | End-to-end request walkthrough with diagrams |
| 11 | [11-testing.md](docs/11-testing.md) | Integration tests, test patterns |

---

## Architecture

```
Client
  │  RESP request
  ▼
TCP Server ──► Connection Handler
                    │               │               │
                    ▼               ▼               ▼
               RESP Parser    Command Router   RESP Serializer
                                   │
                          ┌────────┴────────┐
                          ▼                 ▼
                      MemoryStore       AOF Writer
```

See [02-architecture.md](docs/02-architecture.md) for the full layered diagram and dependency graph.
