# go-redis

An educational Redis clone built in Go to deeply understand systems programming,
networking, storage engines, and database architecture.

> This is not a production Redis replacement. It is a well-architected, step-by-step
> implementation of the core Redis subsystems for learning purposes.

---

## Features (Phase 1)

- TCP server on port 6379
- RESP v2 protocol parser and serializer
- Commands: `PING`, `SET`, `GET`, `DEL`, `EXISTS`, `KEYS`
- Thread-safe in-memory key-value store
- Append-Only File (AOF) persistence with startup replay

## Planned Extensions (Phase 2+)

- Key expiration (TTL / EXPIRE)
- RDB snapshot persistence
- Pub/Sub
- Replication (leader/follower)
- Sharded storage

---

## Quick Start

### Local (requires Go 1.21+)

```bash
make run
```

### Docker (development — live source mount)

```bash
make docker-dev
```

### Docker (production — optimised image)

```bash
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
```

---

## Development

```bash
make build        # compile binary to ./bin/
make test         # run all tests
make test-race    # run tests with race detector
make test-cover   # generate HTML coverage report
make fmt          # format source files
make vet          # run go vet
make clean        # remove build artifacts
make help         # list all targets
```

---

## Documentation

Step-by-step design and implementation notes live in [`docs/`](docs/).

| File | Content |
|------|---------|
| [step-01-project-scope.md](docs/step-01-project-scope.md) | What Redis is, scope, learning objectives |
| [step-02-architecture.md](docs/step-02-architecture.md) | System architecture and data flow |
| [step-03-project-structure.md](docs/step-03-project-structure.md) | Repository layout and dependency rules |
| [step-04-dev-environment.md](docs/step-04-dev-environment.md) | Docker, Makefile, local setup |

---

## Architecture Overview

```
Client → TCP Server → RESP Parser → Command Router → Store → AOF → Response
```

See [step-02-architecture.md](docs/step-02-architecture.md) for the full diagram.
