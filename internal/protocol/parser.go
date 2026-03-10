package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Parser reads from a buffered reader and decodes RESP messages.
//
// Usage:
//
//	p := NewParser(conn)
//	command, err := p.ReadCommand()
type Parser struct {
	rd *bufio.Reader
}

// NewParser wraps r in a buffered reader and returns a Parser.
func NewParser(r io.Reader) *Parser {
	return &Parser{rd: bufio.NewReader(r)}
}

// ReadValue reads exactly one RESP value from the stream and returns it as a
// Response. Unlike ReadCommand, it accepts any RESP type — not just arrays.
// This is used by test clients and future response-reading code.
func (p *Parser) ReadValue() (Response, error) {
	return p.readValue()
}

// ReadCommand reads exactly one RESP message from the stream and returns it
// as a slice of strings (command name + arguments).
//
// Redis clients always send commands as RESP Arrays of Bulk Strings:
//
//	*3\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n
//	→ []string{"SET", "hello", "world"}
//
// ReadCommand returns io.EOF when the client closes the connection.
func (p *Parser) ReadCommand() ([]string, error) {
	val, err := p.readValue()
	if err != nil {
		return nil, err
	}

	// Commands must arrive as arrays.
	if val.Type != TypeArray {
		return nil, fmt.Errorf("protocol: expected array, got type %d", val.Type)
	}
	if len(val.Array) == 0 {
		return nil, fmt.Errorf("protocol: empty command array")
	}

	// Each element of the array must be a bulk string.
	parts := make([]string, len(val.Array))
	for i, elem := range val.Array {
		if elem.Type != TypeBulkString {
			return nil, fmt.Errorf("protocol: command element %d is not a bulk string", i)
		}
		parts[i] = elem.Str
	}
	return parts, nil
}

// readValue recursively reads one complete RESP value from the stream.
func (p *Parser) readValue() (Response, error) {
	// Read the type prefix byte (e.g. '*', '$', '+', '-', ':').
	prefix, err := p.rd.ReadByte()
	if err != nil {
		return Response{}, err
	}

	switch prefix {
	case '+':
		return p.readSimpleString()
	case '-':
		return p.readError()
	case ':':
		return p.readInteger()
	case '$':
		return p.readBulkString()
	case '*':
		return p.readArray()
	default:
		return Response{}, fmt.Errorf("protocol: unknown type prefix %q", prefix)
	}
}

// readSimpleString reads the rest of a '+'-prefixed line.
// Format: +<string>\r\n
func (p *Parser) readSimpleString() (Response, error) {
	line, err := p.readLine()
	if err != nil {
		return Response{}, err
	}
	return SimpleString(line), nil
}

// readError reads the rest of a '-'-prefixed line.
// Format: -<message>\r\n
func (p *Parser) readError() (Response, error) {
	line, err := p.readLine()
	if err != nil {
		return Response{}, err
	}
	return Error(line), nil
}

// readInteger reads the rest of a ':'-prefixed line.
// Format: :<integer>\r\n
func (p *Parser) readInteger() (Response, error) {
	line, err := p.readLine()
	if err != nil {
		return Response{}, err
	}
	n, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return Response{}, fmt.Errorf("protocol: invalid integer %q: %w", line, err)
	}
	return Integer(n), nil
}

// readBulkString reads a '$'-prefixed length-prefixed binary string.
// Formats:
//
//	$<length>\r\n<data>\r\n   — normal bulk string
//	$-1\r\n                   — null bulk string
func (p *Parser) readBulkString() (Response, error) {
	line, err := p.readLine()
	if err != nil {
		return Response{}, err
	}

	length, err := strconv.Atoi(line)
	if err != nil {
		return Response{}, fmt.Errorf("protocol: invalid bulk string length %q: %w", line, err)
	}

	// $-1\r\n → null bulk string (key not found, etc.)
	if length == -1 {
		return NullBulkString(), nil
	}
	if length < 0 {
		return Response{}, fmt.Errorf("protocol: negative bulk string length %d", length)
	}

	// Read exactly <length> bytes of data, then consume the trailing \r\n.
	data := make([]byte, length)
	if _, err := io.ReadFull(p.rd, data); err != nil {
		return Response{}, fmt.Errorf("protocol: reading bulk string data: %w", err)
	}

	// Consume the mandatory trailing \r\n after the data.
	if err := p.consumeCRLF(); err != nil {
		return Response{}, err
	}

	return BulkString(string(data)), nil
}

// readArray reads a '*'-prefixed array of RESP values.
// Formats:
//
//	*<count>\r\n<element>...  — array with <count> elements
//	*-1\r\n                   — null array
//	*0\r\n                    — empty array
func (p *Parser) readArray() (Response, error) {
	line, err := p.readLine()
	if err != nil {
		return Response{}, err
	}

	count, err := strconv.Atoi(line)
	if err != nil {
		return Response{}, fmt.Errorf("protocol: invalid array length %q: %w", line, err)
	}

	if count == -1 {
		return Response{Type: TypeNullArray}, nil
	}
	if count < 0 {
		return Response{}, fmt.Errorf("protocol: negative array length %d", count)
	}

	// Recursively read each element.
	elems := make([]Response, count)
	for i := range elems {
		elems[i], err = p.readValue()
		if err != nil {
			return Response{}, fmt.Errorf("protocol: reading array element %d: %w", i, err)
		}
	}
	return Array(elems), nil
}

// readLine reads bytes up to and including \r\n, then returns the content
// without the trailing \r\n.
func (p *Parser) readLine() (string, error) {
	// ReadString reads until the delimiter (inclusive), handling partial reads.
	line, err := p.rd.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("protocol: reading line: %w", err)
	}
	// Strip \r\n
	return strings.TrimRight(line, "\r\n"), nil
}

// consumeCRLF reads and discards exactly two bytes (\r\n).
// Called after reading bulk string data.
func (p *Parser) consumeCRLF() error {
	crlf := make([]byte, 2)
	if _, err := io.ReadFull(p.rd, crlf); err != nil {
		return fmt.Errorf("protocol: reading CRLF: %w", err)
	}
	if crlf[0] != '\r' || crlf[1] != '\n' {
		return fmt.Errorf("protocol: expected CRLF, got %q", crlf)
	}
	return nil
}
