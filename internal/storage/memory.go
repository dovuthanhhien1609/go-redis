package storage

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// strEntry holds a string value together with an optional expiration time.
// A zero expiresAt means the entry never expires.
type strEntry struct {
	value     string
	expiresAt time.Time
}

// MemoryStore is a thread-safe in-memory store that supports string, hash,
// list, set, and sorted set data types with optional key expiration.
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
	lists   map[string][]string
	sets    map[string]map[string]struct{}
	zsets   map[string]*zset

	// keyExpiry stores TTLs for hash/list/set/zset keys.
	// String TTLs are stored inside strEntry.
	keyExpiry map[string]time.Time

	// keyVersion is incremented on every write to a key (used by WATCH).
	keyVersion map[string]uint64
}

// zset is the in-memory sorted set type.
// Scores are stored in a map for O(1) lookup; range queries sort on demand.
type zset struct {
	scores map[string]float64 // member -> score
}

// NewMemoryStore allocates and returns a new empty MemoryStore.
// Call StartCleanup(ctx) to enable background expiry of TTL-ed keys.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		strings:    make(map[string]strEntry),
		hashes:     make(map[string]map[string]string),
		lists:      make(map[string][]string),
		sets:       make(map[string]map[string]struct{}),
		zsets:      make(map[string]*zset),
		keyExpiry:  make(map[string]time.Time),
		keyVersion: make(map[string]uint64),
	}
}

// StartCleanup launches a background goroutine that deletes expired keys once
// per second. It exits when ctx is cancelled.
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

// deleteExpired sweeps all maps under a write lock and removes expired entries.
func (s *MemoryStore) deleteExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.strings {
		if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
			delete(s.strings, k)
			delete(s.keyVersion, k)
		}
	}
	for k, exp := range s.keyExpiry {
		if now.After(exp) {
			delete(s.hashes, k)
			delete(s.lists, k)
			delete(s.sets, k)
			delete(s.zsets, k)
			delete(s.keyExpiry, k)
			delete(s.keyVersion, k)
		}
	}
}

// expired reports whether the given absolute time has passed.
// A zero time is treated as "never expires".
func expired(t time.Time) bool {
	return !t.IsZero() && time.Now().After(t)
}

// keyExpiredNonStr returns true if a non-string key has expired.
func (s *MemoryStore) keyExpiredNonStr(key string) bool {
	exp, ok := s.keyExpiry[key]
	return ok && expired(exp)
}

// bumpVersion increments the version counter for key (must hold write lock).
func (s *MemoryStore) bumpVersion(key string) {
	s.keyVersion[key]++
}

// deleteKey removes key from all type maps (must hold write lock).
func (s *MemoryStore) deleteKey(key string) {
	delete(s.strings, key)
	delete(s.hashes, key)
	delete(s.lists, key)
	delete(s.sets, key)
	delete(s.zsets, key)
	delete(s.keyExpiry, key)
}

// typeOf returns the internal type tag for key (must hold at least read lock).
// Returns "" if the key does not exist or has expired.
func (s *MemoryStore) typeOf(key string) string {
	if e, ok := s.strings[key]; ok && !expired(e.expiresAt) {
		return "string"
	}
	if _, ok := s.hashes[key]; ok && !s.keyExpiredNonStr(key) {
		return "hash"
	}
	if _, ok := s.lists[key]; ok && !s.keyExpiredNonStr(key) {
		return "list"
	}
	if _, ok := s.sets[key]; ok && !s.keyExpiredNonStr(key) {
		return "set"
	}
	if _, ok := s.zsets[key]; ok && !s.keyExpiredNonStr(key) {
		return "zset"
	}
	return ""
}

// normaliseIndex converts a Redis list index (negative = from tail) to a
// non-negative index, returning -1 if out of range.
func normaliseIndex(idx, length int) int {
	if idx < 0 {
		idx = length + idx
	}
	if idx < 0 || idx >= length {
		return -1
	}
	return idx
}

// clampRange converts Redis-style start/stop for LRANGE / LTRIM into a valid
// [lo, hi] pair (inclusive). Returns lo > hi if the range is empty.
func clampRange(start, stop, length int) (int, int) {
	if start < 0 {
		start = length + start
	}
	if stop < 0 {
		stop = length + stop
	}
	if start < 0 {
		start = 0
	}
	if stop >= length {
		stop = length - 1
	}
	return start, stop
}

// ── String operations ────────────────────────────────────────────────────────

// Set stores value under key, overwriting any existing value and clearing TTL.
func (s *MemoryStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteKey(key)
	s.strings[key] = strEntry{value: value}
	s.bumpVersion(key)
}

// SetWithTTL stores value under key with an expiration after ttl.
func (s *MemoryStore) SetWithTTL(key, value string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteKey(key)
	s.strings[key] = strEntry{value: value, expiresAt: time.Now().Add(ttl)}
	s.bumpVersion(key)
}

// SetAdv atomically implements SET with NX/XX/GET/KEEPTTL options.
func (s *MemoryStore) SetAdv(key, value string, ttl time.Duration, opts SetAdvOpts) (string, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Capture old value if requested.
	var oldVal string
	var oldExists bool
	if e, ok := s.strings[key]; ok && !expired(e.expiresAt) {
		oldVal = e.value
		oldExists = true
	}

	// NX: only set if key does not exist.
	if opts.NX && oldExists {
		return oldVal, oldExists, false
	}
	// XX: only set if key already exists.
	if opts.XX && !oldExists {
		return oldVal, oldExists, false
	}

	// Determine new expiry.
	var newExpiry time.Time
	if opts.KeepTTL {
		if e, ok := s.strings[key]; ok {
			newExpiry = e.expiresAt
		}
	} else if ttl > 0 {
		newExpiry = time.Now().Add(ttl)
	}

	s.deleteKey(key)
	s.strings[key] = strEntry{value: value, expiresAt: newExpiry}
	s.bumpVersion(key)
	return oldVal, oldExists, true
}

