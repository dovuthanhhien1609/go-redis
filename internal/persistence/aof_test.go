package persistence

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hiendvt/go-redis/internal/storage"
)

// ── AOF write tests ───────────────────────────────────────────────────────────

func TestAOF_Append_CreatesFile(t *testing.T) {
	path := tempPath(t)
	aof, err := Open(path, SyncNo)
	assertNoError(t, err)
	defer aof.Close()

	assertNoError(t, aof.Append([]string{"SET", "key", "value"}))

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("AOF file was not created")
	}
}

func TestAOF_Append_WritesRESP(t *testing.T) {
	path := tempPath(t)
	aof, err := Open(path, SyncNo)
	assertNoError(t, err)

	assertNoError(t, aof.Append([]string{"SET", "hello", "world"}))
	assertNoError(t, aof.Close())

	data, err := os.ReadFile(path)
	assertNoError(t, err)

	want := "*3\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n"
	if string(data) != want {
		t.Errorf("AOF content:\ngot  %q\nwant %q", string(data), want)
	}
}

func TestAOF_Append_MultipleCommands(t *testing.T) {
	path := tempPath(t)
	aof, err := Open(path, SyncNo)
	assertNoError(t, err)

	assertNoError(t, aof.Append([]string{"SET", "a", "1"}))
	assertNoError(t, aof.Append([]string{"SET", "b", "2"}))
	assertNoError(t, aof.Append([]string{"DEL", "a"}))
	assertNoError(t, aof.Close())

	// Replay and verify the final state.
	store := storage.NewMemoryStore()
	n, err := Replay(path, store)
	assertNoError(t, err)

	if n != 3 {
		t.Errorf("replayed %d commands, want 3", n)
	}
	// "a" was deleted, "b" remains.
	if _, ok := store.Get("a"); ok {
		t.Error("key 'a' should have been deleted by DEL replay")
	}
	if v, ok := store.Get("b"); !ok || v != "2" {
		t.Errorf("key 'b': got (%q, %v), want (\"2\", true)", v, ok)
	}
}

// ── Replay tests ──────────────────────────────────────────────────────────────

func TestReplay_MissingFile_IsNotAnError(t *testing.T) {
	store := storage.NewMemoryStore()
	n, err := Replay("/tmp/nonexistent-aof-file-xyz.aof", store)
	if err != nil {
		t.Errorf("missing file should not be an error, got: %v", err)
	}
	if n != 0 {
		t.Errorf("replayed %d commands from a missing file, want 0", n)
	}
}

func TestReplay_EmptyFile(t *testing.T) {
	path := tempPath(t)
	// Create an empty file.
	f, _ := os.Create(path)
	f.Close()

	store := storage.NewMemoryStore()
	n, err := Replay(path, store)
	assertNoError(t, err)
	if n != 0 {
		t.Errorf("replayed %d commands from empty file, want 0", n)
	}
}

func TestReplay_SET(t *testing.T) {
	path := tempPath(t)
	writeAOF(t, path, "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n")

	store := storage.NewMemoryStore()
	n, err := Replay(path, store)
	assertNoError(t, err)

	if n != 1 {
		t.Errorf("replayed %d commands, want 1", n)
	}
	v, ok := store.Get("foo")
	if !ok || v != "bar" {
		t.Errorf("key 'foo': got (%q, %v), want (\"bar\", true)", v, ok)
	}
}

func TestReplay_DEL(t *testing.T) {
	path := tempPath(t)
	writeAOF(t, path,
		"*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"+
			"*2\r\n$3\r\nDEL\r\n$3\r\nfoo\r\n",
	)

	store := storage.NewMemoryStore()
	n, err := Replay(path, store)
	assertNoError(t, err)

	if n != 2 {
		t.Errorf("replayed %d commands, want 2", n)
	}
	if _, ok := store.Get("foo"); ok {
		t.Error("key 'foo' should not exist after DEL replay")
	}
}

func TestReplay_FlushesStoreFirst(t *testing.T) {
	path := tempPath(t)
	writeAOF(t, path, "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nnew\r\n")

	// Pre-populate the store with stale data.
	store := storage.NewMemoryStore()
	store.Set("stale", "data")
	store.Set("foo", "old")

	_, err := Replay(path, store)
	assertNoError(t, err)

	// Stale key must be gone — Replay flushes before replaying.
	if _, ok := store.Get("stale"); ok {
		t.Error("Replay should have flushed stale keys before replaying")
	}
	// Replayed key must have the new value.
	if v, _ := store.Get("foo"); v != "new" {
		t.Errorf("key 'foo': got %q, want %q", v, "new")
	}
}

func TestReplay_RoundTrip(t *testing.T) {
	// Write via AOF, read back via Replay — the state must match exactly.
	path := tempPath(t)

	aof, err := Open(path, SyncNo)
	assertNoError(t, err)

	commands := [][]string{
		{"SET", "name", "alice"},
		{"SET", "city", "bangkok"},
		{"SET", "name", "bob"}, // overwrite
		{"DEL", "city"},
	}
	for _, cmd := range commands {
		assertNoError(t, aof.Append(cmd))
	}
	assertNoError(t, aof.Close())

	// Replay into a fresh store.
	store := storage.NewMemoryStore()
	n, err := Replay(path, store)
	assertNoError(t, err)

	if n != 4 {
		t.Errorf("replayed %d commands, want 4", n)
	}
	if v, _ := store.Get("name"); v != "bob" {
		t.Errorf("name: got %q, want \"bob\"", v)
	}
	if _, ok := store.Get("city"); ok {
		t.Error("city should not exist after DEL")
	}
	if store.Len() != 1 {
		t.Errorf("store Len: got %d, want 1", store.Len())
	}
}

func TestAOF_SyncAlways(t *testing.T) {
	// SyncAlways calls fsync after every write — verify it does not error.
	path := tempPath(t)
	aof, err := Open(path, SyncAlways)
	assertNoError(t, err)
	defer aof.Close()

	for i := 0; i < 10; i++ {
		assertNoError(t, aof.Append([]string{"SET", "k", "v"}))
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func tempPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.aof")
}

func writeAOF(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeAOF: %v", err)
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
