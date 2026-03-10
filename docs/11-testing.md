# Step 11 — Testing

---

## Test Coverage by Package

| Package | Tests | What they cover |
|---|---|---|
| `internal/protocol` | 23 | All 5 RESP types, edge cases, round-trips, multi-command stream |
| `internal/storage` | 20 | Correctness, interface compliance, 5 concurrency tests, 5 benchmarks |
| `internal/commands` | 29 | Every handler (arity, logic, edge cases) + router dispatch |
| `internal/persistence` | 10 | Write, replay, round-trip, flush-before-replay, sync policies |
| `internal/server` | 15 | Full-stack integration via real TCP sockets |
| **Total** | **97** | |

All 97 tests pass with `-race` enabled.

---

## Integration Test Design

Server tests exercise the full stack — TCP socket → RESP parser → command router
→ storage → serializer → TCP socket. No mocking.

### Key helpers

**`newTestServer`** — binds on `:0` (OS-assigned free port), starts `Serve` in a
goroutine, returns the address and a cancel func.

```go
addr, cancel := newTestServer(t)
defer cancel()
```

**`testClient.do`** — writes a RESP array, reads one RESP value. Clean, one-liner
test assertions.

```go
c := newTestClient(t, addr)
assertSimpleString(t, c.do("PING"), "PONG")
assertBulkString(t, c.do("GET", "key"), "value")
```

### Shutdown deadlock — and the fix

Naive shutdown has a circular wait:
```
defer cancel() → blocks on <-done
  Serve.wg.Wait() → waits for handlers
    handler.serve() → blocks on conn.Read()
      conn.Close() → registered as t.Cleanup, only runs after defers
DEADLOCK
```

Fix: the server tracks all open `net.Conn` in a map. When `ctx` is cancelled,
`closeAllConns()` closes every tracked connection before `wg.Wait()`. Handler
goroutines get `io.EOF` from `conn.Read()` and return immediately.

This is also the correct production pattern: an orderly shutdown closes active
connections rather than leaving them hanging.

---

## What Each Integration Test Verifies

| Test | What it catches |
|---|---|
| `TestServer_PING` | End-to-end path for simplest command |
| `TestServer_SET_GET` | Store write + read through TCP |
| `TestServer_GET_Missing` | Null bulk string serialization |
| `TestServer_SET_Overwrite` | Store overwrites correctly |
| `TestServer_DEL` | Multi-key DEL + verify gone |
| `TestServer_EXISTS` | Duplicate key counting |
| `TestServer_UnknownCommand` | Error response for unknown commands |
| `TestServer_WrongArgCount` | Error response + connection still usable |
| `TestServer_CaseInsensitiveCommands` | ToUpper normalization in router |
| `TestServer_Pipelining` | Parser handles back-to-back commands in one write |
| `TestServer_ConcurrentClients` | 50 goroutines, no races, no cross-contamination |
| `TestServer_MultipleCommandsPerConnection` | Handler loop re-enters for 100 commands |
| `TestServer_MaxClients` | Second connection rejected when limit reached |

---

## Running Tests

```bash
# All tests (no race detector)
go test ./...

# All tests with race detector (recommended)
go test -race ./...

# Specific package
go test -race ./internal/server/...

# Verbose output
go test -race -v ./internal/protocol/...

# With timeout (guards against deadlocks)
go test -race -timeout 60s ./...

# Benchmarks (storage package)
go test -bench=. -benchmem ./internal/storage/...

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Via Makefile
make test
make test-race
make test-cover
```