// GetEx returns the value for key and optionally modifies its TTL.
func (s *MemoryStore) GetEx(key string, ttl time.Duration, persist, hasTTL bool) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.strings[key]
	if !ok || expired(e.expiresAt) {
		return "", false
	}
	if persist {
		e.expiresAt = time.Time{}
		s.strings[key] = e
	} else if hasTTL {
		e.expiresAt = time.Now().Add(ttl)
		s.strings[key] = e
	}
	return e.value, true
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
		if s.typeOf(k) != "" {
			s.deleteKey(k)
			s.bumpVersion(k)
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
		if s.typeOf(k) != "" {
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
	add := func(k string) {
		if matched, _ := filepath.Match(pattern, k); matched {
			result = append(result, k)
		}
	}
	for k, e := range s.strings {
		if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
			continue
		}
		add(k)
	}
	for k := range s.hashes {
		if s.keyExpiredNonStr(k) {
			continue
		}
		add(k)
	}
	for k := range s.lists {
		if s.keyExpiredNonStr(k) {
			continue
		}
		add(k)
	}
	for k := range s.sets {
		if s.keyExpiredNonStr(k) {
			continue
		}
		add(k)
	}
	for k := range s.zsets {
		if s.keyExpiredNonStr(k) {
			continue
		}
		add(k)
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
		if !s.keyExpiredNonStr(k) {
			n++
		}
	}
	for k := range s.lists {
		if !s.keyExpiredNonStr(k) {
			n++
		}
	}
	for k := range s.sets {
		if !s.keyExpiredNonStr(k) {
			n++
		}
	}
	for k := range s.zsets {
		if !s.keyExpiredNonStr(k) {
			n++
		}
	}
	return n
}

