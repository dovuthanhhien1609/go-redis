package server

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/hiendvt/go-redis/internal/commands"
	"github.com/hiendvt/go-redis/internal/config"
	"github.com/hiendvt/go-redis/internal/logger"
	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// ── Test infrastructure ───────────────────────────────────────────────────────

// newTestServer starts a real Server on a random OS-assigned port.
// Returns the bound "host:port" and a cancel func that stops the server and
// waits for full shutdown before returning.
func newTestServer(t *testing.T) (addr string, cancel func()) {
	t.Helper()

	// ":0" asks the OS to assign a free port immediately.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr = ln.Addr().String()

	cfg := &config.Config{MaxClients: 0}
	log := logger.New("error") // silence logs during tests
	store := storage.NewMemoryStore()
	router := commands.NewRouter(store, nil, nil) // AOF and pub/sub disabled
	srv := New(cfg, log, router, nil)

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Serve(ctx, ln) //nolint:errcheck
	}()

	cancel = func() {
		stop() // cancel ctx → listener closed → acceptLoop exits
		<-done // wait for wg.Wait() inside Serve to return
	}
	return addr, cancel
}

// testClient wraps a net.Conn with our RESP parser for clean test code.
type testClient struct {
	t      *testing.T
	conn   net.Conn
	parser *protocol.Parser
}

func newTestClient(t *testing.T, addr string) *testClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testClient{t: t, conn: conn, parser: protocol.NewParser(conn)}
}

// do sends args as a RESP Array and reads back one RESP value.
func (c *testClient) do(args ...string) protocol.Response {
	c.t.Helper()
	elems := make([]protocol.Response, len(args))
	for i, a := range args {
		elems[i] = protocol.BulkString(a)
	}
	if _, err := fmt.Fprint(c.conn, protocol.Serialize(protocol.Array(elems))); err != nil {
		c.t.Fatalf("send %v: %v", args, err)
	}
	r, err := c.parser.ReadValue()
	if err != nil {
		c.t.Fatalf("recv %v: %v", args, err)
	}
	return r
}

// ── Integration tests ─────────────────────────────────────────────────────────

func TestServer_PING(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	assertSimpleString(t, c.do("PING"), "PONG")
}

func TestServer_PING_WithMessage(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	assertBulkString(t, c.do("PING", "hello"), "hello")
}

func TestServer_SET_GET(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	assertSimpleString(t, c.do("SET", "key", "value"), "OK")
	assertBulkString(t, c.do("GET", "key"), "value")
}

func TestServer_GET_Missing(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	r := c.do("GET", "nokey")
	if r.Type != protocol.TypeNullBulkString {
		t.Errorf("type: got %d, want NullBulkString", r.Type)
	}
}

func TestServer_SET_Overwrite(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	c.do("SET", "k", "v1")
	c.do("SET", "k", "v2")
	assertBulkString(t, c.do("GET", "k"), "v2")
}

func TestServer_DEL(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	c.do("SET", "a", "1")
	c.do("SET", "b", "2")

	assertInteger(t, c.do("DEL", "a", "b", "missing"), 2)
	assertNullBulkString(t, c.do("GET", "a"))
	assertNullBulkString(t, c.do("GET", "b"))
}

func TestServer_EXISTS(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	c.do("SET", "x", "1")
	assertInteger(t, c.do("EXISTS", "x"), 1)
	assertInteger(t, c.do("EXISTS", "nope"), 0)
	// Repeated key counts multiple times.
	assertInteger(t, c.do("EXISTS", "x", "x"), 2)
}

func TestServer_KEYS(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	c.do("SET", "k1", "v1")
	c.do("SET", "k2", "v2")

	r := c.do("KEYS", "*")
	if r.Type != protocol.TypeArray {
		t.Fatalf("type: got %d, want Array", r.Type)
	}
	if len(r.Array) != 2 {
		t.Errorf("KEYS count: got %d, want 2", len(r.Array))
	}
}

func TestServer_UnknownCommand(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	r := c.do("NOTACOMMAND")
	if r.Type != protocol.TypeError {
		t.Errorf("type: got %d, want Error", r.Type)
	}
}

func TestServer_WrongArgCount(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	// GET requires exactly one key argument.
	r := c.do("GET")
	if r.Type != protocol.TypeError {
		t.Errorf("type: got %d, want Error", r.Type)
	}
	// Connection must still be usable after an error response.
	assertSimpleString(t, c.do("PING"), "PONG")
}

func TestServer_CaseInsensitiveCommands(t *testing.T) {
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	assertSimpleString(t, c.do("ping"), "PONG")
	assertSimpleString(t, c.do("Ping"), "PONG")
	assertSimpleString(t, c.do("set", "k", "v"), "OK")
	assertBulkString(t, c.do("get", "k"), "v")
}

