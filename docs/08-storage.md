# Step 8 — In-Memory Storage

---

## The Core Problem: Shared Mutable State

The store is accessed by many goroutines simultaneously — one per connected client.
Without synchronization, concurrent reads and writes to a Go map cause a **data race**
that corrupts memory and crashes the process.

---

## Synchronization Options

### `sync.Mutex` — exclusive lock for everything
Every operation blocks all others. Readers unnecessarily block each other.
Wrong for a read-heavy workload.

### `sync.RWMutex` — our choice ✓
```
Read A  ──rlock────────runlock──▶
Read B  ──rlock────────runlock──▶   concurrent, no blocking
Write   ──────────lock────unlock──▶ waits for active readers
```
Multiple readers run concurrently. Writers are exclusive.
Optimal for caches (80–95% reads).

### `sync.Map`
Optimized for write-once/read-many or disjoint key-per-goroutine patterns.
Higher overhead than `RWMutex + map` for general workloads. Less ergonomic API.

### Sharded map
N sub-maps, each with its own `RWMutex`. Writers on different shards never block
each other. The right choice for very high write throughput — see Step 13.
A single `RWMutex` is correct and fast enough for Phase 1.

---

## Memory Layout

A `map[string]string` stores:
- **Keys/Values**: Go strings are `(pointer, length)` pairs — bytes live on the heap
- **Buckets**: 8 key-value pairs per bucket, overflow buckets chained as needed
- **No boxing**: no interface overhead, no allocations per get/set

The benchmarks confirm **0 allocations per operation** for both Get and Set on
already-stored keys.

---

## Benchmark Results

```
BenchmarkMemoryStore_Set-16             30M    37 ns/op    0 B/op   0 allocs/op
BenchmarkMemoryStore_Get_Hit-16         53M    24 ns/op    0 B/op   0 allocs/op
BenchmarkMemoryStore_Get_Miss-16        63M    17 ns/op    0 B/op   0 allocs/op
BenchmarkMemoryStore_Set_Parallel-16    12M   103 ns/op    0 B/op   0 allocs/op
BenchmarkMemoryStore_Get_Parallel-16    27M    45 ns/op    0 B/op   0 allocs/op
```

Single-threaded: ~37 ns/Set, ~24 ns/Get.
Under parallel contention: ~103 ns/Set, ~45 ns/Get — the RWMutex overhead under
heavy parallel read/write pressure is ~2–3×, still well within sub-microsecond range.

---

## Multi-Type Storage

The `MemoryStore` holds two separate maps — one for strings, one for hashes — sharing
a single key namespace and a single `RWMutex`:

```go
type MemoryStore struct {
    mu      sync.RWMutex
    strings map[string]strEntry              // string values + per-entry expiry
    hashes  map[string]map[string]string     // hash values
    hashExp map[string]time.Time             // per-key expiry for hashes
}

type strEntry struct {
    value     string
    expiresAt time.Time // zero means "no expiry"
}
```

A key name can refer to at most one type. Setting a string key automatically removes
any hash at the same name, and vice versa (matches Redis semantics).

---

## Key Expiration

### Lazy deletion (on every read)

Every `Get`, `Exists`, `HGet`, etc. checks the `expiresAt` field before returning a value.
An expired entry is treated as if it does not exist, even if the background sweeper
has not yet collected it.

### Active deletion (background goroutine)

`store.StartCleanup(ctx)` launches a goroutine that calls `deleteExpired()` once per
second. It acquires a write lock, iterates both maps, and deletes any entry whose
`expiresAt` is in the past. The goroutine exits when `ctx` is cancelled (server shutdown).

```go
// main.go — after context creation
store.StartCleanup(ctx)
```

This dual strategy (lazy + active) matches Redis's own approach: expired keys are
invisible immediately on read, and memory is reclaimed by the background sweep within
1 second.

---

## `Flush()` Design Note

```go
func (s *MemoryStore) Flush() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.strings = make(map[string]strEntry)
    s.hashes  = make(map[string]map[string]string)
    s.hashExp = make(map[string]time.Time)
}
```

Replacing the map pointers is O(1) regardless of key count, releases all existing
bucket memory to the GC immediately, and holds the write lock for the minimum
possible time.

---

## `Rename()` — Atomic Cross-Type Move

`Rename(src, dst)` takes the write lock for the full operation so no other goroutine
can observe a half-moved state:

```go
func (s *MemoryStore) Rename(src, dst string) bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    // copy src (string or hash) to dst, then delete src
}
```

The TTL of `src` is preserved at `dst`.

---

## Store Interface

```go
type Store interface {
    // Strings
    Set(key, value string)
    SetWithTTL(key, value string, ttl time.Duration)
    Get(key string) (string, bool)
    Del(keys ...string) int
    Exists(keys ...string) int
    Keys(pattern string) []string
    Len() int
    Flush()

    // Expiry
    Expire(key string, ttl time.Duration) bool
    TTL(key string) time.Duration  // -1s = no TTL, -2s = not found
    Persist(key string) bool

    // Atomic rename
    Rename(src, dst string) bool

    // Hashes
    HSet(key string, fields map[string]string) int
    HGet(key, field string) (string, bool)
    HDel(key string, fields ...string) int
    HGetAll(key string) map[string]string
    HLen(key string) int
    HExists(key, field string) bool
    HKeys(key string) []string
    HVals(key string) []string

    // Type introspection
    Type(key string) string  // "string" | "hash" | "none"
}
```

Commands depend only on this interface. `MemoryStore` is never referenced directly
outside of `main.go` and the storage package itself.

---

## Files

| File | Responsibility |
|---|---|
| `internal/storage/store.go` | `Store` interface |
| `internal/storage/memory.go` | `MemoryStore` — strings + hashes + expiry + rename |
| `internal/storage/memory_test.go` | 45+ tests: correctness, expiry, hashes, rename, concurrency, benchmarks |

## Running Tests

```bash
# Correctness only
go test ./internal/storage/...

# With race detector (recommended)
go test -race ./internal/storage/...

# With benchmarks
go test -bench=. -benchmem ./internal/storage/...
```
