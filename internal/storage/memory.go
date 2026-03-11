package storage

import (
	"context"
	"path/filepath"
	"sync"
	"time"
)

// strEntry holds a string value together with an optional expiration time.
// A zero expiresAt means the entry never expires.
type strEntry struct {
	value     string
	expiresAt time.Time
}

// MemoryStore is a thread-safe in-memory store that supports string and hash
// data types with optional key expiration.
//
// Concurrency model:
//   - sync.RWMutex allows multiple concurrent readers (GET, EXISTS, KEYS, …).
//   - Writers (SET, DEL, EXPIRE, …) acquire an exclusive write lock.
//   - A background goroutine performs active expiry cleanup once per second.
//     Lazy expiry is also performed on every read so callers never observe
//     a stale value even if the cleanup goroutine has not run yet.
type MemoryStore struct {
	mu      sync.RWMutex
	strings map[string]strEntry
	hashes  map[string]map[string]string
	hashExp map[string]time.Time // per-key expiry for hash values
}

// NewMemoryStore allocates and returns a new empty MemoryStore.
// Call StartCleanup(ctx) to enable background expiry of TTL-ed keys.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		strings: make(map[string]strEntry),
		hashes:  make(map[string]map[string]string),
		hashExp: make(map[string]time.Time),
	}
}

// StartCleanup launches a background goroutine that deletes expired keys once
// per second. It exits when ctx is cancelled. Call this after creating the
// store so that expired keys are reclaimed even when not accessed.
func (s *MemoryStore) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.deleteExpired()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// deleteExpired sweeps both maps under a write lock and removes all entries
// whose TTL has passed.
func (s *MemoryStore) deleteExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.strings {
		if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
			delete(s.strings, k)
		}
	}
	for k, exp := range s.hashExp {
		if now.After(exp) {
			delete(s.hashes, k)
			delete(s.hashExp, k)
		}
	}
}

// expired reports whether the given absolute time has passed.
// A zero time is treated as "never expires".
func expired(t time.Time) bool {
	return !t.IsZero() && time.Now().After(t)
}

// ── String operations ────────────────────────────────────────────────────────

// Set stores value under key. Overwrites any existing value and clears any TTL.
func (s *MemoryStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strings[key] = strEntry{value: value}
	// Overwriting a string key removes any hash stored under the same name.
	delete(s.hashes, key)
	delete(s.hashExp, key)
}

// SetWithTTL stores value under key with an expiration after ttl.
func (s *MemoryStore) SetWithTTL(key, value string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strings[key] = strEntry{value: value, expiresAt: time.Now().Add(ttl)}
	delete(s.hashes, key)
	delete(s.hashExp, key)
}

// Get returns the value for key and true, or ("", false) if the key does not
// exist or has expired. Acquires a read lock.
func (s *MemoryStore) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.strings[key]
	if !ok || expired(e.expiresAt) {
		return "", false
	}
	return e.value, true
}

// Del removes the given keys (any type) and returns the count removed.
func (s *MemoryStore) Del(keys ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range keys {
		if e, ok := s.strings[k]; ok && !expired(e.expiresAt) {
			delete(s.strings, k)
			n++
			continue
		}
		if _, ok := s.hashes[k]; ok && !expired(s.hashExp[k]) {
			delete(s.hashes, k)
			delete(s.hashExp, k)
			n++
		}
	}
	return n
}

// Exists returns the number of given keys that currently exist (any type).
// A key repeated N times counts N times.
func (s *MemoryStore) Exists(keys ...string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, k := range keys {
		if e, ok := s.strings[k]; ok && !expired(e.expiresAt) {
			n++
			continue
		}
		if _, ok := s.hashes[k]; ok && !expired(s.hashExp[k]) {
			n++
		}
	}
	return n
}

// Keys returns all non-expired keys matching the glob-style pattern.
func (s *MemoryStore) Keys(pattern string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	var result []string
	for k, e := range s.strings {
		if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
			continue
		}
		if matched, _ := filepath.Match(pattern, k); matched {
			result = append(result, k)
		}
	}
	for k := range s.hashes {
		if exp, hasExp := s.hashExp[k]; hasExp && now.After(exp) {
			continue
		}
		if matched, _ := filepath.Match(pattern, k); matched {
			result = append(result, k)
		}
	}
	return result
}

// Len returns the total number of non-expired keys across all data types.
func (s *MemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	n := 0
	for _, e := range s.strings {
		if e.expiresAt.IsZero() || now.Before(e.expiresAt) {
			n++
		}
	}
	for k := range s.hashes {
		if exp, hasExp := s.hashExp[k]; !hasExp || now.Before(exp) {
			n++
		}
	}
	return n
}

// Flush removes all keys from the store by replacing both internal maps.
func (s *MemoryStore) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strings = make(map[string]strEntry)
	s.hashes = make(map[string]map[string]string)
	s.hashExp = make(map[string]time.Time)
}

// ── Expiry operations ────────────────────────────────────────────────────────

// Expire sets a TTL on key. Returns true if the key exists and the TTL was set.
func (s *MemoryStore) Expire(key string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.strings[key]; ok && !expired(e.expiresAt) {
		e.expiresAt = time.Now().Add(ttl)
		s.strings[key] = e
		return true
	}
	if _, ok := s.hashes[key]; ok && !expired(s.hashExp[key]) {
		s.hashExp[key] = time.Now().Add(ttl)
		return true
	}
	return false
}