// Flush removes all keys from the store by replacing all internal maps.
func (s *MemoryStore) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strings = make(map[string]strEntry)
	s.hashes = make(map[string]map[string]string)
	s.lists = make(map[string][]string)
	s.sets = make(map[string]map[string]struct{})
	s.zsets = make(map[string]*zset)
	s.keyExpiry = make(map[string]time.Time)
	s.keyVersion = make(map[string]uint64)
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
	t := s.typeOf(key)
	if t == "hash" || t == "list" || t == "set" || t == "zset" {
		s.keyExpiry[key] = time.Now().Add(ttl)
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
	t := s.typeOf(key)
	if t == "" {
		return -2 * time.Second
	}
	exp, hasExp := s.keyExpiry[key]
	if !hasExp {
		return -1 * time.Second
	}
	if expired(exp) {
		return -2 * time.Second
	}
	return time.Until(exp)
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
	t := s.typeOf(key)
	if t == "hash" || t == "list" || t == "set" || t == "zset" {
		if _, hasExp := s.keyExpiry[key]; hasExp {
			delete(s.keyExpiry, key)
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

	srcType := s.typeOf(src)
	if srcType == "" {
		return false
	}

	// Remove whatever is at dst.
	s.deleteKey(dst)

	switch srcType {
	case "string":
		e := s.strings[src]
		s.strings[dst] = e
		delete(s.strings, src)

	case "hash":
		h := s.hashes[src]
		dst_h := make(map[string]string, len(h))
		for f, v := range h {
			dst_h[f] = v
		}
		s.hashes[dst] = dst_h
		delete(s.hashes, src)

	case "list":
		l := make([]string, len(s.lists[src]))
		copy(l, s.lists[src])
		s.lists[dst] = l
		delete(s.lists, src)

	case "set":
		st := make(map[string]struct{}, len(s.sets[src]))
		for m := range s.sets[src] {
			st[m] = struct{}{}
		}
		s.sets[dst] = st
		delete(s.sets, src)

	case "zset":
		z := s.zsets[src]
		dst_z := &zset{scores: make(map[string]float64, len(z.scores))}
		for m, sc := range z.scores {
			dst_z.scores[m] = sc
		}
		s.zsets[dst] = dst_z
		delete(s.zsets, src)
	}

	if exp, ok := s.keyExpiry[src]; ok {
		s.keyExpiry[dst] = exp
		delete(s.keyExpiry, src)
	} else {
		delete(s.keyExpiry, dst)
	}

	s.bumpVersion(dst)
	s.bumpVersion(src)
	return true
}

// ── Hash operations ──────────────────────────────────────────────────────────

// HSet sets the given fields in the hash at key.
func (s *MemoryStore) HSet(key string, fields map[string]string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.bumpVersion(key)
	return added
}

// HGet returns the value of field in the hash at key.
func (s *MemoryStore) HGet(key, field string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || s.keyExpiredNonStr(key) {
		return "", false
	}
	v, found := h[field]
	return v, found
}

// HDel removes fields from the hash at key and returns the count removed.
func (s *MemoryStore) HDel(key string, fields ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.hashes[key]
	if !ok || s.keyExpiredNonStr(key) {
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
		delete(s.keyExpiry, key)
		delete(s.keyVersion, key)
	} else {
		s.bumpVersion(key)
	}
	return n
}

// HGetAll returns a copy of all field-value pairs in the hash at key.
func (s *MemoryStore) HGetAll(key string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || s.keyExpiredNonStr(key) {
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
	if !ok || s.keyExpiredNonStr(key) {
		return 0
	}
	return len(h)
}

// HExists reports whether field exists in the hash at key.
func (s *MemoryStore) HExists(key, field string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || s.keyExpiredNonStr(key) {
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
	if !ok || s.keyExpiredNonStr(key) {
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
	if !ok || s.keyExpiredNonStr(key) {
		return nil
	}
	vals := make([]string, 0, len(h))
	for _, v := range h {
		vals = append(vals, v)
	}
	return vals
}

// ── List operations ──────────────────────────────────────────────────────────

// getList returns the list at key, nil if absent/expired.
// Requires at least a read lock. Returns ErrWrongType if the key is another type.
func (s *MemoryStore) getList(key string) ([]string, error) {
	t := s.typeOf(key)
	if t == "" {
		return nil, nil
	}
	if t != "list" {
		return nil, ErrWrongType
	}
	return s.lists[key], nil
}

// LPush prepends values to the list at key (each value is prepended, so the
// first argument ends up as the head after all insertions).
func (s *MemoryStore) LPush(key string, values ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.typeOf(key); t != "" && t != "list" {
		return 0, ErrWrongType
	}
	l := s.lists[key]
	// Prepend in order: LPUSH k a b c → [c, b, a, ...existing]
	prepend := make([]string, len(values))
	for i, v := range values {
		prepend[len(values)-1-i] = v
	}
	s.lists[key] = append(prepend, l...)
	s.bumpVersion(key)
	return len(s.lists[key]), nil
}

// RPush appends values to the list at key.
func (s *MemoryStore) RPush(key string, values ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.typeOf(key); t != "" && t != "list" {
		return 0, ErrWrongType
	}
	s.lists[key] = append(s.lists[key], values...)
	s.bumpVersion(key)
	return len(s.lists[key]), nil
}

// LPop removes count elements from the head and returns them.
func (s *MemoryStore) LPop(key string, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := s.getList(key)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, nil
	}
	if count > len(l) {
		count = len(l)
	}
	result := make([]string, count)
	copy(result, l)
	s.lists[key] = l[count:]
	if len(s.lists[key]) == 0 {
		delete(s.lists, key)
		delete(s.keyExpiry, key)
	}
	s.bumpVersion(key)
	return result, nil
}

// RPop removes count elements from the tail and returns them.
func (s *MemoryStore) RPop(key string, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := s.getList(key)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, nil
	}
	if count > len(l) {
		count = len(l)
	}
	result := make([]string, count)
	for i := 0; i < count; i++ {
		result[i] = l[len(l)-count+i]
	}
	s.lists[key] = l[:len(l)-count]
	if len(s.lists[key]) == 0 {
		delete(s.lists, key)
		delete(s.keyExpiry, key)
	}
	s.bumpVersion(key)
	return result, nil
}

// LLen returns the length of the list.
func (s *MemoryStore) LLen(key string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, err := s.getList(key)
	if err != nil {
		return 0, err
	}
	return len(l), nil
}

// LRange returns elements in [start, stop].
func (s *MemoryStore) LRange(key string, start, stop int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, err := s.getList(key)
	if err != nil {
		return nil, err
	}
	if len(l) == 0 {
		return []string{}, nil
	}
	lo, hi := clampRange(start, stop, len(l))
	if lo > hi {
		return []string{}, nil
	}
	result := make([]string, hi-lo+1)
	copy(result, l[lo:hi+1])
	return result, nil
}

// LIndex returns the element at the given index.
func (s *MemoryStore) LIndex(key string, index int) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, err := s.getList(key)
	if err != nil {
		return "", false, err
	}
	idx := normaliseIndex(index, len(l))
	if idx < 0 {
		return "", false, nil
	}
	return l[idx], true, nil
}

// LSet sets the element at the given index.
func (s *MemoryStore) LSet(key string, index int, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.typeOf(key)
	if t == "" {
		return fmt.Errorf("ERR no such key")
	}
	if t != "list" {
		return ErrWrongType
	}
	l := s.lists[key]
	idx := normaliseIndex(index, len(l))
	if idx < 0 {
		return fmt.Errorf("ERR index out of range")
	}
	s.lists[key][idx] = value
	s.bumpVersion(key)
	return nil
}

// LInsert inserts value before or after the first occurrence of pivot.
func (s *MemoryStore) LInsert(key string, before bool, pivot, value string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.typeOf(key)
	if t == "" {
		return 0, nil
	}
	if t != "list" {
		return 0, ErrWrongType
	}
	l := s.lists[key]
	pos := -1
	for i, v := range l {
		if v == pivot {
			pos = i
			break
		}
	}
	if pos < 0 {
		return -1, nil
	}
	if !before {
		pos++
	}
	// Insert at pos.
	newList := make([]string, len(l)+1)
	copy(newList, l[:pos])
	newList[pos] = value
	copy(newList[pos+1:], l[pos:])
	s.lists[key] = newList
	s.bumpVersion(key)
	return len(newList), nil
}

// LRem removes count occurrences of value.
func (s *MemoryStore) LRem(key string, count int, value string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := s.getList(key)
	if err != nil {
		return 0, err
	}
	if l == nil {
		return 0, nil
	}
	removed := 0
	abs := count
	if abs < 0 {
		abs = -abs
	}
	newList := make([]string, 0, len(l))
	if count >= 0 {
		// Remove from head.
		for _, v := range l {
			if v == value && (count == 0 || removed < abs) {
				removed++
			} else {
				newList = append(newList, v)
			}
		}
	} else {
		// Remove from tail: reverse, remove from head, reverse back.
		for i := len(l) - 1; i >= 0; i-- {
			if l[i] == value && removed < abs {
				removed++
			} else {
				newList = append([]string{l[i]}, newList...)
			}
		}
	}
	if removed > 0 {
		if len(newList) == 0 {
			delete(s.lists, key)
			delete(s.keyExpiry, key)
		} else {
			s.lists[key] = newList
		}
		s.bumpVersion(key)
	}
	return removed, nil
}

// LTrim trims the list to only contain elements in [start, stop].
func (s *MemoryStore) LTrim(key string, start, stop int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.typeOf(key)
	if t == "" {
		return nil
	}
	if t != "list" {
		return ErrWrongType
	}
	l := s.lists[key]
	lo, hi := clampRange(start, stop, len(l))
	if lo > hi {
		delete(s.lists, key)
		delete(s.keyExpiry, key)
	} else {
		trimmed := make([]string, hi-lo+1)
		copy(trimmed, l[lo:hi+1])
		s.lists[key] = trimmed
	}
	s.bumpVersion(key)
	return nil
}

// LMove atomically pops from src and pushes to dst.
func (s *MemoryStore) LMove(src, dst string, srcLeft, dstLeft bool) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.typeOf(src); t != "" && t != "list" {
		return "", false, ErrWrongType
	}
	if t := s.typeOf(dst); t != "" && t != "list" {
		return "", false, ErrWrongType
	}
	l := s.lists[src]
	if len(l) == 0 {
		return "", false, nil
	}
	var elem string
	if srcLeft {
		elem = l[0]
		s.lists[src] = l[1:]
	} else {
		elem = l[len(l)-1]
		s.lists[src] = l[:len(l)-1]
	}
	if len(s.lists[src]) == 0 {
		delete(s.lists, src)
		delete(s.keyExpiry, src)
	}
	if dstLeft {
		s.lists[dst] = append([]string{elem}, s.lists[dst]...)
	} else {
		s.lists[dst] = append(s.lists[dst], elem)
	}
	s.bumpVersion(src)
	s.bumpVersion(dst)
	return elem, true, nil
}

// ── Set operations ───────────────────────────────────────────────────────────

// getSet returns the set at key. Requires at least a read lock.
func (s *MemoryStore) getSet(key string) (map[string]struct{}, error) {
	t := s.typeOf(key)
	if t == "" {
		return nil, nil
	}
	if t != "set" {
		return nil, ErrWrongType
	}
	return s.sets[key], nil
}

// SAdd adds members to the set at key.
func (s *MemoryStore) SAdd(key string, members ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.typeOf(key); t != "" && t != "set" {
		return 0, ErrWrongType
	}
	if s.sets[key] == nil {
		s.sets[key] = make(map[string]struct{})
	}
	added := 0
	for _, m := range members {
		if _, exists := s.sets[key][m]; !exists {
			s.sets[key][m] = struct{}{}
			added++
		}
	}
	s.bumpVersion(key)
	return added, nil
}

// SRem removes members from the set at key.
func (s *MemoryStore) SRem(key string, members ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getSet(key)
	if err != nil {
		return 0, err
	}
	if st == nil {
		return 0, nil
	}
	n := 0
	for _, m := range members {
		if _, ok := st[m]; ok {
			delete(st, m)
			n++
		}
	}
	if len(st) == 0 {
		delete(s.sets, key)
		delete(s.keyExpiry, key)
	}
	s.bumpVersion(key)
	return n, nil
}

// SMembers returns all members of the set.
func (s *MemoryStore) SMembers(key string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.getSet(key)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return []string{}, nil
	}
	result := make([]string, 0, len(st))
	for m := range st {
		result = append(result, m)
	}
	return result, nil
}

