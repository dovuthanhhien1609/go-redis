# Storage Data Model

Internal layout of `MemoryStore` — all five data types, the shared expiry map, and the key versioning map used for `WATCH`.

## MemoryStore Struct Layout

```mermaid
flowchart TB
    subgraph MS["MemoryStore  •  memory.go"]
        direction TB

        MU["sync.RWMutex\n─────────────\nProtects all maps below.\nRLock for reads,\nLock for writes."]

        subgraph STRINGS["strings  map[string]strEntry"]
            S1["'name' → {value:'alice', expiresAt:zero}"]
            S2["'counter' → {value:'42', expiresAt:zero}"]
            S3["'session' → {value:'tok123', expiresAt:2026-04-04T13:00}"]
        end

        subgraph HASHES["hashes  map[string]map[string]string"]
            H1["'user:1' → { 'name':'bob', 'age':'30', 'email':'b@x.com' }"]
            H2["'config' → { 'debug':'true', 'port':'6379' }"]
        end

        subgraph LISTS["lists  map[string][]string"]
            L1["'queue' → ['job3','job2','job1']  ← index 0 = head"]
            L2["'log'   → ['entry1','entry2','entry3']"]
        end

        subgraph SETS["sets  map[string]map[string]struct{}"]
            SET1["'tags'   → {'go','redis','nosql'}"]
            SET2["'online' → {'alice','bob'}"]
        end

        subgraph ZSETS["zsets  map[string]*zset"]
            Z1["'leaderboard' → scores:{'alice':1500,'bob':2300,'charlie':900}"]
            Z2["'delays' → scores:{'task1':0.5,'task2':1.2}"]
        end

        subgraph EXPIRY["keyExpiry  map[string]time.Time\n(hash / list / set / zset keys only — strings embed expiry in strEntry)"]
            E1["'user:1'  → 2026-04-04T14:00:00"]
            E2["'queue'   → zero  (no TTL)"]
        end

        subgraph VERSION["keyVersion  map[string]uint64\n(incremented on every write — used by WATCH)"]
            V1["'name'         → 3"]
            V2["'leaderboard'  → 7"]
            V3["'counter'      → 41"]
        end
    end
```

## Expiry Model

```mermaid
flowchart LR
    subgraph WRITE["Write path (any Set* / LPush / SAdd / ZAdd…)"]
        W1["deleteKey(key)\nclears all type maps"]
        W2["store in correct type map"]
        W1 --> W2
    end

    subgraph READ["Read path (Get / LRange / SMembers…)"]
        R1["acquire RLock"]
        R2{"expired?\nexpiresAt != zero\n&& now.After(expiresAt)"}
        R3["return value"]
        R4["return not-found"]
        R1 --> R2
        R2 -->|no| R3
        R2 -->|yes| R4
    end

    subgraph BG["Background cleanup goroutine\nStartCleanup(ctx) — every 1s"]
        B1["deleteExpired()"]
        B2["sweep strings map"]
        B3["sweep keyExpiry map\n→ delete hash/list/set/zset keys"]
        B1 --> B2
        B1 --> B3
    end

    WRITE -.->|"keyExpiry[key] = now+TTL\n(non-string types)"| BG
    WRITE -.->|"strEntry.expiresAt = now+TTL\n(string type)"| BG
```

## Type Conflict Rules

```mermaid
flowchart TD
    CMD["Command arrives\ne.g. LPUSH mykey value"]
    CHECK{"typeOf(key) ?"}
    EMPTY["'' — key absent\nor expired"]
    SAME["same type\ne.g. 'list'"]
    DIFF["different type\ne.g. 'string' or 'hash'"]

    CMD --> CHECK
    CHECK -->|"empty"| EMPTY --> OK["proceed\ncreate new entry"]
    CHECK -->|"matches"| SAME --> OK2["proceed\nappend / update"]
    CHECK -->|"mismatch"| DIFF --> ERR["return ErrWrongType\n→ '-WRONGTYPE Operation…'"]
```

## Sorted Set Internal Layout

```mermaid
flowchart LR
    subgraph ZSET["*zset struct"]
        SCORES["scores  map[string]float64\n────────────────────\n'alice'   → 1500\n'bob'     → 2300\n'charlie' →  900\n\nO(1) member→score lookup"]
    end

    subgraph OPS["Range / Rank operations"]
        SORT["sortedMembers(z)\n→ []ZMember sorted by\n  (score ASC, member ASC)\n  built on demand via sort.Slice\n  O(N log N)"]
        RANK["ZRank — linear scan O(N)\nZRange — slice [start:stop+1]\nZRangeByScore — filter by score\nZRangeByLex — filter by member string"]
    end

    SCORES -->|"iterate + sort"| SORT --> RANK
```

## Concurrency Model

```mermaid
flowchart LR
    subgraph READERS["Concurrent readers — all hold RLock simultaneously"]
        G1["Goroutine 1\nGET foo"]
        G2["Goroutine 2\nLRANGE list 0 -1"]
        G3["Goroutine 3\nSMEMBERS myset"]
    end

    subgraph LOCK["sync.RWMutex"]
        RL["RLock — N readers allowed"]
        WL["Lock — 1 writer, blocks all"]
    end

    subgraph WRITERS["Writers — one at a time, blocks readers"]
        G4["Goroutine 4\nSET foo bar"]
        G5["Goroutine 5\nSADD myset x"]
    end

    G1 & G2 & G3 --> RL
    G4 & G5 --> WL
```