// TTL returns the remaining time-to-live for a key.
// Returns -1*time.Second if key has no TTL, -2*time.Second if key not found.
func (s *MemoryStore) TTL(key string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.strings[key]; ok {
		if expired(e.expiresAt) {
			return -2 * time.Second
		}
		if e.expiresAt.IsZero() {
			return -1 * time.Second
		}
		return time.Until(e.expiresAt)
	}
	if _, ok := s.hashes[key]; ok {
		exp, hasExp := s.hashExp[key]
		if hasExp && expired(exp) {
			return -2 * time.Second
		}
		if !hasExp {
			return -1 * time.Second
		}
		return time.Until(exp)
	}
	return -2 * time.Second
}

// Persist removes any TTL from key. Returns true if the TTL was removed.
func (s *MemoryStore) Persist(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.strings[key]; ok && !expired(e.expiresAt) && !e.expiresAt.IsZero() {
		e.expiresAt = time.Time{}
		s.strings[key] = e
		return true
	}
	if _, ok := s.hashes[key]; ok && !expired(s.hashExp[key]) {
		if _, hasExp := s.hashExp[key]; hasExp {
			delete(s.hashExp, key)
			return true
		}
	}
	return false
}

// ── Atomic rename ────────────────────────────────────────────────────────────

// Rename atomically moves the value at src to dst, overwriting any existing
// value at dst. Returns false if src does not exist or has expired.
func (s *MemoryStore) Rename(src, dst string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check string type at src.
	if e, ok := s.strings[src]; ok && !expired(e.expiresAt) {
		// Copy TTL: if src had a TTL, preserve it on dst.
		s.strings[dst] = e
		delete(s.strings, src)
		// Clear any hash at dst.
		delete(s.hashes, dst)
		delete(s.hashExp, dst)
		return true
	}

	// Check hash type at src.
	if srcHash, ok := s.hashes[src]; ok && !expired(s.hashExp[src]) {
		// Deep-copy the hash to dst.
		dstHash := make(map[string]string, len(srcHash))
		for f, v := range srcHash {
			dstHash[f] = v
		}
		s.hashes[dst] = dstHash
		if exp, hasExp := s.hashExp[src]; hasExp {
			s.hashExp[dst] = exp
		} else {
			delete(s.hashExp, dst)
		}
		delete(s.hashes, src)
		delete(s.hashExp, src)
		// Clear any string at dst.
		delete(s.strings, dst)
		return true
	}

	return false
}

// ── Hash operations ──────────────────────────────────────────────────────────

// HSet sets the given fields in the hash at key.
// Returns the number of new fields added (existing fields updated do not count).
func (s *MemoryStore) HSet(key string, fields map[string]string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A string with the same key is replaced by the hash.
	delete(s.strings, key)
	if s.hashes[key] == nil {
		s.hashes[key] = make(map[string]string, len(fields))
	}
	added := 0
	for f, v := range fields {
		if _, exists := s.hashes[key][f]; !exists {
			added++
		}
		s.hashes[key][f] = v
	}
	return added
}

// HGet returns the value of field in the hash at key.
func (s *MemoryStore) HGet(key, field string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || expired(s.hashExp[key]) {
		return "", false
	}
	v, found := h[field]
	return v, found
}

// HDel removes fields from the hash at key and returns the count removed.
// If the hash becomes empty it is deleted from the store.
func (s *MemoryStore) HDel(key string, fields ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hashes[key]
	if !ok || expired(s.hashExp[key]) {
		return 0
	}
	n := 0
	for _, f := range fields {
		if _, exists := h[f]; exists {
			delete(h, f)
			n++
		}
	}
	if len(h) == 0 {
		delete(s.hashes, key)
		delete(s.hashExp, key)
	}
	return n
}

// HGetAll returns a copy of all field-value pairs in the hash at key,
// or nil if the key does not exist or has expired.
func (s *MemoryStore) HGetAll(key string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || expired(s.hashExp[key]) {
		return nil
	}
	result := make(map[string]string, len(h))
	for f, v := range h {
		result[f] = v
	}
	return result
}

// HLen returns the number of fields in the hash at key.
func (s *MemoryStore) HLen(key string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || expired(s.hashExp[key]) {
		return 0
	}
	return len(h)
}

// HExists reports whether field exists in the hash at key.
func (s *MemoryStore) HExists(key, field string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || expired(s.hashExp[key]) {
		return false
	}
	_, exists := h[field]
	return exists
}

// HKeys returns all field names in the hash at key.
func (s *MemoryStore) HKeys(key string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || expired(s.hashExp[key]) {
		return nil
	}
	keys := make([]string, 0, len(h))
	for f := range h {
		keys = append(keys, f)
	}
	return keys
}

// HVals returns all values in the hash at key.
func (s *MemoryStore) HVals(key string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || expired(s.hashExp[key]) {
		return nil
	}
	vals := make([]string, 0, len(h))
	for _, v := range h {
		vals = append(vals, v)
	}
	return vals
}

// ── Type introspection ───────────────────────────────────────────────────────

// Type returns the Redis type name for key: "string", "hash", or "none".
func (s *MemoryStore) Type(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.strings[key]; ok && !expired(e.expiresAt) {
		return "string"
	}
	if _, ok := s.hashes[key]; ok && !expired(s.hashExp[key]) {
		return "hash"
	}
	return "none"
}