// SCard returns the set cardinality.
func (s *MemoryStore) SCard(key string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.getSet(key)
	if err != nil {
		return 0, err
	}
	return len(st), nil
}

// SIsMember reports whether member belongs to the set.
func (s *MemoryStore) SIsMember(key, member string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.getSet(key)
	if err != nil {
		return false, err
	}
	_, ok := st[member]
	return ok, nil
}

// SMIsMember returns membership flags for multiple members.
func (s *MemoryStore) SMIsMember(key string, members ...string) ([]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.getSet(key)
	if err != nil {
		return nil, err
	}
	result := make([]int64, len(members))
	for i, m := range members {
		if _, ok := st[m]; ok {
			result[i] = 1
		}
	}
	return result, nil
}

// SInter returns the intersection of the sets at the given keys.
func (s *MemoryStore) SInter(keys ...string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.setInter(keys...)
}

func (s *MemoryStore) setInter(keys ...string) ([]string, error) {
	if len(keys) == 0 {
		return []string{}, nil
	}
	first, err := s.getSet(keys[0])
	if err != nil {
		return nil, err
	}
	if first == nil {
		return []string{}, nil
	}
	var result []string
	for m := range first {
		inAll := true
		for _, k := range keys[1:] {
			st, err := s.getSet(k)
			if err != nil {
				return nil, err
			}
			if _, ok := st[m]; !ok {
				inAll = false
				break
			}
		}
		if inAll {
			result = append(result, m)
		}
	}
	return result, nil
}

// SUnion returns the union of the sets.
func (s *MemoryStore) SUnion(keys ...string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.setUnion(keys...)
}

