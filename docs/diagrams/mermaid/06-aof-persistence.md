# AOF Persistence

Two complementary flows: the **write path** (how commands are durably logged) and the **startup replay** (how the store is reconstructed from the log).

## Write Path Overview

```mermaid
flowchart TB
    CMD["Router.Dispatch(args)"]
    EXEC["Execute command\nagainst MemoryStore"]
    ERR{{"resp.Type\n== Error?"}}
    SKIP["skip AOF\n(failed commands\nnever logged)"]
    AOFCHECK{{"command\nmutates state?"}}
    READONLY["skip AOF\n(GET/KEYS/TTL…\nnever logged)"]
    TRANSFORM["appendToAOF()\n─────────────────\nApply AOF transform:\n• SETEX k t v\n  → SET k v\n     PEXPIREAT k <abs_ms>\n• PSETEX k t v\n  → SET k v + PEXPIREAT\n• EXPIRE / PEXPIRE\n  → PEXPIREAT k <abs_ms>\n• INCR/INCRBY/DECR\n  → SET k <final_value>\n• HINCRBY\n  → HSET k f <final_value>\n• SETNX (if set)\n  → SET k v\n• Others logged verbatim"]
    APPEND["AOF.Append(args)\n─────────────────\nSerialize as RESP array\nbufWriter.WriteString()\nbufWriter.Flush() → OS"]
    FSYNC{{"SyncPolicy?"}}
    ALWAYS["file.Sync() now\n→ physical disk\n(slowest, safest)"]
    EVERYSEC["syncLoop goroutine\nfile.Sync() every 1s\n(default — good balance)"]
    NO["never fsync\n(fastest, OS decides)"]
    RETURN["Return response\nto client"]

    CMD --> EXEC --> ERR
    ERR -->|yes| SKIP --> RETURN
    ERR -->|no| AOFCHECK
    AOFCHECK -->|read-only| READONLY --> RETURN
    AOFCHECK -->|mutating| TRANSFORM --> APPEND --> FSYNC
    FSYNC -->|always| ALWAYS --> RETURN
    FSYNC -->|everysec| EVERYSEC --> RETURN
    FSYNC -->|no| NO --> RETURN
```

## Why Transform?

```mermaid
flowchart LR
    subgraph PROBLEM["Problem: Relative TTLs break on restart"]
        P1["SETEX session 3600 tok\n(3600s from when?)"]
        P2["Server restarts 1800s later"]
        P3["Replay: SETEX session 3600 tok\n→ adds ANOTHER 3600s"]
        P1 --> P2 --> P3
    end

    subgraph SOLUTION["Solution: Convert to absolute timestamps"]
        S1["SETEX session 3600 tok\n→ AOF records:\n   SET session tok\n   PEXPIREAT session 1743764400000"]
        S2["Server restarts 1800s later"]
        S3["Replay PEXPIREAT 1743764400000:\n   remaining = 1800s  ✓"]
        S1 --> S2 --> S3
    end

    PROBLEM -.->|"Transform solves"| SOLUTION
```

## Startup Replay Flow

```mermaid
sequenceDiagram
    autonumber
    participant MAIN as main.go
    participant REPLAY as replay.go<br/>Replay()
    participant PARSER as protocol.Parser<br/>(reading AOF file)
    participant STORE as MemoryStore

    MAIN->>REPLAY: persistence.Replay(path, store)
    REPLAY->>STORE: store.Flush() — clean slate

    REPLAY->>REPLAY: os.Open(appendonly.aof)
    Note right of REPLAY: os.ErrNotExist → return 0, nil<br/>(fresh start, no error)

    loop until EOF
        REPLAY->>PARSER: parser.ReadCommand()
        PARSER-->>REPLAY: []string{cmd, args…}

        alt "SET"
            REPLAY->>STORE: store.Set(key, value)
        else "DEL"
            REPLAY->>STORE: store.Del(keys…)
        else "MSET"
            REPLAY->>STORE: store.Set(k1,v1), Set(k2,v2)…
        else "HSET / HMSET"
            REPLAY->>STORE: store.HSet(key, fields)
        else "HDEL"
            REPLAY->>STORE: store.HDel(key, fields…)
        else "LPUSH / RPUSH / SADD / ZADD…"
            REPLAY->>STORE: store.LPush/RPush/SAdd/ZAdd…
        else "PEXPIREAT"
            REPLAY->>REPLAY: remaining = time.Until(time.UnixMilli(absMs))
            alt remaining <= 0
                REPLAY->>STORE: store.Del(key) — already expired
            else remaining > 0
                REPLAY->>STORE: store.Expire(key, remaining)
            end
        else "PERSIST"
            REPLAY->>STORE: store.Persist(key)
        else "FLUSHDB / FLUSHALL"
            REPLAY->>STORE: store.Flush()
        end
    end

    REPLAY-->>MAIN: (replayed N, nil)
    MAIN->>MAIN: log "aof replay complete commands=N keys=M"
    MAIN->>MAIN: persistence.Open(path, policy)<br/>→ open for appending (not overwrite)
    MAIN->>MAIN: server.Run(ctx) → accept clients
```

## Fsync Policy Comparison

```mermaid
flowchart LR
    subgraph ALWAYS["always"]
        A1["file.Sync() on every\nAppend() call"]
        A2["Max durability:\nlose at most 1 command"]
        A3["Slowest:\nevery write waits for disk"]
        A1 --> A2 --> A3
    end

    subgraph EVERYSEC["everysec (default)"]
        E1["syncLoop goroutine\ncalls file.Sync()\nevery 1 second"]
        E2["Lose at most ~1s\nof writes on crash"]
        E3["Balanced:\nmost workloads"]
        E1 --> E2 --> E3
    end

    subgraph NO["no"]
        N1["OS decides when\nto flush page cache"]
        N2["May lose seconds\nof writes on crash"]
        N3["Fastest:\nmaximum throughput"]
        N1 --> N2 --> N3
    end
```

## AOF File Format (RESP arrays)

```
*3\r\n                   ← array of 3 elements
$3\r\n                   ← bulk string length 3
SET\r\n                  ← command name
$5\r\n
hello\r\n
$5\r\n
world\r\n

*3\r\n
$9\r\n
PEXPIREAT\r\n            ← always absolute ms timestamp
$5\r\n
hello\r\n
$13\r\n
1743768000000\r\n        ← Unix milliseconds

*4\r\n
$4\r\n
HSET\r\n
$6\r\n
user:1\r\n
$4\r\n
name\r\n
$3\r\n
bob\r\n
```
