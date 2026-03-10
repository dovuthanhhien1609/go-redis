// Package storage defines the Store interface and provides an in-memory implementation.
// The Store interface is the only thing the commands package depends on —
// the concrete implementation is injected at startup.
package storage

// Store is the interface all storage backends must satisfy.
// Commands depend on this interface, never on a concrete type.
type Store interface {
	// Set stores value under key, overwriting any existing value.
	Set(key, value string)

	// Get returns the value for key and true, or ("", false) if the key
	// does not exist.
	Get(key string) (string, bool)

	// Del removes the given keys and returns the count of keys that existed.
	Del(keys ...string) int

	// Exists returns the number of the given keys that currently exist.
	// A key repeated N times counts N times.
	Exists(keys ...string) int

	// Keys returns all keys matching the glob-style pattern.
	// The only pattern supported in Phase 1 is "*" (all keys).
	Keys(pattern string) []string

	// Len returns the total number of keys currently stored.
	Len() int

	// Flush removes all keys from the store.
	// Used by AOF replay to reset state before re-executing the log.
	Flush()
}
