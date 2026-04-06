package storage

import (
	"sort"
	"testing"
)

// ── List tests ───────────────────���────────────────────────���────────────────────

func newStore() *MemoryStore { return NewMemoryStore() }

func TestLPush(t *testing.T) {
	s := newStore()
	n, err := s.LPush("k", "a", "b", "c")
	if err != nil || n != 3 {
		t.Fatalf("LPush: got (%d, %v), want (3, nil)", n, err)
	}
	// LPUSH pushes c first, then b, then a → [c, b, a]
	l, _ := s.LRange("k", 0, -1)
	want := []string{"c", "b", "a"}
	if !equal(l, want) {
		t.Errorf("LRange after LPush: got %v, want %v", l, want)
	}
}

func TestRPush(t *testing.T) {
	s := newStore()
	n, _ := s.RPush("k", "a", "b", "c")
	if n != 3 {
		t.Fatalf("RPush len: got %d, want 3", n)
	}
	l, _ := s.LRange("k", 0, -1)
	if !equal(l, []string{"a", "b", "c"}) {
		t.Errorf("got %v, want [a b c]", l)
	}
}

func TestLPopRPop(t *testing.T) {
	s := newStore()
	s.RPush("k", "a", "b", "c")
	v, _ := s.LPop("k", 1)
	if len(v) != 1 || v[0] != "a" {
		t.Errorf("LPop: got %v, want [a]", v)
	}
	v, _ = s.RPop("k", 1)
	if len(v) != 1 || v[0] != "c" {
		t.Errorf("RPop: got %v, want [c]", v)
	}
	n, _ := s.LLen("k")
	if n != 1 {
		t.Errorf("LLen: got %d, want 1", n)
	}
}

func TestLRange(t *testing.T) {
	s := newStore()
	s.RPush("k", "a", "b", "c", "d")
	l, _ := s.LRange("k", 1, 2)
	if !equal(l, []string{"b", "c"}) {
		t.Errorf("LRange(1,2): got %v", l)
	}
	l, _ = s.LRange("k", -2, -1)
	if !equal(l, []string{"c", "d"}) {
		t.Errorf("LRange(-2,-1): got %v", l)
	}
}

func TestLInsert(t *testing.T) {
	s := newStore()
	s.RPush("k", "a", "b", "c")
	n, _ := s.LInsert("k", true, "b", "X")
	if n != 4 {
		t.Errorf("LInsert before b: len=%d, want 4", n)
	}
	l, _ := s.LRange("k", 0, -1)
	if !equal(l, []string{"a", "X", "b", "c"}) {
		t.Errorf("after LInsert BEFORE: got %v", l)
	}
	n, _ = s.LInsert("k", false, "b", "Y")
	l, _ = s.LRange("k", 0, -1)
	if !equal(l, []string{"a", "X", "b", "Y", "c"}) {
		t.Errorf("after LInsert AFTER: got %v", l)
	}
}

func TestLRem(t *testing.T) {
	s := newStore()
	s.RPush("k", "a", "b", "a", "c", "a")
	n, _ := s.LRem("k", 2, "a")
	if n != 2 {
		t.Errorf("LRem count=2: removed %d, want 2", n)
	}
	l, _ := s.LRange("k", 0, -1)
	if !equal(l, []string{"b", "c", "a"}) {
		t.Errorf("after LRem(2,'a'): got %v", l)
	}
}

func TestLTrim(t *testing.T) {
	s := newStore()
	s.RPush("k", "a", "b", "c", "d", "e")
	s.LTrim("k", 1, 3)
	l, _ := s.LRange("k", 0, -1)
	if !equal(l, []string{"b", "c", "d"}) {
		t.Errorf("after LTrim(1,3): got %v", l)
	}
}

func TestLMove(t *testing.T) {
	s := newStore()
	s.RPush("src", "a", "b", "c")
	v, ok, _ := s.LMove("src", "dst", false, true) // RPOP src, LPUSH dst
	if !ok || v != "c" {
		t.Errorf("LMove: got (%q, %v), want (c, true)", v, ok)
	}
	l, _ := s.LRange("dst", 0, -1)
	if !equal(l, []string{"c"}) {
		t.Errorf("dst after LMove: got %v", l)
	}
}

