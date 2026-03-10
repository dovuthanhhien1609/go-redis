# Step 1 — Project Idea and Scope

---

## 1. What is Redis?

Redis (Remote Dictionary Server) is an open-source, in-memory data structure store.
It was created by Salvatore Sanfilippo in 2009 and is one of the most widely deployed
databases in the world.

At its core, Redis is:

- **An in-memory key-value store** — all data lives in RAM, making reads and writes
  extremely fast (sub-millisecond latency)
- **A network server** — clients connect over TCP using a well-defined protocol called
  RESP (Redis Serialization Protocol)
- **A multi-data-structure engine** — beyond simple strings, Redis supports lists, sets,
  sorted sets, hashes, streams, and more
- **Optionally persistent** — Redis can write data to disk using either AOF
  (Append-Only File) or RDB (snapshot) strategies

Redis achieves its performance by running a single-threaded event loop for command
processing, eliminating lock contention entirely. Modern Redis (6.x+) added I/O
threading, but the command execution model remains largely single-threaded for
simplicity and correctness.

**Real-world uses**: caching, session storage, rate limiting, pub/sub messaging,
leaderboards, distributed locks, job queues.

---

## 2. What We Will Reimplement

Our clone — **`go-redis`** — will reimplement the following Redis subsystems:

| Subsystem          | What we build                                          |
|--------------------|--------------------------------------------------------|
| TCP Server         | Accepts client connections on a configurable port      |
| RESP Protocol      | Full parser and serializer for RESP v2                 |
| Command Engine     | Dispatching and executing commands                     |
| Core Commands      | `PING`, `SET`, `GET`, `DEL`, `EXISTS`, `KEYS`          |
| In-Memory Storage  | Thread-safe string key-value map                       |
| AOF Persistence    | Write-ahead log for durability and recovery            |
| Configuration      | File-based and flag-based config loading               |
| Logging            | Structured logging with levels                         |

This covers the critical path from client connection → command execution → response —
the same path every Redis request takes.

---

## 3. What We Will Intentionally Simplify

Redis is a 150,000+ line C codebase with 15+ years of production hardening.
We will consciously simplify:

| Redis Feature        | Our Simplification                                    | Reason                                                               |
|----------------------|-------------------------------------------------------|----------------------------------------------------------------------|
| Event loop (ae)      | Use Go's goroutine-per-connection model               | Go's scheduler handles multiplexing; no need for epoll abstraction   |
| Multi data types     | Only string values in Phase 1                         | Focus on core architecture first                                     |
| Single-threaded exec | Use RWMutex instead                                   | Go makes concurrent safety natural; teaches the tradeoff explicitly  |
| RDB snapshots        | Phase 2 only                                          | AOF is simpler and teaches the core concept                          |
| Cluster/sharding     | Architecture only                                     | Introduces partitioning theory without implementation complexity     |
| TLS, AUTH, ACL       | Not in scope                                          | Security layer is orthogonal to the core learning goals              |
| Lua scripting        | Not in scope                                          | Embedded scripting is a separate systems topic                       |
| Pub/Sub              | Architecture only in Phase 1                          | Requires event fan-out design — excellent Phase 2 topic              |

The simplifications are **deliberate** — not shortcuts. Each one removes incidental
complexity so the essential complexity of each subsystem stays visible.

---

## 4. Learning Objectives

This project is a structured deep-dive into six areas of systems programming:

### Networking
- How TCP servers accept and manage concurrent connections
- Why Go goroutines are well-suited for I/O-bound connection handling
- How connection lifecycles work: accept → read → parse → respond → loop

### Protocol Design
- How wire protocols encode typed data (RESP)
- The difference between text and binary protocols
- How to write a robust incremental parser that handles partial reads

### Concurrency
- Why shared mutable state requires synchronization
- `sync.RWMutex` — reader/writer fairness and performance characteristics
- Goroutine lifecycle management and graceful shutdown

### Storage Engines
- In-memory hash map as a storage primitive
- Why memory layout matters for cache performance
- The fundamental tradeoff: speed vs. durability

### Persistence
- The Append-Only File pattern (write-ahead logging)
- How to replay a log to reconstruct state after a crash
- fsync semantics and the durability vs. performance spectrum

### Database Architecture
- How to structure a database server as loosely coupled components
- How a request flows through multiple abstraction layers
- How to design for extensibility (TTL, replication, pub/sub)
