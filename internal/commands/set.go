package commands

import (
	"errors"
	"strconv"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handleSAdd implements SADD key member [member ...].
func handleSAdd(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'SADD' command")
	}
	n, err := store.SAdd(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleSRem implements SREM key member [member ...].
func handleSRem(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'SREM' command")
	}
	n, err := store.SRem(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleSMembers implements SMEMBERS key.
func handleSMembers(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'SMEMBERS' command")
	}
	members, err := store.SMembers(args[1])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(members))
	for i, m := range members {
		elems[i] = protocol.BulkString(m)
	}
	return protocol.Array(elems)
}

// handleSCard implements SCARD key.
func handleSCard(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'SCARD' command")
	}
	n, err := store.SCard(args[1])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleSIsMember implements SISMEMBER key member.
func handleSIsMember(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'SISMEMBER' command")
	}
	ok, err := store.SIsMember(args[1], args[2])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if ok {
		return protocol.Integer(1)
	}
	return protocol.Integer(0)
}

// handleSMIsMember implements SMISMEMBER key member [member ...].
func handleSMIsMember(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'SMISMEMBER' command")
	}
	flags, err := store.SMIsMember(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(flags))
	for i, f := range flags {
		elems[i] = protocol.Integer(f)
	}
	return protocol.Array(elems)
}

// handleSInter implements SINTER key [key ...].
func handleSInter(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'SINTER' command")
	}
	members, err := store.SInter(args[1:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(members))
	for i, m := range members {
		elems[i] = protocol.BulkString(m)
	}
	return protocol.Array(elems)
}

// handleSUnion implements SUNION key [key ...].
func handleSUnion(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'SUNION' command")
	}
	members, err := store.SUnion(args[1:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(members))
	for i, m := range members {
		elems[i] = protocol.BulkString(m)
	}
	return protocol.Array(elems)
}

// handleSDiff implements SDIFF key [key ...].
func handleSDiff(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'SDIFF' command")
	}
	members, err := store.SDiff(args[1:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(members))
	for i, m := range members {
		elems[i] = protocol.BulkString(m)
	}
	return protocol.Array(elems)
}

// handleSInterStore implements SINTERSTORE destination key [key ...].
func handleSInterStore(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'SINTERSTORE' command")
	}
	n, err := store.SInterStore(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleSUnionStore implements SUNIONSTORE destination key [key ...].
func handleSUnionStore(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'SUNIONSTORE' command")
	}
	n, err := store.SUnionStore(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleSDiffStore implements SDIFFSTORE destination key [key ...].
func handleSDiffStore(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'SDIFFSTORE' command")
	}
	n, err := store.SDiffStore(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleSRandMember implements SRANDMEMBER key [count].
func handleSRandMember(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 || len(args) > 3 {
		return protocol.Error("ERR wrong number of arguments for 'SRANDMEMBER' command")
	}
	count := 1
	withCount := len(args) == 3
	if withCount {
		n, err := strconv.Atoi(args[2])
		if err != nil {
			return protocol.Error("ERR value is not an integer or out of range")
		}
		count = n
	}
	members, err := store.SRandMember(args[1], count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if !withCount {
		if len(members) == 0 {
			return protocol.NullBulkString()
		}
		return protocol.BulkString(members[0])
	}
	elems := make([]protocol.Response, len(members))
	for i, m := range members {
		elems[i] = protocol.BulkString(m)
	}
	return protocol.Array(elems)
}

// handleSPop implements SPOP key [count].
func handleSPop(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 || len(args) > 3 {
		return protocol.Error("ERR wrong number of arguments for 'SPOP' command")
	}
	count := 1
	withCount := len(args) == 3
	if withCount {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 0 {
			return protocol.Error("ERR value is not an integer or out of range")
		}
		count = n
	}
	members, err := store.SPop(args[1], count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if !withCount {
		if len(members) == 0 {
			return protocol.NullBulkString()
		}
		return protocol.BulkString(members[0])
	}
	elems := make([]protocol.Response, len(members))
	for i, m := range members {
		elems[i] = protocol.BulkString(m)
	}
	return protocol.Array(elems)
}

// handleSMove implements SMOVE source destination member.
func handleSMove(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'SMOVE' command")
	}
	ok, err := store.SMove(args[1], args[2], args[3])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if ok {
		return protocol.Integer(1)
	}
	return protocol.Integer(0)
}
