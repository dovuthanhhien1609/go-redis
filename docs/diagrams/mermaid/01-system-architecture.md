# System Architecture

End-to-end view of every layer in the go-redis server — from the TCP socket that accepts client connections to the disk file that survives restarts.

```mermaid
flowchart LR
    CLIENT(["redis-cli\n/ any RESP client"])

    subgraph NETWORK["Network Layer  •  internal/server"]
        TCP["server.go\nTCP Listener\n:6379"]
        HANDLER["handler.go\nConnection Handler\n─────────────\none goroutine\nper client\n─────────────\n• RESP read loop\n• pub/sub state\n• MULTI/EXEC state"]
    end

    subgraph PROTO["Protocol Layer  •  internal/protocol"]
        PARSER["parser.go\nRESP v2 Parser\nbinary-safe\nincremental"]
        SERIAL["serializer.go\nRESP v2 Serializer"]
    end

    subgraph CMD["Command Layer  •  internal/commands"]
        ROUTER["router.go\nCommand Router\n─────────────\nToUpper → lookup\n→ HandlerFunc\n→ AOF append"]
        HANDLERS["Handler Functions\n─────────────\nstring.go  hash.go\nlist.go    set.go\nzset.go    scan.go\nexpire.go  counter.go\nserver_cmds.go\npubsub.go"]
    end

    subgraph STORE["Storage Layer  •  internal/storage"]
        MEM["memory.go\nMemoryStore\n─────────────\nstrings  hashes\nlists    sets\nzsets    keyExpiry\nkeyVersion\n─────────────\nsync.RWMutex\nbg cleanup goroutine"]
    end

    subgraph PERSIST["Persistence Layer  •  internal/persistence"]
        AOF["aof.go\nAOF Writer\nappendonly.aof\n─────────────\nalways / everysec / no\nfsync policies"]
        REPLAY["replay.go\nAOF Replay\n(startup only)"]
    end

    subgraph PUBSUB["Pub/Sub Layer  •  internal/pubsub"]
        BROKER["broker.go\nBroker\n─────────────\nchannels map\npatterns map\n(glob matching)"]
        SUB["subscriber.go\nSubscriber\ninbox chan\ndone chan"]
    end

    subgraph INFRA["🔧 Infrastructure"]
        CFG["config.go\nConfig\nport / aof path\nfsync policy"]
        LOG["logger.go\nStructured Logger\nslog, leveled"]
    end

    CLIENT -->|"TCP bytes\n*3\\r\\n$3\\r\\nSET…"| TCP
    TCP -->|"net.Conn\ngoroutine spawn"| HANDLER
    HANDLER -->|"ReadCommand()"| PARSER
    PARSER -->|"[]string{cmd,args…}"| HANDLER
    HANDLER -->|"Dispatch(args)"| ROUTER
    ROUTER -->|"fn(args, store)"| HANDLERS
    HANDLERS <-->|"Get/Set/LPush\n/SAdd/ZAdd…"| MEM
    HANDLERS -->|"Publish(ch,msg)"| BROKER
    BROKER -->|"Send(msg)"| SUB
    SUB -->|"write loop\ngoroutine"| HANDLER
    ROUTER -->|"Append(args)\nmutating cmds only"| AOF
    ROUTER -->|"Response"| SERIAL
    SERIAL -->|"RESP bytes\n+OK\\r\\n"| HANDLER
    HANDLER -->|"TCP bytes"| CLIENT
    REPLAY -->|"store.Set/LPush…\nat startup"| MEM
    CFG -.->|config| TCP
    CFG -.->|config| AOF
    LOG -.->|logs| TCP
    LOG -.->|logs| HANDLER
```

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| Goroutine per connection | Exploits Go's scheduler; simpler than event loop; no shared handler state needed |
| `storage.Store` interface | Allows mock injection in tests; decouples commands from storage implementation |
| Handler intercepts SUBSCRIBE/MULTI | These are connection-state commands — the router only knows stateless pure functions |
| AOF after command succeeds | Guarantees the log only contains operations that actually changed state |
| `commands.Appender` interface | Prevents import cycle: `commands` must not import `persistence` |
