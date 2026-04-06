package commands

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// parseScore converts a score string to float64.
// Accepts "+inf", "-inf", "inf", and decimal notation.
func parseScore(s string) (float64, error) {
	switch strings.ToLower(s) {
	case "+inf", "inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	}
	return strconv.ParseFloat(s, 64)
}

// parseScoreBound parses a ZRANGEBYSCORE min/max bound.
// Returns (value, exclusive, error).
// Exclusive bounds are prefixed with '(' in Redis.
func parseScoreBound(s string) (float64, bool, error) {
	if strings.HasPrefix(s, "(") {
		f, err := parseScore(s[1:])
		return f, true, err
	}
	f, err := parseScore(s)
	return f, false, err
}

// handleZAdd implements ZADD key [NX|XX] [GT|LT] [CH] score member [score member ...].
func handleZAdd(args []string, store storage.Store) protocol.Response {
	if len(args) < 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZADD' command")
	}
	key := args[1]
	var nx, xx, gt, lt, ch bool
	i := 2
	for ; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "NX":
			nx = true
		case "XX":
			xx = true
		case "GT":
			gt = true
		case "LT":
			lt = true
		case "CH":
			ch = true
		default:
			goto parseMembers
		}
	}
parseMembers:
	if nx && xx {
		return protocol.Error("ERR XX and NX options at the same time are not compatible")
	}
	if (gt && nx) || (lt && nx) {
		return protocol.Error("ERR GT, LT, and NX options at the same time are not compatible")
	}
	if len(args[i:])%2 != 0 || len(args[i:]) == 0 {
		return protocol.Error("ERR syntax error")
	}
	members := make([]storage.ZMember, 0, (len(args)-i)/2)
	for j := i; j < len(args); j += 2 {
		score, err := parseScore(args[j])
		if err != nil {
			return protocol.Error("ERR value is not a valid float")
		}
		members = append(members, storage.ZMember{Score: score, Member: args[j+1]})
	}
	n, err := store.ZAdd(key, members, nx, xx, gt, lt, ch)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(n)
}

// handleZRem implements ZREM key member [member ...].
func handleZRem(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'ZREM' command")
	}
	n, err := store.ZRem(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(n)
}

// handleZScore implements ZSCORE key member.
func handleZScore(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'ZSCORE' command")
	}
	score, ok, err := store.ZScore(args[1], args[2])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.BulkString(formatScore(score))
}

// handleZCard implements ZCARD key.
func handleZCard(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'ZCARD' command")
	}
	n, err := store.ZCard(args[1])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(n)
}

// handleZRank implements ZRANK key member.
func handleZRank(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'ZRANK' command")
	}
	rank, ok, err := store.ZRank(args[1], args[2])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.Integer(rank)
}

// handleZRevRank implements ZREVRANK key member.
func handleZRevRank(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'ZREVRANK' command")
	}
	rank, ok, err := store.ZRevRank(args[1], args[2])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.Integer(rank)
}

// handleZIncrBy implements ZINCRBY key increment member.
func handleZIncrBy(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZINCRBY' command")
	}
	incr, err := parseScore(args[2])
	if err != nil {
		return protocol.Error("ERR value is not a valid float")
	}
	score, err := store.ZIncrBy(args[1], incr, args[3])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.BulkString(formatScore(score))
}