func TestListWrongType(t *testing.T) {
	s := newStore()
	s.Set("k", "string")
	_, err := s.LPush("k", "v")
	if err != ErrWrongType {
		t.Errorf("LPush on string key: got %v, want ErrWrongType", err)
	}
}

// ── Set tests ──────────────────────────────────────────────────────��───────────

func TestSAddSMembersSCard(t *testing.T) {
	s := newStore()
	n, _ := s.SAdd("k", "a", "b", "c", "a")
	if n != 3 {
		t.Errorf("SAdd: added %d, want 3 (a duplicate)", n)
	}
	card, _ := s.SCard("k")
	if card != 3 {
		t.Errorf("SCard: got %d, want 3", card)
	}
	members, _ := s.SMembers("k")
	if len(members) != 3 {
		t.Errorf("SMembers len: got %d", len(members))
	}
}

func TestSRem(t *testing.T) {
	s := newStore()
	s.SAdd("k", "a", "b", "c")
	n, _ := s.SRem("k", "a", "z") // z not present
	if n != 1 {
		t.Errorf("SRem: removed %d, want 1", n)
	}
}

func TestSIsMemberSMIsMember(t *testing.T) {
	s := newStore()
	s.SAdd("k", "a", "b")
	ok, _ := s.SIsMember("k", "a")
	if !ok {
		t.Error("SIsMember(a): want true")
	}
	ok, _ = s.SIsMember("k", "z")
	if ok {
		t.Error("SIsMember(z): want false")
	}
	flags, _ := s.SMIsMember("k", "a", "z", "b")
	if flags[0] != 1 || flags[1] != 0 || flags[2] != 1 {
		t.Errorf("SMIsMember: got %v", flags)
	}
}

func TestSInterSUnionSDiff(t *testing.T) {
	s := newStore()
	s.SAdd("a", "1", "2", "3")
	s.SAdd("b", "2", "3", "4")

	inter, _ := s.SInter("a", "b")
	sortStr(inter)
	if !equal(inter, []string{"2", "3"}) {
		t.Errorf("SInter: got %v", inter)
	}

	union, _ := s.SUnion("a", "b")
	sortStr(union)
	if !equal(union, []string{"1", "2", "3", "4"}) {
		t.Errorf("SUnion: got %v", union)
	}

	diff, _ := s.SDiff("a", "b")
	sortStr(diff)
	if !equal(diff, []string{"1"}) {
		t.Errorf("SDiff: got %v", diff)
	}
}

func TestSPop(t *testing.T) {
	s := newStore()
	s.SAdd("k", "a", "b", "c")
	popped, _ := s.SPop("k", 2)
	if len(popped) != 2 {
		t.Errorf("SPop(2): got %d elements", len(popped))
	}
	card, _ := s.SCard("k")
	if card != 1 {
		t.Errorf("SCard after SPop(2): got %d, want 1", card)
	}
}

func TestSMove(t *testing.T) {
	s := newStore()
	s.SAdd("src", "a", "b")
	s.SAdd("dst", "c")
	ok, _ := s.SMove("src", "dst", "a")
	if !ok {
		t.Error("SMove: want true")
	}
	isMem, _ := s.SIsMember("dst", "a")
	if !isMem {
		t.Error("after SMove, a should be in dst")
	}
}

func TestSetWrongType(t *testing.T) {
	s := newStore()
	s.Set("k", "string")
	_, err := s.SAdd("k", "v")
	if err != ErrWrongType {
		t.Errorf("SAdd on string key: got %v, want ErrWrongType", err)
	}
}

// ── Sorted set tests ───────────────────────────────────────────────────────────

func TestZAddZScoreZCard(t *testing.T) {
	s := newStore()
	n, _ := s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}, {Score: 2, Member: "b"}}, false, false, false, false, false)
	if n != 2 {
		t.Errorf("ZAdd: added %d, want 2", n)
	}
	card, _ := s.ZCard("k")
	if card != 2 {
		t.Errorf("ZCard: got %d, want 2", card)
	}
	sc, ok, _ := s.ZScore("k", "a")
	if !ok || sc != 1 {
		t.Errorf("ZScore(a): got (%g, %v)", sc, ok)
	}
}

