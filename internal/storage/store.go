// Package storage defines the Store interface and provides an in-memory implementation.
// The Store interface is the only thing the commands package depends on —
// the concrete implementation is injected at startup.
package storage

import (
	"errors"
	"time"
)

// ErrWrongType is returned when an operation is attempted on a key that holds
// a value of the wrong type. Matches the Redis WRONGTYPE error.
var ErrWrongType = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")

// ZMember pairs a sorted set member with its score.
type ZMember struct {
	Member string
	Score  float64
}

// SetAdvOpts captures the optional flags for the SET command.
type SetAdvOpts struct {
	KeepTTL bool // preserve existing TTL, ignore ttl argument
	NX      bool // only set if key does not exist
	XX      bool // only set if key already exists
	Get     bool // return the old value before setting
}

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

	// SetAdv is the atomic full SET implementation supporting NX/XX/GET/KEEPTTL.
	// ttl is ignored when opts.KeepTTL is true. A zero ttl means no expiry.
	// Returns (oldValue, oldExists, wasSet).
	SetAdv(key, value string, ttl time.Duration, opts SetAdvOpts) (string, bool, bool)

	// Get returns the value for key and true, or ("", false) if the key does
	// not exist or has expired.
	Get(key string) (string, bool)

	// GetEx returns the value for key and modifies its TTL:
	//   - if persist is true, remove any TTL
	//   - if hasTTL is true, set the TTL to ttl
	//   - otherwise leave the TTL unchanged
	// Returns ("", false) if the key does not exist or is not a string.
	GetEx(key string, ttl time.Duration, persist, hasTTL bool) (string, bool)

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

	// ── List operations ──────────────────────────────────────────────────────

	// LPush prepends one or more values to the list stored at key.
	// Values are inserted from left to right so the first argument is the
	// leftmost element after the push. Creates the list if it does not exist.
	// Returns (new length, ErrWrongType) if key holds a non-list value.
	LPush(key string, values ...string) (int, error)

	// RPush appends one or more values to the list stored at key.
	// Returns (new length, ErrWrongType).
	RPush(key string, values ...string) (int, error)

	// LPop removes and returns count elements from the head of the list.
	// Returns (nil, nil) when the key does not exist.
	LPop(key string, count int) ([]string, error)

	// RPop removes and returns count elements from the tail of the list.
	RPop(key string, count int) ([]string, error)

	// LLen returns the length of the list at key, or 0 if the key does not exist.
	LLen(key string) (int, error)

	// LRange returns the specified range of list elements (inclusive, 0-based).
	// Negative indices are relative to the tail (-1 is the last element).
	LRange(key string, start, stop int) ([]string, error)

	// LIndex returns the element at index in the list at key.
	// Returns ("", false, nil) when index is out of range.
	LIndex(key string, index int) (string, bool, error)

	// LSet sets the element at index in the list to value.
	// Returns ErrWrongType or an out-of-range error if applicable.
	LSet(key string, index int, value string) error

	// LInsert inserts value before or after the pivot element in the list.
	// Returns the new length, 0 if key does not exist, -1 if pivot not found,
	// or (0, ErrWrongType) on type error.
	LInsert(key string, before bool, pivot, value string) (int, error)

	// LRem removes count occurrences of value from the list.
	//   count > 0: remove from head
	//   count < 0: remove from tail
	//   count == 0: remove all
	// Returns (removed count, ErrWrongType).
	LRem(key string, count int, value string) (int, error)

	// LTrim keeps only the elements in the given range, discarding the rest.
	LTrim(key string, start, stop int) error

	// LMove atomically moves an element from src to dst.
	// srcLeft/dstLeft control whether to pop/push from the left or right end.
	// Returns ("", false, nil) when src does not exist.
	LMove(src, dst string, srcLeft, dstLeft bool) (string, bool, error)

	// ── Set operations ───────────────────────────────────────────────────────

	// SAdd adds members to the set stored at key.
	// Returns (count of new members added, ErrWrongType).
	SAdd(key string, members ...string) (int, error)

	// SRem removes members from the set. Returns (count removed, ErrWrongType).
	SRem(key string, members ...string) (int, error)

	// SMembers returns all members of the set at key.
	SMembers(key string) ([]string, error)

	// SCard returns the cardinality of the set at key.
	SCard(key string) (int, error)

	// SIsMember reports whether member belongs to the set at key.
	SIsMember(key, member string) (bool, error)

	// SMIsMember returns a slice of 1/0 values indicating whether each member
	// exists in the set.
	SMIsMember(key string, members ...string) ([]int64, error)

	// SInter returns the intersection of the sets at the given keys.
	// Non-existent keys are treated as empty sets.
	SInter(keys ...string) ([]string, error)

	// SUnion returns the union of the sets at the given keys.
	SUnion(keys ...string) ([]string, error)

	// SDiff returns the difference: members in keys[0] not in any other key.
	SDiff(keys ...string) ([]string, error)

	// SInterStore stores the intersection of keys at dst. Returns (cardinality, error).
	SInterStore(dst string, keys ...string) (int, error)

	// SUnionStore stores the union of keys at dst. Returns (cardinality, error).
	SUnionStore(dst string, keys ...string) (int, error)

	// SDiffStore stores the difference of keys at dst. Returns (cardinality, error).
	SDiffStore(dst string, keys ...string) (int, error)

	// SRandMember returns random members from the set.
	//   count > 0: return up to count unique members
	//   count < 0: return |count| members (with possible repetition)
	SRandMember(key string, count int) ([]string, error)

	// SPop removes and returns count random members from the set.
	SPop(key string, count int) ([]string, error)

	// SMove atomically moves member from src to dst set.
	// Returns (true, nil) on success, (false, nil) if member not in src.
	SMove(src, dst, member string) (bool, error)

	// ── Sorted set operations ────────────────────────────────────────────────

	// ZAddOpts controls the behaviour of ZAdd.
	// ZAdd adds or updates members in the sorted set at key.
	// Returns (added, error) where added is the number of new members
	// (or changed members if CH is set).
	ZAdd(key string, members []ZMember, nx, xx, gt, lt, ch bool) (int64, error)

	// ZRem removes members from the sorted set. Returns (count removed, error).
	ZRem(key string, members ...string) (int64, error)

	// ZScore returns the score of member in the sorted set.
	ZScore(key, member string) (float64, bool, error)

	// ZCard returns the number of members in the sorted set.
	ZCard(key string) (int64, error)

	// ZRank returns the 0-based rank of member (ascending order).
	// Returns (-1, false, nil) if the member or key does not exist.
	ZRank(key, member string) (int64, bool, error)

	// ZRevRank returns the 0-based rank in descending order.
	ZRevRank(key, member string) (int64, bool, error)

	// ZIncrBy increments the score of member by increment.
	// Creates the member if it does not exist.
	ZIncrBy(key string, increment float64, member string) (float64, error)

	// ZRange returns members in [start, stop] rank range.
	// If rev is true, the range is in descending order.
	// If withScores is true, the returned slice alternates member/score.
	ZRange(key string, start, stop int64, rev, withScores bool) ([]string, error)

	// ZRangeByScore returns members whose score is between min and max.
	// minExcl/maxExcl mark exclusive bounds. offset/count implement LIMIT.
	ZRangeByScore(key string, min, max float64, minExcl, maxExcl bool, withScores bool, offset, count int64) ([]string, error)

	// ZRevRangeByScore is like ZRangeByScore but in descending order.
	ZRevRangeByScore(key string, max, min float64, maxExcl, minExcl bool, withScores bool, offset, count int64) ([]string, error)

	// ZCount returns the number of members with scores in [min, max].
	ZCount(key string, min, max float64, minExcl, maxExcl bool) (int64, error)

	// ZRangeByLex returns members whose value is between min and max
	// lexicographically (scores must all be equal).
	// min/max follow Redis conventions: "[value" inclusive, "(value" exclusive,
	// "-" is -inf, "+" is +inf.
	ZRangeByLex(key, min, max string, offset, count int64) ([]string, error)

	// ZLexCount counts members in the lexicographic range.
	ZLexCount(key, min, max string) (int64, error)

	// ZPopMin removes and returns count members with the lowest scores.
	ZPopMin(key string, count int64) ([]ZMember, error)

	// ZPopMax removes and returns count members with the highest scores.
	ZPopMax(key string, count int64) ([]ZMember, error)

	// ZUnionStore stores the union of the input sorted sets at dst.
	// weights[i] scales the scores of keys[i]; default weight is 1.
	// aggregate is "SUM", "MIN", or "MAX".
	ZUnionStore(dst string, keys []string, weights []float64, aggregate string) (int64, error)

	// ZInterStore stores the intersection of the input sorted sets at dst.
	ZInterStore(dst string, keys []string, weights []float64, aggregate string) (int64, error)

	// ── Cursor-based iteration ───────────────────────────────────────────────

	// Scan iterates over keys matching pattern, returning up to count per call.
	// cursor 0 starts a new scan; returns next cursor (0 means complete).
	Scan(cursor uint64, match string, count int) (uint64, []string)

	// HScan iterates over fields of the hash at key.
	// Returns (nextCursor, flat [field, value, ...], error).
	HScan(key string, cursor uint64, match string, count int) (uint64, []string, error)

	// SScan iterates over members of the set at key.
	SScan(key string, cursor uint64, match string, count int) (uint64, []string, error)

	// ZScan iterates over members of the sorted set at key.
	// Returns (nextCursor, flat [member, score, ...], error).
	ZScan(key string, cursor uint64, match string, count int) (uint64, []string, error)

	// ── Key versioning for WATCH ─────────────────────────────────────────────

	// Version returns the current modification version of a key (0 = never set).
	Version(key string) uint64

	// ── Type introspection ───────────────────────────────────────────────────

	// Type returns the Redis type name for key: "string", "hash", "list",
	// "set", "zset", or "none".
	Type(key string) string
}