// handleZRange implements ZRANGE key start stop [REV] [BYSCORE | BYLEX]
// [LIMIT offset count] [WITHSCORES].
func handleZRange(args []string, store storage.Store) protocol.Response {
	if len(args) < 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZRANGE' command")
	}
	key := args[1]
	var rev, withScores, byScore, byLex bool
	var offset, count int64 = 0, -1

	// Parse optional flags after start stop.
	i := 4
	for i < len(args) {
		switch strings.ToUpper(args[i]) {
		case "REV":
			rev = true
		case "WITHSCORES":
			withScores = true
		case "BYSCORE":
			byScore = true
		case "BYLEX":
			byLex = true
		case "LIMIT":
			if i+2 >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			o, err := strconv.ParseInt(args[i+1], 10, 64)
			if err != nil {
				return protocol.Error("ERR value is not an integer or out of range")
			}
			c, err := strconv.ParseInt(args[i+2], 10, 64)
			if err != nil {
				return protocol.Error("ERR value is not an integer or out of range")
			}
			offset, count = o, c
			i += 2
		default:
			return protocol.Error("ERR syntax error")
		}
		i++
	}

	if byLex {
		var result []string
		var err error
		if rev {
			result, err = store.ZRangeByLex(key, args[3], args[2], offset, count)
		} else {
			result, err = store.ZRangeByLex(key, args[2], args[3], offset, count)
		}
		if errors.Is(err, storage.ErrWrongType) {
			return protocol.Error(storage.ErrWrongType.Error())
		}
		elems := make([]protocol.Response, len(result))
		for i, v := range result {
			elems[i] = protocol.BulkString(v)
		}
		return protocol.Array(elems)
	}

	if byScore {
		var result []string
		var err error
		if rev {
			max, maxExcl, e1 := parseScoreBound(args[2])
			min, minExcl, e2 := parseScoreBound(args[3])
			if e1 != nil || e2 != nil {
				return protocol.Error("ERR min or max is not a float")
			}
			result, err = store.ZRevRangeByScore(key, max, min, maxExcl, minExcl, withScores, offset, count)
		} else {
			min, minExcl, e1 := parseScoreBound(args[2])
			max, maxExcl, e2 := parseScoreBound(args[3])
			if e1 != nil || e2 != nil {
				return protocol.Error("ERR min or max is not a float")
			}
			result, err = store.ZRangeByScore(key, min, max, minExcl, maxExcl, withScores, offset, count)
		}
		if errors.Is(err, storage.ErrWrongType) {
			return protocol.Error(storage.ErrWrongType.Error())
		}
		elems := make([]protocol.Response, len(result))
		for i, v := range result {
			elems[i] = protocol.BulkString(v)
		}
		return protocol.Array(elems)
	}

	// Rank-based range.
	start, e1 := strconv.ParseInt(args[2], 10, 64)
	stop, e2 := strconv.ParseInt(args[3], 10, 64)
	if e1 != nil || e2 != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	result, err := store.ZRange(key, start, stop, rev, withScores)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(result))
	for i, v := range result {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleZRevRange implements ZREVRANGE key start stop [WITHSCORES].
func handleZRevRange(args []string, store storage.Store) protocol.Response {
	if len(args) < 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZREVRANGE' command")
	}
	start, e1 := strconv.ParseInt(args[2], 10, 64)
	stop, e2 := strconv.ParseInt(args[3], 10, 64)
	if e1 != nil || e2 != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	withScores := len(args) == 5 && strings.ToUpper(args[4]) == "WITHSCORES"
	result, err := store.ZRange(args[1], start, stop, true, withScores)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(result))
	for i, v := range result {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleZRangeByScore implements ZRANGEBYSCORE key min max [WITHSCORES] [LIMIT offset count].
func handleZRangeByScore(args []string, store storage.Store) protocol.Response {
	if len(args) < 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZRANGEBYSCORE' command")
	}
	min, minExcl, e1 := parseScoreBound(args[2])
	max, maxExcl, e2 := parseScoreBound(args[3])
	if e1 != nil || e2 != nil {
		return protocol.Error("ERR min or max is not a float")
	}
	withScores := false
	var offset, count int64 = 0, -1
	for i := 4; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "WITHSCORES":
			withScores = true
		case "LIMIT":
			if i+2 >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			o, err1 := strconv.ParseInt(args[i+1], 10, 64)
			c, err2 := strconv.ParseInt(args[i+2], 10, 64)
			if err1 != nil || err2 != nil {
				return protocol.Error("ERR value is not an integer or out of range")
			}
			offset, count = o, c
			i += 2
		}
	}
	result, err := store.ZRangeByScore(args[1], min, max, minExcl, maxExcl, withScores, offset, count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(result))
	for i, v := range result {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleZRevRangeByScore implements ZREVRANGEBYSCORE key max min [WITHSCORES] [LIMIT offset count].
func handleZRevRangeByScore(args []string, store storage.Store) protocol.Response {
	if len(args) < 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZREVRANGEBYSCORE' command")
	}
	max, maxExcl, e1 := parseScoreBound(args[2])
	min, minExcl, e2 := parseScoreBound(args[3])
	if e1 != nil || e2 != nil {
		return protocol.Error("ERR min or max is not a float")
	}
	withScores := false
	var offset, count int64 = 0, -1
	for i := 4; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "WITHSCORES":
			withScores = true
		case "LIMIT":
			if i+2 >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			o, err1 := strconv.ParseInt(args[i+1], 10, 64)
			c, err2 := strconv.ParseInt(args[i+2], 10, 64)
			if err1 != nil || err2 != nil {
				return protocol.Error("ERR value is not an integer or out of range")
			}
			offset, count = o, c
			i += 2
		}
	}
	result, err := store.ZRevRangeByScore(args[1], max, min, maxExcl, minExcl, withScores, offset, count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(result))
	for i, v := range result {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleZCount implements ZCOUNT key min max.
func handleZCount(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZCOUNT' command")
	}
	min, minExcl, e1 := parseScoreBound(args[2])
	max, maxExcl, e2 := parseScoreBound(args[3])
	if e1 != nil || e2 != nil {
		return protocol.Error("ERR min or max is not a float")
	}
	n, err := store.ZCount(args[1], min, max, minExcl, maxExcl)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(n)
}

