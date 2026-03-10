// Package protocol implements encoding and decoding of the Redis
// Serialization Protocol (RESP v2).
//
// Every value in RESP is one of five types, identified by the first byte:
//
//	'+' SimpleString  — single-line, no binary
//	'-' Error         — single-line error message
//	':' Integer       — signed 64-bit integer
//	'$' BulkString    — binary-safe, length-prefixed string
//	'*' Array         — ordered list of RESP values
package protocol

// Type identifies which RESP data type a Response carries.
type Type int

const (
	TypeSimpleString  Type = iota // +OK\r\n
	TypeError                     // -ERR ...\r\n
	TypeInteger                   // :42\r\n
	TypeBulkString                // $5\r\nhello\r\n
	TypeNullBulkString            // $-1\r\n  (used for "key not found")
	TypeArray                     // *3\r\n...
	TypeNullArray                 // *-1\r\n
)

// Response is the unified value type used throughout the system.
// Every command handler returns a Response; the connection handler serializes it.
//
// Only the fields relevant to the Type are populated:
//
//	TypeSimpleString  → Str
//	TypeError         → Str
//	TypeInteger       → Integer
//	TypeBulkString    → Str
//	TypeArray         → Array
type Response struct {
	Type    Type
	Str     string
	Integer int64
	Array   []Response
}

// Convenience constructors keep handler code readable.

// SimpleString returns a +OK-style response.
func SimpleString(s string) Response {
	return Response{Type: TypeSimpleString, Str: s}
}

// Error returns a -ERR-style response.
func Error(s string) Response {
	return Response{Type: TypeError, Str: s}
}

// Integer returns a :n-style response.
func Integer(n int64) Response {
	return Response{Type: TypeInteger, Integer: n}
}

// BulkString returns a $n-style response.
func BulkString(s string) Response {
	return Response{Type: TypeBulkString, Str: s}
}

// NullBulkString returns a $-1 response (key not found).
func NullBulkString() Response {
	return Response{Type: TypeNullBulkString}
}

// Array returns a *n-style response.
func Array(elems []Response) Response {
	return Response{Type: TypeArray, Array: elems}
}
