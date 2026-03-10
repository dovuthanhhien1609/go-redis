package protocol

import (
	"io"
	"strings"
	"testing"
)

// Parser tests

func TestParser_SimpleString(t *testing.T) {
	p := NewParser(strings.NewReader("+OK\r\n"))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeSimpleString)
	assertStr(t, val, "OK")
}

func TestParser_Error(t *testing.T) {
	p := NewParser(strings.NewReader("-ERR unknown command\r\n"))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeError)
	assertStr(t, val, "ERR unknown command")
}

func TestParser_Integer(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{":0\r\n", 0},
		{":42\r\n", 42},
		{":-1\r\n", -1},
		{":9223372036854775807\r\n", 9223372036854775807}, // int64 max
	}
	for _, tc := range tests {
		p := NewParser(strings.NewReader(tc.input))
		val, err := p.readValue()
		assertNoError(t, err)
		assertType(t, val, TypeInteger)
		if val.Integer != tc.want {
			t.Errorf("integer: got %d, want %d", val.Integer, tc.want)
		}
	}
}

func TestParser_BulkString(t *testing.T) {
	p := NewParser(strings.NewReader("$5\r\nhello\r\n"))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeBulkString)
	assertStr(t, val, "hello")
}

func TestParser_BulkString_Empty(t *testing.T) {
	p := NewParser(strings.NewReader("$0\r\n\r\n"))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeBulkString)
	assertStr(t, val, "")
}

func TestParser_NullBulkString(t *testing.T) {
	p := NewParser(strings.NewReader("$-1\r\n"))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeNullBulkString)
}

func TestParser_BulkString_WithEmbeddedCRLF(t *testing.T) {
	// Bulk strings are binary-safe — \r\n inside the payload is fine
	// because we read by length, not by delimiter.
	payload := "he\r\nllo"
	input := "$7\r\nhe\r\nllo\r\n"
	p := NewParser(strings.NewReader(input))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeBulkString)
	assertStr(t, val, payload)
}

func TestParser_Array_SET(t *testing.T) {
	// *3\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n
	input := "*3\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n"
	p := NewParser(strings.NewReader(input))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeArray)
	if len(val.Array) != 3 {
		t.Fatalf("array length: got %d, want 3", len(val.Array))
	}
	assertStr(t, val.Array[0], "SET")
	assertStr(t, val.Array[1], "hello")
	assertStr(t, val.Array[2], "world")
}

func TestParser_Array_Empty(t *testing.T) {
	p := NewParser(strings.NewReader("*0\r\n"))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeArray)
	if len(val.Array) != 0 {
		t.Errorf("expected empty array, got %d elements", len(val.Array))
	}
}

func TestParser_NullArray(t *testing.T) {
	p := NewParser(strings.NewReader("*-1\r\n"))
	val, err := p.readValue()
	assertNoError(t, err)
	assertType(t, val, TypeNullArray)
}

func TestParser_ReadCommand_PING(t *testing.T) {
	p := NewParser(strings.NewReader("*1\r\n$4\r\nPING\r\n"))
	cmd, err := p.ReadCommand()
	assertNoError(t, err)
	if len(cmd) != 1 || cmd[0] != "PING" {
		t.Errorf("got %v, want [PING]", cmd)
	}
}

func TestParser_ReadCommand_GET(t *testing.T) {
	p := NewParser(strings.NewReader("*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n"))
	cmd, err := p.ReadCommand()
	assertNoError(t, err)
	if len(cmd) != 2 || cmd[0] != "GET" || cmd[1] != "foo" {
		t.Errorf("got %v, want [GET foo]", cmd)
	}
}

func TestParser_EOF(t *testing.T) {
	p := NewParser(strings.NewReader(""))
	_, err := p.ReadCommand()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestParser_MultipleCommands(t *testing.T) {
	// Two back-to-back commands in the same stream (pipelining)
	input := "*1\r\n$4\r\nPING\r\n" +
		"*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	p := NewParser(strings.NewReader(input))

	cmd1, err := p.ReadCommand()
	assertNoError(t, err)
	if cmd1[0] != "PING" {
		t.Errorf("first command: got %v, want PING", cmd1)
	}

	cmd2, err := p.ReadCommand()
	assertNoError(t, err)
	if len(cmd2) != 3 || cmd2[0] != "SET" {
		t.Errorf("second command: got %v, want [SET foo bar]", cmd2)
	}
}

// ── Serializer tests ──────────────────────────────────────────────────────────

func TestSerialize_SimpleString(t *testing.T) {
	got := Serialize(SimpleString("OK"))
	want := "+OK\r\n"
	assertEqual(t, got, want)
}

func TestSerialize_Error(t *testing.T) {
	got := Serialize(Error("ERR bad command"))
	want := "-ERR bad command\r\n"
	assertEqual(t, got, want)
}

func TestSerialize_Integer(t *testing.T) {
	got := Serialize(Integer(42))
	want := ":42\r\n"
	assertEqual(t, got, want)
}

func TestSerialize_BulkString(t *testing.T) {
	got := Serialize(BulkString("hello"))
	want := "$5\r\nhello\r\n"
	assertEqual(t, got, want)
}

func TestSerialize_NullBulkString(t *testing.T) {
	got := Serialize(NullBulkString())
	want := "$-1\r\n"
	assertEqual(t, got, want)
}

func TestSerialize_Array(t *testing.T) {
	got := Serialize(Array([]Response{
		BulkString("SET"),
		BulkString("hello"),
		BulkString("world"),
	}))
	want := "*3\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n"
	assertEqual(t, got, want)
}

// ── Round-trip tests ──────────────────────────────────────────────────────────

// TestRoundTrip verifies that serializing a Response and then parsing it back
// produces the original value.
func TestRoundTrip_BulkString(t *testing.T) {
	original := BulkString("hello world")
	wire := Serialize(original)
	p := NewParser(strings.NewReader(wire))
	decoded, err := p.readValue()
	assertNoError(t, err)
	assertType(t, decoded, TypeBulkString)
	assertStr(t, decoded, "hello world")
}

func TestRoundTrip_Array(t *testing.T) {
	original := Array([]Response{
		BulkString("SET"),
		BulkString("key"),
		BulkString("value"),
	})
	wire := Serialize(original)
	p := NewParser(strings.NewReader(wire))
	decoded, err := p.readValue()
	assertNoError(t, err)
	assertType(t, decoded, TypeArray)
	if len(decoded.Array) != 3 {
		t.Fatalf("array length: got %d, want 3", len(decoded.Array))
	}
	assertStr(t, decoded.Array[0], "SET")
	assertStr(t, decoded.Array[1], "key")
	assertStr(t, decoded.Array[2], "value")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertType(t *testing.T, r Response, want Type) {
	t.Helper()
	if r.Type != want {
		t.Errorf("type: got %d, want %d", r.Type, want)
	}
}

func assertStr(t *testing.T, r Response, want string) {
	t.Helper()
	if r.Str != want {
		t.Errorf("str: got %q, want %q", r.Str, want)
	}
}

func assertEqual(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