func (s *MemoryStore) setUnion(keys ...string) ([]string, error) {
	seen := make(map[string]struct{})
	for _, k := range keys {
		st, err := s.getSet(k)
		if err != nil {
			return nil, err
		}
		for m := range st {
			seen[m] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for m := range seen {
		result = append(result, m)
	}
	return result, nil
}

// SDiff returns the difference: members in keys[0] not in any other key.
func (s *MemoryStore) SDiff(keys ...string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.setDiff(keys...)
}

func (s *MemoryStore) setDiff(keys ...string) ([]string, error) {
	if len(keys) == 0 {
		return []string{}, nil
	}
	first, err := s.getSet(keys[0])
	if err != nil {
		return nil, err
	}
	var result []string
	for m := range first {
		inOther := false
		for _, k := range keys[1:] {
			st, err := s.getSet(k)
			if err != nil {
				return nil, err
			}
			if _, ok := st[m]; ok {
				inOther = true
				break
			}
		}
		if !inOther {
			result = append(result, m)
		}
	}
	return result, nil
}

// SInterStore stores the intersection at dst.
func (s *MemoryStore) SInterStore(dst string, keys ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	members, err := s.setInter(keys...)
	if err != nil {
		return 0, err
	}
	s.deleteKey(dst)
	if len(members) > 0 {
		st := make(map[string]struct{}, len(members))
		for _, m := range members {
			st[m] = struct{}{}
		}
		s.sets[dst] = st
		s.bumpVersion(dst)
	}
	return len(members), nil
}

// SUnionStore stores the union at dst.
func (s *MemoryStore) SUnionStore(dst string, keys ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	members, err := s.setUnion(keys...)
	if err != nil {
		return 0, err
	}
	s.deleteKey(dst)
	if len(members) > 0 {
		st := make(map[string]struct{}, len(members))
		for _, m := range members {
			st[m] = struct{}{}
		}
		s.sets[dst] = st
		s.bumpVersion(dst)
	}
	return len(members), nil
}

// SDiffStore stores the difference at dst.
func (s *MemoryStore) SDiffStore(dst string, keys ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	members, err := s.setDiff(keys...)
	if err != nil {
		return 0, err
	}
	s.deleteKey(dst)
	if len(members) > 0 {
		st := make(map[string]struct{}, len(members))
		for _, m := range members {
			st[m] = struct{}{}
		}
		s.sets[dst] = st
		s.bumpVersion(dst)
	}
	return len(members), nil
}

// SRandMember returns random members from the set.
func (s *MemoryStore) SRandMember(key string, count int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, err := s.getSet(key)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return []string{}, nil
	}
	all := make([]string, 0, len(st))
	for m := range st {
		all = append(all, m)
	}
	if count == 0 {
		return []string{}, nil
	}
	if count > 0 {
		// Unique members.
		if count >= len(all) {
			return all, nil
		}
		rand.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
		return all[:count], nil
	}
	// Negative count: allow repetition.
	abs := -count
	result := make([]string, abs)
	for i := range result {
		result[i] = all[rand.IntN(len(all))]
	}
	return result, nil
}

// SPop removes and returns random members from the set.
func (s *MemoryStore) SPop(key string, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.getSet(key)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return []string{}, nil
	}
	all := make([]string, 0, len(st))
	for m := range st {
		all = append(all, m)
	}
	if count >= len(all) {
		delete(s.sets, key)
		delete(s.keyExpiry, key)
		s.bumpVersion(key)
		return all, nil
	}
	rand.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	result := all[:count]
	for _, m := range result {
		delete(st, m)
	}
	s.bumpVersion(key)
	return result, nil
}

// SMove moves member from src to dst.
func (s *MemoryStore) SMove(src, dst, member string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.typeOf(src); t != "" && t != "set" {
		return false, ErrWrongType
	}
	if t := s.typeOf(dst); t != "" && t != "set" {
		return false, ErrWrongType
	}
	srcSet := s.sets[src]
	if srcSet == nil {
		return false, nil
	}
	if _, ok := srcSet[member]; !ok {
		return false, nil
	}
	delete(srcSet, member)
	if len(srcSet) == 0 {
		delete(s.sets, src)
		delete(s.keyExpiry, src)
	}
	if s.sets[dst] == nil {
		s.sets[dst] = make(map[string]struct{})
	}
	s.sets[dst][member] = struct{}{}
	s.bumpVersion(src)
	s.bumpVersion(dst)
	return true, nil
}

// ── Sorted set operations ────────────────────────────────────────────────────

// getZSet returns the sorted set at key. Requires at least a read lock.
func (s *MemoryStore) getZSet(key string) (*zset, error) {
	t := s.typeOf(key)
	if t == "" {
		return nil, nil
	}
	if t != "zset" {
		return nil, ErrWrongType
	}
	return s.zsets[key], nil
}

// sortedMembers returns all members of z sorted by (score ASC, member ASC).
func sortedMembers(z *zset) []ZMember {
	members := make([]ZMember, 0, len(z.scores))
	for m, sc := range z.scores {
		members = append(members, ZMember{Member: m, Score: sc})
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Score != members[j].Score {
			return members[i].Score < members[j].Score
		}
		return members[i].Member < members[j].Member
	})
	return members
}

// ZAdd adds or updates members in the sorted set.
func (s *MemoryStore) ZAdd(key string, members []ZMember, nx, xx, gt, lt, ch bool) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.typeOf(key); t != "" && t != "zset" {
		return 0, ErrWrongType
	}
	if s.zsets[key] == nil {
		s.zsets[key] = &zset{scores: make(map[string]float64)}
	}
	z := s.zsets[key]
	var changed int64
	for _, m := range members {
		oldScore, exists := z.scores[m.Member]
		if nx && exists {
			continue
		}
		if xx && !exists {
			continue
		}
		if gt && exists && m.Score <= oldScore {
			continue
		}
		if lt && exists && m.Score >= oldScore {
			continue
		}
		if !exists {
			changed++
		} else if ch && m.Score != oldScore {
			changed++
		}
		z.scores[m.Member] = m.Score
	}
	s.bumpVersion(key)
	return changed, nil
}

