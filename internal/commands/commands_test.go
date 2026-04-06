package commands

import (
	"testing"
	"time"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// ── Mock store ────────────────────────────────────────────────────────────────

// mockStore is a simple in-memory store that satisfies storage.Store.
// NOT thread-safe — intentional; command handler tests are single-threaded.
type mockStore struct {
	data    map[string]string
	hashes  map[string]map[string]string
	expires map[string]time.Time // zero = no expiry
}

func newMock(pairs ...string) *mockStore {
	m := &mockStore{
		data:    make(map[string]string),
		hashes:  make(map[string]map[string]string),
		expires: make(map[string]time.Time),
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		m.data[pairs[i]] = pairs[i+1]
	}
	return m
}

func (m *mockStore) isExpired(key string) bool {
	exp, ok := m.expires[key]
	return ok && !exp.IsZero() && time.Now().After(exp)
}

func (m *mockStore) Set(key, value string) {
	m.data[key] = value
	delete(m.expires, key)
	delete(m.hashes, key)
}

func (m *mockStore) SetWithTTL(key, value string, ttl time.Duration) {
	m.data[key] = value
	m.expires[key] = time.Now().Add(ttl)
	delete(m.hashes, key)
}

func (m *mockStore) Get(key string) (string, bool) {
	if m.isExpired(key) {
		return "", false
	}
	v, ok := m.data[key]
	return v, ok
}

func (m *mockStore) Del(keys ...string) int {
	n := 0
	for _, k := range keys {
		if _, ok := m.data[k]; ok && !m.isExpired(k) {
			delete(m.data, k)
			delete(m.expires, k)
			n++
			continue
		}
		if _, ok := m.hashes[k]; ok {
			delete(m.hashes, k)
			delete(m.expires, k)
			n++
		}
	}
	return n
}

func (m *mockStore) Exists(keys ...string) int {
	n := 0
	for _, k := range keys {
		if _, ok := m.data[k]; ok && !m.isExpired(k) {
			n++
			continue
		}
		if _, ok := m.hashes[k]; ok && !m.isExpired(k) {
			n++
		}
	}
	return n
}

func (m *mockStore) Keys(_ string) []string {
	keys := make([]string, 0, len(m.data)+len(m.hashes))
	for k := range m.data {
		if !m.isExpired(k) {
			keys = append(keys, k)
		}
	}
	for k := range m.hashes {
		if !m.isExpired(k) {
			keys = append(keys, k)
		}
	}
	return keys
}

func (m *mockStore) Len() int {
	n := 0
	for k := range m.data {
		if !m.isExpired(k) {
			n++
		}
	}
	for k := range m.hashes {
		if !m.isExpired(k) {
			n++
		}
	}
	return n
}

func (m *mockStore) Flush() {
	m.data = make(map[string]string)
	m.hashes = make(map[string]map[string]string)
	m.expires = make(map[string]time.Time)
}

func (m *mockStore) Expire(key string, ttl time.Duration) bool {
	if _, ok := m.data[key]; ok && !m.isExpired(key) {
		m.expires[key] = time.Now().Add(ttl)
		return true
	}
	if _, ok := m.hashes[key]; ok && !m.isExpired(key) {
		m.expires[key] = time.Now().Add(ttl)
		return true
	}
	return false
}

func (m *mockStore) TTL(key string) time.Duration {
	if _, ok := m.data[key]; ok {
		if m.isExpired(key) {
			return -2 * time.Second
		}
		exp, hasExp := m.expires[key]
		if !hasExp || exp.IsZero() {
			return -1 * time.Second
		}
		return time.Until(exp)
	}
	return -2 * time.Second
}

func (m *mockStore) Persist(key string) bool {
	if exp, ok := m.expires[key]; ok && !exp.IsZero() {
		delete(m.expires, key)
		return true
	}
	return false
}

func (m *mockStore) Rename(src, dst string) bool {
	if val, ok := m.data[src]; ok && !m.isExpired(src) {
		m.data[dst] = val
		delete(m.data, src)
		delete(m.expires, src)
		return true
	}
	if h, ok := m.hashes[src]; ok && !m.isExpired(src) {
		m.hashes[dst] = h
		delete(m.hashes, src)
		delete(m.expires, src)
		return true
	}
	return false
}

func (m *mockStore) HSet(key string, fields map[string]string) int {
	delete(m.data, key)
	if m.hashes[key] == nil {
		m.hashes[key] = make(map[string]string)
	}
	added := 0
	for f, v := range fields {
		if _, exists := m.hashes[key][f]; !exists {
			added++
		}
		m.hashes[key][f] = v
	}
	return added
}

func (m *mockStore) HGet(key, field string) (string, bool) {
	if m.isExpired(key) {
		return "", false
	}
	h, ok := m.hashes[key]
	if !ok {
		return "", false
	}
	v, found := h[field]
	return v, found
}

func (m *mockStore) HDel(key string, fields ...string) int {
	if m.isExpired(key) {
		return 0
	}
	h, ok := m.hashes[key]
	if !ok {
		return 0
	}
	n := 0
	for _, f := range fields {
		if _, exists := h[f]; exists {
			delete(h, f)
			n++
		}
	}
	return n
}

func (m *mockStore) HGetAll(key string) map[string]string {
	if m.isExpired(key) {
		return nil
	}
	h, ok := m.hashes[key]
	if !ok {
		return nil
	}
	result := make(map[string]string, len(h))
	for f, v := range h {
		result[f] = v
	}
	return result
}

func (m *mockStore) HLen(key string) int {
	if m.isExpired(key) {
		return 0
	}
	return len(m.hashes[key])
}

func (m *mockStore) HExists(key, field string) bool {
	if m.isExpired(key) {
		return false
	}
	_, ok := m.hashes[key][field]
	return ok
}

func (m *mockStore) HKeys(key string) []string {
	if m.isExpired(key) {
		return nil
	}
	h := m.hashes[key]
	if h == nil {
		return nil
	}
	keys := make([]string, 0, len(h))
	for f := range h {
		keys = append(keys, f)
	}
	return keys
}

func (m *mockStore) HVals(key string) []string {
	if m.isExpired(key) {
		return nil
	}
	h := m.hashes[key]
	if h == nil {
		return nil
	}
	vals := make([]string, 0, len(h))
	for _, v := range h {
		vals = append(vals, v)
	}
	return vals
}

func (m *mockStore) Type(key string) string {
	if m.isExpired(key) {
		return "none"
	}
	if _, ok := m.data[key]; ok {
		return "string"
	}
	if _, ok := m.hashes[key]; ok {
		return "hash"
	}
	return "none"
}

// ── New interface methods added with lists/sets/zsets/scan/watch ──────────────

func (m *mockStore) SetAdv(key, value string, ttl time.Duration, opts storage.SetAdvOpts) (string, bool, bool) {
	oldVal, oldExists := m.data[key]
	if m.isExpired(key) {
		oldExists = false
		oldVal = ""
	}
	if opts.NX && oldExists {
		return oldVal, oldExists, false
	}
	if opts.XX && !oldExists {
		return oldVal, oldExists, false
	}
	delete(m.hashes, key)
	m.data[key] = value
	if opts.KeepTTL {
		// keep existing TTL unchanged
	} else if ttl > 0 {
		m.expires[key] = time.Now().Add(ttl)
	} else {
		delete(m.expires, key)
	}
	return oldVal, oldExists, true
}

func (m *mockStore) GetEx(key string, ttl time.Duration, persist, hasTTL bool) (string, bool) {
	if m.isExpired(key) {
		return "", false
	}
	v, ok := m.data[key]
	if !ok {
		return "", false
	}
	if persist {
		delete(m.expires, key)
	} else if hasTTL {
		m.expires[key] = time.Now().Add(ttl)
	}
	return v, true
}

func (m *mockStore) Version(_ string) uint64                                { return 0 }
func (m *mockStore) Scan(_ uint64, _ string, _ int) (uint64, []string)     { return 0, nil }
func (m *mockStore) HScan(_ string, _ uint64, _ string, _ int) (uint64, []string, error) {
	return 0, nil, nil
}
func (m *mockStore) SScan(_ string, _ uint64, _ string, _ int) (uint64, []string, error) {
	return 0, nil, nil
}
func (m *mockStore) ZScan(_ string, _ uint64, _ string, _ int) (uint64, []string, error) {
	return 0, nil, nil
}

// List stubs.
func (m *mockStore) LPush(_ string, _ ...string) (int, error)                        { return 0, nil }
func (m *mockStore) RPush(_ string, _ ...string) (int, error)                        { return 0, nil }
func (m *mockStore) LPop(_ string, _ int) ([]string, error)                          { return nil, nil }
func (m *mockStore) RPop(_ string, _ int) ([]string, error)                          { return nil, nil }
func (m *mockStore) LLen(_ string) (int, error)                                      { return 0, nil }
func (m *mockStore) LRange(_ string, _, _ int) ([]string, error)                     { return nil, nil }
func (m *mockStore) LIndex(_ string, _ int) (string, bool, error)                    { return "", false, nil }
func (m *mockStore) LSet(_ string, _ int, _ string) error                            { return nil }
func (m *mockStore) LInsert(_ string, _ bool, _, _ string) (int, error)              { return 0, nil }
func (m *mockStore) LRem(_ string, _ int, _ string) (int, error)                     { return 0, nil }
func (m *mockStore) LTrim(_ string, _, _ int) error                                  { return nil }
func (m *mockStore) LMove(_, _ string, _, _ bool) (string, bool, error)              { return "", false, nil }

// Set stubs.
func (m *mockStore) SAdd(_ string, _ ...string) (int, error)                         { return 0, nil }
func (m *mockStore) SRem(_ string, _ ...string) (int, error)                         { return 0, nil }
func (m *mockStore) SMembers(_ string) ([]string, error)                             { return nil, nil }
func (m *mockStore) SCard(_ string) (int, error)                                     { return 0, nil }
func (m *mockStore) SIsMember(_, _ string) (bool, error)                             { return false, nil }
func (m *mockStore) SMIsMember(_ string, _ ...string) ([]int64, error)               { return nil, nil }
func (m *mockStore) SInter(_ ...string) ([]string, error)                            { return nil, nil }
func (m *mockStore) SUnion(_ ...string) ([]string, error)                            { return nil, nil }
func (m *mockStore) SDiff(_ ...string) ([]string, error)                             { return nil, nil }
func (m *mockStore) SInterStore(_ string, _ ...string) (int, error)                  { return 0, nil }
func (m *mockStore) SUnionStore(_ string, _ ...string) (int, error)                  { return 0, nil }
func (m *mockStore) SDiffStore(_ string, _ ...string) (int, error)                   { return 0, nil }
func (m *mockStore) SRandMember(_ string, _ int) ([]string, error)                   { return nil, nil }
func (m *mockStore) SPop(_ string, _ int) ([]string, error)                          { return nil, nil }
func (m *mockStore) SMove(_, _, _ string) (bool, error)                              { return false, nil }

// Sorted set stubs.
func (m *mockStore) ZAdd(_ string, _ []storage.ZMember, _, _, _, _, _ bool) (int64, error) {
	return 0, nil
}
func (m *mockStore) ZRem(_ string, _ ...string) (int64, error)                       { return 0, nil }
func (m *mockStore) ZScore(_, _ string) (float64, bool, error)                       { return 0, false, nil }
func (m *mockStore) ZCard(_ string) (int64, error)                                   { return 0, nil }
func (m *mockStore) ZRank(_, _ string) (int64, bool, error)                          { return 0, false, nil }
func (m *mockStore) ZRevRank(_, _ string) (int64, bool, error)                       { return 0, false, nil }
func (m *mockStore) ZIncrBy(_ string, _ float64, _ string) (float64, error)          { return 0, nil }
func (m *mockStore) ZRange(_ string, _, _ int64, _, _ bool) ([]string, error)        { return nil, nil }
func (m *mockStore) ZRangeByScore(_ string, _, _ float64, _, _, _ bool, _, _ int64) ([]string, error) {
	return nil, nil
}
func (m *mockStore) ZRevRangeByScore(_ string, _, _ float64, _, _, _ bool, _, _ int64) ([]string, error) {
	return nil, nil
}
func (m *mockStore) ZCount(_ string, _, _ float64, _, _ bool) (int64, error)         { return 0, nil }
func (m *mockStore) ZRangeByLex(_ string, _, _ string, _, _ int64) ([]string, error) { return nil, nil }
func (m *mockStore) ZLexCount(_ string, _, _ string) (int64, error)                  { return 0, nil }
func (m *mockStore) ZPopMin(_ string, _ int64) ([]storage.ZMember, error)            { return nil, nil }
func (m *mockStore) ZPopMax(_ string, _ int64) ([]storage.ZMember, error)            { return nil, nil }
func (m *mockStore) ZUnionStore(_ string, _ []string, _ []float64, _ string) (int64, error) {
	return 0, nil
}
func (m *mockStore) ZInterStore(_ string, _ []string, _ []float64, _ string) (int64, error) {
	return 0, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func assertSimpleString(t *testing.T, r protocol.Response, want string) {
	t.Helper()
	if r.Type != protocol.TypeSimpleString {
		t.Fatalf("type: got %d, want SimpleString", r.Type)
	}
	if r.Str != want {
		t.Errorf("str: got %q, want %q", r.Str, want)
	}
}

func assertError(t *testing.T, r protocol.Response) {
	t.Helper()
	if r.Type != protocol.TypeError {
		t.Fatalf("type: got %d, want Error", r.Type)
	}
}

func assertBulkString(t *testing.T, r protocol.Response, want string) {
	t.Helper()
	if r.Type != protocol.TypeBulkString {
		t.Fatalf("type: got %d, want BulkString", r.Type)
	}
	if r.Str != want {
		t.Errorf("str: got %q, want %q", r.Str, want)
	}
}

func assertNullBulkString(t *testing.T, r protocol.Response) {
	t.Helper()
	if r.Type != protocol.TypeNullBulkString {
		t.Fatalf("type: got %d, want NullBulkString", r.Type)
	}
}

func assertInteger(t *testing.T, r protocol.Response, want int64) {
	t.Helper()
	if r.Type != protocol.TypeInteger {
		t.Fatalf("type: got %d, want Integer", r.Type)
	}
	if r.Integer != want {
		t.Errorf("integer: got %d, want %d", r.Integer, want)
	}
}

func assertArrayLen(t *testing.T, r protocol.Response, want int) {
	t.Helper()
	if r.Type != protocol.TypeArray {
		t.Fatalf("type: got %d, want Array", r.Type)
	}
	if len(r.Array) != want {
		t.Errorf("array length: got %d, want %d", len(r.Array), want)
	}
}

// ── PING tests ────────────────────────────────────────────────────────────────

func TestPing_NoArgs(t *testing.T) {
	r := handlePing([]string{"PING"}, newMock())
	assertSimpleString(t, r, "PONG")
}

func TestPing_WithMessage(t *testing.T) {
	r := handlePing([]string{"PING", "hello"}, newMock())
	assertBulkString(t, r, "hello")
}

func TestPing_TooManyArgs(t *testing.T) {
	r := handlePing([]string{"PING", "a", "b"}, newMock())
	assertError(t, r)
}

// ── SET tests ─────────────────────────────────────────────────────────────────

func TestSet_OK(t *testing.T) {
	store := newMock()
	r := handleSet([]string{"SET", "key", "value"}, store)
	assertSimpleString(t, r, "OK")
	if v, ok := store.data["key"]; !ok || v != "value" {
		t.Errorf("store: got %q, want %q", v, "value")
	}
}

func TestSet_Overwrites(t *testing.T) {
	store := newMock("key", "old")
	handleSet([]string{"SET", "key", "new"}, store)
	if store.data["key"] != "new" {
		t.Error("SET did not overwrite existing key")
	}
}

func TestSet_TooFewArgs(t *testing.T) {
	r := handleSet([]string{"SET", "key"}, newMock())
	assertError(t, r)
}

func TestSet_TooManyArgs(t *testing.T) {
	r := handleSet([]string{"SET", "key", "val", "extra"}, newMock())
	assertError(t, r)
}

func TestSet_EmptyValue(t *testing.T) {
	store := newMock()
	r := handleSet([]string{"SET", "key", ""}, store)
	assertSimpleString(t, r, "OK")
	if v := store.data["key"]; v != "" {
		t.Errorf("expected empty string value, got %q", v)
	}
}

// ── GET tests ─────────────────────────────────────────────────────────────────

func TestGet_Existing(t *testing.T) {
	r := handleGet([]string{"GET", "k"}, newMock("k", "v"))
	assertBulkString(t, r, "v")
}

func TestGet_Missing(t *testing.T) {
	r := handleGet([]string{"GET", "missing"}, newMock())
	assertNullBulkString(t, r)
}

func TestGet_TooFewArgs(t *testing.T) {
	r := handleGet([]string{"GET"}, newMock())
	assertError(t, r)
}

func TestGet_TooManyArgs(t *testing.T) {
	r := handleGet([]string{"GET", "a", "b"}, newMock())
	assertError(t, r)
}

// ── DEL tests ─────────────────────────────────────────────────────────────────

func TestDel_ExistingKey(t *testing.T) {
	r := handleDel([]string{"DEL", "k"}, newMock("k", "v"))
	assertInteger(t, r, 1)
}

func TestDel_MissingKey(t *testing.T) {
	r := handleDel([]string{"DEL", "missing"}, newMock())
	assertInteger(t, r, 0)
}

func TestDel_MultipleKeys(t *testing.T) {
	store := newMock("a", "1", "b", "2", "c", "3")
	r := handleDel([]string{"DEL", "a", "b", "nope"}, store)
	assertInteger(t, r, 2)
	if _, ok := store.data["a"]; ok {
		t.Error("key 'a' should have been deleted")
	}
}

func TestDel_NoArgs(t *testing.T) {
	r := handleDel([]string{"DEL"}, newMock())
	assertError(t, r)
}

// ── EXISTS tests ──────────────────────────────────────────────────────────────

func TestExists_SingleExists(t *testing.T) {
	r := handleExists([]string{"EXISTS", "k"}, newMock("k", "v"))
	assertInteger(t, r, 1)
}

func TestExists_SingleMissing(t *testing.T) {
	r := handleExists([]string{"EXISTS", "nope"}, newMock())
	assertInteger(t, r, 0)
}

func TestExists_DuplicateKeyCounts(t *testing.T) {
	r := handleExists([]string{"EXISTS", "k", "k", "k"}, newMock("k", "v"))
	assertInteger(t, r, 3)
}

func TestExists_MixedKeys(t *testing.T) {
	store := newMock("a", "1", "b", "2")
	r := handleExists([]string{"EXISTS", "a", "b", "missing"}, store)
	assertInteger(t, r, 2)
}

func TestExists_NoArgs(t *testing.T) {
	r := handleExists([]string{"EXISTS"}, newMock())
	assertError(t, r)
}

// ── KEYS tests ────────────────────────────────────────────────────────────────

func TestKeys_Wildcard(t *testing.T) {
	store := newMock("a", "1", "b", "2", "c", "3")
	r := handleKeys([]string{"KEYS", "*"}, store)
	assertArrayLen(t, r, 3)
}

func TestKeys_Empty(t *testing.T) {
	r := handleKeys([]string{"KEYS", "*"}, newMock())
	assertArrayLen(t, r, 0)
}

func TestKeys_NoArgs(t *testing.T) {
	r := handleKeys([]string{"KEYS"}, newMock())
	assertError(t, r)
}

// ── SETNX tests ───────────────────────────────────────────────────────────────

func TestSetNX_NewKey(t *testing.T) {
	store := newMock()
	r := handleSetNX([]string{"SETNX", "k", "v"}, store)
	assertInteger(t, r, 1)
	if store.data["k"] != "v" {
		t.Error("SETNX should have stored the value")
	}
}

func TestSetNX_ExistingKey(t *testing.T) {
	store := newMock("k", "old")
	r := handleSetNX([]string{"SETNX", "k", "new"}, store)
	assertInteger(t, r, 0)
	if store.data["k"] != "old" {
		t.Error("SETNX should not overwrite existing key")
	}
}

// ── INCR / DECR tests ─────────────────────────────────────────────────────────

func TestIncr_NewKey(t *testing.T) {
	r := handleIncr([]string{"INCR", "counter"}, newMock())
	assertInteger(t, r, 1)
}

func TestIncr_Existing(t *testing.T) {
	r := handleIncr([]string{"INCR", "counter"}, newMock("counter", "5"))
	assertInteger(t, r, 6)
}

func TestIncr_NotInteger(t *testing.T) {
	r := handleIncr([]string{"INCR", "k"}, newMock("k", "notanint"))
	assertError(t, r)
}

func TestIncrBy(t *testing.T) {
	r := handleIncrBy([]string{"INCRBY", "k", "10"}, newMock("k", "5"))
	assertInteger(t, r, 15)
}

func TestDecr(t *testing.T) {
	r := handleDecr([]string{"DECR", "k"}, newMock("k", "3"))
	assertInteger(t, r, 2)
}

func TestDecrBy(t *testing.T) {
	r := handleDecrBy([]string{"DECRBY", "k", "3"}, newMock("k", "10"))
	assertInteger(t, r, 7)
}

// ── MSET / MGET tests ─────────────────────────────────────────────────────────

func TestMSet(t *testing.T) {
	store := newMock()
	r := handleMSet([]string{"MSET", "a", "1", "b", "2"}, store)
	assertSimpleString(t, r, "OK")
	if store.data["a"] != "1" || store.data["b"] != "2" {
		t.Error("MSET did not store all values")
	}
}

func TestMGet(t *testing.T) {
	store := newMock("a", "1", "b", "2")
	r := handleMGet([]string{"MGET", "a", "b", "missing"}, store)
	assertArrayLen(t, r, 3)
	assertBulkString(t, r.Array[0], "1")
	assertBulkString(t, r.Array[1], "2")
	assertNullBulkString(t, r.Array[2])
}

// ── GETSET / GETDEL tests ─────────────────────────────────────────────────────

func TestGetSet_Existing(t *testing.T) {
	store := newMock("k", "old")
	r := handleGetSet([]string{"GETSET", "k", "new"}, store)
	assertBulkString(t, r, "old")
	if store.data["k"] != "new" {
		t.Error("GETSET should update the value")
	}
}

func TestGetSet_Missing(t *testing.T) {
	store := newMock()
	r := handleGetSet([]string{"GETSET", "k", "new"}, store)
	assertNullBulkString(t, r)
	if store.data["k"] != "new" {
		t.Error("GETSET should set a new key")
	}
}

func TestGetDel_Existing(t *testing.T) {
	store := newMock("k", "v")
	r := handleGetDel([]string{"GETDEL", "k"}, store)
	assertBulkString(t, r, "v")
	if _, ok := store.data["k"]; ok {
		t.Error("GETDEL should delete the key")
	}
}

func TestGetDel_Missing(t *testing.T) {
	r := handleGetDel([]string{"GETDEL", "k"}, newMock())
	assertNullBulkString(t, r)
}

// ── APPEND / STRLEN tests ─────────────────────────────────────────────────────

func TestAppend(t *testing.T) {
	store := newMock("k", "hello")
	r := handleAppend([]string{"APPEND", "k", " world"}, store)
	assertInteger(t, r, 11)
	if store.data["k"] != "hello world" {
		t.Error("APPEND did not concatenate correctly")
	}
}

func TestStrLen(t *testing.T) {
	r := handleStrLen([]string{"STRLEN", "k"}, newMock("k", "hello"))
	assertInteger(t, r, 5)
}

func TestStrLen_Missing(t *testing.T) {
	r := handleStrLen([]string{"STRLEN", "k"}, newMock())
	assertInteger(t, r, 0)
}

// ── EXPIRE / TTL tests ────────────────────────────────────────────────────────

func TestExpire_SetAndTTL(t *testing.T) {
	store := newMock("k", "v")
	r := handleExpire([]string{"EXPIRE", "k", "100"}, store)
	assertInteger(t, r, 1)

	ttl := handleTTL([]string{"TTL", "k"}, store)
	if ttl.Integer <= 0 || ttl.Integer > 100 {
		t.Errorf("TTL should be 1–100, got %d", ttl.Integer)
	}
}

func TestTTL_NoExpiry(t *testing.T) {
	r := handleTTL([]string{"TTL", "k"}, newMock("k", "v"))
	assertInteger(t, r, -1)
}

func TestTTL_Missing(t *testing.T) {
	r := handleTTL([]string{"TTL", "missing"}, newMock())
	assertInteger(t, r, -2)
}

func TestPersist(t *testing.T) {
	store := newMock("k", "v")
	handleExpire([]string{"EXPIRE", "k", "100"}, store)
	r := handlePersist([]string{"PERSIST", "k"}, store)
	assertInteger(t, r, 1)

	ttl := handleTTL([]string{"TTL", "k"}, store)
	assertInteger(t, ttl, -1)
}

// ── HSET / HGET / HGETALL tests ───────────────────────────────────────────────

func TestHSet_And_HGet(t *testing.T) {
	store := newMock()
	r := handleHSet([]string{"HSET", "user:1", "name", "alice", "age", "30"}, store)
	assertInteger(t, r, 2) // 2 new fields added

	r = handleHGet([]string{"HGET", "user:1", "name"}, store)
	assertBulkString(t, r, "alice")
}

func TestHGet_Missing(t *testing.T) {
	r := handleHGet([]string{"HGET", "nokey", "field"}, newMock())
	assertNullBulkString(t, r)
}

func TestHGetAll(t *testing.T) {
	store := newMock()
	handleHSet([]string{"HSET", "h", "f1", "v1", "f2", "v2"}, store)
	r := handleHGetAll([]string{"HGETALL", "h"}, store)
	assertArrayLen(t, r, 4) // 2 fields × 2 (key+value)
}

func TestHDel(t *testing.T) {
	store := newMock()
	handleHSet([]string{"HSET", "h", "f1", "v1", "f2", "v2"}, store)
	r := handleHDel([]string{"HDEL", "h", "f1"}, store)
	assertInteger(t, r, 1)
	r = handleHGet([]string{"HGET", "h", "f1"}, store)
	assertNullBulkString(t, r)
}

func TestHLen(t *testing.T) {
	store := newMock()
	handleHSet([]string{"HSET", "h", "a", "1", "b", "2"}, store)
	r := handleHLen([]string{"HLEN", "h"}, store)
	assertInteger(t, r, 2)
}

func TestHExists(t *testing.T) {
	store := newMock()
	handleHSet([]string{"HSET", "h", "f", "v"}, store)
	r := handleHExists([]string{"HEXISTS", "h", "f"}, store)
	assertInteger(t, r, 1)
	r = handleHExists([]string{"HEXISTS", "h", "missing"}, store)
	assertInteger(t, r, 0)
}

func TestHIncrBy(t *testing.T) {
	store := newMock()
	handleHSet([]string{"HSET", "h", "score", "10"}, store)
	r := handleHIncrBy([]string{"HINCRBY", "h", "score", "5"}, store)
	assertInteger(t, r, 15)
}

// ── TYPE tests ────────────────────────────────────────────────────────────────

func TestType_String(t *testing.T) {
	r := handleType([]string{"TYPE", "k"}, newMock("k", "v"))
	assertSimpleString(t, r, "string")
}

func TestType_Hash(t *testing.T) {
	store := newMock()
	handleHSet([]string{"HSET", "h", "f", "v"}, store)
	r := handleType([]string{"TYPE", "h"}, store)
	assertSimpleString(t, r, "hash")
}

func TestType_None(t *testing.T) {
	r := handleType([]string{"TYPE", "missing"}, newMock())
	assertSimpleString(t, r, "none")
}

// ── Router dispatch tests ─────────────────────────────────────────────────────

func TestRouter_Dispatch_CaseInsensitive(t *testing.T) {
	store := storage.NewMemoryStore()
	router := NewRouter(store, nil, nil)

	cases := [][]string{
		{"ping"}, {"Ping"}, {"PING"}, {"pInG"},
	}
	for _, args := range cases {
		r := router.Dispatch(args)
		assertSimpleString(t, r, "PONG")
	}
}

func TestRouter_Dispatch_UnknownCommand(t *testing.T) {
	router := NewRouter(storage.NewMemoryStore(), nil, nil)
	r := router.Dispatch([]string{"NOTACOMMAND"})
	assertError(t, r)
}

func TestRouter_Dispatch_EmptyArgs(t *testing.T) {
	router := NewRouter(storage.NewMemoryStore(), nil, nil)
	r := router.Dispatch([]string{})
	assertError(t, r)
}

func TestRouter_Dispatch_SetThenGet(t *testing.T) {
	router := NewRouter(storage.NewMemoryStore(), nil, nil)
	router.Dispatch([]string{"SET", "name", "alice"})
	r := router.Dispatch([]string{"GET", "name"})
	assertBulkString(t, r, "alice")
}

func TestRouter_Dispatch_COMMAND(t *testing.T) {
	router := NewRouter(storage.NewMemoryStore(), nil, nil)
	r := router.Dispatch([]string{"COMMAND"})
	assertArrayLen(t, r, 0)
}

func TestRouter_Dispatch_IncrRoundtrip(t *testing.T) {
	router := NewRouter(storage.NewMemoryStore(), nil, nil)
	router.Dispatch([]string{"SET", "x", "10"})
	r := router.Dispatch([]string{"INCR", "x"})
	assertInteger(t, r, 11)
}