// handleZRangeByLex implements ZRANGEBYLEX key min max [LIMIT offset count].
func handleZRangeByLex(args []string, store storage.Store) protocol.Response {
	if len(args) < 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZRANGEBYLEX' command")
	}
	var offset, count int64 = 0, -1
	for i := 4; i < len(args); i++ {
		if strings.ToUpper(args[i]) == "LIMIT" {
			if i+2 >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			o, err1 := strconv.ParseInt(args[i+1], 10, 64)
			c, err2 := strconv.ParseInt(args[i+2], 10, 64)
			if err1 != nil || err2 != nil {
				return protocol.Error("ERR value is not an integer or out of range")
			}
			offset, count = o, c
			i += 2
		}
	}
	result, err := store.ZRangeByLex(args[1], args[2], args[3], offset, count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(result))
	for i, v := range result {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleZRevRangeByLex implements ZREVRANGEBYLEX key max min [LIMIT offset count].
func handleZRevRangeByLex(args []string, store storage.Store) protocol.Response {
	if len(args) < 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZREVRANGEBYLEX' command")
	}
	var offset, count int64 = 0, -1
	for i := 4; i < len(args); i++ {
		if strings.ToUpper(args[i]) == "LIMIT" {
			if i+2 >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			o, err1 := strconv.ParseInt(args[i+1], 10, 64)
			c, err2 := strconv.ParseInt(args[i+2], 10, 64)
			if err1 != nil || err2 != nil {
				return protocol.Error("ERR value is not an integer or out of range")
			}
			offset, count = o, c
			i += 2
		}
	}
	// ZREVRANGEBYLEX: max comes before min in Redis.
	result, err := store.ZRangeByLex(args[1], args[3], args[2], offset, count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	// Reverse the result.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	elems := make([]protocol.Response, len(result))
	for i, v := range result {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleZLexCount implements ZLEXCOUNT key min max.
func handleZLexCount(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'ZLEXCOUNT' command")
	}
	n, err := store.ZLexCount(args[1], args[2], args[3])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(n)
}

// handleZPopMin implements ZPOPMIN key [count].
func handleZPopMin(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 || len(args) > 3 {
		return protocol.Error("ERR wrong number of arguments for 'ZPOPMIN' command")
	}
	count := int64(1)
	if len(args) == 3 {
		n, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil || n < 0 {
			return protocol.Error("ERR value is not an integer or out of range")
		}
		count = n
	}
	members, err := store.ZPopMin(args[1], count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, 0, len(members)*2)
	for _, m := range members {
		elems = append(elems, protocol.BulkString(m.Member), protocol.BulkString(formatScore(m.Score)))
	}
	return protocol.Array(elems)
}

// handleZPopMax implements ZPOPMAX key [count].
func handleZPopMax(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 || len(args) > 3 {
		return protocol.Error("ERR wrong number of arguments for 'ZPOPMAX' command")
	}
	count := int64(1)
	if len(args) == 3 {
		n, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil || n < 0 {
			return protocol.Error("ERR value is not an integer or out of range")
		}
		count = n
	}
	members, err := store.ZPopMax(args[1], count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, 0, len(members)*2)
	for _, m := range members {
		elems = append(elems, protocol.BulkString(m.Member), protocol.BulkString(formatScore(m.Score)))
	}
	return protocol.Array(elems)
}

// handleZUnionStore implements ZUNIONSTORE destination numkeys key [key ...]
// [WEIGHTS weight [weight ...]] [AGGREGATE SUM|MIN|MAX].
func handleZUnionStore(args []string, store storage.Store) protocol.Response {
	return handleZSetStore("ZUNIONSTORE", args, store, false)
}

// handleZInterStore implements ZINTERSTORE destination numkeys key [key ...]
// [WEIGHTS weight [weight ...]] [AGGREGATE SUM|MIN|MAX].
func handleZInterStore(args []string, store storage.Store) protocol.Response {
	return handleZSetStore("ZINTERSTORE", args, store, true)
}

func handleZSetStore(cmd string, args []string, store storage.Store, inter bool) protocol.Response {
	if len(args) < 4 {
		return protocol.Error("ERR wrong number of arguments for '" + cmd + "' command")
	}
	dst := args[1]
	numkeys, err := strconv.Atoi(args[2])
	if err != nil || numkeys < 1 {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	if len(args) < 3+numkeys {
		return protocol.Error("ERR syntax error")
	}
	keys := args[3 : 3+numkeys]
	weights := make([]float64, numkeys)
	for i := range weights {
		weights[i] = 1
	}
	aggregate := "SUM"
	i := 3 + numkeys
	for i < len(args) {
		switch strings.ToUpper(args[i]) {
		case "WEIGHTS":
			i++
			for j := 0; j < numkeys && i < len(args); j++ {
				w, err := strconv.ParseFloat(args[i], 64)
				if err != nil {
					return protocol.Error("ERR weight value is not a float")
				}
				weights[j] = w
				i++
			}
		case "AGGREGATE":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			aggregate = strings.ToUpper(args[i])
			i++
		default:
			return protocol.Error("ERR syntax error")
		}
	}
	var n int64
	if inter {
		n, err = store.ZInterStore(dst, keys, weights, aggregate)
	} else {
		n, err = store.ZUnionStore(dst, keys, weights, aggregate)
	}
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(n)
}

// formatScore is re-exported for use in this file; defined in util.go.
// (Declared here to avoid circular dependency with the formatScore in memory.go)
func formatScore(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
