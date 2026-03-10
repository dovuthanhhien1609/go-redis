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

## `Flush()` Design Note

```go
func (s *MemoryStore) Flush() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.data = make(map[string]string)  // O(1), not O(n)
}
```

Replacing the map pointer is O(1) regardless of key count, releases all existing
bucket memory to the GC immediately, and holds the write lock for the minimum
possible time. Iterating and deleting would be O(n) and hold the lock the entire time.

---

## Store Interface

```go
type Store interface {
    Set(key, value string)
    Get(key string) (string, bool)
    Del(keys ...string) int
    Exists(keys ...string) int
    Keys(pattern string) []string
    Len() int
    Flush()
}
```

Commands depend only on this interface. `MemoryStore` is never referenced directly
outside of `main.go` and the storage package itself.

---

## Files

| File | Responsibility |
|---|---|
| `internal/storage/store.go` | `Store` interface |
| `internal/storage/memory.go` | `MemoryStore` — `RWMutex` + `map[string]string` |
| `internal/storage/memory_test.go` | 20 tests: correctness + 5 concurrency + 5 benchmarks |

## Running Tests

```bash
# Correctness only
go test ./internal/storage/...

# With race detector (recommended)
go test -race ./internal/storage/...

# With benchmarks
go test -bench=. -benchmem ./internal/storage/...
```