func TestZRankZRevRank(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}, {Score: 2, Member: "b"}, {Score: 3, Member: "c"}}, false, false, false, false, false)
	rank, ok, _ := s.ZRank("k", "a")
	if !ok || rank != 0 {
		t.Errorf("ZRank(a): got (%d, %v)", rank, ok)
	}
	rank, ok, _ = s.ZRevRank("k", "a")
	if !ok || rank != 2 {
		t.Errorf("ZRevRank(a): got (%d, %v)", rank, ok)
	}
}

func TestZRange(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}, {Score: 2, Member: "b"}, {Score: 3, Member: "c"}}, false, false, false, false, false)
	result, _ := s.ZRange("k", 0, -1, false, false)
	if !equal(result, []string{"a", "b", "c"}) {
		t.Errorf("ZRange: got %v", result)
	}
	result, _ = s.ZRange("k", 0, -1, true, false)
	if !equal(result, []string{"c", "b", "a"}) {
		t.Errorf("ZRange rev: got %v", result)
	}
}

func TestZRangeByScore(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}, {Score: 2, Member: "b"}, {Score: 3, Member: "c"}, {Score: 4, Member: "d"}}, false, false, false, false, false)
	result, _ := s.ZRangeByScore("k", 2, 3, false, false, false, 0, -1)
	if !equal(result, []string{"b", "c"}) {
		t.Errorf("ZRangeByScore(2,3): got %v", result)
	}
	// Exclusive min
	result, _ = s.ZRangeByScore("k", 2, 3, true, false, false, 0, -1)
	if !equal(result, []string{"c"}) {
		t.Errorf("ZRangeByScore(>2,3): got %v", result)
	}
}

func TestZCount(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}, {Score: 2, Member: "b"}, {Score: 3, Member: "c"}}, false, false, false, false, false)
	n, _ := s.ZCount("k", 1, 2, false, false)
	if n != 2 {
		t.Errorf("ZCount(1,2): got %d, want 2", n)
	}
}

func TestZIncrBy(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}}, false, false, false, false, false)
	score, _ := s.ZIncrBy("k", 5, "a")
	if score != 6 {
		t.Errorf("ZIncrBy: got %g, want 6", score)
	}
}

func TestZPopMinMax(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}, {Score: 2, Member: "b"}, {Score: 3, Member: "c"}}, false, false, false, false, false)
	popped, _ := s.ZPopMin("k", 1)
	if len(popped) != 1 || popped[0].Member != "a" {
		t.Errorf("ZPopMin: got %v", popped)
	}
	popped, _ = s.ZPopMax("k", 1)
	if len(popped) != 1 || popped[0].Member != "c" {
		t.Errorf("ZPopMax: got %v", popped)
	}
}

func TestZRangeByLex(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 0, Member: "a"}, {Score: 0, Member: "b"}, {Score: 0, Member: "c"}, {Score: 0, Member: "d"}}, false, false, false, false, false)
	result, _ := s.ZRangeByLex("k", "[b", "[c", 0, -1)
	if !equal(result, []string{"b", "c"}) {
		t.Errorf("ZRangeByLex([b,[c): got %v", result)
	}
	result, _ = s.ZRangeByLex("k", "-", "+", 0, -1)
	if !equal(result, []string{"a", "b", "c", "d"}) {
		t.Errorf("ZRangeByLex(-,+): got %v", result)
	}
}

func TestZAddNX(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}}, false, false, false, false, false)
	// NX: don't update existing
	n, _ := s.ZAdd("k", []ZMember{{Score: 99, Member: "a"}, {Score: 2, Member: "b"}}, true, false, false, false, false)
	if n != 1 { // only b was added
		t.Errorf("ZAdd NX: added %d, want 1", n)
	}
	sc, _, _ := s.ZScore("k", "a")
	if sc != 1 {
		t.Errorf("ZAdd NX should not update existing score, got %g", sc)
	}
}

