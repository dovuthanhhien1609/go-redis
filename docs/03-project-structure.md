# Step 3 — Project Structure

---

## Repository Layout

```
go-redis/
├── cmd/
│   └── server/
│       └── main.go                 # Binary entry point
│
├── internal/
│   ├── config/
│   │   └── config.go               # Config struct, flag parsing, defaults
│   │
│   ├── logger/
│   │   └── logger.go               # Leveled structured logger (slog wrapper)
│   │
│   ├── protocol/
│   │   ├── parser.go               # RESP parser  (bytes → []string)
│   │   ├── serializer.go           # RESP serializer (Response → bytes)
│   │   ├── types.go                # Response type definitions
│   │   └── parser_test.go          # Parser unit tests
│   │
│   ├── storage/
│   │   ├── store.go                # Store interface definition
│   │   ├── memory.go               # In-memory RWMutex implementation
│   │   └── memory_test.go          # Storage unit tests
│   │
│   ├── persistence/
│   │   ├── aof.go                  # AOF writer + fsync policy
│   │   ├── replay.go               # AOF startup replay logic
│   │   └── aof_test.go             # Persistence tests
│   │
│   ├── commands/
│   │   ├── router.go               # Command registry + dispatch
│   │   ├── ping.go                 # PING handler
│   │   ├── string.go               # SET / GET / DEL / EXISTS / KEYS handlers
│   │   └── commands_test.go        # Command handler tests
│   │
│   └── server/
│       ├── server.go               # TCP listener, accept loop, shutdown
│       └── handler.go              # Per-connection read/parse/dispatch/write loop
│
├── docs/
│   ├── step-01-project-scope.md
│   ├── step-02-architecture.md
│   └── step-03-project-structure.md
│
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
└── README.md
```

---

## Folder Responsibilities

### `cmd/server/`
The binary entry point. `main.go` is intentionally thin — it wires together all
internal components and starts the server. No business logic lives here.

```
main.go does exactly 5 things:
  1. Load config
  2. Init logger
  3. Init storage
  4. Init AOF (+ replay if enabled)
  5. Start TCP server
```

### `internal/`
All packages here are private to this module (Go enforces this). This is where all
the real work happens.

| Package       | Single Responsibility                                     |
|---------------|-----------------------------------------------------------|
| `config`      | Parse and expose server settings                          |
| `logger`      | Provide a shared logger instance                          |
| `protocol`    | Encode and decode RESP wire format                        |
| `storage`     | Store and retrieve key-value pairs, thread-safely         |
| `persistence` | Append commands to disk; replay on startup                |
| `commands`    | Map command names to handler functions; execute them      |
| `server`      | Accept TCP connections; drive the per-client loop         |

### `internal/protocol/types.go`
Defines the `Response` type used throughout the system. Every command handler
returns a `Response`. The connection handler serializes it.

```go
type ResponseType int

const (
    SimpleString ResponseType = iota
    Error
    Integer
    BulkString
    NullBulkString
    Array
)

type Response struct {
    Type    ResponseType
    Str     string
    Integer int64
    Array   []Response
}
```

### `internal/storage/store.go`
Defines the `Store` interface. Commands depend on this interface, not the concrete
implementation. This makes it trivial to swap in a sharded store later, or use a
mock store in tests.

### `docs/`
One markdown file per step of this guide. Living documentation that grows
alongside the code.

---

## Why `internal/` for Everything?

Go's `internal/` directory is a compiler-enforced visibility boundary. Packages
under `internal/` can only be imported by code rooted at the parent of `internal/`.

- External packages **cannot** import `go-redis/internal/storage`
- This forces clean API design: any deliberate public API goes in `pkg/`
- Every package boundary is an intentional design decision

---

## Dependency Rules

```
cmd/server           → may import anything in internal/
internal/server      → imports: config, logger, protocol, commands
internal/commands    → imports: storage, persistence, protocol
internal/persistence → imports: storage, protocol, logger
internal/storage     → imports: nothing internal  (pure data structure)
internal/protocol    → imports: nothing internal  (pure encoding)
internal/config      → imports: nothing internal  (pure config)
internal/logger      → imports: nothing internal  (pure logging)
```

Lower layers never import upper layers. This is the layered architecture enforced
by dependency direction.