// ZRem removes members from the sorted set.
func (s *MemoryStore) ZRem(key string, members ...string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, err := s.getZSet(key)
	if err != nil {
		return 0, err
	}
	if z == nil {
		return 0, nil
	}
	var n int64
	for _, m := range members {
		if _, ok := z.scores[m]; ok {
			delete(z.scores, m)
			n++
		}
	}
	if len(z.scores) == 0 {
		delete(s.zsets, key)
		delete(s.keyExpiry, key)
	}
	s.bumpVersion(key)
	return n, nil
}

// ZScore returns the score of member.
func (s *MemoryStore) ZScore(key, member string) (float64, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return 0, false, err
	}
	if z == nil {
		return 0, false, nil
	}
	sc, ok := z.scores[member]
	return sc, ok, nil
}

// ZCard returns the number of members.
func (s *MemoryStore) ZCard(key string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return 0, err
	}
	return int64(len(z.scores)), nil
}

// ZRank returns the 0-based ascending rank of member.
func (s *MemoryStore) ZRank(key, member string) (int64, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return 0, false, err
	}
	if z == nil {
		return 0, false, nil
	}
	if _, ok := z.scores[member]; !ok {
		return 0, false, nil
	}
	sorted := sortedMembers(z)
	for i, m := range sorted {
		if m.Member == member {
			return int64(i), true, nil
		}
	}
	return 0, false, nil
}

// ZRevRank returns the 0-based descending rank of member.
func (s *MemoryStore) ZRevRank(key, member string) (int64, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return 0, false, err
	}
	if z == nil {
		return 0, false, nil
	}
	if _, ok := z.scores[member]; !ok {
		return 0, false, nil
	}
	sorted := sortedMembers(z)
	n := int64(len(sorted))
	for i, m := range sorted {
		if m.Member == member {
			return n - 1 - int64(i), true, nil
		}
	}
	return 0, false, nil
}

// ZIncrBy increments the score of member.
func (s *MemoryStore) ZIncrBy(key string, increment float64, member string) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.typeOf(key); t != "" && t != "zset" {
		return 0, ErrWrongType
	}
	if s.zsets[key] == nil {
		s.zsets[key] = &zset{scores: make(map[string]float64)}
	}
	s.zsets[key].scores[member] += increment
	s.bumpVersion(key)
	return s.zsets[key].scores[member], nil
}

// ZRange returns members in [start, stop] rank range.
func (s *MemoryStore) ZRange(key string, start, stop int64, rev, withScores bool) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return nil, err
	}
	if z == nil || len(z.scores) == 0 {
		return []string{}, nil
	}
	sorted := sortedMembers(z)
	n := int64(len(sorted))
	// Normalise negative indices.
	if start < 0 {
		start = n + start
	}
	if stop < 0 {
		stop = n + stop
	}
	if start < 0 {
		start = 0
	}
	if stop >= n {
		stop = n - 1
	}
	if start > stop {
		return []string{}, nil
	}
	slice := sorted[start : stop+1]
	if rev {
		// Reverse the slice.
		for i, j := 0, len(slice)-1; i < j; i, j = i+1, j-1 {
			slice[i], slice[j] = slice[j], slice[i]
		}
	}
	var result []string
	for _, m := range slice {
		result = append(result, m.Member)
		if withScores {
			result = append(result, formatScore(m.Score))
		}
	}
	return result, nil
}

// ZRangeByScore returns members with scores in [min, max].
func (s *MemoryStore) ZRangeByScore(key string, min, max float64, minExcl, maxExcl bool, withScores bool, offset, count int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return nil, err
	}
	if z == nil {
		return []string{}, nil
	}
	sorted := sortedMembers(z)
	var result []string
	skipped := int64(0)
	taken := int64(0)
	for _, m := range sorted {
		if minExcl && m.Score <= min {
			continue
		}
		if !minExcl && m.Score < min {
			continue
		}
		if maxExcl && m.Score >= max {
			continue
		}
		if !maxExcl && m.Score > max {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		if count >= 0 && taken >= count {
			break
		}
		result = append(result, m.Member)
		if withScores {
			result = append(result, formatScore(m.Score))
		}
		taken++
	}
	if result == nil {
		return []string{}, nil
	}
	return result, nil
}

// ZRevRangeByScore returns members in descending score order.
func (s *MemoryStore) ZRevRangeByScore(key string, max, min float64, maxExcl, minExcl bool, withScores bool, offset, count int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return nil, err
	}
	if z == nil {
		return []string{}, nil
	}
	sorted := sortedMembers(z)
	// Iterate in reverse.
	var result []string
	skipped := int64(0)
	taken := int64(0)
	for i := len(sorted) - 1; i >= 0; i-- {
		m := sorted[i]
		if maxExcl && m.Score >= max {
			continue
		}
		if !maxExcl && m.Score > max {
			continue
		}
		if minExcl && m.Score <= min {
			continue
		}
		if !minExcl && m.Score < min {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		if count >= 0 && taken >= count {
			break
		}
		result = append(result, m.Member)
		if withScores {
			result = append(result, formatScore(m.Score))
		}
		taken++
	}
	if result == nil {
		return []string{}, nil
	}
	return result, nil
}

