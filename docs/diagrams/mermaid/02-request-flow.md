# Request Flow

Two complete request lifecycles: a plain `SET` (read/write path) and a `GET` (read-only path).

## Normal Command: `SET hello world EX 30`

```mermaid
sequenceDiagram
    autonumber
    actor Client as redis-cli
    participant TCP as server.go<br/>TCP Listener
    participant H as handler.go<br/>Connection Handler
    participant P as parser.go<br/>RESP Parser
    participant R as router.go<br/>Command Router
    participant FN as string.go<br/>handleSet()
    participant MS as memory.go<br/>MemoryStore
    participant AOF as aof.go<br/>AOF Writer

    Client->>TCP: TCP segment<br/>*5\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n$2\r\nEX\r\n$2\r\n30\r\n

    TCP->>H: net.Conn<br/>(goroutine spawned on first connect)

    loop serve() read loop
        H->>P: ReadCommand()
        P->>P: ReadByte() → '*'<br/>readLine() → "5"<br/>loop 5×: read bulk string
        P-->>H: []string{"SET","hello","world","EX","30"}
    end

    H->>H: ToUpper(args[0]) → "SET"<br/>not SUBSCRIBE / MULTI / WATCH

    H->>R: Dispatch(["SET","hello","world","EX","30"])
    R->>R: lookup handlers["SET"] → handleSet

    R->>FN: handleSet(args, store)
    FN->>FN: parse options:<br/>EX→ttl=30s, no NX/XX/GET/KEEPTTL
    FN->>MS: SetAdv("hello","world", 30s, {})
    MS->>MS: mu.Lock()<br/>deleteKey("hello")<br/>strings["hello"]={value:"world",<br/>expiresAt:now+30s}<br/>keyVersion["hello"]++<br/>mu.Unlock()
    MS-->>FN: (oldVal="", oldExists=false, wasSet=true)
    FN-->>R: SimpleString("OK")

    Note over R,AOF: AOF append — mutating command succeeded
    R->>AOF: appendToAOF("SET", args, resp)
    AOF->>AOF: Append(["SET","hello","world"])<br/>Append(["PEXPIREAT","hello","<abs_ms>"])<br/>(EX converted to absolute timestamp — see 06-aof-persistence.md)

    R-->>H: Response{SimpleString, "OK"}
    H->>H: Serialize → "+OK\r\n"
    H-->>Client: TCP segment: "+OK\r\n"
```

## Read-Only Command: `GET hello`

```mermaid
sequenceDiagram
    autonumber
    actor Client as redis-cli
    participant H as handler.go
    participant P as parser.go
    participant R as router.go
    participant FN as string.go<br/>handleGet()
    participant MS as memory.go<br/>MemoryStore

    Client->>H: *2\r\n$3\r\nGET\r\n$5\r\nhello\r\n
    H->>P: ReadCommand()
    P-->>H: []string{"GET","hello"}
    H->>R: Dispatch(["GET","hello"])
    R->>FN: handleGet(args, store)
    FN->>MS: Get("hello")
    MS->>MS: mu.RLock()<br/>e = strings["hello"]<br/>check expired(e.expiresAt) → false<br/>mu.RUnlock()
    MS-->>FN: ("world", true)
    FN-->>R: BulkString("world")
    Note over R: No AOF append — GET is read-only
    R-->>H: Response{BulkString, "world"}
    H-->>Client: $5\r\nworld\r\n
```

## Error Path: Wrong Type

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant H as handler.go
    participant R as router.go
    participant FN as list.go<br/>handleLPush()
    participant MS as MemoryStore

    Client->>H: LPUSH mystring_key value
    H->>R: Dispatch(["LPUSH","mystring_key","value"])
    R->>FN: handleLPush(args, store)
    FN->>MS: LPush("mystring_key", "value")
    MS->>MS: typeOf("mystring_key") → "string"<br/>return 0, ErrWrongType
    MS-->>FN: (0, ErrWrongType)
    FN->>FN: errors.Is(err, ErrWrongType)
    FN-->>R: Error("WRONGTYPE Operation against...")
    Note over R: resp.Type == TypeError → skip AOF
    R-->>H: Response{Error, "WRONGTYPE…"}
    H-->>Client: -WRONGTYPE Operation against a key holding the wrong kind of value\r\n
```
