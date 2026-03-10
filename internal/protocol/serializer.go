package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

// Serialize converts a Response into its RESP wire representation.
//
// The returned string is ready to be written directly to a net.Conn.
//
// Examples:
//
//	SimpleString("OK")  → "+OK\r\n"
//	Error("ERR bad")    → "-ERR bad\r\n"
//	Integer(42)         → ":42\r\n"
//	BulkString("hello") → "$5\r\nhello\r\n"
//	NullBulkString()    → "$-1\r\n"
//	Array([...])        → "*N\r\n" + each element serialized
func Serialize(r Response) string {
	switch r.Type {
	case TypeSimpleString:
		return "+" + r.Str + "\r\n"

	case TypeError:
		return "-" + r.Str + "\r\n"

	case TypeInteger:
		return ":" + strconv.FormatInt(r.Integer, 10) + "\r\n"

	case TypeBulkString:
		return "$" + strconv.Itoa(len(r.Str)) + "\r\n" + r.Str + "\r\n"

	case TypeNullBulkString:
		return "$-1\r\n"

	case TypeNullArray:
		return "*-1\r\n"

	case TypeArray:
		var b strings.Builder
		b.WriteString("*")
		b.WriteString(strconv.Itoa(len(r.Array)))
		b.WriteString("\r\n")
		for _, elem := range r.Array {
			b.WriteString(Serialize(elem))
		}
		return b.String()

	default:
		// Should never happen if callers use the constructor functions.
		return fmt.Sprintf("-ERR internal: unknown response type %d\r\n", r.Type)
	}
}
