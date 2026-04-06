# Transaction Lifecycle

How `MULTI`, `EXEC`, `DISCARD`, and `WATCH` work inside `handler.go` — all transaction state is per-connection.

## Connection State Machine

```mermaid
stateDiagram-v2
    [*] --> Normal : connection accepted

    Normal --> Normal : regular commands\n(dispatched immediately)
    Normal --> Watching : WATCH key [key …]\n(saves keyVersion snapshot)
    Normal --> Multi : MULTI
    Watching --> Multi : MULTI\n(versions still tracked)
    Watching --> Normal : UNWATCH\nor EXEC / DISCARD

    Multi --> Multi : any command\n→ reply QUEUED\n(added to txQueue)
    Multi --> Multi : MULTI\n→ ERR MULTI calls can not be nested
    Multi --> ExecDone : EXEC
    Multi --> Normal : DISCARD\n(clear queue + watches)

    ExecDone --> Normal : (implicit)\nwatchedKeys cleared

    Normal --> [*] : client disconnects
    Multi --> [*] : client disconnects\n(queue abandoned)
```

## WATCH + EXEC — Optimistic Locking

```mermaid
sequenceDiagram
    autonumber
    actor C as Client
    participant H as handler.go
    participant R as router.go
    participant MS as MemoryStore

    C->>H: WATCH account_balance
    H->>H: handleWatch(["account_balance"])
    H->>R: router.KeyVersion("account_balance")
    R->>MS: store.Version("account_balance")
    MS-->>R: 5
    R-->>H: 5
    H->>H: watchedKeys["account_balance"] = 5
    H-->>C: +OK

    Note over C,MS: Client reads balance, computes new value

    C->>H: MULTI
    H->>H: inMulti=true, txQueue=[], txErrors=0
    H-->>C: +OK

    C->>H: SET account_balance 900
    H->>H: inMulti=true → append to txQueue
    H-->>C: +QUEUED

    Note over MS: Another client modifies account_balance!
    MS->>MS: keyVersion["account_balance"] = 6

    C->>H: EXEC
    H->>H: handleExec()
    H->>H: watchDirty()?<br/>KeyVersion("account_balance") == 6 ≠ 5 → DIRTY!
    H-->>C: *-1\r\n   ← null array = transaction aborted

    Note over C: Client retries: WATCH → read → MULTI → SET → EXEC
```

## EXEC — Happy Path (no conflicts)

```mermaid
sequenceDiagram
    autonumber
    actor C as Client
    participant H as handler.go
    participant R as router.go

    C->>H: MULTI
    H-->>C: +OK

    C->>H: SET k1 v1
    H-->>C: +QUEUED

    C->>H: INCR counter
    H-->>C: +QUEUED

    C->>H: LPUSH mylist a b c
    H-->>C: +QUEUED

    C->>H: EXEC
    H->>H: handleExec()
    H->>H: txErrors == 0? ✓<br/>watchDirty()? ✗ (no watches)

    loop for each queued command
        H->>R: Dispatch(["SET","k1","v1"])
        R-->>H: +OK
        H->>R: Dispatch(["INCR","counter"])
        R-->>H: :43
        H->>R: Dispatch(["LPUSH","mylist","a","b","c"])
        R-->>H: :3
    end

    H->>H: build *3\r\n array of 3 responses
    H-->>C: *3\r\n+OK\r\n:43\r\n:3\r\n

    H->>H: inMulti=false, txQueue=nil<br/>watchedKeys={}
```

## EXEC with Command Error (EXECABORT)

```mermaid
sequenceDiagram
    autonumber
    actor C as Client
    participant H as handler.go
    participant R as router.go

    C->>H: MULTI
    H-->>C: +OK

    C->>H: SET k1 v1
    H-->>C: +QUEUED

    C->>H: NOTACOMMAND arg
    Note over H: Command queued as-is.<br/>txErrors++ (detected at queue time\nif router returns error on syntax check)
    H-->>C: +QUEUED

    C->>H: EXEC
    H->>H: txErrors > 0 → EXECABORT
    H-->>C: -EXECABORT Transaction discarded because of previous errors.

    H->>H: inMulti=false, txQueue=nil
```

## handler.go Transaction Fields

```mermaid
flowchart LR
    subgraph HANDLER["handler struct — per connection"]
        IM["inMulti  bool\n────────────\ntrue between MULTI\nand EXEC/DISCARD"]
        TQ["txQueue  [][]string\n────────────\ncommands buffered\nduring MULTI mode"]
        TE["txErrors  int\n────────────\ncount of syntax-error\ncommands in queue"]
        WK["watchedKeys  map[string]uint64\n────────────\nkey → version at\nWATCH time"]
    end

    subgraph ROUTER["Router"]
        KV["KeyVersion(key) uint64\n────────────\nproxies to\nstore.Version(key)"]
    end

    subgraph STORE["MemoryStore"]
        VER["keyVersion  map[string]uint64\n────────────\nincremented on\nevery write"]
    end

    WK -->|"compare at EXEC"| KV
    KV --> VER
```