func TestServer_Pipelining(t *testing.T) {
	// Write three commands in one write call without waiting for responses.
	// Verifies the parser correctly handles a stream of consecutive commands.
	addr, cancel := newTestServer(t)
	defer cancel()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	pipeline :=
		protocol.Serialize(protocol.Array([]protocol.Response{
			protocol.BulkString("SET"), protocol.BulkString("a"), protocol.BulkString("1"),
		})) +
			protocol.Serialize(protocol.Array([]protocol.Response{
				protocol.BulkString("SET"), protocol.BulkString("b"), protocol.BulkString("2"),
			})) +
			protocol.Serialize(protocol.Array([]protocol.Response{
				protocol.BulkString("GET"), protocol.BulkString("a"),
			}))

	if _, err := fmt.Fprint(conn, pipeline); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}

	p := protocol.NewParser(conn)
	r1, _ := p.ReadValue()
	r2, _ := p.ReadValue()
	r3, _ := p.ReadValue()

	assertSimpleString(t, r1, "OK")
	assertSimpleString(t, r2, "OK")
	assertBulkString(t, r3, "1")
}

func TestServer_ConcurrentClients(t *testing.T) {
	// 50 goroutines each SET and GET their own key simultaneously.
	// Verifies no data races and no cross-client contamination.
	addr, cancel := newTestServer(t)
	defer cancel()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			c := newTestClient(t, addr)
			key := fmt.Sprintf("key:%d", i)
			val := fmt.Sprintf("val:%d", i)

			if r := c.do("SET", key, val); r.Type != protocol.TypeSimpleString {
				t.Errorf("goroutine %d SET: unexpected type %d", i, r.Type)
			}
			if r := c.do("GET", key); r.Str != val {
				t.Errorf("goroutine %d GET: got %q, want %q", i, r.Str, val)
			}
		}(i)
	}
	wg.Wait()
}

func TestServer_MultipleCommandsPerConnection(t *testing.T) {
	// One persistent connection, many sequential commands.
	// Verifies the handler read loop correctly re-enters for the next command.
	addr, cancel := newTestServer(t)
	defer cancel()

	c := newTestClient(t, addr)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("k%d", i)
		val := fmt.Sprintf("v%d", i)
		assertSimpleString(t, c.do("SET", key, val), "OK")
		assertBulkString(t, c.do("GET", key), val)
	}
}

func TestServer_MaxClients(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	cfg := &config.Config{MaxClients: 1}
	log := logger.New("error")
	router := commands.NewRouter(storage.NewMemoryStore(), nil, nil)
	srv := New(cfg, log, router, nil)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go srv.Serve(ctx, ln) //nolint:errcheck

	// First client — accepted, PING should work.
	c1, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer c1.Close()
	p1 := protocol.NewParser(c1)
	fmt.Fprint(c1, protocol.Serialize(protocol.Array([]protocol.Response{protocol.BulkString("PING")})))
	r, _ := p1.ReadValue()
	assertSimpleString(t, r, "PONG")

	// Second client — server should close it immediately (max = 1 and c1 holds it).
	c2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer c2.Close()

	buf := make([]byte, 64)
	// The server either sends nothing and closes, or closes immediately.
	// Either way, a subsequent read will return 0 bytes or io.EOF.
	n, _ := c2.Read(buf)
	if n != 0 {
		// Accept that the server may send a response before closing.
		t.Logf("second client received %d bytes before close: %q", n, buf[:n])
	}
}

// ── Assertion helpers ─────────────────────────────────────────────────────────

func assertSimpleString(t *testing.T, r protocol.Response, want string) {
	t.Helper()
	if r.Type != protocol.TypeSimpleString || r.Str != want {
		t.Errorf("SimpleString: got (type=%d %q), want %q", r.Type, r.Str, want)
	}
}

func assertBulkString(t *testing.T, r protocol.Response, want string) {
	t.Helper()
	if r.Type != protocol.TypeBulkString || r.Str != want {
		t.Errorf("BulkString: got (type=%d %q), want %q", r.Type, r.Str, want)
	}
}

func assertNullBulkString(t *testing.T, r protocol.Response) {
	t.Helper()
	if r.Type != protocol.TypeNullBulkString {
		t.Errorf("NullBulkString: got type %d", r.Type)
	}
}

func assertInteger(t *testing.T, r protocol.Response, want int64) {
	t.Helper()
	if r.Type != protocol.TypeInteger || r.Integer != want {
		t.Errorf("Integer: got (type=%d val=%d), want %d", r.Type, r.Integer, want)
	}
}
