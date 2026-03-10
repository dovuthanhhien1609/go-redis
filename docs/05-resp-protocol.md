# Step 5 — RESP Protocol Design

---

## What is RESP?

RESP (Redis Serialization Protocol) is the wire protocol Redis uses for client-server
communication. It was designed with three goals:

1. **Simple to implement** — a parser fits in ~100 lines
2. **Fast to parse** — no backtracking, single-pass, line-oriented
3. **Human-readable** — you can type it with `telnet` and read responses directly

Every message in RESP starts with a **type byte** — a single ASCII character that
tells the parser what to expect next.

---

## The 5 RESP Data Types

```
+   Simple String    → +OK\r\n
-   Error            → -ERR unknown command\r\n
:   Integer          → :42\r\n
$   Bulk String      → $5\r\nhello\r\n   (length-prefixed)
*   Array            → *3\r\n...         (element count, then N elements)
```

### Simple String — `+`
A single-line string. No binary data allowed (no `\r\n` inside).
```
+OK\r\n
+PONG\r\n
```

### Error — `-`
A single-line error message. The client treats this as an exception.
```
-ERR unknown command 'FOO'\r\n
-WRONGTYPE Operation against a key holding the wrong kind of value\r\n
```

### Integer — `:`
A signed 64-bit integer.
```
:0\r\n
:1000\r\n
:-1\r\n
```

### Bulk String — `$`
A binary-safe string. Length is declared before the data, so `\r\n` can appear
inside the payload.
```
$5\r\n
hello\r\n

$0\r\n       ← empty string
\r\n

$-1\r\n      ← null bulk string (Redis uses this for "key not found")
```

### Array — `*`
An ordered list of any RESP values. Arrays can be nested.
```
*3\r\n           ← 3 elements follow
$3\r\n
SET\r\n
$5\r\n
hello\r\n
$5\r\n
world\r\n

*0\r\n           ← empty array
*-1\r\n          ← null array
```

---

## How Commands Are Encoded

Every command from the client is sent as a **RESP Array of Bulk Strings**.

```
SET hello world
```
On the wire:
```
*3\r\n
$3\r\n
SET\r\n
$5\r\n
hello\r\n
$5\r\n
world\r\n
```

---

## Parser Design

The parser wraps a `bufio.Reader`:
- `ReadString('\n')` consumes one CRLF-terminated line efficiently
- Never worries about partial TCP reads — the buffer handles reassembly

**Parsing algorithm:**
```
1. ReadByte() → read type prefix byte
2. Branch on type byte:
   '+' → ReadLine()              → SimpleString
   '-' → ReadLine()              → Error
   ':' → ReadLine() + ParseInt  → Integer
   '$' → ReadLine() + ParseInt  → length
           if -1  → NullBulkString
           else   → ReadFull(length) + consume \r\n → BulkString
   '*' → ReadLine() + ParseInt  → count
           if -1  → NullArray
           else   → loop count times, recurse → Array
```

The parser is **recursive** for arrays: each element is a full RESP value,
so `readValue()` is called recursively for each element.

---

## Files

| File | Responsibility |
|------|---------------|
| `internal/protocol/types.go` | `Response` type + constructor functions |
| `internal/protocol/parser.go` | `Parser` — bytes → `Response` |
| `internal/protocol/serializer.go` | `Serialize` — `Response` → bytes |
| `internal/protocol/parser_test.go` | 23 tests covering all types + round-trips |
