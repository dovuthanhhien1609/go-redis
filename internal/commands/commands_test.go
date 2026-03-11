package commands

import (
	"testing"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// ── Mock store ────────────────────────────────────────────────────────────────

// mockStore is a simple in-memory map that satisfies storage.Store.
// It is NOT thread-safe — that is intentional; command handler tests are
// single-threaded and we want test failures to be deterministic.
type mockStore struct {
	data map[string]string
}

func newMock(pairs ...string) *mockStore {
	m := &mockStore{data: make(map[string]string)}
	for i := 0; i+1 < len(pairs); i += 2 {
		m.data[pairs[i]] = pairs[i+1]
	}
	return m
}

func (m *mockStore) Set(key, value string)         { m.data[key] = value }
func (m *mockStore) Get(key string) (string, bool) { v, ok := m.data[key]; return v, ok }
func (m *mockStore) Del(keys ...string) int {
	n := 0
	for _, k := range keys {
		if _, ok := m.data[k]; ok {
			delete(m.data, k)
			n++
		}
	}
	return n
}
func (m *mockStore) Exists(keys ...string) int {
	n := 0
	for _, k := range keys {
		if _, ok := m.data[k]; ok {
			n++
		}
	}
	return n
}
func (m *mockStore) Keys(_ string) []string {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}
func (m *mockStore) Len() int { return len(m.data) }
func (m *mockStore) Flush()   { m.data = make(map[string]string) }

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
	// Verify the value was actually stored.
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
	// "a" and "b" exist; "nope" does not → count = 2
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
	// Redis counts a repeated key each time it appears in the argument list.
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
	// Should return an array (possibly empty) — not an error.
	assertArrayLen(t, r, 0)
}