func TestZAddXX(t *testing.T) {
	s := newStore()
	s.ZAdd("k", []ZMember{{Score: 1, Member: "a"}}, false, false, false, false, false)
	// XX: only update existing
	n, _ := s.ZAdd("k", []ZMember{{Score: 99, Member: "a"}, {Score: 2, Member: "b"}}, false, true, false, false, false)
	if n != 0 { // a updated (but CH not set, so count = added new = 0), b not added
		t.Errorf("ZAdd XX: added %d, want 0", n)
	}
	sc, _, _ := s.ZScore("k", "a")
	if sc != 99 {
		t.Errorf("ZAdd XX should update existing score, got %g", sc)
	}
	card, _ := s.ZCard("k")
	if card != 1 {
		t.Errorf("ZAdd XX should not add new members, card=%d", card)
	}
}

func TestZSetWrongType(t *testing.T) {
	s := newStore()
	s.Set("k", "string")
	_, err := s.ZAdd("k", []ZMember{{Score: 1, Member: "m"}}, false, false, false, false, false)
	if err != ErrWrongType {
		t.Errorf("ZAdd on string key: got %v, want ErrWrongType", err)
	}
}

// ── SCAN tests ──────────────────────────────���─────────────────────────��────────

func TestScan(t *testing.T) {
	s := newStore()
	s.Set("a", "1")
	s.Set("b", "2")
	s.Set("c", "3")
	cursor, keys := s.Scan(0, "", 10)
	if cursor != 0 || len(keys) != 3 {
		t.Errorf("Scan: cursor=%d keys=%v", cursor, keys)
	}
}

func TestScanMatch(t *testing.T) {
	s := newStore()
	s.Set("foo1", "1")
	s.Set("foo2", "2")
	s.Set("bar", "3")
	_, keys := s.Scan(0, "foo*", 10)
	if len(keys) != 2 {
		t.Errorf("Scan MATCH foo*: got %v", keys)
	}
}

func TestScanCursor(t *testing.T) {
	s := newStore()
	for i := 0; i < 20; i++ {
		s.Set(string(rune('a'+i)), "v")
	}
	// Get first 10 with cursor 0.
	cursor, keys1 := s.Scan(0, "", 10)
	if cursor == 0 {
		t.Error("Scan should return non-zero cursor after partial scan")
	}
	if len(keys1) != 10 {
		t.Errorf("Scan page 1: got %d keys, want 10", len(keys1))
	}
	// Continue from cursor.
	cursor2, keys2 := s.Scan(cursor, "", 10)
	if cursor2 != 0 {
		t.Error("Scan second page should return cursor=0 (done)")
	}
	if len(keys2) != 10 {
		t.Errorf("Scan page 2: got %d keys, want 10", len(keys2))
	}
}

// ── SetAdv tests ──────────────────────────────────────────────────────────────��

func TestSetAdvNX(t *testing.T) {
	s := newStore()
	_, _, wasSet := s.SetAdv("k", "v1", 0, SetAdvOpts{NX: true})
	if !wasSet {
		t.Error("SetAdv NX on new key: want wasSet=true")
	}
	_, _, wasSet = s.SetAdv("k", "v2", 0, SetAdvOpts{NX: true})
	if wasSet {
		t.Error("SetAdv NX on existing key: want wasSet=false")
	}
	v, _ := s.Get("k")
	if v != "v1" {
		t.Errorf("value after NX failed set: got %q, want v1", v)
	}
}

func TestSetAdvXX(t *testing.T) {
	s := newStore()
	_, _, wasSet := s.SetAdv("k", "v", 0, SetAdvOpts{XX: true})
	if wasSet {
		t.Error("SetAdv XX on non-existent key: want wasSet=false")
	}
	s.Set("k", "old")
	old, oldExists, wasSet := s.SetAdv("k", "new", 0, SetAdvOpts{XX: true, Get: true})
	if !wasSet || old != "old" || !oldExists {
		t.Errorf("SetAdv XX+GET: wasSet=%v old=%q oldExists=%v", wasSet, old, oldExists)
	}
}

// ── helpers ─────────────────────��──────────────────────────────────────────────

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortStr(s []string) { sort.Strings(s) }
