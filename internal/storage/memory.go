package storage

import (
	"path/filepath"
	"sync"
)

// MemoryStore is a thread-safe in-memory key-value store backed by a Go map.
// It satisfies the Store interface and is the primary storage engine for Phase 1.
//
// Concurrency model:
//   - sync.RWMutex allows multiple concurrent readers (GET, EXISTS, KEYS)
//   - Writers (SET, DEL) acquire an exclusive write lock
//   - This is appropriate for read-heavy workloads, which is typical for caches
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemoryStore allocates and returns a new empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]string),
	}
}

// Set stores value under key. Overwrites any existing value.
// Acquires a write lock.
func (s *MemoryStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

// Get returns the value for key and true, or ("", false) if the key does not
// exist. Acquires a read lock.
func (s *MemoryStore) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

// Del removes the given keys and returns the count of keys that actually
// existed and were removed. Acquires a write lock.
func (s *MemoryStore) Del(keys ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range keys {
		if _, ok := s.data[k]; ok {
			delete(s.data, k)
			n++
		}
	}
	return n
}

// Exists returns the number of the given keys that currently exist.
// A key repeated N times is counted N times. Acquires a read lock.
func (s *MemoryStore) Exists(keys ...string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, k := range keys {
		if _, ok := s.data[k]; ok {
			n++
		}
	}
	return n
}

// Keys returns all keys matching the glob-style pattern.
// Uses filepath.Match for pattern matching — supports *, ?, and [ranges].
// Acquires a read lock.
func (s *MemoryStore) Keys(pattern string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []string
	for k := range s.data {
		matched, err := filepath.Match(pattern, k)
		if err == nil && matched {
			result = append(result, k)
		}
	}
	return result
}

// Len returns the total number of keys currently stored.
// Acquires a read lock.
func (s *MemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

// Flush removes all keys from the store by replacing the internal map with a
// fresh allocation. Acquires a write lock.
//
// A new map allocation is used rather than iterating and deleting, because:
//   - It is O(1) regardless of the number of keys
//   - It releases all existing bucket memory to the GC immediately
//   - It avoids holding the write lock for O(n) time on large datasets
func (s *MemoryStore) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]string)
}
