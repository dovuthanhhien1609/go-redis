package storage

import (
	"fmt"
	"sync"
	"testing"
)

// ── Basic correctness tests ───────────────────────────────────────────────────

func TestMemoryStore_SetGet(t *testing.T) {
	s := NewMemoryStore()
	s.Set("hello", "world")
	v, ok := s.Get("hello")
	if !ok {
		t.Fatal("key should exist after Set")
	}
	if v != "world" {
		t.Errorf("got %q, want %q", v, "world")
	}
}

func TestMemoryStore_Get_Missing(t *testing.T) {
	s := NewMemoryStore()
	_, ok := s.Get("nope")
	if ok {
		t.Error("Get on missing key should return false")
	}
}

func TestMemoryStore_Set_Overwrites(t *testing.T) {
	s := NewMemoryStore()
	s.Set("k", "v1")
	s.Set("k", "v2")
	v, _ := s.Get("k")
	if v != "v2" {
		t.Errorf("got %q, want v2", v)
	}
}

func TestMemoryStore_Del_Existing(t *testing.T) {
	s := NewMemoryStore()
	s.Set("k", "v")
	n := s.Del("k")
	if n != 1 {
		t.Errorf("Del count: got %d, want 1", n)
	}
	_, ok := s.Get("k")
	if ok {
		t.Error("key should not exist after Del")
	}
}

func TestMemoryStore_Del_Missing(t *testing.T) {
	s := NewMemoryStore()
	n := s.Del("nope")
	if n != 0 {
		t.Errorf("Del count: got %d, want 0", n)
	}
}

func TestMemoryStore_Del_Multiple(t *testing.T) {
	s := NewMemoryStore()
	s.Set("a", "1")
	s.Set("b", "2")
	n := s.Del("a", "b", "missing")
	if n != 2 {
		t.Errorf("Del count: got %d, want 2", n)
	}
}

func TestMemoryStore_Exists_Present(t *testing.T) {
	s := NewMemoryStore()
	s.Set("k", "v")
	if s.Exists("k") != 1 {
		t.Error("Exists should return 1 for a present key")
	}
}

func TestMemoryStore_Exists_Missing(t *testing.T) {
	s := NewMemoryStore()
	if s.Exists("nope") != 0 {
		t.Error("Exists should return 0 for a missing key")
	}
}

func TestMemoryStore_Exists_Duplicate(t *testing.T) {
	// Each occurrence of a key in the argument list is counted independently.
	s := NewMemoryStore()
	s.Set("k", "v")
	if n := s.Exists("k", "k", "k"); n != 3 {
		t.Errorf("Exists with duplicates: got %d, want 3", n)
	}
}

func TestMemoryStore_Keys_Wildcard(t *testing.T) {
	s := NewMemoryStore()
	s.Set("foo", "1")
	s.Set("bar", "2")
	s.Set("baz", "3")
	keys := s.Keys("*")
	if len(keys) != 3 {
		t.Errorf("Keys(*): got %d keys, want 3", len(keys))
	}
}

func TestMemoryStore_Keys_Prefix(t *testing.T) {
	s := NewMemoryStore()
	s.Set("user:1", "alice")
	s.Set("user:2", "bob")
	s.Set("session:1", "xyz")
	keys := s.Keys("user:*")
	if len(keys) != 2 {
		t.Errorf("Keys(user:*): got %d keys, want 2", len(keys))
	}
}

func TestMemoryStore_Keys_Empty(t *testing.T) {
	s := NewMemoryStore()
	keys := s.Keys("*")
	if len(keys) != 0 {
		t.Errorf("Keys on empty store: got %d, want 0", len(keys))
	}
}

func TestMemoryStore_Len(t *testing.T) {
	s := NewMemoryStore()
	if s.Len() != 0 {
		t.Error("Len should be 0 on new store")
	}
	s.Set("a", "1")
	s.Set("b", "2")
	if s.Len() != 2 {
		t.Errorf("Len: got %d, want 2", s.Len())
	}
	s.Del("a")
	if s.Len() != 1 {
		t.Errorf("Len after Del: got %d, want 1", s.Len())
	}
}

func TestMemoryStore_Flush(t *testing.T) {
	s := NewMemoryStore()
	s.Set("a", "1")
	s.Set("b", "2")
	s.Flush()
	if s.Len() != 0 {
		t.Errorf("Len after Flush: got %d, want 0", s.Len())
	}
	// After flush the store must still be usable — not nil-map panicking.
	s.Set("c", "3")
	v, ok := s.Get("c")
	if !ok || v != "3" {
		t.Error("store should be usable after Flush")
	}
}

