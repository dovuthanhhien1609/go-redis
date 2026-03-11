// Package storage defines the Store interface and provides an in-memory implementation.
// The Store interface is the only thing the commands package depends on —
// the concrete implementation is injected at startup.
package storage

import "time"

// Store is the interface all storage backends must satisfy.
// Commands depend on this interface, never on a concrete type.
// All methods must be safe for concurrent use.
type Store interface {
	// ── String operations ────────────────────────────────────────────────────

	// Set stores value under key, overwriting any existing value and clearing
	// any TTL associated with the key.
	Set(key, value string)

	// SetWithTTL stores value under key with an expiration after ttl.
	SetWithTTL(key, value string, ttl time.Duration)

	// Get returns the value for key and true, or ("", false) if the key does
	// not exist or has expired.
	Get(key string) (string, bool)

	// Del removes the given keys (strings or hashes) and returns the count of
	// keys that existed and were removed.
	Del(keys ...string) int

	// Exists returns the number of the given keys that currently exist.
	// A key repeated N times counts N times.
	Exists(keys ...string) int

	// Keys returns all keys matching the glob-style pattern.
	Keys(pattern string) []string

	// Len returns the total number of non-expired keys currently stored
	// across all data types.
	Len() int

	// Flush removes all keys from the store.
	Flush()

	// ── Expiry operations ────────────────────────────────────────────────────

	// Expire sets a TTL on key. Returns true if the key exists and the TTL
	// was set, false if the key does not exist.
	Expire(key string, ttl time.Duration) bool

	// TTL returns the remaining time-to-live for a key.
	//   -1 * time.Second means the key exists but has no associated TTL.
	//   -2 * time.Second means the key does not exist (or has already expired).
	TTL(key string) time.Duration

	// Persist removes any TTL from key. Returns true if the key had a TTL
	// that was successfully removed.
	Persist(key string) bool

	// ── Atomic rename ────────────────────────────────────────────────────────

	// Rename atomically moves the value at src to dst, overwriting any
	// existing value at dst. Returns false if src does not exist.
	Rename(src, dst string) bool

	// ── Hash operations ──────────────────────────────────────────────────────

	// HSet sets the given fields in the hash stored at key.
	// Returns the number of fields that were added (not updated).
	HSet(key string, fields map[string]string) int

	// HGet returns the value of field in the hash at key.
	HGet(key, field string) (string, bool)

	// HDel removes the given fields from the hash at key.
	// Returns the number of fields that were removed.
	HDel(key string, fields ...string) int

	// HGetAll returns a copy of all field-value pairs in the hash at key,
	// or nil if the key does not exist or has expired.
	HGetAll(key string) map[string]string

	// HLen returns the number of fields in the hash at key.
	HLen(key string) int

	// HExists reports whether field exists in the hash at key.
	HExists(key, field string) bool

	// HKeys returns all field names in the hash at key.
	HKeys(key string) []string

	// HVals returns all values in the hash at key.
	HVals(key string) []string

	// ── Type introspection ───────────────────────────────────────────────────

	// Type returns the Redis type name for key: "string", "hash", or "none".
	Type(key string) string
}