// ZCount counts members with scores in [min, max].
func (s *MemoryStore) ZCount(key string, min, max float64, minExcl, maxExcl bool) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return 0, err
	}
	if z == nil {
		return 0, nil
	}
	var n int64
	for _, sc := range z.scores {
		lo := (!minExcl && sc >= min) || (minExcl && sc > min)
		hi := (!maxExcl && sc <= max) || (maxExcl && sc < max)
		if lo && hi {
			n++
		}
	}
	return n, nil
}

// parseLexBound parses a lex range bound.
// Returns (value, exclusive, isNegInf, isPosInf).
func parseLexBound(s string) (string, bool, bool, bool) {
	if s == "-" {
		return "", false, true, false
	}
	if s == "+" {
		return "", false, false, true
	}
	if strings.HasPrefix(s, "(") {
		return s[1:], true, false, false
	}
	if strings.HasPrefix(s, "[") {
		return s[1:], false, false, false
	}
	return s, false, false, false
}

// ZRangeByLex returns members in a lex range (assumes all scores are equal).
func (s *MemoryStore) ZRangeByLex(key, min, max string, offset, count int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, err := s.getZSet(key)
	if err != nil {
		return nil, err
	}
	if z == nil {
		return []string{}, nil
	}
	minVal, minExcl, minNeg, _ := parseLexBound(min)
	maxVal, maxExcl, _, maxPos := parseLexBound(max)
	sorted := sortedMembers(z)
	var result []string
	skipped := int64(0)
	taken := int64(0)
	for _, m := range sorted {
		if !minNeg {
			cmp := strings.Compare(m.Member, minVal)
			if minExcl && cmp <= 0 {
				continue
			}
			if !minExcl && cmp < 0 {
				continue
			}
		}
		if !maxPos {
			cmp := strings.Compare(m.Member, maxVal)
			if maxExcl && cmp >= 0 {
				continue
			}
			if !maxExcl && cmp > 0 {
				continue
			}
		}
		if skipped < offset {
			skipped++
			continue
		}
		if count >= 0 && taken >= count {
			break
		}
		result = append(result, m.Member)
		taken++
	}
	if result == nil {
		return []string{}, nil
	}
	return result, nil
}

// ZLexCount counts members in a lex range.
func (s *MemoryStore) ZLexCount(key, min, max string) (int64, error) {
	members, err := s.ZRangeByLex(key, min, max, 0, -1)
	return int64(len(members)), err
}

// ZPopMin removes and returns the members with the lowest scores.
func (s *MemoryStore) ZPopMin(key string, count int64) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, err := s.getZSet(key)
	if err != nil {
		return nil, err
	}
	if z == nil || len(z.scores) == 0 {
		return []ZMember{}, nil
	}
	sorted := sortedMembers(z)
	if count > int64(len(sorted)) {
		count = int64(len(sorted))
	}
	result := make([]ZMember, count)
	for i := int64(0); i < count; i++ {
		result[i] = sorted[i]
		delete(z.scores, sorted[i].Member)
	}
	if len(z.scores) == 0 {
		delete(s.zsets, key)
		delete(s.keyExpiry, key)
	}
	s.bumpVersion(key)
	return result, nil
}

// ZPopMax removes and returns the members with the highest scores.
func (s *MemoryStore) ZPopMax(key string, count int64) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, err := s.getZSet(key)
	if err != nil {
		return nil, err
	}
	if z == nil || len(z.scores) == 0 {
		return []ZMember{}, nil
	}
	sorted := sortedMembers(z)
	n := int64(len(sorted))
	if count > n {
		count = n
	}
	result := make([]ZMember, count)
	for i := int64(0); i < count; i++ {
		result[i] = sorted[n-1-i]
		delete(z.scores, sorted[n-1-i].Member)
	}
	if len(z.scores) == 0 {
		delete(s.zsets, key)
		delete(s.keyExpiry, key)
	}
	s.bumpVersion(key)
	return result, nil
}

// ZUnionStore stores the union of the input sorted sets at dst.
func (s *MemoryStore) ZUnionStore(dst string, keys []string, weights []float64, aggregate string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scores := make(map[string]float64)
	for i, k := range keys {
		z, err := s.getZSet(k)
		if err != nil {
			return 0, err
		}
		if z == nil {
			continue
		}
		w := float64(1)
		if i < len(weights) {
			w = weights[i]
		}
		for m, sc := range z.scores {
			weighted := sc * w
			cur, exists := scores[m]
			if !exists {
				scores[m] = weighted
			} else {
				scores[m] = applyAggregate(cur, weighted, aggregate)
			}
		}
	}
	s.deleteKey(dst)
	if len(scores) > 0 {
		s.zsets[dst] = &zset{scores: scores}
		s.bumpVersion(dst)
	}
	return int64(len(scores)), nil
}

// ZInterStore stores the intersection of the input sorted sets at dst.
func (s *MemoryStore) ZInterStore(dst string, keys []string, weights []float64, aggregate string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(keys) == 0 {
		return 0, nil
	}
	// Start with members from the first key.
	first, err := s.getZSet(keys[0])
	if err != nil {
		return 0, err
	}
	scores := make(map[string]float64)
	if first != nil {
		w := float64(1)
		if len(weights) > 0 {
			w = weights[0]
		}
		for m, sc := range first.scores {
			scores[m] = sc * w
		}
	}
	for i := 1; i < len(keys); i++ {
		z, err := s.getZSet(keys[i])
		if err != nil {
			return 0, err
		}
		w := float64(1)
		if i < len(weights) {
			w = weights[i]
		}
		newScores := make(map[string]float64)
		for m, sc := range scores {
			if z == nil {
				continue
			}
			if zsc, ok := z.scores[m]; ok {
				newScores[m] = applyAggregate(sc, zsc*w, aggregate)
			}
		}
		scores = newScores
	}
	s.deleteKey(dst)
	if len(scores) > 0 {
		s.zsets[dst] = &zset{scores: scores}
		s.bumpVersion(dst)
	}
	return int64(len(scores)), nil
}

