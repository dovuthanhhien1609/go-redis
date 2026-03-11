# Step 9 — AOF Persistence

---

## What is an Append-Only File?

AOF is a **write-ahead log** — every mutating command is serialized and appended
to a file on disk *after* it executes successfully in memory. On startup, the server
replays the file from top to bottom to reconstruct the in-memory state.

Same pattern as: PostgreSQL WAL, Kafka log segments, etcd Raft log.

---

## AOF File Format

Commands are stored as back-to-back RESP arrays — the same encoding the client sends.

```
*3\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n
*2\r\n$3\r\nDEL\r\n$5\r\nhello\r\n
```

Reuses the existing protocol layer. Human-readable. Trivially parseable.

---

## fsync Policy

Writing to a file calls `write(2)` — data enters the OS page cache, not disk.
`fsync(2)` forces the cache to disk.

| Policy | When | Durability | Throughput |
|---|---|---|---|
| `always` | After every command | Max — 0 commands lost | Lowest |
| `everysec` | Background goroutine, 1/sec | Good — ≤1 sec of writes lost | High |
| `no` | Never | Weakest — OS decides (~30s) | Highest |

Default: `everysec` — same as Redis.

---

## Import Cycle Prevention

```
persistence → storage   (replay calls store.Set/Del directly)
persistence → protocol  (replay uses the RESP parser)
commands    → storage
commands    → protocol
```

If `commands → persistence` AND `persistence → commands` → **cycle**.

Solution: `commands` defines a one-method `Appender` interface.
`*persistence.AOF` satisfies it. `main.go` wires them.

```go
// In commands/router.go — no persistence import needed
type Appender interface {
    Append(args []string) error
}
```

---

## What Gets Logged

Only **mutating** commands that **succeed**. The router's `appendToAOF` method handles
two concerns: (1) which commands write to the AOF, and (2) **what form** they are written in.

### Direct records (logged verbatim)

| Command | AOF record |
|---------|------------|
| `SET key value` | `SET key value` |
| `DEL key [key ...]` | `DEL key [key ...]` |
| `MSET k v [k v ...]` | `MSET k v [k v ...]` |
| `HSET key f v [f v ...]` | `HSET key f v [f v ...]` |
| `HDEL key field [field ...]` | `HDEL key field [field ...]` |
| `FLUSHDB` / `FLUSHALL` | as-is |
| `PERSIST key` | `PERSIST key` |

### Transformed records

Some commands carry **relative time** or need the **final computed value**. Storing them
verbatim would produce incorrect state on replay. Instead:

| Command | AOF records written |
|---------|---------------------|
| `SETEX key 10 value` | `SET key value` + `PEXPIREAT key <abs_ms>` |
| `PSETEX key 5000 value` | `SET key value` + `PEXPIREAT key <abs_ms>` |
| `EXPIRE key 30` | `PEXPIREAT key <abs_ms>` |
| `PEXPIRE key 5000` | `PEXPIREAT key <abs_ms>` |
| `SETNX key value` (set) | `SET key value` |
| `GETSET key value` | `SET key value` |
| `GETDEL key` (existed) | `DEL key` |
| `INCR key` → new value N | `SET key N` |
| `INCRBY key delta` → N | `SET key N` |
| `DECR key` → N | `SET key N` |
| `DECRBY key delta` → N | `SET key N` |
| `HINCRBY key field delta` → N | `HSET key field N` |
| `RENAME src dst` | `SET dst value` (or `HSET dst …`) + `DEL src` |

### Why `PEXPIREAT`?

`EXPIRE key 30` is relative to the moment it was executed. If the server restarts
after 20 seconds, on replay we would wrongly apply another 30 seconds. By converting
to `PEXPIREAT key <unix_ms>` (absolute timestamp), replay can compute the exact
remaining TTL:

```
remaining = abs_ms - now_ms
if remaining > 0: store.Expire(key, remaining)
else:             store.Del(key)   // already expired
```

Read-only commands (`GET`, `PING`, `EXISTS`, `KEYS`, …) are never written.
Failed commands (`TypeError` responses) are never written.


---

## Replay Logic

> PlantUML source: [`docs/diagrams/aof-flow.puml`](diagrams/aof-flow.puml)

**Write Path — per command:**

```mermaid
flowchart TD
    A["Router.Dispatch(args)"] --> B["Execute against Store"]
    B --> C{Response\nis Error?}
    C -->|Yes — never log| Z["Return Response"]
    C -->|No| D{Mutating?\nSET / DEL}
    D -->|No — read-only| Z
    D -->|Yes| E["AOF.Append(args)\nbufWriter.Flush → OS page cache"]
    E --> F{fsync policy}
    F -->|always| G["file.Sync() → disk"]
    F -->|everysec| H["syncLoop goroutine\nSync() every 1 s"]
    F -->|no| I["OS decides\n~30 s"]
    G --> Z
    H --> Z
    I --> Z
```

**Startup Replay:**

```mermaid
flowchart TD
    A([Server starts]) --> B["store.Flush() — clean slate"]
    B --> C["Open AOF for reading"]
    C --> D{ReadCommand}
    D -->|SET/MSET/DEL| E["store.Set / store.Del"]
    D -->|HSET/HDEL| F["store.HSet / store.HDel"]
    D -->|PEXPIREAT| P["compute remaining TTL\nExpire or Del"]
    D -->|PERSIST| Q["store.Persist"]
    D -->|FLUSHDB| R["store.Flush"]
    D -->|EOF| G["Log: replay complete\ncommands=N, keys=M"]
    E --> D
    F --> D
    P --> D
    Q --> D
    R --> D
    G --> H["persistence.Open for appending"]
    H --> I["store.StartCleanup(ctx)"]
    I --> J["server.Run — accept clients"]
    J --> K([Ready])
```

Replay bypasses the Router — no further AOF writes, no import cycle.

---

## End-to-End Verification

```
Server 1 writes:
  SET name alice   → AOF: *3\r\n$3\r\nSET\r\n$4\r\nname\r\n$5\r\nalice\r\n
  SET city bangkok → AOF: *3\r\n$3\r\nSET\r\n$4\r\ncity\r\n$7\r\nbangkok\r\n
  DEL city         → AOF: *2\r\n$3\r\nDEL\r\n$4\r\ncity\r\n

Server 2 startup log:
  {"msg":"aof replay complete","commands":3,"keys":1}

Server 2 queries:
  GET name → "alice"    ✓
  GET city → (nil)      ✓  (DEL was replayed)
```

---

## Files

| File | Responsibility |
|---|---|
| `internal/persistence/aof.go` | `AOF` writer, `bufio` buffer, fsync policies, `syncLoop` goroutine |
| `internal/persistence/replay.go` | `Replay()` — parse AOF and apply directly to store |
| `internal/persistence/aof_test.go` | 10 tests: write, replay, round-trip, flush-first, sync-always |
| `internal/commands/router.go` | `Appender` interface, `mutatingCommands` set, AOF call in `Dispatch` |
| `cmd/server/main.go` | `setupAOF()` — replay then open; wires into Router |
