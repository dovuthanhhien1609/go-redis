# Step 7 — Command Handler

---

## Design Philosophy

The command system has one job: map a `[]string` to a `protocol.Response`.
Everything else — reading bytes, writing bytes, storing data — belongs to another layer.

Command handlers are **pure functions**. Given the same store state and arguments,
they always return the same response. No network, no goroutines, no file I/O.
That makes them trivial to unit-test.

---

## The Registry Pattern

The dispatcher is a `map[string]HandlerFunc`. Adding a new command is one line:

```go
r.handlers["SET"] = handleSet
```

| Approach | Problem |
|---|---|
| `switch/case` | Becomes a 200-line function; hard to extend |
| Interface per command | Too much boilerplate for simple functions |
| Plugin/reflection | Over-engineering |

A map of functions scales from 6 commands to 60 without structural changes.

---

## The `HandlerFunc` Signature

```go
type HandlerFunc func(args []string, store storage.Store) protocol.Response
```

- **`args[0]`** — always the command name (upper-cased before dispatch)
- **`storage.Store`** — the interface, never the concrete type
- **`protocol.Response`** — a value type; errors are `TypeError` responses, never panics

---

## Dispatch Flow

```
Dispatch(["set", "hello", "world"])
    │
    ├─ len check → ok
    ├─ ToUpper("set") → "SET"
    ├─ handlers["SET"] → handleSet (found)
    └─ handleSet(["set","hello","world"], store)
           │
           ├─ len(args) == 3 → ok
           ├─ store.Set("hello", "world")
           └─ return SimpleString("OK")
```

---

## Commands Implemented

| Command | Arity | Returns |
|---------|-------|---------|
| `PING [msg]` | 1–2 | `+PONG` or bulk echo |
| `SET key value` | 3 | `+OK` |
| `GET key` | 2 | bulk string or `$-1` |
| `DEL key [key …]` | 2+ | integer (deleted count) |
| `EXISTS key [key …]` | 2+ | integer (found count, duplicates count) |
| `KEYS pattern` | 2 | array of bulk strings |
| `COMMAND` | 1+ | empty array (satisfies redis-cli) |

---

## Testing Strategy

Command handlers are tested with a `mockStore` (plain map, no locks) rather than
`MemoryStore`. This keeps handler tests free of concurrency concerns and fast.

The `Router` dispatch tests use the real `MemoryStore` to verify the full path
from `Dispatch()` down to storage.

---

## Files

| File | Responsibility |
|---|---|
| `internal/commands/router.go` | `HandlerFunc` type, `Router`, `Dispatch()`, registry |
| `internal/commands/ping.go` | `PING` handler |
| `internal/commands/string.go` | `SET`, `GET`, `DEL`, `EXISTS`, `KEYS` handlers |
| `internal/commands/meta.go` | `COMMAND` handler (redis-cli compatibility) |
| `internal/commands/commands_test.go` | 29 tests: all handlers + router dispatch |