// ── Interface compliance ───────────────────────────────────────────────────────

// TestMemoryStore_ImplementsStore is a compile-time assertion that MemoryStore
// satisfies the Store interface. If it ever drifts, this test will fail to compile.
func TestMemoryStore_ImplementsStore(t *testing.T) {
	var _ Store = (*MemoryStore)(nil)
}

// ── Concurrency tests (run with -race) ────────────────────────────────────────
//
// These tests are designed to be run with:
//   go test -race ./internal/storage/...
//
// They exercise concurrent access patterns that would trigger the race detector
// if the locking were incorrect.

// TestMemoryStore_ConcurrentWrites launches N goroutines each writing a unique
// key. If locking is wrong the race detector triggers.
func TestMemoryStore_ConcurrentWrites(t *testing.T) {
	s := NewMemoryStore()
	const n = 500
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			s.Set(fmt.Sprintf("key:%d", i), fmt.Sprintf("val:%d", i))
		}(i)
	}
	wg.Wait()
	if s.Len() != n {
		t.Errorf("after %d concurrent writes: Len = %d", n, s.Len())
	}
}

// TestMemoryStore_ConcurrentReads launches N goroutines all reading the same key.
// RLock must allow them to run in parallel without deadlocking.
func TestMemoryStore_ConcurrentReads(t *testing.T) {
	s := NewMemoryStore()
	s.Set("shared", "value")

	const n = 500
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			v, ok := s.Get("shared")
			if !ok || v != "value" {
				t.Errorf("concurrent Get: got (%q, %v)", v, ok)
			}
		}()
	}
	wg.Wait()
}

// TestMemoryStore_ConcurrentReadWrite mixes readers and writers to expose any
// locking gap between RLock and Lock acquisition.
func TestMemoryStore_ConcurrentReadWrite(t *testing.T) {
	s := NewMemoryStore()
	const n = 200

	var wg sync.WaitGroup
	wg.Add(n * 2)

	// Writers
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			s.Set(fmt.Sprintf("k%d", i), fmt.Sprintf("v%d", i))
		}(i)
	}

	// Readers — may observe keys mid-write; that is fine. What must NOT happen
	// is a data race (concurrent map read and map write).
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			s.Get(fmt.Sprintf("k%d", i))
		}(i)
	}

	wg.Wait()
}

// TestMemoryStore_ConcurrentDel verifies Del is safe when multiple goroutines
// delete overlapping sets of keys simultaneously.
func TestMemoryStore_ConcurrentDel(t *testing.T) {
	s := NewMemoryStore()
	const n = 200
	for i := 0; i < n; i++ {
		s.Set(fmt.Sprintf("k%d", i), "v")
	}

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			s.Del(fmt.Sprintf("k%d", i))
		}(i)
	}
	wg.Wait()

	if s.Len() != 0 {
		t.Errorf("after concurrent Del: Len = %d, want 0", s.Len())
	}
}

// TestMemoryStore_ConcurrentFlush verifies Flush does not race with concurrent
// Setters — the store must be consistent (not nil) after Flush completes.
func TestMemoryStore_ConcurrentFlush(t *testing.T) {
	s := NewMemoryStore()
	var wg sync.WaitGroup
	const writers = 100

	wg.Add(writers + 1)

	// Writers hammer the store.
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			s.Set(fmt.Sprintf("k%d", i), "v")
		}(i)
	}

	// One goroutine flushes mid-write.
	go func() {
		defer wg.Done()
		s.Flush()
	}()

	wg.Wait()

	// After everything settles the store must be usable.
	s.Set("post-flush", "ok")
	v, ok := s.Get("post-flush")
	if !ok || v != "ok" {
		t.Error("store should be usable after concurrent Flush")
	}
}

// ── Benchmarks ────────────────────────────────────────────────────────────────

func BenchmarkMemoryStore_Set(b *testing.B) {
	s := NewMemoryStore()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Set("key", "value")
	}
}

func BenchmarkMemoryStore_Get_Hit(b *testing.B) {
	s := NewMemoryStore()
	s.Set("key", "value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Get("key")
	}
}

func BenchmarkMemoryStore_Get_Miss(b *testing.B) {
	s := NewMemoryStore()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Get("missing")
	}
}

func BenchmarkMemoryStore_Set_Parallel(b *testing.B) {
	s := NewMemoryStore()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Set("key", "value")
		}
	})
}

func BenchmarkMemoryStore_Get_Parallel(b *testing.B) {
	s := NewMemoryStore()
	s.Set("key", "value")
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Get("key")
		}
	})
}