func applyAggregate(a, b float64, agg string) float64 {
	switch strings.ToUpper(agg) {
	case "MIN":
		if b < a {
			return b
		}
		return a
	case "MAX":
		if b > a {
			return b
		}
		return a
	default: // SUM
		return a + b
	}
}

// formatScore converts a float64 score to a Redis-compatible string.
func formatScore(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// ── Cursor-based iteration ───────────────────────────────────────────────────

// allKeys returns a sorted snapshot of all non-expired keys.
func (s *MemoryStore) allKeys() []string {
	now := time.Now()
	var keys []string
	for k, e := range s.strings {
		if e.expiresAt.IsZero() || now.Before(e.expiresAt) {
			keys = append(keys, k)
		}
	}
	for k := range s.hashes {
		if !s.keyExpiredNonStr(k) {
			keys = append(keys, k)
		}
	}
	for k := range s.lists {
		if !s.keyExpiredNonStr(k) {
			keys = append(keys, k)
		}
	}
	for k := range s.sets {
		if !s.keyExpiredNonStr(k) {
			keys = append(keys, k)
		}
	}
	for k := range s.zsets {
		if !s.keyExpiredNonStr(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Scan iterates over all keys matching pattern.
func (s *MemoryStore) Scan(cursor uint64, match string, count int) (uint64, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if count <= 0 {
		count = 10
	}
	keys := s.allKeys()
	total := uint64(len(keys))
	if total == 0 {
		return 0, []string{}
	}
	if cursor >= total {
		cursor = 0
	}
	var result []string
	pos := cursor
	for int(pos-cursor) < count && pos < total {
		k := keys[pos]
		if match == "" || match == "*" {
			result = append(result, k)
		} else if matched, _ := filepath.Match(match, k); matched {
			result = append(result, k)
		}
		pos++
	}
	if pos >= total {
		return 0, result
	}
	return pos, result
}

// HScan iterates over fields of the hash at key.
func (s *MemoryStore) HScan(key string, cursor uint64, match string, count int) (uint64, []string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.hashes[key]
	if !ok || s.keyExpiredNonStr(key) {
		return 0, []string{}, nil
	}
	if t := s.typeOf(key); t != "hash" {
		return 0, nil, ErrWrongType
	}
	if count <= 0 {
		count = 10
	}
	fields := make([]string, 0, len(h))
	for f := range h {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	total := uint64(len(fields))
	if cursor >= total {
		cursor = 0
	}
	var result []string
	pos := cursor
	for int(pos-cursor) < count && pos < total {
		f := fields[pos]
		if match == "" || match == "*" {
			result = append(result, f, h[f])
		} else if matched, _ := filepath.Match(match, f); matched {
			result = append(result, f, h[f])
		}
		pos++
	}
	if pos >= total {
		return 0, result, nil
	}
	return pos, result, nil
}

// SScan iterates over members of the set at key.
func (s *MemoryStore) SScan(key string, cursor uint64, match string, count int) (uint64, []string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.sets[key]
	if !ok || s.keyExpiredNonStr(key) {
		return 0, []string{}, nil
	}
	if t := s.typeOf(key); t != "set" {
		return 0, nil, ErrWrongType
	}
	if count <= 0 {
		count = 10
	}
	members := make([]string, 0, len(st))
	for m := range st {
		members = append(members, m)
	}
	sort.Strings(members)
	total := uint64(len(members))
	if cursor >= total {
		cursor = 0
	}
	var result []string
	pos := cursor
	for int(pos-cursor) < count && pos < total {
		m := members[pos]
		if match == "" || match == "*" {
			result = append(result, m)
		} else if matched, _ := filepath.Match(match, m); matched {
			result = append(result, m)
		}
		pos++
	}
	if pos >= total {
		return 0, result, nil
	}
	return pos, result, nil
}

// ZScan iterates over members of the sorted set at key.
func (s *MemoryStore) ZScan(key string, cursor uint64, match string, count int) (uint64, []string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, ok := s.zsets[key]
	if !ok || s.keyExpiredNonStr(key) {
		return 0, []string{}, nil
	}
	if t := s.typeOf(key); t != "zset" {
		return 0, nil, ErrWrongType
	}
	if count <= 0 {
		count = 10
	}
	sorted := sortedMembers(z)
	total := uint64(len(sorted))
	if cursor >= total {
		cursor = 0
	}
	var result []string
	pos := cursor
	for int(pos-cursor) < count && pos < total {
		m := sorted[pos]
		if match == "" || match == "*" {
			result = append(result, m.Member, formatScore(m.Score))
		} else if matched, _ := filepath.Match(match, m.Member); matched {
			result = append(result, m.Member, formatScore(m.Score))
		}
		pos++
	}
	if pos >= total {
		return 0, result, nil
	}
	return pos, result, nil
}

// ── Key versioning ───────────────────────────────────────────────────────────

// Version returns the current write version of a key.
func (s *MemoryStore) Version(key string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keyVersion[key]
}

// ── Type introspection ───────────────────────────────────────────────────────

// Type returns the Redis type name for key.
func (s *MemoryStore) Type(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.typeOf(key)
	if t == "" {
		return "none"
	}
	return t
}
