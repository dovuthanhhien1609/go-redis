# Package Dependencies

The import graph shows which packages depend on which. **Lower layers never import upper layers** — this is enforced by `commands.Appender` and `commands.Publisher` interfaces that break potential cycles.

## Full Dependency Graph

```mermaid
flowchart TB
    subgraph ENTRY["Entry Point"]
        MAIN["cmd/server\nmain.go"]
    end

    subgraph SRV["Network Layer"]
        SERVER["internal/server\nserver.go  handler.go"]
    end

    subgraph CMD["Command Layer"]
        COMMANDS["internal/commands\nrouter.go\nstring.go  hash.go\nlist.go    set.go\nzset.go    scan.go\nexpire.go  counter.go\nserver_cmds.go\nserver_extra.go\npubsub.go  meta.go\nutil.go"]
    end

    subgraph PERSIST["Persistence Layer"]
        PERSISTENCE["internal/persistence\naof.go  replay.go"]
    end

    subgraph PROTO["Protocol Layer"]
        PROTOCOL["internal/protocol\nparser.go\nserializer.go\ntypes.go"]
    end

    subgraph STOR["Storage Layer"]
        STORAGE["internal/storage\nstore.go  memory.go"]
    end

    subgraph PS["Pub/Sub Layer"]
        PUBSUB["internal/pubsub\nbroker.go\nsubscriber.go\nmessage.go"]
    end

    subgraph INF["Infrastructure"]
        CONFIG["internal/config\nconfig.go"]
        LOGGER["internal/logger\nlogger.go"]
    end

    MAIN --> SERVER
    MAIN --> COMMANDS
    MAIN --> STORAGE
    MAIN --> PERSISTENCE
    MAIN --> PUBSUB
    MAIN --> CONFIG
    MAIN --> LOGGER

    SERVER --> COMMANDS
    SERVER --> PROTOCOL
    SERVER --> PUBSUB
    SERVER --> CONFIG
    SERVER --> LOGGER

    COMMANDS --> STORAGE
    COMMANDS --> PROTOCOL

    PERSISTENCE --> STORAGE
    PERSISTENCE --> PROTOCOL

    PUBSUB --> PROTOCOL
```

## Why No Import Cycles?

```mermaid
flowchart LR
    subgraph PROBLEM["Would create cycle"]
        C1["commands\n(router.go)"]
        P1["persistence\n(aof.go)"]
        C1 -->|"wants to import\nfor Append()"| P1
        P1 -->|"imports for\nReplay(store)"| S1["storage"]
        S1 -->|"(fine)"| X1[" "]
    end

    subgraph SOLUTION["Interface breaks the cycle"]
        C2["commands\n(router.go)"]
        AI["commands.Appender\ninterface\n─────────────\nAppend(args) error"]
        P2["persistence.AOF\n*AOF satisfies\ncommands.Appender"]

        C2 -->|"depends on interface\n(not concrete type)"| AI
        P2 -.->|"implements"| AI
        MAIN2["main.go\ninjects *AOF\ninto Router"]
        MAIN2 -->|"NewRouter(store, aof, pub)"| C2
    end
```

## Layer Rules

```mermaid
flowchart LR
    subgraph RULES["Dependency Rules"]
        R1["cmd/server  → any package"]
        R2["server      → commands, protocol, pubsub"]
        R3["commands    → storage, protocol  (interfaces only for persistence/pubsub)"]
        R4["persistence → storage, protocol"]
        R5["pubsub      → protocol"]
        R6["storage     → (stdlib only, no internal imports)"]
        R7["protocol    → (stdlib only)"]
        R8["storage     → commands  (FORBIDDEN)"]
        R9["protocol    → storage   (FORBIDDEN)"]
        R10["commands   → persistence (FORBIDDEN — use Appender interface)"]
    end
```
