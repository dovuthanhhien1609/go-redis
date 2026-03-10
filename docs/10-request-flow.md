# Step 10 — Request Flow Walkthrough

This step traces a single `SET hello world` command from the moment a TCP packet
arrives to the moment `+OK\r\n` leaves the server.

---

## The Full Path

> PlantUML source: [`docs/diagrams/request-flow.puml`](diagrams/request-flow.puml)

```mermaid
sequenceDiagram
    actor       redis-cli
    participant TCP     as TCP Server<br/>server.go
    participant HANDLER as Connection Handler<br/>handler.go
    participant PARSER  as RESP Parser<br/>parser.go
    participant ROUTER  as Command Router<br/>router.go
    participant CMD     as handleSet<br/>string.go
    participant STORE   as MemoryStore<br/>memory.go
    participant AOF     as AOF Writer<br/>aof.go
    participant SERIAL  as RESP Serializer<br/>serializer.go

    rect rgb(240, 248, 255)
        Note over redis-cli,TCP: Client sends command
        redis-cli ->> TCP: "*3\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n"
        TCP ->> HANDLER: net.Conn (goroutine spawned)
    end

    rect rgb(240, 248, 255)
        Note over HANDLER,PARSER: Parse
        HANDLER ->> PARSER: ReadCommand()
        Note right of PARSER: ReadByte() → '*'<br/>readLine() → "3"<br/>loop 3×: ReadFull(len) + CRLF
        PARSER -->> HANDLER: []string{"SET","hello","world"}
    end

    rect rgb(240, 248, 255)
        Note over HANDLER,CMD: Dispatch & Execute
        HANDLER ->> ROUTER: Dispatch(args)
        ROUTER  ->> CMD:    handleSet(args, store)
        CMD     ->> STORE:  Set("hello","world")
        Note right of STORE: mu.Lock()<br/>data["hello"] = "world"<br/>mu.Unlock()
        STORE  -->> CMD:    done
        CMD    -->> ROUTER: SimpleString("OK")
    end

    rect rgb(240, 248, 255)
        Note over ROUTER,AOF: Persist
        ROUTER ->> AOF: Append(["SET","hello","world"])
        Note right of AOF: bufWriter.WriteString(RESP)<br/>bufWriter.Flush() → OS page cache<br/>fsync via syncLoop every 1s
        AOF -->> ROUTER: done
    end

    rect rgb(240, 248, 255)
        Note over ROUTER,redis-cli: Respond
        ROUTER  -->> HANDLER: Response{SimpleString,"OK"}
        HANDLER  ->> SERIAL:  Serialize(response)
        SERIAL  -->> HANDLER: "+OK\r\n"
        HANDLER  ->> redis-cli: "+OK\r\n"
    end
```

---

## Layer-by-Layer Timing

```
Phase                    Typical cost     Notes
─────────────────────────────────────────────────────────────────────
Network RTT              0.1–50 ms        Depends on client proximity
TCP Accept               ~1 µs            net.Listener.Accept()
Goroutine spawn          ~1 µs            Stack alloc + scheduler
bufio.Reader.Read        ~0 µs            Data already in kernel buffer
RESP parse               ~200 ns          ReadString + Atoi + ReadFull
map lookup (registry)    ~10 ns           Hash + compare
RWMutex.Lock             ~20–100 ns       Uncontended; ~3× under load
map write (store.Set)    ~20–40 ns        Hash + insert; 0 allocs
bufio.WriteString (AOF)  ~30 ns           Writes to userspace buffer
Serialize response       ~50 ns           String concat
conn.Write               ~500 ns          Syscall write() to kernel
─────────────────────────────────────────────────────────────────────
Total (excluding RTT):   ~1–2 µs
```

The network RTT dominates. In-memory operations are nanoseconds.

---

## Concurrent Clients

> PlantUML source: [`docs/diagrams/concurrent-clients.puml`](diagrams/concurrent-clients.puml)

```mermaid
sequenceDiagram
    participant A  as Goroutine A<br/>(client 1 SET)
    participant MU as MemoryStore<br/>RWMutex
    participant B  as Goroutine B<br/>(client 2 SET)

    Note over A,B: Two concurrent SET commands — serialize at write lock
    A  ->> MU: mu.Lock()
    activate MU
    Note over MU: write lock held by A
    B  ->> MU: mu.Lock() — BLOCKED
    A  ->> MU: data["k1"] = "v1"
    A  ->> MU: mu.Unlock()
    deactivate MU
    MU ->> B:  lock acquired
    activate MU
    B  ->> MU: data["k2"] = "v2"
    B  ->> MU: mu.Unlock()
    deactivate MU

    participant C  as Goroutine C<br/>(client 3 GET)
    participant MU2 as MemoryStore<br/>RWMutex
    participant D  as Goroutine D<br/>(client 4 GET)

    Note over C,D: Two concurrent GET commands — run in parallel via RLock
    C  ->> MU2: mu.RLock()
    activate MU2
    D  ->> MU2: mu.RLock()
    Note over MU2: both readers proceed concurrently — no blocking
    C  ->> MU2: read data["k1"]
    D  ->> MU2: read data["k2"]
    C  ->> MU2: mu.RUnlock()
    D  ->> MU2: mu.RUnlock()
    deactivate MU2
```

---

## Graceful Shutdown

> PlantUML source: [`docs/diagrams/graceful-shutdown.puml`](diagrams/graceful-shutdown.puml)

```mermaid
sequenceDiagram
    participant SIG      as OS Signal<br/>SIGTERM/SIGINT
    participant MAIN     as main()
    participant SERVE    as Serve()
    participant LN       as net.Listener
    participant CLOSE    as closeAllConns()
    participant HANDLERS as Handler goroutines
    participant WG       as wg.Wait()

    SIG     ->> MAIN:     signal received
    MAIN    ->> MAIN:     ctx cancelled
    MAIN    ->> LN:       ln.Close()
    LN     -->> SERVE:    Accept() → net.ErrClosed
    SERVE   ->> CLOSE:    closeAllConns()
    CLOSE   ->> HANDLERS: conn.Close() all tracked connections
    Note over HANDLERS: conn.Read() → io.EOF<br/>serve() returns<br/>wg.Done()
    SERVE   ->> WG:       wg.Wait()
    WG     -->> SERVE:    all handlers done
    SERVE  -->> MAIN:     returns nil
    Note over MAIN: process exits cleanly<br/>in-flight commands completed
```

---

## AOF Replay on Startup

> PlantUML source: [`docs/diagrams/aof-flow.puml`](diagrams/aof-flow.puml)

```mermaid
flowchart TD
    A([Server starts]) --> B["persistence.Replay(path, store)"]
    B --> C["store.Flush() — clean slate"]
    C --> D["Open AOF for reading"]
    D --> E{ReadCommand}
    E -->|SET| F["store.Set(key, value)"]
    E -->|DEL| G["store.Del(keys...)"]
    E -->|EOF| H["Log: replay complete\ncommands=N, keys=M"]
    F --> E
    G --> E
    H --> I["persistence.Open(path, policy)\nopen for appending"]
    I --> J["server.Run(ctx) — accept clients"]
    J --> K([Ready])
```

No client connection is accepted until replay is complete.
Clients always see a consistent post-replay state.
