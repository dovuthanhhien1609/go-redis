# go-redis Architecture Diagrams

All diagrams describe the current production-like implementation of the go-redis server.

| # | Diagram | What it shows |
|---|---------|---------------|
| 1 | [System Architecture](01-system-architecture.md) | All packages and their relationships at a glance |
| 2 | [Request Flow](02-request-flow.md) | Full lifecycle of one command from TCP bytes to response |
| 3 | [Storage Data Model](03-storage-data-model.md) | MemoryStore internals — all 5 data types + expiry + versioning |
| 4 | [Pub/Sub Flow](04-pubsub-flow.md) | SUBSCRIBE/PUBLISH message delivery through the Broker |
| 5 | [Transaction Lifecycle](05-transaction-lifecycle.md) | MULTI/EXEC/DISCARD/WATCH state machine per connection |
| 6 | [AOF Persistence](06-aof-persistence.md) | Write path (append + fsync policies) and startup replay |
| 7 | [Package Dependencies](07-package-dependencies.md) | Import graph — which packages depend on which |
